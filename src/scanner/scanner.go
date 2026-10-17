package scanner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lyrarma/cloud-api/src/config"
	"github.com/lyrarma/cloud-api/src/models"
	"github.com/lyrarma/cloud-api/src/storage"
)

const (
	reasonMalware       = "malware"
	reasonBlockedType   = "blocked_type"
	reasonInvalidFile   = "invalid_file"
	reasonInvalidMedia  = "invalid_media"
	reasonArchiveLimits = "archive_limits"
	reasonContentPolicy = "content_policy"
	reasonScannerError  = "scanner_error"
	maxCommandOutput    = 16 * 1024 * 1024
)

var archiveMIMETypes = map[string]struct{}{
	"application/gzip":             {},
	"application/vnd.rar":          {},
	"application/x-7z-compressed":  {},
	"application/x-bzip2":          {},
	"application/x-compressed-tar": {},
	"application/x-gtar":           {},
	"application/x-lzip":           {},
	"application/x-lzma":           {},
	"application/x-rar":            {},
	"application/x-rar-compressed": {},
	"application/x-tar":            {},
	"application/x-xz":             {},
	"application/zip":              {},
	"application/zstd":             {},
}

type scanResult struct {
	status              models.FileStatus
	detectedContentType string
	sha256              string
	reason              string
}

type archiveBudget struct {
	files int
	bytes int64
}

// Service performs background checks for files in the quarantine bucket
type Service struct {
	metadataStore *storage.SQLStore
	fileStore     *storage.S3Store
	settings      config.ScannerConfig
	blockedMIMEs  map[string]struct{}
	logger        *slog.Logger
	cancel        context.CancelFunc
	workers       sync.WaitGroup
}

// New creates a scanning service with the provided stores and settings
func New(metadataStore *storage.SQLStore, fileStore *storage.S3Store, settings config.ScannerConfig, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	blockedMIMEs := make(map[string]struct{}, len(settings.BlockedMIMETypes()))
	for _, mimeType := range settings.BlockedMIMETypes() {
		blockedMIMEs[strings.ToLower(strings.TrimSpace(mimeType))] = struct{}{}
	}
	return &Service{metadataStore: metadataStore, fileStore: fileStore, settings: settings, blockedMIMEs: blockedMIMEs, logger: logger}
}

// Start launches the file queue workers
func (s *Service) Start(parent context.Context) error {
	if s.cancel != nil {
		return errors.New("file scanner is already running")
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	if !s.settings.Enabled() {
		s.logger.Info("file scanning disabled; workers only promote uploaded files")
	}
	for workerID := 0; workerID < s.settings.WorkerCount(); workerID++ {
		s.workers.Add(1)
		go s.worker(ctx, workerID)
	}
	return nil
}

// Close stops the workers and waits for active commands
func (s *Service) Close() {
	if s == nil || s.cancel == nil {
		return
	}
	s.cancel()
	s.workers.Wait()
	s.cancel = nil
}

func (s *Service) worker(ctx context.Context, workerID int) {
	defer s.workers.Done()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		entry, scanStartedAt, found, err := s.metadataStore.ClaimFileForScan(ctx, time.Now().UTC().Add(-s.settings.Timeout()))
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				s.logger.Error("failed to claim file scan", "worker_id", workerID, "err", err)
			}
			timer.Reset(s.settings.PollInterval())
			continue
		}
		if !found {
			timer.Reset(s.settings.PollInterval())
			continue
		}
		s.process(ctx, entry, scanStartedAt)
		timer.Reset(0)
	}
}

func (s *Service) process(parent context.Context, entry models.FileEntry, scanStartedAt time.Time) {
	scanContext, cancelScan := context.WithTimeout(parent, s.settings.Timeout())
	result := s.inspect(scanContext, entry)
	objectKey := fileObjectKey(entry.OwnerID, entry.ID)
	promoted := false
	if result.status == models.FileApproved {
		contentType := result.detectedContentType
		if contentType == "" {
			contentType = entry.ContentType
		}
		if err := s.fileStore.PromoteQuarantineFile(scanContext, objectKey, objectKey, contentType); err != nil {
			s.logger.Error("failed to promote approved file", "file_id", entry.ID, "err", err)
			result.status = models.FileFailed
			result.reason = reasonScannerError
		} else {
			promoted = true
		}
	}
	cancelScan()

	persistContext, cancelPersist := context.WithTimeout(parent, 30*time.Second)
	defer cancelPersist()
	if err := s.metadataStore.CompleteFileScan(persistContext, entry.ID, result.status, result.detectedContentType, result.sha256, result.reason, scanStartedAt); err != nil {
		s.logger.Error("failed to persist file scan result", "file_id", entry.ID, "status", result.status, "err", err)
		if errors.Is(err, storage.ErrFileNotFound) {
			if promoted {
				_ = s.fileStore.DeleteFile(persistContext, objectKey)
			}
			_ = s.fileStore.DeleteQuarantineFile(persistContext, objectKey)
		}
		return
	}
	if result.status != models.FileApproved {
		if err := s.fileStore.DeleteFile(persistContext, objectKey); err != nil {
			s.logger.Error("failed to remove non-approved production object", "file_id", entry.ID, "err", err)
		}
	}
	if result.status == models.FileApproved || result.status == models.FileBlocked {
		if err := s.fileStore.DeleteQuarantineFile(persistContext, objectKey); err != nil {
			s.logger.Error("failed to remove quarantine object", "file_id", entry.ID, "err", err)
		}
	}
	s.logger.Info("file scan completed", "file_id", entry.ID, "status", result.status, "reason", result.reason)
}

func (s *Service) inspect(ctx context.Context, entry models.FileEntry) scanResult {
	if !s.settings.Enabled() {
		return scanResult{status: models.FileApproved}
	}
	result := scanResult{status: models.FileFailed, reason: reasonScannerError}
	temporaryFile, err := os.CreateTemp("", "lyrarma-scan-*")
	if err != nil {
		s.logger.Error("failed to create scan file", "file_id", entry.ID, "err", err)
		return result
	}
	temporaryPath := temporaryFile.Name()
	defer os.Remove(temporaryPath)
	body, _, err := s.fileStore.DownloadQuarantineFile(ctx, fileObjectKey(entry.OwnerID, entry.ID))
	if err != nil {
		_ = temporaryFile.Close()
		s.logger.Error("failed to download quarantine object", "file_id", entry.ID, "err", err)
		return result
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporaryFile, hash), io.LimitReader(body, safeReadLimit(entry.Size)))
	bodyCloseErr := body.Close()
	fileCloseErr := temporaryFile.Close()
	if copyErr != nil || bodyCloseErr != nil || fileCloseErr != nil {
		s.logger.Error("failed to stage quarantine object", "file_id", entry.ID, "copy_err", copyErr, "body_close_err", bodyCloseErr, "file_close_err", fileCloseErr)
		return result
	}
	result.sha256 = hex.EncodeToString(hash.Sum(nil))
	if written != entry.Size {
		result.reason = reasonInvalidFile
		return result
	}
	outcome, detectedMIME, reason, err := s.inspectPath(ctx, temporaryPath, 0, &archiveBudget{})
	result.detectedContentType = detectedMIME
	if err != nil {
		s.logger.Error("file inspection failed", "file_id", entry.ID, "err", err)
		return result
	}
	result.status = outcome
	result.reason = reason
	return result
}

func safeReadLimit(size int64) int64 {
	if size < 0 || size == math.MaxInt64 {
		return math.MaxInt64
	}
	return size + 1
}

func (s *Service) inspectPath(ctx context.Context, filePath string, archiveDepth int, budget *archiveBudget) (models.FileStatus, string, string, error) {
	detectedMIME, err := s.detectMIME(ctx, filePath)
	if err != nil {
		return models.FileFailed, "", reasonScannerError, err
	}
	if _, blocked := s.blockedMIMEs[detectedMIME]; blocked {
		return models.FileBlocked, detectedMIME, reasonBlockedType, nil
	}
	infected, err := s.scanAntivirus(ctx, filePath)
	if err != nil {
		return models.FileFailed, detectedMIME, reasonScannerError, err
	}
	if infected {
		return models.FileBlocked, detectedMIME, reasonMalware, nil
	}
	if _, archive := archiveMIMETypes[detectedMIME]; archive {
		status, reason, err := s.inspectArchive(ctx, filePath, archiveDepth+1, budget)
		return status, detectedMIME, reason, err
	}
	if strings.HasPrefix(detectedMIME, "image/") {
		if err := s.validateImage(ctx, filePath); err != nil {
			return models.FileBlocked, detectedMIME, reasonInvalidMedia, nil
		}
	}
	if strings.HasPrefix(detectedMIME, "audio/") || strings.HasPrefix(detectedMIME, "video/") {
		if err := s.validateAudioVideo(ctx, filePath); err != nil {
			return models.FileBlocked, detectedMIME, reasonInvalidMedia, nil
		}
	}
	if strings.HasPrefix(detectedMIME, "video/") && strings.TrimSpace(s.settings.ModerationCommand()) != "" {
		blocked, framesFound, err := s.moderateVideo(ctx, filePath)
		if err != nil {
			return models.FileFailed, detectedMIME, reasonScannerError, err
		}
		if !framesFound {
			return models.FileBlocked, detectedMIME, reasonInvalidMedia, nil
		}
		if blocked {
			return models.FileBlocked, detectedMIME, reasonContentPolicy, nil
		}
		return models.FileApproved, detectedMIME, "", nil
	}
	blocked, err := s.moderate(ctx, filePath, detectedMIME)
	if err != nil {
		return models.FileFailed, detectedMIME, reasonScannerError, err
	}
	if blocked {
		return models.FileBlocked, detectedMIME, reasonContentPolicy, nil
	}
	return models.FileApproved, detectedMIME, "", nil
}

func (s *Service) detectMIME(ctx context.Context, filePath string) (string, error) {
	output, exitCode, err := runCommand(ctx, s.settings.FileCommand(), "--brief", "--mime-type", "--", filePath)
	if err != nil {
		return "", fmt.Errorf("detect MIME type: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("detect MIME type: utility exited with code %d: %s", exitCode, commandMessage(output))
	}
	mimeType := strings.ToLower(strings.TrimSpace(string(output)))
	if mimeType == "" || strings.ContainsAny(mimeType, "\r\n") {
		return "", errors.New("detect MIME type: invalid utility output")
	}
	return mimeType, nil
}

func (s *Service) scanAntivirus(ctx context.Context, filePath string) (bool, error) {
	output, exitCode, err := runCommand(ctx, s.settings.AntivirusCommand(), "--no-summary", "--", filePath)
	if err != nil {
		return false, fmt.Errorf("run antivirus: %w", err)
	}
	switch exitCode {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("run antivirus: utility exited with code %d: %s", exitCode, commandMessage(output))
	}
}

func (s *Service) validateImage(ctx context.Context, filePath string) error {
	output, exitCode, err := runCommand(
		ctx,
		s.settings.ImageCommand(),
		"-ping",
		"-format", "%w %h",
		filePath+"[0]",
	)
	if err != nil {
		return fmt.Errorf("validate image: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("validate image: utility exited with code %d: %s", exitCode, commandMessage(output))
	}
	dimensions := strings.Fields(string(output))
	if len(dimensions) != 2 {
		return errors.New("validate image: invalid utility output")
	}
	width, widthErr := strconv.ParseInt(dimensions[0], 10, 64)
	height, heightErr := strconv.ParseInt(dimensions[1], 10, 64)
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return errors.New("validate image: invalid dimensions")
	}
	if width > s.settings.MaxImagePixels()/height {
		return errors.New("validate image: pixel limit exceeded")
	}
	return nil
}

func (s *Service) validateAudioVideo(ctx context.Context, filePath string) error {
	output, exitCode, err := runCommand(ctx, s.settings.VideoCommand(), "-v", "error", "-show_entries", "format=duration:stream=codec_type,codec_name", "-of", "json", filePath)
	if err != nil {
		return fmt.Errorf("validate media: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("validate media: utility exited with code %d: %s", exitCode, commandMessage(output))
	}
	return nil
}

func (s *Service) inspectArchive(ctx context.Context, archivePath string, depth int, budget *archiveBudget) (models.FileStatus, string, error) {
	if depth > s.settings.MaxArchiveDepth() {
		return models.FileBlocked, reasonArchiveLimits, nil
	}
	listing, exitCode, err := runCommand(ctx, s.settings.ArchiveCommand(), "l", "-slt", "--", archivePath)
	if err != nil {
		return models.FileFailed, reasonScannerError, fmt.Errorf("list archive: %w", err)
	}
	if exitCode != 0 {
		return models.FileBlocked, reasonInvalidFile, nil
	}
	files, unpackedBytes, err := archiveMetrics(listing)
	if err != nil {
		return models.FileBlocked, reasonInvalidFile, nil
	}
	if budget.files > s.settings.MaxArchiveFiles()-files || budget.bytes > s.settings.MaxArchiveBytes()-unpackedBytes {
		return models.FileBlocked, reasonArchiveLimits, nil
	}
	budget.files += files
	budget.bytes += unpackedBytes
	_, exitCode, err = runCommand(ctx, s.settings.ArchiveCommand(), "t", "--", archivePath)
	if err != nil {
		return models.FileFailed, reasonScannerError, fmt.Errorf("test archive: %w", err)
	}
	if exitCode != 0 {
		return models.FileBlocked, reasonInvalidFile, nil
	}
	extractionDir, err := os.MkdirTemp("", "lyrarma-archive-*")
	if err != nil {
		return models.FileFailed, reasonScannerError, fmt.Errorf("create archive directory: %w", err)
	}
	defer os.RemoveAll(extractionDir)
	_, exitCode, err = runCommand(ctx, s.settings.ArchiveCommand(), "x", "-y", "-bd", "-o"+extractionDir, "--", archivePath)
	if err != nil {
		return models.FileFailed, reasonScannerError, fmt.Errorf("extract archive: %w", err)
	}
	if exitCode != 0 {
		return models.FileBlocked, reasonInvalidFile, nil
	}
	walkErr := filepath.WalkDir(extractionDir, func(extractedPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errArchiveBlocked
		}
		status, _, reason, err := s.inspectPath(ctx, extractedPath, depth, budget)
		if err != nil {
			return err
		}
		if status != models.FileApproved {
			return archiveOutcomeError{status: status, reason: reason}
		}
		return nil
	})
	if walkErr == nil {
		return models.FileApproved, "", nil
	}
	var outcome archiveOutcomeError
	if errors.As(walkErr, &outcome) {
		return outcome.status, outcome.reason, nil
	}
	if errors.Is(walkErr, errArchiveBlocked) {
		return models.FileBlocked, reasonInvalidFile, nil
	}
	return models.FileFailed, reasonScannerError, fmt.Errorf("inspect extracted archive: %w", walkErr)
}

var errArchiveBlocked = errors.New("archive contains an unsupported entry")

type archiveOutcomeError struct {
	status models.FileStatus
	reason string
}

func (err archiveOutcomeError) Error() string { return "archive entry did not pass inspection" }

func archiveMetrics(output []byte) (int, int64, error) {
	files := 0
	var unpackedBytes int64
	inEntries := false
	for _, block := range strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n\n") {
		lines := strings.Split(block, "\n")
		isDirectory := false
		sizeFound := false
		var size int64
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "----------" {
				inEntries = true
				continue
			}
			if !inEntries {
				continue
			}
			if line == "Folder = +" {
				isDirectory = true
			}
			if strings.HasPrefix(line, "Size = ") {
				rawSize := strings.TrimSpace(strings.TrimPrefix(line, "Size = "))
				parsed, err := strconv.ParseInt(rawSize, 10, 64)
				if err != nil || parsed < 0 {
					return 0, 0, errors.New("invalid archive size")
				}
				size, sizeFound = parsed, true
			}
		}
		if !inEntries || isDirectory || !sizeFound {
			continue
		}
		if unpackedBytes > math.MaxInt64-size {
			return 0, 0, errors.New("archive size overflow")
		}
		files++
		unpackedBytes += size
	}
	return files, unpackedBytes, nil
}

func (s *Service) moderateVideo(ctx context.Context, filePath string) (bool, bool, error) {
	framesDirectory, err := os.MkdirTemp("", "lyrarma-video-*")
	if err != nil {
		return false, false, fmt.Errorf("create video frames directory: %w", err)
	}
	defer os.RemoveAll(framesDirectory)

	framePattern := filepath.Join(framesDirectory, "frame-%06d.jpg")
	output, exitCode, err := runCommand(
		ctx,
		s.settings.FrameCommand(),
		"-v", "error",
		"-i", filePath,
		"-map", "0:v:0",
		"-an", "-sn", "-dn",
		"-vf", "fps=1/5,scale=1280:-2:force_original_aspect_ratio=decrease",
		"-frames:v", "120",
		framePattern,
	)
	if err != nil {
		return false, false, fmt.Errorf("extract video frames: %w", err)
	}
	if exitCode != 0 {
		return false, false, fmt.Errorf("extract video frames: utility exited with code %d: %s", exitCode, commandMessage(output))
	}

	framesFound := false
	blocked := false
	walkErr := filepath.WalkDir(framesDirectory, func(framePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return errArchiveBlocked
		}
		framesFound = true
		frameBlocked, err := s.moderate(ctx, framePath, "image/jpeg")
		if err != nil {
			return err
		}
		if frameBlocked {
			blocked = true
			return fs.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return false, framesFound, fmt.Errorf("moderate video frames: %w", walkErr)
	}
	return blocked, framesFound, nil
}

func (s *Service) moderate(ctx context.Context, filePath, detectedMIME string) (bool, error) {
	if strings.TrimSpace(s.settings.ModerationCommand()) == "" {
		return false, nil
	}
	output, exitCode, err := runCommand(ctx, s.settings.ModerationCommand(), filePath, detectedMIME)
	if err != nil {
		return false, fmt.Errorf("run content moderation: %w", err)
	}
	switch exitCode {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("run content moderation: utility exited with code %d: %s", exitCode, commandMessage(output))
	}
}

func runCommand(ctx context.Context, commandLine string, arguments ...string) ([]byte, int, error) {
	parts := strings.Fields(commandLine)
	if len(parts) == 0 {
		return nil, -1, errors.New("utility command is empty")
	}
	commandArguments := append(append([]string(nil), parts[1:]...), arguments...)
	command := exec.CommandContext(ctx, parts[0], commandArguments...)
	var output limitedBuffer
	command.Stdout, command.Stderr = &output, &output
	err := command.Run()
	if output.truncated {
		return output.Bytes(), -1, errors.New("utility output exceeds the allowed limit")
	}
	if err == nil {
		return output.Bytes(), 0, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return output.Bytes(), exitError.ExitCode(), nil
	}
	return output.Bytes(), -1, err
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	truncated bool
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	remaining := maxCommandOutput - buffer.buffer.Len()
	if remaining > 0 {
		toWrite := len(data)
		if toWrite > remaining {
			toWrite = remaining
		}
		_, _ = buffer.buffer.Write(data[:toWrite])
	}
	if len(data) > remaining {
		buffer.truncated = true
	}
	return len(data), nil
}
func (buffer *limitedBuffer) Bytes() []byte { return buffer.buffer.Bytes() }

func commandMessage(output []byte) string {
	message := strings.TrimSpace(string(output))
	if len(message) > 512 {
		message = message[:512]
	}
	return message
}

func fileObjectKey(ownerID, fileID string) string {
	return filepath.ToSlash(filepath.Join("users", ownerID, "files", fileID))
}

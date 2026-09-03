package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/lyrarma/cloud-api/src/models"
)

var ErrFileNotFound = errors.New("file not found")

type rowScanner interface {
	Scan(dest ...any) error
}

func scanFile(row rowScanner) (models.FileEntry, error) {
	var entry models.FileEntry
	err := row.Scan(
		&entry.ID,
		&entry.OwnerID,
		&entry.FolderID,
		&entry.OriginalName,
		&entry.Size,
		&entry.ContentType,
		&entry.IsPublic,
		&entry.UploadedAt,
	)
	return entry, err
}

// CreateFile stores file metadata in PostgreSQL.
func (s *SQLStore) CreateFile(ctx context.Context, entry models.FileEntry) error {
	entry.FolderID = models.NormalizeFolderID(entry.FolderID)
	if entry.FolderID != "root" {
		if _, err := s.GetFolder(ctx, entry.OwnerID, entry.FolderID); err != nil {
			return err
		}
	}

	_, err := s.db.Exec(ctx, insertFileSQL,
		entry.ID,
		entry.OwnerID,
		entry.FolderID,
		entry.OriginalName,
		entry.Size,
		entry.ContentType,
		entry.IsPublic,
		entry.UploadedAt,
	)
	if err != nil {
		return fmt.Errorf("save file metadata: %w", err)
	}
	return nil
}

// GetFile returns metadata for a file owned by a user.
func (s *SQLStore) GetFile(ctx context.Context, userID, fileID string) (models.FileEntry, error) {
	entry, err := scanFile(s.db.QueryRow(ctx, selectOwnedFileSQL, fileID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return models.FileEntry{}, ErrFileNotFound
	}
	if err != nil {
		return models.FileEntry{}, fmt.Errorf("get file: %w", err)
	}
	return entry, nil
}

// GetPublicFile returns metadata for a public file.
func (s *SQLStore) GetPublicFile(ctx context.Context, fileID string) (models.FileEntry, error) {
	entry, err := scanFile(s.db.QueryRow(ctx, selectPublicFileSQL, fileID))
	if errors.Is(err, pgx.ErrNoRows) {
		return models.FileEntry{}, ErrFileNotFound
	}
	if err != nil {
		return models.FileEntry{}, fmt.Errorf("get public file: %w", err)
	}
	return entry, nil
}

// ListFiles returns user files, optionally filtering them by folder.
func (s *SQLStore) ListFiles(ctx context.Context, userID, folderID string) ([]models.FileEntry, error) {
	folderID = strings.TrimSpace(folderID)
	if folderID != "" {
		folderID = models.NormalizeFolderID(folderID)
	}

	rows, err := s.db.Query(ctx, listOwnedFilesSQL, userID, folderID)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	defer rows.Close()

	entries := make([]models.FileEntry, 0)
	for rows.Next() {
		entry, scanErr := scanFile(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("read file metadata: %w", scanErr)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read file list: %w", err)
	}
	return entries, nil
}

// DeleteFile removes file metadata and returns the deleted record.
func (s *SQLStore) DeleteFile(ctx context.Context, userID, fileID string) (models.FileEntry, error) {
	entry, err := s.GetFile(ctx, userID, fileID)
	if err != nil {
		return models.FileEntry{}, err
	}

	result, err := s.db.Exec(ctx, deleteOwnedFileSQL, fileID, userID)
	if err != nil {
		return models.FileEntry{}, fmt.Errorf("delete file metadata: %w", err)
	}
	if result.RowsAffected() == 0 {
		return models.FileEntry{}, ErrFileNotFound
	}
	return entry, nil
}

// UpdateFilePermission changes whether a file is public.
func (s *SQLStore) UpdateFilePermission(ctx context.Context, userID, fileID string, isPublic bool) error {
	result, err := s.db.Exec(ctx, updateFilePermissionSQL, fileID, userID, isPublic)
	if err != nil {
		return fmt.Errorf("change file permission: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrFileNotFound
	}
	return nil
}

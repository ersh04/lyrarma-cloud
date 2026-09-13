package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/lyrarma/cloud-api/src/models"
)

var (
	ErrStorageQuotaExceeded      = errors.New("storage quota exceeded")
	ErrUploadReservationNotFound = errors.New("upload reservation not found")
)

const (
	lockUserForUploadSQL = `
		SELECT id FROM users WHERE id = $1 FOR UPDATE
	`
	selectStorageUsageSQL = `
		SELECT
			COALESCE((SELECT SUM(size) FROM files WHERE owner_id = $1), 0)::BIGINT,
			COALESCE((SELECT SUM(size) FROM upload_reservations WHERE owner_id = $1), 0)::BIGINT
	`
	insertUploadReservationSQL = `
		INSERT INTO upload_reservations (id, owner_id, size, created_at)
		VALUES ($1, $2, $3, $4)
	`
	selectUploadReservationSQL = `
		SELECT size
		FROM upload_reservations
		WHERE id = $1 AND owner_id = $2
		FOR UPDATE
	`
	deleteUploadReservationSQL = `
		DELETE FROM upload_reservations
		WHERE id = $1 AND owner_id = $2
	`
)

// ReserveUpload atomically reserves space in a user's quota.
func (s *SQLStore) ReserveUpload(
	ctx context.Context,
	userID string,
	reservationID string,
	size int64,
	maxStorageBytes int64,
) error {
	if size < 0 || maxStorageBytes <= 0 || size > maxStorageBytes {
		return ErrStorageQuotaExceeded
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin quota reservation: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var lockedUserID string
	if err := tx.QueryRow(ctx, lockUserForUploadSQL, userID).Scan(&lockedUserID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		return fmt.Errorf("lock user quota: %w", err)
	}

	var usedBytes int64
	var reservedBytes int64
	if err := tx.QueryRow(ctx, selectStorageUsageSQL, userID).Scan(&usedBytes, &reservedBytes); err != nil {
		return fmt.Errorf("read storage usage: %w", err)
	}
	if usedBytes < 0 || reservedBytes < 0 {
		return fmt.Errorf("storage usage contains a negative size")
	}

	remainingAfterFile := maxStorageBytes - size
	if usedBytes > remainingAfterFile || reservedBytes > remainingAfterFile-usedBytes {
		return ErrStorageQuotaExceeded
	}

	if _, err := tx.Exec(
		ctx,
		insertUploadReservationSQL,
		reservationID,
		userID,
		size,
		time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("create quota reservation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit quota reservation: %w", err)
	}
	return nil
}

// ReleaseUploadReservation releases an existing quota reservation.
func (s *SQLStore) ReleaseUploadReservation(ctx context.Context, userID, reservationID string) error {
	if _, err := s.db.Exec(ctx, deleteUploadReservationSQL, reservationID, userID); err != nil {
		return fmt.Errorf("release quota reservation: %w", err)
	}
	return nil
}

// CommitReservedFile atomically stores file metadata and consumes its reservation.
func (s *SQLStore) CommitReservedFile(
	ctx context.Context,
	entry models.FileEntry,
	reservationID string,
) error {
	entry.FolderID = models.NormalizeFolderID(entry.FolderID)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin file metadata commit: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var reservedSize int64
	err = tx.QueryRow(ctx, selectUploadReservationSQL, reservationID, entry.OwnerID).Scan(&reservedSize)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUploadReservationNotFound
	}
	if err != nil {
		return fmt.Errorf("read quota reservation: %w", err)
	}
	if reservedSize != entry.Size {
		return fmt.Errorf("file size does not match quota reservation")
	}

	if entry.FolderID != "root" {
		var folderID string
		err = tx.QueryRow(
			ctx,
			"SELECT id FROM folders WHERE id = $1 AND owner_id = $2",
			entry.FolderID,
			entry.OwnerID,
		).Scan(&folderID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrFolderNotFound
		}
		if err != nil {
			return fmt.Errorf("validate folder before file commit: %w", err)
		}
	}

	_, err = tx.Exec(
		ctx,
		insertFileSQL,
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
		return fmt.Errorf("store file metadata: %w", err)
	}
	if _, err := tx.Exec(ctx, deleteUploadReservationSQL, reservationID, entry.OwnerID); err != nil {
		return fmt.Errorf("consume quota reservation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit file metadata: %w", err)
	}
	return nil
}

// StorageUsage returns a user's used and reserved storage.
func (s *SQLStore) StorageUsage(ctx context.Context, userID string) (int64, int64, error) {
	var usedBytes int64
	var reservedBytes int64
	if err := s.db.QueryRow(ctx, selectStorageUsageSQL, userID).Scan(&usedBytes, &reservedBytes); err != nil {
		return 0, 0, fmt.Errorf("read storage usage: %w", err)
	}
	return usedBytes, reservedBytes, nil
}

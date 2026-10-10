package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	createFilesTableSQL = `
		CREATE TABLE IF NOT EXISTS files (
			id            TEXT PRIMARY KEY,
			owner_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			folder_id     TEXT NOT NULL DEFAULT 'root',
			original_name TEXT NOT NULL,
			size          BIGINT NOT NULL
				CONSTRAINT files_size_nonnegative CHECK (size >= 0),
			content_type  TEXT NOT NULL,
			is_public     BOOLEAN NOT NULL DEFAULT FALSE,
			status        TEXT NOT NULL DEFAULT 'uploading'
				CONSTRAINT files_status_valid CHECK (status IN ('uploading', 'scanning', 'approved', 'blocked', 'scan_failed')),
			detected_content_type TEXT NOT NULL DEFAULT '',
			sha256         TEXT NOT NULL DEFAULT '',
			status_reason  TEXT NOT NULL DEFAULT '',
			scan_started_at TIMESTAMPTZ,
			uploaded_at   TIMESTAMPTZ NOT NULL
		)
	`

	createFoldersTableSQL = `
		CREATE TABLE IF NOT EXISTS folders (
			id         TEXT PRIMARY KEY,
			owner_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			parent_id  TEXT NOT NULL DEFAULT 'root',
			name       TEXT NOT NULL,
			is_public  BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMPTZ NOT NULL
		)
	`

	addFilesSizeConstraintSQL = `
		DO $$
		BEGIN
			ALTER TABLE files
			ADD CONSTRAINT files_size_nonnegative CHECK (size >= 0);
		EXCEPTION
			WHEN duplicate_object THEN NULL;
		END;
		$$
	`

	addFilesStatusColumnSQL = `
		DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = current_schema()
					AND table_name = 'files'
					AND column_name = 'status'
			) THEN
				ALTER TABLE files
				ADD COLUMN status TEXT NOT NULL DEFAULT 'approved';
			END IF;

			ALTER TABLE files ALTER COLUMN status SET DEFAULT 'uploading';
		END;
		$$
	`

	addFilesStatusConstraintSQL = `
		DO $$
		BEGIN
			ALTER TABLE files
			ADD CONSTRAINT files_status_valid
			CHECK (status IN ('uploading', 'scanning', 'approved', 'blocked', 'scan_failed'));
		EXCEPTION
			WHEN duplicate_object THEN NULL;
		END;
		$$
	`

	addFileScanMetadataSQL = `
		ALTER TABLE files
			ADD COLUMN IF NOT EXISTS detected_content_type TEXT NOT NULL DEFAULT '',
			ADD COLUMN IF NOT EXISTS sha256 TEXT NOT NULL DEFAULT '',
			ADD COLUMN IF NOT EXISTS status_reason TEXT NOT NULL DEFAULT '',
			ADD COLUMN IF NOT EXISTS scan_started_at TIMESTAMPTZ
	`

	createFilesOwnerIndexSQL     = `CREATE INDEX IF NOT EXISTS files_owner_folder_idx ON files (owner_id, folder_id)`
	createFoldersOwnerIndexSQL   = `CREATE INDEX IF NOT EXISTS folders_owner_parent_idx ON folders (owner_id, parent_id)`
	createFilesScanQueueIndexSQL = `CREATE INDEX IF NOT EXISTS files_scan_queue_idx ON files (status, uploaded_at, scan_started_at)`

	insertFileSQL = `
		INSERT INTO files (id, owner_id, folder_id, original_name, size, content_type, is_public, status, uploaded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	selectOwnedFileSQL = `
		SELECT id, owner_id, folder_id, original_name, size, content_type, is_public, status, detected_content_type, sha256, status_reason, uploaded_at
		FROM files WHERE id = $1 AND owner_id = $2
	`
	selectPublicFileSQL = `
		SELECT id, owner_id, folder_id, original_name, size, content_type, is_public, status, detected_content_type, sha256, status_reason, uploaded_at
		FROM files WHERE id = $1 AND is_public = TRUE AND status = 'approved'
	`
	listOwnedFilesSQL = `
		SELECT id, owner_id, folder_id, original_name, size, content_type, is_public, status, detected_content_type, sha256, status_reason, uploaded_at
		FROM files
		WHERE owner_id = $1 AND ($2 = '' OR folder_id = $2)
		ORDER BY uploaded_at DESC
	`
	deleteOwnedFileSQL      = `DELETE FROM files WHERE id = $1 AND owner_id = $2`
	updateFilePermissionSQL = `UPDATE files SET is_public = $3 WHERE id = $1 AND owner_id = $2`
	claimFileForScanSQL     = `
		WITH candidate AS (
			SELECT id FROM files
			WHERE status = 'uploading'
				OR (status = 'scanning' AND (scan_started_at IS NULL OR scan_started_at < $1))
			ORDER BY CASE WHEN status = 'uploading' THEN 0 ELSE 1 END, uploaded_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE files
		SET status = 'scanning', status_reason = '', scan_started_at = NOW()
		FROM candidate
		WHERE files.id = candidate.id
		RETURNING files.id, files.owner_id, files.folder_id, files.original_name, files.size, files.content_type, files.is_public, files.status, files.detected_content_type, files.sha256, files.status_reason, files.uploaded_at, files.scan_started_at
	`
	completeFileScanSQL = `
		UPDATE files
		SET status = $2,
			content_type = COALESCE(NULLIF($3, ''), content_type),
			detected_content_type = $3,
			sha256 = $4,
			status_reason = $5,
			scan_started_at = NULL
		WHERE id = $1 AND status = 'scanning' AND scan_started_at = $6
	`
	selectFileExistsSQL     = `SELECT EXISTS (SELECT 1 FROM files WHERE id = $1)`
	deleteFilesInFoldersSQL = `DELETE FROM files WHERE owner_id = $1 AND folder_id = ANY($2)`
	selectFilesInFoldersSQL = `SELECT id, owner_id, folder_id, original_name, size, content_type, is_public, status, detected_content_type, sha256, status_reason, uploaded_at FROM files WHERE owner_id = $1 AND folder_id = ANY($2)`

	insertFolderSQL = `
		INSERT INTO folders (id, owner_id, parent_id, name, is_public, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	selectOwnedFolderSQL = `
		SELECT id, owner_id, parent_id, name, is_public, created_at
		FROM folders WHERE id = $1 AND owner_id = $2
	`
	selectPublicFolderSQL = `
		SELECT id, owner_id, parent_id, name, is_public, created_at
		FROM folders WHERE id = $1 AND is_public = TRUE
	`
	listOwnedFoldersSQL = `
		SELECT id, owner_id, parent_id, name, is_public, created_at
		FROM folders WHERE owner_id = $1 ORDER BY created_at
	`
	selectFolderTreeSQL = `
		WITH RECURSIVE folder_tree AS (
			SELECT id, owner_id, parent_id, name, is_public, created_at
			FROM folders WHERE id = $1 AND owner_id = $2
			UNION ALL
			SELECT child.id, child.owner_id, child.parent_id, child.name, child.is_public, child.created_at
			FROM folders child
			JOIN folder_tree parent ON child.parent_id = parent.id
			WHERE child.owner_id = $2
		)
		SELECT id, owner_id, parent_id, name, is_public, created_at FROM folder_tree
	`
	deleteFolderTreeSQL       = `DELETE FROM folders WHERE owner_id = $1 AND id = ANY($2)`
	updateFolderPermissionSQL = `UPDATE folders SET is_public = $3 WHERE id = $1 AND owner_id = $2`

	createUsersTableSQL = `
		CREATE TABLE IF NOT EXISTS users (
			id         TEXT PRIMARY KEY,
			created_at TIMESTAMPTZ NOT NULL
		)
	`
	makeLegacyIdentityColumnsNullableSQL = `
		DO $$
		BEGIN
			IF EXISTS (
				SELECT 1
				FROM information_schema.columns
				WHERE table_schema = current_schema()
					AND table_name = 'users'
					AND column_name = 'username'
			) THEN
				ALTER TABLE users ALTER COLUMN username DROP NOT NULL;
			END IF;
			IF EXISTS (
				SELECT 1
				FROM information_schema.columns
				WHERE table_schema = current_schema()
					AND table_name = 'users'
					AND column_name = 'password_hash'
			) THEN
				ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;
			END IF;
		END;
		$$
	`

	createUploadReservationsTableSQL = `
		CREATE TABLE IF NOT EXISTS upload_reservations (
			id         TEXT PRIMARY KEY,
			owner_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			size       BIGINT NOT NULL CHECK (size >= 0),
			created_at TIMESTAMPTZ NOT NULL
		)
	`
	createUploadReservationsOwnerIndexSQL = `
		CREATE INDEX IF NOT EXISTS upload_reservations_owner_idx
		ON upload_reservations (owner_id)
	`
	deleteExpiredUploadReservationsSQL = `
		DELETE FROM upload_reservations
		WHERE created_at < NOW() - INTERVAL '24 hours'
	`

	insertUserProfileSQL = `INSERT INTO users (id, created_at) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING`
	selectUserProfileSQL = `SELECT id, created_at FROM users WHERE id = $1`
)

// SQLStore contains a PostgreSQL connection and a data directory
type SQLStore struct {
	db      *pgxpool.Pool
	dataDir string
}

func New(dataDir string, databaseURL string) (*SQLStore, error) {
	db, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return nil, err
	}

	store := &SQLStore{db: db, dataDir: dataDir}
	if err := store.init(); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

func (s *SQLStore) Close() {
	if s != nil && s.db != nil {
		s.db.Close()
	}
}

func (s *SQLStore) init() error {
	ctx := context.Background()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin database migration: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	_, err = tx.Exec(ctx, createUsersTableSQL)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, makeLegacyIdentityColumnsNullableSQL)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, createFilesTableSQL)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, addFilesSizeConstraintSQL)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, addFilesStatusColumnSQL)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, addFilesStatusConstraintSQL)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, addFileScanMetadataSQL)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, createFilesScanQueueIndexSQL)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, createFoldersTableSQL)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, createUploadReservationsTableSQL)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, createUploadReservationsOwnerIndexSQL)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, deleteExpiredUploadReservationsSQL)
	if err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit database migration: %w", err)
	}
	return nil
}

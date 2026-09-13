package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
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

	createFilesOwnerIndexSQL   = `CREATE INDEX IF NOT EXISTS files_owner_folder_idx ON files (owner_id, folder_id)`
	createFoldersOwnerIndexSQL = `CREATE INDEX IF NOT EXISTS folders_owner_parent_idx ON folders (owner_id, parent_id)`

	insertFileSQL = `
		INSERT INTO files (id, owner_id, folder_id, original_name, size, content_type, is_public, uploaded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	selectOwnedFileSQL = `
		SELECT id, owner_id, folder_id, original_name, size, content_type, is_public, uploaded_at
		FROM files WHERE id = $1 AND owner_id = $2
	`
	selectPublicFileSQL = `
		SELECT id, owner_id, folder_id, original_name, size, content_type, is_public, uploaded_at
		FROM files WHERE id = $1 AND is_public = TRUE
	`
	listOwnedFilesSQL = `
		SELECT id, owner_id, folder_id, original_name, size, content_type, is_public, uploaded_at
		FROM files
		WHERE owner_id = $1 AND ($2 = '' OR folder_id = $2)
		ORDER BY uploaded_at DESC
	`
	deleteOwnedFileSQL      = `DELETE FROM files WHERE id = $1 AND owner_id = $2`
	updateFilePermissionSQL = `UPDATE files SET is_public = $3 WHERE id = $1 AND owner_id = $2`
	deleteFilesInFoldersSQL = `DELETE FROM files WHERE owner_id = $1 AND folder_id = ANY($2)`
	selectFilesInFoldersSQL = `SELECT id, owner_id, folder_id, original_name, size, content_type, is_public, uploaded_at FROM files WHERE owner_id = $1 AND folder_id = ANY($2)`

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
			id            TEXT PRIMARY KEY,
			username      TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			plan          TEXT NOT NULL
				CONSTRAINT users_plan_name_check CHECK (plan ~ '^[a-z][a-z0-9_-]{0,31}$'),
			created_at    TIMESTAMPTZ NOT NULL
		)
	`

	addUserPlanColumnSQL = `
		ALTER TABLE users
		ADD COLUMN IF NOT EXISTS plan TEXT
	`
	dropLegacyUserPlanConstraintSQL = `
		ALTER TABLE users
		DROP CONSTRAINT IF EXISTS users_plan_check
	`
	backfillUserPlanSQL = `
		UPDATE users
		SET plan = $1
		WHERE plan IS NULL OR BTRIM(plan) = ''
	`
	dropUserPlanDefaultSQL = `
		ALTER TABLE users
		ALTER COLUMN plan DROP DEFAULT
	`
	setUserPlanNotNullSQL = `
		ALTER TABLE users
		ALTER COLUMN plan SET NOT NULL
	`
	addUserPlanNameConstraintSQL = `
		DO $$
		BEGIN
			ALTER TABLE users
			ADD CONSTRAINT users_plan_name_check
			CHECK (plan ~ '^[a-z][a-z0-9_-]{0,31}$');
		EXCEPTION
			WHEN duplicate_object THEN NULL;
		END;
		$$
	`
	selectUnknownUserPlanSQL = `
		SELECT plan
		FROM users
		WHERE NOT (plan = ANY($1::text[]))
		LIMIT 1
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

	insertUserSQL           = `INSERT INTO users (id, username, password_hash, plan, created_at) VALUES ($1, $2, $3, $4, $5)`
	selectUserByUsernameSQL = `SELECT id, username, password_hash, created_at FROM users WHERE username = $1`
	selectUserByIDSQL       = `SELECT id, username, password_hash, created_at FROM users WHERE id = $1`
	selectUserPlanSQL       = `SELECT plan FROM users WHERE id = $1`
)

// SQLStore contains a PostgreSQL connection and a data directory.
type SQLStore struct {
	db          *pgxpool.Pool
	dataDir     string
	defaultPlan string
}

func New(
	dataDir string,
	databaseURL string,
	defaultPlan string,
	configuredPlans []string,
) (*SQLStore, error) {
	db, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return nil, err
	}

	store := &SQLStore{db: db, dataDir: dataDir, defaultPlan: defaultPlan}
	if err := store.init(defaultPlan, configuredPlans); err != nil {
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

func (s *SQLStore) init(defaultPlan string, configuredPlans []string) error {
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

	_, err = tx.Exec(ctx, addUserPlanColumnSQL)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, dropLegacyUserPlanConstraintSQL)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, backfillUserPlanSQL, defaultPlan)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, dropUserPlanDefaultSQL)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, setUserPlanNotNullSQL)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, addUserPlanNameConstraintSQL)
	if err != nil {
		return err
	}

	var unknownPlan string
	err = tx.QueryRow(ctx, selectUnknownUserPlanSQL, configuredPlans).Scan(&unknownPlan)
	if err == nil {
		return fmt.Errorf("database contains unconfigured service plan %q", unknownPlan)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("validate configured service plans: %w", err)
	}

	_, err = tx.Exec(ctx, createFilesTableSQL)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, addFilesSizeConstraintSQL)
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

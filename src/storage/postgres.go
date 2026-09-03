package storage

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	createFilesTableSQL = `
		CREATE TABLE IF NOT EXISTS files (
			id            TEXT PRIMARY KEY,
			owner_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			folder_id     TEXT NOT NULL DEFAULT 'root',
			original_name TEXT NOT NULL,
			size          BIGINT NOT NULL,
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
			created_at    TIMESTAMPTZ NOT NULL
		)
	`

	insertUserSQL           = `INSERT INTO users (id, username, password_hash, created_at) VALUES ($1, $2, $3, $4)`
	selectUserByUsernameSQL = `SELECT id, username, password_hash, created_at FROM users WHERE username = $1`
	selectUserByIDSQL       = `SELECT id, username, password_hash, created_at FROM users WHERE id = $1`
)

// SQLStore contains a connection to PostgreSQL and a data directory for file storage
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
	_, err := s.db.Exec(context.Background(), createUsersTableSQL)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(context.Background(), createFilesTableSQL)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(context.Background(), createFoldersTableSQL)
	if err != nil {
		return err
	}

	return nil
}

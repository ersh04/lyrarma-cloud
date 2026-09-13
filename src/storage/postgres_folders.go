package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lyrarma/cloud-api/src/models"
)

var (
	ErrFolderNotFound      = errors.New("folder not found")
	ErrFolderAlreadyExists = errors.New("folder already exists")
)

func scanFolder(row rowScanner) (models.FolderEntry, error) {
	var entry models.FolderEntry
	err := row.Scan(
		&entry.ID,
		&entry.OwnerID,
		&entry.ParentID,
		&entry.Name,
		&entry.IsPublic,
		&entry.CreatedAt,
	)
	return entry, err
}

// CreateFolder saves a folder in PostgreSQL
func (s *SQLStore) CreateFolder(ctx context.Context, entry models.FolderEntry) error {
	entry.ParentID = models.NormalizeFolderID(entry.ParentID)
	if entry.ParentID != "root" {
		if _, err := s.GetFolder(ctx, entry.OwnerID, entry.ParentID); err != nil {
			return err
		}
	}

	_, err := s.db.Exec(ctx, insertFolderSQL,
		entry.ID,
		entry.OwnerID,
		entry.ParentID,
		entry.Name,
		entry.IsPublic,
		entry.CreatedAt,
	)
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrFolderAlreadyExists
	}
	return fmt.Errorf("create folder: %w", err)
}

// GetFolder returns a folder by its ID and owner ID
func (s *SQLStore) GetFolder(ctx context.Context, userID, folderID string) (models.FolderEntry, error) {
	entry, err := scanFolder(s.db.QueryRow(ctx, selectOwnedFolderSQL, folderID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return models.FolderEntry{}, ErrFolderNotFound
	}
	if err != nil {
		return models.FolderEntry{}, fmt.Errorf("find folder: %w", err)
	}
	return entry, nil
}

// GetPublicFolder returns a public folder.
func (s *SQLStore) GetPublicFolder(ctx context.Context, folderID string) (models.FolderEntry, error) {
	entry, err := scanFolder(s.db.QueryRow(ctx, selectPublicFolderSQL, folderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return models.FolderEntry{}, ErrFolderNotFound
	}
	if err != nil {
		return models.FolderEntry{}, fmt.Errorf("find public folder: %w", err)
	}
	return entry, nil
}

// ListFolders returns all folders belonging to a user.
func (s *SQLStore) ListFolders(ctx context.Context, userID string) ([]models.FolderEntry, error) {
	rows, err := s.db.Query(ctx, listOwnedFoldersSQL, userID)
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	defer rows.Close()

	entries := make([]models.FolderEntry, 0)
	for rows.Next() {
		entry, scanErr := scanFolder(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("read folder: %w", scanErr)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read folder list: %w", err)
	}
	return entries, nil
}

// UpdateFolderPermission changes the access level of a folder (public/private) for a specific user
func (s *SQLStore) UpdateFolderPermission(
	ctx context.Context,
	userID, folderID string,
	isPublic bool,
) error {
	result, err := s.db.Exec(ctx, updateFolderPermissionSQL, folderID, userID, isPublic)
	if err != nil {
		return fmt.Errorf("change folder permission: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrFolderNotFound
	}
	return nil
}

// FolderTree returns a folder and all its descendants.
func (s *SQLStore) FolderTree(
	ctx context.Context,
	userID, folderID string,
) ([]models.FolderEntry, error) {
	rows, err := s.db.Query(ctx, selectFolderTreeSQL, folderID, userID)
	if err != nil {
		return nil, fmt.Errorf("get folder tree: %w", err)
	}
	defer rows.Close()

	entries := make([]models.FolderEntry, 0)
	for rows.Next() {
		entry, scanErr := scanFolder(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("read folder tree: %w", scanErr)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read folder tree: %w", err)
	}
	return entries, nil
}

// DeleteFolder removes a folder tree and returns metadata for the files it contained.
func (s *SQLStore) DeleteFolder(
	ctx context.Context,
	userID, folderID string,
) ([]models.FileEntry, error) {
	folders, err := s.FolderTree(ctx, userID, folderID)
	if err != nil {
		return nil, err
	}
	if len(folders) == 0 {
		return nil, ErrFolderNotFound
	}

	folderIDs := make([]string, 0, len(folders))
	for _, folder := range folders {
		folderIDs = append(folderIDs, folder.ID)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin delete folder: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	rows, err := tx.Query(ctx, selectFilesInFoldersSQL, userID, folderIDs)
	if err != nil {
		return nil, fmt.Errorf("get files in folder to delete: %w", err)
	}

	files := make([]models.FileEntry, 0)
	for rows.Next() {
		entry, scanErr := scanFile(rows)
		if scanErr != nil {
			rows.Close()
			return nil, fmt.Errorf("read file in folder to delete: %w", scanErr)
		}
		files = append(files, entry)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return nil, fmt.Errorf("read files in folder to delete: %w", rowsErr)
	}

	if _, err := tx.Exec(ctx, deleteFilesInFoldersSQL, userID, folderIDs); err != nil {
		return nil, fmt.Errorf("delete files in folder: %w", err)
	}
	if _, err := tx.Exec(ctx, deleteFolderTreeSQL, userID, folderIDs); err != nil {
		return nil, fmt.Errorf("delete folder tree: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit delete folder: %w", err)
	}
	return files, nil
}

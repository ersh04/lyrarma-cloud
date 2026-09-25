package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lyrarma/cloud-api/src/models"
)

const (
	createAuthUsersTableSQL = `
		CREATE TABLE IF NOT EXISTS users (
			id       TEXT,
			username TEXT NOT NULL,
			password TEXT NOT NULL,
			email    TEXT NOT NULL CONSTRAINT users_email_unique UNIQUE
		)
	`
	addAuthUserIDSQL    = `ALTER TABLE users ADD COLUMN IF NOT EXISTS id TEXT`
	selectAuthSchemaSQL = `
		SELECT COALESCE(string_agg(column_name, ',' ORDER BY column_name), '')
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'users'
	`
	selectAuthPrimaryKeySQL = `
		SELECT
			COALESCE(MAX(tc.constraint_name), ''),
			COALESCE(string_agg(kcu.column_name, ',' ORDER BY kcu.ordinal_position), '')
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
			ON kcu.constraint_schema = tc.constraint_schema
			AND kcu.constraint_name = tc.constraint_name
		WHERE tc.table_schema = current_schema()
			AND tc.table_name = 'users'
			AND tc.constraint_type = 'PRIMARY KEY'
	`
	createAuthUsernameIndexSQL = `CREATE UNIQUE INDEX IF NOT EXISTS users_username_unique ON users (username)`
	setAuthUserIDNotNullSQL    = `ALTER TABLE users ALTER COLUMN id SET NOT NULL`
	addAuthUsersPrimaryKeySQL  = `ALTER TABLE users ADD CONSTRAINT users_pkey PRIMARY KEY (id)`
	insertAuthUserSQL          = `INSERT INTO users (id, username, password, email) VALUES ($1, $2, $3, $4)`
	selectAuthUserSQL          = `SELECT id, username, password, email FROM users WHERE username = $1`
	deleteAuthUserSQL          = `DELETE FROM users WHERE id = $1 AND username = $2 AND email = $3`
	selectMissingUserIDsSQL    = `SELECT username FROM users WHERE id IS NULL OR BTRIM(id) = ''`
	updateAuthUserIDSQL        = `UPDATE users SET id = $1 WHERE username = $2`
	selectAuthUserLinksSQL     = `SELECT id, username FROM users`
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrUserAlreadyExists = errors.New("user already exists")
	ErrEmailAlreadyUsed  = errors.New("email already used")
)

// UserStore stores identifiers and data required to authenticate users.
type UserStore struct {
	db *pgxpool.Pool
}

// NewUserStore connects to lyrarma-services-users and prepares its schema.
func NewUserStore(databaseURL string) (*UserStore, error) {
	db, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return nil, err
	}

	store := &UserStore{db: db}
	if err := store.init(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *UserStore) init() error {
	ctx := context.Background()
	if _, err := s.db.Exec(ctx, createAuthUsersTableSQL); err != nil {
		return fmt.Errorf("create authentication users table: %w", err)
	}
	if _, err := s.db.Exec(ctx, addAuthUserIDSQL); err != nil {
		return fmt.Errorf("add authentication user ID: %w", err)
	}

	var columns string
	if err := s.db.QueryRow(ctx, selectAuthSchemaSQL).Scan(&columns); err != nil {
		return fmt.Errorf("read authentication users schema: %w", err)
	}
	if columns != "email,id,password,username" {
		return fmt.Errorf("authentication users table must contain only id, username, password and email columns; got %q", columns)
	}
	return nil
}

// Close closes the user database connection.
func (s *UserStore) Close() {
	if s != nil && s.db != nil {
		s.db.Close()
	}
}

// CreateUser stores a new user account.
func (s *UserStore) CreateUser(user models.User) error {
	_, err := s.db.Exec(context.Background(), insertAuthUserSQL, user.ID, user.Username, user.Password, user.Email)
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		if pgErr.ConstraintName == "users_email_unique" {
			return ErrEmailAlreadyUsed
		}
		return ErrUserAlreadyExists
	}
	return fmt.Errorf("create authentication user: %w", err)
}

// GetUserByUsername returns authentication data for a username.
func (s *UserStore) GetUserByUsername(username string) (models.User, error) {
	var user models.User
	err := s.db.QueryRow(context.Background(), selectAuthUserSQL, username).
		Scan(&user.ID, &user.Username, &user.Password, &user.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.User{}, ErrUserNotFound
	}
	if err != nil {
		return models.User{}, fmt.Errorf("find authentication user: %w", err)
	}
	return user, nil
}

// DeleteUser removes an account only when its ID, username, and email match.
func (s *UserStore) DeleteUser(user models.User) error {
	if _, err := s.db.Exec(context.Background(), deleteAuthUserSQL, user.ID, user.Username, user.Email); err != nil {
		return fmt.Errorf("delete authentication user: %w", err)
	}
	return nil
}

func (s *UserStore) assignMissingIDs(ctx context.Context) error {
	rows, err := s.db.Query(ctx, selectMissingUserIDsSQL)
	if err != nil {
		return fmt.Errorf("read users without identifiers: %w", err)
	}

	var usernames []string
	for rows.Next() {
		var username string
		if err := rows.Scan(&username); err != nil {
			rows.Close()
			return fmt.Errorf("scan user without identifier: %w", err)
		}
		usernames = append(usernames, username)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate users without identifiers: %w", err)
	}
	rows.Close()

	for _, username := range usernames {
		id, err := randomUserID()
		if err != nil {
			return err
		}
		if _, err := s.db.Exec(ctx, updateAuthUserIDSQL, id, username); err != nil {
			return fmt.Errorf("assign ID to authentication user %q: %w", username, err)
		}
	}
	return nil
}

func (s *UserStore) finalizeSchema(ctx context.Context) error {
	var primaryKeyName, primaryKeyColumns string
	if err := s.db.QueryRow(ctx, selectAuthPrimaryKeySQL).Scan(&primaryKeyName, &primaryKeyColumns); err != nil {
		return fmt.Errorf("read authentication users primary key: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin authentication schema migration: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	statements := []string{
		createAuthUsernameIndexSQL,
		setAuthUserIDNotNullSQL,
	}
	if primaryKeyColumns != "id" {
		if primaryKeyName != "" {
			statements = append(
				statements,
				"ALTER TABLE users DROP CONSTRAINT "+pgx.Identifier{primaryKeyName}.Sanitize(),
			)
		}
		statements = append(statements, addAuthUsersPrimaryKeySQL)
	}

	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("finalize authentication users schema: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit authentication schema migration: %w", err)
	}
	return nil
}

func (s *UserStore) setUserID(ctx context.Context, username, id string) (bool, error) {
	result, err := s.db.Exec(ctx, updateAuthUserIDSQL, id, username)
	if err != nil {
		return false, fmt.Errorf("link authentication user %q to ID %q: %w", username, id, err)
	}
	return result.RowsAffected() > 0, nil
}

func (s *UserStore) userLinks(ctx context.Context) ([]userLink, error) {
	rows, err := s.db.Query(ctx, selectAuthUserLinksSQL)
	if err != nil {
		return nil, fmt.Errorf("read authentication user links: %w", err)
	}
	defer rows.Close()

	var links []userLink
	for rows.Next() {
		var link userLink
		if err := rows.Scan(&link.id, &link.username); err != nil {
			return nil, fmt.Errorf("scan authentication user link: %w", err)
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate authentication user links: %w", err)
	}
	return links, nil
}

func randomUserID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate user ID: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

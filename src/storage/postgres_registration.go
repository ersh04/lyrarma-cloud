package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lyrarma/cloud-api/src/models"
)

const (
	createPendingRegistrationsSQL = `
		CREATE TABLE IF NOT EXISTS registration_requests (
			confirmation_key_hash TEXT PRIMARY KEY,
			username TEXT NOT NULL CONSTRAINT registration_requests_username_unique UNIQUE,
			email TEXT NOT NULL CONSTRAINT registration_requests_email_unique UNIQUE,
			password_hash TEXT NOT NULL,
			plan TEXT[] NOT NULL CHECK (
				array_ndims(plan) = 1 AND cardinality(plan) > 0
				AND array_position(plan, NULL) IS NULL
			),
			created_at TIMESTAMPTZ NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > created_at)
		)
	`
	createPendingRegistrationsExpiryIndexSQL = `
		CREATE INDEX IF NOT EXISTS registration_requests_expires_at_idx
		ON registration_requests (expires_at)
	`
	deleteExpiredRegistrationsSQL = `DELETE FROM registration_requests WHERE expires_at <= CURRENT_TIMESTAMP`
	selectRegisteredIdentitySQL   = `
		SELECT EXISTS (SELECT 1 FROM users WHERE username = $1),
		       EXISTS (SELECT 1 FROM users WHERE email = $2)
	`
	insertPendingRegistrationSQL = `
		INSERT INTO registration_requests
			(confirmation_key_hash, username, email, password_hash, plan, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	consumePendingRegistrationSQL = `
		DELETE FROM registration_requests
		WHERE confirmation_key_hash = $1
		RETURNING username, email, password_hash, plan, expires_at > clock_timestamp()
	`
)

var (
	ErrRegistrationNotFound = errors.New("registration confirmation not found")
	ErrRegistrationExpired  = errors.New("registration confirmation expired")
)

// ConfirmRegistration atomically creates an account and consumes its pending request
func (s *UserStore) ConfirmRegistration(key string) (models.User, error) {
	return confirmPendingRegistration(context.Background(), s.db.Begin, key, s.validateAssignedPlans)
}

func confirmPendingRegistration(ctx context.Context, begin func(context.Context) (pgx.Tx, error), key string, validatePlans func([]string) error) (models.User, error) {
	if len(key) != 64 {
		return models.User{}, ErrRegistrationNotFound
	}
	decoded, err := hex.DecodeString(key)
	if err != nil || len(decoded) != 32 {
		return models.User{}, ErrRegistrationNotFound
	}
	digest := sha256.Sum256([]byte(key))
	tx, err := begin(ctx)
	if err != nil {
		return models.User{}, fmt.Errorf("begin registration confirmation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var user models.User
	var valid bool
	// DELETE locks the matching row; rollback restores it if account creation fails
	err = tx.QueryRow(ctx, consumePendingRegistrationSQL, hex.EncodeToString(digest[:])).
		Scan(&user.Username, &user.Email, &user.Password, &user.Plan, &valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.User{}, ErrRegistrationNotFound
	}
	if err != nil {
		return models.User{}, fmt.Errorf("consume pending registration: %w", err)
	}
	if !valid {
		if err := tx.Commit(ctx); err != nil {
			return models.User{}, fmt.Errorf("remove expired registration: %w", err)
		}
		return models.User{}, ErrRegistrationExpired
	}
	if err := validatePlans(user.Plan); err != nil {
		return models.User{}, err
	}
	user.ID, err = randomUserID()
	if err != nil {
		return models.User{}, err
	}
	if _, err := tx.Exec(ctx, insertAuthUserSQL, user.ID, user.Username, user.Password, user.Email, user.Plan); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			switch pgErr.ConstraintName {
			case "users_email_unique":
				return models.User{}, ErrEmailAlreadyUsed
			case "users_username_unique":
				return models.User{}, ErrUserAlreadyExists
			}
		}
		return models.User{}, fmt.Errorf("create confirmed user: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return models.User{}, fmt.Errorf("commit registration confirmation: %w", err)
	}
	return user, nil
}

func (s *UserStore) initPendingRegistrations(ctx context.Context) error {
	for _, statement := range []string{
		createPendingRegistrationsSQL, createPendingRegistrationsExpiryIndexSQL, deleteExpiredRegistrationsSQL,
	} {
		if _, err := s.db.Exec(ctx, statement); err != nil {
			return fmt.Errorf("prepare pending registrations: %w", err)
		}
	}
	return nil
}

// CreatePendingRegistration stores a request separately from users and cloud profiles
func (s *UserStore) CreatePendingRegistration(request models.PendingRegistration) error {
	if err := s.validateAssignedPlans(request.Plan); err != nil {
		return err
	}
	if request.ConfirmationKey == "" || request.CreatedAt.IsZero() || !request.ExpiresAt.After(request.CreatedAt) {
		return fmt.Errorf("invalid pending registration key or lifetime")
	}
	ctx := context.Background()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin pending registration: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := insertPendingRegistration(ctx, tx, request); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit pending registration: %w", err)
	}
	return nil
}

func insertPendingRegistration(ctx context.Context, tx pgx.Tx, request models.PendingRegistration) error {
	// Remove expired requests to release usernames and email addresses for registration
	if _, err := tx.Exec(ctx, deleteExpiredRegistrationsSQL); err != nil {
		return fmt.Errorf("delete expired registrations: %w", err)
	}
	var usernameExists, emailExists bool
	if err := tx.QueryRow(ctx, selectRegisteredIdentitySQL, request.Username, request.Email).
		Scan(&usernameExists, &emailExists); err != nil {
		return fmt.Errorf("check registered identity: %w", err)
	}
	if usernameExists {
		return ErrUserAlreadyExists
	}
	if emailExists {
		return ErrEmailAlreadyUsed
	}

	// The original key is only needed by the link sender and is not stored in the database
	digest := sha256.Sum256([]byte(request.ConfirmationKey))
	_, err := tx.Exec(ctx, insertPendingRegistrationSQL,
		hex.EncodeToString(digest[:]), request.Username, request.Email, request.PasswordHash,
		request.Plan, request.CreatedAt, request.ExpiresAt,
	)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "registration_requests_username_unique":
			return ErrUserAlreadyExists
		case "registration_requests_email_unique":
			return ErrEmailAlreadyUsed
		}
	}
	if err != nil {
		return fmt.Errorf("create pending registration: %w", err)
	}
	return nil
}

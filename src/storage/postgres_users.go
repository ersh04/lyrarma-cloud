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
	ErrUserNotFound      = errors.New("user not found")
	ErrUserAlreadyExists = errors.New("user already exists")
)

// CreateUser adds a user to PostgreSQL.
func (s *SQLStore) CreateUser(user models.User) error {
	_, err := s.db.Exec(context.Background(), insertUserSQL,
		user.ID, user.Username, user.PasswordHash, s.defaultPlan, user.CreatedAt)
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrUserAlreadyExists
	}
	return fmt.Errorf("create user: %w", err)
}

// GetUserByUsername returns a user by username.
func (s *SQLStore) GetUserByUsername(username string) (models.User, error) {
	var user models.User
	err := s.db.QueryRow(context.Background(), selectUserByUsernameSQL, username).
		Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.User{}, ErrUserNotFound
	}
	if err != nil {
		return models.User{}, fmt.Errorf("find user: %w", err)
	}
	return user, nil
}

// GetUserByID returns a user by identifier.
func (s *SQLStore) GetUserByID(id string) (models.User, error) {
	var user models.User
	err := s.db.QueryRow(context.Background(), selectUserByIDSQL, id).
		Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.User{}, ErrUserNotFound
	}
	if err != nil {
		return models.User{}, fmt.Errorf("find user by ID: %w", err)
	}
	return user, nil
}

// GetUserPlan returns the user's current service plan.
func (s *SQLStore) GetUserPlan(ctx context.Context, id string) (string, error) {
	var plan string
	err := s.db.QueryRow(ctx, selectUserPlanSQL, id).Scan(&plan)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUserNotFound
	}
	if err != nil {
		return "", fmt.Errorf("find user service plan: %w", err)
	}
	return plan, nil
}

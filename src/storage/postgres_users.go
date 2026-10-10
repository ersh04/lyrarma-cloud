package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lyrarma/cloud-api/src/models"
)

// EnsureUserProfile creates a cloud user profile or returns the existing one
func (s *SQLStore) EnsureUserProfile(profile models.UserProfile) (models.UserProfile, error) {
	_, err := s.db.Exec(
		context.Background(),
		insertUserProfileSQL,
		profile.ID,
		profile.CreatedAt,
	)
	if err != nil {
		return models.UserProfile{}, fmt.Errorf("create user profile: %w", err)
	}

	var stored models.UserProfile
	err = s.db.QueryRow(context.Background(), selectUserProfileSQL, profile.ID).
		Scan(&stored.ID, &stored.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.UserProfile{}, ErrUserNotFound
	}
	if err != nil {
		return models.UserProfile{}, fmt.Errorf("find user profile: %w", err)
	}
	return stored, nil
}

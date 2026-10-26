package models

import "time"

// PendingRegistration contains a temporary request that is not yet a user account
type PendingRegistration struct {
	Username        string    `json:"username"`
	Email           string    `json:"email"`
	PasswordHash    string    `json:"-"`
	Plan            []string  `json:"plan"`
	ConfirmationKey string    `json:"-"`
	CreatedAt       time.Time `json:"created_at"`
	ExpiresAt       time.Time `json:"expires_at"`
}

// RegistrationConfirmationResponse starts a session for the confirmed account
type RegistrationConfirmationResponse struct {
	LoginResponse
	Username string `json:"username"`
}

package managers

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/lyrarma/cloud-api/src/config"
	"github.com/lyrarma/cloud-api/src/httpresponse"
	"github.com/lyrarma/cloud-api/src/middleware"
	"github.com/lyrarma/cloud-api/src/models"
	"github.com/lyrarma/cloud-api/src/storage"
)

// AuthHandler handles user registration and sign-in.
type AuthHandler struct {
	store           *storage.SQLStore
	jwt             config.JWTConfig
	registrationKey string
	logger          *slog.Logger
}

// NewAuthHandler creates an authentication handler with the provided dependencies.
func NewAuthHandler(
	store *storage.SQLStore,
	jwtConfig config.JWTConfig,
	registrationKey string,
	logger *slog.Logger,
) *AuthHandler {
	return &AuthHandler{
		store:           store,
		jwt:             jwtConfig,
		registrationKey: registrationKey,
		logger:          logger,
	}
}

// Register creates a new user account.
func (h *AuthHandler) Register(c fiber.Ctx) {
	var request models.RegisterRequest
	if err := json.NewDecoder(c.Request().BodyStream()).Decode(&request); err != nil {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "invalid_body", "invalid JSON")
		return
	}

	if !equalRegistrationKey(request.BetaKey, h.registrationKey) {
		httpresponse.WriteError(c, fiber.StatusForbidden, "invalid_beta_key", "invalid registration key")
		return
	}

	if len(request.Username) < 3 {
		httpresponse.WriteError(
			c,
			fiber.StatusBadRequest,
			"validation_error",
			"username must be at least 3 characters long",
		)
		return
	}
	if len(request.Password) < 8 {
		httpresponse.WriteError(
			c,
			fiber.StatusBadRequest,
			"validation_error",
			"password must be at least 8 characters long",
		)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(request.Password), 12)
	if err != nil {
		h.logger.Error("bcrypt password hashing failed", "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	user := models.User{
		ID:           generateID(),
		Username:     request.Username,
		PasswordHash: string(hash),
		CreatedAt:    time.Now().UTC(),
	}
	if err := h.store.CreateUser(user); err != nil {
		if errors.Is(err, storage.ErrUserAlreadyExists) {
			httpresponse.WriteError(
				c,
				fiber.StatusConflict,
				"user_exists",
				"a user with this name already exists",
			)
			return
		}
		h.logger.Error("user creation failed", "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	h.logger.Info("new user registered", "username", user.Username, "id", user.ID)
	httpresponse.WriteJSON(c, fiber.StatusCreated, models.SuccessResponse{Message: "user registered"})
}

// Login validates credentials and issues a JWT.
func (h *AuthHandler) Login(c fiber.Ctx) {
	var request models.LoginRequest
	if err := json.NewDecoder(c.Request().BodyStream()).Decode(&request); err != nil {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "invalid_body", "invalid JSON")
		return
	}

	user, err := h.store.GetUserByUsername(request.Username)
	if err != nil {
		if errors.Is(err, storage.ErrUserNotFound) {
			httpresponse.WriteError(
				c,
				fiber.StatusUnauthorized,
				"invalid_credentials",
				"invalid username or password",
			)
			return
		}
		h.logger.Error("user lookup failed", "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	if err := bcrypt.CompareHashAndPassword(
		[]byte(user.PasswordHash),
		[]byte(request.Password),
	); err != nil {
		httpresponse.WriteError(
			c,
			fiber.StatusUnauthorized,
			"invalid_credentials",
			"invalid username or password",
		)
		return
	}

	now := time.Now().UTC()
	expiresAt := now.Add(h.jwt.TokenTTL())
	claims := middleware.Claims{
		UserID: user.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "lyrarma-cloud",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(h.jwt.Secret()))
	if err != nil {
		h.logger.Error("JWT signing failed", "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	h.logger.Info("user signed in", "username", user.Username)
	httpresponse.WriteJSON(c, fiber.StatusOK, models.LoginResponse{
		Token:     tokenString,
		ExpiresAt: expiresAt,
	})
}

func equalRegistrationKey(value, expected string) bool {
	if expected == "" || len(value) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(value), []byte(expected)) == 1
}

// generateID creates a cryptographically random hexadecimal identifier.
func generateID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic("failed to obtain random bytes: " + err.Error())
	}
	return hex.EncodeToString(bytes)
}

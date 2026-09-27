package managers

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/mail"
	"strings"
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
	cloudStore      *storage.SQLStore
	userStore       *storage.UserStore
	jwt             config.JWTConfig
	registrationKey string
	defaultPlan     string
	logger          *slog.Logger
}

// NewAuthHandler creates an authentication handler with the provided dependencies.
func NewAuthHandler(
	cloudStore *storage.SQLStore,
	userStore *storage.UserStore,
	jwtConfig config.JWTConfig,
	registrationKey string,
	defaultPlan string,
	logger *slog.Logger,
) *AuthHandler {
	return &AuthHandler{
		cloudStore:      cloudStore,
		userStore:       userStore,
		jwt:             jwtConfig,
		registrationKey: registrationKey,
		defaultPlan:     defaultPlan,
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

	request.Username = strings.TrimSpace(request.Username)
	request.Email = strings.ToLower(strings.TrimSpace(request.Email))
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
	if !validEmail(request.Email) {
		httpresponse.WriteError(
			c,
			fiber.StatusBadRequest,
			"validation_error",
			"a valid email address is required",
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
		ID:       generateID(),
		Username: request.Username,
		Password: string(hash),
		Email:    request.Email,
		Plan:     []string{h.defaultPlan},
	}
	if err := h.userStore.CreateUser(user); err != nil {
		if errors.Is(err, storage.ErrUserAlreadyExists) {
			httpresponse.WriteError(
				c,
				fiber.StatusConflict,
				"user_exists",
				"a user with this name already exists",
			)
			return
		}
		if errors.Is(err, storage.ErrEmailAlreadyUsed) {
			httpresponse.WriteError(
				c,
				fiber.StatusConflict,
				"email_exists",
				"a user with this email already exists",
			)
			return
		}
		h.logger.Error("user creation failed", "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	profile, err := h.cloudStore.EnsureUserProfile(models.UserProfile{
		ID:        user.ID,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		if deleteErr := h.userStore.DeleteUser(user); deleteErr != nil {
			h.logger.Error("failed to roll back authentication user", "username", user.Username, "err", deleteErr)
		}
		h.logger.Error("cloud user profile creation failed", "username", user.Username, "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	h.logger.Info("new user registered", "username", user.Username, "id", profile.ID)
	httpresponse.WriteJSON(c, fiber.StatusCreated, models.SuccessResponse{Message: "user registered"})
}

// Login validates credentials and issues a JWT.
func (h *AuthHandler) Login(c fiber.Ctx) {
	var request models.LoginRequest
	if err := json.NewDecoder(c.Request().BodyStream()).Decode(&request); err != nil {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "invalid_body", "invalid JSON")
		return
	}

	request.Username = strings.TrimSpace(request.Username)
	user, err := h.userStore.GetUserByUsername(request.Username)
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
		[]byte(user.Password),
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
	profile, err := h.cloudStore.EnsureUserProfile(models.UserProfile{
		ID:        user.ID,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		h.logger.Error("cloud user profile lookup failed", "username", user.Username, "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	now := time.Now().UTC()
	expiresAt := now.Add(h.jwt.TokenTTL())
	claims := middleware.Claims{
		UserID: profile.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "lyrarma-services-users",
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

func validEmail(value string) bool {
	if value == "" || len(value) > 254 {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && strings.Contains(value, "@")
}

// generateID creates a cryptographically random hexadecimal identifier.
func generateID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic("failed to obtain random bytes: " + err.Error())
	}
	return hex.EncodeToString(bytes)
}

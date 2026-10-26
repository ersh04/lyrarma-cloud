package handlers

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
	"github.com/gofiber/fiber/v3/client"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/lyrarma/cloud-api/src/config"
	"github.com/lyrarma/cloud-api/src/httpresponse"
	"github.com/lyrarma/cloud-api/src/middleware"
	"github.com/lyrarma/cloud-api/src/models"
	"github.com/lyrarma/cloud-api/src/storage"
)

const (
	registrationRequestTTL = 24 * time.Hour
)

type authUserStore interface {
	CreatePendingRegistration(models.PendingRegistration) error
	ConfirmRegistration(string) (models.User, error)
	GetUserByUsername(string) (models.User, error)
}

type authCloudStore interface {
	EnsureUserProfile(models.UserProfile) (models.UserProfile, error)
}

// AuthHandler handles user registration and sign-in
type AuthHandler struct {
	cloudStore                  authCloudStore
	userStore                   authUserStore
	jwt                         config.JWTConfig
	registrationKey             string
	defaultPlan                 string
	postControllerURL           string
	postControllerToken         string
	registrationConfirmationURL string
	logger                      *slog.Logger
}

// NewAuthHandler creates an authentication handler with the provided dependencies
func NewAuthHandler(
	cloudStore *storage.SQLStore,
	userStore *storage.UserStore,
	jwtConfig config.JWTConfig,
	registrationKey string,
	defaultPlan string,
	postControllerURL string,
	postControllerToken string,
	registrationConfirmationURL string,
	logger *slog.Logger,
) *AuthHandler {
	return &AuthHandler{
		cloudStore:                  cloudStore,
		userStore:                   userStore,
		jwt:                         jwtConfig,
		registrationKey:             registrationKey,
		defaultPlan:                 defaultPlan,
		postControllerURL:           postControllerURL,
		postControllerToken:         postControllerToken,
		registrationConfirmationURL: registrationConfirmationURL,
		logger:                      logger,
	}
}

// Register creates a pending registration for a new user, requiring confirmation via a registration key
func (h *AuthHandler) Register(c fiber.Ctx) {
	var request models.RegisterRequest
	if err := json.NewDecoder(c.Request().BodyStream()).Decode(&request); err != nil {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "invalid_body", "invalid JSON")
		return
	}

	if h.registrationKey != "" && !equalRegistrationKey(request.BetaKey, h.registrationKey) {
		httpresponse.WriteError(c, fiber.StatusForbidden, "invalid_beta_key", "invalid registration key")
		return
	}

	if !request.PrivacyPolicyAccepted || !request.IDPolicyAccepted {
		httpresponse.WriteError(c, fiber.StatusBadRequest, "policies_not_accepted", "both policies must be accepted")
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

	key, err := generateRegistrationKey()
	if err != nil {
		h.logger.Error("registration key generation failed", "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	now := time.Now().UTC()
	pending := models.PendingRegistration{
		Username: request.Username, Email: request.Email, PasswordHash: string(hash),
		Plan: []string{h.defaultPlan}, ConfirmationKey: key,
		CreatedAt: now, ExpiresAt: now.Add(registrationRequestTTL),
	}
	if err := h.userStore.CreatePendingRegistration(pending); err != nil {
		if errors.Is(err, storage.ErrUserAlreadyExists) {
			httpresponse.WriteError(
				c,
				fiber.StatusConflict,
				"user_exists",
				"a user or pending registration with this name already exists",
			)
			return
		}
		if errors.Is(err, storage.ErrEmailAlreadyUsed) {
			httpresponse.WriteError(
				c,
				fiber.StatusConflict,
				"email_exists",
				"a user or pending registration with this email already exists",
			)
			return
		}
		h.logger.Error("pending registration creation failed", "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	h.SendRegistrationKey(c, pending.Email, pending.ConfirmationKey)
	h.logger.Info("registration request created", "username", pending.Username)
	httpresponse.WriteJSON(c, fiber.StatusCreated, models.SuccessResponse{Message: "registration pending confirmation"})
}

func (h *AuthHandler) SendRegistrationKey(c fiber.Ctx, email string, confirmationKey string) {
	payload, err := registrationEmailPayload(email, confirmationKey, h.registrationConfirmationURL, c.Get(fiber.HeaderAcceptLanguage))
	if err != nil {
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "Unable to prepare registration email")
		return
	}
	httpRemoteClient := client.New().
		SetBaseURL(h.postControllerURL).
		SetTimeout(15 * time.Second)

	var targetResp string = ""

	// Send the email with a localized subject and body
	resp, err := httpRemoteClient.R().
		SetHeader("authorization", "Bearer "+h.postControllerToken).
		SetJSON(payload).
		Post("/send-email")

	if err != nil {
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "Unable to send request to external server: "+err.Error())
		return
	}
	defer resp.Close()

	if resp.StatusCode() != fiber.StatusOK {
		httpresponse.WriteError(c, fiber.StatusBadGateway, "bad_gateway", "Dedicated server returned an error: "+resp.Status())
		return
	}

	httpresponse.WriteJSON(
		c,
		fiber.StatusOK,
		fiber.Map{
			"message": "Key sent successfully to " + email,
			"data":    targetResp,
		},
	)
}

// ConfirmRegistration handles the confirmation of a pending registration using a registration key
func (h *AuthHandler) ConfirmRegistration(c fiber.Ctx) {
	c.Set(fiber.HeaderCacheControl, "no-store")
	c.Set("Referrer-Policy", "no-referrer")
	user, err := h.userStore.ConfirmRegistration(c.Query("key"))
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrRegistrationNotFound):
			httpresponse.WriteError(c, fiber.StatusNotFound, "confirmation_invalid", "confirmation key not found or already used")
		case errors.Is(err, storage.ErrRegistrationExpired):
			httpresponse.WriteError(c, fiber.StatusNotFound, "confirmation_expired", "confirmation key has expired")
		case errors.Is(err, storage.ErrUserAlreadyExists), errors.Is(err, storage.ErrEmailAlreadyUsed):
			httpresponse.WriteError(c, fiber.StatusConflict, "confirmation_conflict", "an account with this username or email already exists")
		default:
			h.logger.Error("registration confirmation failed", "err", err)
			httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "internal server error")
		}
		return
	}
	// The account is committed; a later login can retry cloud profile initialization
	profile, err := h.cloudStore.EnsureUserProfile(models.UserProfile{ID: user.ID, CreatedAt: time.Now().UTC()})
	if err != nil {
		h.logger.Error("confirmed user profile initialization failed", "user_id", user.ID, "err", err)
		httpresponse.WriteError(c, fiber.StatusServiceUnavailable, "confirmation_session_failed", "account confirmed; please try signing in later")
		return
	}
	session, err := h.newLoginResponse(profile.ID)
	if err != nil {
		h.logger.Error("confirmed user session creation failed", "user_id", user.ID, "err", err)
		httpresponse.WriteError(c, fiber.StatusServiceUnavailable, "confirmation_session_failed", "account confirmed; please try signing in later")
		return
	}
	h.logger.Info("user registration confirmed", "user_id", user.ID)
	httpresponse.WriteJSON(c, fiber.StatusOK, models.RegistrationConfirmationResponse{LoginResponse: session, Username: user.Username})
}

func generateRegistrationKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// Login validates credentials and issues a JWT
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

	session, err := h.newLoginResponse(profile.ID)
	if err != nil {
		h.logger.Error("JWT signing failed", "err", err)
		httpresponse.WriteError(c, fiber.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	h.logger.Info("user signed in", "username", user.Username)
	httpresponse.WriteJSON(c, fiber.StatusOK, session)
}

func (h *AuthHandler) newLoginResponse(userID string) (models.LoginResponse, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(h.jwt.TokenTTL())
	claims := middleware.Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "lyrarma-services-users",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(h.jwt.Secret()))
	if err != nil {
		return models.LoginResponse{}, err
	}
	return models.LoginResponse{
		Token:     tokenString,
		ExpiresAt: expiresAt,
	}, nil
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

// generateID creates a cryptographically random hexadecimal identifier
func generateID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic("failed to obtain random bytes: " + err.Error())
	}
	return hex.EncodeToString(bytes)
}

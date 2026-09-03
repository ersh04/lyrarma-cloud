// Package middleware provides middleware for handling HTTP requests.
package middleware

import (
	"context"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"

	"github.com/lyrarma/cloud-api/src/httpresponse"
)

// ContextKey defines the type used for context value keys.
type ContextKey string

const (
	// ContextKeyUserID stores the authenticated user identifier.
	ContextKeyUserID ContextKey = "userID"
)

// Claims extends the standard JWT claims with a user identifier.
type Claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

// RequireAuth validates the JWT and adds the user identifier to the request context.
func RequireAuth(jwtSecret string) func(c fiber.Ctx) error {
	return func(c fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error":   "unauthorized",
				"message": "Authorization header is missing",
			})
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			return writeUnauthorized(c, "invalid Authorization header format")
		}

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(
			parts[1],
			claims,
			func(token *jwt.Token) (any, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(jwtSecret), nil
			},
		)
		if err != nil || !token.Valid {
			return writeUnauthorized(c, "invalid or expired token")
		}

		c.SetContext(context.WithValue(c.Context(), ContextKeyUserID, claims.UserID))
		return c.Next()
	}
}

// UserIDFromContext returns the user identifier from the context.
func UserIDFromContext(ctx context.Context) string {
	userID, _ := ctx.Value(ContextKeyUserID).(string)
	return userID
}

func writeUnauthorized(c fiber.Ctx, reason string) error {
	return httpresponse.WriteError(c, fiber.StatusUnauthorized, "unauthorized", reason)
}

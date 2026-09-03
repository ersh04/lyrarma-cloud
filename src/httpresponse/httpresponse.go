package httpresponse

import (
	"github.com/gofiber/fiber/v3"
)

// WriteError sends a JSON response with a status code, error type, and message.
func WriteError(c fiber.Ctx, statusCode int, errorType, message string) error {
	return c.Status(statusCode).JSON(fiber.Map{
		"error":   errorType,
		"message": message,
	})
}

// WriteJSON sends a value as JSON with the specified status code.
func WriteJSON(c fiber.Ctx, statusCode int, data any) error {
	return c.Status(statusCode).JSON(data)
}

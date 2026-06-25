package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

// AuthMiddleware validates the Bearer session token from Authorization header.
// It looks up the token in Redis and stores the resolved userID in fiber locals.
func AuthMiddleware(c *fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	if authHeader == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Missing Authorization header"})
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid Authorization header format"})
	}

	token := strings.TrimSpace(parts[1])
	if token == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Missing session token"})
	}

	c.Locals("token", token)

	return c.Next()
}

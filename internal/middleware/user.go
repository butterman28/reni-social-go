package middleware

import (
	"strings"

	"reni-social/internal/utils"

	"github.com/gofiber/fiber/v2"
)

// UserMiddleware validates the session token and loads the authenticated userID.
// It is identical to AuthMiddleware but kept separate so it can be composed
// into workspace-scoped route groups.
func UserMiddleware(c *fiber.Ctx) error {
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

	userID, err := utils.GetSession(token)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid or expired session"})
	}

	c.Locals("userid", userID)
	c.Locals("token", token)

	return c.Next()
}

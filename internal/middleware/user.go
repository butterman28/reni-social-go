package middleware

import (
	"dogo/internal/utils"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func UserMiddleware(c *fiber.Ctx) error {
	// --- 1. Validate Authorization header ---
	authHeader := c.Get("Authorization")
	if authHeader == "" {
		return c.Status(401).JSON(fiber.Map{"error": "Missing Authorization header"})
	}

	// Expect header format: "Bearer <token>"
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return c.Status(401).JSON(fiber.Map{"error": "Invalid Authorization header format"})
	}
	token := parts[1]

	if token == "" {
		fmt.Println("header error - ", token)
		return c.Status(400).JSON(fiber.Map{"error": "Missing required headers"})
	}

	sessionid := c.Get("X-Session-Id")
	if sessionid == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Missing required headers"})
	}

	userID, err := utils.GetSession(sessionid)
	if err != nil {
		return c.Status(401).JSON(fiber.Map{"error": "Invalid or expired session"})
	}

	fmt.Println("User Session ID Authenticated: ", userID)

	// Store userID in context for later handlers
	c.Locals("userid", userID)

	return c.Next()

	// return nil
}

package middleware

import (
	"dogo/internal/utils"
	"dogo/pkg/db"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func AdminMiddleware(c *fiber.Ctx) error {
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

	// --- 2. Check if the user is an admin ---
	var email string
	var isAdmin bool
	query := "SELECT email, is_admin FROM users WHERE id = $1"
	err = db.DB.QueryRowContext(c.Context(), query, userID).Scan(&email, &isAdmin)
	if err != nil {
		return c.Status(401).JSON(fiber.Map{"error": "User not found or database error"})
	}

	// User must be is_admin == true AND email ends with @biki.africa OR email == admin@biki.co
	if !(isAdmin && strings.HasSuffix(email, "@biki.africa")) && email != "admin@biki.co" {
		return c.Status(403).JSON(fiber.Map{"error": "Forbidden: Admin access required"})
	}

	fmt.Println("Admin Session ID Authenticated: ", userID)

	// Store userID in context for later handlers
	c.Locals("userid", userID)

	return c.Next()
}

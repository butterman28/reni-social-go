package middleware

import (
	"github.com/gofiber/fiber/v2"
	// "valsafe/internal/utils"
	"fmt"
	"strings"
)

func AuthMiddleware(c *fiber.Ctx) error {
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

	// Store userID in context for later handlers
	// c.Locals("userToken", userToken)
	// c.Locals("userToken", usertoken)

	return c.Next()

	// return nil
}

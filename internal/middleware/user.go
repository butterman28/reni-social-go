package middleware

import (
	"fmt"
	"strings"

	"reni-social/internal/utils"

	"github.com/gofiber/fiber/v2"
)

// UserMiddleware validates the session token and loads the authenticated userID.
// It is identical to AuthMiddleware but kept separate so it can be composed
// into workspace-scoped route groups.
// UserMiddleware godoc
// @Summary UserMiddleware
// @Description Auto-generated thorough documentation for UserMiddleware
// @Tags middleware
// @Produce json
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
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


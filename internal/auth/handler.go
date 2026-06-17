package auth

import (
	"reni-social/internal/types"

	"github.com/gofiber/fiber/v2"
)

// Signup handles POST /api/auth/signup
func SignupHandler(c *fiber.Ctx) error {
	var req SignupRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: "Invalid request body",
		})
	}

	resp, err := Signup(c.Context(), req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: err.Error(),
		})
	}

	return c.Status(fiber.StatusCreated).JSON(types.APIResponse[*AuthResponse]{
		Status:  true,
		Message: "Account created successfully",
		Data:    resp,
	})
}

// Signin handles POST /api/auth/signin
func SigninHandler(c *fiber.Ctx) error {
	var req SigninRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: "Invalid request body",
		})
	}

	resp, err := Signin(c.Context(), req)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(types.APIResponse[any]{
			Status:  false,
			Message: err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(types.APIResponse[*AuthResponse]{
		Status:  true,
		Message: "Signed in successfully",
		Data:    resp,
	})
}

// Signout handles POST /api/auth/signout
func SignoutHandler(c *fiber.Ctx) error {
	token, ok := c.Locals("token").(string)
	if !ok || token == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(types.APIResponse[any]{
			Status:  false,
			Message: "Not authenticated",
		})
	}

	if err := Signout(token); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(types.APIResponse[any]{
			Status:  false,
			Message: "Could not sign out",
		})
	}

	return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
		Status:  true,
		Message: "Signed out successfully",
	})
}

// Me handles GET /api/auth/me
func MeHandler(c *fiber.Ctx) error {
	userID, ok := c.Locals("userid").(string)
	if !ok || userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(types.APIResponse[any]{
			Status:  false,
			Message: "Not authenticated",
		})
	}

	user, err := GetMe(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(types.APIResponse[any]{
			Status:  false,
			Message: err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
		Status:  true,
		Message: "OK",
		Data:    user,
	})
}

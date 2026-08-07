package auth

import (
	"fmt"
	"reni-social/internal/types"

	"github.com/gofiber/fiber/v2"
)

// RegisterEmailHandler handles POST /api/auth/register/email
// RegisterEmailHandler godoc
// @Summary POST /api/auth/register/email
// @Tags auth
// @Produce json
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/auth/register/email [post]
func RegisterEmailHandler(c *fiber.Ctx) error {
	var req RegisterEmailRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: "Invalid request body",
		})
	}

	resp, err := RegisterEmail(c.Context(), req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: err.Error(),
		})
	}

	return c.Status(resp.HttpStatus).JSON(resp)
}

// ActivateEmailHandler handles POST /api/auth/register/email/activate
// ActivateEmailHandler godoc
// @Summary POST /api/auth/register/email/activate
// @Tags auth
// @Produce json
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/auth/register/email/activate [post]
func ActivateEmailHandler(c *fiber.Ctx) error {
	var req ActivateEmailRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: "Invalid request body",
		})
	}

	resp, err := ActivateEmail(c.Context(), req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: err.Error(),
		})
	}

	return c.Status(resp.HttpStatus).JSON(resp)
}

// CompleteRegisterHandler handles POST /api/auth/register/complete
// CompleteRegisterHandler godoc
// @Summary POST /api/auth/register/complete
// @Tags auth
// @Produce json
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/auth/register/complete [post]
func CompleteRegisterHandler(c *fiber.Ctx) error {
	var req CompleteRegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: "Invalid request body",
		})
	}

	resp, err := CompleteRegister(c.Context(), req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: err.Error(),
		})
	}

	return c.Status(resp.HttpStatus).JSON(resp)
}

// Signin handles POST /api/auth/signin
// SigninHandler godoc
// @Summary POST /api/auth/signin
// @Tags auth
// @Produce json
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/auth/signin [post]
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

	return c.Status(resp.HttpStatus).JSON(resp)
}

// Signout handles POST /api/auth/signout
// SignoutHandler godoc
// @Summary POST /api/in/auth/signout
// @Tags auth
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/auth/signout [post]
func SignoutHandler(c *fiber.Ctx) error {
	token, ok := c.Locals("token").(string)
	if !ok || token == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(types.APIResponse[any]{
			Status:  false,
			Message: "Not authenticated",
		})
	}

	if err := Signout(token); err != nil {
		fmt.Println(err)
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
// MeHandler godoc
// @Summary GET /api/in/auth/me
// @Tags auth
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/auth/me [get]
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

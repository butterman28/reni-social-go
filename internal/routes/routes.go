package routes

import (
	"dogo/internal/middleware"
	"dogo/internal/services/auth"

	"github.com/gofiber/fiber/v2"
)

// Setup configures all the application routes.
func Setup(app *fiber.App) {
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status": "healthy",
		})
	})

	_ = middleware.AuthMiddleware

	// AUTHENTICATED SERVICES
	// set the middleware that handles header authentication
	api := app.Group("/api", middleware.AuthMiddleware)
	us := api.Group("/uin", middleware.UserMiddleware)
	dr := api.Group("/din", middleware.DriverMiddleware)
	ad := api.Group("/ain", middleware.AdminMiddleware)

	_ = us
	_ = dr
	_ = ad

	// User Auth
	api.Post("/auth/signup", auth.Signup)
	api.Post("/auth/verify", auth.Verify)
	api.Post("/auth/signup/complete", auth.CompleteSignup)
	api.Post("/auth/signin", auth.Signin)
	api.Post("/admin/auth/signin", auth.AdminSignin)

}

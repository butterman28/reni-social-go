package routes

import (
	"github.com/gofiber/fiber/v2"

	"reni-social/internal/auth"
	"reni-social/internal/middleware"
	"reni-social/internal/post"
	"reni-social/internal/workspace"
)

// Setup registers all application routes on the Fiber app.
func Setup(app *fiber.App) {

	// ──────────────────────────────────────────────────────
	// Health check
	// ──────────────────────────────────────────────────────
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "healthy"})
	})

	// ──────────────────────────────────────────────────────
	// Public auth routes (no middleware)
	// ──────────────────────────────────────────────────────
	app.Post("/api/auth/signup", auth.SignupHandler)
	app.Post("/api/auth/signin", auth.SigninHandler)

	// ──────────────────────────────────────────────────────
	// Authenticated routes
	// ──────────────────────────────────────────────────────
	api := app.Group("/api", middleware.AuthMiddleware)

	// Auth (session-protected)
	api.Post("/auth/signout", auth.SignoutHandler)
	api.Get("/auth/me", auth.MeHandler)

	// Workspaces
	api.Post("/workspaces", workspace.Create)
	api.Get("/workspaces", workspace.ListMine)
	api.Get("/workspaces/in/:id", workspace.GetOne)

	// Workspace members
	api.Post("/workspaces/in/:id/members", workspace.Invite)
	api.Get("/workspaces/in/:id/members", workspace.Members)
	api.Delete("/workspaces/in/:id/members/:userId", workspace.RemoveMemberHandler)

	// Platform connections (OAuth)
	api.Get("/workspaces/in/:id/connections", workspace.Connections)
	api.Post("/workspaces/in/:id/connect/:platform", workspace.ConnectPlatform)
	api.Get("/workspaces/in/:id/connect/:platform/callback", workspace.OAuthCallback)
	api.Delete("/workspaces/in/:id/connections/:connectionId", workspace.Disconnect)

	// Posts
	api.Post("/posts", post.Create)
	api.Get("/posts", post.List)
	api.Get("/posts/in/:id", post.Get)
	api.Put("/posts/in/:id", post.Update)
	api.Delete("/posts/in/:id", post.Delete)
	api.Post("/posts/in/:id/publish", post.Publish)
	api.Post("/posts/in/:id/schedule", post.Schedule)
}

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
	api.Get("/workspaces/:id", workspace.GetOne)

	// Workspace members
	api.Post("/workspaces/:id/members", workspace.Invite)
	api.Get("/workspaces/:id/members", workspace.Members)
	api.Delete("/workspaces/:id/members/:userId", workspace.RemoveMemberHandler)

	// Platform connections (OAuth)
	api.Get("/workspaces/:id/connections", workspace.Connections)
	api.Post("/workspaces/:id/connect/:platform", workspace.ConnectPlatform)
	api.Get("/workspaces/:id/connect/:platform/callback", workspace.OAuthCallback)
	api.Delete("/workspaces/:id/connections/:connectionId", workspace.Disconnect)

	// Posts
	api.Post("/posts", post.Create)
	api.Get("/posts", post.List)
	api.Get("/posts/:id", post.Get)
	api.Put("/posts/:id", post.Update)
	api.Delete("/posts/:id", post.Delete)
	api.Post("/posts/:id/publish", post.Publish)
	api.Post("/posts/:id/schedule", post.Schedule)
}

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

	api := app.Group("/api", middleware.AuthMiddleware)

	// ──────────────────────────────────────────────────────
	// Public auth routes (no middleware)
	// ──────────────────────────────────────────────────────
	api.Post("/auth/register/email", auth.RegisterEmailHandler)
	api.Post("/auth/register/email/activate", auth.ActivateEmailHandler)
	api.Post("/auth/register/complete", auth.CompleteRegisterHandler)
	api.Post("/auth/signin", auth.SigninHandler)

	// ──────────────────────────────────────────────────────
	// Authenticated routes
	// ──────────────────────────────────────────────────────
	in := api.Group("/in", middleware.UserMiddleware)

	// Auth (session-protected)
	in.Post("/auth/signout", auth.SignoutHandler)
	in.Get("/auth/me", auth.MeHandler)

	// Workspaces
	in.Post("/workspaces", workspace.Create)
	in.Get("/workspaces", workspace.ListMine)
	in.Get("/workspaces/in/:id", workspace.GetOne)
	in.Get("/workspaces/in/slug/:slug", workspace.GetOneBySlug)

	// Workspace members
	in.Post("/workspaces/in/:id/members", workspace.Invite)
	in.Get("/workspaces/in/:id/members", workspace.Members)
	in.Delete("/workspaces/in/:id/members/in/:userId", workspace.RemoveMemberHandler)

	// Platform connections (OAuth)
	in.Get("/workspaces/in/:id/connections", workspace.Connections)
	in.Post("/workspaces/in/:id/connect/:platform", workspace.ConnectPlatform)
	in.Get("/workspaces/in/:id/connect/:platform/callback", workspace.OAuthCallback)
	in.Delete("/workspaces/in/:id/connections/:connectionId", workspace.Disconnect)

	// Posts
	in.Post("/posts", post.Create)
	in.Get("/posts", post.List)
	in.Get("/posts/in/:id", post.Get)
	in.Put("/posts/in/:id", post.Update)
	in.Delete("/posts/in/:id", post.Delete)
	in.Post("/posts/in/:id/publish", post.Publish)
	in.Post("/posts/in/:id/schedule", post.Schedule)
}

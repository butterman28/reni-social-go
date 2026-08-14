package routes

import (
	"reni-social/internal/auth"
	"reni-social/internal/middleware"
	"reni-social/internal/post"
	"reni-social/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

// Setup godoc
// @Summary Setup
// @Description Auto-generated thorough documentation for Setup
// @Tags routes
// @Produce json
// @Param app body object true "app"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
func Setup(app *fiber.App) {
	// ──────────────────────────────────────────────────────
	// 1. PUBLIC ROUTES
	// ──────────────────────────────────────────────────────
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "healthy"})
	})

	app.Get("/api/workspaces/connect/:platform/callback", workspace.OAuthCallback)

	// Public Auth Routes (Strict Rate Limit: Max 5 req/min to stop brute forcing)
	authGroup := app.Group("/api/auth", middleware.StrictAuthLimiter())
	authGroup.Post("/register/email", auth.RegisterEmailHandler)
	authGroup.Post("/register/email/activate", auth.ActivateEmailHandler)
	authGroup.Post("/register/complete", auth.CompleteRegisterHandler)
	authGroup.Post("/signin", auth.SigninHandler)

	// ──────────────────────────────────────────────────────
	// 2. AUTHENTICATED & PROTECTED ROUTES
	// ──────────────────────────────────────────────────────
	// Apply standard rate limit (60 req/min) + Auth check
	api := app.Group("/api", middleware.GeneralApiLimiter(), middleware.AuthMiddleware)
	
	in := api.Group("/in", middleware.UserMiddleware)

	in.Post("/auth/signout", auth.SignoutHandler)
	in.Get("/auth/me", auth.MeHandler)

	in.Post("/workspaces", workspace.Create)
	in.Get("/workspaces", workspace.ListMine)
	in.Get("/workspaces/in/:id", workspace.GetOne)
	in.Get("/workspaces/in/slug/:slug", workspace.GetOneBySlug)

	in.Post("/workspaces/in/:id/members", workspace.Invite)
	in.Get("/workspaces/in/:id/members", workspace.Members)
	in.Delete("/workspaces/in/:id/members/in/:userId", workspace.RemoveMemberHandler)
	in.Get("/workspaces/in/:id/connections/reauth-status", workspace.ReauthStatus)

	in.Get("/workspaces/in/:id/x/connections", workspace.ListXConnections)
	in.Get("/workspaces/in/:id/x/tweets/in/:tweetId", workspace.GetXTweet)
	in.Get("/workspaces/in/:id/x/tweets/in/:tweetId/full", workspace.GetXTweetWithIncludes)
	in.Get("/workspaces/in/:id/x/users/in/:userId", workspace.GetXUser)
	in.Get("/workspaces/in/:id/x/spaces/in/:spaceId", workspace.GetXSpace)
	in.Get("/workspaces/in/:id/x/lists/in/:listId", workspace.GetXList)

	in.Get("/workspaces/in/:id/connections", workspace.Connections)
	in.Post("/workspaces/in/:id/connect/in/:platform", workspace.ConnectPlatform)
	in.Delete("/workspaces/in/:id/connections/:connectionId", workspace.Disconnect)

	in.Post("/posts", post.Create)
	in.Get("/posts", post.List)
	in.Get("/posts/in/:id", post.Get)
	in.Put("/posts/in/:id", post.Update)
	in.Delete("/posts/in/:id", post.Delete)
	in.Post("/posts/in/:id/publish", post.Publish)
	in.Post("/posts/in/:id/schedule", post.Schedule)
	in.Get("/posts/in/:id/results", post.Results)   
}
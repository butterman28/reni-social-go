package routes

import (
	"reni-social/internal/auth"
	"reni-social/internal/middleware"
	"reni-social/internal/post"
	"reni-social/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

func Setup(app *fiber.App) {
	// ──────────────────────────────────────────────────────
	// 1. PUBLIC ROUTES (No Authentication Required)
	// ──────────────────────────────────────────────────────
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "healthy"})
	})


	app.Get("/api/workspaces/connect/:platform/callback", workspace.OAuthCallback)

	// Public Auth Routes (Signup/Signin must also be public)
	app.Post("/api/auth/register/email", auth.RegisterEmailHandler)
	app.Post("/api/auth/register/email/activate", auth.ActivateEmailHandler)
	app.Post("/api/auth/register/complete", auth.CompleteRegisterHandler)
	app.Post("/api/auth/signin", auth.SigninHandler)

	// ──────────────────────────────────────────────────────
	// 2. AUTHENTICATED ROUTES (Requires Authorization Header)
	// ──────────────────────────────────────────────────────
	api := app.Group("/api", middleware.AuthMiddleware)
	
	// Routes that need both Auth and User Session validation
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
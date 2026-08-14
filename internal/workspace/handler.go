package workspace

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"net/url"

	"reni-social/internal/domain"
	"reni-social/internal/platform/linkedin"
	"reni-social/internal/platform/meta"
	xplatform "reni-social/internal/platform/x"
	"reni-social/internal/types"
	"reni-social/internal/utils"
	"reni-social/pkg/config"
	"reni-social/pkg/db"
)

// ──────────────────────────────────────────────────────────────────────────────
// Workspace CRUD
// ──────────────────────────────────────────────────────────────────────────────

// Create handles POST /api/workspaces
// Create godoc
// @Summary POST /api/in/workspaces
// @Tags workspace
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/workspaces [post]
func Create(c *fiber.Ctx) error {
	userID := c.Locals("userid").(string)

	var req CreateWorkspaceRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: "Invalid request body",
		})
	}

	ws, err := CreateWorkspace(c.Context(), userID, req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: err.Error(),
		})
	}

	return c.Status(fiber.StatusCreated).JSON(types.APIResponse[any]{
		Status:  true,
		Message: "Workspace created",
		Data:    ws,
	})
}

// GetOne handles GET /api/workspaces/in/:id
// GetOne godoc
// @Summary GET /api/in/workspaces/in/:id
// @Tags workspace
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/workspaces/in/{id} [get]
func GetOne(c *fiber.Ctx) error {
	userID := c.Locals("userid").(string)
	wsID := c.Params("id")

	ws, err := GetWorkspace(c.Context(), wsID, userID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(types.APIResponse[any]{
			Status:  false,
			Message: err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
		Status:  true,
		Message: "OK",
		Data:    ws,
	})
}

// GetOneBySlug handles GET /api/workspaces/in/slug/:slug
// GetOneBySlug godoc
// @Summary GET /api/in/workspaces/in/slug/:slug
// @Tags workspace
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param slug path string true "slug"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/workspaces/in/slug/{slug} [get]
func GetOneBySlug(c *fiber.Ctx) error {
	userID := c.Locals("userid").(string)
	slug := c.Params("slug")

	ws, err := GetWorkspaceBySlug(c.Context(), slug, userID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(types.APIResponse[any]{
			Status:  false,
			Message: err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
		Status:  true,
		Message: "OK",
		Data:    ws,
	})
}

// ListMine handles GET /api/workspaces
// ListMine godoc
// @Summary GET /api/in/workspaces
// @Tags workspace
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/workspaces [get]
func ListMine(c *fiber.Ctx) error {
	userID := c.Locals("userid").(string)

	workspaces, err := ListMyWorkspaces(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(types.APIResponse[any]{
			Status:  false,
			Message: err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
		Status:  true,
		Message: "OK",
		Data:    workspaces,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// Members
// ──────────────────────────────────────────────────────────────────────────────

// Invite handles POST /api/workspaces/in/:id/members
// Invite godoc
// @Summary POST /api/in/workspaces/in/:id/members
// @Tags workspace
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/workspaces/in/{id}/members [post]
func Invite(c *fiber.Ctx) error {
	userID := c.Locals("userid").(string)
	wsID := c.Params("id")

	var req InviteMemberRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: "Invalid request body",
		})
	}

	switch req.Role {
	case "", domain.RoleMember, domain.RoleAdmin, domain.RoleOwner:
		// Valid roles (empty string defaults to member in the service layer)
	default:
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: "Invalid role",
		})
	}

	if err := InviteMember(c.Context(), wsID, userID, req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
		Status:  true,
		Message: "Member invited",
	})
}

// Members handles GET /api/workspaces/in/:id/members
// Members godoc
// @Summary GET /api/in/workspaces/in/:id/members
// @Tags workspace
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/workspaces/in/{id}/members [get]
func Members(c *fiber.Ctx) error {
	userID := c.Locals("userid").(string)
	wsID := c.Params("id")

	members, err := ListMembers(c.Context(), wsID, userID)
	if err != nil {
		return c.Status(fiber.StatusForbidden).JSON(types.APIResponse[any]{
			Status:  false,
			Message: err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
		Status:  true,
		Message: "OK",
		Data:    members,
	})
}

// RemoveMemberHandler handles DELETE /api/workspaces/in/:id/members/:userId
// RemoveMemberHandler godoc
// @Summary DELETE /api/in/workspaces/in/:id/members/in/:userId
// @Tags workspace
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Param userId path string true "userId"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/workspaces/in/{id}/members/in/{userId} [delete]
func RemoveMemberHandler(c *fiber.Ctx) error {
	callerID := c.Locals("userid").(string)
	wsID := c.Params("id")
	targetID := c.Params("userId")

	if err := RemoveMember(c.Context(), wsID, callerID, targetID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
		Status:  true,
		Message: "Member removed",
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// Platform Connections
// ──────────────────────────────────────────────────────────────────────────────

// Connections handles GET /api/workspaces/in/:id/connections
// Connections godoc
// @Summary GET /api/in/workspaces/in/:id/connections
// @Tags workspace
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/workspaces/in/{id}/connections [get]
func Connections(c *fiber.Ctx) error {
	userID := c.Locals("userid").(string)
	wsID := c.Params("id")

	conns, err := ListConnections(c.Context(), wsID, userID)
	if err != nil {
		return c.Status(fiber.StatusForbidden).JSON(types.APIResponse[any]{
			Status:  false,
			Message: err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
		Status:  true,
		Message: "OK",
		Data:    conns,
	})
}


// ConnectPlatform handles POST /api/workspaces/in/:id/connect/:platform
// ConnectPlatform godoc
// @Summary POST /api/in/workspaces/in/:id/connect/in/:platform
// @Tags workspace
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Param platform path string true "platform"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/workspaces/in/{id}/connect/in/{platform} [post]
func ConnectPlatform(c *fiber.Ctx) error {
	userID := c.Locals("userid").(string)
	wsID := c.Params("id")
	p := c.Params("platform")

	// 1. Verify the user has permission
	if err := requireRole(c.UserContext(), wsID, userID, domain.RoleAdmin, domain.RoleOwner); err != nil {
		return c.Status(fiber.StatusForbidden).JSON(types.APIResponse[any]{
			Status:  false,
			Message: err.Error(),
		})
	}

	if p != "linkedin" && p != "meta" && p != "x" {
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: "Unsupported platform: " + p,
		})
	}

		// ?force=true bypasses the "already connected" short-circuit so the
	// frontend's Reconnect button can always trigger a fresh OAuth flow
	// (needed when a connection is flagged needs_reauth but not yet expired).
	force := c.QueryBool("force")

	if !force {
		hasActive, err := CheckAndReviveConnection(c.UserContext(), wsID, p)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(types.APIResponse[any]{
				Status:  false,
				Message: "Failed to check existing connections: " + err.Error(),
			})
		}
		if hasActive {
			return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
				Status:  true,
				Message: "Already connected",
				Data:    fiber.Map{"already_connected": true},
			})
		}
	}

	// 3. Proceed with normal OAuth flow
	state := wsID + ":" + utils.RandomAlphaNumeric(16)
	utils.RDB.Set(utils.Ctx, "oauth_state:"+state, wsID, 10*60*1000000000)
	
	var redirectURL string
	switch p {
	case "linkedin":
		redirectURL = linkedin.AuthorizationURL(state, wsID, p)
	case "meta":
		redirectURL = meta.InstagramLoginAuthorizationURL(state)
	case "x":
		xClient := xplatform.New()
		_, authURL, err := xClient.RequestToken(c.UserContext(), state)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(types.APIResponse[any]{
				Status:  false,
				Message: "Failed to initiate X OAuth: " + err.Error(),
			})
		}
		redirectURL = authURL
	}

	return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
		Status:  true,
		Message: "OK",
		Data:    fiber.Map{"redirect_url": redirectURL},
	})
}

// OAuthCallback handles GET /api/workspaces/in/:id/connect/:platform/callback
// OAuthCallback godoc
// @Summary GET /api/workspaces/connect/:platform/callback
// @Tags workspace
// @Produce json
// @Param platform path string true "platform"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/workspaces/connect/{platform}/callback [get]
func OAuthCallback(c *fiber.Ctx) error {
	p := c.Params("platform")
	code := c.Query("code")
	state := c.Query("state")
	if state == "" {
		return c.Redirect(config.C.AppUrl + "/?error=" + url.QueryEscape("Missing state parameter"))
	}

	// Extract wsID from state (format: "wsID:randomString")
	parts := strings.Split(state, ":")
	if len(parts) < 2 {
		return c.Redirect(config.C.AppUrl + "/?error=" + url.QueryEscape("Invalid state format"))
	}
	wsID := parts[0]

	// Verify state
	storedWsID, err := utils.RDB.Get(utils.Ctx, "oauth_state:"+state).Result()
	if err != nil || storedWsID != wsID {
		return c.Redirect(config.C.AppUrl + "/?error=" + url.QueryEscape("Invalid or expired OAuth state"))
	}

	// Clear the state from Redis
	utils.RDB.Del(utils.Ctx, "oauth_state:"+state)

	wsUUID, err := uuid.Parse(wsID)
	if err != nil {
		return c.Redirect(config.C.AppUrl + "/?error=" + url.QueryEscape("Invalid workspace ID"))
	}

	var slug string
	err = db.DB.QueryRowContext(c.UserContext(), `SELECT slug FROM workspaces WHERE id = $1`, wsUUID).Scan(&slug)
	if err != nil || slug == "" {
		slug = wsID // Fallback to UUID if slug is somehow missing
	}
	redirectBase := config.C.AppUrl + "/" + slug + "/settings"

	var conn *domain.PlatformConnection
	switch p {
	case "linkedin":
		client := linkedin.New()
		conn, err = client.ExchangeCode(c.UserContext(), code)
	case "meta":
		client := meta.New()
		conn, err = client.ExchangeInstagramLoginCode(c.UserContext(), code)
	case "x":
		client := xplatform.New()
		oauthToken := c.Query("oauth_token")
		oauthVerifier := c.Query("oauth_verifier")
		conn, err = client.ExchangeVerifier(c.UserContext(), oauthToken, oauthVerifier)
	default:
		return c.Redirect(redirectBase + "?tab=accounts&error=" + url.QueryEscape("Unsupported platform"))
	}

	if err != nil {
		return c.Redirect(redirectBase + "?tab=accounts&error=" + url.QueryEscape("OAuth exchange failed: "+err.Error()))
	}

	conn.WorkspaceID = wsUUID

	// Save the connection (This will now succeed because we fixed the SQL comma!)
	if err := SaveConnection(c.UserContext(), conn); err != nil {
		return c.Redirect(redirectBase + "?tab=accounts&error=" + url.QueryEscape("Failed to save connection: "+err.Error()))
	}

	// Redirect to frontend settings page, accounts tab, with success message
	return c.Redirect(redirectBase + "?tab=accounts&connected=" + p)
}
// Disconnect handles DELETE /api/workspaces/in/:id/connections/:connectionId
// Disconnect godoc
// @Summary DELETE /api/in/workspaces/in/:id/connections/:connectionId
// @Tags workspace
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Param connectionId path string true "connectionId"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/workspaces/in/{id}/connections/{connectionId} [delete]
func Disconnect(c *fiber.Ctx) error {
	callerID := c.Locals("userid").(string)
	wsID := c.Params("id")
	connectionID := c.Params("connectionId")

	if err := DisconnectPlatform(c.Context(), wsID, connectionID, callerID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
		Status:  true,
		Message: "Platform disconnected",
	})
}
// ──────────────────────────────────────────────────────────────────────────────
// X (Twitter) — read endpoints
// ──────────────────────────────────────────────────────────────────────────────

// GetXTweet handles GET /api/workspaces/in/:id/x/tweets/:tweetId
// Optional query params are passed through as tweet.fields / expansions etc.
// GetXTweet godoc
// @Summary GET /api/in/workspaces/in/:id/x/tweets/in/:tweetId
// @Tags workspace
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Param tweetId path string true "tweetId"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/workspaces/in/{id}/x/tweets/in/{tweetId} [get]
func GetXTweet(c *fiber.Ctx) error {
	userID := c.Locals("userid").(string)
	wsID := c.Params("id")
	tweetID := c.Params("tweetId")
	connectionID := c.Query("connection_id")
	if _, err := GetWorkspace(c.Context(), wsID, userID); err != nil {
		return c.Status(fiber.StatusForbidden).JSON(types.APIResponse[any]{
			Status: false, Message: err.Error(),
		})
	}

	conn, err := GetXConnection(c.Context(), wsID, connectionID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(types.APIResponse[any]{
			Status: false, Message: err.Error(),
		})
	}

	client := xplatform.New()
	params := url.Values{}
	for k, v := range c.Queries() {
		params.Set(k, v)
	}

	data, err := client.GetTweet(c.Context(), conn, tweetID, params)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(types.APIResponse[any]{
			Status: false, Message: err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
		Status: true, Message: "OK", Data: data,
	})
}

// GetXTweetWithIncludes handles GET /api/workspaces/in/:id/x/tweets/:tweetId/full
// Fixed rich-expansion endpoint (media, polls, places, author) — no custom fields needed.
// GetXTweetWithIncludes godoc
// @Summary GET /api/in/workspaces/in/:id/x/tweets/in/:tweetId/full
// @Tags workspace
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Param tweetId path string true "tweetId"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/workspaces/in/{id}/x/tweets/in/{tweetId}/full [get]
func GetXTweetWithIncludes(c *fiber.Ctx) error {
	userID := c.Locals("userid").(string)
	wsID := c.Params("id")
	tweetID := c.Params("tweetId")
	connectionID := c.Query("connection_id")

	if _, err := GetWorkspace(c.Context(), wsID, userID); err != nil {
		return c.Status(fiber.StatusForbidden).JSON(types.APIResponse[any]{
			Status: false, Message: err.Error(),
		})
	}

	conn, err := GetXConnection(c.Context(), wsID,connectionID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(types.APIResponse[any]{
			Status: false, Message: err.Error(),
		})
	}

	client := xplatform.New()
	data, err := client.GetTweetWithIncludes(c.Context(), conn, tweetID)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(types.APIResponse[any]{
			Status: false, Message: err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
		Status: true, Message: "OK", Data: data,
	})
}

// GetXUser handles GET /api/workspaces/in/:id/x/users/:userId
// GetXUser godoc
// @Summary GET /api/in/workspaces/in/:id/x/users/in/:userId
// @Tags workspace
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Param userId path string true "userId"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/workspaces/in/{id}/x/users/in/{userId} [get]
func GetXUser(c *fiber.Ctx) error {
	userID := c.Locals("userid").(string)
	wsID := c.Params("id")
	targetUserID := c.Params("userId")
	connectionID := c.Query("connection_id")

	if _, err := GetWorkspace(c.Context(), wsID, userID); err != nil {
		return c.Status(fiber.StatusForbidden).JSON(types.APIResponse[any]{
			Status: false, Message: err.Error(),
		})
	}

	conn, err := GetXConnection(c.Context(), wsID, connectionID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(types.APIResponse[any]{
			Status: false, Message: err.Error(),
		})
	}

	client := xplatform.New()
	params := url.Values{}
	for k, v := range c.Queries() {
		params.Set(k, v)
	}
	if params.Get("user.fields") == "" {
		params.Set("user.fields", "description,profile_image_url,public_metrics,created_at")
	}

	data, err := client.GetUser(c.Context(), conn, targetUserID, params)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(types.APIResponse[any]{
			Status: false, Message: err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
		Status: true, Message: "OK", Data: data,
	})
}

// GetXSpace handles GET /api/workspaces/in/:id/x/spaces/:spaceId
// GetXSpace godoc
// @Summary GET /api/in/workspaces/in/:id/x/spaces/in/:spaceId
// @Tags workspace
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Param spaceId path string true "spaceId"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/workspaces/in/{id}/x/spaces/in/{spaceId} [get]
func GetXSpace(c *fiber.Ctx) error {
	userID := c.Locals("userid").(string)
	wsID := c.Params("id")
	spaceID := c.Params("spaceId")
	connectionID := c.Query("connection_id")

	if _, err := GetWorkspace(c.Context(), wsID, userID); err != nil {
		return c.Status(fiber.StatusForbidden).JSON(types.APIResponse[any]{
			Status: false, Message: err.Error(),
		})
	}

	conn, err := GetXConnection(c.Context(), wsID, connectionID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(types.APIResponse[any]{
			Status: false, Message: err.Error(),
		})
	}

	client := xplatform.New()
	params := url.Values{}
	for k, v := range c.Queries() {
		params.Set(k, v)
	}
	if params.Get("space.fields") == "" {
		params.Set("space.fields", "title,state,created_at,lang")
	}

	data, err := client.GetSpace(c.Context(), conn, spaceID, params)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(types.APIResponse[any]{
			Status: false, Message: err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
		Status: true, Message: "OK", Data: data,
	})
}

// GetXList handles GET /api/workspaces/in/:id/x/lists/:listId
// GetXList godoc
// @Summary GET /api/in/workspaces/in/:id/x/lists/in/:listId
// @Tags workspace
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Param listId path string true "listId"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/workspaces/in/{id}/x/lists/in/{listId} [get]
func GetXList(c *fiber.Ctx) error {
	userID := c.Locals("userid").(string)
	wsID := c.Params("id")
	listID := c.Params("listId")
	connectionID := c.Query("connection_id")

	if _, err := GetWorkspace(c.Context(), wsID, userID); err != nil {
		return c.Status(fiber.StatusForbidden).JSON(types.APIResponse[any]{
			Status: false, Message: err.Error(),
		})
	}

	conn, err := GetXConnection(c.Context(), wsID,connectionID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(types.APIResponse[any]{
			Status: false, Message: err.Error(),
		})
	}

	client := xplatform.New()
	params := url.Values{}
	for k, v := range c.Queries() {
		params.Set(k, v)
	}
	if params.Get("list.fields") == "" {
		params.Set("list.fields", "name,description,member_count,follower_count")
	}

	data, err := client.GetList(c.Context(), conn, listID, params)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(types.APIResponse[any]{
			Status: false, Message: err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
		Status: true, Message: "OK", Data: data,
	})
}

// ReauthStatus handles GET /api/in/workspaces/in/:id/connections/reauth-status
// Tokenless advisory endpoint — safe for global frontend polling.
// ReauthStatus godoc
// @Summary GET /api/in/workspaces/in/:id/connections/reauth-status
// @Tags workspace
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/workspaces/in/{id}/connections/reauth-status [get]
func ReauthStatus(c *fiber.Ctx) error {
	userID := c.Locals("userid").(string)
	wsID := c.Params("id")

	alerts, err := ListReauthAlerts(c.Context(), wsID, userID)
	if err != nil {
		return c.Status(fiber.StatusForbidden).JSON(types.APIResponse[any]{
			Status: false, Message: err.Error(),
		})
	}
	return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
		Status:  true,
		Message: "OK",
		Data: fiber.Map{
			"needs_attention": len(alerts) > 0,
			"alerts":          alerts,
		},
	})
}
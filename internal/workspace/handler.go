package workspace

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"reni-social/internal/domain"
	"reni-social/internal/platform/linkedin"
	"reni-social/internal/platform/meta"
	xplatform "reni-social/internal/platform/x"
	"reni-social/internal/types"
	"reni-social/internal/utils"
	"reni-social/pkg/config"
)

// ──────────────────────────────────────────────────────────────────────────────
// Workspace CRUD
// ──────────────────────────────────────────────────────────────────────────────

// Create handles POST /api/workspaces
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

// ListMine handles GET /api/workspaces
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
// Returns the OAuth redirect URL for the frontend to navigate the user to.
func ConnectPlatform(c *fiber.Ctx) error {
	wsID := c.Params("id")
	p := c.Params("platform")

	// State encodes workspaceID so the callback knows where to save the connection
	state := wsID + ":" + utils.RandomAlphaNumeric(16)

	// Store state in Redis for 10 minutes to verify in the callback
	utils.RDB.Set(utils.Ctx, "oauth_state:"+state, wsID, 10*60*1000000000)

	var redirectURL string
	switch p {
	case "linkedin":
		redirectURL = linkedin.AuthorizationURL(state)
	case "meta":
		redirectURL = meta.AuthorizationURL(state)
	case "x":
		xClient := xplatform.New()
		_, authURL, err := xClient.RequestToken(c.Context())
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(types.APIResponse[any]{
				Status:  false,
				Message: "Failed to initiate X OAuth: " + err.Error(),
			})
		}
		redirectURL = authURL
	default:
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: "Unsupported platform: " + p,
		})
	}

	return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
		Status:  true,
		Message: "OK",
		Data:    fiber.Map{"redirect_url": redirectURL},
	})
}

// OAuthCallback handles GET /api/workspaces/in/:id/connect/:platform/callback
func OAuthCallback(c *fiber.Ctx) error {
	wsID := c.Params("id")
	p := c.Params("platform")
	code := c.Query("code")
	state := c.Query("state")

	// Verify state
	storedWsID, err := utils.RDB.Get(utils.Ctx, "oauth_state:"+state).Result()
	if err != nil || storedWsID != wsID {
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: "Invalid or expired OAuth state",
		})
	}
	utils.RDB.Del(utils.Ctx, "oauth_state:"+state)

	wsUUID, err := uuid.Parse(wsID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: "Invalid workspace ID",
		})
	}

	var conn *domain.PlatformConnection

	switch p {
	case "linkedin":
		client := linkedin.New()
		conn, err = client.ExchangeCode(c.Context(), code)
	case "meta":
		client := meta.New()
		conn, err = client.ExchangeCode(c.Context(), code, "meta_fb")
	case "x":
		client := xplatform.New()
		oauthToken := c.Query("oauth_token")
		oauthVerifier := c.Query("oauth_verifier")
		conn, err = client.ExchangeVerifier(c.Context(), oauthToken, oauthVerifier)
	default:
		return c.Status(fiber.StatusBadRequest).JSON(types.APIResponse[any]{
			Status:  false,
			Message: "Unsupported platform",
		})
	}

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(types.APIResponse[any]{
			Status:  false,
			Message: "OAuth exchange failed: " + err.Error(),
		})
	}

	conn.WorkspaceID = wsUUID

	if err := SaveConnection(c.Context(), conn); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(types.APIResponse[any]{
			Status:  false,
			Message: "Failed to save connection",
		})
	}

	// Redirect to frontend success page
	return c.Redirect(config.C.AppUrl + "/workspaces/" + wsID + "?connected=" + p)
}

// Disconnect handles DELETE /api/workspaces/in/:id/connections/:connectionId
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

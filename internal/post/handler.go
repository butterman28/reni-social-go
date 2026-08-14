package post

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"reni-social/internal/domain"
	"reni-social/internal/scheduler"
	"reni-social/pkg/db"
)

// Create handles POST /api/posts
// Create godoc
// @Summary POST /api/in/posts
// @Tags post
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/posts [post]
func Create(c *fiber.Ctx) error {
	userID := c.Locals("userid").(string)

	var req CreatePostRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	post, err := CreatePost(c.Context(), userID, req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"post": post})
}

// List handles GET /api/posts?workspace_id=&status=&page=
// List godoc
// @Summary List
// @Description Auto-generated thorough documentation for List
// @Tags post
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param workspace_id query string true "Workspace ID"
// @Router /api/in/posts [get]
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func List(c *fiber.Ctx) error {
	workspaceID := c.Query("workspace_id")
	if workspaceID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "workspace_id is required"})
	}

	filter := PostFilter{
		Status: c.Query("status"),
		Page:   c.QueryInt("page", 0),
		Limit:  c.QueryInt("limit", 20),
	}

	posts, err := ListPosts(c.Context(), workspaceID, filter)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"posts": posts, "page": filter.Page, "limit": filter.Limit})
}

// Get handles GET /api/posts/:id
// Get godoc
// @Summary GET /api/in/posts/in/:id
// @Tags post
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Param workspace_id query string true "Workspace ID"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/posts/in/{id} [get]
func Get(c *fiber.Ctx) error {
	postID := c.Params("id")
	workspaceID := c.Query("workspace_id")
	if workspaceID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "workspace_id is required"})
	}

	post, err := GetPost(c.Context(), postID, workspaceID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"post": post})
}

// Update handles PUT /api/posts/:id
// Update godoc
// @Summary Update
// @Description Auto-generated thorough documentation for Update
// @Tags post
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Router /api/in/posts/in/{id} [put]
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func Update(c *fiber.Ctx) error {
	postID := c.Params("id")
	workspaceID := c.Query("workspace_id")
	if workspaceID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "workspace_id is required"})
	}

	var req UpdatePostRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	post, err := UpdatePost(c.Context(), postID, workspaceID, req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"post": post})
}

// Delete handles DELETE /api/posts/:id
// Delete godoc
// @Summary DELETE /api/in/posts/in/:id
// @Tags post
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/posts/in/{id} [delete]
func Delete(c *fiber.Ctx) error {
	postID := c.Params("id")
	workspaceID := c.Query("workspace_id")
	if workspaceID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "workspace_id is required"})
	}

	if err := DeletePost(c.Context(), postID, workspaceID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"message": "post deleted"})
}

// Publish handles POST /api/posts/:id/publish
// Publish godoc
// @Summary Publish
// @Description Auto-generated thorough documentation for Publish
// @Tags post
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Router /api/in/posts/in/{id}/publish [post]
// @Success 201 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func Publish(c *fiber.Ctx) error {
	postID := c.Params("id")
	workspaceID := c.Query("workspace_id")
	if workspaceID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "workspace_id is required"})
	}
	
	// Call the updated service that returns the attemptID
	attemptID, err := PublishNow(c.Context(), postID, workspaceID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	
	//Tell the frontend to start tracking this specific attempt
	return c.JSON(fiber.Map{
		"message":    "publishing_initiated",
		"attempt_id": attemptID,
	})
}

// Schedule handles POST /api/posts/:id/schedule
// Schedule godoc
// @Summary POST /api/in/posts/in/:id/schedule
// @Tags post
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/posts/in/{id}/schedule [post]
func Schedule(c *fiber.Ctx) error {
	postID := c.Params("id")
	workspaceID := c.Query("workspace_id")
	if workspaceID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "workspace_id is required"})
	}

	var body struct {
		ScheduledAt time.Time `json:"scheduled_at"`
		Platforms   []domain.Platform `json:"platforms"`
	}
	if err := c.BodyParser(&body); err != nil || body.ScheduledAt.IsZero() {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "scheduled_at (ISO8601) is required"})
	}

	if err := SchedulePost(c.Context(), postID, workspaceID, body.ScheduledAt); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"message": "post scheduled", "scheduled_at": body.ScheduledAt})
}

// Results handles GET /api/posts/in/:id/results
// Returns per-platform publish status for the frontend to display.
// Reads from Redis cache first, falls back to Postgres on cache miss.
// Results godoc
// @Summary GET /api/in/posts/in/:id/results
// @Tags post
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Param attempt_id query string true "Publish attempt ID"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/posts/in/{id}/results [get]
func Results(c *fiber.Ctx) error {
	postID := c.Params("id")
	attemptID := c.Query("attempt_id")

	// Fast path: read from Redis cache
	if attemptID != "" {
		attUUID, err := uuid.Parse(attemptID)
		if err == nil {
			cached := scheduler.ReadCachedResults(postID, attUUID)
			if cached != nil {
				return c.JSON(fiber.Map{"results": cached})
			}
		}
	}

	// Fallback: Postgres
	query := `
		SELECT id::text, platform, status, platform_post_id, platform_post_url,
		       container_id, fail_reason, created_at, updated_at
		FROM publish_results
		WHERE post_id = $1
	`
	args := []any{postID}
	if attemptID != "" {
		query += " AND publish_attempt_id = $2"
		args = append(args, attemptID)
	}
	query += " ORDER BY created_at ASC"

	rows, err := db.DB.QueryContext(c.Context(), query, args...)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	defer rows.Close()

	type resultRow struct {
		ID             string    `json:"id"`
		Platform       string    `json:"platform"`
		Status         string    `json:"status"`
		PlatformPostID string    `json:"platform_post_id"`
		PlatformURL    string    `json:"platform_post_url"`
		ContainerID    string    `json:"container_id"`
		FailReason     string    `json:"fail_reason"`
		CreatedAt      time.Time `json:"created_at"`
		UpdatedAt      time.Time `json:"updated_at"`
	}
	var results []resultRow
	for rows.Next() {
		var r resultRow
		if err := rows.Scan(
			&r.ID, &r.Platform, &r.Status,
			&r.PlatformPostID, &r.PlatformURL,
			&r.ContainerID, &r.FailReason,
			&r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			continue
		}
		results = append(results, r)
	}
	return c.JSON(fiber.Map{"results": results})
}

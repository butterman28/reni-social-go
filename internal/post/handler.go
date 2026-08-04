package post

import (
	"database/sql"
	"time"

	"github.com/gofiber/fiber/v2"

	"reni-social/internal/domain"
	"reni-social/pkg/db"
)

// Create handles POST /api/posts
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
func Publish(c *fiber.Ctx) error {
	postID := c.Params("id")
	workspaceID := c.Query("workspace_id")
	if workspaceID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "workspace_id is required"})
	}

	if err := PublishNow(c.Context(), postID, workspaceID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"message": "post published"})
}

// Schedule handles POST /api/posts/:id/schedule
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
func Results(c *fiber.Ctx) error {
	postID := c.Params("id")

	// 1. Fetch the current attempt ID for this post
	var attemptIDStr sql.NullString
	err := db.DB.QueryRowContext(c.Context(), `SELECT current_publish_attempt_id FROM posts WHERE id=$1`, postID).Scan(&attemptIDStr)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	// 2. Build query to ONLY fetch rows from the current attempt
	query := `
		SELECT id::text, platform, status, platform_post_id, platform_post_url,
		       container_id, fail_reason, created_at, updated_at
		FROM publish_results
		WHERE post_id = $1
	`
	args := []any{postID}

	if attemptIDStr.Valid {
		query += ` AND publish_attempt_id = $2`
		args = append(args, attemptIDStr.String)
	}
	query += ` ORDER BY created_at ASC`

	rows, err := db.DB.QueryContext(c.Context(), query, args...)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	defer rows.Close()

	// ... (keep your existing resultRow struct and scanning loop exactly the same) ...

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

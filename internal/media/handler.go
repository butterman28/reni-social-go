package media

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"reni-social/internal/workspace"
	"reni-social/pkg/config"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// Upload handles POST /api/in/media/upload
func Upload(c *fiber.Ctx) error {
	userID := c.Locals("userid").(string)
	workspaceID := c.Query("workspace_id")
	if workspaceID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "workspace_id is required"})
	}

	// Verify workspace membership
	if _, err := workspace.GetWorkspace(c.Context(), workspaceID, userID); err != nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "access denied to workspace"})
	}

	// Parse file (Fiber's FormFile returns *multipart.FileHeader, error)
	header, err := c.FormFile("file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "no file uploaded or invalid form data"})
	}

	// Open the file to read its contents
	file, err := header.Open()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to open file"})
	}
	defer file.Close()

	// Validate extension
	ext := strings.ToLower(filepath.Ext(header.Filename))
	allowedExts := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
		".mp4": true, ".mov": true, ".m4v": true, ".avi": true, ".webm": true,
	}
	if !allowedExts[ext] {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "file type not allowed"})
	}

	// Save file
	filename := uuid.New().String() + ext
	uploadDir := filepath.Join("uploads", workspaceID)
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create directory"})
	}

	dstPath := filepath.Join(uploadDir, filename)
	dst, err := os.Create(dstPath)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to save file"})
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to write file"})
	}

	// Generate public URL
	publicURL := fmt.Sprintf("%s/media/uploads/%s/%s", config.C.AppUrl, uploadDir, filename)

	// Determine priority from query param (default to normal)
	priorityStr := c.Query("priority", "normal")
	var priority MediaJobPriority
	if priorityStr == "high" {
		priority = PriorityHigh
	} else if priorityStr == "low" {
		priority = PriorityLow
	} else {
		priority = PriorityNormal
	}

	// Parse platforms for fan-out (expects comma-separated string, e.g., "linkedin,meta_ig")
	platformsStr := c.FormValue("platforms")
	selectedPlatforms := strings.Split(platformsStr, ",")
	if len(selectedPlatforms) == 0 || selectedPlatforms[0] == "" {
		selectedPlatforms = []string{"universal"}
	}

	// Fan-out: Create a specific job for each platform
	for _, platform := range selectedPlatforms {
		platform = strings.TrimSpace(platform)
		if platform == "" {
			continue
		}

		// Generate platform-specific output path (e.g., abc123_meta_ig.mp4)
		outputExt := ext
		if strings.HasPrefix(ext, ".mp4") || strings.HasPrefix(ext, ".mov") || strings.HasPrefix(ext, ".m4v") || strings.HasPrefix(ext, ".avi") || strings.HasPrefix(ext, ".webm") {
			outputExt = ".mp4"
		}
		platformOutputPath := strings.Replace(dstPath, ext, fmt.Sprintf("_%s%s", platform, outputExt), 1)

		if strings.HasPrefix(ext, ".mp4") || strings.HasPrefix(ext, ".mov") || strings.HasPrefix(ext, ".m4v") || strings.HasPrefix(ext, ".avi") || strings.HasPrefix(ext, ".webm") {
			EnqueueJob(MediaJob{
				Type:       JobVideoTranscode,
				Platform:   platform,
				InputPath:  dstPath,
				OutputPath: platformOutputPath,
				Priority:   priority,
			})
			EnqueueJob(MediaJob{
				Type:       JobThumbnailGenerate,
				Platform:   platform,
				InputPath:  dstPath,
				OutputPath: strings.Replace(platformOutputPath, outputExt, ".thumb.jpg", 1),
				Priority:   priority,
			})
		} else {
			EnqueueJob(MediaJob{
				Type:       JobImageResize,
				Platform:   platform,
				InputPath:  dstPath,
				OutputPath: platformOutputPath,
				Width:      1080,
				Height:     1080,
				Priority:   priority,
			})
		}
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"url":       publicURL,
		"filename":  filename,
		"size":      header.Size,
		"mime_type": header.Header.Get("Content-Type"),
	})
}
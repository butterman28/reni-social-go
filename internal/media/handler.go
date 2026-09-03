package media

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"reni-social/internal/workspace"
	"reni-social/pkg/config"
	"reni-social/pkg/db" // <-- ADDED
	"strings"

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

	if _, err := workspace.GetWorkspace(c.Context(), workspaceID, userID); err != nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "access denied to workspace"})
	}

	header, err := c.FormFile("file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "no file uploaded or invalid form data"})
	}

	file, err := header.Open()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to open file"})
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	allowedExts := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
		".mp4": true, ".mov": true, ".m4v": true, ".avi": true, ".webm": true,
	}
	if !allowedExts[ext] {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "file type not allowed"})
	}

	filename := uuid.New().String() + ext // Only for the physical file name on disk
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

	publicURL := fmt.Sprintf("%s/media/uploads/%s/%s", config.C.AppUrl, uploadDir, filename)

	priorityStr := c.Query("priority", "normal")
	var priority MediaJobPriority
	if priorityStr == "high" {
		priority = PriorityHigh
	} else if priorityStr == "low" {
		priority = PriorityLow
	} else {
		priority = PriorityNormal
	}

	// 1. STRICT REQUIREMENT: Parse platforms
	platformsStr := c.FormValue("platforms")
	if platformsStr == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "platforms parameter is required"})
	}
	selectedPlatforms := strings.Split(platformsStr, ",")

	// 2. STRICT REQUIREMENT: Frontend MUST provide media_ids
	mediaIDsStr := c.FormValue("media_ids")
	if mediaIDsStr == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "media_ids are strictly required. Frontend must generate and provide UUIDs for media assets.",
		})
	}
	frontendIDs := strings.Split(mediaIDsStr, ",")

	// Ensure we have enough IDs for the requested platforms
	if len(frontendIDs) < len(selectedPlatforms) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "number of media_ids must match or exceed the number of platforms",
		})
	}

	var createdMediaIDs []string

	// 3. Process each platform with the frontend-provided UUID
	for i, platform := range selectedPlatforms {
		platform = strings.TrimSpace(platform)
		if platform == "" {
			continue
		}

		// Parse the frontend-provided UUID (Strict validation)
		assetIDStr := strings.TrimSpace(frontendIDs[i])
		assetID, err := uuid.Parse(assetIDStr)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fmt.Sprintf("invalid media_id format at index %d: %s", i, assetIDStr),
			})
		}
		createdMediaIDs = append(createdMediaIDs, assetID.String())

		// Insert into media_assets immediately with post_id = NULL
		_, err = db.DB.ExecContext(c.Context(), `
			INSERT INTO media_assets (id, post_id, platform, original_url, processed_url, status, created_at)
			VALUES ($1, NULL, $2, $3, NULL, 'processing', NOW())
		`, assetID, platform, publicURL)
		if err != nil {
			log.Printf("❌ [Media] Failed to insert media asset: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to register media asset in database"})
		}

		// Generate platform-specific output path
		outputExt := ext
		if strings.HasPrefix(ext, ".mp4") || strings.HasPrefix(ext, ".mov") || strings.HasPrefix(ext, ".m4v") || strings.HasPrefix(ext, ".avi") || strings.HasPrefix(ext, ".webm") {
			outputExt = ".mp4"
		}
		platformOutputPath := strings.Replace(dstPath, ext, fmt.Sprintf("_%s%s", platform, outputExt), 1)

		// Enqueue Jobs (passing the strict frontend AssetID)
		if strings.HasPrefix(ext, ".mp4") || strings.HasPrefix(ext, ".mov") || strings.HasPrefix(ext, ".m4v") || strings.HasPrefix(ext, ".avi") || strings.HasPrefix(ext, ".webm") {
			EnqueueJob(MediaJob{
				Type:       JobVideoTranscode,
				AssetID:    assetID.String(), // <-- Frontend's UUID
				Platform:   platform,
				InputPath:  dstPath,
				OutputPath: platformOutputPath,
				Priority:   priority,
			})
			EnqueueJob(MediaJob{
				Type:       JobThumbnailGenerate,
				AssetID:    assetID.String(),
				Platform:   platform,
				InputPath:  dstPath,
				OutputPath: strings.Replace(platformOutputPath, outputExt, ".thumb.jpg", 1),
				Priority:   priority,
			})
		} else {
			EnqueueJob(MediaJob{
				Type:       JobImageResize,
				AssetID:    assetID.String(),
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
		"url":         publicURL,
		"media_ids":   createdMediaIDs, // Echo back the validated IDs
		"filename":    filename,
		"size":        header.Size,
		"mime_type":   header.Header.Get("Content-Type"),
	})
}
package media

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"reni-social/internal/utils"
	"reni-social/pkg/config"
	"reni-social/pkg/db" // <-- ADDED
)

type JobType string

const (
	JobVideoTranscode    JobType = "video_transcode"
	JobThumbnailGenerate JobType = "thumbnail_generate"
	JobImageResize       JobType = "image_resize"
)

type MediaJobPriority string

const (
	PriorityHigh   MediaJobPriority = "high"
	PriorityNormal MediaJobPriority = "normal"
	PriorityLow    MediaJobPriority = "low"
)

const (
	QueueHigh   = "social:media_jobs_high"
	QueueNormal = "social:media_jobs_normal"
	QueueLow    = "social:media_jobs_low"
)

type MediaJob struct {
	Type       JobType          `json:"type"`
	AssetID    string           `json:"asset_id"` // 🆕 ADDED
	PostID     string           `json:"post_id"`
	Platform   string           `json:"platform"`
	InputPath  string           `json:"input_path"`
	OutputPath string           `json:"output_path"`
	Width      int              `json:"width,omitempty"`
	Height     int              `json:"height,omitempty"`
	Priority   MediaJobPriority `json:"priority"`
}

func EnqueueJob(job MediaJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("media: marshal job: %w", err)
	}
	var queueKey string
	switch job.Priority {
	case PriorityHigh:
		queueKey = QueueHigh
	case PriorityLow:
		queueKey = QueueLow
	default:
		queueKey = QueueNormal
	}
	return utils.RDB.LPush(utils.Ctx, queueKey, string(data)).Err()
}

func StartMediaWorker(db *sql.DB) {
	log.Println("🎬 [Media] Started — listening on priority queues (High > Normal > Low)")
	for {
		result, err := utils.RDB.BRPop(utils.Ctx, 5*time.Second, QueueHigh, QueueNormal, QueueLow).Result()
		if err != nil {
			continue
		}
		payload := result[1]
		var job MediaJob
		if err := json.Unmarshal([]byte(payload), &job); err != nil {
			log.Printf("  [Media] malformed job payload: %v", err)
			continue
		}
		go processJob(job)
	}
}

func processJob(job MediaJob) {
	log.Printf("🎬 [Media] Processing %s for asset %s", job.Type, job.AssetID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var err error
	switch job.Type {
	case JobVideoTranscode:
		err = transcodeVideo(ctx, job.InputPath, job.OutputPath, job.Platform)
	case JobThumbnailGenerate:
		err = generateThumbnail(ctx, job.InputPath, job.OutputPath)
	case JobImageResize:
		err = resizeImage(ctx, job.InputPath, job.OutputPath, job.Width, job.Height, job.Platform)
	default:
		log.Printf("  [Media] unknown job type: %s", job.Type)
		return
	}

	if err != nil {
		log.Printf("❌ [Media] job failed for asset %s: %v", job.AssetID, err)
		// 🆕 Mark as failed in DB
		if job.AssetID != "" {
			_, _ = db.DB.ExecContext(ctx, `UPDATE media_assets SET status = 'failed' WHERE id = $1`, job.AssetID)
		}
		return
	}

	log.Printf("✅ [Media] %s complete for asset %s → %s", job.Type, job.AssetID, job.OutputPath)

	// 🆕 Update DB with processed URL (only for main transcode/resize, not thumbnails)
	if job.AssetID != "" && (job.Type == JobVideoTranscode || job.Type == JobImageResize) {
		// Convert local path (uploads/ws-123/file.mp4) to public URL
		processedURL := fmt.Sprintf("%s/media/%s", config.C.AppUrl, job.OutputPath)
		
		_, err = db.DB.ExecContext(ctx, `
			UPDATE media_assets 
			SET processed_url = $1, status = 'completed' 
			WHERE id = $2
		`, processedURL, job.AssetID)
		if err != nil {
			log.Printf("❌ [Media] failed to update media_assets for %s: %v", job.AssetID, err)
		}
	}
}

// --- FFmpeg processors remain exactly the same ---
func transcodeVideo(ctx context.Context, input, output, platform string) error {
	if err := ensureDir(output); err != nil {
		return err
	}
	var scaleFilter, vBitrate, aBitrate string
	switch platform {
	case "meta_ig", "tiktok":
		scaleFilter = "scale=1080:1920:force_original_aspect_ratio=decrease,pad=1080:1920:(ow-iw)/2:(oh-ih)/2:black"
		vBitrate = "3000k"
		aBitrate = "128k"
	case "linkedin", "x", "meta_fb":
		scaleFilter = "scale=1920:1080:force_original_aspect_ratio=decrease,pad=1920:1080:(ow-iw)/2:(oh-ih)/2:black"
		vBitrate = "2000k"
		aBitrate = "128k"
	default:
		scaleFilter = "scale=1080:-1:force_original_aspect_ratio=decrease"
		vBitrate = "2000k"
		aBitrate = "128k"
	}
	cmd := exec.CommandContext(ctx, config.C.FFmpegPath, "-y", "-i", input, "-vcodec", "libx264", "-acodec", "aac", "-b:v", vBitrate, "-b:a", aBitrate, "-vf", scaleFilter, "-movflags", "+faststart", output)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func generateThumbnail(ctx context.Context, input, output string) error {
	if err := ensureDir(output); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, config.C.FFmpegPath, "-y", "-i", input, "-ss", "00:00:01", "-frames:v", "1", "-q:v", "2", output)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func resizeImage(ctx context.Context, input, output string, width, height int, platform string) error {
	if err := ensureDir(output); err != nil {
		return err
	}
	var scaleFilter string
	switch platform {
	case "meta_ig", "tiktok":
		scaleFilter = "scale=1080:1920:force_original_aspect_ratio=decrease,pad=1080:1920:(ow-iw)/2:(oh-ih)/2:black"
	case "linkedin", "x", "meta_fb":
		scaleFilter = "scale=1920:1080:force_original_aspect_ratio=decrease,pad=1920:1080:(ow-iw)/2:(oh-ih)/2:black"
	default:
		scaleFilter = fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease", width, height)
		if width == 0 || height == 0 {
			scaleFilter = "scale=1080:-1"
		}
	}
	cmd := exec.CommandContext(ctx, config.C.FFmpegPath, "-y", "-i", input, "-vf", scaleFilter, "-q:v", "2", output)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func ensureDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0755)
}
var _ = time.Now

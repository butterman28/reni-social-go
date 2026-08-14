// Package media implements the FFmpeg-based media processing worker.
//
// Jobs are pushed to the Redis queue (config.C.MediaWorkerQueue) as JSON strings.
// The worker blocks on BRPOP and processes each job in a goroutine.
//
// Supported job types:
//   - video_transcode    : re-encode to H.264/AAC MP4
//   - thumbnail_generate : extract first frame as JPEG
//   - image_resize       : resize/compress for platform requirements
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
)

// JobType identifies the kind of media processing to perform.
type JobType string

const (
	JobVideoTranscode    JobType = "video_transcode"
	JobThumbnailGenerate JobType = "thumbnail_generate"
	JobImageResize       JobType = "image_resize"
)

// MediaJob is the payload pushed to the Redis queue.
type MediaJob struct {
	Type       JobType `json:"type"`
	PostID     string  `json:"post_id"`
	InputPath  string  `json:"input_path"`  // local filesystem path after download
	OutputPath string  `json:"output_path"` // where to write the processed file
	Width      int     `json:"width,omitempty"`
	Height     int     `json:"height,omitempty"`
}

// EnqueueJob pushes a media job onto the Redis queue.
// EnqueueJob godoc
// @Summary EnqueueJob
// @Description Auto-generated thorough documentation for EnqueueJob
// @Tags media
// @Produce json
// @Param job body string true "job"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func EnqueueJob(job MediaJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("media: marshal job: %w", err)
	}
	return utils.RDB.LPush(utils.Ctx, config.C.MediaWorkerQueue, string(data)).Err()
}

// StartMediaWorker is the entry point for the media processing goroutine.
// Designed to run under the supervisor in cmd/worker/main.go.
// The db parameter is kept for future use (e.g., updating post media URLs after processing).
// StartMediaWorker godoc
// @Summary StartMediaWorker
// @Description Auto-generated thorough documentation for StartMediaWorker
// @Tags media
// @Produce json
// @Param db body object true "db"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
func StartMediaWorker(db *sql.DB) {
	log.Printf(" [Media] Started — listening on queue: %s", config.C.MediaWorkerQueue)

	for {
		result, err := utils.RDB.BRPop(utils.Ctx, 5*time.Second, config.C.MediaWorkerQueue).Result()
		if err != nil {
			// Timeout is normal — loop back
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

// processJob dispatches to the correct processor and logs the result.
// processJob godoc
// @Summary processJob
// @Description Auto-generated thorough documentation for processJob
// @Tags media
// @Produce json
// @Param job body string true "job"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
func processJob(job MediaJob) {
	log.Printf(" [Media] Processing %s for post %s", job.Type, job.PostID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var err error
	switch job.Type {
	case JobVideoTranscode:
		err = transcodeVideo(ctx, job.InputPath, job.OutputPath)
	case JobThumbnailGenerate:
		err = generateThumbnail(ctx, job.InputPath, job.OutputPath)
	case JobImageResize:
		err = resizeImage(ctx, job.InputPath, job.OutputPath, job.Width, job.Height)
	default:
		log.Printf("  [Media] unknown job type: %s", job.Type)
		return
	}

	if err != nil {
		log.Printf(" [Media] job failed for post %s: %v", job.PostID, err)
		return
	}

	log.Printf(" [Media] %s complete for post %s → %s", job.Type, job.PostID, job.OutputPath)
}

// ──────────────────────────────────────────────────────────────────────────────
// FFmpeg processors
// ──────────────────────────────────────────────────────────────────────────────

// transcodeVideo re-encodes video to H.264 + AAC MP4.
// Output is suitable for LinkedIn, Meta, and X upload requirements.
// transcodeVideo godoc
// @Summary transcodeVideo
// @Description Auto-generated thorough documentation for transcodeVideo
// @Tags media
// @Produce json
// @Param output body string true "output"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func transcodeVideo(ctx context.Context, input, output string) error {
	if err := ensureDir(output); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx,
		config.C.FFmpegPath,
		"-y",                  // overwrite output
		"-i", input,           // input file
		"-vcodec", "libx264",  // H.264 video codec
		"-acodec", "aac",      // AAC audio codec
		"-b:v", "2000k",       // video bitrate
		"-b:a", "128k",        // audio bitrate
		"-movflags", "+faststart", // optimize for streaming
		output,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// generateThumbnail extracts the first frame of a video as a JPEG.
// generateThumbnail godoc
// @Summary generateThumbnail
// @Description Auto-generated thorough documentation for generateThumbnail
// @Tags media
// @Produce json
// @Param output body string true "output"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func generateThumbnail(ctx context.Context, input, output string) error {
	if err := ensureDir(output); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx,
		config.C.FFmpegPath,
		"-y",
		"-i", input,
		"-ss", "00:00:01",   // seek to 1 second
		"-frames:v", "1",    // extract 1 frame
		"-q:v", "2",         // JPEG quality (1=best, 31=worst)
		output,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// resizeImage resizes/compresses an image using FFmpeg's scale filter.
// resizeImage godoc
// @Summary resizeImage
// @Description Auto-generated thorough documentation for resizeImage
// @Tags media
// @Produce json
// @Param output body string true "output"
// @Param height body integer true "height"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func resizeImage(ctx context.Context, input, output string, width, height int) error {
	if err := ensureDir(output); err != nil {
		return err
	}

	scaleFilter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease", width, height)
	if width == 0 || height == 0 {
		scaleFilter = "scale=1080:-1" // default: 1080px wide, maintain aspect
	}

	cmd := exec.CommandContext(ctx,
		config.C.FFmpegPath,
		"-y",
		"-i", input,
		"-vf", scaleFilter,
		"-q:v", "2",
		output,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ensureDir godoc
// @Summary ensureDir
// @Description Auto-generated thorough documentation for ensureDir
// @Tags media
// @Produce json
// @Param path body string true "path"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func ensureDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0755)
}

// Blank import to keep db import used without forcing a DB call in this file.
var _ = time.Now

// Package notify implements a Redis-queued dispatcher for outbound email
// and SMS. Handlers enqueue a job and return immediately instead of
// blocking on the upstream Reni mail/SMS API's latency (up to 30s per call
// via utils.DoRequest) — that wait now happens in a background worker
// goroutine instead of a request-serving Fiber goroutine.
package notify

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"reni-social/internal/utils"
	"reni-social/pkg/config"
)

// Channel identifies which notification API to hit.
type Channel string

const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
)

// Job is the payload pushed to the Redis queue.
type Job struct {
	Channel Channel `json:"channel"`
	To      string  `json:"to"`
	Subject string  `json:"subject,omitempty"` // email only
	Body    string  `json:"body"`
}

// EnqueueEmail queues an email send. Returns immediately — actual delivery
// happens asynchronously in StartNotifyWorker.
// EnqueueEmail godoc
// @Summary EnqueueEmail
// @Description Auto-generated thorough documentation for EnqueueEmail
// @Tags notify
// @Produce json
// @Param body body string true "body"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func EnqueueEmail(to, subject, body string) error {
	return enqueue(Job{Channel: ChannelEmail, To: to, Subject: subject, Body: body})
}

// EnqueueSMS queues an SMS send. Returns immediately — actual delivery
// happens asynchronously in StartNotifyWorker.
// EnqueueSMS godoc
// @Summary EnqueueSMS
// @Description Auto-generated thorough documentation for EnqueueSMS
// @Tags notify
// @Produce json
// @Param body body string true "body"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func EnqueueSMS(to, body string) error {
	return enqueue(Job{Channel: ChannelSMS, To: to, Body: body})
}

// enqueue godoc
// @Summary enqueue
// @Description Auto-generated thorough documentation for enqueue
// @Tags notify
// @Produce json
// @Param job body string true "job"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func enqueue(job Job) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("notify: marshal job: %w", err)
	}
	return utils.RDB.LPush(utils.Ctx, config.C.NotifyQueue, string(data)).Err()
}

// StartNotifyWorker is the entry point for the notification-dispatch
// goroutine. Designed to run under the supervisor in cmd/worker/main.go.
// db is unused but accepted to match the supervise(func(*sql.DB)) signature
// shared by the other workers.
// StartNotifyWorker godoc
// @Summary StartNotifyWorker
// @Description Auto-generated thorough documentation for StartNotifyWorker
// @Tags notify
// @Produce json
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
func StartNotifyWorker() {
	log.Printf("  [Notify] Started — listening on queue: %s", config.C.NotifyQueue)

	for {
		result, err := utils.RDB.BRPop(utils.Ctx, 5*time.Second, config.C.NotifyQueue).Result()
		if err != nil {
			// Timeout is normal — loop back
			continue
		}

		payload := result[1]
		var job Job
		if err := json.Unmarshal([]byte(payload), &job); err != nil {
			log.Printf("  [Notify] malformed job payload: %v", err)
			continue
		}

		go dispatch(job)
	}
}

// dispatch godoc
// @Summary dispatch
// @Description Auto-generated thorough documentation for dispatch
// @Tags notify
// @Produce json
// @Param job body string true "job"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
func dispatch(job Job) {
	var err error
	switch job.Channel {
	case ChannelEmail:
		err = utils.SendEmail(job.To, job.Subject, job.Body)
	case ChannelSMS:
		err = utils.SendSMS(job.To, job.Body)
	default:
		log.Printf("  [Notify] unknown channel: %s", job.Channel)
		return
	}

	if err != nil {
		// No retry/DLQ yet — logged so it's visible, but not re-enqueued.
		// OTP-style messages go stale fast, so a blind retry loop risks
		// spamming an already-failing upstream more than it helps. Worth
		// revisiting with a bounded retry + dead-letter queue if delivery
		// failures turn out to be common in practice.
		log.Printf(" [Notify] %s send to %s failed: %v", job.Channel, job.To, err)
		return
	}
	log.Printf(" [Notify] %s sent to %s", job.Channel, job.To)
}
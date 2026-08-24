package utils

import (
	"fmt"
	"time"

	"reni-social/pkg/config"
)

// emailPayload is the request body for the Reni mail API.
type emailPayload struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Sender  string `json:"sender"`
}

// reniMailRes is the response from the Reni mail API.
type reniMailRes struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
}

// SendEmail sends a transactional email via the configured Reni mail API.
// SendEmail godoc
// @Summary SendEmail
// @Description Auto-generated thorough documentation for SendEmail
// @Tags utils
// @Produce json
// @Param body body string true "body"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func SendEmail(to, subject, body string) error {
	var res reniMailRes

	err := DoRequest(RequestOptions{
		Method: "POST",
		URL:    config.C.ReniApiBaseUrl + "/v1/mail/ra/api/email/send",
		Body: emailPayload{
			To:      to,
			Subject: subject,
			Body:    body,
			Sender:  "Reni Social",
		},
		Headers: map[string]string{
			"Authorization": "Bearer " + config.C.ReniApiKey,
		},
		Timeout: 30 * time.Second,
	}, &res)

	if err != nil {
		return err
	}

	if !res.Status {
		return fmt.Errorf("email sending failed: %s", res.Message)
	}

	return nil
}

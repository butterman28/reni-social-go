package utils

import (
	"fmt"
	"time"

	"reni-social/pkg/config"
)

// smsPayload is the request body for the Reni SMS API.
type smsPayload struct {
	To     string `json:"to"`
	Body   string `json:"body"`
	Sender string `json:"sender"`
}

// SendSMS sends an SMS via the configured Reni SMS gateway API.
// SendSMS godoc
// @Summary SendSMS
// @Description Auto-generated thorough documentation for SendSMS
// @Tags utils
// @Produce json
// @Param message body string true "message"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func SendSMS(to, message string) error {
	var res reniMailRes // reuses the status/message response shape from email.go

	err := DoRequest(RequestOptions{
		Method: "POST",
		URL:    config.C.ReniApiBaseUrl + "/v1/mail/ra/api/sms/send",
		Body: smsPayload{
			To:     to,
			Body:   message,
			Sender: "Reni Social",
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
		return fmt.Errorf("sms sending failed: %s", res.Message)
	}

	return nil
}

package utils

import (
	"dogo/pkg/config"
	"dogo/internal/types"
	"fmt"
	"time"
)

func SendEmail(to string, subject string, body string) error {
	var res types.ReniMailRes
	var payload types.EmailPayload

	payload.To = to
	payload.Subject = subject
	payload.Body = body
	payload.Sender = "Biki"

	err := DoRequest(types.RequestOptions{
		Method: "POST",
		URL:    config.C.ReniApiBaseUrl + "/v1/mail/ra/api/email/send",
		Body:   payload,
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

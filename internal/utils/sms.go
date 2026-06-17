package utils

import (
	"dogo/internal/types"
	"dogo/pkg/config"
	"fmt"
	"time"
)

// SendSMS sends an SMS via the Reni SMS gateway API.
func SendSMS(to, message string) error {
	var res types.ReniMailRes
	var payload types.SMSPayload

	payload.To = to
	payload.Body = message
	payload.Sender = "Biki"

	err := DoRequest(types.RequestOptions{
		Method: "POST",
		URL:    config.C.ReniApiBaseUrl + "/v1/mail/ra/api/sms/send",
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
		return fmt.Errorf("sms sending failed: %s", res.Message)
	}

	return nil
}

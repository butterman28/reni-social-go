package notify

import (
	"context"
	"fmt"

	"reni-social/pkg/config"
	"reni-social/pkg/linkedin"
)

// LinkedInExpiryNotifier emails the workspace owner when a LinkedIn
// connection is flagged for re-authentication. Implements linkedin.Notifier.
type LinkedInExpiryNotifier struct{}

var _ linkedin.Notifier = LinkedInExpiryNotifier{}

// NotifyReauthRequired godoc
// @Summary NotifyReauthRequired
// @Description Auto-generated thorough documentation for NotifyReauthRequired
// @Tags notify
// @Produce json
// @Param conn body string true "conn"
// @Param reason body string true "reason"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func (LinkedInExpiryNotifier) NotifyReauthRequired(_ context.Context, conn linkedin.Connection, reason string) error {
	if conn.OwnerEmail == "" {
		// No email on record — the needs_reauth DB flag still surfaces in
		// the connections endpoint so the UI can show the banner anyway.
		return nil
	}

	account := conn.AccountName
	if account == "" {
		account = "your LinkedIn account"
	}

	settingsURL := config.C.AppUrl
	if conn.WorkspaceSlug != "" {
		settingsURL = settingsURL + "/" + conn.WorkspaceSlug + "/settings?tab=accounts"
	}

	subject := "Action required: reconnect your LinkedIn account on Reni Social"
	body := fmt.Sprintf(
		"Hi there,\n\n"+
			"%s connected to your Reni Social workspace has expired or is expiring soon.\n\n"+
			"%s\n\n"+
			"LinkedIn access tokens cannot be refreshed automatically, so to keep publishing "+
			"without interruption, please reconnect LinkedIn from your workspace settings:\n\n"+
			"%s\n\n"+
			"— The Reni Social team",
		account, reason, settingsURL,
	)

	return EnqueueEmail(conn.OwnerEmail, subject, body)
}
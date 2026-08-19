package notify

import (
	"context"
	"fmt"

	"reni-social/pkg/config"
	"reni-social/pkg/meta"
)

// MetaReauthNotifier emails the workspace owner when a Meta (Instagram)
// connection is flagged for re-authentication. Implements meta.Notifier.
type MetaReauthNotifier struct{}

var _ meta.Notifier = MetaReauthNotifier{}

// NotifyReauthRequired godoc
// @Summary NotifyReauthRequired
// @Description Auto-generated documentation for NotifyReauthRequired
// @Tags notify
// @Produce json
// @Param conn body string true "conn"
// @Param reason body string true "reason"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} types.APIResponse "Bad Request"
// @Failure 401 {object} types.APIResponse "Unauthorized"
// @Failure 403 {object} types.APIResponse "Forbidden"
// @Failure 404 {object} types.APIResponse "Not Found"
// @Failure 500 {object} types.APIResponse "Internal Server Error"
func (MetaReauthNotifier) NotifyReauthRequired(_ context.Context, conn meta.MetaConnection, reason string) error {
	if conn.OwnerEmail == "" {
		return nil
	}

	account := conn.AccountName
	if account == "" {
		account = "your Instagram account"
	}

	settingsURL := config.C.AppUrl
	if conn.WorkspaceSlug != "" {
		settingsURL = settingsURL + "/" + conn.WorkspaceSlug + "/settings?tab=accounts&reconnect=meta"
	}

	subject := "Action required: reconnect your Instagram account on Reni Social"

	body := fmt.Sprintf(
		"Hi there,\n\n"+
			"%s connected to your Reni Social workspace has been disconnected or revoked.\n\n"+
			"%s\n\n"+
			"Instagram access tokens cannot be recovered once revoked, so to keep publishing "+
			"without interruption, please reconnect Instagram from your workspace settings:\n\n"+
			"%s\n\n"+
			"— The Reni Social team",
		account, reason, settingsURL,
	)

	return EnqueueEmail(conn.OwnerEmail, subject, body)
}
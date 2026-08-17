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
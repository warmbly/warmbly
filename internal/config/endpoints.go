package config

import (
	"net/url"
	"os"
	"strings"
)

// AppBaseURL is the dashboard origin every emailed link is built from.
//
// This used to be a hardcoded https://app.warmbly.com, which meant a
// self-hosted deployment mailed its users a reset link pointing at someone
// else's dashboard, carrying a live reset token signed with the self-host's own
// AUTH_SECRET. APP_URL is the documented variable; FRONTEND_BASE_URL is the
// older name and stays supported so existing deployments keep working.
func AppBaseURL() string {
	for _, key := range []string{"APP_URL", "FRONTEND_BASE_URL"} {
		if v := strings.TrimRight(strings.TrimSpace(os.Getenv(key)), "/"); v != "" {
			return v
		}
	}
	return "https://app.warmbly.com"
}

func GetPasswordResetURL(sessionToken string) string {
	return AppBaseURL() + "/auth/reset-password/confirm?session=" + url.QueryEscape(sessionToken)
}

// GetInviteURL is the team-invitation accept link. Same reasoning as the reset
// URL: the dashboard's own copy-link button already used the browser origin, so
// only the emailed variant was broken on self-host.
func GetInviteURL(token string) string {
	return AppBaseURL() + "/invite?token=" + url.QueryEscape(token)
}

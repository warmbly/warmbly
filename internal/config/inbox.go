package config

import (
	"os"
	"strconv"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
)

type Oauth2Inbox struct {
	Google  *oauth2.Config
	Outlook *oauth2.Config
}

// GoogleOAuthConnect reports whether a NEW Gmail mailbox may be connected with
// Google sign-in. Configured clients default on; an explicit flag overrides it.
// Existing mailboxes can still be re-authorized regardless of this gate.
func GoogleOAuthConnect() bool {
	if strings.TrimSpace(os.Getenv("BOX_GOOGLE_CLIENT_ID")) == "" || strings.TrimSpace(os.Getenv("BOX_GOOGLE_CLIENT_SECRET")) == "" {
		return false
	}
	value := strings.TrimSpace(os.Getenv("BOX_GOOGLE_OAUTH_CONNECT"))
	if value == "" {
		return true
	}
	b, err := strconv.ParseBool(value)
	return err == nil && b
}

// GoogleOAuthAppName is the Google client's app name as its consent screen shows it, which an inbox vendor checks.
func GoogleOAuthAppName() string {
	if v := strings.TrimSpace(os.Getenv("BOX_GOOGLE_APP_NAME")); v != "" {
		return v
	}
	return "Warmbly"
}

func GoogleOauth2Inbox(baseURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     os.Getenv("BOX_GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("BOX_GOOGLE_CLIENT_SECRET"),
		RedirectURL:  baseURL + "/addresses/google/callback",
		// Full mail access lets warmup engagement run over IMAP and confers all gmail.modify did; settings.basic reads send-as aliases.
		Scopes: []string{
			gmail.MailGoogleComScope,
			gmail.GmailSettingsBasicScope,
		},
		Endpoint: google.Endpoint,
	}
}

// OutlookOauth2Inbox configures delegated Microsoft Graph access for Outlook /
// Microsoft 365 mailboxes. Graph is the transport now (RAW MIME sendMail + delta
// sync), so we request Graph scopes rather than the legacy IMAP/SMTP scopes:
// Mail.Send (send), Mail.ReadWrite (delta sync + warmup move/mark/flag),
// User.Read (resolve the mailbox owner via /me), and offline_access (refresh
// token). None require tenant admin consent by default and all work on personal
// Outlook.com accounts.
func OutlookOauth2Inbox(baseURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     os.Getenv("BOX_OUTLOOK_CLIENT_ID"),
		ClientSecret: os.Getenv("BOX_OUTLOOK_CLIENT_SECRET"),
		RedirectURL:  baseURL + "/addresses/outlook/callback",
		Scopes: []string{
			"openid",
			"email",
			"profile",
			"offline_access",
			"https://graph.microsoft.com/User.Read",
			"https://graph.microsoft.com/Mail.Send",
			"https://graph.microsoft.com/Mail.ReadWrite",
		},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
			TokenURL: "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		},
	}
}

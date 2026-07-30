package config

import (
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
)

type Oauth2Inbox struct {
	Google         *oauth2.Config
	Outlook        *oauth2.Config
	OutlookAppOnly *clientcredentials.Config
}

func GoogleOauth2Inbox(baseURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     os.Getenv("BOX_GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("BOX_GOOGLE_CLIENT_SECRET"),
		RedirectURL:  baseURL + "/addresses/google/callback",
		Scopes: []string{
			gmail.GmailComposeScope,
			gmail.GmailMetadataScope,
			gmail.GmailModifyScope,
			gmail.GmailSendScope,
			gmail.GmailSettingsBasicScope,
			gmail.GmailReadonlyScope,
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

// OutlookAppOnlyInbox configures Microsoft Graph application-permission access.
// It is used only for tenant-owned Microsoft 365 mailboxes that have passed the
// human approval/admin-consent gate. The worker targets a specific mailbox with
// /users/{mailbox}; the token itself is tenant/app scoped.
func OutlookAppOnlyInbox() *clientcredentials.Config {
	tenantID := os.Getenv("BOX_OUTLOOK_TENANT_ID")
	if tenantID == "" {
		tenantID = os.Getenv("MICROSOFT_TENANT_ID")
	}
	if tenantID == "" {
		tenantID = "common"
	}
	return &clientcredentials.Config{
		ClientID:     os.Getenv("BOX_OUTLOOK_CLIENT_ID"),
		ClientSecret: os.Getenv("BOX_OUTLOOK_CLIENT_SECRET"),
		TokenURL:     "https://login.microsoftonline.com/" + tenantID + "/oauth2/v2.0/token",
		Scopes:       []string{"https://graph.microsoft.com/.default"},
	}
}

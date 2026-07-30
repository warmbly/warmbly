package config

import (
	"strings"
	"testing"
)

func TestOutlookAppOnlyInboxUsesTenantAndDefaultGraphScope(t *testing.T) {
	t.Setenv("BOX_OUTLOOK_TENANT_ID", "tenant-123")
	t.Setenv("MICROSOFT_TENANT_ID", "")
	t.Setenv("BOX_OUTLOOK_CLIENT_ID", "client-id")
	t.Setenv("BOX_OUTLOOK_CLIENT_SECRET", "client-secret")

	cfg := OutlookAppOnlyInbox()
	if cfg.ClientID != "client-id" || cfg.ClientSecret != "client-secret" {
		t.Fatalf("client credentials not loaded")
	}
	if !strings.Contains(cfg.TokenURL, "/tenant-123/oauth2/v2.0/token") {
		t.Fatalf("token URL = %q, want tenant-specific v2 token endpoint", cfg.TokenURL)
	}
	if len(cfg.Scopes) != 1 || cfg.Scopes[0] != "https://graph.microsoft.com/.default" {
		t.Fatalf("scopes = %#v, want Graph .default", cfg.Scopes)
	}
}

func TestOutlookAppOnlyInboxFallsBackToMicrosoftTenantID(t *testing.T) {
	t.Setenv("BOX_OUTLOOK_TENANT_ID", "")
	t.Setenv("MICROSOFT_TENANT_ID", "fallback-tenant")

	cfg := OutlookAppOnlyInbox()
	if !strings.Contains(cfg.TokenURL, "/fallback-tenant/oauth2/v2.0/token") {
		t.Fatalf("token URL = %q, want MICROSOFT_TENANT_ID fallback", cfg.TokenURL)
	}
}

package config

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestDashboardEmailLinks(t *testing.T) {
	t.Setenv("APP_URL", "https://app.warmbly.com")
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("CORS_ALLOW_ORIGINS", "https://app.warmbly.com,https://second.example.com")
	for _, tt := range []struct{ origin, want string }{
		{"https://second.example.com", "https://second.example.com"},
		{"https://app.warmbly.com", "https://app.warmbly.com"},
		{"", "https://app.warmbly.com"},
		{"https://evil.example.com", "https://app.warmbly.com"},
	} {
		ctx := WithDashboardOrigin(context.Background(), tt.origin)
		if got := DashboardBaseURL(ctx); got != tt.want {
			t.Errorf("dashboard base for %q = %q", tt.origin, got)
		}
		for _, got := range []string{GetPasswordResetURL("token&next=evil", DashboardOriginFromContext(ctx)), GetInviteURL("token&next=evil", DashboardOriginFromContext(ctx))} {
			if !strings.HasPrefix(got, tt.want+"/") || !strings.HasSuffix(got, "token%26next%3Devil") {
				t.Errorf("email URL for %q = %q", tt.origin, got)
			}
		}
	}
}

// The reset token travels in the fragment, so neither the request line nor a Referer carries it.
func TestPasswordResetLinkKeepsTheTokenOutOfTheQuery(t *testing.T) {
	t.Setenv("APP_URL", "https://app.warmbly.com")
	got := GetPasswordResetURL("tok.en", "")
	if got != "https://app.warmbly.com/auth/reset-password/confirm#session=tok.en" || strings.Contains(got, "?") {
		t.Fatalf("reset link = %q", got)
	}
}

func TestPrimaryDashboardOriginAndRevalidation(t *testing.T) {
	t.Setenv("APP_URL", "https://app.warmbly.com/")
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("CORS_ALLOW_ORIGINS", "https://second.example.com")
	if got := PrimaryDashboardOrigin(); got != "https://app.warmbly.com" {
		t.Fatalf("primary origin = %q", got)
	}
	ctx := WithDashboardOrigin(context.Background(), "https://second.example.com")
	t.Setenv("CORS_ALLOW_ORIGINS", "*")
	if DashboardOriginFromContext(ctx) != "" || DashboardBaseURL(ctx) != "https://app.warmbly.com" {
		t.Fatal("removed origin must not remain trusted")
	}
}

func TestSharedBackendDashboardOrigins(t *testing.T) {
	t.Setenv("APP_URL", "https://app.warmbly.com")
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("WEBSOCKET_URL", "wss://rt.example.com/socket/websocket")
	t.Setenv("CORS_ALLOW_ORIGINS", "https://app.warmbly.com, https://admin.example.com,https://second.example.com")
	cfg, err := (&Config{Env: "prod"}).LoadApiConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://app.warmbly.com", "https://admin.example.com", "https://second.example.com"}
	if !slices.Equal(cfg.AllowedOrigins, want) {
		t.Fatalf("API origins = %v, want %v", cfg.AllowedOrigins, want)
	}
	for _, origin := range want {
		if got := DashboardOrigin(origin); got != origin {
			t.Errorf("OAuth return origin %q = %q", origin, got)
		}
	}
	t.Setenv("CORS_ALLOW_ORIGINS", "https://app.warmbly.com,https://admin.example.com")
	if got := DashboardOrigin("https://second.example.com"); got != "" {
		t.Fatal("a second dashboard origin must be explicitly configured, not hard-coded")
	}
}

func TestDashboardOrigin(t *testing.T) {
	t.Setenv("APP_URL", "https://app.example.com")
	t.Setenv("APP_ORIGIN", "https://primary.example.com")
	t.Setenv("CORS_ALLOW_ORIGINS", "https://app.example.com, https://second.example.com,http://localhost:5173")
	for _, value := range []string{"https://app.example.com", "https://primary.example.com", "https://second.example.com", "http://localhost:5173"} {
		if got := DashboardOrigin(value); got != value {
			t.Errorf("DashboardOrigin(%q) = %q", value, got)
		}
	}
	for _, value := range []string{"", "null", "*", "https://evil.example.com", "https://second.example.com.evil.test", "http://second.example.com", "https://second.example.com:444", "https://second.example.com/", "https://second.example.com/redirect", "https://second.example.com?x=1", "https://second.example.com?", "https://second.example.com#fragment", "https://user@second.example.com", "javascript:alert(1)"} {
		if got := DashboardOrigin(value); got != "" {
			t.Errorf("DashboardOrigin(%q) = %q, want empty", value, got)
		}
	}
	t.Setenv("CORS_ALLOW_ORIGINS", "*")
	if got := DashboardOrigin("https://evil.example.com"); got != "" {
		t.Fatal("wildcard must not allow an OAuth return origin")
	}
}

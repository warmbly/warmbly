package handler

import (
	"context"
	"testing"

	"github.com/warmbly/warmbly/internal/config"
)

func TestBillingReturnURLUsesInitiatingDashboard(t *testing.T) {
	t.Setenv("APP_URL", "https://app.warmbly.com")
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("CORS_ALLOW_ORIGINS", "https://second.example.com")
	const second = "https://second.example.com"
	const fallback = "/app/settings/billing"
	for _, tt := range []struct{ origin, raw, want string }{
		{second, second + "/app/settings/billing/ai-credits?topup=success", second + "/app/settings/billing/ai-credits?topup=success"},
		{second, "/app/settings/billing?checkout=done", second + "/app/settings/billing?checkout=done"},
		{second, "", second + fallback},
		{second, "https://app.warmbly.com" + fallback, second + fallback},
		{"", "https://app.warmbly.com" + fallback, "https://app.warmbly.com" + fallback},
		{"", second + fallback, "https://app.warmbly.com" + fallback},
		{"https://evil.example.com", "/app/settings/billing", "https://app.warmbly.com" + fallback},
	} {
		ctx := config.WithDashboardOrigin(context.Background(), tt.origin)
		if got := billingReturnURL(ctx, tt.raw, fallback); got != tt.want {
			t.Errorf("origin %q URL %q = %q, want %q", tt.origin, tt.raw, got, tt.want)
		}
	}
	ctx := config.WithDashboardOrigin(context.Background(), second)
	for _, raw := range []string{
		"https://evil.example.com/", "//evil.example.com", "/\\evil.example.com", "https://second.example.com.evil.example.com/",
		"https://evil@second.example.com/", "http://second.example.com/", "https://second.example.com:444/", "javascript:alert(1)", "/path\nother", "/%zz",
	} {
		if got := billingReturnURL(ctx, raw, fallback); got != second+fallback {
			t.Errorf("unsafe URL %q returned %q", raw, got)
		}
	}
}

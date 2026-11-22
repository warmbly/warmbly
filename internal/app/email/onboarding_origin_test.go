package email

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func TestOAuthStartRejectsUntrustedReturnOrigin(t *testing.T) {
	t.Setenv("APP_URL", "https://app.example.com")
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("CORS_ALLOW_ORIGINS", "https://second.example.com")
	svc := &emailService{}
	org := uuid.New()
	for _, tt := range []struct {
		origin string
		web    bool
	}{
		{"https://evil.example.com", true},
		{"https://second.example.com/path", true},
		{"https://second.example.com", false},
	} {
		if _, xerr := svc.OAuthStart(context.Background(), uuid.NewString(), &org, models.InboxProviderGoogle, "", tt.web, tt.origin); xerr != errx.ErrEmailOnboardReturnOrigin {
			t.Fatalf("untrusted return origin should fail before storing state, got %v", xerr)
		}
	}
}

func TestOAuthReauthRejectsUntrustedGoogleReturnOrigin(t *testing.T) {
	t.Setenv("APP_URL", "https://app.example.com")
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("CORS_ALLOW_ORIGINS", "https://second.example.com")
	svc, repo, _, _ := reauthFixture("gmail", "owner@example.com")
	_, xerr := svc.OAuthReauth(context.Background(), repo.account.UserID, repo.account.OrganizationID, repo.account.ID, "https://evil.example.com")
	if xerr != errx.ErrEmailOnboardReturnOrigin {
		t.Fatalf("untrusted Google reauthorization origin should be refused, got %v", xerr)
	}
}

func TestOAuthReauthRejectsUntrustedMicrosoftReturnOrigin(t *testing.T) {
	svc, repo, _, _ := reauthFixture("outlook", "owner@example.com")
	_, xerr := svc.OAuthReauth(context.Background(), repo.account.UserID, repo.account.OrganizationID, repo.account.ID, "https://evil.example.com")
	if xerr != errx.ErrEmailOnboardReturnOrigin {
		t.Fatalf("Microsoft must reject an untrusted return origin, got %v", xerr)
	}
}

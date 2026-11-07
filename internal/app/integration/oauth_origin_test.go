package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type originRepo struct {
	repository.IntegrationRepository
	state *models.IntegrationOAuthState
}

func (r *originRepo) CreateOAuthState(_ context.Context, st *models.IntegrationOAuthState) error {
	r.state = st
	return nil
}
func (r *originRepo) OAuthReturnOrigin(_ context.Context, state string) (string, error) {
	if r.state == nil || r.state.State != state || r.state.UsedAt != nil || r.state.ExpiresAt.Before(time.Now()) {
		return "", errors.New("no live state")
	}
	return r.state.Params["return_origin"], nil
}
func (r *originRepo) GetConnectionByID(_ context.Context, _, _ uuid.UUID) (*models.IntegrationConnection, error) {
	return &models.IntegrationConnection{Provider: models.IntegrationHubSpot, AuthMethod: string(models.IntegrationAuthOAuth)}, nil
}
func (r *originRepo) SetConnectionStatus(context.Context, uuid.UUID, models.IntegrationStatus, models.IntegrationHealth, string) error {
	return nil
}

func TestIntegrationStartAndReauthBindDashboardOrigin(t *testing.T) {
	t.Setenv("APP_URL", "https://app.warmbly.com")
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("CORS_ALLOW_ORIGINS", "https://second.example.com")
	t.Setenv("HUBSPOT_OAUTH_CLIENT_ID", "client")
	t.Setenv("HUBSPOT_OAUTH_CLIENT_SECRET", "secret")
	r := &originRepo{}
	s := &service{repo: r, oauth: NewOAuthManager()}
	ctx := config.WithDashboardOrigin(context.Background(), "https://second.example.com")
	org, user := uuid.New(), uuid.New()
	for _, reauth := range []bool{false, true} {
		var resp *models.IntegrationOAuthStartResponse
		var err error
		if reauth {
			resp, err = s.Reauth(ctx, org, user, uuid.New())
		} else {
			resp, err = s.OAuthStart(ctx, org, user, models.IntegrationHubSpot, "HubSpot", map[string]string{"return_origin": "https://evil.example.com"})
		}
		if err != nil {
			t.Fatal(err)
		}
		if got := s.OAuthReturnOrigin(context.Background(), resp.State); got != "https://second.example.com" {
			t.Fatalf("origin = %q", got)
		}
		if r.state.UserID != user || r.state.OrganizationID != org || r.state.UsedAt != nil {
			t.Fatal("origin peek must preserve user/org bound single-use state")
		}
		if s.OAuthReturnOrigin(ctx, "unknown") != "" {
			t.Fatal("unknown state must not route")
		}
		now := time.Now()
		r.state.UsedAt = &now
		if s.OAuthReturnOrigin(ctx, resp.State) != "" {
			t.Fatal("consumed state must not route")
		}
		r.state.UsedAt = nil
		r.state.ExpiresAt = now.Add(-time.Second)
		if s.OAuthReturnOrigin(ctx, resp.State) != "" {
			t.Fatal("expired state must not route")
		}
		r.state.ExpiresAt = now.Add(time.Minute)
		t.Setenv("CORS_ALLOW_ORIGINS", "https://app.warmbly.com")
		if s.OAuthReturnOrigin(ctx, resp.State) != "" {
			t.Fatal("removed origin must not route")
		}
		t.Setenv("CORS_ALLOW_ORIGINS", "https://second.example.com")
	}
}

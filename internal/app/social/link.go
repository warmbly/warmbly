package social

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// LinkStart is POST /v1/social/link: GraphKit's approval page.
type LinkStart struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// StartLink opens GraphKit's page where the customer links a GraphKit workspace they administer.
func (s *Service) StartLink(ctx context.Context, orgID uuid.UUID) (*LinkStart, error) {
	if err := s.ready(ctx, orgID); err != nil {
		return nil, err
	}
	// GraphKit appends link_session and status to the return address.
	back := strings.TrimRight(config.DashboardBaseURL(ctx), "/") + "/app/settings/social"
	ls, err := s.api().CreateLinkSession(ctx, ext(orgID), back, opKey("link"))
	if err != nil {
		return nil, s.upstream(ctx, "social.link.start", err)
	}
	if err := s.Repo.SetLinkSession(ctx, orgID, &ls.ID); err != nil {
		return nil, err
	}
	return &LinkStart{URL: ls.URL, ExpiresAt: ls.ExpiresAt}, nil
}

// CompleteLink reads the link session back from GraphKit and records the mode it left; repeating it changes nothing.
func (s *Service) CompleteLink(ctx context.Context, orgID uuid.UUID, sessionID string) (*Status, error) {
	if err := s.ready(ctx, orgID); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(sessionID, "sls_") || len(sessionID) > 100 {
		return nil, errNotFound
	}
	// The session is read under the workspace's own GraphKit account, so another workspace's id is not found.
	if _, err := s.api().GetLinkSession(ctx, ext(orgID), sessionID); err != nil {
		return nil, s.upstream(ctx, "social.link.get", err)
	}
	if _, err := s.refreshWorkspace(ctx, orgID, 0); err != nil {
		return nil, s.upstream(ctx, "social.workspace", err)
	}
	if err := s.Repo.SetLinkSession(ctx, orgID, nil); err != nil {
		return nil, err
	}
	return s.Status(ctx, orgID)
}

// Unlink ends the link to the customer's GraphKit workspace; its accounts stay there and leave Warmbly.
func (s *Service) Unlink(ctx context.Context, orgID uuid.UUID) error {
	if err := s.ready(ctx, orgID); err != nil {
		return err
	}
	if err := s.api().Unlink(ctx, ext(orgID), opKey("unlink")); err != nil {
		if ge, ok := graphkit.AsError(err); !ok || ge.Code != graphkit.CodeNotFound {
			return s.upstream(ctx, "social.unlink", err)
		}
	}
	s.applyWorkspace(ctx, orgID, &graphkit.SocialWorkspace{Mode: models.SocialModeNone})
	if _, err := s.Repo.ForgetLinkedAccounts(ctx, orgID, s.now()); err != nil {
		return err
	}
	s.publish(ctx, orgID, pubsub.SocialEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventSocialLinkUpdated}, Status: models.SocialModeNone})
	return nil
}

// Sandbox is the simulated GraphKit on a development instance, whose link page the backend stands in for; nil otherwise.
func (s *Service) Sandbox() *graphkit.Fake {
	if !config.DevEnv() || !s.available() {
		return nil
	}
	f, _ := s.api().(*graphkit.Fake)
	return f
}

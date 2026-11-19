package partner

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// stateTTL bounds how long another replica may act on a stale partner state.
const stateTTL = 10 * time.Second

// noLinksTTL bounds how long a replica trusts that no workspace on the instance has a partner.
const noLinksTTL = 30 * time.Second

type cachedState struct {
	ref *models.PartnerManagedRef
	at  time.Time
}

// ManagedRef is the partner billing a workspace and its standing with it, or nil for a workspace no partner manages.
// A failed read keeps a linked workspace's last known standing and treats any other workspace as unlinked.
func (s *Service) ManagedRef(ctx context.Context, orgID uuid.UUID) (*models.PartnerManagedRef, error) {
	s.mu.Lock()
	c, cached := s.states[orgID]
	if cached && s.now().Sub(c.at) < stateTTL {
		s.mu.Unlock()
		return c.ref, nil
	}
	s.mu.Unlock()
	if !cached && !s.instanceHasLinks(ctx) {
		return nil, nil
	}
	po, display, err := s.Repo.GetPartnerOrgByOrganization(ctx, orgID)
	if err != nil {
		log.Error().Err(err).Str("organization_id", orgID.String()).Msg("partner: reading a workspace's partner standing failed")
		if cached && c.ref != nil {
			return c.ref, nil
		}
		return nil, nil
	}
	var ref *models.PartnerManagedRef
	if po != nil {
		ref = &models.PartnerManagedRef{DisplayName: display, State: po.State, DeletesAt: po.DeletesAt, Test: po.Mode == models.PartnerModeTest}
		if po.State != models.PartnerOrgActive {
			ref.SuspendedReason = po.SuspendedReason
		}
	}
	s.mu.Lock()
	if len(s.states) > 10_000 {
		s.states = map[uuid.UUID]cachedState{}
	}
	s.states[orgID] = cachedState{ref: ref, at: s.now()}
	if ref != nil {
		s.linked = true
	}
	s.mu.Unlock()
	return ref, nil
}

// instanceHasLinks is false only while the instance is known to have no partner workspace, so most instances never look one up.
func (s *Service) instanceHasLinks(ctx context.Context) bool {
	s.mu.Lock()
	if s.linked || (!s.noLinksAt.IsZero() && s.now().Sub(s.noLinksAt) < noLinksTTL) {
		linked := s.linked
		s.mu.Unlock()
		return linked
	}
	s.mu.Unlock()
	has, err := s.Repo.HasPartnerOrganizations(ctx)
	if err != nil {
		log.Error().Err(err).Msg("partner: reading whether the instance has partner workspaces failed")
		return true
	}
	s.mu.Lock()
	if has {
		s.linked = true
	} else {
		s.noLinksAt = s.now()
	}
	s.mu.Unlock()
	return has
}

// markLinked records a partner workspace created here, so this replica looks up standings at once.
func (s *Service) markLinked() {
	s.mu.Lock()
	s.linked = true
	s.mu.Unlock()
}

func (s *Service) forget(orgID uuid.UUID) {
	s.mu.Lock()
	delete(s.states, orgID)
	s.mu.Unlock()
}

// IsPartnerManaged reports whether a partner bills the workspace, and the name its members see for it.
// A failed read answers false: callers use it to hide self-serve purchases, never to grant anything.
func (s *Service) IsPartnerManaged(ctx context.Context, orgID uuid.UUID) (bool, string) {
	if s == nil {
		return false, ""
	}
	ref, err := s.ManagedRef(ctx, orgID)
	if err != nil {
		errs.CaptureException(err)
		return false, ""
	}
	if ref == nil {
		return false, ""
	}
	return true, ref.DisplayName
}

// WarmupPaused reports whether the workspace's partner holds its warmup (suspended or cancelled).
// It satisfies the feature gate's partner hold.
func (s *Service) WarmupPaused(ctx context.Context, orgID uuid.UUID) (bool, error) {
	if s == nil {
		return false, nil
	}
	ref, err := s.ManagedRef(ctx, orgID)
	if err != nil || ref == nil {
		return false, err
	}
	// A test-mode workspace never warms: its mailboxes would join the shared pools real customers warm in.
	return ref.State.Paused() || ref.Test, nil
}

// ReadOnly reports whether members of the workspace may only read: its partner suspended or cancelled it.
func (s *Service) ReadOnly(ctx context.Context, orgID uuid.UUID) (*models.PartnerManagedRef, *errx.Error) {
	ref, err := s.ManagedRef(ctx, orgID)
	if err != nil {
		return nil, internal(err)
	}
	if ref == nil || !ref.State.Paused() {
		return nil, nil
	}
	return ref, nil
}

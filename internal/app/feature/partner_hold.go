package feature

import (
	"context"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
)

// PartnerHold reports a workspace whose reseller suspended or cancelled it, which stops its warmup
// whatever the deployment's billing mode. Campaign sending stops through the workspace's risk state.
type PartnerHold interface {
	WarmupPaused(ctx context.Context, orgID uuid.UUID) (bool, error)
}

// WithPartnerHold wires h into a gate built by NewService and returns it.
func WithPartnerHold(g FeatureGateService, h PartnerHold) FeatureGateService {
	if w, ok := g.(*featureGateService); ok {
		w.partnerHold = h
	}
	return g
}

// heldByPartner is true when the workspace's partner holds its warmup; a failed read is an unknown entitlement.
func (s *featureGateService) heldByPartner(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error) {
	if s.partnerHold == nil {
		return false, nil
	}
	paused, err := s.partnerHold.WarmupPaused(ctx, orgID)
	if err != nil {
		return true, errx.New(errx.Internal, "failed to read the workspace's partner standing")
	}
	return paused, nil
}

// MailboxHold is the per-mailbox half of a partner's hold: a mailbox its partner left off warms nothing.
type MailboxHold interface {
	MailboxWarmupHeld(ctx context.Context, orgID, mailboxID uuid.UUID) (bool, error)
}

// MailboxWarmupGate answers the per-mailbox half of warmup, after the workspace's entitlement.
type MailboxWarmupGate interface {
	MailboxWarmupHeld(ctx context.Context, orgID, mailboxID uuid.UUID) (bool, *errx.Error)
}

// CanWarmMailbox is the warmup entitlement of one mailbox: the workspace's and the mailbox's own.
// Every per-mailbox warmup decision reads it.
func CanWarmMailbox(ctx context.Context, g FeatureGateService, orgID, mailboxID uuid.UUID) (bool, *errx.Error) {
	ok, xerr := g.CanUseWarmup(ctx, orgID)
	if !ok || xerr != nil {
		return ok, xerr
	}
	held, xerr := MailboxWarmupHeld(ctx, g, orgID, mailboxID)
	return !held && xerr == nil, xerr
}

// MailboxWarmupHeld is the per-mailbox half alone, for a caller that already read the workspace's entitlement.
func MailboxWarmupHeld(ctx context.Context, g FeatureGateService, orgID, mailboxID uuid.UUID) (bool, *errx.Error) {
	if m, ok := g.(MailboxWarmupGate); ok {
		return m.MailboxWarmupHeld(ctx, orgID, mailboxID)
	}
	return false, nil
}

// MailboxWarmupHeld reports a mailbox its workspace's partner did not enable for warmup; a failed read holds it.
func (s *featureGateService) MailboxWarmupHeld(ctx context.Context, orgID, mailboxID uuid.UUID) (bool, *errx.Error) {
	m, ok := s.partnerHold.(MailboxHold)
	if !ok {
		return false, nil
	}
	held, err := m.MailboxWarmupHeld(ctx, orgID, mailboxID)
	if err != nil {
		return true, errx.New(errx.Internal, "failed to read the mailbox's partner enablement")
	}
	return held, nil
}

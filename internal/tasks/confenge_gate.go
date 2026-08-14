package tasks

import (
	"context"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/confenge"
)

// ConfengeOutboundGate is the optional final gate for CONFENGE-attributed campaign email.
// Implemented by confenge.Service; nil means no global CONFENGE pacing.
type ConfengeOutboundGate interface {
	GateCampaignEmail(ctx context.Context, orgID uuid.UUID, campaignName, recipientEmail string, campaignID, contactID, sequenceID uuid.UUID) confenge.CampaignGateResult
	ReleaseCampaignEmail(ctx context.Context, reservationID uuid.UUID, errText string)
}

// WireConfengeDispatch attaches the CONFENGE global outbound governor to campaign sends.
func (s *tasksService) WireConfengeDispatch(g ConfengeOutboundGate) {
	s.confengeGate = g
}

package advanced

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// RecordInboundBounce turns a permanent NDR the worker parsed into a bounce
// deliverability event. The worker only emits permanent (5.x.x) bounces with an
// original Message-ID, so this resolves that id to the campaign send and routes
// it through IngestDeliverabilityEvent (suppression + campaign progress +
// breaker), keyed idempotently so re-delivered NDRs don't double-count.
func (s *service) RecordInboundBounce(ctx context.Context, emailAccountID uuid.UUID, originalMessageID, failedRecipient, reason string) *errx.Error {
	originalMessageID = strings.Trim(strings.TrimSpace(originalMessageID), "<>")
	if originalMessageID == "" {
		return nil
	}

	task, err := s.taskRepo.GetTaskByMessageID(ctx, originalMessageID)
	if err != nil || task == nil {
		// Unknown message id (warmup mail, non-campaign send, or already
		// pruned) — nothing to attribute the bounce to.
		return nil
	}

	// The NDR should have landed in the same mailbox that sent it; if it didn't
	// resolve to this account, don't attribute a cross-account bounce.
	if task.EmailAccountID != emailAccountID {
		return nil
	}

	account, aerr := s.emailRepo.GetByID(ctx, emailAccountID)
	if aerr != nil || account == nil || account.OrganizationID == nil {
		return nil
	}

	req := &models.IngestDeliverabilityEventRequest{
		EventType:      models.DeliverabilityEventBounce,
		Provider:       "inbound_ndr",
		TaskID:         &task.ID,
		RecipientEmail: failedRecipient,
		Reason:         reason,
		// Same NDR re-synced (delta re-runs, reconnects) must not double-count.
		IdempotencyKey: "ndr:" + originalMessageID,
	}

	if ct, cerr := s.taskRepo.GetCampaignTask(ctx, task.ID); cerr == nil && ct != nil {
		req.CampaignID = ct.CampaignID
		req.ContactID = ct.ContactID
		if req.RecipientEmail == "" && ct.ContactID != nil {
			if contact, cerr := s.contactRepo.GetByID(ctx, *ct.ContactID); cerr == nil && contact != nil {
				req.RecipientEmail = contact.Email
			}
		}
	}

	if req.RecipientEmail == "" {
		// IngestDeliverabilityEvent requires a recipient; without one we can't
		// suppress or record. Give up rather than guess.
		return nil
	}

	return s.IngestDeliverabilityEvent(ctx, *account.OrganizationID, req)
}

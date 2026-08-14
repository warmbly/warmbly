package confenge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// OutcomeSink is the consumer-facing surface for commercial outcomes.
// Implemented by *service.
type OutcomeSink interface {
	Enabled() bool
	// NoteReply enqueues REPLIED when the address matches a staged candidate.
	NoteReply(ctx context.Context, orgID uuid.UUID, contactEmail string, meta map[string]any) error
	// NoteBounce enqueues BOUNCED for a failed recipient email.
	NoteBounce(ctx context.Context, orgID uuid.UUID, contactEmail, reason string) error
	// NoteDNC enqueues DO_NOT_CONTACT and marks matching candidates.
	NoteDNC(ctx context.Context, orgID uuid.UUID, contactEmail, reason string) error
}

func (s *service) enqueueErr(ctx context.Context, orgID uuid.UUID, ev models.OutreachOutcome) error {
	if xerr := s.EnqueueOutcome(ctx, orgID, ev); xerr != nil {
		return fmt.Errorf("%s", xerr.Message)
	}
	return nil
}

// NoteReply implements OutcomeSink.
func (s *service) NoteReply(ctx context.Context, orgID uuid.UUID, contactEmail string, meta map[string]any) error {
	if !s.cfg.Enabled {
		return nil
	}
	email := strings.TrimSpace(strings.ToLower(contactEmail))
	if email == "" {
		return nil
	}
	cand, acc, err := s.repo.FindCandidateByEmail(ctx, orgID, email)
	if err != nil || cand == nil || acc == nil {
		return nil // not a confenge lead
	}
	_, _ = s.repo.CancelOpenTouchpoints(ctx, orgID, acc.ID, models.TouchpointReplied, "REPLY")
	payload, _ := jsonMarshalMap(meta)
	phone := ""
	if cand != nil {
		phone = cand.PhoneE164
		if phone == "" {
			phone = cand.Phone
		}
	}
	s.cancelQueuedForRecipient(ctx, orgID, email, phone, "reply")
	return s.enqueueErr(ctx, orgID, models.OutreachOutcome{
		IdempotencyKey: fmt.Sprintf("replied:%s:%s:%d", orgID, email, time.Now().UTC().Truncate(time.Minute).Unix()),
		SourceLeadID:   acc.SourceLeadID,
		CNPJ14:         acc.CNPJ14,
		ContactEmail:   email,
		EventType:      OutcomeReplied,
		OccurredAt:     time.Now().UTC(),
		Payload:        payload,
	})
}

// NoteBounce implements OutcomeSink.
func (s *service) NoteBounce(ctx context.Context, orgID uuid.UUID, contactEmail, reason string) error {
	if !s.cfg.Enabled {
		return nil
	}
	email := strings.TrimSpace(strings.ToLower(contactEmail))
	if email == "" {
		return nil
	}
	cand, acc, err := s.repo.FindCandidateByEmail(ctx, orgID, email)
	if err != nil {
		return err
	}
	if cand != nil {
		cand.Bounced = true
		cand.VerificationStatus = models.OutreachVerifyBounced
		_, _ = s.repo.UpsertCandidate(ctx, cand)
	}
	cnpj, lead := "", ""
	if acc != nil {
		cnpj, lead = acc.CNPJ14, acc.SourceLeadID
		_ = s.repo.SetAccountHumanFlags(ctx, orgID, acc.ID, acc.Blocked, acc.DoNotContact, "bounce", models.OutreachQueueBounced)
		_, _ = s.repo.CancelOpenTouchpoints(ctx, orgID, acc.ID, models.TouchpointBounced, "BOUNCE")
	}
	phone := ""
	if cand != nil {
		phone = cand.PhoneE164
		if phone == "" {
			phone = cand.Phone
		}
	}
	s.cancelQueuedForRecipient(ctx, orgID, email, phone, "bounce")
	return s.enqueueErr(ctx, orgID, models.OutreachOutcome{
		IdempotencyKey: fmt.Sprintf("bounced:%s:%s", orgID, email),
		SourceLeadID:   lead,
		CNPJ14:         cnpj,
		ContactEmail:   email,
		EventType:      OutcomeBounced,
		OccurredAt:     time.Now().UTC(),
		Payload:        mustJSON(map[string]any{"reason": reason}),
	})
}

// NoteDNC implements OutcomeSink.
func (s *service) NoteDNC(ctx context.Context, orgID uuid.UUID, contactEmail, reason string) error {
	if !s.cfg.Enabled {
		return nil
	}
	email := strings.TrimSpace(strings.ToLower(contactEmail))
	if email == "" {
		return nil
	}
	cand, acc, err := s.repo.FindCandidateByEmail(ctx, orgID, email)
	if err != nil {
		return err
	}
	if cand != nil {
		cand.DoNotContact = true
		cand.VerificationStatus = models.OutreachVerifyDoNotContact
		_, _ = s.repo.UpsertCandidate(ctx, cand)
	}
	cnpj, lead := "", ""
	if acc != nil {
		cnpj, lead = acc.CNPJ14, acc.SourceLeadID
		_ = s.repo.SetAccountHumanFlags(ctx, orgID, acc.ID, true, true, reason, models.OutreachQueueDoNotContact)
		_, _ = s.repo.CancelOpenTouchpoints(ctx, orgID, acc.ID, models.TouchpointDNC, "DNC")
	}
	// Dominant block: drop queued email AND WhatsApp outbound for this recipient.
	phone := ""
	if cand != nil {
		phone = cand.PhoneE164
		if phone == "" {
			phone = cand.Phone
		}
	}
	s.cancelQueuedForRecipient(ctx, orgID, email, phone, "DO_NOT_CONTACT")
	return s.enqueueErr(ctx, orgID, models.OutreachOutcome{
		IdempotencyKey: fmt.Sprintf("dnc:%s:%s", orgID, email),
		SourceLeadID:   lead,
		CNPJ14:         cnpj,
		ContactEmail:   email,
		EventType:      OutcomeDoNotContact,
		OccurredAt:     time.Now().UTC(),
		Payload:        mustJSON(map[string]any{"reason": reason}),
	})
}

func jsonMarshalMap(m map[string]any) ([]byte, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	return marshalJSON(m)
}

// OnClassifiedReply implements advanced.ConfengeReplyHook with subject/body for commercial lexicon.
func (s *service) OnClassifiedReply(ctx context.Context, orgID uuid.UUID, contactEmail, replyClass string, contactID *uuid.UUID, subject, bodyText string, actorID uuid.UUID) error {
	if xerr := s.HandleClassifiedReplyFull(ctx, orgID, actorID, contactEmail, replyClass, contactID, subject, bodyText, nil); xerr != nil {
		return fmt.Errorf("%s", xerr.Message)
	}
	return nil
}

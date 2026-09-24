package advanced

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/replyclassify"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Outcomes recorded on an entry the recheck has read.
const (
	replyOptOutKept        = "kept"
	replyOptOutLifted      = "lifted"
	replyOptOutNoEvidence  = "no_evidence"
	replyOptOutOtherOrigin = "other_origin"
)

// replyOptOutEvidenceLimit bounds how much of one sender's mail is re-read.
const replyOptOutEvidenceLimit = 200

// replyOptOutWriteWindow is how close to the entry's creation the message
// that wrote it was processed.
const replyOptOutWriteWindow = 10 * time.Minute

// AuditLogger records a system action in the workspace's audit trail.
type AuditLogger interface {
	LogAction(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, ip, userAgent string, changes, metadata map[string]string)
}

// WireAudit attaches the audit trail the reply opt-out recheck writes to.
func (s *service) WireAudit(a AuditLogger) { s.audit = a }

// RecheckReplyOptOuts re-reads one page of reply opt-outs under the current
// rules and lifts each one that no message from its sender supports. It
// returns the cursor and whether the list is exhausted.
func (s *service) RecheckReplyOptOuts(ctx context.Context, afterID uuid.UUID, limit int) (uuid.UUID, bool, error) {
	if s.uniboxRepo == nil {
		return afterID, true, nil
	}
	entries, err := s.repo.ListUncheckedReplyOptOuts(ctx, afterID, limit)
	if err != nil {
		return afterID, false, err
	}
	for i := range entries {
		entry := &entries[i]
		afterID = entry.ID
		outcome, err := s.recheckReplyOptOut(ctx, entry)
		if err != nil {
			log.Warn().Err(err).Str("suppression_id", entry.ID.String()).Msg("reply opt-out recheck failed; left for the next pass")
			continue
		}
		if outcome == replyOptOutLifted || outcome == "" {
			continue
		}
		if err := s.repo.MarkReplyOptOutChecked(ctx, entry.ID, outcome); err != nil {
			log.Warn().Err(err).Str("suppression_id", entry.ID.String()).Msg("reply opt-out recheck: could not record the outcome")
		}
	}
	return afterID, len(entries) < limit, nil
}

// recheckReplyOptOut lifts an entry only on positive evidence: the message
// that wrote it is found, and neither it nor any other message from the
// sender asks to stop under the current rule. Any read that fails keeps the
// entry, and "" means it changed under us and is read again next pass.
func (s *service) recheckReplyOptOut(ctx context.Context, entry *models.SuppressedRecipient) (string, error) {
	sender := entry.Email
	if as, ok := entry.Metadata["replied_as"].(string); ok && as != "" {
		sender = as
	}
	msgs, err := s.uniboxRepo.ListInboundFrom(ctx, entry.OrganizationID, sender, replyOptOutEvidenceLimit)
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		return replyOptOutNoEvidence, nil
	}
	// The row is upserted, so a link or one-click unsubscribe that a reply
	// later relabelled keeps its first created_at. Only an entry some reply
	// wrote at that moment is ours to judge.
	if !wroteByReply(entry, msgs) {
		return replyOptOutOtherOrigin, nil
	}

	// Contacts are deleted outright, so "was a contact" is read from what
	// outlives one: a campaign the entry was attributed to, or mail we sent
	// the address. Either makes it someone we were writing to.
	fromContact := entry.CampaignID != nil
	if !fromContact {
		c, cerr := s.contactRepo.GetByEmailAndOrganization(ctx, entry.OrganizationID, sender)
		if cerr != nil {
			return "", cerr
		}
		fromContact = c != nil
	}
	if !fromContact {
		written, werr := s.uniboxRepo.HasWrittenTo(ctx, entry.OrganizationID, sender)
		if werr != nil {
			return "", werr
		}
		fromContact = written
	}
	threads := map[string]bool{}
	for i := range msgs {
		stands, err := s.replyOptOutStands(ctx, &msgs[i].EmailMessageStoreData, fromContact, threads)
		if err != nil {
			return "", err
		}
		if stands {
			return replyOptOutKept, nil
		}
	}

	deleted, err := s.repo.DeleteReplyOptOut(ctx, entry.OrganizationID, entry.ID, entry.UpdatedAt)
	if err != nil {
		return "", err
	}
	if !deleted {
		return "", nil
	}
	if err := s.contactRepo.SetSubscribedByEmail(ctx, entry.OrganizationID, entry.Email, true); err != nil {
		log.Warn().Err(err).Str("suppression_id", entry.ID.String()).Msg("reply opt-out recheck: could not restore the contact's subscription flag")
	}
	if s.audit != nil {
		s.audit.LogAction(ctx, entry.OrganizationID, uuid.Nil, models.AuditActionDelete, models.AuditEntitySuppression, &entry.ID, "", "", nil,
			map[string]string{
				"value":  entry.Email,
				"kind":   string(entry.Kind),
				"source": string(entry.Source),
				"reason": "no message from the sender asks to stop: it was a bounce, an automatic reply, list mail or quoted history",
			})
	}
	log.Info().
		Str("organization_id", entry.OrganizationID.String()).
		Str("suppression_id", entry.ID.String()).
		Int("messages_read", len(msgs)).
		Msg("reply opt-out recheck: lifted a suppression no message from its sender supports")
	return replyOptOutLifted, nil
}

// wroteByReply finds the message the entry came from: processed within
// minutes of the entry's creation, with opt-out wording somewhere in it,
// which is all the earlier reading of a whole body needed.
func wroteByReply(entry *models.SuppressedRecipient, msgs []repository.InboundMessage) bool {
	for i := range msgs {
		m := &msgs[i]
		gap := entry.CreatedAt.Sub(m.ProcessedAt)
		if gap < -replyOptOutWriteWindow || gap > replyOptOutWriteWindow {
			continue
		}
		if replyclassify.MentionsOptOut(m.Subject, firstNonEmpty(m.BodyText, m.Snippet)) {
			return true
		}
	}
	return false
}

// replyOptOutStands is the live path's decision, asked again of a stored
// message. The thread test is looser than live: a send whose campaign has
// since been deleted still makes the message a reply to our outreach.
func (s *service) replyOptOutStands(ctx context.Context, msg *models.EmailMessageStoreData, fromContact bool, threads map[string]bool) (bool, error) {
	body := firstNonEmpty(msg.BodyText, msg.Snippet)
	if !replyclassify.IsOptOut(msg.Subject, body) {
		return false, nil
	}
	headers := buildReplyHeaders(msg)
	verdict := replyclassify.ClassifyOffline(replyclassify.Input{Headers: headers, Subject: msg.Subject, BodyText: body})
	if replyclassify.IsAutomated(verdict.Class) {
		return false, nil
	}
	inThread, err := s.answersCampaignSend(ctx, msg.InReplyTo, threads)
	if err != nil {
		return false, err
	}
	return replyOptOutEligible(verdict, inThread, fromContact, headers), nil
}

// answersCampaignSend reports whether any of the ids is one of our campaign
// sends, memoized across one entry's messages.
func (s *service) answersCampaignSend(ctx context.Context, inReplyTo []string, seen map[string]bool) (bool, error) {
	if s.taskRepo == nil {
		return false, nil
	}
	for _, mid := range inReplyTo {
		id := cleanMessageID(mid)
		if id == "" {
			continue
		}
		known, ok := seen[id]
		if !ok {
			task, err := s.taskRepo.GetTaskByMessageID(ctx, id)
			if err != nil {
				return false, err
			}
			known = task != nil && task.TaskType == "campaign"
			seen[id] = known
		}
		if known {
			return true, nil
		}
	}
	return false, nil
}

// inCampaignThread reports whether a message answers one of this workspace's
// campaign sends.
func (s *service) inCampaignThread(ctx context.Context, orgID uuid.UUID, inReplyTo []string) (bool, error) {
	if s.taskRepo == nil || s.campaignRepo == nil {
		return false, nil
	}
	for _, mid := range inReplyTo {
		id := cleanMessageID(mid)
		if id == "" {
			continue
		}
		task, err := s.taskRepo.GetTaskByMessageID(ctx, id)
		if err != nil {
			return false, err
		}
		if task == nil || task.TaskType != "campaign" {
			continue
		}
		ct, err := s.taskRepo.GetCampaignTask(ctx, task.ID)
		if err != nil {
			return false, err
		}
		if ct == nil || ct.CampaignID == nil {
			continue
		}
		campaign, err := s.campaignRepo.GetByID(ctx, *ct.CampaignID)
		if errors.Is(err, errx.ErrResourceNotFound) {
			continue
		}
		if err != nil {
			return false, err
		}
		if campaign != nil && campaign.OrganizationID != nil && *campaign.OrganizationID == orgID {
			return true, nil
		}
	}
	return false, nil
}

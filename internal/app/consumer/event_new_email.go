package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/advanced"
	"github.com/warmbly/warmbly/internal/app/inboxtag"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/arrivaldiag"
	"github.com/warmbly/warmbly/internal/pkg/mailhdr"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"github.com/warmbly/warmbly/internal/repository"
)

func (s *JobsService) HandleNewEmail(ctx context.Context, e *models.JobEventNewEmail) error {
	ctx = withMailboxMemo(ctx)
	err := s.ingestNewEmail(ctx, e)
	if errors.Is(err, errWarmupVerification) && s.UniboxRepository != nil {
		finish := arrivaldiag.Begin(ctx, arrivaldiag.WarmupDefer)
		storeErr := s.UniboxRepository.DeferWarmupVerification(ctx, e)
		finish()
		if storeErr != nil {
			if s.dropForDeletedMailbox(ctx, e.UserID, e.Message.EmailID, storeErr) {
				return nil
			}
			return fmt.Errorf("defer inbox arrival: %w", storeErr)
		}
		log.Warn().Err(err).Str("email_id", e.Message.EmailID.String()).Msg("inbox arrival queued for warmup verification")
		return nil
	}
	return err
}

var errWarmupVerification = errors.New("warmup verification unavailable")

func (s *JobsService) ingestNewEmail(ctx context.Context, e *models.JobEventNewEmail) error {
	// Drop malformed events rather than dereferencing nil: this handler runs on
	// the shared consumer, so one bad payload would otherwise panic the process
	// and stop every org's event processing.
	if e == nil || e.Message == nil {
		log.Warn().Msg("NEW_EMAIL event without a message body, dropping")
		return nil
	}
	e.Message.ValidText()
	if recorder, ok := s.PlacementRepo.(interface {
		RecordProbeObservation(context.Context, uuid.UUID, string, string, []string, time.Time) error
	}); ok && !strings.EqualFold(e.Message.Folder, models.FolderSent) && e.Message.MessageID != "" {
		finish := arrivaldiag.Begin(ctx, arrivaldiag.ProbeObservation)
		err := recorder.RecordProbeObservation(ctx, e.Message.EmailID, e.Message.MessageID, e.Message.Folder, e.Message.Flags, time.Now().UTC())
		finish()
		if err != nil {
			return fmt.Errorf("record seed observation: %w", err)
		}
	}
	if s.CloudExec != nil {
		if own, err := s.CloudExec.OwnSentCopy(ctx, e.Message); err != nil {
			return fmt.Errorf("%w: %w", errWarmupVerification, err)
		} else if own {
			return nil
		}
	}
	warmupToken := warmupTokenFromMessage(e.Message)
	if warmupToken != "" {
		handled, err := s.handleWarmupEmail(ctx, e, warmupToken)
		if err != nil {
			return fmt.Errorf("%w: %w", errWarmupVerification, err)
		}
		if handled {
			return s.fileWarmupOutOfMailbox(ctx, e)
		}
	}
	if handled, err := s.handleUnmarkedWarmupEmail(ctx, e); err != nil {
		return fmt.Errorf("%w: %w", errWarmupVerification, err)
	} else if handled {
		return s.fileWarmupOutOfMailbox(ctx, e)
	}
	if warmup, err := s.isKnownWarmupEmail(ctx, e); err != nil {
		return fmt.Errorf("%w: %w", errWarmupVerification, err)
	} else if warmup {
		return s.fileOrReportWarmup(ctx, e)
	}
	if reply, err := s.isWarmupThreadReply(ctx, e); err != nil {
		return fmt.Errorf("%w: %w", errWarmupVerification, err)
	} else if reply {
		return s.fileOrReportWarmup(ctx, e)
	}
	if report, err := s.isWarmupReport(ctx, e); err != nil {
		return fmt.Errorf("%w: %w", errWarmupVerification, err)
	} else if report {
		return nil
	}

	// A placement test's copies are the seeds' business; the sender's own
	// sent copy of each stays out of its unibox.
	if s.PlacementRepo != nil && e.Message.Folder == models.FolderSent && e.Message.MessageID != "" {
		if probe, perr := s.PlacementRepo.IsProbeSentCopy(ctx, e.Message.EmailID, e.Message.MessageID); perr == nil && probe {
			return nil
		}
	}

	// A pool-linked mailbox is warmup-only: everything else is dropped unread.
	if s.PoolLinkRepo != nil {
		if linked, lerr := s.PoolLinkRepo.GetMailboxByAccount(ctx, e.Message.EmailID); lerr == nil && linked != nil {
			log.Debug().Str("email_account_id", e.Message.EmailID.String()).Msg("dropping non-warmup mail for pool-linked mailbox")
			return nil
		}
	}

	// Normal email processing
	finish := arrivaldiag.Begin(ctx, arrivaldiag.UniboxCreate)
	err := s.UniboxRepository.CreateEntry(ctx, e.UserID, e.Message)
	finish()
	if err != nil {
		if s.dropForDeletedMailbox(ctx, e.UserID, e.Message.EmailID, err) {
			return nil
		}
		CaptureError(e.UserID, e.Message.EmailID, err)
		return err
	}
	if e.Message != nil {
		finish = arrivaldiag.Begin(ctx, arrivaldiag.Realtime)
		inbox := s.emailInboxEvent(ctx, e.UserID, e.Message)
		if s.StreamingPublisher != nil {
			s.StreamingPublisher.PublishEmailReceived(ctx, inbox)
		}
		finish()
		// Fan an opt-in firehose webhook for the arrival (inbox.email_received).
		if s.AdvancedService != nil && inbox.OrgID != "" {
			if orgID, perr := uuid.Parse(inbox.OrgID); perr == nil {
				finish = arrivaldiag.Begin(ctx, arrivaldiag.Webhook)
				s.AdvancedService.EmitCampaignEvent(ctx, orgID, models.WebhookEventInboxEmailReceived, map[string]any{
					"email_account_id": inbox.EmailAccountID,
					"message_id":       inbox.MessageID,
					"thread_id":        inbox.ThreadID,
					"subject":          inbox.Subject,
					"from":             inbox.From,
				})
				finish()
			}
		}
	}

	// Automatic tagging. Optional, off unless an operator configured it, and
	// best-effort in exactly the same way as the reply automation below: a
	// classification that fails must never cost the workspace the message.
	//
	// MayBeInbound is the direction filter, read from the folder rather than
	// guessed from the content. Given only a body, the model called our own
	// outbound a human reply at 0.94 confidence, so direction is decided here
	// and the model is never asked.
	if s.InboxTagger.Enabled() && e.Message.MayBeInbound() {
		finish = arrivaldiag.Begin(ctx, arrivaldiag.Tagging)
		s.tagInboundMessage(ctx, e)
		finish()
	}

	// Advanced reply-intent automation is best-effort and should not block inbox
	// ingest. ProcessIncomingReply also runs the layered reply classifier
	// (replyclassify) and persists reply_class/confidence/source on the contact's
	// campaign progress, gating replied_at so automated replies (auto_reply /
	// out_of_office) never count as a human reply for stop_on_reply / branching.
	if s.AdvancedService != nil && e.Message.MayBeInbound() {
		finish = arrivaldiag.Begin(ctx, arrivaldiag.ReplyAutomation)
		// Logged, not propagated: the ingest must survive it, but a silent
		// failure here is indistinguishable from a reply that linked fine.
		if xerr := s.AdvancedService.ProcessIncomingReply(ctx, e.Message.EmailID, e.Message); xerr != nil {
			log.Warn().Err(xerr).
				Str("email_account_id", e.Message.EmailID.String()).
				Str("message_id", e.Message.MessageID).
				Msg("Reply-intent automation failed; inbox ingest kept")
		}
		finish()
	}

	if s.SlackInbox != nil && e.Message.MayBeInbound() {
		finish = arrivaldiag.Begin(ctx, arrivaldiag.Slack)
		if account, aerr := s.mailbox(ctx, e.Message.EmailID); aerr == nil && account != nil && account.OrganizationID != nil {
			s.SlackInbox.InboundMessage(ctx, *account.OrganizationID, account, e.Message)
		}
		finish()
	}

	return nil
}

// SlackInboxPoster mirrors one stored inbox arrival into Slack. It must not
// block ingest; delivery happens in the background.
type SlackInboxPoster interface {
	InboundMessage(ctx context.Context, orgID uuid.UUID, account *models.Email, msg *models.EmailMessageStoreData)
}

func (s *JobsService) publishEmailUpdated(ctx context.Context, userID uuid.UUID, message *models.EmailMessageStoreData) {
	if s.StreamingPublisher == nil || message == nil {
		return
	}
	s.StreamingPublisher.PublishEmailUpdated(ctx, s.emailInboxEvent(ctx, userID, message))
}

// emailInboxEvent builds the realtime inbox payload. Org-scoped (best-effort)
// so every teammate's unibox updates live, not just the mailbox owner's.
func (s *JobsService) emailInboxEvent(ctx context.Context, userID uuid.UUID, message *models.EmailMessageStoreData) *pubsub.EmailInboxEvent {
	var orgID string
	if account, err := s.mailbox(ctx, message.EmailID); err == nil && account != nil && account.OrganizationID != nil {
		orgID = account.OrganizationID.String()
	}
	return &pubsub.EmailInboxEvent{
		BaseEvent:      pubsub.BaseEvent{UserID: userID.String()},
		OrgID:          orgID,
		EmailAccountID: message.EmailID.String(),
		MessageID:      message.ID.String(),
		ThreadID:       message.ThreadID,
		Subject:        message.Subject,
		From:           strings.Join(message.FromAddr, ", "),
		Preview:        message.Snippet,
		Folder:         models.NormalizeFolder(message.Folder, message.Flags),
	}
}

// extractHeaderValue extracts a custom header value from the email message
// Checks InReplyTo field encoding or direct header access
func extractHeaderValue(msg *models.EmailMessageStoreData, headerName string) string {
	if msg == nil {
		return ""
	}

	// Check flags for X-Warmbly-Token (workers store custom headers in flags for detection)
	for _, flag := range msg.Flags {
		if name, value, ok := strings.Cut(flag, ":"); ok && strings.EqualFold(strings.TrimSpace(name), headerName) {
			return strings.TrimSpace(value)
		}
	}

	return ""
}

// handleWarmupEmail hides either verified copy; only a live recipient token triggers engagement.
func (s *JobsService) handleWarmupEmail(ctx context.Context, e *models.JobEventNewEmail, tokenStr string) (bool, error) {
	defer arrivaldiag.Begin(ctx, arrivaldiag.WarmupMarked)()
	if s.WarmupRepo == nil {
		return false, nil
	}

	tokenUUID, err := uuid.Parse(tokenStr)
	if err != nil {
		return false, nil
	}

	token, err := s.WarmupRepo.FindWarmupToken(ctx, tokenUUID)
	if err != nil {
		return false, fmt.Errorf("warmup token lookup: %w", err)
	}
	if token == nil {
		return false, nil
	}
	if token.RecipientAccountID != e.Message.EmailID {
		// A foreign token is never evidence against the receiving mailbox.
		if token.SenderAccountID != e.Message.EmailID {
			log.Info().
				Str("email_account_id", e.Message.EmailID.String()).
				Str("token_sender", token.SenderAccountID.String()).
				Str("token_recipient", token.RecipientAccountID.String()).
				Msg("warmup token for another mailbox arrived; filed as ordinary mail")
		}
		return token.SenderAccountID == e.Message.EmailID, nil
	}

	return s.processWarmupReceipt(ctx, e, token)
}

// handleUnmarkedWarmupEmail verifies warmup mail that arrived without its
// verify header. Microsoft Graph drops custom headers in transit (and
// re-stamps the Message-ID), so mail sent from an Outlook or Microsoft 365
// mailbox reaches every recipient carrying no marker at all; matched only on
// the header it would count for nobody and be filed as ordinary inbox mail.
func (s *JobsService) handleUnmarkedWarmupEmail(ctx context.Context, e *models.JobEventNewEmail) (bool, error) {
	defer arrivaldiag.Begin(ctx, arrivaldiag.WarmupUnmarked)()
	if s.WarmupRepo == nil || e.Message == nil {
		return false, nil
	}
	token, err := s.WarmupRepo.FindDeliveredWarmupToken(
		ctx,
		e.Message.EmailID,
		firstSenderAddress(e.Message.FromAddr),
		e.Message.MessageID,
		e.Message.Subject,
	)
	if err != nil {
		return false, fmt.Errorf("unmarked warmup lookup: %w", err)
	}
	if token == nil {
		return false, nil
	}
	log.Debug().
		Str("task_id", token.TaskID.String()).
		Str("email_account_id", e.Message.EmailID.String()).
		Msg("verified warmup mail that arrived without its verify header")
	return s.processWarmupReceipt(ctx, e, token)
}

func warmupReceiptProof(message *models.EmailMessageStoreData) repository.WarmupReceiptProof {
	marker, _ := uuid.Parse(warmupTokenFromMessage(message))
	return repository.WarmupReceiptProof{Token: marker, SenderAddress: firstSenderAddress(message.FromAddr), MessageID: message.MessageID}
}

func (s *JobsService) processWarmupReceipt(ctx context.Context, e *models.JobEventNewEmail, token *models.WarmupToken) (bool, error) {
	if processor, ok := s.WarmupRepo.(repository.WarmupReceiptProcessor); ok {
		return processor.ProcessWarmupReceipt(ctx, token.Token, e.Message.EmailID, warmupReceiptProof(e.Message), func(current *models.WarmupToken) error {
			return s.acceptWarmupEmail(ctx, e, current)
		})
	}
	if token.ConsumedAt == nil && token.ExpiresAt.After(time.Now()) {
		return true, s.acceptWarmupEmail(ctx, e, token)
	}
	return true, nil
}

const cloudWarmupCheckTimeout = 5 * time.Second

func warmupTokenFromMessage(message *models.EmailMessageStoreData) string {
	if token := extractHeaderValue(message, config.WarmupVerifyHeader); token != "" {
		return token
	}
	return extractHeaderValue(message, "X-Warmbly-Token")
}

// isWarmupReport reports whether this arrival is a bounce notification or an
// abuse report ABOUT one of this mailbox's warmup sends.
//
// Such a report is not warmup mail and carries no token, so nothing above
// recognises it: what lands in the unibox is "Undelivered Mail Returned to
// Sender" naming a pool partner the customer has never heard of and cannot act
// on. The worker resolved which send it is about (the id is in the report's
// body, which only the worker can read); all this has to do is ask whether that
// send was warmup.
//
// The report is still parsed and still emitted as its own event; refusing it
// here only keeps it out of the customer's mail. A report about a campaign send
// is left alone, because a bounce on real outreach is exactly what the unibox
// should show.
func (s *JobsService) isWarmupReport(ctx context.Context, e *models.JobEventNewEmail) (bool, error) {
	defer arrivaldiag.Begin(ctx, arrivaldiag.WarmupReport)()
	if s.WarmupRepo == nil || e.ReportOriginalMessageID == "" {
		return false, nil
	}
	// Matched on the id alone: the report's own subject and sender are the
	// mail server's, so the sender-and-subject fallback has nothing to match and
	// stays out of it. The warmup task's message_id is written before the send
	// leaves, so it is already there by the time any report about it can arrive.
	known, err := s.WarmupRepo.IsWarmupDelivery(ctx, e.Message.EmailID, "", e.ReportOriginalMessageID, "")
	if err != nil {
		// Held rather than stored: filing a report in the customer's mail is not
		// undoable, and the arrival is re-offered once the lookup works again.
		return false, fmt.Errorf("warmup report lookup: %w", err)
	}
	if known {
		log.Debug().
			Str("email_account_id", e.Message.EmailID.String()).
			Str("about", e.ReportOriginalMessageID).
			Msg("delivery report is about a warmup send; kept out of the unibox")
	}
	return known, nil
}

// fileOrReportWarmup hands warmup mail in an instance-executed mailbox to Cloud, which decides its engagement.
func (s *JobsService) fileOrReportWarmup(ctx context.Context, e *models.JobEventNewEmail) error {
	if s.CloudExec != nil {
		handled, err := s.CloudExec.ReportArrival(ctx, e.UserID, e.Message, warmupTokenFromMessage(e.Message), firstSenderAddress(e.Message.FromAddr))
		if err != nil {
			return fmt.Errorf("%w: report cloud warmup arrival: %w", errWarmupVerification, err)
		}
		if handled {
			if s.WarmupRecoveryRepo != nil {
				if err := s.WarmupRecoveryRepo.RememberMessage(ctx, e.Message.EmailID, e.Message.MessageID, e.Message.InternalDate); err != nil {
					return fmt.Errorf("%w: remember warmup message: %w", errWarmupVerification, err)
				}
			}
			return nil
		}
	}
	return s.fileWarmupOutOfMailbox(ctx, e)
}

// isKnownWarmupEmail separates inbox visibility from single-use recipient engagement.
func (s *JobsService) isKnownWarmupEmail(ctx context.Context, e *models.JobEventNewEmail) (bool, error) {
	defer arrivaldiag.Begin(ctx, arrivaldiag.WarmupKnown)()
	if s.WarmupRecoveryRepo != nil {
		known, err := s.WarmupRecoveryRepo.IsKnown(ctx, e.Message.EmailID, warmupTokenFromMessage(e.Message), []string{e.Message.MessageID})
		if err != nil || known {
			return known, err
		}
	}
	token, tokenErr := uuid.Parse(warmupTokenFromMessage(e.Message))
	sender := firstSenderAddress(e.Message.FromAddr)
	if s.WarmupRepo != nil {
		if tokenErr == nil {
			known, err := s.WarmupRepo.FindWarmupToken(ctx, token)
			if err != nil {
				return false, fmt.Errorf("warmup visibility token lookup: %w", err)
			}
			if known != nil && (known.SenderAccountID == e.Message.EmailID || known.RecipientAccountID == e.Message.EmailID) {
				return true, nil
			}
		}
		known, err := s.WarmupRepo.IsWarmupDelivery(ctx, e.Message.EmailID, sender, e.Message.MessageID, e.Message.Subject)
		if err != nil || known {
			return known, err
		}
	}
	if s.CloudLink == nil {
		return false, nil
	}
	enrolled, err := s.CloudLink.CheckEnrollment(ctx, e.Message.EmailID)
	if err != nil {
		return false, fmt.Errorf("cloud warmup enrollment lookup: %w", err)
	}
	if !enrolled {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, cloudWarmupCheckTimeout)
	defer cancel()
	if tokenErr == nil {
		known, err := s.CloudLink.VerifyWarmupToken(ctx, e.Message.EmailID, token.String())
		if err != nil {
			return false, fmt.Errorf("cloud warmup token verification: %w", err)
		}
		if known {
			return true, nil
		}
	}
	known, err := s.CloudLink.IsCloudWarmupDelivery(ctx, e.Message.EmailID, sender, e.Message.MessageID, e.Message.Subject)
	if err != nil {
		return false, fmt.Errorf("cloud warmup delivery verification: %w", err)
	}
	return known, nil
}

// isWarmupThreadReply recognises a message by what it answers.
//
// Everything above matches a token, a known Message-ID or a recent send's
// subject, and a reply typed by hand at a partner mailbox carries none of
// those: Gmail and Outlook compose a fresh id, prepend "Re:" and copy no
// custom header. What it does carry is In-Reply-To naming the warmup send,
// and that is enough. A pool partner is another Warmbly mailbox (on a
// self-hosted instance, usually the owner's own), so whoever typed it, the
// thread is warmup traffic and stays out of the unibox.
//
// A yes is recorded so the turn answering this one is recognised the same
// way. Nothing is consumed and nothing is engaged with: engagement is earned
// by verified deliveries only.
func (s *JobsService) isWarmupThreadReply(ctx context.Context, e *models.JobEventNewEmail) (bool, error) {
	defer arrivaldiag.Begin(ctx, arrivaldiag.WarmupThread)()
	parents := parentMessageIDs(e.Message.InReplyTo)
	if len(parents) == 0 {
		return false, nil
	}
	if s.WarmupRecoveryRepo != nil {
		known, err := s.WarmupRecoveryRepo.IsKnown(ctx, e.Message.EmailID, "", parents)
		if err != nil {
			return false, err
		}
		if known {
			if s.WarmupRepo != nil {
				if err := s.WarmupRepo.RecordWarmupThreadMessage(ctx, e.Message.EmailID, e.Message.MessageID); err != nil {
					return false, err
				}
			}
			return true, nil
		}
	}
	if s.WarmupRepo != nil {
		known, err := s.WarmupRepo.IsWarmupThreadReply(ctx, e.Message.EmailID, parents)
		if err != nil {
			return false, fmt.Errorf("warmup thread lookup: %w", err)
		}
		if known {
			if err := s.WarmupRepo.RecordWarmupThreadMessage(ctx, e.Message.EmailID, e.Message.MessageID); err != nil {
				log.Warn().Err(err).Str("email_id", e.Message.EmailID.String()).Msg("warmup thread turn not recorded; its reply will be matched on ancestry only")
			}
			return true, nil
		}
	}
	if s.CloudLink == nil {
		return false, nil
	}
	enrolled, err := s.CloudLink.CheckEnrollment(ctx, e.Message.EmailID)
	if err != nil {
		return false, fmt.Errorf("cloud warmup enrollment lookup: %w", err)
	}
	if !enrolled {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, cloudWarmupCheckTimeout)
	defer cancel()
	known, err := s.CloudLink.IsCloudWarmupThreadReply(ctx, e.Message.EmailID, e.Message.MessageID, parents)
	if err != nil {
		return false, fmt.Errorf("cloud warmup thread verification: %w", err)
	}
	return known, nil
}

// parentMessageIDs is In-Reply-To with the brackets and blanks gone. IMAP
// envelopes hand the header over as one string per id; Gmail and Graph
// already split it.
func parentMessageIDs(inReplyTo []string) []string {
	var out []string
	for _, raw := range inReplyTo {
		for _, id := range strings.Fields(raw) {
			if id = strings.Trim(id, "<>"); id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}

// firstSenderAddress pulls the bare address out of the first From value, in
// any form a sync has stored it ("Name <addr>", "Name (addr)", bare).
func firstSenderAddress(from []string) string {
	for _, raw := range from {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if addr := mailhdr.Bare(raw); strings.Contains(addr, "@") {
			return addr
		}
	}
	return ""
}

// acceptWarmupEmail consumes a verified token and runs everything that follows
// from a warmup email having arrived. Shared by both verification paths so the
// header and the header-less route cannot drift apart.
func (s *JobsService) acceptWarmupEmail(ctx context.Context, e *models.JobEventNewEmail, token *models.WarmupToken) error {
	landed := models.ClassifyWarmupLanding(e.Message.Folder, e.Message.Flags)

	// Record the receipt so a later deletion or spam-flag of THIS message can be
	// attributed back to warmup and to the sender. Verified warmup mail is not
	// stored in the unibox, so this is the only record that the message was a
	// warmup email.
	if e.Message != nil {
		if err := s.WarmupRepo.RecordWarmupReceived(ctx, e.Message.EmailID, e.Message.ID, e.Message.MessageID, token.SenderAccountID, landed == models.WarmupLandedSpam); err != nil {
			return fmt.Errorf("record warmup receipt: %w", err)
		}
	}
	if recorder, ok := s.WarmupPlacementRepo.(interface {
		RecordReceiptObservation(context.Context, uuid.UUID, uuid.UUID, string, []string, time.Time) error
	}); ok {
		if err := recorder.RecordReceiptObservation(ctx, e.Message.EmailID, e.Message.ID, e.Message.Folder, e.Message.Flags, time.Now().UTC()); err != nil {
			return fmt.Errorf("record warmup observation: %w", err)
		}
	}
	if lineage, ok := s.TaskRepo.(repository.WarmupLineageRepository); ok {
		if err := lineage.RecordVerifiedWarmupParent(ctx, token.TaskID, e.Message.EmailID, e.Message.ID, e.Message.ThreadID, warmupReceiptProof(e.Message)); err != nil {
			return err
		}
	}
	boosted := s.senderGetsRecoveryEngagement(ctx, token.SenderAccountID)
	if err := s.scheduleWarmupReplyBack(ctx, token, boosted, e.Message.EmailID, e.Message.ID); err != nil {
		return err
	}
	if err := s.WarmupRepo.ConsumeWarmupToken(ctx, token.Token); err != nil {
		return err
	}

	recipient := s.recipientAccount(ctx, e.Message.EmailID)

	// If the warmup mail arrived in a Junk/Spam state, record a
	// spam_placement event against the sender. This is distinct from a
	// user_complaint (which fires later via HandleFlagsAdd when a recipient
	// flags an already-delivered message) because nobody actively rejected
	// it — the provider classifier placed it there on arrival.
	if landed == models.WarmupLandedSpam && s.WarmupService != nil {
		// Record which recipient provider/domain filtered it into spam so the
		// placement signal can be segmented per provider, not one flat rate.
		provider, domain := recipientProviderDomain(recipient)
		health, _ := s.WarmupService.RecordSpamPlacement(ctx, e.Message.EmailID, token.SenderAccountID, e.Message.MessageID, token.ContentSource, provider, domain)
		s.markRiskBandFromWarmupHealth(ctx, token.SenderAccountID, health)
	}

	// Perform warmup actions
	rescued := s.performWarmupActions(ctx, e, recipient, boosted, landed == models.WarmupLandedSpam)

	s.recordWarmupPlacement(ctx, e, recipient, landed, rescued)
	return nil
}

// recordWarmupPlacement tells the recipient's workspace the arrival landed, adds
// it to the sender's daily placement history and tells the sender's workspace.
func (s *JobsService) recordWarmupPlacement(ctx context.Context, e *models.JobEventNewEmail, recipient *models.Email, landed string, rescued bool) {
	// The receipt is already stored, so the recipient's counts moved whatever the placement claim does.
	if s.StreamingPublisher != nil && recipient != nil && recipient.OrganizationID != nil {
		s.StreamingPublisher.PublishWarmupActivity(ctx, recipient.OrganizationID.String(), recipient.UserID, recipient.ID.String(), recipient.Email)
	}
	if s.WarmupPlacementRepo == nil || e.Message == nil {
		return
	}
	group, host := placementRecipient(recipient)
	sender, err := s.WarmupPlacementRepo.RecordPlacement(ctx, e.Message.EmailID, e.Message.ID, group, host, landed, rescued)
	if err != nil {
		log.Warn().Err(err).Str("email_id", e.Message.EmailID.String()).Msg("Failed to record warmup placement")
		return
	}
	// Nothing counted means nothing changed on anyone's dashboard.
	if sender == nil || sender.OrgID == nil || s.StreamingPublisher == nil {
		return
	}
	s.StreamingPublisher.PublishWarmupPlacement(ctx, sender.OrgID.String(), sender.UserID, sender.ID.String(), sender.Email, landed)
}

// placementRecipient is the group and host a receipt is counted under.
func placementRecipient(recipient *models.Email) (group, host string) {
	if recipient == nil {
		return models.WarmupRecipientOther, ""
	}
	// Resolved, not stored: a mailbox connected before host detection still
	// counts under the host partner selection keys it by.
	host = string(mailhost.ForMailbox(recipient.MailHost, recipient.Provider, recipient.Email))
	return models.WarmupRecipientGroup(host, recipient.Provider), host
}

// performWarmupActions publishes warmup action events to the worker. Action
// selection is probabilistic and per-mailbox (see engagementPlan) so the pool
// doesn't behave in detectable lockstep, with a randomised recipient-side
// dwell before the actions run. recipient is the receiving mailbox when the
// caller has it. Only mail that arrived in spam is rescued. Reports whether a
// spam rescue was handed to the worker.
func (s *JobsService) performWarmupActions(ctx context.Context, e *models.JobEventNewEmail, recipient *models.Email, senderBoosted, landedSpam bool) (rescueQueued bool) {
	if s.Publisher == nil {
		return false
	}

	settings := s.getGenerationSettings(ctx)
	engagement := settings.Engagement
	if senderBoosted {
		engagement = recoveryEngagement(engagement)
	}
	actions, delaySeconds := engagementPlan(e.Message.EmailID, engagement)
	if !landedSpam {
		actions = slices.DeleteFunc(actions, func(a string) bool { return a == models.WarmupActionRescueFromSpam })
	}

	base := models.WarmupEmailAction{
		UserID:             e.UserID,
		EmailID:            e.Message.EmailID,
		GmailID:            e.Message.GmailID,
		UID:                e.Message.UID,
		MailboxUIDValidity: e.Message.Mailbox,
		MailboxFolder:      e.Message.FolderPath,
		// Stable key so Graph accounts re-resolve the live message id at action
		// time (Graph ids change on move).
		RFCMessageID: e.Message.MessageID,
	}

	// Resolve the receiving mailbox once (worker routing, timezone for the
	// waking-hours engagement guard, and where its owner wants warmup filed).
	var workerID uuid.UUID
	routable := false
	var recipientTZ string
	if recipient == nil {
		recipient = s.recipientAccount(ctx, e.Message.EmailID)
	}
	if recipient != nil {
		workerID, routable = s.warmupActionWorker(ctx, recipient)
		recipientTZ = recipient.ClockTimezone()
		base.Placement, base.TargetFolder = recipient.WarmupFiling()
		actions = recipient.PermittedWarmupActions(actions)
	}
	// A mailbox whose owner wants warmup left in the inbox is not foldered.
	// Spam-rescue still runs: that is the reputation signal warmup exists for,
	// and it moves the mail to where this placement says it belongs anyway.
	if base.Placement == models.WarmupPlacementInbox || s.WarmupRecoveryRepo != nil {
		actions = slices.DeleteFunc(actions, func(a string) bool { return a == models.WarmupActionFile })
	}
	immediate, delayed := splitEngagementLegs(actions)

	if !routable {
		// No assigned worker (mid-migration / just-unassigned / assignment lag):
		// the warmup mail can't be foldered or engaged with. Log instead of
		// dropping silently so the gap is observable.
		log.Warn().
			Str("email_id", e.Message.EmailID.String()).
			Msg("Warmup actions skipped: recipient mailbox has no assigned worker")
		return false
	}

	// Immediate, durable leg (folder + spam-rescue): publish to the worker now.
	if len(immediate) > 0 {
		act := base
		act.Actions = immediate
		// Mark BEFORE publishing: the move can land, and its removal be
		// observed, before a marker written afterwards would exist. Either
		// action can move the message (the rescue is a move out of Junk on
		// every provider), and Graph reports a move exactly like a delete, so
		// both have to be excused or the mailbox is struck for foldering we
		// asked it to do.
		if hasAction(immediate, models.WarmupActionFile) || hasAction(immediate, models.WarmupActionRescueFromSpam) {
			s.markSelfMove(ctx, e.Message.EmailID, e.Message.MessageID)
		}
		rescueQueued = hasAction(immediate, models.WarmupActionRescueFromSpam)
		if rescueQueued {
			// Asks the worker to report what the rescue came to.
			act.InternalID = e.Message.ID.String()
		}
		s.Publisher.PublishWarmupAction(ctx, workerID, &act)
	}

	if len(delayed) == 0 {
		return rescueQueued
	}

	act := base
	act.Actions = delayed

	// Delayed leg (read / important / star): with no dwell (or no durable store
	// available) publish immediately; otherwise persist it to the durable
	// schedule so a worker restart mid-dwell can't drop it. The poller publishes
	// it when fire_at passes.
	if delaySeconds <= 0 || s.WarmupEngagementRepo == nil {
		s.Publisher.PublishWarmupAction(ctx, workerID, &act)
		return rescueQueued
	}

	payload, err := json.Marshal(act)
	if err != nil {
		log.Warn().Err(err).Str("email_id", e.Message.EmailID.String()).Msg("Failed to marshal delayed warmup engagement; publishing immediately")
		s.Publisher.PublishWarmupAction(ctx, workerID, &act)
		return rescueQueued
	}
	fireAt := humanizeFireAt(time.Now().Add(time.Duration(delaySeconds)*time.Second), recipientTZ)
	if err := s.WarmupEngagementRepo.EnqueuePendingEngagement(ctx, e.Message.EmailID, payload, fireAt); err != nil {
		log.Warn().Err(err).Str("email_id", e.Message.EmailID.String()).Msg("Failed to enqueue delayed warmup engagement; publishing immediately")
		s.Publisher.PublishWarmupAction(ctx, workerID, &act)
	}
	return rescueQueued
}

// sentFolderCopy reports whether this arrival is the mailbox's own copy of
// something it sent. Either source is trusted: Folder is what the worker
// resolved at sync time and ProviderFolder is where the provider still has it,
// and a sent copy only ever needs one of them to say so.
func sentFolderCopy(m *models.EmailMessageStoreData) bool {
	return models.NormalizeFolder(m.Folder, m.Flags) == models.FolderSent ||
		m.ProviderFolder == models.FolderSent
}

// recipientAccount best-effort loads the receiving mailbox; nil when it can't.
func (s *JobsService) recipientAccount(ctx context.Context, accountID uuid.UUID) *models.Email {
	if s.EmailRepository == nil {
		return nil
	}
	acc, err := s.mailbox(ctx, accountID)
	if err != nil {
		return nil
	}
	return acc
}

type mailboxMemoKey struct{}

type mailboxMemo struct {
	mu       sync.Mutex
	accounts map[uuid.UUID]*models.Email
}

// withMailboxMemo makes one event load each mailbox row once.
func withMailboxMemo(ctx context.Context) context.Context {
	return context.WithValue(ctx, mailboxMemoKey{}, &mailboxMemo{accounts: map[uuid.UUID]*models.Email{}})
}

// mailbox reads through the event's memo when there is one.
func (s *JobsService) mailbox(ctx context.Context, accountID uuid.UUID) (*models.Email, *errx.Error) {
	memo, _ := ctx.Value(mailboxMemoKey{}).(*mailboxMemo)
	if memo != nil {
		memo.mu.Lock()
		acc, ok := memo.accounts[accountID]
		memo.mu.Unlock()
		if ok {
			return acc, nil
		}
	}
	acc, err := s.EmailRepository.GetByID(ctx, accountID)
	if err == nil && acc != nil && memo != nil {
		memo.mu.Lock()
		memo.accounts[accountID] = acc
		memo.mu.Unlock()
	}
	return acc, err
}

// recipientProviderDomain is a recipient mailbox's connect provider
// ("gmail"/"outlook"/"smtp_imap") and email domain for the per-provider
// placement dimension. Empty strings when the account is unknown.
func recipientProviderDomain(acc *models.Email) (string, string) {
	if acc == nil {
		return "", ""
	}
	domain := ""
	if at := strings.LastIndex(acc.Email, "@"); at >= 0 {
		domain = strings.ToLower(acc.Email[at+1:])
	}
	return acc.Provider, domain
}

// containsSpamFlag checks if any flag is a spam flag.
func containsSpamFlag(flags []string) bool {
	return models.HasSpamFlag(flags)
}

// tagInboundMessage runs the optional automatic tagger for one arrival.
//
// Everything here is best-effort: the message is already stored and visible by
// the time this runs, so a tagging failure costs a label, never the mail. The
// campaign name and our previous message in the thread are looked up in code,
// because they are facts and a question about a fact is a question that can be
// answered confidently and wrongly.
func (s *JobsService) tagInboundMessage(ctx context.Context, e *models.JobEventNewEmail) {
	account := s.recipientAccount(ctx, e.Message.EmailID)
	if account == nil || account.OrganizationID == nil || *account.OrganizationID == uuid.Nil {
		return
	}
	orgID := *account.OrganizationID

	// Our previous message in the thread, read from the database rather than
	// asked. A reply is an answer, and the question it answers is not in it:
	// without this, "yes" and "that works" carry no meaning for the model.
	previous, campaign := s.InboxTagger.PreviousContext(ctx, e.Message.EmailID, e.Message.ThreadID, e.Message.InReplyTo, e.Message.InternalDate)

	msg := inboxtag.MessageFrom(orgID, e.UserID, e.Message, nil, previous, campaign)
	d, err := s.InboxTagger.Classify(ctx, msg)
	if err != nil {
		log.Warn().Err(err).
			Str("email_account_id", e.Message.EmailID.String()).
			Str("message_id", e.Message.MessageID).
			Msg("Inbox tagging failed; ingest kept")
		return
	}

	// Phases 2 and 3: what the verdict may do, per the workspace's switches.
	// Live arrivals only; the backfill labels history and never acts on it.
	s.actOnInboxTag(ctx, orgID, msg, d)
	if d.ActionRequired {
		s.notifyActionRequired(orgID, account.Email, e.Message)
	}

	// Tell the dashboard the message changed.
	//
	// The arrival event above this already fired, and it fired BEFORE the
	// labels existed: classifying makes a network call, so putting it ahead of
	// the arrival would hold every message back by the length of that call for
	// the sake of a chip. The mail therefore lands instantly and untagged, and
	// this second event is what makes the label appear a moment later without
	// anybody reloading. Without it the tag showed up on the next refetch,
	// which is a refresh, a scope change, or whenever the 30s cache went stale.
	if d.KindSource != "" {
		s.publishEmailUpdated(ctx, e.UserID, e.Message)
	}
}

// UniboxGate answers whether a workspace's plan opens the inbox.
type UniboxGate interface {
	CanUseUnibox(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error)
}

// notifyActionRequired tells the members who keep mailboxes running that one
// received mail needing action, since nobody may be reading that mailbox. The
// sender's subject stays out: this mail is phishing-shaped by selection.
func (s *JobsService) notifyActionRequired(orgID uuid.UUID, mailbox string, m *models.EmailMessageStoreData) {
	if s.Notifier == nil {
		return
	}
	title := "Message to review in a mailbox"
	if mailbox != "" {
		title = "Message to review in " + mailbox
	}
	notify := func(ctx context.Context) {
		// An unknown answer still notifies: a missed notice costs a paying workspace more.
		if s.UniboxAccess != nil {
			if ok, xerr := s.UniboxAccess.CanUseUnibox(ctx, orgID); xerr == nil && !ok {
				return
			}
		}
		s.Notifier.NotifyOrgAboutMessage(ctx, orgID, models.PermManageEmails|models.PermAccessUnibox, m.ID,
			models.NotifInboxActionRequired, title,
			"An incoming message was marked for review. Its claims are unverified. This is an inbox notice, not a Warmbly connection check. Review it in the inbox.",
			advanced.UniboxThreadLink(m.ThreadID), map[string]any{
				"email_account_id": m.EmailID.String(),
				"thread_id":        m.ThreadID,
			}, "inbox_action_required:"+m.ID.String())
	}
	// Off the ingest path: the fan-out is a round trip per member.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		notify(ctx)
	}()
}

// actOnInboxTag executes the actions the tagging policy allows for one
// verdict and records them on the row, so the review page shows the action
// next to the answer that caused it. The policy decides in inboxtag; the
// advanced service executes on the primitives a member's own click uses.
func (s *JobsService) actOnInboxTag(ctx context.Context, orgID uuid.UUID, msg inboxtag.Message, d inboxtag.Decision) {
	if s.AdvancedService == nil || d.Skipped() || d.Kind != inboxtag.KindHumanReply {
		return
	}
	settings, xerr := s.AdvancedService.GetOrganizationSettings(ctx, orgID)
	if xerr != nil || settings == nil {
		return
	}
	plan := inboxtag.PlanActions(d, settings.InboxTagging)
	if plan.Empty() {
		return
	}
	done := s.AdvancedService.ApplyInboxTagActions(ctx, advanced.InboxTagAction{
		OrganizationID: orgID,
		OwnerUserID:    msg.UserID,
		Sender:         msg.FromAddr,
		Subject:        msg.Subject,
		MessageID:      msg.MessageID,
		Plan:           plan,
		InReplyTo:      msg.InReplyTo,
		Headers:        msg.Headers,
	})
	if len(done) == 0 {
		return
	}
	if err := s.InboxTagger.RecordActions(ctx, orgID, msg.MessageID, done); err != nil {
		log.Warn().Err(err).Str("message_id", msg.MessageID).Msg("inbox tagging: actions not recorded")
	}
	log.Info().Str("message_id", msg.MessageID).Strs("actions", done).Msg("inbox tagging acted on a reply")
}

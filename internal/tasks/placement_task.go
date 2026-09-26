package tasks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

// SetPlacement wires the placement test store the probe handler reads.
func (s *tasksService) SetPlacement(repo repository.PlacementRepository) {
	s.placementRepo = repo
}

// HandlePlacementTask sends one placement probe: the test's copy, rendered the
// way the campaign would render it, from the test's sender to one seed. The
// copy carries nothing a real send would not (no marker, no warmup header,
// no CC or BCC); the probe is found again by its Message-ID.
func (s *tasksService) HandlePlacementTask(task *proto.ProcessTask) *errx.Error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	taskID, err := uuid.Parse(task.TaskId)
	if err != nil {
		return errx.New(errx.BadRequest, "invalid task ID")
	}
	if s.placementRepo == nil {
		return errx.New(errx.Internal, "placement tests are not configured")
	}
	record, err := s.taskRepo.GetTask(ctx, taskID)
	if err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}
	if record == nil || record.Status != "pending" {
		return nil
	}
	probe, err := s.placementRepo.GetProbeByTask(ctx, taskID)
	if err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}
	if probe == nil || probe.Result.Folder != models.PlacementFolderPending || probe.Test.Status != models.PlacementStatusRunning {
		_ = s.taskRepo.UpdateTaskStatus(ctx, taskID, "cancelled")
		return nil
	}
	fail := func(reason string) *errx.Error {
		_ = s.taskRepo.RecordTaskFailure(ctx, taskID, "Placement probe not sent", reason)
		_ = s.taskRepo.UpdateTaskStatus(ctx, taskID, "cancelled")
		if err := s.placementRepo.FailProbe(ctx, probe.Result.ID, reason); err != nil {
			errs.CaptureException(err)
		}
		return nil
	}

	test := probe.Test
	if test.SenderAccountID == nil || test.OrganizationID == nil {
		return fail("The test has no sender")
	}
	account, xerr := s.emailRepo.GetByID(ctx, *test.SenderAccountID)
	if xerr != nil || account == nil {
		return fail("The sending mailbox no longer exists")
	}
	// Tenancy: the sender, and everything rendered below, belongs to the test's
	// workspace.
	if account.OrganizationID == nil || *account.OrganizationID != *test.OrganizationID {
		return fail("The sending mailbox is not in this workspace")
	}
	if account.Status != "active" {
		return fail("The sending mailbox is not active")
	}
	// A probe is outbound mail from the same domains a suspension protects.
	if s.orgBlocksSending(ctx, account.OrganizationID) {
		return fail("Sending is suspended for this workspace")
	}

	msg, reason := s.renderPlacementProbe(ctx, taskID, &test, account, probe.Result.SeedAddress)
	if reason != "" {
		return fail(reason)
	}

	// The Message-ID is stored before the send, so the worker's answer (Gmail
	// and Graph restamp it) can only ever correct it, never be overwritten.
	if err := s.taskRepo.UpdateTaskMessageID(ctx, taskID, msg.MessageID); err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}
	if err := s.placementRepo.SetProbeMessageID(ctx, probe.Result.ID, msg.MessageID); err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}
	if err := s.taskRepo.UpdateTaskStatusWithLock(ctx, taskID, "active"); err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}
	if err := s.emailSender.Send(ctx, taskID, msg, *account); err != nil {
		switch {
		case errors.Is(err, ErrSendDispatchUnknown):
			// The bus may have taken it: leave the probe to the worker's answer
			// or to the classify window.
			_ = s.taskRepo.UpdateTaskStatusWithLock(ctx, taskID, "completed")
			_ = s.placementRepo.MarkProbeSent(ctx, probe.Result.ID, msg.MessageID, time.Now())
			return nil
		case errors.Is(err, ErrWorkerOffline), errors.Is(err, ErrWorkerUnconfirmed):
			return fail("The sending mailbox's worker is offline")
		default:
			return fail("The send could not be handed to a worker")
		}
	}
	if err := s.taskRepo.UpdateTaskStatusWithLock(ctx, taskID, "completed"); err != nil {
		errs.CaptureException(err)
	}
	if err := s.placementRepo.MarkProbeSent(ctx, probe.Result.ID, msg.MessageID, time.Now()); err != nil {
		errs.CaptureException(err)
	}
	log.Debug().Str("task_id", taskID.String()).Str("test_id", test.ID.String()).Msg("placement probe sent")
	return nil
}

// renderPlacementProbe builds one probe the way HandleCampaignTask builds a
// send, in the same order: form links, merge fields and spintax, AI blocks,
// plain-text rule, unsubscribe link, tracking, signature, opt-out footer,
// CSS inlining. What it leaves out is what only a real lead has: threading,
// A/B arm selection and the campaign's CC and BCC. The second result is why
// the probe cannot be sent, empty when it can.
func (s *tasksService) renderPlacementProbe(ctx context.Context, taskID uuid.UUID, test *models.PlacementTest, account *models.Email, recipient string) (EmailMessage, string) {
	orgID := *test.OrganizationID

	// An ad-hoc test renders against a campaign with the platform defaults, so
	// the opt-out and the List-Unsubscribe header match a new campaign.
	campaign := &models.Campaign{ID: uuid.Nil, OrganizationID: &orgID, UnsubscribeHeader: true}
	var sequenceID uuid.UUID
	if test.CampaignID != nil {
		c, err := s.campaignRepo.GetByID(ctx, *test.CampaignID)
		if err != nil || c == nil || c.OrganizationID == nil || *c.OrganizationID != orgID {
			return EmailMessage{}, "The campaign no longer exists"
		}
		campaign = c
	}
	if test.SequenceID != nil {
		sequenceID = *test.SequenceID
	}

	contact := testContact(recipient)
	realContact := false
	if test.ContactID != nil && s.contactRepo != nil {
		if found, xerr := s.contactRepo.GetByIDsAndOrganization(ctx, orgID, []uuid.UUID{*test.ContactID}); xerr == nil && len(found) == 1 {
			contact = found[0]
			realContact = true
		}
	}

	rawSubject, rawHTML, rawPlain := test.Subject, test.BodyHTML, test.BodyPlain
	if test.CampaignID != nil && realContact {
		s.resolveFormLinks(ctx, orgID, campaign, &contact, &rawSubject, &rawHTML, &rawPlain)
	} else {
		// No lead to personalise a form for: drop the markers, as the send path
		// does when a form cannot be resolved.
		for _, part := range []*string{&rawSubject, &rawHTML, &rawPlain} {
			*part = models.FormLinkMarkerRE.ReplaceAllString(*part, "")
		}
	}

	optOut := s.resolveOptOut(ctx, orgID, campaign)
	var unsubscribeURL string
	if s.unsubLinks != nil && s.unsubLinks.Enabled() {
		// No contact: clicking it can never suppress anyone.
		unsubscribeURL = s.mintUnsubscribeLink(ctx, resolveOptOutOrigin(account, campaign), orgID, campaign.ID, uuid.Nil)
	}
	extra := map[string]string{UnsubscribeLinkVar: unsubscribeURL}
	subject := expandSpintax(RenderTemplateWith(rawSubject, contact, extra))
	bodyHTML := expandSpintax(RenderTemplateWith(rawHTML, contact, extra))
	bodyPlain := expandSpintax(RenderTemplateWith(rawPlain, contact, extra))
	if bodyPlain == "" && bodyHTML != "" {
		bodyPlain = ExtractPlainTextFromHTML(bodyHTML)
	}

	// AI blocks resolve for the chosen lead through the send path's own cache,
	// so every probe reads the copy that lead would get.
	if realContact && test.CampaignID != nil && sequenceID != uuid.Nil && s.aiProvider != nil && s.aiCredits != nil {
		var err error
		subject, bodyHTML, bodyPlain, err = s.resolveAIVariables(ctx, campaign, &contact, sequenceID, subject, bodyHTML, bodyPlain)
		if err != nil {
			return EmailMessage{}, "The AI blocks in the copy could not be generated"
		}
	}

	// With no lead there is nothing to generate for, so the blocks drop out
	// the way a block that cannot be generated does.
	if !realContact {
		subject = aiVarTokenRE.ReplaceAllString(subject, "")
		bodyHTML = aiVarTokenRE.ReplaceAllString(aiVarSpanRE.ReplaceAllString(bodyHTML, ""), "")
		bodyPlain = aiVarTokenRE.ReplaceAllString(bodyPlain, "")
	}

	if campaign.TextOnly {
		bodyHTML = ""
	}
	bodyHTML = dropBlankHTMLPart(bodyHTML, bodyPlain)
	if !mailhtml.HasContent(bodyHTML) && bodyPlain == "" {
		return EmailMessage{}, "The copy rendered empty"
	}
	bodyHTML = linkifyUnsubscribeURL(bodyHTML, unsubscribeURL, optOut.LinkText)

	trackingDomain, _ := resolveTrackingHost(config.TrackingHost(), account, campaign)
	openTracking := test.OpenTracking && bodyHTML != "" && trackingDomain != ""
	linkTracking := test.LinkTracking && bodyHTML != "" && trackingDomain != ""
	if openTracking {
		bodyHTML = AddOpenTrackingPixel(bodyHTML, taskID, trackingDomain)
	}
	if bodyHTML != "" && (linkTracking || campaign.UTMTracking) {
		opts := LinkTracking{
			TaskID:         taskID,
			CampaignID:     campaign.ID,
			TrackingDomain: trackingDomain,
			Wrap:           linkTracking,
			UTM:            CampaignUTM(campaign),
		}
		tracked, links := TrackLinks(bodyHTML, opts)
		switch {
		case len(links) == 0:
			bodyHTML = tracked
		case s.trackedLinkRepo == nil:
			bodyHTML, _ = TrackLinks(bodyHTML, LinkTracking{TrackingDomain: trackingDomain, UTM: opts.UTM})
		default:
			if err := s.trackedLinkRepo.CreateBatch(ctx, links); err != nil {
				bodyHTML, _ = TrackLinks(bodyHTML, LinkTracking{TrackingDomain: trackingDomain, UTM: opts.UTM})
			} else {
				bodyHTML = tracked
			}
		}
	}
	if campaign.UTMTracking && bodyPlain != "" {
		bodyPlain = TagPlainTextLinks(bodyPlain, CampaignUTM(campaign), trackingDomain)
	}

	if account.SignatureSync {
		if bodyHTML != "" {
			bodyHTML = AddSignature(bodyHTML, account.SignatureHTML, true)
		}
		if bodyPlain != "" {
			bodyPlain = AddSignature(bodyPlain, account.SignaturePlain, false)
		}
	}
	bodyHTML, bodyPlain = appendOptOut(bodyHTML, bodyPlain, optOut, unsubscribeURL)
	bodyHTML = mailhtml.InlineCSS(bodyHTML)

	if s.cipherService != nil {
		if _, err := s.cipherService.Cipher(ctx, orgID); err != nil {
			errs.CaptureException(fmt.Errorf("placement probe: organization key: %w", err))
			return EmailMessage{}, "The workspace's encryption key is unavailable"
		}
	}

	var tracking *models.TrackingInfo
	if openTracking || linkTracking {
		tracking = &models.TrackingInfo{OpenTracking: openTracking, LinkTracking: linkTracking, TrackingDomain: trackingDomain}
	}
	headerURL := ""
	if campaign.UnsubscribeHeader {
		headerURL = unsubscribeURL
	}
	var attachments []models.AttachmentRef
	if test.CampaignID != nil {
		attachments = s.campaignAttachmentRefs(ctx, campaign.ID, sequenceID)
	}
	return EmailMessage{
		From:           account.Email,
		To:             []string{recipient},
		Subject:        subject,
		BodyHTML:       bodyHTML,
		BodyPlain:      bodyPlain,
		MessageID:      generateMessageID(account.SendFrom()),
		Tracking:       tracking,
		UnsubscribeURL: headerURL,
		Attachments:    attachments,
	}, ""
}

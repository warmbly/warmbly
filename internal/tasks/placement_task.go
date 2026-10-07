package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/cipher"
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
	if err := s.sendAdmission(ctx, account); err != nil {
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

// placementBase is a probe's copy with everything but tracking resolved: what
// both halves of a tracking comparison send to one seed. A refusal is frozen
// too, so a pair fails together instead of one half going out alone.
type placementBase struct {
	Subject        string                     `json:"subject"`
	BodyHTML       string                     `json:"body_html"`
	BodyPlain      string                     `json:"body_plain"`
	UnsubscribeURL string                     `json:"unsubscribe_url,omitempty"`
	HeaderURL      string                     `json:"header_url,omitempty"`
	SignatureHTML  string                     `json:"signature_html,omitempty"`
	SignaturePlain string                     `json:"signature_plain,omitempty"`
	OptOut         models.UnsubscribeSettings `json:"opt_out"`
	TrackingDomain string                     `json:"tracking_domain,omitempty"`
	CampaignID     uuid.UUID                  `json:"campaign_id"`
	Attachments    []models.AttachmentRef     `json:"attachments,omitempty"`
	Refusal        string                     `json:"refusal,omitempty"`
}

// renderPlacementProbe builds one probe: the base copy, then the tracking the
// test's half carries. The second result is why the probe cannot be sent,
// empty when it can.
func (s *tasksService) renderPlacementProbe(ctx context.Context, taskID uuid.UUID, test *models.PlacementTest, account *models.Email, recipient string) (EmailMessage, string) {
	var sealer *cipher.Cipher
	if s.cipherService != nil {
		c, err := s.cipherService.Cipher(ctx, *test.OrganizationID)
		if err != nil {
			errs.CaptureException(fmt.Errorf("placement probe: organization key: %w", err))
			return EmailMessage{}, "The workspace's encryption key is unavailable"
		}
		sealer = c
	}
	base := s.placementBaseFor(ctx, test, account, recipient, sealer)
	if base.Refusal != "" {
		return EmailMessage{}, base.Refusal
	}
	bodyHTML, bodyPlain, tracking := s.finishPlacementProbe(ctx, base, taskID, test.OpenTracking, test.LinkTracking)
	return EmailMessage{
		From:           account.Email,
		To:             []string{recipient},
		Subject:        base.Subject,
		BodyHTML:       bodyHTML,
		BodyPlain:      bodyPlain,
		MessageID:      generateMessageID(account.SendFrom()),
		Tracking:       tracking,
		UnsubscribeURL: base.HeaderURL,
		Attachments:    base.Attachments,
	}, ""
}

// placementBaseFor is the base copy of one probe. The halves of a tracking
// comparison share one per seed: the first to run renders and freezes it, and
// the other half, like any retry, sends what was frozen.
func (s *tasksService) placementBaseFor(ctx context.Context, test *models.PlacementTest, account *models.Email, recipient string, sealer *cipher.Cipher) placementBase {
	if test.CompareGroupID == nil {
		return s.renderPlacementBase(ctx, test, account, recipient)
	}
	if sealer == nil {
		return placementBase{Refusal: "The workspace's encryption key is unavailable"}
	}
	orgID, group := *test.OrganizationID, *test.CompareGroupID
	sealed, err := s.placementRepo.GetPlacementRender(ctx, orgID, group, recipient)
	if err != nil {
		errs.CaptureException(fmt.Errorf("placement probe: read frozen copy: %w", err))
		return placementBase{Refusal: "The comparison's copy could not be read"}
	}
	if sealed == "" {
		raw, err := json.Marshal(s.renderPlacementBase(ctx, test, account, recipient))
		if err == nil {
			sealed, err = sealer.Encrypt(ctx, string(raw))
		}
		if err == nil {
			sealed, err = s.placementRepo.FreezePlacementRender(ctx, orgID, group, recipient, sealed)
		}
		if err != nil {
			errs.CaptureException(fmt.Errorf("placement probe: freeze copy: %w", err))
			return placementBase{Refusal: "The comparison's copy could not be stored"}
		}
	}
	// Both halves read the stored copy, the one that rendered it included.
	var base placementBase
	raw, err := sealer.Decrypt(ctx, sealed)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &base)
	}
	if err != nil {
		errs.CaptureException(fmt.Errorf("placement probe: open frozen copy: %w", err))
		return placementBase{Refusal: "The comparison's copy could not be read"}
	}
	return base
}

// renderPlacementBase resolves a probe's copy the way HandleCampaignTask
// builds a send, in the same order: form links, merge fields and spintax, AI
// blocks, plain-text rule, unsubscribe link and UTM tags, plus the signature,
// opt-out and attachments that go on after tracking. What it leaves out is
// what only a real lead has: threading, A/B arm selection and the campaign's
// CC and BCC.
func (s *tasksService) renderPlacementBase(ctx context.Context, test *models.PlacementTest, account *models.Email, recipient string) placementBase {
	orgID := *test.OrganizationID

	// An ad-hoc test renders against a campaign with the platform defaults, so
	// the opt-out and the List-Unsubscribe header match a new campaign.
	campaign := &models.Campaign{ID: uuid.Nil, OrganizationID: &orgID, UnsubscribeHeader: true}
	var sequenceID uuid.UUID
	if test.CampaignID != nil {
		c, err := s.campaignRepo.GetByID(ctx, *test.CampaignID)
		if err != nil || c == nil || c.OrganizationID == nil || *c.OrganizationID != orgID {
			return placementBase{Refusal: "The campaign no longer exists"}
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
	extra := templateContext(account, unsubscribeURL)
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
			return placementBase{Refusal: "The AI blocks in the copy could not be generated"}
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
		return placementBase{Refusal: "The copy rendered empty"}
	}
	bodyHTML = linkifyUnsubscribeURL(bodyHTML, unsubscribeURL, optOut.LinkText)

	// UTM tags are the campaign's own links, not tracking, so both halves of a
	// comparison carry them.
	trackingDomain, _ := resolveTrackingHost(config.TrackingHost(), account, campaign)
	if utm := CampaignUTM(campaign); utm != nil {
		if bodyHTML != "" {
			bodyHTML, _ = TrackLinks(bodyHTML, LinkTracking{TrackingDomain: trackingDomain, UTM: utm})
		}
		bodyPlain = TagPlainTextLinks(bodyPlain, utm, trackingDomain)
	}

	base := placementBase{
		Subject:        subject,
		BodyHTML:       bodyHTML,
		BodyPlain:      bodyPlain,
		UnsubscribeURL: unsubscribeURL,
		OptOut:         optOut,
		TrackingDomain: trackingDomain,
		CampaignID:     campaign.ID,
	}
	if account.SignatureSync {
		base.SignatureHTML, base.SignaturePlain = account.SignatureHTML, account.SignaturePlain
	}
	if campaign.UnsubscribeHeader {
		base.HeaderURL = unsubscribeURL
	}
	if test.CampaignID != nil {
		base.Attachments = s.campaignAttachmentRefs(ctx, campaign.ID, sequenceID)
	}
	return base
}

// finishPlacementProbe turns a base copy into what one half sends: the open
// pixel and wrapped links when that half is tracked, then the signature,
// opt-out footer and CSS inlining every copy gets. Tracking is the only input
// that differs between the halves of a comparison.
func (s *tasksService) finishPlacementProbe(ctx context.Context, base placementBase, taskID uuid.UUID, open, link bool) (string, string, *models.TrackingInfo) {
	bodyHTML, bodyPlain := base.BodyHTML, base.BodyPlain
	openTracking := open && bodyHTML != "" && base.TrackingDomain != ""
	linkTracking := link && bodyHTML != "" && base.TrackingDomain != ""
	if openTracking {
		bodyHTML = AddOpenTrackingPixel(bodyHTML, taskID, base.TrackingDomain)
	}
	if linkTracking {
		tracked, links := TrackLinks(bodyHTML, LinkTracking{
			TaskID:         taskID,
			CampaignID:     base.CampaignID,
			TrackingDomain: base.TrackingDomain,
			Wrap:           true,
		})
		// A body carries tickets only once they are stored; otherwise its links
		// stay as written.
		if len(links) > 0 && s.trackedLinkRepo != nil && s.trackedLinkRepo.CreateBatch(ctx, links) == nil {
			bodyHTML = tracked
		}
	}

	if bodyHTML != "" {
		bodyHTML = AddSignature(bodyHTML, base.SignatureHTML, true)
	}
	if bodyPlain != "" {
		bodyPlain = AddSignature(bodyPlain, base.SignaturePlain, false)
	}
	bodyHTML, bodyPlain = appendOptOut(bodyHTML, bodyPlain, base.OptOut, base.UnsubscribeURL)
	bodyHTML = mailhtml.InlineCSS(bodyHTML)

	var tracking *models.TrackingInfo
	if openTracking || linkTracking {
		tracking = &models.TrackingInfo{OpenTracking: openTracking, LinkTracking: linkTracking, TrackingDomain: base.TrackingDomain}
	}
	return bodyHTML, bodyPlain, tracking
}

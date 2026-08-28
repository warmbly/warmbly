package campaign

import (
	"context"
	"errors"
	"fmt"
	"github.com/rs/zerolog/log"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/dailythrottle"
	"github.com/warmbly/warmbly/internal/app/listgate"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/trackdns"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
	"github.com/warmbly/warmbly/internal/tasks"
	"github.com/warmbly/warmbly/internal/tasks/proto"
	"github.com/warmbly/warmbly/internal/utils/paging"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

func (s *campaignService) Create(ctx context.Context, userID string, orgID *uuid.UUID, data *models.CreateCampaign) (*models.Campaign, *errx.Error) {
	if err := validate.CampaignName(data.Name); err != nil {
		return nil, err
	}
	if err := validate.CampaignDescription(data.Description); err != nil {
		return nil, err
	}

	// A campaign carries its workspace from creation: suppression, the
	// entitlement gate and the throttle below are all org-scoped, and a row
	// without one would skip every one of them at send time.
	if orgID == nil {
		return nil, errx.ErrNoOrganization
	}

	// Daily creation throttle (config.DailyThrottleNewCampaigns). Caps
	// per-day new-campaign rate per org so an unlimited plan can't be
	// abused to ramp instantly.
	if s.throttle != nil {
		if xerr := s.throttle.CheckAndIncrement(ctx, *orgID, dailythrottle.ResourceCampaign, config.DailyThrottleNewCampaigns); xerr != nil {
			return nil, xerr
		}
	}

	resp, xerr := s.campaignRepository.Create(ctx, userID, orgID, data)
	if xerr != nil {
		return nil, xerr
	}

	if s.streamingPublisher != nil {
		s.streamingPublisher.PublishCampaignEvent(ctx, &pubsub.CampaignEvent{
			BaseEvent: pubsub.BaseEvent{
				EventType: pubsub.EventCampaignCreated,
				UserID:    userID,
			},
			OrgID:      modelOrgID(orgID),
			CampaignID: resp.ID.String(),
			Name:       resp.Name,
			Status:     resp.Status,
		})
	}

	return resp, nil
}

func (s *campaignService) Get(ctx context.Context, orgID, id string) (*models.Campaign, *errx.Error) {
	resp, err := s.campaignRepository.Get(ctx, orgID, id)
	if err != nil {
		if errors.Is(err, errx.ErrResourceNotFound) {
			return nil, errx.ErrNotFound
		}

		return nil, errx.InternalError()
	}

	return resp, nil
}

func (s *campaignService) Search(ctx context.Context, orgID, query, cursor, folder, status, limit string) (*models.CampaignsResult, *errx.Error) {
	cursorId, err := paging.DecodeCursor(cursor)
	if err != nil {
		return nil, err
	}
	folderId, err := validate.Uuid(folder)
	if err != nil {
		return nil, err
	}
	limitN, err := validate.Limit(limit)
	if err != nil {
		return nil, err
	}
	switch status {
	case "", "draft", "active", "paused", "completed":
	default:
		return nil, errx.New(errx.BadRequest, "invalid status filter: must be draft, active, paused, or completed")
	}

	resp, xerr := s.campaignRepository.Search(ctx, orgID, query, cursorId, folderId, status, limitN)
	if xerr != nil {
		return nil, errx.InternalError()
	}

	return resp, nil
}

func (s *campaignService) Overview(ctx context.Context, orgID string) (*models.CampaignsOverview, *errx.Error) {
	resp, err := s.campaignRepository.Overview(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	return resp, nil
}

func (s *campaignService) Update(ctx context.Context, userID, query string, data *models.UpdateCampaign) (*models.Campaign, *errx.Error) {
	resp, err := s.campaignRepository.Update(ctx, userID, query, data)
	if err != nil {
		return nil, err
	}
	// A schedule edit on a running campaign must move its parked wakeup task:
	// otherwise a cleared or earlier start date only takes effect when the old
	// slot fires, and the campaign looks stuck until then (issue #171).
	if data.TouchesSchedule() && resp != nil && resp.Status == "active" {
		s.rescheduleCampaignWakeup(ctx, resp.ID)
	}
	return resp, nil
}

// rescheduleCampaignWakeup drops the campaign's parked pending tasks and seeds
// a fresh wakeup from the just-saved schedule. Best-effort: enqueue handles the
// no-mailbox/completed cases itself, and the campaign reconciler re-seeds any
// chain left without a task.
func (s *campaignService) rescheduleCampaignWakeup(ctx context.Context, campaignID uuid.UUID) {
	if s.taskRepo != nil {
		if pending, err := s.campaignRepository.GetPendingCampaignTasks(ctx, campaignID); err == nil {
			for _, t := range pending {
				_ = s.taskRepo.DeleteTask(ctx, t.ID)
			}
		}
	}
	_ = s.enqueueCampaignWakeup(ctx, campaignID)
}

func (s *campaignService) Delete(ctx context.Context, orgID uuid.UUID, campaignID string) (*models.Campaign, *errx.Error) {
	campaign, cID, xerr := s.campaignForOrg(ctx, orgID, campaignID)
	if xerr != nil {
		return nil, xerr
	}

	// Attachment objects are not reachable once the rows cascade, so list
	// them first and drop the bytes after the delete has committed.
	var attachments []models.CampaignAttachment
	if s.attachmentRepo != nil {
		var err error
		if attachments, err = s.attachmentRepo.ListByCampaign(ctx, cID); err != nil {
			sentry.CaptureException(fmt.Errorf("campaign %s delete: list attachments: %w", cID, err))
		}
	}

	if err := s.campaignRepository.Delete(ctx, cID); err != nil {
		if errors.Is(err, errx.ErrResourceNotFound) {
			return nil, errx.ErrNotFound
		}
		return nil, errx.InternalError()
	}

	if s.storage != nil {
		for _, att := range attachments {
			if err := s.storage.Delete(ctx, att.S3Key); err != nil {
				sentry.CaptureException(fmt.Errorf("campaign %s delete: object %s: %w", cID, att.S3Key, err))
			}
		}
	}

	if s.streamingPublisher != nil {
		s.streamingPublisher.PublishCampaignEvent(ctx, &pubsub.CampaignEvent{
			BaseEvent: pubsub.BaseEvent{
				EventType: pubsub.EventCampaignDeleted,
				UserID:    campaign.UserID,
			},
			OrgID:      orgID.String(),
			CampaignID: cID.String(),
			Name:       campaign.Name,
			Status:     campaign.Status,
		})
	}

	return campaign, nil
}

func (s *campaignService) Duplicate(ctx context.Context, orgID, userID uuid.UUID, campaignID, name string) (*models.Campaign, *errx.Error) {
	source, cID, xerr := s.campaignForOrg(ctx, orgID, campaignID)
	if xerr != nil {
		return nil, xerr
	}

	if name == "" {
		name = duplicateName(source.Name)
	}
	if xerr := validate.CampaignName(name); xerr != nil {
		return nil, xerr
	}

	// A copy is a new campaign, so it spends the same daily creation budget.
	if s.throttle != nil {
		if xerr := s.throttle.CheckAndIncrement(ctx, orgID, dailythrottle.ResourceCampaign, config.DailyThrottleNewCampaigns); xerr != nil {
			return nil, xerr
		}
	}

	newID := uuid.New()
	copied, cleanup, xerr := s.copyAttachments(ctx, orgID, cID, newID)
	if xerr != nil {
		return nil, xerr
	}

	campaign, err := s.campaignRepository.Duplicate(ctx, repository.DuplicateCampaignInput{
		SourceID:    cID,
		NewID:       newID,
		UserID:      userID,
		Name:        name,
		Attachments: copied,
	})
	if err != nil {
		cleanup()
		if errors.Is(err, errx.ErrResourceNotFound) {
			return nil, errx.ErrNotFound
		}
		return nil, errx.InternalError()
	}

	if s.campaignLogRepo != nil {
		_ = s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
			CampaignID: newID,
			EventType:  "created",
			Message:    fmt.Sprintf("Duplicated from %q", source.Name),
			Metadata:   map[string]interface{}{"source_campaign_id": cID.String()},
		})
	}

	if s.streamingPublisher != nil {
		s.streamingPublisher.PublishCampaignEvent(ctx, &pubsub.CampaignEvent{
			BaseEvent: pubsub.BaseEvent{
				EventType: pubsub.EventCampaignCreated,
				UserID:    userID.String(),
			},
			OrgID:      orgID.String(),
			CampaignID: campaign.ID.String(),
			Name:       campaign.Name,
			Status:     campaign.Status,
		})
	}

	return campaign, nil
}

// copyAttachments writes a copy of every attachment object of src under dst
// and returns the rows to insert plus a best-effort undo for when the copy
// transaction fails. The copies count against the organization's storage
// quota exactly like an upload would. An attachment whose bytes cannot be
// read is reported and skipped rather than failing the whole duplicate.
func (s *campaignService) copyAttachments(ctx context.Context, orgID, src, dst uuid.UUID) ([]models.CampaignAttachment, func(), *errx.Error) {
	noop := func() {}
	if s.attachmentRepo == nil || s.storage == nil {
		return nil, noop, nil
	}
	sources, err := s.attachmentRepo.ListByCampaign(ctx, src)
	if err != nil {
		return nil, noop, errx.InternalError()
	}
	if len(sources) == 0 {
		return nil, noop, nil
	}

	if s.featureGate != nil {
		limit, xerr := s.featureGate.GetStorageLimitBytes(ctx, orgID)
		if xerr != nil {
			return nil, noop, xerr
		}
		used, err := s.attachmentRepo.SumStorageUsedByOrg(ctx, orgID)
		if err != nil {
			return nil, noop, errx.InternalError()
		}
		var adding int64
		for _, att := range sources {
			adding += att.Size
		}
		if used+adding > limit {
			const mb = 1024 * 1024
			return nil, noop, errx.New(errx.BadRequest, fmt.Sprintf(
				"duplicating would exceed your storage limit (%d MB of %d MB used, %d MB of attachments to copy): remove attachments or upgrade your plan",
				used/mb, limit/mb, adding/mb))
		}
	}

	copied := make([]models.CampaignAttachment, 0, len(sources))
	for _, att := range sources {
		body, err := s.storage.Get(ctx, att.S3Key)
		if err != nil {
			sentry.CaptureException(fmt.Errorf("campaign %s duplicate: read %s: %w", src, att.S3Key, err))
			continue
		}
		key := models.AttachmentObjectKey(dst, att.Filename)
		err = s.storage.Put(ctx, key, body, att.MimeType)
		body.Close()
		if err != nil {
			sentry.CaptureException(fmt.Errorf("campaign %s duplicate: write %s: %w", src, key, err))
			continue
		}
		att.S3Key = key
		copied = append(copied, att)
	}
	return copied, func() {
		for _, att := range copied {
			if err := s.storage.Delete(ctx, att.S3Key); err != nil {
				sentry.CaptureException(fmt.Errorf("campaign %s duplicate undo: object %s: %w", src, att.S3Key, err))
			}
		}
	}, nil
}

// copySuffix matches a name that is already a copy: "X (copy)" or "X (copy 3)".
var copySuffix = regexp.MustCompile(`^(.*?) \(copy(?: (\d+))?\)$`)

// duplicateName derives the copy's name inside the 50 character cap. A copy
// of a copy counts up ("X (copy 2)") instead of stacking suffixes.
func duplicateName(source string) string {
	const maxLen = 50
	base := strings.TrimSpace(source)
	suffix := " (copy)"
	if m := copySuffix.FindStringSubmatch(base); m != nil {
		n := 2
		if m[2] != "" {
			if parsed, err := strconv.Atoi(m[2]); err == nil {
				n = parsed + 1
			}
		}
		base = m[1]
		suffix = fmt.Sprintf(" (copy %d)", n)
	}
	if len(base)+len(suffix) > maxLen {
		base = strings.TrimSpace(truncateUTF8(base, maxLen-len(suffix)))
	}
	return base + suffix
}

// truncateUTF8 cuts s to at most n bytes without splitting a rune.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func (s *campaignService) StartCampaign(ctx context.Context, orgID uuid.UUID, campaignID string) *errx.Error {
	cID, parseErr := uuid.Parse(campaignID)
	if parseErr != nil {
		return errx.ErrUuid
	}

	// Get campaign
	campaign, err := s.campaignRepository.GetByID(ctx, cID)
	if err != nil {
		return errx.InternalError()
	}
	if campaign == nil {
		return errx.ErrNotFound
	}

	// Verify it belongs to the org
	if campaign.OrganizationID == nil || *campaign.OrganizationID != orgID {
		return errx.ErrNotFound
	}

	// Verify status allows starting. paused_guardrail is included: an
	// auto-pause is meant to be reviewed and then explicitly restarted, not to
	// become a dead end the owner cannot recover from. completed is included
	// for the same reason: a campaign closed by a passed end date must be
	// restartable once the date is extended or cleared, and a truly finished
	// one just re-completes in enqueueCampaignWakeup with a clear message.
	startable := map[string]bool{
		"draft": true, "paused": true, "paused_no_accounts": true,
		"paused_guardrail": true, "completed": true,
	}
	if !startable[campaign.Status] {
		return errx.New(errx.BadRequest, "campaign must be in draft, paused, or completed status to start")
	}

	// Check cooldown
	if campaign.LastStatusChangeAt != nil {
		elapsed := time.Since(*campaign.LastStatusChangeAt)
		if elapsed.Seconds() < campaignCooldownSeconds {
			return errx.New(errx.BadRequest, "please wait before changing campaign status")
		}
	}

	// Check feature gate
	if s.featureGate != nil {
		canSend, xerr := s.featureGate.CanSendCampaignEmail(ctx, orgID)
		if xerr != nil {
			return xerr
		}
		if !canSend {
			return errx.New(errx.Forbidden, "your plan does not allow sending campaign emails")
		}
	}

	// Validate campaign readiness
	if err := s.campaignRepository.ValidateCampaignReady(ctx, cID); err != nil {
		var bizErr *errx.Error
		if errors.As(err, &bizErr) {
			return bizErr
		}
		return errx.InternalError()
	}

	// Sender-pool validity (explicit senders OR tags OR "all" fallback) is part
	// of ValidateCampaignReady above, so no separate strategy-gated check here.

	// Block start if any step's template is malformed. Without this, a broken
	// conditional (e.g. an {{if}} with no {{end}}) silently degrades to literal
	// template text in the sent email — better to catch it here with a clear,
	// step-scoped error than to ship {{if ...}} to recipients.
	if seqs, serr := s.campaignRepository.GetSequencesByCampaignID(ctx, cID); serr == nil {
		for i, seq := range seqs {
			for _, f := range []struct {
				name, val string
			}{{"subject", seq.Subject}, {"body", seq.BodyHTML}, {"plain-text body", seq.BodyPlain}} {
				if terr := tasks.TemplateError(f.val); terr != nil {
					return errx.New(errx.BadRequest, fmt.Sprintf(
						"Step %d's %s has a template error — fix the {{if}}/{{end}} or {{eq}} syntax before starting.",
						i+1, f.name,
					))
				}
			}
		}
	}

	// Refuse a launch whose list is known to be largely undeliverable. Only
	// KNOWN-invalid addresses count: a list nobody has verified is not evidence
	// of a bad list, and blocking on that would refuse nearly every launch.
	if s.audienceRepo != nil {
		audience, aerr := s.audienceRepo.GetCampaignAudience(ctx, orgID, cID)
		switch {
		case aerr != nil:
			// Fail open, but never silently: a transient database error must
			// not stop a customer launching, and it must not look like the
			// list passed either.
			log.Warn().Err(aerr).Str("campaign_id", cID.String()).
				Msg("could not measure the campaign's list; launching without the check")
		default:
			if verdict := listgate.Project(audience); verdict.Block {
				return errx.New(errx.BadRequest, verdict.Summary+" "+verdict.Remediation)
			}
		}
	}

	activeCampaigns, err := s.campaignRepository.CountActiveForOrganization(ctx, orgID)
	if err != nil {
		return errx.InternalError()
	}
	if activeCampaigns >= config.HardCapCampaignsActive {
		return errx.New(errx.Forbidden, "You have 50 active campaigns. Contact us if you need to run more.")
	}

	// Set status to active
	if err := s.campaignRepository.StartCampaign(ctx, cID); err != nil {
		return errx.InternalError()
	}

	if xerr := s.enqueueCampaignWakeup(ctx, cID); xerr != nil {
		return xerr
	}

	// Log campaign started
	if s.campaignLogRepo != nil {
		s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
			CampaignID: cID,
			EventType:  "started",
			Message:    "Campaign started",
		})
	}

	// Publish realtime event
	if s.streamingPublisher != nil {
		s.streamingPublisher.PublishCampaignEvent(ctx, &pubsub.CampaignEvent{
			BaseEvent: pubsub.BaseEvent{
				EventType: pubsub.EventCampaignStarted,
				UserID:    campaign.UserID,
			},
			OrgID:      modelOrgID(campaign.OrganizationID),
			CampaignID: cID.String(),
			Name:       campaign.Name,
			Status:     "active",
		})
	}

	return nil
}

// WakeCampaigns implements the interface comment on CampaignService: after
// leads are attached to a campaign, pull its parked wakeup forward if the
// campaign can now act sooner than where it sits.
//
// It only ever moves a wakeup EARLIER, and only when the parked one is beyond
// the deferral horizon, so a campaign already ticking on its send pacing is left
// alone. Everything here is best effort: the reconciler and the capped deferral
// horizon are the backstops, so a failure delays the new leads rather than
// losing them.
func (s *campaignService) WakeCampaigns(ctx context.Context, orgID uuid.UUID, campaignIDs []string) {
	if s.scheduler == nil || s.tasksClient == nil || s.taskRepo == nil {
		return
	}
	seen := map[uuid.UUID]bool{}
	for _, raw := range campaignIDs {
		id, perr := uuid.Parse(raw)
		if perr != nil || seen[id] {
			continue
		}
		seen[id] = true

		campaign, err := s.campaignRepository.GetByID(ctx, id)
		if err != nil || campaign == nil || campaign.Status != "active" {
			continue
		}
		if campaign.OrganizationID == nil || *campaign.OrganizationID != orgID {
			continue
		}

		pending, perr2 := s.campaignRepository.GetPendingCampaignTasks(ctx, id)
		if perr2 != nil {
			continue
		}
		var parked *time.Time
		for i := range pending {
			at := pending[i].ScheduledAt
			if at != nil && (parked == nil || at.Before(*parked)) {
				parked = at
			}
		}
		// Already about to wake: leave the chain's own pacing alone.
		if parked != nil && !parked.After(time.Now().Add(config.CampaignMaxDeferMinutes*time.Minute)) {
			continue
		}

		nextTime, _, _, cerr := s.scheduler.CalculateNextCampaignTime(ctx, id)
		if cerr != nil && !errors.Is(cerr, scheduler.ErrCampaignDeferred) {
			continue
		}
		if errors.Is(cerr, scheduler.ErrCampaignDeferred) {
			nextTime = scheduler.DeferSlot(nextTime)
		}
		if nextTime.IsZero() || (parked != nil && !nextTime.Before(*parked)) {
			continue
		}

		for i := range pending {
			_ = s.taskRepo.DeleteTask(ctx, pending[i].ID)
		}
		_ = s.enqueueCampaignWakeup(ctx, id)
	}
}

func (s *campaignService) enqueueCampaignWakeup(ctx context.Context, campaignID uuid.UUID) *errx.Error {
	if s.scheduler == nil || s.tasksClient == nil || s.taskRepo == nil {
		return nil
	}

	nextTime, _, accountID, err := s.scheduler.CalculateNextCampaignTime(ctx, campaignID)
	// A deferral still yields a usable first-send slot (nextTime) and a nominal
	// pool mailbox (accountID), so fall through and schedule the first wakeup at
	// the defer time rather than failing the campaign start.
	if errors.Is(err, scheduler.ErrCampaignDeferred) {
		nextTime = scheduler.DeferSlot(nextTime)
	}
	if err != nil && !errors.Is(err, scheduler.ErrCampaignDeferred) {
		switch {
		// Checked before ErrNoEmailAccounts, which they wrap.
		case errors.Is(err, scheduler.ErrDomainAuthFailing):
			_ = s.campaignRepository.UpdateStatusWithLock(ctx, campaignID, "paused_no_accounts")
			return errx.New(errx.BadRequest,
				"every mailbox on this campaign is sending from a domain that fails SPF or DMARC authentication; "+
					"add the missing DNS records at your registrar, then re-check the domain from the mailbox")
		case errors.Is(err, scheduler.ErrNoEligibleMailbox):
			_ = s.campaignRepository.UpdateStatusWithLock(ctx, campaignID, "paused_no_accounts")
			return errx.New(errx.BadRequest,
				"this campaign's mailboxes are all outside their sending window or over their daily limit right now; "+
					"check each mailbox's timezone, sending behaviour and daily cap")
		case errors.Is(err, scheduler.ErrNoEmailAccounts):
			_ = s.campaignRepository.UpdateStatusWithLock(ctx, campaignID, "paused_no_accounts")
			return errx.New(errx.BadRequest, "no active email accounts found for campaign's email tags")
		case errors.Is(err, scheduler.ErrCampaignCompleted):
			_ = s.campaignRepository.UpdateStatusWithLock(ctx, campaignID, "completed")
			return errx.New(errx.BadRequest, "campaign has no remaining contacts to send")
		case errors.Is(err, scheduler.ErrCampaignEnded):
			_ = s.campaignRepository.UpdateStatusWithLock(ctx, campaignID, "completed")
			return errx.New(errx.BadRequest, "campaign is past its end date; extend or clear the end date to keep sending")
		default:
			sentry.CaptureException(err)
			return errx.InternalError()
		}
	}

	taskID := uuid.New()
	task := &repository.Task{
		ID:             taskID,
		TaskType:       "campaign",
		EmailAccountID: accountID,
		Status:         "pending",
		ScheduledAt:    &nextTime,
	}
	campaignTask := &repository.CampaignTask{
		TaskID:     taskID,
		CampaignID: &campaignID,
	}

	created, err := s.taskRepo.CreateTaskWithLock(ctx, task, campaignTask)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}
	if !created {
		return nil
	}

	cloudTaskName, err := s.tasksClient.CreateTask(ctx, &proto.ProcessTask{TaskId: taskID.String()}, nextTime)
	if err != nil {
		_ = s.taskRepo.DeleteTask(ctx, taskID)
		_ = s.campaignRepository.StopCampaign(ctx, campaignID)
		sentry.CaptureException(err)
		return errx.New(errx.ServiceUnavailable, "could not schedule campaign right now")
	}
	if err := s.taskRepo.UpdateTaskScheduledAt(ctx, taskID, nextTime, cloudTaskName); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	return nil
}

func (s *campaignService) StopCampaign(ctx context.Context, orgID uuid.UUID, campaignID string) *errx.Error {
	cID, parseErr := uuid.Parse(campaignID)
	if parseErr != nil {
		return errx.ErrUuid
	}

	// Get campaign
	campaign, err := s.campaignRepository.GetByID(ctx, cID)
	if err != nil {
		return errx.InternalError()
	}
	if campaign == nil {
		return errx.ErrNotFound
	}

	// Verify it belongs to the org
	if campaign.OrganizationID == nil || *campaign.OrganizationID != orgID {
		return errx.ErrNotFound
	}

	// Verify status
	if campaign.Status != "active" {
		return errx.New(errx.BadRequest, "campaign must be active to stop")
	}

	// Check cooldown
	if campaign.LastStatusChangeAt != nil {
		elapsed := time.Since(*campaign.LastStatusChangeAt)
		if elapsed.Seconds() < campaignCooldownSeconds {
			return errx.New(errx.BadRequest, "please wait before changing campaign status")
		}
	}

	// Set status to paused
	if err := s.campaignRepository.StopCampaign(ctx, cID); err != nil {
		return errx.InternalError()
	}

	// Log campaign stopped
	if s.campaignLogRepo != nil {
		s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
			CampaignID: cID,
			EventType:  "stopped",
			Message:    "Campaign stopped by user",
		})
	}

	// Publish realtime event
	if s.streamingPublisher != nil {
		s.streamingPublisher.PublishCampaignEvent(ctx, &pubsub.CampaignEvent{
			BaseEvent: pubsub.BaseEvent{
				EventType: pubsub.EventCampaignPaused,
				UserID:    campaign.UserID,
			},
			OrgID:      modelOrgID(campaign.OrganizationID),
			CampaignID: cID.String(),
			Name:       campaign.Name,
			Status:     "paused",
		})
	}

	// Get and delete all pending tasks
	pendingTasks, err := s.campaignRepository.GetPendingCampaignTasks(ctx, cID)
	if err != nil {
		// Log but don't fail the stop
		return nil
	}

	for _, task := range pendingTasks {
		// Delete from DB (GCP Cloud Tasks will fail gracefully when triggered)
		if s.taskRepo != nil {
			_ = s.taskRepo.DeleteTask(ctx, task.ID)
		}
	}

	return nil
}

func (s *campaignService) GetLogs(ctx context.Context, userID, campaignID string, limit int, cursor *string) (*models.CampaignLogsResult, *errx.Error) {
	cID, parseErr := uuid.Parse(campaignID)
	if parseErr != nil {
		return nil, errx.ErrUuid
	}

	// Verify user owns this campaign
	_, err := s.campaignRepository.Get(ctx, userID, campaignID)
	if err != nil {
		if errors.Is(err, errx.ErrResourceNotFound) {
			return nil, errx.ErrNotFound
		}
		return nil, errx.InternalError()
	}

	result, err := s.campaignLogRepo.GetLogs(ctx, cID, limit, cursor)
	if err != nil {
		return nil, errx.InternalError()
	}

	return result, nil
}

// campaignForOrg loads a campaign and verifies it belongs to the given org.
func (s *campaignService) campaignForOrg(ctx context.Context, orgID uuid.UUID, campaignID string) (*models.Campaign, uuid.UUID, *errx.Error) {
	cID, parseErr := uuid.Parse(campaignID)
	if parseErr != nil {
		return nil, uuid.Nil, errx.ErrUuid
	}
	campaign, err := s.campaignRepository.GetByID(ctx, cID)
	if err != nil {
		if errors.Is(err, errx.ErrResourceNotFound) {
			return nil, uuid.Nil, errx.ErrNotFound
		}
		return nil, uuid.Nil, errx.InternalError()
	}
	if campaign == nil || campaign.OrganizationID == nil || *campaign.OrganizationID != orgID {
		return nil, uuid.Nil, errx.ErrNotFound
	}
	return campaign, cID, nil
}

// ListCampaignSenders returns the campaign's explicit sender pool.
func (s *campaignService) ListCampaignSenders(ctx context.Context, orgID uuid.UUID, campaignID string) ([]models.CampaignSender, *errx.Error) {
	_, cID, xerr := s.campaignForOrg(ctx, orgID, campaignID)
	if xerr != nil {
		return nil, xerr
	}
	senders, err := s.campaignRepository.GetCampaignSenders(ctx, cID)
	if err != nil {
		return nil, errx.InternalError()
	}
	return senders, nil
}

// ReplaceCampaignSenders atomically replaces the explicit sender pool.
func (s *campaignService) ReplaceCampaignSenders(ctx context.Context, orgID uuid.UUID, campaignID string, in []models.CampaignSenderInput) ([]models.CampaignSender, *errx.Error) {
	_, cID, xerr := s.campaignForOrg(ctx, orgID, campaignID)
	if xerr != nil {
		return nil, xerr
	}
	return s.campaignRepository.ReplaceCampaignSenders(ctx, cID, in)
}

// VerifyCampaignTrackingDomain resolves the campaign-scoped tracking domain
// against this install's tracking host and flips verified on success. Only a
// verified override is honored at send time, so an unresolved record stays
// "pending" rather than erroring, and the status carries the reason.
func (s *campaignService) VerifyCampaignTrackingDomain(ctx context.Context, orgID uuid.UUID, campaignID string) (*models.TrackingDomainStatus, *errx.Error) {
	campaign, cID, xerr := s.campaignForOrg(ctx, orgID, campaignID)
	if xerr != nil {
		return nil, xerr
	}

	target := config.TrackingHostname()
	res := trackdns.Verify(ctx, campaign.TrackingDomain, target)

	status := &models.TrackingDomainStatus{
		TrackingDomain:           res.Domain,
		TrackingDomainVerified:   res.Verified,
		CNAMETarget:              target,
		Status:                   res.Code,
		Message:                  res.Reason,
		Observed:                 res.Observed,
		TrackingHostUnresolvable: res.TargetUnresolvable,
	}
	if res.Verified {
		now := time.Now().UTC()
		status.TrackingDomainVerifiedAt = &now
	}

	if err := s.campaignRepository.SetCampaignTrackingDomainVerified(ctx, cID, status.TrackingDomainVerified, status.TrackingDomainVerifiedAt); err != nil {
		return nil, errx.InternalError()
	}
	return status, nil
}

// modelOrgID renders an optional org UUID for org-scoped realtime events.
func modelOrgID(orgID *uuid.UUID) string {
	if orgID == nil {
		return ""
	}
	return orgID.String()
}

package placement

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
	"github.com/warmbly/warmbly/internal/repository"
)

// MonitorInput configures a campaign's scheduled placement test. Nil fields
// keep the stored value, or the default on a new monitor.
type MonitorInput struct {
	Enabled      *bool   `json:"enabled"`
	IntervalDays *int    `json:"interval_days"`
	Panel        *string `json:"panel"`
	AlertBelow   *int    `json:"alert_below"`
	PauseOnAlert *bool   `json:"pause_on_alert"`
}

func (s *service) ownedCampaign(ctx context.Context, orgID, campaignID uuid.UUID) (*models.Campaign, *errx.Error) {
	c, err := s.Campaigns.GetByID(ctx, campaignID)
	if err != nil || c == nil || c.OrganizationID == nil || *c.OrganizationID != orgID {
		return nil, errx.New(errx.NotFound, "campaign not found")
	}
	return c, nil
}

// GetMonitor is a campaign's monitor, nil when it has none.
func (s *service) GetMonitor(ctx context.Context, orgID, campaignID uuid.UUID) (*models.PlacementMonitor, *errx.Error) {
	if _, xerr := s.ownedCampaign(ctx, orgID, campaignID); xerr != nil {
		return nil, xerr
	}
	m, err := s.Repo.GetMonitor(ctx, orgID, campaignID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	return m, nil
}

// PutMonitor creates or updates a campaign's monitor. A new or re-enabled
// monitor runs its first test within minutes, not an interval from now.
func (s *service) PutMonitor(ctx context.Context, orgID, userID, campaignID uuid.UUID, in MonitorInput) (*models.PlacementMonitor, *errx.Error) {
	if _, xerr := s.ownedCampaign(ctx, orgID, campaignID); xerr != nil {
		return nil, xerr
	}
	current, err := s.Repo.GetMonitor(ctx, orgID, campaignID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	// An API key has no user behind it.
	var createdBy *uuid.UUID
	if userID != uuid.Nil {
		createdBy = &userID
	}
	m := models.PlacementMonitor{
		OrganizationID: orgID,
		CampaignID:     campaignID,
		CreatedBy:      createdBy,
		Enabled:        true,
		IntervalDays:   config.PlacementMonitorIntervalDaysDef,
		Panel:          models.PlacementPanelInstance,
		AlertBelow:     config.PlacementMonitorAlertBelowDefault,
		NextRunAt:      s.now().Add(5 * time.Minute),
	}
	if current != nil {
		m = *current
	}
	wasEnabled := current != nil && current.Enabled
	if in.Enabled != nil {
		m.Enabled = *in.Enabled
	}
	if in.IntervalDays != nil {
		if *in.IntervalDays < config.PlacementMonitorIntervalDaysMin || *in.IntervalDays > config.PlacementMonitorIntervalDaysMax {
			return nil, errx.New(errx.BadRequest, fmt.Sprintf("interval_days must be between %d and %d",
				config.PlacementMonitorIntervalDaysMin, config.PlacementMonitorIntervalDaysMax))
		}
		m.IntervalDays = *in.IntervalDays
	}
	if in.Panel != nil {
		if !models.ValidPlacementPanel(*in.Panel) {
			return nil, errx.New(errx.BadRequest, "panel must be instance, workspace or cloud")
		}
		m.Panel = *in.Panel
	}
	if in.AlertBelow != nil {
		if *in.AlertBelow < 0 || *in.AlertBelow > 100 {
			return nil, errx.New(errx.BadRequest, "alert_below must be between 0 and 100")
		}
		m.AlertBelow = *in.AlertBelow
	}
	if in.PauseOnAlert != nil {
		m.PauseOnAlert = *in.PauseOnAlert
	}
	if m.Enabled && !wasEnabled {
		m.NextRunAt = s.now().Add(5 * time.Minute)
	} else if current != nil && in.IntervalDays != nil && current.LastRunAt != nil {
		m.NextRunAt = current.LastRunAt.Add(time.Duration(m.IntervalDays) * 24 * time.Hour)
	}
	if err := s.Repo.UpsertMonitor(ctx, &m); err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	return &m, nil
}

// DeleteMonitor removes a campaign's monitor.
func (s *service) DeleteMonitor(ctx context.Context, orgID, campaignID uuid.UUID) *errx.Error {
	if _, xerr := s.ownedCampaign(ctx, orgID, campaignID); xerr != nil {
		return xerr
	}
	ok, err := s.Repo.DeleteMonitor(ctx, orgID, campaignID)
	if err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}
	if !ok {
		return errx.New(errx.NotFound, "this campaign has no placement monitor")
	}
	return nil
}

// runMonitors starts the test of every due monitor: the campaign's first
// email step, from the next of its mailboxes in turn, because every mailbox
// the campaign sends from has its own standing.
func (s *service) runMonitors(ctx context.Context) {
	now := s.now()
	due, err := s.Repo.ClaimDueMonitors(ctx, now, 20)
	if err != nil {
		errs.CaptureException(err)
		return
	}
	for _, m := range due {
		next := now.Add(time.Duration(m.IntervalDays) * 24 * time.Hour)
		testID, senderID, reason := s.runMonitor(ctx, m)
		if reason != "" && testID == nil {
			// Nothing started: look again in a day rather than a whole interval.
			next = now.Add(24 * time.Hour)
		}
		if err := s.Repo.MarkMonitorRun(ctx, m.ID, next, testID, senderID, reason); err != nil {
			errs.CaptureException(err)
		}
	}
}

func (s *service) runMonitor(ctx context.Context, m models.PlacementMonitor) (*uuid.UUID, *uuid.UUID, string) {
	campaign, xerr := s.ownedCampaign(ctx, m.OrganizationID, m.CampaignID)
	if xerr != nil {
		return nil, nil, "The campaign no longer exists."
	}
	if campaign.Status != "active" {
		return nil, nil, "The campaign is not running, so nothing was tested."
	}
	seqs, err := s.Campaigns.GetSequencesByCampaignID(ctx, campaign.ID)
	if err != nil {
		errs.CaptureException(err)
		return nil, nil, "The campaign's steps could not be read."
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i].Position < seqs[j].Position })
	var step *models.Sequence
	for i := range seqs {
		if strings.TrimSpace(seqs[i].Subject) != "" && (mailhtml.HasContent(seqs[i].BodyHTML) || strings.TrimSpace(seqs[i].BodyPlain) != "") {
			step = &seqs[i]
			break
		}
	}
	if step == nil {
		return nil, nil, "The campaign has no email step to test."
	}
	sender := s.nextSender(ctx, campaign, m.LastSenderID)
	if sender == nil {
		return nil, nil, "None of the campaign's mailboxes is connected and running."
	}
	views, xerr := s.CreateTests(ctx, CreateInput{
		OrgID:           m.OrganizationID,
		UserID:          m.CreatedBy,
		SenderAccountID: *sender,
		CampaignID:      &campaign.ID,
		SequenceID:      &step.ID,
		Tracking:        models.PlacementTrackingCampaign,
		Panel:           m.Panel,
		Origin:          models.PlacementOriginMonitor,
		MonitorID:       &m.ID,
	})
	if xerr != nil {
		return nil, sender, xerr.Message
	}
	id := views[0].ID
	return &id, sender, ""
}

// nextSender is the campaign's mailbox after the one tested last, in a
// stable order, skipping any that cannot send.
func (s *service) nextSender(ctx context.Context, campaign *models.Campaign, last *uuid.UUID) *uuid.UUID {
	pool, xerr := repository.ResolveCampaignSenderPool(ctx, s.Emails, campaign)
	if xerr != nil {
		return nil
	}
	var ids []uuid.UUID
	for _, a := range pool.Accounts {
		if a.Status == "active" && a.WorkerID != nil {
			ids = append(ids, a.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	pick := ids[0]
	if last != nil {
		for i, id := range ids {
			if id == *last {
				pick = ids[(i+1)%len(ids)]
				break
			}
		}
	}
	return &pick
}

// checkMonitorAlert raises the alert when a monitor's test lands below its
// threshold, and pauses the campaign when the monitor is set to.
func (s *service) checkMonitorAlert(ctx context.Context, monitorID, orgID uuid.UUID, campaignID *uuid.UUID, c models.PlacementCounts, body, link string) {
	m, err := s.Repo.GetMonitorByID(ctx, monitorID)
	if err != nil || m == nil || m.OrganizationID != orgID {
		return
	}
	rate := percent(c.InboxRate)
	if rate < 0 || rate >= m.AlertBelow {
		return
	}
	title := fmt.Sprintf("Inbox placement dropped to %d%%", rate)
	if m.PauseOnAlert && s.Pauser != nil && campaignID != nil {
		reason := fmt.Sprintf("Scheduled placement test landed %d%% in the inbox, below the %d%% alert threshold", rate, m.AlertBelow)
		if err := s.Pauser.PausePlacementAlert(ctx, orgID, *campaignID, reason); err != nil {
			errs.CaptureException(err)
		} else {
			title += "; campaign paused"
		}
	}
	if s.Notifier != nil {
		s.Notifier.NotifyOrg(ctx, orgID, models.PermViewCampaigns, uuid.Nil, models.NotifPlacementAlert, title, body, link,
			map[string]any{"campaign_id": m.CampaignID.String()}, "placement_alert:"+m.ID.String())
	}
	if err := s.Repo.MarkMonitorAlert(ctx, m.ID, s.now()); err != nil {
		errs.CaptureException(err)
	}
}

package hubspot

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/idlepoll"
)

// Jobs are claimed one at a time under a lease longer than a job may run, so
// a lease never lapses while its job is still working. An empty outbox is
// polled less often, up to drainIdleMax.
const (
	drainEvery    = 2 * time.Second
	drainIdleMax  = 10 * time.Second
	drainPerTick  = 50
	jobTimeout    = 2 * time.Minute
	drainLease    = jobTimeout + time.Minute
	maxJobRetries = 6
)

// retryDelays space a failing job's attempts out over several hours.
var retryDelays = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour, 12 * time.Hour}

// RunDrainer works the outbox until ctx ends. Safe to run in several processes.
func (s *Service) RunDrainer(ctx context.Context) {
	pace := idlepoll.Pace{Min: drainEvery, Max: drainIdleMax}
	t := time.NewTimer(drainEvery)
	defer t.Stop()
	purge := time.NewTicker(time.Hour)
	defer purge.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-purge.C:
			_ = s.d.Repo.PurgeJobs(ctx)
		case <-t.C:
			ran := 0
			for ran < drainPerTick && ctx.Err() == nil {
				jobs, err := s.d.Repo.ClaimJobs(ctx, provider, 1, drainLease)
				if err != nil {
					log.Warn().Err(err).Msg("hubspot: claim jobs")
					break
				}
				if len(jobs) == 0 {
					break
				}
				s.runJob(ctx, &jobs[0])
				ran++
			}
			t.Reset(pace.Next(ran, ran >= drainPerTick))
		}
	}
}

// errContinue asks the drainer to run the job again with a new payload (the
// next step of a backfill) instead of completing it.
type errContinue struct{ payload map[string]any }

func (e *errContinue) Error() string { return "continue" }

func (s *Service) runJob(ctx context.Context, job *models.CRMSyncJob) {
	jctx, cancel := context.WithTimeout(ctx, jobTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Str("kind", job.Kind).Msg("hubspot: job panicked")
			_ = s.d.Repo.FailJob(ctx, job, "Warmbly hit an internal error on this item.", nil)
		}
	}()
	o, err := s.resolve(jctx, job.OrganizationID)
	if o == nil && err == nil {
		// The workspace left HubSpot mode: nothing to deliver to.
		_ = s.d.Repo.FailJob(ctx, job, "HubSpot is no longer this workspace's CRM", nil)
		return
	}
	if err != nil {
		at := time.Now().Add(retryDelays[min(job.Attempts-1, len(retryDelays)-1)])
		if errors.Is(err, errNotConnected) || job.Attempts >= maxJobRetries {
			_ = s.d.Repo.FailJob(ctx, job, "HubSpot is disconnected. Reconnect it, then retry.", nil)
			return
		}
		_ = s.d.Repo.FailJob(ctx, job, "Warmbly could not reach its database; retrying.", &at)
		return
	}
	err = s.execute(jctx, o, job)
	if err == nil {
		_ = s.d.Repo.CompleteJob(ctx, job)
		return
	}
	var cont *errContinue
	if errors.As(err, &cont) {
		_ = s.d.Repo.ContinueJob(ctx, job, cont.payload)
		return
	}
	msg, retry := s.classify(jctx, o, err)
	if retry && job.Attempts < maxJobRetries {
		at := time.Now().Add(retryDelays[min(job.Attempts-1, len(retryDelays)-1)])
		if ae, ok := AsAPIError(err); ok && ae.RetryAfter > 0 {
			at = time.Now().Add(ae.RetryAfter)
		}
		_ = s.d.Repo.FailJob(ctx, job, msg, &at)
		return
	}
	_ = s.d.Repo.FailJob(ctx, job, msg, nil)
}

// classify words a failure for the sync health list and decides whether
// trying again could help.
func (s *Service) classify(ctx context.Context, o *org, err error) (string, bool) {
	var xe *errx.Error
	if errors.As(err, &xe) {
		return xe.Message, xe.Code == errx.ServiceUnavailable || xe.Code == errx.TooManyRequests
	}
	if ae, ok := AsAPIError(err); ok {
		ue := s.userError(ctx, o, err)
		return ue.Message, ae.Retryable() || ae.AuthProblem()
	}
	var ne net.Error
	if errors.As(err, &ne) || errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "connection") {
		return "HubSpot did not answer in time.", true
	}
	return s.userError(ctx, o, err).Message, true
}

func (s *Service) execute(ctx context.Context, o *org, job *models.CRMSyncJob) error {
	p := job.Payload
	switch job.Kind {
	case models.CRMJobLogEmail:
		return s.logEmail(ctx, o, job)
	case models.CRMJobReplyOutcome:
		return s.replyOutcomeJob(ctx, o, p)
	case models.CRMJobLogEvent:
		return s.logEvent(ctx, o, p)
	case models.CRMJobLogMeeting:
		return s.logMeeting(ctx, o, p)
	case models.CRMJobPushDeal:
		return s.syncLocalDeal(ctx, o, uuidOf(p, "local_id"))
	case models.CRMJobPushTask:
		return s.syncLocalTask(ctx, o, uuidOf(p, "local_id"))
	case models.CRMJobPushNote:
		return s.syncLocalNote(ctx, o, uuidOf(p, "local_id"))
	case models.CRMJobPushContact:
		return s.pushContactFields(ctx, o, p)
	case models.CRMJobRefreshObject:
		return s.refreshObject(ctx, o, str(p, "object_type"), str(p, "external_id"))
	case models.CRMJobBackfill:
		return s.backfill(ctx, o, p)
	default:
		return nil
	}
}

func uuidOf(p map[string]any, k string) uuid.UUID {
	id, _ := uuid.Parse(str(p, k))
	return id
}

// syncLocalDeal brings HubSpot in line with a deal an automation wrote.
func (s *Service) syncLocalDeal(ctx context.Context, o *org, id uuid.UUID) error {
	deal, err := s.d.CRM.GetDeal(ctx, o.ID, id)
	if err != nil || deal == nil {
		return nil
	}
	link, err := s.d.Repo.GetLinkByLocal(ctx, o.ID, provider, models.CRMObjectDeal, id)
	if err != nil {
		return err
	}
	if link == nil {
		if xerr := s.PushDealCreate(ctx, o.ID, deal); xerr != nil {
			return xerr
		}
		return nil
	}
	stageExt, _, xerr := s.stageInfo(ctx, o, deal.StageID)
	if xerr != nil {
		return xerr
	}
	props := dealProperties(deal.Name, deal.Value, deal.Currency, deal.ExpectedCloseDate)
	props["dealstage"] = stageExt
	if _, err := o.Client.Update(ctx, "deals", link.ExternalID, props); err != nil {
		return err
	}
	s.notify(ctx, o.ID, contactIDString(deal.ContactID), "deal")
	return nil
}

func (s *Service) syncLocalTask(ctx context.Context, o *org, id uuid.UUID) error {
	task, err := s.d.CRM.GetCRMTask(ctx, o.ID, id)
	if err != nil || task == nil {
		return nil
	}
	link, err := s.d.Repo.GetLinkByLocal(ctx, o.ID, provider, models.CRMObjectTask, id)
	if err != nil {
		return err
	}
	if link != nil {
		return nil
	}
	if xerr := s.PushTaskCreate(ctx, o.ID, task); xerr != nil {
		return xerr
	}
	return nil
}

func (s *Service) syncLocalNote(ctx context.Context, o *org, id uuid.UUID) error {
	note, err := s.d.CRM.GetNote(ctx, o.ID, id)
	if err != nil || note == nil {
		return nil
	}
	if link, err := s.d.Repo.GetLinkByLocal(ctx, o.ID, provider, models.CRMObjectNote, id); err != nil || link != nil {
		return err
	}
	if xerr := s.PushNoteCreate(ctx, o.ID, note); xerr != nil {
		return xerr
	}
	return nil
}

package toolmigrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
	"github.com/warmbly/warmbly/internal/repository"
)

// Runner pacing. A lease outlives one provider page with its retries.
const (
	leaseDuration = 3 * time.Minute
	maxAttempts   = 5
	idleWindow    = 7 * 24 * time.Hour
	maxNotes      = 200
)

// Kick wakes the runner now instead of at its next tick.
func (s *Service) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Run scans and applies migrations until ctx ends, plus the upkeep sweep. Work
// interrupted by a shutdown resumes from its last checkpoint on the next pass.
func (s *Service) Run(ctx context.Context) {
	go jobrun.Loop(ctx, "tool_migration_upkeep", time.Hour, true, func(ctx context.Context) error {
		_, err := s.d.Repo.Upkeep(ctx, idleWindow, maxAttempts)
		return err
	})
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		s.pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.kick:
		}
	}
}

func (s *Service) pass(ctx context.Context) {
	for ctx.Err() == nil {
		job, err := s.d.Repo.ClaimScan(ctx, leaseDuration)
		if err != nil || job == nil {
			break
		}
		held, release := s.hold(ctx, func(c context.Context) (bool, error) {
			return s.d.Repo.RenewScan(c, job.ID, job.Token, leaseDuration)
		})
		s.scan(held, job)
		release()
	}
	for ctx.Err() == nil {
		job, err := s.d.Repo.ClaimTarget(ctx, leaseDuration)
		if err != nil || job == nil {
			return
		}
		held, release := s.hold(ctx, func(c context.Context) (bool, error) {
			return s.d.Repo.RenewTarget(c, job.ID, job.Token, leaseDuration)
		})
		s.runTarget(held, job)
		release()
	}
}

// hold renews a lease while its work runs. The returned context ends when the
// lease is lost, so provider calls stop; writes are refused by the token anyway.
func (s *Service) hold(parent context.Context, renew func(context.Context) (bool, error)) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(s.renewEvery)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				// A failed renewal is retried; only a refusal means another holder has it.
				if held, err := renew(context.WithoutCancel(ctx)); err == nil && !held {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, func() {
		close(done)
		cancel()
	}
}

// openCredentials decrypts a migration's credential sets.
func (s *Service) openCredentials(sealed string) ([]source.Credentials, error) {
	raw, err := s.d.Sealer.Decrypt(sealed)
	if err != nil {
		return nil, err
	}
	var creds []source.Credentials
	if err := json.Unmarshal([]byte(raw), &creds); err != nil {
		return nil, err
	}
	return creds, nil
}

// scan reads every source workspace each key can see and what it holds.
func (s *Service) scan(ctx context.Context, job *repository.ToolMigrationScanJob) {
	work := context.WithoutCancel(ctx)
	failScan := func(msg string) {
		_, _ = s.d.Repo.FinishScan(work, job.ID, job.Token, nil, models.ToolMigrationFailed, msg)
	}
	if job.Attempts > maxAttempts {
		failScan("Reading the other tool stopped unexpectedly several times. Start a new migration.")
		return
	}
	p, ok := s.d.Registry.Get(source.ProviderID(job.Provider))
	if !ok {
		failScan("Warmbly no longer migrates from this tool.")
		return
	}
	creds, err := s.openCredentials(job.Sealed)
	if err != nil {
		errs.CaptureException(err)
		failScan("The keys for this migration could not be read. Start a new migration.")
		return
	}
	inv := []models.ToolMigrationWorkspace{}
	readable := 0
	var lastErr error
	for i, c := range creds {
		if ctx.Err() != nil {
			return
		}
		sess, err := p.Open(ctx, c)
		if err != nil {
			lastErr = err
			continue
		}
		list, err := sess.Workspaces(ctx)
		if err != nil {
			lastErr = err
			continue
		}
		for _, ws := range list {
			ws.Key = i
			entry := models.ToolMigrationWorkspace{Workspace: ws}
			if got, err := sess.Inventory(ctx, ws); err != nil {
				entry.Error = providerMessage(err)
			} else {
				entry.Inventory = got
				readable++
			}
			inv = append(inv, entry)
		}
	}
	if ctx.Err() != nil {
		// Shutting down or the lease was lost: the next holder scans again.
		return
	}
	if readable == 0 {
		msg := "Nothing could be read with these keys."
		if lastErr != nil {
			msg = providerMessage(lastErr)
		}
		failScan(msg)
		return
	}
	raw, err := json.Marshal(inv)
	if err != nil {
		failScan("The scan could not be saved.")
		return
	}
	_, _ = s.d.Repo.FinishScan(work, job.ID, job.Token, raw, models.ToolMigrationReady, "")
}

// providerMessage is a provider error as a person reads it.
func providerMessage(err error) string {
	var apiErr *source.APIError
	switch {
	case errors.As(err, &apiErr):
		return apiErr.Error()
	case errors.Is(err, source.ErrKey), errors.Is(err, source.ErrForbidden), errors.Is(err, source.ErrRateLimited),
		errors.Is(err, source.ErrUnavailable), errors.Is(err, source.ErrUnsupported):
		return err.Error()
	}
	return "The other tool could not be read."
}

// transient errors are retried by letting the lease lapse.
func transient(err error) bool {
	return errors.Is(err, source.ErrRateLimited) || errors.Is(err, source.ErrUnavailable) ||
		errors.Is(err, context.DeadlineExceeded)
}

// cursor is a target's resume point inside its current phase.
type cursor struct {
	Kind source.Kind `json:"kind,omitempty"`
	// Campaign indexes the source campaign list in ID order; past its end is the account-level leads.
	Campaign int    `json:"campaign,omitempty"`
	Page     string `json:"page,omitempty"`
	// LeadsServed is true once any form of the leads listing answered.
	LeadsServed bool `json:"leads_served,omitempty"`
}

// stop ends a target's run early without finishing it.
var (
	errLost        = errors.New("claim lost")
	errInterrupted = errors.New("shutting down")
)

// failure is a target error worth showing to a person.
type failure struct{ msg string }

func (f *failure) Error() string { return f.msg }

func fail(format string, args ...any) error { return &failure{msg: fmt.Sprintf(format, args...)} }

// run is one claimed target in progress.
type run struct {
	s               *Service
	job             *repository.ToolMigrationTargetJob
	parent          context.Context
	ctx             context.Context
	opts            models.ToolMigrationOptions
	info            source.Info
	sess            source.Session
	ws              source.Workspace
	actor           uuid.UUID
	label           string
	cur             cursor
	counts          models.ToolMigrationCounts
	followup        models.ToolMigrationFollowup
	mailboxImportID *uuid.UUID
	categoryID      *uuid.UUID
	// campaignList caches the source's campaigns for this run.
	campaignList []source.Campaign
}

func (s *Service) runTarget(ctx context.Context, job *repository.ToolMigrationTargetJob) {
	r := &run{s: s, job: job, parent: ctx, ctx: context.WithoutCancel(ctx), label: job.Label,
		mailboxImportID: job.MailboxImportID, categoryID: job.CategoryID,
		ws: source.Workspace{ID: job.WorkspaceID, Name: job.WorkspaceName, Key: job.KeyIndex}}
	_ = json.Unmarshal(job.Counts, &r.counts)
	_ = json.Unmarshal(job.Followup, &r.followup)
	if len(job.Cursor) > 0 {
		_ = json.Unmarshal(job.Cursor, &r.cur)
	}

	err := r.prepare()
	if err == nil {
		err = r.apply()
	}
	switch {
	case errors.Is(err, errLost), errors.Is(err, errInterrupted), err != nil && ctx.Err() != nil:
		// Stopping, canceled, removed or claimed by another holder: the next holder carries on.
		return
	case err != nil && transient(err) && job.Attempts < maxAttempts:
		// The lease lapses and a later pass resumes from the checkpoint.
		log.Warn().Str("migration_id", job.MigrationID.String()).Msg("tool migration: the provider is unavailable, retrying later")
		return
	case err != nil:
		var f *failure
		msg := "This workspace could not be brought over."
		if errors.As(err, &f) {
			msg = f.msg
		} else if transient(err) {
			msg = providerMessage(err)
		} else {
			errs.CaptureException(err)
		}
		r.finish(models.ToolMigrationTargetFailed, msg)
	default:
		r.finish(models.ToolMigrationTargetDone, "")
	}
}

// prepare re-checks the creator's standing and opens the source.
func (r *run) prepare() error {
	job := r.job
	if job.Attempts > maxAttempts {
		return fail("This workspace stopped unexpectedly several times. What it brought over stays.")
	}
	if job.CreatedBy == nil {
		return fail("The member who started this migration no longer has an account.")
	}
	r.actor = *job.CreatedBy
	if job.Sealed == nil {
		return fail("The keys for this migration are gone.")
	}
	if err := json.Unmarshal(job.Options, &r.opts); err != nil {
		return fail("The migration's choices could not be read.")
	}
	if xerr := r.s.requireMember(r.ctx, job.OrgID, r.actor, neededPerms(r.opts.Kinds)); xerr != nil {
		return fail("The member who started this migration can no longer write to this workspace: %s", xerr.Message)
	}
	if job.AgencyID != nil {
		if xerr := r.s.stillAgencyAdmin(r.ctx, *job.AgencyID, r.actor); xerr != nil {
			return fail("%s", xerr.Message)
		}
		if xerr := r.s.stillClient(r.ctx, *job.AgencyID, job.OrgID, r.actor); xerr != nil {
			return fail("%s", xerr.Message)
		}
	}
	p, ok := r.s.d.Registry.Get(source.ProviderID(job.Provider))
	if !ok {
		return fail("Warmbly no longer migrates from this tool.")
	}
	r.info = p.Info()
	creds, err := r.s.openCredentials(*job.Sealed)
	if err != nil {
		errs.CaptureException(err)
		return fail("The keys for this migration could not be read.")
	}
	if job.KeyIndex < 0 || job.KeyIndex >= len(creds) {
		return fail("The key for this workspace is missing.")
	}
	sess, err := p.Open(r.parent, creds[job.KeyIndex])
	if err != nil {
		if transient(err) {
			return err
		}
		return fail("%s", providerMessage(err))
	}
	r.sess = sess
	return nil
}

// stillAgencyAdmin checks the member who started an agency migration still manages the agency.
func (s *Service) stillAgencyAdmin(ctx context.Context, agencyID, userID uuid.UUID) *errx.Error {
	if s.d.Agencies == nil {
		return errx.New(errx.ServiceUnavailable, "Agencies are not available on this instance.")
	}
	a, xerr := s.d.Agencies.Get(ctx, agencyID, userID)
	if xerr != nil || !a.Role.CanManage() {
		return errx.New(errx.Forbidden, "The member who started this migration is no longer an owner or admin of the agency.")
	}
	return nil
}

// stillClient checks the workspace still belongs to the agency.
func (s *Service) stillClient(ctx context.Context, agencyID, orgID, userID uuid.UUID) *errx.Error {
	if s.d.AgencyClients == nil {
		return errx.New(errx.ServiceUnavailable, "Agencies are not available on this instance.")
	}
	list, err := s.d.AgencyClients.ClientMemberships(ctx, agencyID, userID)
	if err != nil {
		return errx.InternalError()
	}
	for _, m := range list {
		if m.OrganizationID == orgID {
			return nil
		}
	}
	return errx.New(errx.Forbidden, "This workspace is no longer one of the agency's clients.")
}

// apply runs every chosen phase from the checkpoint on.
func (r *run) apply() error {
	phases := make([]source.Kind, 0, len(r.opts.Kinds))
	for _, k := range source.AllKinds {
		if slices.Contains(r.opts.Kinds, k) && r.info.Supports(k) {
			phases = append(phases, k)
		}
	}
	start := 0
	if r.cur.Kind != "" {
		if i := slices.Index(phases, r.cur.Kind); i >= 0 {
			start = i
		}
	}
	for _, k := range phases[start:] {
		if r.cur.Kind != k {
			r.cur = cursor{Kind: k}
		}
		if err := r.save(); err != nil {
			return err
		}
		var err error
		switch k {
		case source.KindBlocklist:
			err = r.blocklist()
		case source.KindMailboxes:
			err = r.mailboxes()
		case source.KindCampaigns:
			err = r.campaigns()
		case source.KindLeads:
			err = r.leads()
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// save checkpoints the target and renews its lease, stopping when it was canceled or the process is stopping.
func (r *run) save() error {
	if r.parent.Err() != nil {
		return errInterrupted
	}
	cur, _ := json.Marshal(r.cur)
	held, err := r.s.d.Repo.SaveTarget(r.ctx, r.job.ID, r.job.Token, leaseDuration, r.progress(cur))
	if err != nil {
		return err
	}
	if !held {
		return errLost
	}
	return nil
}

func (r *run) progress(cur []byte) repository.ToolMigrationProgress {
	counts, _ := json.Marshal(r.counts)
	followup, _ := json.Marshal(r.followup)
	return repository.ToolMigrationProgress{
		Phase: string(r.cur.Kind), Cursor: cur, Counts: counts, Followup: followup,
		MailboxImportID: r.mailboxImportID, CategoryID: r.categoryID,
	}
}

func (r *run) finish(status models.ToolMigrationTargetStatus, msg string) {
	phase := r.cur.Kind
	if status == models.ToolMigrationTargetDone {
		r.cur.Kind = ""
	}
	p := r.progress(nil)
	if status != models.ToolMigrationTargetDone {
		p.Phase = string(phase)
	}
	ended, err := r.s.d.Repo.FinishTarget(r.ctx, r.job.ID, r.job.Token, status, p, msg)
	if err != nil || !ended {
		return
	}
	r.s.audit(r.ctx, r.job.OrgID, r.actor, models.AuditActionUpdate, r.job.MigrationID, map[string]string{"status": string(status)})
	if _, err := r.s.d.Repo.Finalize(r.ctx, r.job.MigrationID); err != nil {
		log.Warn().Str("migration_id", r.job.MigrationID.String()).Msg("tool migration: could not close the migration")
	}
}

// note records something a person has to know, once.
func (r *run) note(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if slices.Contains(r.followup.Notes, msg) {
		return
	}
	if len(r.followup.Notes) >= maxNotes {
		return
	}
	r.followup.Notes = append(r.followup.Notes, msg)
}

// Package clientreport keeps the reports a workspace shares with its client:
// a saved scope over campaigns, folders and mailboxes, a public link to a
// read-only page served by the portal service, and scheduled summary emails.
// Every number is read through the analytics service, so a report says what
// the dashboard says for the same campaigns and days.
package clientreport

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/analytics"
	"github.com/warmbly/warmbly/internal/app/brandkit"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/notify"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

// Error codes a client can branch on.
const (
	CodeNotFound          = "report_not_found"
	CodeInvalidScope      = "invalid_report_scope"
	CodeInvalidRange      = "invalid_report_range"
	CodeInvalidSections   = "invalid_report_sections"
	CodeLimitReached      = "report_limit_reached"
	CodeLinksUnavailable  = "report_links_unavailable"
	CodeEmailsUnavailable = "report_emails_unavailable"
	CodeInvalidLink       = "invalid_report_link"
)

var (
	errNotFound          = errx.NewWithIdentifier(errx.NotFound, CodeNotFound, "This report does not exist in the selected workspace.")
	errInvalidRange      = errx.NewWithIdentifier(errx.BadRequest, CodeInvalidRange, "date_range must be last_7_days, last_30_days, this_month or last_month.")
	errNoSections        = errx.NewWithIdentifier(errx.BadRequest, CodeInvalidSections, "Turn on at least one section.")
	errLimitReached      = errx.NewWithIdentifier(errx.Conflict, CodeLimitReached, "This workspace already has 200 reports. Delete one to add another.")
	errLinksUnavailable  = errx.NewWithIdentifier(errx.Unprocessable, CodeLinksUnavailable, "This instance has no portal service configured (PORTAL_DOMAIN), so a report link would point nowhere.")
	errEmailsUnavailable = errx.NewWithIdentifier(errx.Unprocessable, CodeEmailsUnavailable, "This instance has no outgoing mail configured, so a scheduled report could not be sent.")
	errInvalidLink       = errx.NewWithIdentifier(errx.BadRequest, CodeInvalidLink, "expires_in_days must be between 1 and 365.")
)

func scopeError(msg string) *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, CodeInvalidScope, msg)
}

// PublicHosts names the host a workspace's client pages are linked on: its agency portal domain, else the shared portal address.
type PublicHosts interface {
	PortalHost(ctx context.Context, orgID uuid.UUID) string
}

// Service is the client report API.
type Service interface {
	List(ctx context.Context, orgID uuid.UUID, cursor string, limit int) ([]models.ClientReport, *string, *errx.Error)
	Get(ctx context.Context, orgID, id uuid.UUID) (*models.ClientReport, *errx.Error)
	Create(ctx context.Context, orgID, userID uuid.UUID, in *models.ClientReportInput) (*models.ClientReport, *errx.Error)
	Update(ctx context.Context, orgID, id uuid.UUID, in *models.ClientReportInput) (*models.ClientReport, *errx.Error)
	Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error
	// CreateLink mints a new share link, replacing any previous one; the URL is returned only here.
	CreateLink(ctx context.Context, orgID, id uuid.UUID, req *models.CreateClientReportLinkRequest) (*models.ClientReportLinkResult, *errx.Error)
	// RevokeLinks turns off the share link and every link already sent by email.
	RevokeLinks(ctx context.Context, orgID, id uuid.UUID) (*models.ClientReport, *errx.Error)
	SetSchedule(ctx context.Context, orgID, id uuid.UUID, req *models.SetClientReportScheduleRequest) (*models.ClientReport, *errx.Error)
	DeleteSchedule(ctx context.Context, orgID, id uuid.UUID) (*models.ClientReport, *errx.Error)
	// Preview renders the report as its public page would; an empty range is the report's default.
	Preview(ctx context.Context, orgID, id uuid.UUID, rng models.ClientReportRange) (*models.ClientReportView, *errx.Error)
	// Public renders the report a token opens; nil, nil when the token opens nothing.
	Public(ctx context.Context, token string, rng models.ClientReportRange) (*PublicReport, *errx.Error)
	// Start runs the scheduled-email loop until ctx ends.
	Start(ctx context.Context, interval time.Duration)
}

// PublicReport is what a public link shows, and whose it is.
type PublicReport struct {
	OrganizationID uuid.UUID
	View           *models.ClientReportView
	Brand          brandkit.Brand
}

// Deps are the service's collaborators; Mail and Hosts may be nil.
type Deps struct {
	Repo      repository.ClientReportRepository
	Analytics analytics.AnalyticsService
	Brands    brandkit.Resolver
	Hosts     PublicHosts
	Mail      notify.EmailNotificationService
	// LinkKey derives the tokens inside scheduled emails (EmailLinkKey).
	LinkKey []byte
}

type service struct {
	Deps
	now func() time.Time
}

// New builds the service; a nil Brands resolves to the deployment's brand.
func New(d Deps) Service {
	if d.Brands == nil {
		d.Brands = brandkit.DeploymentResolver()
	}
	return &service{Deps: d, now: time.Now}
}

func (s *service) List(ctx context.Context, orgID uuid.UUID, cursor string, limit int) ([]models.ClientReport, *string, *errx.Error) {
	at, id, xerr := paging.DecodeTimeCursor(cursor)
	if xerr != nil {
		return nil, nil, xerr
	}
	var after *time.Time
	if id != uuid.Nil {
		after = &at
	}
	rows, err := s.Repo.List(ctx, orgID, after, id, limit+1)
	if err != nil {
		return nil, nil, errx.InternalError()
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = paging.EncodeTime(last.CreatedAt, last.ID)
	}
	return rows, next, nil
}

func (s *service) Get(ctx context.Context, orgID, id uuid.UUID) (*models.ClientReport, *errx.Error) {
	rep, err := s.Repo.Get(ctx, orgID, id)
	if err != nil {
		return nil, errx.InternalError()
	}
	if rep == nil {
		return nil, errNotFound
	}
	return rep, nil
}

func (s *service) Create(ctx context.Context, orgID, userID uuid.UUID, in *models.ClientReportInput) (*models.ClientReport, *errx.Error) {
	n, err := s.Repo.Count(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if n >= models.MaxClientReportsPerOrg {
		return nil, errLimitReached
	}
	rep := &models.ClientReport{OrganizationID: orgID, Access: *models.WorkspaceAccess(),
		Sections: models.DefaultClientReportSections(), DateRange: models.ClientReportLast30Days}
	if userID != uuid.Nil {
		rep.CreatedBy = &userID
	}
	if in.Name == nil {
		return nil, displayname.Error("name", displayname.Workspace, displayname.Empty)
	}
	if xerr := s.apply(ctx, rep, in); xerr != nil {
		return nil, xerr
	}
	if err := s.Repo.Create(ctx, rep); err != nil {
		return nil, errx.InternalError()
	}
	return s.Get(ctx, orgID, rep.ID)
}

func (s *service) Update(ctx context.Context, orgID, id uuid.UUID, in *models.ClientReportInput) (*models.ClientReport, *errx.Error) {
	rep, xerr := s.Get(ctx, orgID, id)
	if xerr != nil {
		return nil, xerr
	}
	if xerr := s.apply(ctx, rep, in); xerr != nil {
		return nil, xerr
	}
	ok, err := s.Repo.Update(ctx, rep)
	if err != nil {
		return nil, errx.InternalError()
	}
	if !ok {
		return nil, errNotFound
	}
	return s.Get(ctx, orgID, id)
}

// apply validates in onto rep; absent fields keep rep's value.
func (s *service) apply(ctx context.Context, rep *models.ClientReport, in *models.ClientReportInput) *errx.Error {
	if in.Name != nil {
		name, xerr := displayname.Validate("name", *in.Name, displayname.Workspace, false)
		if xerr != nil {
			return xerr
		}
		rep.Name = name
	}
	if in.Access != nil {
		access := *in.Access
		if xerr := s.validateAccess(ctx, rep.OrganizationID, &access); xerr != nil {
			return xerr
		}
		rep.Access = access
	}
	if in.Sections != nil {
		rep.Sections = *in.Sections
	}
	if in.ShowReplySnippets != nil {
		rep.ShowReplySnippets = *in.ShowReplySnippets
	}
	if in.DateRange != nil {
		if !in.DateRange.Valid() {
			return errInvalidRange
		}
		rep.DateRange = *in.DateRange
	}
	if rep.Sections == (models.ClientReportSections{}) {
		return errNoSections
	}
	return nil
}

// validateAccess normalizes a and refuses grants outside the workspace or an empty restriction.
func (s *service) validateAccess(ctx context.Context, orgID uuid.UUID, a *models.MemberAccess) *errx.Error {
	if a.Scope != "" && !a.Scope.Valid() {
		return scopeError("scope must be workspace or restricted.")
	}
	a.Normalize()
	if !a.Restricted() {
		return nil
	}
	if len(a.FolderIDs) > models.MaxAccessGrants || len(a.CampaignIDs) > models.MaxAccessGrants || len(a.EmailAccountIDs) > models.MaxAccessGrants {
		return scopeError("A report covers at most 500 of each of folders, campaigns and mailboxes.")
	}
	if len(a.FolderIDs)+len(a.CampaignIDs)+len(a.EmailAccountIDs) == 0 {
		return scopeError("Pick at least one campaign, folder or mailbox, or cover the whole workspace.")
	}
	folders, campaigns, mailboxes, err := s.Repo.OwnedCounts(ctx, orgID, a)
	if err != nil {
		return errx.InternalError()
	}
	if folders != len(a.FolderIDs) || campaigns != len(a.CampaignIDs) || mailboxes != len(a.EmailAccountIDs) {
		return scopeError("Some of the selected campaigns, folders or mailboxes are not in this workspace.")
	}
	return nil
}

func (s *service) Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error {
	ok, err := s.Repo.Delete(ctx, orgID, id)
	if err != nil {
		return errx.InternalError()
	}
	if !ok {
		return errNotFound
	}
	return nil
}

func (s *service) linkHost(ctx context.Context, orgID uuid.UUID) string {
	if s.Hosts == nil {
		return ""
	}
	return s.Hosts.PortalHost(ctx, orgID)
}

func (s *service) CreateLink(ctx context.Context, orgID, id uuid.UUID, req *models.CreateClientReportLinkRequest) (*models.ClientReportLinkResult, *errx.Error) {
	days := models.ClientReportLinkDefaultDays
	if req != nil && req.ExpiresInDays != nil {
		days = *req.ExpiresInDays
	}
	if days < 1 || days > models.ClientReportLinkMaxDays {
		return nil, errInvalidLink
	}
	if _, xerr := s.Get(ctx, orgID, id); xerr != nil {
		return nil, xerr
	}
	token, err := newShareToken()
	if err != nil {
		return nil, errx.InternalError()
	}
	url := config.ReportURLOn(s.linkHost(ctx, orgID), token)
	if url == "" {
		return nil, errLinksUnavailable
	}
	expires := s.now().Add(time.Duration(days) * 24 * time.Hour)
	ok, err := s.Repo.SetLink(ctx, orgID, id, hashToken(token), tokenHint(token), expires)
	if err != nil {
		return nil, errx.InternalError()
	}
	if !ok {
		return nil, errNotFound
	}
	rep, xerr := s.Get(ctx, orgID, id)
	if xerr != nil {
		return nil, xerr
	}
	out := &models.ClientReportLinkResult{URL: url, Token: token, Report: rep}
	if rep.Link != nil {
		out.Link = *rep.Link
	}
	return out, nil
}

func (s *service) RevokeLinks(ctx context.Context, orgID, id uuid.UUID) (*models.ClientReport, *errx.Error) {
	ok, err := s.Repo.RevokeLinks(ctx, orgID, id)
	if err != nil {
		return nil, errx.InternalError()
	}
	if !ok {
		return nil, errNotFound
	}
	return s.Get(ctx, orgID, id)
}

func (s *service) SetSchedule(ctx context.Context, orgID, id uuid.UUID, req *models.SetClientReportScheduleRequest) (*models.ClientReport, *errx.Error) {
	if s.Mail == nil {
		return nil, errEmailsUnavailable
	}
	if config.ReportURLOn(s.linkHost(ctx, orgID), "x") == "" {
		return nil, errLinksUnavailable
	}
	sched, xerr := normalizeSchedule(req)
	if xerr != nil {
		return nil, xerr
	}
	next, err := NextRun(*sched, s.now())
	if err != nil {
		return nil, scheduleError("This schedule has no next run.")
	}
	sched.NextRunAt = next
	if err := s.Repo.UpsertSchedule(ctx, orgID, id, sched); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errNotFound
		}
		return nil, errx.InternalError()
	}
	return s.Get(ctx, orgID, id)
}

func (s *service) DeleteSchedule(ctx context.Context, orgID, id uuid.UUID) (*models.ClientReport, *errx.Error) {
	rep, xerr := s.Get(ctx, orgID, id)
	if xerr != nil {
		return nil, xerr
	}
	if _, err := s.Repo.DeleteSchedule(ctx, orgID, id); err != nil {
		return nil, errx.InternalError()
	}
	rep.Schedule = nil
	return rep, nil
}

func (s *service) Preview(ctx context.Context, orgID, id uuid.UUID, rng models.ClientReportRange) (*models.ClientReportView, *errx.Error) {
	if rng != "" && !rng.Valid() {
		return nil, errInvalidRange
	}
	rep, xerr := s.Get(ctx, orgID, id)
	if xerr != nil {
		return nil, xerr
	}
	return s.render(ctx, rep, rng)
}

func (s *service) Public(ctx context.Context, token string, rng models.ClientReportRange) (*PublicReport, *errx.Error) {
	if !validTokenShape(token) {
		return nil, nil
	}
	if rng != "" && !rng.Valid() {
		return nil, errInvalidRange
	}
	hit, err := s.Repo.ResolveToken(ctx, hashToken(token), s.now())
	if err != nil {
		return nil, errx.InternalError()
	}
	if hit == nil {
		return nil, nil
	}
	if rng == "" {
		rng = hit.Range
	}
	view, xerr := s.render(ctx, hit.Report, rng)
	if xerr != nil {
		return nil, xerr
	}
	brand, err := s.Brands.ForOrganization(ctx, hit.Report.OrganizationID)
	if err != nil {
		brand = brandkit.Default()
	}
	return &PublicReport{OrganizationID: hit.Report.OrganizationID, View: view, Brand: brand}, nil
}

// render reads the report's numbers for rng inside the report's scope and organization, and nothing else.
func (s *service) render(ctx context.Context, rep *models.ClientReport, rng models.ClientReportRange) (*models.ClientReportView, *errx.Error) {
	if rng == "" {
		rng = rep.DateRange
	}
	now := s.now()
	from, to := rng.Window(now)
	q, xerr := s.query(ctx, rep, from, to)
	if xerr != nil {
		return nil, xerr
	}
	data, xerr := s.Analytics.GetClientReportAnalytics(ctx, rep.OrganizationID, *q)
	if xerr != nil {
		return nil, xerr
	}
	return buildView(rep, rng, from, to, now, data), nil
}

// query resolves the report's scope to the campaign and mailbox sets the analytics reads.
func (s *service) query(ctx context.Context, rep *models.ClientReport, from, to time.Time) (*models.ClientReportQuery, *errx.Error) {
	q := &models.ClientReportQuery{From: from, To: to, Sections: rep.Sections, ReplySnippets: rep.ShowReplySnippets}
	if !rep.Access.Restricted() {
		return q, nil
	}
	scope, err := s.Repo.ResolveScope(ctx, rep.OrganizationID, &rep.Access)
	if err != nil {
		return nil, errx.InternalError()
	}
	return restrictQuery(q, scope), nil
}

// restrictQuery holds q to scope; an empty grant reads nothing rather than everything.
func restrictQuery(q *models.ClientReportQuery, scope *models.ResourceScope) *models.ClientReportQuery {
	campaigns := append([]uuid.UUID{}, scope.Campaigns...)
	mailboxes := append([]uuid.UUID{}, scope.Mailboxes...)
	q.Campaigns = &models.CampaignScope{CampaignIDs: campaigns}
	q.Mailboxes = mailboxes
	return q
}

func (s *service) Start(ctx context.Context, interval time.Duration) {
	if s.Mail == nil {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		s.runOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

const (
	scheduleBatch      = 100
	deliveryBatch      = 50
	deliveryMaxAttempt = 3
)

func (s *service) runOnce(ctx context.Context) {
	now := s.now()
	due, err := s.Repo.DueSchedules(ctx, now, scheduleBatch)
	if err != nil {
		log.Warn().Err(err).Msg("client reports: reading due schedules failed")
	}
	for _, d := range due {
		s.advance(ctx, d, now)
	}
	claimed, err := s.Repo.ClaimDeliveries(ctx, now, deliveryBatch, deliveryMaxAttempt)
	if err != nil {
		log.Warn().Err(err).Msg("client reports: claiming deliveries failed")
		return
	}
	for i := range claimed {
		s.deliver(ctx, &claimed[i])
	}
}

// advance records one run of a due schedule and moves it to its next slot; a missed slot is run once, not once per missed period.
func (s *service) advance(ctx context.Context, due repository.DueClientReportSchedule, now time.Time) {
	next, err := NextRun(due.Schedule, now)
	if err != nil {
		log.Warn().Err(err).Str("report_id", due.ReportID.String()).Msg("client reports: schedule has no next run")
		return
	}
	d := &models.ClientReportDelivery{ID: uuid.New(), ReportID: due.ReportID, OrganizationID: due.OrganizationID,
		RunAt: due.Schedule.NextRunAt, DateRange: due.Schedule.Cadence.EmailRange(), Recipients: due.Schedule.Recipients,
		ExpiresAt: now.Add(models.ClientReportEmailLinkDays * 24 * time.Hour)}
	if _, err := s.Repo.RecordRun(ctx, due, next, d, hashToken(emailToken(s.LinkKey, d.ID))); err != nil {
		log.Warn().Err(err).Str("report_id", due.ReportID.String()).Msg("client reports: recording a run failed")
	}
}

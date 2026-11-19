package partner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/tz"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	defaultSendsPerMailboxPerDay = 50
	maxSendsPerMailboxPerDay     = config.LimitMax
	maxExternalID                = 255
)

// Limits are the per-mailbox limits a partner sets.
type Limits struct {
	CampaignSendsPerMailboxPerDay int `json:"campaign_sends_per_mailbox_per_day"`
}

// GroupInput is the partner's grouping of the workspace (a company or an agency) with its brand.
type GroupInput struct {
	ExternalID string          `json:"external_id"`
	Name       string          `json:"name"`
	Branding   json.RawMessage `json:"branding,omitempty"`
}

// PersonInput is a person at the partner, identified by the partner's id for them.
type PersonInput struct {
	ExternalID string `json:"external_id"`
	Email      string `json:"email"`
	Name       string `json:"name"`
	Role       string `json:"role"`
}

// OrganizationInput is the PUT /organizations/{external_id} body. Unknown fields are ignored.
type OrganizationInput struct {
	Name            string       `json:"name"`
	Plan            string       `json:"plan"`
	State           string       `json:"state"`
	SuspendedReason string       `json:"suspended_reason"`
	Limits          *Limits      `json:"limits"`
	Group           *GroupInput  `json:"group"`
	Owner           *PersonInput `json:"owner"`
	Timezone        string       `json:"timezone"`
}

// EnabledCounts are mailboxes per enablement.
type EnabledCounts struct {
	SendingWarmup int `json:"sending_warmup"`
	WarmupOnly    int `json:"warmup_only"`
}

// Usage is the workspace's activity as the partner sees it.
type Usage struct {
	SendsToday      int64         `json:"sends_today"`
	Mailboxes       int           `json:"mailboxes"`
	Enabled         EnabledCounts `json:"enabled"`
	Contacts        int64         `json:"contacts"`
	ActiveCampaigns int           `json:"active_campaigns"`
	AIBalanceMicros *int64        `json:"ai_balance_micros"`
}

// Warmup summarises warming mailboxes by health.
type Warmup struct {
	Enabled      int  `json:"enabled"`
	Healthy      int  `json:"healthy"`
	Watch        int  `json:"watch"`
	Problem      int  `json:"problem"`
	AverageScore *int `json:"average_score"`
}

// GroupView is the group's brand as Warmbly applies it to every workspace sent with it.
type GroupView struct {
	ExternalID string      `json:"external_id"`
	Branded    bool        `json:"branded"`
	PoweredBy  bool        `json:"powered_by"`
	Portal     *PortalView `json:"portal"`
}

// Organization is the partner API's view of a managed workspace.
type Organization struct {
	ID              string     `json:"id"`
	ExternalID      string     `json:"external_id"`
	Name            string     `json:"name"`
	Plan            string     `json:"plan"`
	State           string     `json:"state"`
	SuspendedReason *string    `json:"suspended_reason"`
	DeletesAt       *time.Time `json:"deletes_at"`
	AppURL          string     `json:"app_url"`
	Limits          Limits     `json:"limits"`
	Group           *GroupView `json:"group"`
	Usage           Usage      `json:"usage"`
	Warmup          Warmup     `json:"warmup"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// ResolveOrganization is the caller's workspace under externalID, scoped to the caller's partner and mode.
// Every partner route that names an organization goes through it.
func (s *Service) ResolveOrganization(ctx context.Context, c *Caller, externalID string) (*models.PartnerOrganization, *errx.Error) {
	if c == nil {
		return nil, ErrUnauthorized
	}
	if externalID == "" || len(externalID) > maxExternalID {
		return nil, ErrNotFound
	}
	po, err := s.Repo.GetPartnerOrg(ctx, c.Partner.ID, c.Mode, externalID)
	if err != nil {
		return nil, internal(err)
	}
	if po == nil || po.OrganizationID == nil {
		return nil, ErrNotFound
	}
	return po, nil
}

// View builds the Organization answer for a workspace.
func (s *Service) View(ctx context.Context, po *models.PartnerOrganization) (*Organization, *errx.Error) {
	v := &Organization{
		ExternalID: po.ExternalID, Name: po.OrganizationName, Plan: po.PlanKey, State: string(po.State),
		DeletesAt: utcPtr(po.DeletesAt), AppURL: appURL(), CreatedAt: po.CreatedAt.UTC(), UpdatedAt: po.UpdatedAt.UTC(),
		Limits: Limits{CampaignSendsPerMailboxPerDay: po.CampaignSendsPerMailboxPerDay},
	}
	if po.State != models.PartnerOrgActive {
		v.SuspendedReason = po.SuspendedReason
	}
	v.Group = s.groupView(ctx, po)
	if po.OrganizationID == nil {
		return v, nil
	}
	orgID := *po.OrganizationID
	v.ID = orgID.String()
	if s.Counts != nil {
		counts, xerr := s.Counts.GetOrganizationCounts(ctx, orgID)
		if xerr != nil {
			return nil, xerr
		}
		v.Usage.SendsToday = int64(counts.EmailsSentToday)
		v.Usage.Mailboxes = counts.EmailAccounts
		v.Usage.Contacts = int64(counts.TotalContacts)
		v.Usage.ActiveCampaigns = counts.ActiveCampaigns
	}
	if s.Balances != nil {
		l, err := s.Balances.GetBalance(ctx, orgID)
		if err != nil {
			return nil, internal(err)
		}
		var total int64
		if l != nil {
			total = int64(l.Total())
		}
		v.Usage.AIBalanceMicros = &total
	}
	if s.enabled != nil {
		e, err := s.enabled.EnabledCounts(ctx, orgID)
		if err != nil {
			return nil, internal(err)
		}
		v.Usage.Enabled = e
	}
	w, err := s.Repo.PartnerWarmupSummary(ctx, orgID)
	if err != nil {
		return nil, internal(err)
	}
	v.Warmup = Warmup{Enabled: w.Enabled, Healthy: w.Healthy, Watch: w.Watch, Problem: w.Problem}
	if s.mailboxesOn() {
		sum, n, err := s.mb.Repo.PartnerWarmupScores(ctx, orgID)
		if err != nil {
			return nil, internal(err)
		}
		if n > 0 {
			avg := int(math.Round(float64(sum) / float64(n)))
			v.Warmup.AverageScore = &avg
		}
	}
	return v, nil
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func appURL() string {
	if base := config.AppBaseURL(); base != "" {
		return base + "/app"
	}
	return ""
}

func validateState(in *OrganizationInput) (models.PartnerOrgState, *string, *errx.Error) {
	switch models.PartnerOrgState(in.State) {
	case models.PartnerOrgActive:
		return models.PartnerOrgActive, nil, nil
	case models.PartnerOrgSuspended:
		reason := strings.TrimSpace(in.SuspendedReason)
		if reason == "" {
			reason = "reseller"
		}
		if !models.PartnerSuspendReasons[reason] {
			return "", nil, invalid("suspended_reason is billing, admin or reseller.")
		}
		return models.PartnerOrgSuspended, &reason, nil
	}
	return "", nil, invalid("state is active or suspended.")
}

func (s *Service) validatePlan(c *Caller, plan string) *errx.Error {
	if _, known := models.PartnerPlanIDs[plan]; !known || !slices.Contains(c.Partner.AllowedPlans, plan) {
		return ErrPlanUnknown
	}
	return nil
}

func applyGroup(po *models.PartnerOrganization, g *GroupInput) *errx.Error {
	if g == nil {
		return nil
	}
	id := strings.TrimSpace(g.ExternalID)
	if id == "" {
		po.GroupExternalID, po.GroupName, po.GroupBranding = nil, nil, nil
		return nil
	}
	if len(id) > maxExternalID {
		return invalid("group.external_id is at most 255 characters.")
	}
	if len(g.Branding) > 16<<10 {
		return invalid("group.branding is too large.")
	}
	name := displayname.CleanOr(g.Name, displayname.Workspace, "")
	po.GroupExternalID = &id
	po.GroupName = &name
	if len(g.Branding) > 0 && string(g.Branding) != "null" {
		if !json.Valid(g.Branding) {
			return invalid("group.branding is not valid JSON.")
		}
		po.GroupBranding = g.Branding
	} else {
		po.GroupBranding = nil
	}
	return nil
}

// Upsert creates the workspace on first sight of externalID and converges it to the body afterwards.
func (s *Service) Upsert(ctx context.Context, c *Caller, externalID string, in OrganizationInput) (*Organization, bool, *errx.Error) {
	externalID = strings.TrimSpace(externalID)
	if externalID == "" || len(externalID) > maxExternalID {
		return nil, false, invalid("external_id is 1-255 characters.")
	}
	if strings.TrimSpace(in.Name) == "" || in.Plan == "" {
		return nil, false, invalid("name and plan are required.")
	}
	if xerr := s.validatePlan(c, in.Plan); xerr != nil {
		return nil, false, xerr
	}
	state, reason, xerr := validateState(&in)
	if xerr != nil {
		return nil, false, xerr
	}
	limit := 0
	if in.Limits != nil {
		limit = in.Limits.CampaignSendsPerMailboxPerDay
		if limit < 0 || limit > maxSendsPerMailboxPerDay {
			return nil, false, invalid(fmt.Sprintf("limits.campaign_sends_per_mailbox_per_day is 1 to %d.", maxSendsPerMailboxPerDay))
		}
	}
	timezone := strings.TrimSpace(in.Timezone)
	if timezone != "" && !tz.Valid(timezone) {
		return nil, false, invalid("timezone is not a known IANA zone.")
	}
	name := displayname.CleanOr(in.Name, displayname.Workspace, "Workspace")

	po, err := s.Repo.GetPartnerOrg(ctx, c.Partner.ID, c.Mode, externalID)
	if err != nil {
		return nil, false, internal(err)
	}
	if po == nil {
		created, xerr := s.create(ctx, c, externalID, name, timezone, in, state, reason, limit)
		if xerr == nil {
			v, xerr := s.View(ctx, created)
			return v, true, xerr
		}
		if xerr != errOrgRace {
			return nil, false, xerr
		}
		if po, err = s.Repo.GetPartnerOrg(ctx, c.Partner.ID, c.Mode, externalID); err != nil || po == nil {
			return nil, false, internal(fmt.Errorf("partner organization vanished after a create race: %w", err))
		}
	}
	if po.OrganizationID == nil {
		return nil, false, ErrOrgDeleting
	}
	before := *po
	if po.State == models.PartnerOrgCancelled {
		if s.Deletions != nil {
			if xerr := s.Deletions.CancelOrganizationDeletion(ctx, *po.OrganizationID, c.Partner.ServiceUserID, &models.CancelDeletionRequest{Reason: "Restored by partner"}); xerr != nil && xerr.Code != errx.BadRequest {
				return nil, false, xerr
			}
		}
		po.CancelledAt, po.DeletesAt = nil, nil
	}
	po.PlanKey, po.State, po.SuspendedReason = in.Plan, state, reason
	if limit > 0 {
		po.CampaignSendsPerMailboxPerDay = limit
	}
	if xerr := applyGroup(po, in.Group); xerr != nil {
		return nil, false, xerr
	}
	if xerr := s.saveGroup(ctx, c, in.Group); xerr != nil {
		return nil, false, xerr
	}
	if err := s.Repo.SavePartnerOrg(ctx, po, name); err != nil {
		return nil, false, internal(err)
	}
	s.forget(*po.OrganizationID)
	po.OrganizationName = name
	v, xerr := s.View(ctx, po)
	if xerr != nil {
		return nil, false, xerr
	}
	if before.State != po.State || before.PlanKey != po.PlanKey || before.CampaignSendsPerMailboxPerDay != po.CampaignSendsPerMailboxPerDay ||
		ptrString(before.SuspendedReason) != ptrString(po.SuspendedReason) {
		s.publishOrg(ctx, c.Partner.ID, c.Mode, v)
		s.audit(ctx, c.Partner, *po.OrganizationID, models.AuditActionUpdate, models.AuditEntitySubscription, map[string]string{
			"partner_state": string(po.State), "plan": po.PlanKey,
		})
	}
	if before.OrganizationName != name {
		s.audit(ctx, c.Partner, *po.OrganizationID, models.AuditActionUpdate, models.AuditEntityOrganization, map[string]string{"name": name})
	}
	return v, false, nil
}

func ptrString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

var errOrgRace = errx.New(errx.Conflict, "partner organization created concurrently")

func (s *Service) create(ctx context.Context, c *Caller, externalID, name, timezone string, in OrganizationInput, state models.PartnerOrgState, reason *string, limit int) (*models.PartnerOrganization, *errx.Error) {
	if limit == 0 {
		limit = defaultSendsPerMailboxPerDay
	}
	link := models.PartnerOrganization{ID: uuid.New(), PartnerID: c.Partner.ID, Mode: c.Mode, ExternalID: externalID,
		PlanKey: in.Plan, State: state, SuspendedReason: reason, CampaignSendsPerMailboxPerDay: limit}
	if xerr := applyGroup(&link, in.Group); xerr != nil {
		return nil, xerr
	}
	if xerr := s.saveGroup(ctx, c, in.Group); xerr != nil {
		return nil, xerr
	}
	var owner *PersonInput
	if in.Owner != nil && strings.TrimSpace(in.Owner.ExternalID) != "" {
		owner = in.Owner
		if xerr := validatePerson(owner); xerr != nil {
			return nil, xerr
		}
	}
	orgID := uuid.New()
	po, err := s.Repo.CreateManagedOrganization(ctx, repository.ManagedOrgCreate{
		OrganizationID: orgID, Name: name, Slug: slugFor(name), Timezone: timezone,
		ServiceUserID: c.Partner.ServiceUserID, PartnerName: c.Partner.Name, Link: link,
	})
	if errors.Is(err, repository.ErrPartnerOrgExists) {
		return nil, errOrgRace
	}
	if err != nil {
		return nil, internal(err)
	}
	s.markLinked()
	for _, seed := range s.Seeders {
		seed(ctx, orgID)
	}
	if owner != nil {
		if _, _, xerr := s.ensurePerson(ctx, c, orgID, owner); xerr != nil {
			return nil, xerr
		}
	}
	s.audit(ctx, c.Partner, orgID, models.AuditActionCreate, models.AuditEntityOrganization, map[string]string{"external_id": externalID})
	return po, nil
}

func slugFor(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-':
			b.WriteByte('-')
		}
		if b.Len() >= 60 {
			break
		}
	}
	return strings.Trim(b.String(), "-") + "-" + randomHex(4)
}

// Get is GET /organizations/{external_id}.
func (s *Service) Get(ctx context.Context, c *Caller, externalID string) (*Organization, *errx.Error) {
	po, xerr := s.ResolveOrganization(ctx, c, externalID)
	if xerr != nil {
		return nil, xerr
	}
	return s.View(ctx, po)
}

// Page is a cursor list answer.
type Page[T any] struct {
	Data       []T        `json:"data"`
	Pagination Pagination `json:"pagination"`
}

type Pagination struct {
	NextCursor *string `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}

// EncodeCursor is an opaque cursor after one workspace.
func EncodeCursor(at time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id.String()))
}

// DecodeCursor reads EncodeCursor.
func DecodeCursor(s string) (*repository.PartnerOrgCursor, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, false
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return nil, false
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return nil, false
	}
	u, err := uuid.Parse(id)
	if err != nil {
		return nil, false
	}
	return &repository.PartnerOrgCursor{CreatedAt: t, ID: u}, true
}

// ParseLimit reads ?limit= (default 50, at most max); anything else is a 400.
func ParseLimit(raw string, max int) (int, *errx.Error) {
	if raw == "" {
		return min(50, max), nil
	}
	var n int
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil || n < 1 || n > max || fmt.Sprint(n) != raw {
		return 0, invalid(fmt.Sprintf("limit is 1 to %d.", max))
	}
	return n, nil
}

// List is GET /organizations, newest first.
func (s *Service) List(ctx context.Context, c *Caller, cursor string, limit int) (*Page[Organization], *errx.Error) {
	var after *repository.PartnerOrgCursor
	if cursor != "" {
		var ok bool
		if after, ok = DecodeCursor(cursor); !ok {
			return nil, invalid("cursor is not valid.")
		}
	}
	mode := c.Mode
	rows, err := s.Repo.ListPartnerOrgs(ctx, c.Partner.ID, &mode, after, limit+1)
	if err != nil {
		return nil, internal(err)
	}
	page := &Page[Organization]{Data: []Organization{}}
	if len(rows) > limit {
		rows = rows[:limit]
		page.Pagination.HasMore = true
		next := EncodeCursor(rows[limit-1].CreatedAt, rows[limit-1].ID)
		page.Pagination.NextCursor = &next
	}
	for i := range rows {
		v, xerr := s.View(ctx, &rows[i])
		if xerr != nil {
			return nil, xerr
		}
		page.Data = append(page.Data, *v)
	}
	return page, nil
}

// Cancel suspends the workspace at once, revokes the partner's keys in it and deletes it after the retention period.
func (s *Service) Cancel(ctx context.Context, c *Caller, externalID string) (*Organization, *errx.Error) {
	po, xerr := s.ResolveOrganization(ctx, c, externalID)
	if xerr != nil {
		return nil, xerr
	}
	if po.State == models.PartnerOrgCancelled {
		return s.View(ctx, po)
	}
	orgID := *po.OrganizationID
	now := s.now().UTC()
	deletes := now.Add(models.PartnerCancelRetention)
	if s.Deletions != nil {
		d, xerr := s.Deletions.ScheduleOrganizationDeletion(ctx, orgID, c.Partner.ServiceUserID, &models.ScheduleDeletionRequest{
			Confirmation: po.OrganizationName, Reason: "Cancelled by partner " + c.Partner.Name,
		})
		switch {
		case xerr == nil:
			deletes = d.ExecuteAfter
		case xerr.Code == errx.Conflict && xerr.Identifier == "":
			// Already scheduled; the retention clock keeps its original end.
		default:
			return nil, xerr
		}
	}
	reason := "reseller"
	po.State, po.SuspendedReason, po.CancelledAt, po.DeletesAt = models.PartnerOrgCancelled, &reason, &now, &deletes
	if err := s.Repo.SavePartnerOrg(ctx, po, ""); err != nil {
		return nil, internal(err)
	}
	s.forget(orgID)
	if xerr := s.revokePartnerKeys(ctx, c.Partner, orgID); xerr != nil {
		return nil, xerr
	}
	// A cancelled workspace bills nothing: every mailbox is off from now.
	s.turnOffAll(ctx, orgID)
	v, xerr := s.View(ctx, po)
	if xerr != nil {
		return nil, xerr
	}
	s.publishOrg(ctx, c.Partner.ID, c.Mode, v)
	s.audit(ctx, c.Partner, orgID, models.AuditActionUpdate, models.AuditEntitySubscription, map[string]string{"partner_state": string(po.State)})
	return v, nil
}

// SweepDeleted tells partners about workspaces the deletion job removed, then forgets them.
func (s *Service) SweepDeleted(ctx context.Context) error {
	for {
		gone, err := s.Repo.TakeDeletedPartnerOrgs(ctx, 100)
		if err != nil {
			return err
		}
		for _, po := range gone {
			data := map[string]any{"external_id": po.ExternalID, "state": "deleted"}
			if err := s.Publish(ctx, po.PartnerID, po.Mode, models.PartnerEventOrganizationDeleted, po.ExternalID, data); err != nil {
				errs.CaptureException(err)
			}
		}
		if len(gone) < 100 {
			break
		}
	}
	now := s.now()
	return s.Repo.PrunePartnerRecords(ctx, now.Add(-idempotencyTTL), now.Add(-eventRetention))
}

func (s *Service) publishOrg(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, v *Organization) {
	if err := s.Publish(ctx, partnerID, mode, models.PartnerEventOrganizationUpdated, v.ExternalID, v); err != nil {
		errs.CaptureException(err)
	}
}

func (s *Service) audit(ctx context.Context, p *models.Partner, orgID uuid.UUID, action models.AuditAction, entity models.AuditEntityType, meta map[string]string) {
	if s.Audit == nil {
		return
	}
	if meta == nil {
		meta = map[string]string{}
	}
	meta["actor_type"] = "partner"
	meta["partner"] = p.Slug
	id := orgID
	if entity != models.AuditEntityOrganization && entity != models.AuditEntitySubscription {
		s.Audit.LogAction(ctx, orgID, p.ServiceUserID, action, entity, nil, "", "", nil, meta)
		return
	}
	s.Audit.LogAction(ctx, orgID, p.ServiceUserID, action, entity, &id, "", "", nil, meta)
}

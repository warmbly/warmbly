// Package agencyhome serves the agency home and the cross-client inbox: reads
// across every client workspace of an agency, each one limited to what the
// caller's own membership in that workspace allows.
package agencyhome

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/repository"
)

// overviewTTL is how long one caller's agency home is reused.
const overviewTTL = 20 * time.Second

// Inbox page sizes.
const (
	DefaultInboxLimit = 30
	MaxInboxLimit     = 100
)

// Agencies is the slice of the agency service this package needs.
type Agencies interface {
	Get(ctx context.Context, agencyID, userID uuid.UUID) (*models.Agency, *errx.Error)
	Usage(ctx context.Context, agencyID, userID uuid.UUID) (*models.AgencyUsage, *errx.Error)
}

// Scopes resolves a restricted member's grants with the same rules the workspace routes use.
type Scopes interface {
	ResolveMemberScope(ctx context.Context, orgID, userID uuid.UUID) (*models.ResourceScope, *errx.Error)
}

// Unibox reads one workspace's conversations.
type Unibox interface {
	GetByThreadWithin(ctx context.Context, orgID, emailID uuid.UUID, threadID, limit, cursor string, allowed []uuid.UUID) (*models.MailSearchResult, *errx.Error)
	Peek(ctx context.Context, orgID, id uuid.UUID) (*models.EmailMessage, *errx.Error)
}

// UniboxGate is the plan check every inbox read passes.
type UniboxGate interface {
	CanUseUnibox(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error)
}

type Service struct {
	agencies Agencies
	repo     repository.AgencyHomeRepository
	scopes   Scopes
	unibox   Unibox
	gate     UniboxGate
	now      func() time.Time

	mu    sync.Mutex
	cache map[cacheKey]cached
}

type cacheKey struct{ agency, user uuid.UUID }

type cached struct {
	at       time.Time
	overview *models.AgencyOverview
}

func NewService(agencies Agencies, repo repository.AgencyHomeRepository, scopes Scopes, unibox Unibox, gate UniboxGate) *Service {
	return &Service{agencies: agencies, repo: repo, scopes: scopes, unibox: unibox, gate: gate, now: time.Now, cache: map[cacheKey]cached{}}
}

var (
	errClientNotFound = errx.NewWithIdentifier(errx.NotFound, "agency_client_not_found", "That workspace is not one of this agency's clients.")
	errClientInbox    = errx.NewWithIdentifier(errx.Forbidden, "agency_client_inbox_forbidden", "You do not have access to this client's inbox.")
	errUniboxPlan     = errx.New(errx.Forbidden, "Unibox requires an active trial or paid subscription")
	errInvalidCursor  = errx.NewWithIdentifier(errx.BadRequest, "invalid_cursor", "The cursor is not valid.")
	errInvalidLimit   = errx.NewWithIdentifier(errx.BadRequest, "invalid_limit", "limit must be between 1 and 100.")
	errMessageMissing = errx.New(errx.NotFound, "message not found")
)

func internal(err error) *errx.Error {
	errs.CaptureException(err)
	return errx.InternalError()
}

// memberships checks agency staff, then lists the agency's clients with the caller's membership in each.
func (s *Service) memberships(ctx context.Context, agencyID, userID uuid.UUID) ([]models.AgencyClientMembership, *errx.Error) {
	if _, xerr := s.agencies.Get(ctx, agencyID, userID); xerr != nil {
		return nil, xerr
	}
	list, err := s.repo.ClientMemberships(ctx, agencyID, userID)
	if err != nil {
		return nil, internal(err)
	}
	return list, nil
}

// metricOrgs splits the clients by which overview sections the caller may read in each.
type metricOrgs struct {
	campaigns, inbox, advisor []uuid.UUID
}

func partition(list []models.AgencyClientMembership) metricOrgs {
	var p metricOrgs
	for _, m := range list {
		if m.Can(models.PermViewCampaigns) {
			p.campaigns = append(p.campaigns, m.OrganizationID)
		}
		if m.Can(models.PermAccessUnibox) {
			p.inbox = append(p.inbox, m.OrganizationID)
		}
		if m.Can(models.PermViewAnalytics) {
			p.advisor = append(p.advisor, m.OrganizationID)
		}
	}
	return p
}

// Overview is the agency home for one caller, briefly cached.
func (s *Service) Overview(ctx context.Context, agencyID, userID uuid.UUID) (*models.AgencyOverview, *errx.Error) {
	now := s.now()
	key := cacheKey{agencyID, userID}
	list, xerr := s.memberships(ctx, agencyID, userID)
	if xerr != nil {
		return nil, xerr
	}
	s.mu.Lock()
	hit, ok := s.cache[key]
	s.mu.Unlock()
	if ok && now.Sub(hit.at) < overviewTTL {
		return hit.overview, nil
	}

	usage, xerr := s.agencies.Usage(ctx, agencyID, userID)
	if xerr != nil {
		return nil, xerr
	}
	p := partition(list)
	campaigns, err := s.repo.CampaignStats(ctx, p.campaigns, now.Add(-7*24*time.Hour))
	if err != nil {
		return nil, internal(err)
	}
	// The mailbox list is gated on view_campaigns, so its health is too.
	mailboxes, err := s.repo.MailboxStats(ctx, p.campaigns)
	if err != nil {
		return nil, internal(err)
	}
	replies, err := s.repo.RepliesWaiting(ctx, p.inbox)
	if err != nil {
		return nil, internal(err)
	}
	advisor, err := s.repo.AdvisorCounts(ctx, p.advisor)
	if err != nil {
		return nil, internal(err)
	}

	used := map[uuid.UUID]int64{}
	for _, c := range usage.Clients {
		used[c.OrganizationID] = c.Used
	}
	o := &models.AgencyOverview{GeneratedAt: now, Usage: usage, Attention: []models.AgencyAttention{}, Clients: make([]models.AgencyOverviewClient, 0, len(list))}
	for _, m := range list {
		c := models.AgencyOverviewClient{
			OrganizationID: m.OrganizationID, Name: m.Name, AvatarURL: m.AvatarURL, Access: m.Access(),
			PoolUsed: used[m.OrganizationID], MonthlySendLimit: m.MonthlySendLimit,
		}
		if m.Can(models.PermViewCampaigns) {
			c.Campaigns = orZero(campaigns[m.OrganizationID])
			c.Mailboxes = orZero(mailboxes[m.OrganizationID])
		}
		if m.Can(models.PermAccessUnibox) {
			c.Inbox = &models.AgencyClientInbox{RepliesWaiting: replies[m.OrganizationID]}
		}
		if m.Can(models.PermViewAnalytics) {
			c.Advisor = orZero(advisor[m.OrganizationID])
		}
		o.Clients = append(o.Clients, c)
	}
	o.Totals = totals(o.Clients)
	o.Attention = attention(usage, o.Clients)

	s.mu.Lock()
	for k, v := range s.cache {
		if now.Sub(v.at) >= overviewTTL {
			delete(s.cache, k)
		}
	}
	s.cache[key] = cached{at: now, overview: o}
	s.mu.Unlock()
	return o, nil
}

func orZero[T any](v *T) *T {
	if v == nil {
		return new(T)
	}
	return v
}

func totals(clients []models.AgencyOverviewClient) models.AgencyOverviewTotals {
	t := models.AgencyOverviewTotals{Clients: len(clients)}
	for _, c := range clients {
		if c.Campaigns != nil {
			t.SentToday += c.Campaigns.SentToday
			t.PositiveReplies7d += c.Campaigns.PositiveReplies7d
			t.ActiveCampaigns += c.Campaigns.Active
		}
		if c.Inbox != nil {
			t.RepliesWaiting += c.Inbox.RepliesWaiting
		}
		if c.Mailboxes != nil {
			t.Mailboxes += c.Mailboxes.Total
			t.MailboxProblems += c.Mailboxes.Disconnected + c.Mailboxes.Erroring + c.Mailboxes.Quarantined + c.Mailboxes.Blocked
		}
	}
	return t
}

// Attention thresholds. The bounce lines are the pool's quarantine and block bands.
const (
	bounceSpikeMinSends = 20
	bounceSpikeWarnRate = 0.05
	bounceSpikeCritRate = 0.10
	poolLowShare        = 0.10
	clientCapNearShare  = 0.90
	severityCritical    = "critical"
	severityWarning     = "warning"
)

// attention lists what needs someone now, worst first: the pool, then each client's problems.
func attention(usage *models.AgencyUsage, clients []models.AgencyOverviewClient) []models.AgencyAttention {
	out := []models.AgencyAttention{}
	if usage != nil && !usage.Unlimited && usage.Total > 0 {
		switch {
		case usage.Remaining == 0:
			out = append(out, models.AgencyAttention{Kind: models.AttentionPoolLow, Severity: severityCritical, Count: usage.Remaining})
		case float64(usage.Remaining) <= float64(usage.Total)*poolLowShare || usage.Projected > usage.Total:
			out = append(out, models.AgencyAttention{Kind: models.AttentionPoolLow, Severity: severityWarning, Count: usage.Remaining})
		}
	}
	for _, c := range clients {
		id := c.OrganizationID
		add := func(kind models.AgencyAttentionKind, severity string, n int64, rate float64) {
			out = append(out, models.AgencyAttention{Kind: kind, Severity: severity, OrganizationID: &id, ClientName: c.Name, Count: n, Rate: rate})
		}
		if m := c.Mailboxes; m != nil {
			if n := m.Disconnected + m.Erroring; n > 0 {
				add(models.AttentionMailboxDisconnected, severityCritical, int64(n), 0)
			}
			if n := m.Quarantined + m.Blocked; n > 0 {
				add(models.AttentionMailboxHeld, severityCritical, int64(n), 0)
			}
		}
		if k := c.Campaigns; k != nil {
			if k.Sent7d >= bounceSpikeMinSends && k.BounceRate7d >= bounceSpikeWarnRate {
				sev := severityWarning
				if k.BounceRate7d >= bounceSpikeCritRate {
					sev = severityCritical
				}
				add(models.AttentionBounceSpike, sev, k.Bounced7d, k.BounceRate7d)
			}
			if k.PausedByBilling > 0 {
				add(models.AttentionCampaignsPausedBilling, severityCritical, int64(k.PausedByBilling), 0)
			}
			if k.PausedByLimits > 0 {
				add(models.AttentionCampaignsPausedLimits, severityWarning, int64(k.PausedByLimits), 0)
			}
		}
		if c.MonthlySendLimit != nil && *c.MonthlySendLimit > 0 {
			share := float64(c.PoolUsed) / float64(*c.MonthlySendLimit)
			if share >= clientCapNearShare {
				sev := severityWarning
				if share >= 1 {
					sev = severityCritical
				}
				add(models.AttentionClientCapNear, sev, c.PoolUsed, share)
			}
		}
	}
	// Critical first, keeping the pool and client order within a severity.
	sorted := make([]models.AgencyAttention, 0, len(out))
	for _, sev := range []string{severityCritical, severityWarning} {
		for _, a := range out {
			if a.Severity == sev {
				sorted = append(sorted, a)
			}
		}
	}
	return sorted
}

// inboxScope is where the caller reads mail across the agency's clients.
type inboxScope struct {
	whole     []uuid.UUID
	scoped    []uuid.UUID
	mailboxes []uuid.UUID
}

// resolveInbox lists the clients whose inbox the caller reads, narrowed to filter when set.
func (s *Service) resolveInbox(ctx context.Context, list []models.AgencyClientMembership, userID uuid.UUID, filter map[uuid.UUID]bool) (*inboxScope, *errx.Error) {
	sc := &inboxScope{}
	for _, m := range list {
		if !m.CanReadInbox() || (filter != nil && !filter[m.OrganizationID]) {
			continue
		}
		if s.gate != nil {
			ok, xerr := s.gate.CanUseUnibox(ctx, m.OrganizationID)
			if xerr != nil {
				return nil, xerr
			}
			if !ok {
				continue
			}
		}
		if !m.Restricted() {
			sc.whole = append(sc.whole, m.OrganizationID)
			continue
		}
		// Restricted memberships are rare; each resolves through the workspace routes' own rules.
		scope, xerr := s.scopes.ResolveMemberScope(ctx, m.OrganizationID, userID)
		if xerr != nil {
			return nil, xerr
		}
		if scope == nil || len(scope.Mailboxes) == 0 {
			continue
		}
		sc.scoped = append(sc.scoped, m.OrganizationID)
		sc.mailboxes = append(sc.mailboxes, scope.Mailboxes...)
	}
	return sc, nil
}

// InboxParams are the cross-client inbox's filters as the request gave them.
type InboxParams struct {
	Clients    []uuid.UUID
	Unread     bool
	NeedsReply bool
	Positive   bool
	Search     string
	Cursor     string
	Limit      string
}

// ParseLimit reads an inbox page size; empty is the default.
func ParseLimit(raw string) (int, *errx.Error) {
	if raw == "" {
		return DefaultInboxLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > MaxInboxLimit {
		return 0, errInvalidLimit
	}
	return n, nil
}

// Inbox is one page of conversations across the caller's clients, newest activity first.
func (s *Service) Inbox(ctx context.Context, agencyID, userID uuid.UUID, p InboxParams) (*models.AgencyInboxPage, *errx.Error) {
	limit, xerr := ParseLimit(p.Limit)
	if xerr != nil {
		return nil, xerr
	}
	at, id, xerr := decodeCursor(p.Cursor)
	if xerr != nil {
		return nil, xerr
	}
	list, xerr := s.memberships(ctx, agencyID, userID)
	if xerr != nil {
		return nil, xerr
	}
	var filter map[uuid.UUID]bool
	if len(p.Clients) > 0 {
		filter = map[uuid.UUID]bool{}
		for _, c := range p.Clients {
			filter[c] = true
		}
	}
	sc, xerr := s.resolveInbox(ctx, list, userID, filter)
	if xerr != nil {
		return nil, xerr
	}
	page := &models.AgencyInboxPage{Data: []models.AgencyInboxThread{}}
	rows, err := s.repo.InboxThreads(ctx, models.AgencyInboxQuery{
		OrgIDs: sc.whole, ScopedOrgIDs: sc.scoped, ScopedMailboxes: sc.mailboxes,
		Unread: p.Unread, NeedsReply: p.NeedsReply, Positive: p.Positive, Search: p.Search,
		BeforeAt: at, BeforeID: id, Limit: limit,
	})
	if err != nil {
		return nil, internal(err)
	}
	if len(rows) > limit {
		last := rows[limit-1]
		next := encodeCursor(last.InternalDate, last.ID)
		page.Pagination = models.CPagination{NextCursor: &next, HasMore: true}
		rows = rows[:limit]
	}
	page.Data = append(page.Data, rows...)
	return page, nil
}

// clientInbox resolves one client the caller reads mail in, and the mailboxes they may read there (nil for all).
func (s *Service) clientInbox(ctx context.Context, agencyID, userID, orgID uuid.UUID) ([]uuid.UUID, *errx.Error) {
	list, xerr := s.memberships(ctx, agencyID, userID)
	if xerr != nil {
		return nil, xerr
	}
	var found *models.AgencyClientMembership
	for i := range list {
		if list[i].OrganizationID == orgID {
			found = &list[i]
			break
		}
	}
	if found == nil {
		return nil, errClientNotFound
	}
	if !found.CanReadInbox() {
		return nil, errClientInbox
	}
	if s.gate != nil {
		ok, xerr := s.gate.CanUseUnibox(ctx, orgID)
		if xerr != nil {
			return nil, xerr
		}
		if !ok {
			return nil, errUniboxPlan
		}
	}
	if !found.Restricted() {
		return nil, nil
	}
	scope, xerr := s.scopes.ResolveMemberScope(ctx, orgID, userID)
	if xerr != nil {
		return nil, xerr
	}
	if scope == nil || len(scope.Mailboxes) == 0 {
		return nil, errClientInbox
	}
	return scope.Mailboxes, nil
}

func allowedMailbox(allowed []uuid.UUID, id uuid.UUID) bool {
	if allowed == nil {
		return true
	}
	for _, a := range allowed {
		if a == id {
			return true
		}
	}
	return false
}

// Thread reads one client conversation without changing anything in it.
func (s *Service) Thread(ctx context.Context, agencyID, userID, orgID uuid.UUID, threadID, cursor string) (*models.MailSearchResult, *errx.Error) {
	if threadID == "" {
		return nil, errx.New(errx.BadRequest, "thread_id is required")
	}
	allowed, xerr := s.clientInbox(ctx, agencyID, userID, orgID)
	if xerr != nil {
		return nil, xerr
	}
	res, xerr := s.unibox.GetByThreadWithin(ctx, orgID, uuid.Nil, threadID, "", cursor, allowed)
	if xerr != nil {
		return nil, xerr
	}
	if allowed != nil && res != nil {
		kept := res.Data[:0]
		for _, m := range res.Data {
			if !allowedMailbox(allowed, m.EmailID) {
				continue
			}
			if m.AnswersMailboxID != nil && !allowedMailbox(allowed, *m.AnswersMailboxID) {
				m.AnswersMailboxID = nil
			}
			kept = append(kept, m)
		}
		res.Data = kept
	}
	return res, nil
}

// Message reads one client message's body without marking it read.
func (s *Service) Message(ctx context.Context, agencyID, userID, orgID, messageID uuid.UUID) (*models.EmailMessage, *errx.Error) {
	allowed, xerr := s.clientInbox(ctx, agencyID, userID, orgID)
	if xerr != nil {
		return nil, xerr
	}
	msg, xerr := s.unibox.Peek(ctx, orgID, messageID)
	if xerr != nil {
		return nil, xerr
	}
	if msg == nil || !allowedMailbox(allowed, msg.EmailID) {
		return nil, errMessageMissing
	}
	return msg, nil
}

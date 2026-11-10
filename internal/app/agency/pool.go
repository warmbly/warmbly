package agency

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// poolTTL is how long a period count is reused; the pool is a soft cap like the daily one.
const poolTTL = 60 * time.Second

// lowPoolTTL replaces poolTTL once less than lowPoolPercent of the pool is left, so concurrent clients cannot overshoot it by much.
const (
	lowPoolTTL     = 5 * time.Second
	lowPoolPercent = 5
)

// orgTTL is how long a workspace's agency lookup is reused on the send path.
const orgTTL = 30 * time.Second

// poolEntry is one instant's read of the pool. today is each client's sends
// today as the sender counts them, read with the rest, so a daily cap built
// from it reaches the sender's fresh count after exactly what was left then.
type poolEntry struct {
	at     time.Time
	start  time.Time
	sends  map[uuid.UUID]int64
	today  map[uuid.UUID]int
	topups int64
}

type orgEntry struct {
	at     time.Time
	agency *models.Agency
	limit  *int
}

func (s *Service) forgetAgency(agencyID uuid.UUID) {
	s.mu.Lock()
	delete(s.pools, agencyID)
	s.mu.Unlock()
}

func (s *Service) forgetOrg(orgID uuid.UUID) {
	s.mu.Lock()
	delete(s.orgAgency, orgID)
	s.mu.Unlock()
}

// grantPlan is the plan an agency's clients hold now and when it ends (nil never), or false when they hold none.
func (s *Service) grantPlan(a *models.Agency) (uuid.UUID, *time.Time, bool) {
	if !s.billing {
		return uuid.Nil, nil, false
	}
	return models.AgencyEntitlement(a, s.now())
}

// IsolatedEgress converges a workspace's reserved worker on its plan (worker.WorkerAssignmentService).
type IsolatedEgress interface {
	SyncIsolatedEgress(ctx context.Context, orgID uuid.UUID) error
}

// WireIsolatedEgress lets grants reserve and release workers the way a Stripe plan change does.
func (s *Service) WireIsolatedEgress(e IsolatedEgress) { s.egress = e }

// syncEgress follows a client's grant with its reserved worker; a failure is retried by the next sweep.
func (s *Service) syncEgress(ctx context.Context, orgID uuid.UUID) {
	if s.egress == nil {
		return
	}
	if err := s.egress.SyncIsolatedEgress(ctx, orgID); err != nil {
		errs.CaptureException(fmt.Errorf("agency isolated egress %s: %w", orgID, err))
	}
}

// syncGrant converges one client's managed grant on the agency's subscription.
func (s *Service) syncGrant(ctx context.Context, a *models.Agency, orgID uuid.UUID) error {
	if planID, until, ok := s.grantPlan(a); ok {
		if err := s.repo.SyncClientGrant(ctx, a.ID, orgID, planID, until); err != nil {
			return err
		}
	} else if !s.billing {
		return nil
	} else if err := s.repo.ClearClientGrant(ctx, a.ID, orgID); err != nil {
		return err
	}
	// One reserved worker per client workspace, not per agency: placement treats each workspace as a tenant, and one worker for every client is one blast radius.
	s.syncEgress(ctx, orgID)
	return nil
}

// SyncAgency converges every client's grant and staff on the agency's current state.
func (s *Service) SyncAgency(ctx context.Context, agencyID uuid.UUID) error {
	a, err := s.repo.Get(ctx, agencyID)
	if err != nil || a == nil {
		return err
	}
	clients, err := s.repo.ListClientOrganizations(ctx, agencyID)
	if err != nil {
		return err
	}
	for _, c := range clients {
		if err := s.syncGrant(ctx, a, c.OrganizationID); err != nil {
			return err
		}
		if xerr := s.mirrorStaffInto(ctx, agencyID, c.OrganizationID, a.OwnerUserID); xerr != nil {
			return xerr
		}
		s.forgetOrg(c.OrganizationID)
	}
	s.forgetAgency(agencyID)
	stale, err := s.repo.ClearStaleGrants(ctx, agencyID)
	for _, orgID := range stale {
		s.syncEgress(ctx, orgID)
	}
	return err
}

// Sweep re-syncs every agency and raises pool alerts. Each step is
// idempotent, so a sweep cut short is finished by the next one.
func (s *Service) Sweep(ctx context.Context) error {
	after := uuid.Nil
	var firstErr error
	for {
		page, err := s.repo.ListPage(ctx, after, 100)
		if err != nil {
			return err
		}
		for i := range page {
			a := &page[i]
			after = a.ID
			if err := s.SyncAgency(ctx, a.ID); err != nil {
				errs.CaptureException(fmt.Errorf("agency sync %s: %w", a.ID, err))
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			// Rolled up here as well as on read, so the day just ended is recorded even when nobody looks.
			if err := s.rollup(ctx, a); err != nil {
				errs.CaptureException(fmt.Errorf("agency pool rollup %s: %w", a.ID, err))
			}
			s.checkAlerts(ctx, a)
		}
		if len(page) < 100 || ctx.Err() != nil {
			return firstErr
		}
	}
}

// poolSize is the agency's pool for its plan; unlimited on a self-host, on a
// plan without a pool, and nothing at all when the plan has lapsed.
func (s *Service) poolSize(ctx context.Context, a *models.Agency) (size int64, unlimited, active bool) {
	if !s.billing {
		return 0, true, true
	}
	planID, _, ok := s.grantPlan(a)
	if !ok {
		return 0, false, false
	}
	p, err := s.plans.GetByID(ctx, planID)
	if err != nil || p == nil || p.MonthlySendPool == nil {
		return 0, false, false
	}
	// An operator's pool for this agency stands in for the plan's.
	if a.PoolOverride != nil {
		return int64(*a.PoolOverride), false, true
	}
	if *p.MonthlySendPool == 0 {
		return 0, true, true
	}
	return int64(*p.MonthlySendPool), false, true
}

// rollup records the agency's sends for the current and previous pool day.
func (s *Service) rollup(ctx context.Context, a *models.Agency) error {
	now := s.now()
	start, _ := models.AgencyPeriod(a.CurrentPeriodStart, a.CurrentPeriodEnd, now)
	return s.repo.RollupPoolUsage(ctx, a.ID, models.NewAgencyPoolDays(start).Recent(now))
}

// period reads the agency's rolled-up sends, its clients' sends today and its
// top-ups in its current period, rolling up first, and reuses the read for up
// to maxAge within one day.
func (s *Service) period(ctx context.Context, a *models.Agency, maxAge time.Duration) (time.Time, time.Time, poolEntry, error) {
	now := s.now()
	start, end := models.AgencyPeriod(a.CurrentPeriodStart, a.CurrentPeriodEnd, now)
	s.mu.Lock()
	e, ok := s.pools[a.ID]
	s.mu.Unlock()
	if ok && e.start.Equal(start) && now.Sub(e.at) < maxAge && sameUTCDay(now, e.at) {
		return start, end, e, nil
	}
	days := models.NewAgencyPoolDays(start)
	if err := s.repo.RollupPoolUsage(ctx, a.ID, days.Recent(now)); err != nil {
		return start, end, poolEntry{}, err
	}
	e = poolEntry{at: now, start: start}
	var err error
	if e.sends, err = s.repo.PoolUsage(ctx, a.ID, days.At(start).Day, days.At(end.Add(-time.Nanosecond)).Day.AddDate(0, 0, 1)); err != nil {
		return start, end, poolEntry{}, err
	}
	if e.today, err = s.repo.SentTodayByClient(ctx, a.ID); err != nil {
		return start, end, poolEntry{}, err
	}
	if e.topups, err = s.repo.TopupSends(ctx, a.ID, start); err != nil {
		return start, end, poolEntry{}, err
	}
	s.mu.Lock()
	s.pools[a.ID] = e
	s.mu.Unlock()
	return start, end, e, nil
}

func sum(m map[uuid.UUID]int64) int64 {
	var n int64
	for _, v := range m {
		n += v
	}
	return n
}

// clientOf resolves a workspace's agency and its monthly cap, briefly cached.
func (s *Service) clientOf(ctx context.Context, orgID uuid.UUID) (*models.Agency, *int, error) {
	now := s.now()
	s.mu.Lock()
	e, ok := s.orgAgency[orgID]
	s.mu.Unlock()
	if ok && now.Sub(e.at) < orgTTL {
		return e.agency, e.limit, nil
	}
	a, err := s.repo.GetAgencyForOrganization(ctx, orgID)
	if err != nil {
		return nil, nil, err
	}
	var limit *int
	if a != nil {
		c, err := s.repo.GetClient(ctx, orgID)
		if err != nil {
			return nil, nil, err
		}
		if c != nil {
			limit = c.MonthlySendLimit
		}
	}
	s.mu.Lock()
	s.orgAgency[orgID] = orgEntry{at: now, agency: a, limit: limit}
	s.mu.Unlock()
	return a, limit, nil
}

// ClientPool is the pool as one client workspace sees it; false when the
// workspace is not a client of an agency whose pool is in force.
func (s *Service) ClientPool(ctx context.Context, orgID uuid.UUID) (models.AgencyPool, bool, error) {
	pool, _, ok, err := s.clientPool(ctx, orgID, poolTTL)
	return pool, ok, err
}

// clientPool also returns the client's sends today, read with the pool.
func (s *Service) clientPool(ctx context.Context, orgID uuid.UUID, maxAge time.Duration) (models.AgencyPool, int, bool, error) {
	a, limit, err := s.clientOf(ctx, orgID)
	if err != nil || a == nil {
		return models.AgencyPool{}, 0, false, err
	}
	size, unlimited, active := s.poolSize(ctx, a)
	if !active {
		return models.AgencyPool{}, 0, false, nil
	}
	pool := models.AgencyPool{Unlimited: unlimited, Size: size}
	if limit != nil {
		l := int64(*limit)
		pool.ClientLimit = &l
	}
	if unlimited && limit == nil {
		return pool, 0, true, nil
	}
	_, _, e, err := s.period(ctx, a, maxAge)
	if err != nil {
		return models.AgencyPool{}, 0, false, err
	}
	pool.Topups, pool.Used, pool.ClientUsed = e.topups, sum(e.sends), e.sends[orgID]
	return pool, e.today[orgID], true, nil
}

// DailyLimit is the campaign cap for an agency client: what it sent today
// plus what the pool and its own cap leave, both from one read, or -1 when
// unlimited. False hands the decision back to the workspace's own plan.
func (s *Service) DailyLimit(ctx context.Context, orgID uuid.UUID) (int, bool) {
	pool, sent, ok, err := s.clientPool(ctx, orgID, poolTTL)
	if err == nil && ok && pool.Low(lowPoolPercent) {
		// Other clients draw on the same pool, so near its end it is read again sooner.
		pool, sent, ok, err = s.clientPool(ctx, orgID, lowPoolTTL)
	}
	if err != nil {
		errs.CaptureException(err)
		return 0, false
	}
	if !ok {
		return 0, false
	}
	if pool.Unlimited && pool.ClientLimit == nil {
		return -1, true
	}
	return pool.DailyLimit(sent), true
}

func sameUTCDay(a, b time.Time) bool {
	ay, am, ad := a.UTC().Date()
	by, bm, bd := b.UTC().Date()
	return ay == by && am == bm && ad == bd
}

// Usage is the pool meter: size, top-ups, use per client and a projection.
func (s *Service) Usage(ctx context.Context, agencyID, userID uuid.UUID) (*models.AgencyUsage, *errx.Error) {
	a, _, xerr := s.access(ctx, agencyID, userID, false)
	if xerr != nil {
		return nil, xerr
	}
	return s.usage(ctx, a)
}

func (s *Service) usage(ctx context.Context, a *models.Agency) (*models.AgencyUsage, *errx.Error) {
	start, end, e, err := s.period(ctx, a, poolTTL)
	if err != nil {
		return nil, internal(err)
	}
	sends, topups := e.sends, e.topups
	size, unlimited, _ := s.poolSize(ctx, a)
	now := s.now()
	u := &models.AgencyUsage{
		Unlimited: unlimited, PeriodStart: start, PeriodEnd: end,
		DaysLeft: int(math.Ceil(end.Sub(now).Hours() / 24)),
		PoolSize: size, Topups: topups, Total: size + topups, Used: sum(sends),
		TopupSends: models.AgencyTopupSends, TopupCents: models.AgencyTopupPriceCents(a.EffectivePlanID(now)),
		Clients: []models.AgencyClientUsage{},
	}
	u.DaysLeft = max(u.DaysLeft, 0)
	u.Remaining = max(0, u.Total-u.Used)
	u.Projected = models.ProjectPoolUse(u.Used, start, end, now)
	clients, err := s.repo.ListClientOrganizations(ctx, a.ID)
	if err != nil {
		return nil, internal(err)
	}
	for _, c := range clients {
		u.Clients = append(u.Clients, models.AgencyClientUsage{
			OrganizationID: c.OrganizationID, Name: c.Name, Used: sends[c.OrganizationID], MonthlySendLimit: c.MonthlySendLimit,
		})
	}
	return u, nil
}

// PoolTotals is the period's use and pool for the admin list.
func (s *Service) PoolTotals(ctx context.Context, a *models.Agency) (used, total int64, unlimited bool) {
	u, xerr := s.usage(ctx, a)
	if xerr != nil {
		return 0, 0, false
	}
	return u.Used, u.Total, u.Unlimited
}

// checkAlerts tells owners and admins once per period when the pool passes 80% and 100%.
func (s *Service) checkAlerts(ctx context.Context, a *models.Agency) {
	if s.notifier == nil {
		return
	}
	if _, unlimited, active := s.poolSize(ctx, a); unlimited || !active {
		return
	}
	u, xerr := s.usage(ctx, a)
	if xerr != nil {
		return
	}
	level := models.PoolAlertLevel(u.Used, u.Total)
	if level == 0 {
		return
	}
	claimed, err := s.repo.ClaimPoolAlert(ctx, a.ID, u.PeriodStart, level)
	if err != nil {
		errs.CaptureException(err)
		return
	}
	if !claimed || len(u.Clients) == 0 {
		return
	}
	staff, err := s.repo.ListMembers(ctx, a.ID)
	if err != nil {
		errs.CaptureException(err)
		return
	}
	// Notifications are addressed within a workspace; staff are members of every client.
	orgID := u.Clients[0].OrganizationID
	title := fmt.Sprintf("Your agency has used %d%% of its monthly sends", level)
	body := fmt.Sprintf("%d of %d sends used this period. Add a top-up pack to keep campaigns sending until the period renews.", u.Used, u.Total)
	if level == 100 {
		title = "Your agency's monthly sends are used up"
		body = "Campaigns across your clients pause until the period renews or you add a top-up pack. Warmup keeps running."
	}
	for _, m := range staff {
		if !m.Role.CanManage() {
			continue
		}
		s.notifier.Notify(ctx, m.UserID, &orgID, models.NotifBillingAlert, title, body, "/app/agency/settings/usage",
			map[string]any{"agency_id": a.ID.String(), "level": level})
	}
}

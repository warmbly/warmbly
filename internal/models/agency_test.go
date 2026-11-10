package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func i64(v int64) *int64 { return &v }

func TestAgencyPoolAllowance(t *testing.T) {
	cases := []struct {
		name      string
		pool      AgencyPool
		remaining int64
		allowance int64
	}{
		{"fresh pool", AgencyPool{Size: 150_000}, 150_000, 150_000},
		{"top-ups add to the pool", AgencyPool{Size: 150_000, Topups: 50_000, Used: 160_000}, 40_000, 40_000},
		{"spent pool never goes negative", AgencyPool{Size: 100, Used: 250}, 0, 0},
		{"client cap below the pool", AgencyPool{Size: 150_000, Used: 1_000, ClientUsed: 900, ClientLimit: i64(1_000)}, 149_000, 100},
		{"client over its cap", AgencyPool{Size: 150_000, Used: 2_000, ClientUsed: 1_500, ClientLimit: i64(1_000)}, 148_000, 0},
		{"pool below the client cap", AgencyPool{Size: 1_000, Used: 990, ClientUsed: 10, ClientLimit: i64(5_000)}, 10, 10},
		{"zero cap pauses the client", AgencyPool{Size: 1_000, ClientLimit: i64(0)}, 1_000, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.pool.Remaining(); got != tc.remaining {
				t.Fatalf("Remaining() = %d, want %d", got, tc.remaining)
			}
			if got := tc.pool.Allowance(); got != tc.allowance {
				t.Fatalf("Allowance() = %d, want %d", got, tc.allowance)
			}
		})
	}
}

func TestAgencyPoolDailyLimit(t *testing.T) {
	if got := (AgencyPool{Unlimited: true}).DailyLimit(40); got != -1 {
		t.Fatalf("unlimited pool: got %d, want -1", got)
	}
	if got := (AgencyPool{Unlimited: true, ClientLimit: i64(100), ClientUsed: 70}).DailyLimit(5); got != 35 {
		t.Fatalf("unlimited pool with a client cap: got %d, want 35", got)
	}
	// Today's sends are already inside the period count, so the cap is today plus what is left.
	if got := (AgencyPool{Size: 1_000, Used: 600, ClientUsed: 600}).DailyLimit(120); got != 520 {
		t.Fatalf("got %d, want 520", got)
	}
	if got := (AgencyPool{Size: 1_000, Used: 1_000}).DailyLimit(30); got != 30 {
		t.Fatalf("spent pool must hold the client at what it sent today: got %d", got)
	}
}

func TestAgencyPeriod(t *testing.T) {
	start := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)

	s, e := AgencyPeriod(&start, &end, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if !s.Equal(start) || !e.Equal(end) {
		t.Fatalf("inside the billing period: got %s..%s", s, e)
	}
	// A renewal webhook that has not arrived yet must not leave the pool on the old period.
	s, e = AgencyPeriod(&start, &end, time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC))
	if !s.Equal(end) || !e.Equal(end.AddDate(0, 1, 0)) {
		t.Fatalf("late renewal: got %s..%s", s, e)
	}
	// The boundary instant belongs to the next period.
	s, _ = AgencyPeriod(&start, &end, end)
	if !s.Equal(end) {
		t.Fatalf("boundary: got start %s, want %s", s, end)
	}
	// A yearly billing period is split into monthly pools from its anchor.
	ys := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	ye := time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC)
	s, e = AgencyPeriod(&ys, &ye, time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC))
	if !s.Equal(time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)) || !e.Equal(time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("yearly plan: got %s..%s", s, e)
	}
	s, e = AgencyPeriod(&ys, &ye, time.Date(2027, 1, 30, 0, 0, 0, 0, time.UTC))
	if !s.Equal(time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)) || !e.Equal(ye) {
		t.Fatalf("yearly plan, last month: got %s..%s", s, e)
	}
	// No billing period (self-host): the calendar month.
	s, e = AgencyPeriod(nil, nil, time.Date(2026, 11, 8, 13, 0, 0, 0, time.UTC))
	if !s.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) || !e.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("calendar month: got %s..%s", s, e)
	}
}

func TestPoolAlertLevel(t *testing.T) {
	cases := []struct {
		used, total int64
		want        int
	}{
		{0, 1000, 0}, {799, 1000, 0}, {800, 1000, 80}, {999, 1000, 80}, {1000, 1000, 100}, {1500, 1000, 100}, {5, 0, 0},
	}
	for _, tc := range cases {
		if got := PoolAlertLevel(tc.used, tc.total); got != tc.want {
			t.Fatalf("PoolAlertLevel(%d, %d) = %d, want %d", tc.used, tc.total, got, tc.want)
		}
	}
}

func TestProjectPoolUse(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 30)
	if got := ProjectPoolUse(10_000, start, end, start.AddDate(0, 0, 10)); got != 30_000 {
		t.Fatalf("got %d, want 30000", got)
	}
	// An hour in, the pace is read over a full day.
	if got := ProjectPoolUse(100, start, end, start.Add(time.Hour)); got != 3_000 {
		t.Fatalf("got %d, want 3000", got)
	}
	if got := ProjectPoolUse(500, start, end, end.Add(time.Hour)); got != 500 {
		t.Fatalf("after the period: got %d, want 500", got)
	}
}

func TestAgencyEntitlement(t *testing.T) {
	end := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	now := end.Add(-24 * time.Hour)
	plan := AgencyPlanID
	base := Agency{PlanID: &plan, CurrentPeriodEnd: &end}
	for _, st := range []SubscriptionStatus{SubscriptionStatusActive, SubscriptionStatusTrialing, SubscriptionStatusPastDue} {
		a := base
		a.Status = st
		got, until, ok := AgencyEntitlement(&a, now)
		if !ok || got != plan || until == nil || !until.Equal(end.Add(AgencyGrantGrace)) {
			t.Fatalf("%s: got %s, %v, %v", st, got, until, ok)
		}
		if _, _, ok := AgencyEntitlement(&a, end.Add(AgencyGrantGrace)); ok {
			t.Fatalf("%s must lapse once the grace is over", st)
		}
	}
	for _, st := range []SubscriptionStatus{SubscriptionStatusCanceled, SubscriptionStatusUnpaid, SubscriptionStatusIncomplete, SubscriptionStatusIncompleteExpired, SubscriptionStatusPaused} {
		a := base
		a.Status = st
		if _, _, ok := AgencyEntitlement(&a, now); ok {
			t.Fatalf("%s must not grant", st)
		}
	}
	noPlan := base
	noPlan.Status = SubscriptionStatusActive
	noPlan.PlanID = nil
	if _, _, ok := AgencyEntitlement(&noPlan, now); ok {
		t.Fatal("an agency without a plan must not grant")
	}
}

func TestAgencyEntitlementOperatorPlan(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	at := now.Add(-time.Hour)
	stripe, granted := AgencyPlanID, AgencyProPlanID
	end := now.Add(10 * 24 * time.Hour)
	// No subscription at all: the operator's plan alone entitles, with no end.
	a := Agency{Status: SubscriptionStatusIncomplete, ManagedPlanID: &granted, ManagedAt: &at}
	got, until, ok := AgencyEntitlement(&a, now)
	if !ok || got != granted || until != nil {
		t.Fatalf("open-ended grant: got %s, %v, %v", got, until, ok)
	}
	// It stands in for a paid subscription while in force.
	a = Agency{PlanID: &stripe, Status: SubscriptionStatusActive, CurrentPeriodEnd: &end, ManagedPlanID: &granted, ManagedAt: &at, ManagedUntil: &end}
	if got, until, ok := AgencyEntitlement(&a, now); !ok || got != granted || until == nil || !until.Equal(end) {
		t.Fatalf("dated grant: got %s, %v, %v", got, until, ok)
	}
	if id := a.EffectivePlanID(now); id == nil || *id != granted {
		t.Fatalf("effective plan: got %v", id)
	}
	// Past its end the subscription is what counts again, with its grace.
	later := end.Add(time.Hour)
	if got, _, ok := AgencyEntitlement(&a, later); !ok || got != stripe {
		t.Fatalf("lapsed grant: got %s, %v", got, ok)
	}
	if id := a.EffectivePlanID(later); id == nil || *id != stripe {
		t.Fatalf("effective plan after the grant: got %v", id)
	}
	// A grant whose plan was deleted entitles to nothing.
	a = Agency{Status: SubscriptionStatusIncomplete, ManagedAt: &at}
	if _, _, ok := AgencyEntitlement(&a, now); ok {
		t.Fatal("a grant without a plan must not entitle")
	}
}

func TestAgencyAIPeriodOfOverride(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	at := now.Add(-time.Hour)
	plan := AgencyProPlanID
	planAllowance, own := 30*MicroPerUSD, 250*MicroPerUSD
	a := Agency{Status: SubscriptionStatusIncomplete, ManagedPlanID: &plan, ManagedAt: &at}
	if p := AgencyAIPeriodOf(&a, &planAllowance, true, now); p.Allowance != planAllowance {
		t.Fatalf("plan allowance: got %d", p.Allowance)
	}
	a.AIAllowanceOverride = &own
	if p := AgencyAIPeriodOf(&a, &planAllowance, true, now); p.Allowance != own {
		t.Fatalf("override: got %d", p.Allowance)
	}
	// Without a plan in force an override refills nothing.
	a.ManagedAt = nil
	if p := AgencyAIPeriodOf(&a, &planAllowance, true, now); p.Allowance != 0 {
		t.Fatalf("no plan: got %d", p.Allowance)
	}
}

func TestAgencyRoleMapping(t *testing.T) {
	cases := map[AgencyRole]struct {
		role Role
		name string
	}{
		AgencyRoleOwner:  {RoleAdmin, "Admin"},
		AgencyRoleAdmin:  {RoleAdmin, "Admin"},
		AgencyRoleMember: {RoleManager, "Manager"},
	}
	seeded := map[string]bool{}
	for _, r := range DefaultSeedRoles() {
		seeded[r.Name] = true
	}
	for role, want := range cases {
		if got := role.WorkspaceRole(); got != want.role {
			t.Fatalf("%s: WorkspaceRole() = %s, want %s", role, got, want.role)
		}
		if got := role.WorkspaceRoleName(); got != want.name || !seeded[got] {
			t.Fatalf("%s: WorkspaceRoleName() = %q, want the seeded %q", role, got, want.name)
		}
	}
}

func TestAgencyTopupPrice(t *testing.T) {
	agency, pro := AgencyPlanID, AgencyProPlanID
	if got := AgencyTopupPriceCents(&agency); got != 8_500 {
		t.Fatalf("Agency pack: got %d", got)
	}
	if got := AgencyTopupPriceCents(&pro); got != 6_000 {
		t.Fatalf("Agency Pro pack: got %d", got)
	}
	other := uuid.New()
	if got := AgencyTopupPriceCents(&other); got != 6_000 {
		t.Fatalf("custom plan pack: got %d", got)
	}
}

func TestAgencyPoolDaysAlignWithThePeriod(t *testing.T) {
	start := time.Date(2026, 10, 20, 13, 30, 0, 0, time.UTC)
	days := NewAgencyPoolDays(start)
	if d := days.At(start); !d.Start.Equal(start) || !d.Day.Equal(time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("the period's first instant opens its first pool day, got %+v", d)
	}
	if d := days.At(start.Add(-time.Second)); !d.Start.Equal(start.AddDate(0, 0, -1)) {
		t.Fatalf("a second before the period belongs to the previous pool day, got %+v", d)
	}
	recent := days.Recent(time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC))
	if len(recent) != 2 || !recent[1].Start.Equal(time.Date(2026, 11, 7, 13, 30, 0, 0, time.UTC)) ||
		!recent[0].Start.Equal(time.Date(2026, 11, 6, 13, 30, 0, 0, time.UTC)) {
		t.Fatalf("recent: got %+v", recent)
	}
	// A calendar-month period (no billing) uses UTC days.
	cal := NewAgencyPoolDays(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if d := cal.At(time.Date(2026, 11, 8, 23, 59, 0, 0, time.UTC)); !d.Day.Equal(time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("calendar day: got %+v", d)
	}
}

func TestAgencyPoolLow(t *testing.T) {
	if !(AgencyPool{Size: 1_000, Used: 960}).Low(5) {
		t.Fatal("4% left is low")
	}
	if (AgencyPool{Size: 1_000, Used: 940}).Low(5) || (AgencyPool{Unlimited: true}).Low(5) {
		t.Fatal("6% left, or no cap, is not low")
	}
	if !(AgencyPool{Size: 1_000, Topups: 1_000, Used: 1_950}).Low(5) {
		t.Fatal("top-ups count toward the pool the share is taken of")
	}
}

func TestAgencyAIPeriodRefillsOnlyWhileThePlanIsBilled(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	start, end := now.AddDate(0, 0, -5), now.AddDate(0, 0, 25)
	plan := AgencyPlanID
	a := &Agency{PlanID: &plan, Status: SubscriptionStatusActive, CurrentPeriodStart: &start, CurrentPeriodEnd: &end}
	allowance := 10 * MicroPerUSD
	if p := AgencyAIPeriodOf(a, &allowance, true, now); !p.Start.Equal(start) || p.Allowance != allowance {
		t.Fatalf("billed active plan: %+v", p)
	}
	if p := AgencyAIPeriodOf(a, &allowance, false, now); p.Allowance != 0 {
		t.Fatalf("unbilled instance refills nothing: %+v", p)
	}
	a.Status = SubscriptionStatusCanceled
	if p := AgencyAIPeriodOf(a, &allowance, true, now); p.Allowance != 0 {
		t.Fatalf("lapsed plan refills nothing: %+v", p)
	}

	// The stored allowance belongs to its period; a newer period refills it, an older one does not.
	stored := AgencyAILedger{Allowance: 2 * MicroPerUSD, Purchased: MicroPerUSD, PeriodStart: &start}
	next := AgencyAIPeriod{Start: end, Allowance: allowance}
	if got := stored.In(next); got.Allowance != allowance || !got.PeriodStart.Equal(end) || got.Purchased != MicroPerUSD {
		t.Fatalf("new period: %+v", got)
	}
	stale := AgencyAIPeriod{Start: start.AddDate(0, -1, 0), Allowance: allowance}
	if got := stored.In(stale); got.Allowance != 2*MicroPerUSD || !got.PeriodStart.Equal(start) {
		t.Fatalf("stale period: %+v", got)
	}
}

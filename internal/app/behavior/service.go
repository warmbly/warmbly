package behavior

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// ErrNotFound is returned when the mailbox behind a profile no longer exists.
var ErrNotFound = errors.New("mailbox not found")

// unlimited is the answer to a capacity question the behaviour engine does not
// govern (profile disabled). It is deliberately a number rather than a bool so
// callers can min() it into their own budgets without a special case.
const unlimited = math.MaxInt

// Service owns sending-behaviour profiles and the daily plans rolled from
// them. The schedulers consume it through Resolve; the API through Get/Update/
// Today.
type Service interface {
	// Get returns the mailbox's profile, substituting the defaults when it has
	// never been configured, with the mailbox timezone filled in.
	Get(ctx context.Context, accountID uuid.UUID) (models.SendingBehavior, error)
	// Update validates and persists a patch, returning the stored profile.
	Update(ctx context.Context, accountID uuid.UUID, patch models.UpdateSendingBehavior) (models.SendingBehavior, error)
	// Today returns the mailbox's plan for the current local date along with
	// how much of it is already spent. Rolls and stores the plan if needed.
	Today(ctx context.Context, accountID uuid.UUID) (*models.DailyPlanView, error)

	// Resolve is the scheduler entry point: one call per mailbox per
	// scheduling pass, returning everything needed to place the next send.
	Resolve(ctx context.Context, account *models.Email) Resolved
	// ResolveMany is Resolve over a campaign's whole sender pool in one
	// profile read, so adding mailboxes to a campaign does not add a query per
	// mailbox to every scheduling pass.
	ResolveMany(ctx context.Context, accounts []models.Email) map[uuid.UUID]Resolved
	// RemainingToday is the mailbox's unspent share of the day's rolled cold
	// budget, counted over its own local day.
	RemainingToday(ctx context.Context, r Resolved, at time.Time) int
	// RemainingThisHour is the same for the local clock hour containing `at`.
	RemainingThisHour(ctx context.Context, r Resolved, at time.Time) int
}

type service struct {
	behaviorRepo repository.BehaviorRepository
	emailRepo    repository.EmailRepository
}

func NewService(behaviorRepo repository.BehaviorRepository, emailRepo repository.EmailRepository) Service {
	return &service{behaviorRepo: behaviorRepo, emailRepo: emailRepo}
}

// loadLocation resolves a mailbox timezone, falling back to UTC. A bad or empty
// timezone must never stop a mailbox sending.
func loadLocation(tz string) *time.Location {
	if tz == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

func (s *service) profileFor(ctx context.Context, accountID uuid.UUID) (models.SendingBehavior, error) {
	stored, err := s.behaviorRepo.GetBehavior(ctx, accountID)
	if err != nil {
		return models.SendingBehavior{}, err
	}
	if stored == nil {
		return models.DefaultSendingBehavior(accountID), nil
	}
	return *stored, nil
}

func (s *service) Get(ctx context.Context, accountID uuid.UUID) (models.SendingBehavior, error) {
	account, aerr := s.emailRepo.GetByID(ctx, accountID)
	if aerr != nil {
		return models.SendingBehavior{}, aerr
	}
	if account == nil {
		return models.SendingBehavior{}, ErrNotFound
	}
	b, err := s.profileFor(ctx, accountID)
	if err != nil {
		return models.SendingBehavior{}, err
	}
	b.Timezone = account.Timezone
	return b, nil
}

func (s *service) Update(ctx context.Context, accountID uuid.UUID, patch models.UpdateSendingBehavior) (models.SendingBehavior, error) {
	current, err := s.Get(ctx, accountID)
	if err != nil {
		return models.SendingBehavior{}, err
	}

	next := patch.Apply(current)
	next.EmailAccountID = accountID
	if verr := next.Validate(); verr != nil {
		return models.SendingBehavior{}, verr
	}

	saved, err := s.behaviorRepo.UpsertBehavior(ctx, &next)
	if err != nil {
		return models.SendingBehavior{}, err
	}
	saved.Timezone = current.Timezone
	return *saved, nil
}

func (s *service) Today(ctx context.Context, accountID uuid.UUID) (*models.DailyPlanView, error) {
	account, aerr := s.emailRepo.GetByID(ctx, accountID)
	if aerr != nil {
		return nil, aerr
	}
	if account == nil {
		return nil, ErrNotFound
	}

	r := s.Resolve(ctx, account)
	now := time.Now()
	plan := r.PlanOn(PlanDateFor(now, r.Loc))

	from, to := s.localDayRange(now, r.Loc)
	sent := s.sentInRange(ctx, accountID, from, to)
	remaining := plan.DailyLimit - sent
	if remaining < 0 {
		remaining = 0
	}

	return &models.DailyPlanView{
		DailyPlan:      plan,
		SentToday:      sent,
		RemainingToday: remaining,
		Behavior:       r.Behavior,
	}, nil
}

// Resolved is one mailbox's behaviour, already paired with its timezone and a
// plan lookup. It is created once per scheduling pass and answers every
// behaviour question the schedulers ask, so a pass costs at most one profile
// read and one plan read.
type Resolved struct {
	// Enabled is false when the mailbox has no profile or has it switched off.
	// Callers must skip every behaviour gate in that case and keep the legacy
	// fixed cap / min-gap path, so opting out is a true no-op.
	Enabled  bool
	Behavior models.SendingBehavior
	Loc      *time.Location

	accountID uuid.UUID
	svc       *service
	ctx       context.Context
	cache     map[string]models.DailyPlan
}

// PlanOn returns the plan for a local day. Today's plan is read from (and
// written to) the database so the day is stable and inspectable; future days
// are rolled on the fly, since persisting a plan for a day the mailbox has not
// reached yet would only freeze settings the customer may still change.
func (r Resolved) PlanOn(day time.Time) models.DailyPlan {
	key := day.Format("2006-01-02")
	if r.cache == nil {
		// A Resolved built without the constructor (NewStandalone, or a zero
		// value someone switched on) has no cache. Roll and return rather than
		// writing to a nil map.
		return RollPlan(r.Behavior, day)
	}
	if cached, ok := r.cache[key]; ok {
		return cached
	}

	rolled := RollPlan(r.Behavior, day)
	plan := rolled

	// Only a mailbox that has actually opted in gets a stored plan. Persisting
	// for a disabled profile would both write a row per mailbox that never uses
	// the feature, and freeze a plan rolled from the pre-opt-in ranges for the
	// rest of the day someone switches it on.
	if r.Enabled && r.svc != nil && key == PlanDateFor(time.Now(), r.Loc).Format("2006-01-02") {
		if stored, err := r.svc.behaviorRepo.EnsurePlan(r.ctx, rolled); err == nil && stored != nil {
			plan = *stored
		}
	}

	r.cache[key] = plan
	return plan
}

// NewStandalone builds a Resolved with no database behind it: every day is
// rolled from the profile on demand and nothing is persisted. Useful for
// previewing a profile the customer has not saved yet, and for testing the
// schedulers' placement logic without a Postgres round trip.
func NewStandalone(b models.SendingBehavior, loc *time.Location) Resolved {
	if loc == nil {
		loc = time.UTC
	}
	return Resolved{Enabled: b.Enabled, Behavior: b, Loc: loc, accountID: b.EmailAccountID}
}

// NextOpen is Resolved's binding of the package-level window search.
func (r Resolved) NextOpen(from time.Time) (time.Time, models.DailyPlan, bool) {
	return NextOpen(from, r.Loc, r.PlanOn)
}

func (s *service) Resolve(ctx context.Context, account *models.Email) Resolved {
	r := Resolved{
		Loc:       time.UTC,
		accountID: account.ID,
		svc:       s,
		ctx:       ctx,
		cache:     map[string]models.DailyPlan{},
	}

	b, err := s.profileFor(ctx, account.ID)
	if err != nil {
		// Fail open: a behaviour lookup that errors must not stop the mailbox
		// sending, it just sends on the legacy fixed schedule this pass.
		return r
	}

	b.Timezone = account.Timezone
	r.Behavior = b
	r.Loc = loadLocation(account.Timezone)
	r.Enabled = b.Enabled
	return r
}

func (s *service) ResolveMany(ctx context.Context, accounts []models.Email) map[uuid.UUID]Resolved {
	out := make(map[uuid.UUID]Resolved, len(accounts))
	if len(accounts) == 0 {
		return out
	}

	ids := make([]uuid.UUID, 0, len(accounts))
	for _, a := range accounts {
		ids = append(ids, a.ID)
	}
	// Fail open on the batch read, exactly as Resolve does: every mailbox
	// falls back to a disabled profile and the legacy path this pass.
	stored, err := s.behaviorRepo.GetBehaviors(ctx, ids)
	if err != nil {
		stored = map[uuid.UUID]models.SendingBehavior{}
	}

	for _, a := range accounts {
		b, ok := stored[a.ID]
		if !ok {
			b = models.DefaultSendingBehavior(a.ID)
		}
		b.Timezone = a.Timezone
		out[a.ID] = Resolved{
			Enabled:   b.Enabled,
			Behavior:  b,
			Loc:       loadLocation(a.Timezone),
			accountID: a.ID,
			svc:       s,
			ctx:       ctx,
			cache:     map[string]models.DailyPlan{},
		}
	}
	return out
}

// localDayRange is the mailbox-local calendar day containing t, as a half-open
// UTC range for counting.
func (s *service) localDayRange(t time.Time, loc *time.Location) (time.Time, time.Time) {
	start := PlanDateFor(t, loc)
	return start, start.AddDate(0, 0, 1)
}

func (s *service) sentInRange(ctx context.Context, accountID uuid.UUID, from, to time.Time) int {
	n, err := s.behaviorRepo.CountSendsBetween(ctx, accountID, "campaign", from, to)
	if err != nil {
		// Fail closed on the count: an unknown spend is treated as the budget
		// being fully consumed, which delays a send rather than over-sending
		// from a mailbox whose real usage we could not read.
		return unlimited
	}
	return n
}

func (s *service) RemainingToday(ctx context.Context, r Resolved, at time.Time) int {
	if !r.Enabled {
		return unlimited
	}
	plan := r.PlanOn(PlanDateFor(at, r.Loc))
	if !plan.IsWorkingDay {
		return 0
	}
	from, to := s.localDayRange(at, r.Loc)
	remaining := plan.DailyLimit - s.sentInRange(ctx, r.accountID, from, to)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (s *service) RemainingThisHour(ctx context.Context, r Resolved, at time.Time) int {
	if !r.Enabled {
		return unlimited
	}
	plan := r.PlanOn(PlanDateFor(at, r.Loc))
	if !plan.IsWorkingDay {
		return 0
	}
	from, to := HourWindow(at, r.Loc)
	remaining := plan.HourlyLimit - s.sentInRange(ctx, r.accountID, from, to)
	if remaining < 0 {
		return 0
	}
	return remaining
}

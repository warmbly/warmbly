// Package credits implements the workspace balance: reads, the check before
// metered work starts, the charge for what it actually cost, grants, and the
// spend controls. Billing correctness (no negative balances, no double charge
// on retry) lives in the repository's atomic SQL; this layer adds pricing, the
// abuse caps (a short rolling window plus a daily window) and the configured
// spend limits on top.
package credits

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// Rolling-window cap: max AI generations per org in a 5-hour window.
	// Catches burst abuse independent of the balance.
	WindowShort       = 5 * time.Hour
	DefaultShortLimit = 60

	// Daily cap: max AI generations per org per UTC day.
	WindowDaily       = 24 * time.Hour
	DefaultDailyLimit = 300

	keyPrefixShort = "credits:cap:5h:"
	keyPrefixDaily = "credits:cap:day:"
)

// ErrInsufficientCredits signals the balance cannot pay for the work. The
// handler maps it to HTTP 402.
var ErrInsufficientCredits = errors.New("insufficient balance")

// AIRefusal is a 402 the workspace's own choice about AI spending made, not
// an empty balance. It wraps ErrInsufficientCredits, so every 402 mapping holds.
type AIRefusal struct {
	Code    string
	Message string
}

func (e *AIRefusal) Error() string { return e.Message }
func (e *AIRefusal) Unwrap() error { return ErrInsufficientCredits }

// ErrAIAllowanceUsed refuses AI once the plan's included usage is spent and the workspace keeps its balance for other things.
var ErrAIAllowanceUsed = &AIRefusal{Code: "ai_allowance_used",
	Message: "This month's included AI usage is used up. To keep using AI, let it use your balance under Settings > Billing > Balance."}

// ErrAIOverageLimitReached refuses AI once it took the workspace's monthly limit from the balance.
var ErrAIOverageLimitReached = &AIRefusal{Code: "ai_overage_limit_reached",
	Message: "AI reached this month's limit on using your balance. Raise or remove the limit under Settings > Billing > Balance."}

// PaymentRefusal is the code and message for a refused balance check: the
// workspace's AI setting when that is what refused, else insufficient_credits.
func PaymentRefusal(err error) (code, message string) {
	var r *AIRefusal
	if errors.As(err, &r) {
		return r.Code, r.Message
	}
	return "insufficient_credits", "The workspace balance is too low. Add funds to keep using AI features."
}

// ErrCapExceeded signals the org hit a rolling or daily generation cap. The
// handler maps it to HTTP 429.
var ErrCapExceeded = errors.New("generation rate cap exceeded")

// ErrSpendLimitReached signals the org hit its own day, week or month spend
// limit. It wraps ErrCapExceeded so every errors.Is mapping (→429) holds.
var ErrSpendLimitReached = fmt.Errorf("%w: configured spend limit reached", ErrCapExceeded)

// ErrMemberLimitReached signals the acting member hit the org's per-member
// limit. Wraps ErrCapExceeded for the same 429 mapping.
var ErrMemberLimitReached = fmt.Errorf("%w: your spend limit for this workspace is used up", ErrCapExceeded)

// PriceSource is the instance price list. *instancesettings.service satisfies it.
type PriceSource interface {
	UsagePrices(ctx context.Context) instancesettings.Usage
}

// UsageOverview is the usage dashboard payload: spend per window, the
// configured limits, a daily series, and breakdowns.
type UsageOverview struct {
	SpentToday models.Micro
	SpentWeek  models.Micro
	SpentMonth models.Micro

	LimitDaily   *models.Micro
	LimitWeekly  *models.Micro
	LimitMonthly *models.Micro

	// FromBalanceMonth is what metered usage took from added money this calendar month (UTC), past the allowance.
	FromBalanceMonth models.Micro

	Series   []models.CreditUsagePoint
	ByReason []models.CreditUsageBucket
	ByModel  []models.CreditUsageBucket
}

// CreditService is the application-facing API for the balance.
type CreditService interface {
	// Unmetered reports whether the ledger is bypassed for every org
	// (BILLING_PROVIDER=none): usage is unlimited and nothing is charged.
	Unmetered() bool

	// GetBalance is the spendable balance across both pools.
	GetBalance(ctx context.Context, orgID uuid.UUID) (models.Micro, *errx.Error)
	// GetLedger is the full ledger; a zero-value one when the org has none.
	GetLedger(ctx context.Context, orgID uuid.UUID) (*models.CreditLedger, *errx.Error)
	// AgencyAvailable is what the workspace's agency balance would add to its
	// own right now; zero outside an agency and when unmetered.
	AgencyAvailable(ctx context.Context, orgID uuid.UUID) models.Micro

	// Reserve checks, before metered work starts, that the workspace can pay
	// for it: the generation caps, its spend limits, and a balance of at least
	// the estimate (never below MinBalance). Nothing is taken.
	// ErrInsufficientCredits (→402) or ErrCapExceeded (→429).
	Reserve(ctx context.Context, orgID uuid.UUID, estimate models.Micro) error

	// Charge takes what delivered work actually cost, once per key. The work
	// already happened, so a balance that runs short goes to zero rather than
	// failing. Returns the amount taken.
	Charge(ctx context.Context, orgID uuid.UUID, u Usage, reason, idempotencyKey string) (models.Micro, error)

	// ChargeUpfront takes the price of work before it runs (a placement
	// test), refusing when the balance or a spend limit is short. The
	// generation caps do not apply. Undo it with RefundCharge.
	ChargeUpfront(ctx context.Context, orgID uuid.UUID, u Usage, reason, idempotencyKey string) (models.Micro, error)

	// ChargeAICost takes AI work a provider already priced (the LinkedIn query builder's cost_micros) from the
	// AI balance at exactly that amount, once per key, like Charge: a short balance goes to zero.
	ChargeAICost(ctx context.Context, orgID uuid.UUID, cost models.Micro, reason, idempotencyKey string) (models.Micro, error)

	// ChargeAmount takes an exact price for something bought (a domain, a
	// month of mailboxes), once per key, from the purchased pool only (never the
	// plan's AI allowance or agency funding), refusing when it is short.
	// No markup applies: the buyer agreed to this amount. Undo it with RefundCharge.
	ChargeAmount(ctx context.Context, orgID uuid.UUID, amount models.Micro, reason, idempotencyKey string) (models.Micro, error)
	// GetPurchasedBalance is the purchased pool alone, what ChargeAmount can take.
	GetPurchasedBalance(ctx context.Context, orgID uuid.UUID) (models.Micro, *errx.Error)

	// RefundCharge gives back what a charge under chargeKey took, to the pools
	// it came from, once. Returns the amount given back.
	RefundCharge(ctx context.Context, orgID uuid.UUID, chargeKey, reason string) (models.Micro, error)
	// ChargeOf is the debit already taken under key, nil when there is none (always nil when unmetered).
	ChargeOf(ctx context.Context, orgID uuid.UUID, key string) (*models.KeyedCharge, error)
	// RefundStore gives a store charge back to the purchased pool under refundKey, once, never past
	// what the debits under capPrefix took less what already came back under it. spendKey sizes it
	// to that debit; amount is used when spendKey is empty.
	RefundStore(ctx context.Context, orgID uuid.UUID, capPrefix, spendKey, refundKey string, amount models.Micro, reason string) (models.Micro, error)

	// Quote is what a usage would be charged at the current prices.
	Quote(ctx context.Context, u Usage) models.Micro

	// Grant adds to an org's monthly pool (a goodwill or provider-failure
	// credit). Returns the resulting balance.
	Grant(ctx context.Context, orgID uuid.UUID, amount models.Micro, reason string) (models.Micro, *errx.Error)

	// GrantPurchased adds money the workspace paid for to the purchased pool
	// (never expires). The key (a Stripe object id) makes retries safe.
	GrantPurchased(ctx context.Context, orgID uuid.UUID, amount models.Micro, reason, idempotencyKey string) (models.Micro, error)

	// ResetMonthlyAllowance sets the monthly pool to the plan's allowance
	// (purchased untouched). The key (the Stripe event id) makes retries safe.
	ResetMonthlyAllowance(ctx context.Context, orgID uuid.UUID, allowance models.Micro, idempotencyKey string) error

	// RefreshAnnualAllowances resets the monthly pool of every subscription
	// billed yearly on each month's anniversary of its period start; the
	// invoice resets the first month. Returns how many it reset.
	RefreshAnnualAllowances(ctx context.Context) (int, error)

	// CheckUsageCaps returns ErrCapExceeded when the org is at a generation
	// cap, without taking anything.
	CheckUsageCaps(ctx context.Context, orgID uuid.UUID) error

	// GetSpendSettings returns the org's spend controls, defaults filled in.
	GetSpendSettings(ctx context.Context, orgID uuid.UUID) (*models.AISpendSettings, *errx.Error)
	// UpdateSpendSettings validates and saves the org's spend limits and
	// low-balance notice threshold. Automatic reload saves separately.
	UpdateSpendSettings(ctx context.Context, orgID uuid.UUID, s *models.AISpendSettings) (*models.AISpendSettings, *errx.Error)
	// UpdateAIOverage saves whether AI may spend added money once the allowance is used up, and its monthly limit (nil: none).
	UpdateAIOverage(ctx context.Context, orgID uuid.UUID, enabled bool, monthlyLimit *models.Micro) (*models.AISpendSettings, *errx.Error)

	// GetUsageOverview composes the usage dashboard over the last `days` days
	// (clamped to 1..90).
	GetUsageOverview(ctx context.Context, orgID uuid.UUID, days int) (*UsageOverview, *errx.Error)
	// AIDailyAverage is what AI spent a day on average over the last `days` days, the store's lines left out.
	AIDailyAverage(ctx context.Context, orgID uuid.UUID, days int) (models.Micro, *errx.Error)

	// SetMonitor installs a hook invoked (async, best-effort) with the balance
	// before and after every fresh debit, which drives the low-balance notices
	// and automatic reload without this package depending on Stripe.
	SetMonitor(fn func(orgID uuid.UUID, before, after models.Micro))

	ListTransactions(ctx context.Context, orgID uuid.UUID, limit int) ([]models.CreditTransaction, *errx.Error)
	ListTransactionsBefore(ctx context.Context, orgID uuid.UUID, limit int, beforeCreatedAt time.Time, beforeID uuid.UUID) ([]models.CreditTransaction, *errx.Error)
	// ListStoreTransactionsBefore is ListTransactionsBefore limited to the store's lines: purchases, renewals, mailbox months and their refunds.
	ListStoreTransactionsBefore(ctx context.Context, orgID uuid.UUID, limit int, beforeCreatedAt time.Time, beforeID uuid.UUID) ([]models.CreditTransaction, *errx.Error)
}

// selfHostBalance is the balance reported when billing is disabled: large
// enough to read as effectively unlimited everywhere.
const selfHostBalance = 1_000_000 * models.MicroPerUSD

type creditService struct {
	repo       repository.CreditRepository
	settings   repository.AISettingsRepository
	prices     PriceSource
	cache      *cache.Cache
	shortLimit int
	dailyLimit int
	monitor    func(orgID uuid.UUID, before, after models.Micro)
	// agencyMonitor hears of charges the agency's shared balance paid part of.
	agencyMonitor func(orgID uuid.UUID)
	// selfHost bypasses the ledger (BILLING_PROVIDER=none): the operator pays
	// their providers directly, so nothing is metered.
	selfHost bool
	// agencyBilled puts agency plan allowances in force; only Stripe bills agencies.
	agencyBilled bool
}

func NewService(repo repository.CreditRepository, settings repository.AISettingsRepository, prices PriceSource, c *cache.Cache) CreditService {
	return &creditService{
		repo:       repo,
		settings:   settings,
		prices:     prices,
		cache:      c,
		shortLimit: DefaultShortLimit,
		dailyLimit: DefaultDailyLimit,
		selfHost:   config.BillingProvider() == "none",
		// Matches agency.Service.billing, so an agency's allowance exists exactly where its plan is sold.
		agencyBilled: config.BillingProvider() == "stripe",
	}
}

func (s *creditService) Unmetered() bool { return s.selfHost }

func (s *creditService) SetMonitor(fn func(orgID uuid.UUID, before, after models.Micro)) {
	s.monitor = fn
}

// SetAgencyMonitor installs a hook invoked (async, best-effort) after a charge drew on the workspace's agency balance.
func (s *creditService) SetAgencyMonitor(fn func(orgID uuid.UUID)) {
	s.agencyMonitor = fn
}

// notifyMonitor runs the watch hook on a detached goroutine so notices and
// reloads can never slow or fail a charge.
func (s *creditService) notifyMonitor(orgID uuid.UUID, before, after models.Micro) {
	if s.monitor == nil {
		return
	}
	go s.monitor(orgID, before, after)
}

func (s *creditService) priceList(ctx context.Context) instancesettings.Usage {
	if s.prices == nil {
		u := instancesettings.DefaultUsage()
		return u
	}
	return s.prices.UsagePrices(ctx)
}

func (s *creditService) Quote(ctx context.Context, u Usage) models.Micro {
	charge, _ := Price(s.priceList(ctx), u)
	return charge
}

// windowStarts returns the UTC starts of the current calendar day, ISO week
// (Monday), and calendar month.
func windowStarts(now time.Time) (day, week, month time.Time) {
	now = now.UTC()
	day = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday belongs to the week that started the prior Monday
	}
	week = day.AddDate(0, 0, -(weekday - 1))
	month = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return day, week, month
}

// checkSpendLimits refuses work that would push the org past a configured
// spend limit. Fails open on read errors: a stats hiccup must not block work
// the workspace can pay for.
func (s *creditService) checkSpendLimits(ctx context.Context, orgID uuid.UUID, amount models.Micro) error {
	if s.settings == nil {
		return nil
	}
	cfg, err := s.settings.Get(ctx, orgID)
	if err != nil || cfg == nil {
		return nil
	}
	orgLimits := cfg.SpendLimitDaily != nil || cfg.SpendLimitWeekly != nil || cfg.SpendLimitMonthly != nil
	memberLimits := cfg.MemberLimitDaily != nil || cfg.MemberLimitWeekly != nil || cfg.MemberLimitMonthly != nil
	if !orgLimits && !memberLimits {
		return nil
	}
	over := func(limit *models.Micro, spent models.Micro) bool { return limit != nil && spent+amount > *limit }
	dayStart, weekStart, monthStart := windowStarts(time.Now())
	if orgLimits {
		day, week, month, err := s.repo.SpentInWindows(ctx, orgID, dayStart, weekStart, monthStart)
		if err != nil {
			return nil
		}
		switch {
		case over(cfg.SpendLimitDaily, day):
			return fmt.Errorf("%w (daily limit $%.2f, spent $%.2f)", ErrSpendLimitReached, cfg.SpendLimitDaily.USD(), day.USD())
		case over(cfg.SpendLimitWeekly, week):
			return fmt.Errorf("%w (weekly limit $%.2f, spent $%.2f)", ErrSpendLimitReached, cfg.SpendLimitWeekly.USD(), week.USD())
		case over(cfg.SpendLimitMonthly, month):
			return fmt.Errorf("%w (monthly limit $%.2f, spent $%.2f)", ErrSpendLimitReached, cfg.SpendLimitMonthly.USD(), month.USD())
		}
	}
	// Per-member limits, attributed via the request's credit meta. Work with no
	// actor (scheduled or system) is exempt by design.
	if memberLimits {
		actor := models.CreditMetaFrom(ctx).ActorID
		if actor == uuid.Nil {
			return nil
		}
		day, week, month, err := s.repo.MemberSpentInWindows(ctx, orgID, actor, dayStart, weekStart, monthStart)
		if err != nil {
			return nil
		}
		switch {
		case over(cfg.MemberLimitDaily, day):
			return fmt.Errorf("%w (daily limit $%.2f, you spent $%.2f today)", ErrMemberLimitReached, cfg.MemberLimitDaily.USD(), day.USD())
		case over(cfg.MemberLimitWeekly, week):
			return fmt.Errorf("%w (weekly limit $%.2f, you spent $%.2f this week)", ErrMemberLimitReached, cfg.MemberLimitWeekly.USD(), week.USD())
		case over(cfg.MemberLimitMonthly, month):
			return fmt.Errorf("%w (monthly limit $%.2f, you spent $%.2f this month)", ErrMemberLimitReached, cfg.MemberLimitMonthly.USD(), month.USD())
		}
	}
	return nil
}

func (s *creditService) GetBalance(ctx context.Context, orgID uuid.UUID) (models.Micro, *errx.Error) {
	if s.selfHost {
		return selfHostBalance, nil
	}
	ledger, err := s.repo.GetBalance(ctx, orgID)
	if err != nil {
		return 0, errx.New(errx.Internal, "failed to read the balance")
	}
	if ledger == nil {
		return 0, nil
	}
	return ledger.Total(), nil
}

func (s *creditService) GetLedger(ctx context.Context, orgID uuid.UUID) (*models.CreditLedger, *errx.Error) {
	ledger, err := s.repo.GetBalance(ctx, orgID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to read the balance")
	}
	if ledger == nil {
		return &models.CreditLedger{OrgID: orgID}, nil
	}
	return ledger, nil
}

func (s *creditService) AgencyAvailable(ctx context.Context, orgID uuid.UUID) models.Micro {
	if s.selfHost {
		return 0
	}
	avail, err := s.repo.AgencyAvailable(ctx, orgID, s.agencyBilled)
	if err != nil {
		return 0
	}
	return avail
}

func (s *creditService) Reserve(ctx context.Context, orgID uuid.UUID, estimate models.Micro) error {
	if s.selfHost {
		return nil
	}
	if err := s.checkCaps(ctx, orgID); err != nil {
		return err
	}
	estimate = max(estimate, MinBalance)
	if err := s.checkSpendLimits(ctx, orgID, estimate); err != nil {
		return err
	}
	ledger, err := s.repo.GetBalance(ctx, orgID)
	if err != nil {
		return errx.New(errx.Internal, "failed to read the balance")
	}
	if ledger == nil {
		ledger = &models.CreditLedger{}
	}
	allowanceOnly, limit := s.aiOverage(ctx, orgID)
	avail, capped := ledger.Monthly, false
	if !allowanceOnly {
		purchased := ledger.Purchased
		if limit != nil {
			used, err := s.repo.AIFromPurchasedThisMonth(ctx, orgID)
			if err != nil {
				return errx.New(errx.Internal, "failed to read the balance")
			}
			if left := max(*limit-used, 0); left < purchased {
				purchased, capped = left, true
			}
		}
		avail += purchased
	}
	if avail+s.AgencyAvailable(ctx, orgID) < estimate {
		switch {
		case allowanceOnly:
			return ErrAIAllowanceUsed
		case capped:
			return ErrAIOverageLimitReached
		}
		return ErrInsufficientCredits
	}
	return nil
}

// aiOverage is the workspace's choice about AI spending added money: none
// past the allowance, or up to a monthly limit (nil: no cap).
func (s *creditService) aiOverage(ctx context.Context, orgID uuid.UUID) (allowanceOnly bool, limit *models.Micro) {
	if s.settings == nil {
		return false, nil
	}
	cfg, err := s.settings.Get(ctx, orgID)
	if err != nil || cfg == nil {
		return false, nil
	}
	return !cfg.AIOverageEnabled, cfg.AIOverageMonthlyLimit
}

// aiDebit applies the workspace's AI spending choice to a metered debit.
func (s *creditService) aiDebit(ctx context.Context, orgID uuid.UUID, d models.CreditDebit) models.CreditDebit {
	d.AllowanceOnly, d.OverageMonthlyLimit = s.aiOverage(ctx, orgID)
	return d
}

func (s *creditService) Charge(ctx context.Context, orgID uuid.UUID, u Usage, reason, idempotencyKey string) (models.Micro, error) {
	if s.selfHost {
		return 0, nil
	}
	charge, cost := Price(s.priceList(ctx), u)
	if charge <= 0 {
		return 0, nil
	}
	return s.debit(ctx, orgID, u, s.aiDebit(ctx, orgID, models.CreditDebit{
		Amount: charge, Clamp: true, Reason: reason, Cost: cost, IdempotencyKey: idempotencyKey, AgencyAllowance: s.agencyBilled,
	}))
}

func (s *creditService) ChargeUpfront(ctx context.Context, orgID uuid.UUID, u Usage, reason, idempotencyKey string) (models.Micro, error) {
	if s.selfHost {
		return 0, nil
	}
	charge, cost := Price(s.priceList(ctx), u)
	if charge <= 0 {
		return 0, nil
	}
	if err := s.checkSpendLimits(ctx, orgID, charge); err != nil {
		return 0, err
	}
	return s.debit(ctx, orgID, u, s.aiDebit(ctx, orgID, models.CreditDebit{
		Amount: charge, Reason: reason, Cost: cost, IdempotencyKey: idempotencyKey, AgencyAllowance: s.agencyBilled,
	}))
}

func (s *creditService) ChargeAICost(ctx context.Context, orgID uuid.UUID, cost models.Micro, reason, idempotencyKey string) (models.Micro, error) {
	if s.selfHost || cost <= 0 {
		return 0, nil
	}
	return s.debit(ctx, orgID, Usage{}, s.aiDebit(ctx, orgID, models.CreditDebit{
		Amount: cost, Clamp: true, Reason: reason, Cost: cost, IdempotencyKey: idempotencyKey, AgencyAllowance: s.agencyBilled,
	}))
}

func (s *creditService) ChargeAmount(ctx context.Context, orgID uuid.UUID, amount models.Micro, reason, idempotencyKey string) (models.Micro, error) {
	if s.selfHost || amount <= 0 {
		return 0, nil
	}
	return s.debit(ctx, orgID, Usage{}, models.CreditDebit{
		Amount: amount, Reason: reason, Cost: amount, IdempotencyKey: idempotencyKey, PurchasedOnly: true,
	})
}

func (s *creditService) GetPurchasedBalance(ctx context.Context, orgID uuid.UUID) (models.Micro, *errx.Error) {
	if s.selfHost {
		return selfHostBalance, nil
	}
	ledger, err := s.repo.GetBalance(ctx, orgID)
	if err != nil {
		return 0, errx.New(errx.Internal, "failed to read the balance")
	}
	if ledger == nil {
		return 0, nil
	}
	return ledger.Purchased, nil
}

func (s *creditService) debit(ctx context.Context, orgID uuid.UUID, u Usage, d models.CreditDebit) (models.Micro, error) {
	d.Model, d.InputTokens, d.OutputTokens = u.Model, u.InputTokens, u.OutputTokens
	d.Quantity = u.Quantity
	after, txn, replayed, err := s.repo.Debit(ctx, orgID, d)
	switch {
	case errors.Is(err, repository.ErrAllowanceUsed):
		return 0, ErrAIAllowanceUsed
	case errors.Is(err, repository.ErrOverageLimitReached):
		return 0, ErrAIOverageLimitReached
	case errors.Is(err, repository.ErrInsufficientCredits):
		return 0, ErrInsufficientCredits
	}
	if err != nil || txn == nil {
		return 0, err
	}
	taken := -txn.Amount
	if !replayed {
		if u.InputTokens+u.OutputTokens > 0 {
			s.commitCaps(ctx, orgID)
		}
		// The monitor watches the workspace's own balance, not what the agency paid.
		s.notifyMonitor(orgID, after+taken+txn.AgencyDelta, after)
		if txn.AgencyDelta != 0 && s.agencyMonitor != nil {
			go s.agencyMonitor(orgID)
		}
	}
	return taken, nil
}

func (s *creditService) RefundCharge(ctx context.Context, orgID uuid.UUID, chargeKey, reason string) (models.Micro, error) {
	if s.selfHost {
		return 0, nil
	}
	refunded, _, err := s.repo.RefundSpend(ctx, orgID, chargeKey, chargeKey+":refund", reason)
	return refunded, err
}

func (s *creditService) ChargeOf(ctx context.Context, orgID uuid.UUID, key string) (*models.KeyedCharge, error) {
	if s.selfHost {
		return nil, nil
	}
	return s.repo.KeyedCharge(ctx, orgID, key)
}

func (s *creditService) RefundStore(ctx context.Context, orgID uuid.UUID, capPrefix, spendKey, refundKey string, amount models.Micro, reason string) (models.Micro, error) {
	if s.selfHost {
		return 0, nil
	}
	return s.repo.RefundCapped(ctx, orgID, capPrefix, spendKey, refundKey, amount, reason)
}

// DefaultSpendSettings is what an org that never saved settings runs with.
func DefaultSpendSettings(orgID uuid.UUID) *models.AISpendSettings {
	return &models.AISpendSettings{
		OrgID:                orgID,
		LowBalanceThreshold:  DefaultLowBalance,
		AutoReloadThreshold:  DefaultReloadThreshold,
		AutoReloadAmount:     DefaultReloadAmount,
		AutoReloadMonthlyMax: DefaultReloadMonthlyLimit,
		AIOverageEnabled:     true,
	}
}

func (s *creditService) UpdateAIOverage(ctx context.Context, orgID uuid.UUID, enabled bool, monthlyLimit *models.Micro) (*models.AISpendSettings, *errx.Error) {
	if s.settings == nil {
		return nil, errx.New(errx.Internal, "spend settings unavailable")
	}
	if monthlyLimit != nil && (*monthlyLimit <= 0 || *monthlyLimit > ReloadMonthlyLimitMax) {
		return nil, errx.New(errx.BadRequest, "the monthly limit on AI using the balance must be between $0.01 and $50,000 (omit it for no limit)")
	}
	out, err := s.settings.UpsertOverage(ctx, orgID, enabled, monthlyLimit)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to save spend settings")
	}
	return out, nil
}

func (s *creditService) GetSpendSettings(ctx context.Context, orgID uuid.UUID) (*models.AISpendSettings, *errx.Error) {
	if s.settings == nil {
		return nil, errx.New(errx.Internal, "spend settings unavailable")
	}
	cfg, err := s.settings.Get(ctx, orgID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to read spend settings")
	}
	if cfg == nil {
		return DefaultSpendSettings(orgID), nil
	}
	return cfg, nil
}

func (s *creditService) UpdateSpendSettings(ctx context.Context, orgID uuid.UUID, in *models.AISpendSettings) (*models.AISpendSettings, *errx.Error) {
	if s.settings == nil {
		return nil, errx.New(errx.Internal, "spend settings unavailable")
	}
	for _, limit := range []*models.Micro{in.SpendLimitDaily, in.SpendLimitWeekly, in.SpendLimitMonthly, in.MemberLimitDaily, in.MemberLimitWeekly, in.MemberLimitMonthly} {
		if limit != nil && (*limit <= 0 || *limit > ReloadMonthlyLimitMax) {
			return nil, errx.New(errx.BadRequest, "spend limits must be between $0.01 and $50,000 (omit to remove one)")
		}
	}
	if in.LowBalanceThreshold < 0 || in.LowBalanceThreshold > ReloadThresholdMax {
		return nil, errx.New(errx.BadRequest, "the low-balance notice must be between $0 and $1,000")
	}
	in.OrgID = orgID
	out, err := s.settings.UpsertLimits(ctx, in)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to save spend settings")
	}
	return out, nil
}

func (s *creditService) GetUsageOverview(ctx context.Context, orgID uuid.UUID, days int) (*UsageOverview, *errx.Error) {
	if days < 1 || days > 90 {
		days = 30
	}
	dayStart, weekStart, monthStart := windowStarts(time.Now())
	day, week, month, err := s.repo.SpentInWindows(ctx, orgID, dayStart, weekStart, monthStart)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to read spend")
	}
	since := dayStart.AddDate(0, 0, -(days - 1))
	series, err := s.repo.UsageDaily(ctx, orgID, since)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to read the usage series")
	}
	byReason, err := s.repo.UsageBreakdown(ctx, orgID, since, "reason")
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to read the usage breakdown")
	}
	byModel, err := s.repo.UsageBreakdown(ctx, orgID, since, "model")
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to read the usage breakdown")
	}
	fromBalance, err := s.repo.AIFromPurchasedThisMonth(ctx, orgID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to read spend")
	}
	out := &UsageOverview{
		SpentToday: day, SpentWeek: week, SpentMonth: month, FromBalanceMonth: fromBalance,
		Series: series, ByReason: byReason, ByModel: byModel,
	}
	if s.settings != nil {
		if cfg, cerr := s.settings.Get(ctx, orgID); cerr == nil && cfg != nil {
			out.LimitDaily, out.LimitWeekly, out.LimitMonthly = cfg.SpendLimitDaily, cfg.SpendLimitWeekly, cfg.SpendLimitMonthly
		}
	}
	return out, nil
}

func (s *creditService) Grant(ctx context.Context, orgID uuid.UUID, amount models.Micro, reason string) (models.Micro, *errx.Error) {
	if amount <= 0 {
		return 0, errx.New(errx.BadRequest, "grant amount must be positive")
	}
	bal, _, err := s.repo.Grant(ctx, orgID, amount, reason, "")
	if err != nil {
		return 0, errx.New(errx.Internal, "failed to add to the balance")
	}
	return bal, nil
}

func (s *creditService) GrantPurchased(ctx context.Context, orgID uuid.UUID, amount models.Micro, reason, idempotencyKey string) (models.Micro, error) {
	if amount <= 0 {
		return 0, errors.New("grant amount must be positive")
	}
	bal, _, err := s.repo.GrantPurchased(ctx, orgID, amount, reason, idempotencyKey)
	return bal, err
}

func (s *creditService) ResetMonthlyAllowance(ctx context.Context, orgID uuid.UUID, allowance models.Micro, idempotencyKey string) error {
	if allowance < 0 {
		return errors.New("allowance must be non-negative")
	}
	_, err := s.repo.ResetMonthly(ctx, orgID, allowance, idempotencyKey)
	return err
}

func (s *creditService) RefreshAnnualAllowances(ctx context.Context) (int, error) {
	if s.selfHost {
		return 0, nil
	}
	due, err := s.repo.AnnualAllowancesDue(ctx, 500)
	if err != nil {
		return 0, err
	}
	now, reset := time.Now(), 0
	for _, a := range due {
		anchor, ok := LatestMonthlyAnchor(a.PeriodStart, now)
		if !ok || !a.LastReset.Before(anchor) {
			continue
		}
		key := "annual_month:" + anchor.UTC().Format("2006-01-02")
		if err := s.ResetMonthlyAllowance(ctx, a.OrgID, a.Allowance, key); err != nil {
			return reset, err
		}
		reset++
	}
	return reset, nil
}

// LatestMonthlyAnchor is the most recent monthly anniversary of start that is
// not after now, from the first month after start on; ok is false during the
// first month. A start on the 31st lands on each month's last day.
func LatestMonthlyAnchor(start, now time.Time) (time.Time, bool) {
	var anchor time.Time
	for k := 1; ; k++ {
		next := addMonthsClamped(start, k)
		if next.After(now) {
			return anchor, k > 1
		}
		anchor = next
	}
}

func addMonthsClamped(t time.Time, months int) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m+time.Month(months), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	last := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, min(d, last)-1)
}

func (s *creditService) CheckUsageCaps(ctx context.Context, orgID uuid.UUID) error {
	if s.selfHost {
		return nil
	}
	return s.checkCaps(ctx, orgID)
}

func (s *creditService) ListTransactions(ctx context.Context, orgID uuid.UUID, limit int) ([]models.CreditTransaction, *errx.Error) {
	txns, err := s.repo.ListTransactions(ctx, orgID, limit)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to list balance activity")
	}
	return txns, nil
}

func (s *creditService) ListTransactionsBefore(ctx context.Context, orgID uuid.UUID, limit int, beforeCreatedAt time.Time, beforeID uuid.UUID) ([]models.CreditTransaction, *errx.Error) {
	txns, err := s.repo.ListTransactionsBefore(ctx, orgID, limit, beforeCreatedAt, beforeID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to list balance activity")
	}
	return txns, nil
}

func (s *creditService) AIDailyAverage(ctx context.Context, orgID uuid.UUID, days int) (models.Micro, *errx.Error) {
	if days < 1 {
		days = 30
	}
	spent, err := s.repo.SpentOutsideSince(ctx, orgID, time.Now().Add(-time.Duration(days)*24*time.Hour), StoreKeyPrefix)
	if err != nil {
		return 0, errx.New(errx.Internal, "failed to read spend")
	}
	return spent / models.Micro(days), nil
}

// StoreKeyPrefix starts the idempotency key of every ledger line the store writes.
const StoreKeyPrefix = repository.StoreKeyPrefix

func (s *creditService) ListStoreTransactionsBefore(ctx context.Context, orgID uuid.UUID, limit int, beforeCreatedAt time.Time, beforeID uuid.UUID) ([]models.CreditTransaction, *errx.Error) {
	txns, err := s.repo.ListTransactionsByKeyBefore(ctx, orgID, StoreKeyPrefix, limit, beforeCreatedAt, beforeID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to list balance activity")
	}
	return txns, nil
}

func (s *creditService) shortKey(orgID uuid.UUID) string {
	return keyPrefixShort + orgID.String()
}

func (s *creditService) dailyKey(orgID uuid.UUID) string {
	return fmt.Sprintf("%s%s:%s", keyPrefixDaily, orgID.String(), time.Now().UTC().Format("2006-01-02"))
}

// checkCaps refuses when the org is at the rolling 5h or daily generation cap.
// It only reads the counters (commitCaps increments after a fresh charge), and
// fails open on Redis errors so a cache outage never blocks work.
func (s *creditService) checkCaps(ctx context.Context, orgID uuid.UUID) error {
	if s.cache == nil {
		return nil
	}
	if s.atOrOverLimit(ctx, s.shortKey(orgID), s.shortLimit) || s.atOrOverLimit(ctx, s.dailyKey(orgID), s.dailyLimit) {
		return ErrCapExceeded
	}
	return nil
}

func (s *creditService) atOrOverLimit(ctx context.Context, key string, limit int) bool {
	n, err := s.cache.Get(ctx, key).Int()
	if err == redis.Nil || err != nil {
		return false
	}
	return n >= limit
}

// commitCaps counts one generation in both windows. Best-effort.
func (s *creditService) commitCaps(ctx context.Context, orgID uuid.UUID) {
	if s.cache == nil {
		return
	}
	s.bump(ctx, s.shortKey(orgID), WindowShort)
	s.bump(ctx, s.dailyKey(orgID), WindowDaily)
}

func (s *creditService) bump(ctx context.Context, key string, window time.Duration) {
	pipe := s.cache.Pipeline()
	pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, window)
	_, _ = pipe.Exec(ctx)
}

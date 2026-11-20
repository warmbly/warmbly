package store

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/balance"
	"github.com/warmbly/warmbly/internal/app/credits"
	stripeapp "github.com/warmbly/warmbly/internal/app/stripe"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// memRepo is an in-memory StoreRepository with the same organization scoping as the SQL.
type memRepo struct {
	mu        sync.Mutex
	accounts  map[uuid.UUID]*models.StoreAccount
	quotes    map[uuid.UUID]*models.StoreQuote
	orders    map[uuid.UUID]*models.StoreOrder
	domains   map[uuid.UUID]*models.StoreDomain
	mailboxes map[uuid.UUID]*models.StoreMailbox
	seen      map[string]bool
	cursor    string
	attempted map[uuid.UUID]time.Time
	// connectAt is each mailbox's last connect claim.
	connectAt map[uuid.UUID]time.Time
	aligned   map[uuid.UUID]bool
	photos    map[uuid.UUID]*models.StorePhoto
	photoTry  map[uuid.UUID]photoTry
	// signatureTry is a mailbox's signature retry state, kept like photoTry.
	signatureTry map[uuid.UUID]photoTry
	now          func() time.Time
	// markChargedErr and reserveErr fail the next such write once, as a crash or an outage would.
	markChargedErr, reserveErr error
}

func newMemRepo() *memRepo {
	return &memRepo{accounts: map[uuid.UUID]*models.StoreAccount{}, quotes: map[uuid.UUID]*models.StoreQuote{},
		orders: map[uuid.UUID]*models.StoreOrder{}, domains: map[uuid.UUID]*models.StoreDomain{},
		mailboxes: map[uuid.UUID]*models.StoreMailbox{}, seen: map[string]bool{}, attempted: map[uuid.UUID]time.Time{}, connectAt: map[uuid.UUID]time.Time{}, aligned: map[uuid.UUID]bool{},
		photos: map[uuid.UUID]*models.StorePhoto{}, photoTry: map[uuid.UUID]photoTry{}, signatureTry: map[uuid.UUID]photoTry{}, now: time.Now}
}

var _ repository.StoreRepository = (*memRepo)(nil)

func cp[T any](v *T) *T {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

func (r *memRepo) GetAccount(_ context.Context, orgID uuid.UUID) (*models.StoreAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cp(r.accounts[orgID]), nil
}

func (r *memRepo) UpsertAccount(_ context.Context, orgID uuid.UUID, gk string, reg json.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.accounts[orgID]
	if a == nil {
		a = &models.StoreAccount{OrganizationID: orgID}
		r.accounts[orgID] = a
	}
	if gk != "" {
		a.GraphKitAccountID = gk
	}
	if reg != nil {
		a.Registrant = reg
	}
	return nil
}

func (r *memRepo) SetIdentityDigest(_ context.Context, orgID uuid.UUID, digest string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a := r.accounts[orgID]; a != nil {
		a.IdentityDigest = digest
	}
	return nil
}

func (r *memRepo) AccountOrgs(context.Context, int) ([]uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []uuid.UUID{}
	for id := range r.accounts {
		out = append(out, id)
	}
	return out, nil
}

func (r *memRepo) SetPlanNotice(_ context.Context, orgID uuid.UUID, at *time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.accounts[orgID]
	if a == nil || (a.PlanNoticeAt == nil) != (at != nil) {
		return false, nil
	}
	a.PlanNoticeAt = at
	return true, nil
}

func (r *memRepo) CreateQuote(_ context.Context, q *models.StoreQuote) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.quotes[q.ID] = cp(q)
	return nil
}

func (r *memRepo) GetQuote(_ context.Context, orgID, id uuid.UUID) (*models.StoreQuote, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	q := r.quotes[id]
	if q == nil || q.OrganizationID != orgID {
		return nil, nil
	}
	return cp(q), nil
}

func (r *memRepo) PruneQuotes(context.Context, time.Time) (int64, error) { return 0, nil }

func (r *memRepo) ReplaceQuote(_ context.Context, orgID, id, by uuid.UUID) (uuid.UUID, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	q := r.quotes[id]
	if q == nil || q.OrganizationID != orgID {
		return uuid.Nil, false, nil
	}
	if q.ReplacedBy != nil {
		return *q.ReplacedBy, false, nil
	}
	for _, o := range r.orders {
		if o.QuoteID == id {
			return uuid.Nil, true, nil
		}
	}
	q.ReplacedBy = &by
	return by, false, nil
}

func (r *memRepo) CreateOrder(_ context.Context, o *models.StoreOrder) (*models.StoreOrder, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if q := r.quotes[o.QuoteID]; q != nil && q.ReplacedBy != nil {
		return nil, false, repository.ErrStoreQuoteReplaced
	}
	for _, ex := range r.orders {
		if ex.QuoteID == o.QuoteID {
			if ex.OrganizationID != o.OrganizationID {
				return nil, false, errors.New("quote held by another organization")
			}
			return cp(ex), true, nil
		}
	}
	n := cp(o)
	n.CreatedAt, n.UpdatedAt = r.now(), r.now()
	if n.Steps == nil {
		n.Steps = json.RawMessage(`[]`)
	}
	r.orders[o.ID] = n
	return cp(n), false, nil
}

func (r *memRepo) order(orgID, id uuid.UUID) *models.StoreOrder {
	o := r.orders[id]
	if o == nil || o.OrganizationID != orgID {
		return nil
	}
	return o
}

func (r *memRepo) GetOrder(_ context.Context, orgID, id uuid.UUID) (*models.StoreOrder, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cp(r.order(orgID, id)), nil
}

func (r *memRepo) GetOrderByGraphKitID(_ context.Context, orgID uuid.UUID, gk string) (*models.StoreOrder, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, o := range r.orders {
		if o.OrganizationID == orgID && o.GraphKitOrderID != nil && *o.GraphKitOrderID == gk {
			return cp(o), nil
		}
	}
	return nil, nil
}

func (r *memRepo) GetOrderByQuote(_ context.Context, orgID, quoteID uuid.UUID) (*models.StoreOrder, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, o := range r.orders {
		if o.OrganizationID == orgID && o.QuoteID == quoteID {
			return cp(o), nil
		}
	}
	return nil, nil
}

func (r *memRepo) ListOrders(_ context.Context, orgID uuid.UUID, _ *models.StorePage, limit int) ([]models.StoreOrder, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreOrder
	for _, o := range r.orders {
		if o.OrganizationID == orgID {
			out = append(out, *o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out[:min(limit, len(out))], nil
}

func (r *memRepo) DeleteUnchargedOrder(_ context.Context, orgID, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if o := r.order(orgID, id); o != nil && o.Status == models.StoreOrderCharging && o.ChargedMicros == 0 && o.GraphKitOrderID == nil {
		delete(r.orders, id)
	}
	return nil
}

func (r *memRepo) MarkOrderCharged(_ context.Context, orgID, id uuid.UUID, charged models.Micro) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.markChargedErr; err != nil {
		r.markChargedErr = nil
		return err
	}
	if o := r.order(orgID, id); o != nil {
		o.ChargedMicros = charged
	}
	return nil
}

func (r *memRepo) MarkOrderPlaced(_ context.Context, orgID, id uuid.UUID, gk, status string, steps json.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if o := r.order(orgID, id); o != nil && (o.GraphKitOrderID == nil || *o.GraphKitOrderID == gk) {
		o.GraphKitOrderID, o.Status, o.Steps = &gk, status, steps
	}
	return nil
}

func (r *memRepo) UpdateOrderState(_ context.Context, orgID, id uuid.UUID, status string, steps json.RawMessage, done *time.Time) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o := r.order(orgID, id)
	if o == nil || o.Status == models.StoreOrderFailed {
		return "", nil
	}
	before := o.Status
	o.Status, o.Steps = status, steps
	if o.CompletedAt == nil {
		o.CompletedAt = done
	}
	return before, nil
}

func (r *memRepo) MarkOrderFailed(_ context.Context, orgID, id uuid.UUID, code string, refunded models.Micro, pending bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if o := r.order(orgID, id); o != nil {
		o.Status, o.FailureCode, o.RefundedMicros, o.RefundPending = models.StoreOrderFailed, code, max(o.RefundedMicros, refunded), pending
		o.UpdatedAt = r.now()
	}
	return nil
}

func (r *memRepo) MarkOrderRefunded(_ context.Context, orgID, id uuid.UUID, refunded models.Micro) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if o := r.order(orgID, id); o != nil {
		o.RefundPending, o.RefundedMicros = false, max(o.RefundedMicros, refunded)
	}
	return nil
}

func (r *memRepo) RefundPendingOrders(_ context.Context, before time.Time, limit int) ([]models.StoreOrder, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreOrder
	for _, o := range r.orders {
		if o.RefundPending && o.UpdatedAt.Before(before) {
			out = append(out, *o)
		}
	}
	return out[:min(limit, len(out))], nil
}

func (r *memRepo) StrandedOrders(_ context.Context, before time.Time, limit int) ([]models.StoreOrder, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreOrder
	for _, o := range r.orders {
		if o.Status == models.StoreOrderCharging && o.ChargedMicros == 0 && o.GraphKitOrderID == nil && o.UpdatedAt.Before(before) {
			out = append(out, *o)
		}
	}
	return out[:min(limit, len(out))], nil
}

func (r *memRepo) AddOrderRefund(_ context.Context, orgID, id uuid.UUID, key string, amount models.Micro) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o := r.order(orgID, id)
	if o == nil || slices.Contains(o.RefundedItems, key) {
		return false, nil
	}
	o.RefundedItems = append(o.RefundedItems, key)
	o.RefundedMicros += amount
	return true, nil
}

func (r *memRepo) ListOpenOrders(_ context.Context, before time.Time, limit int, uncharged bool) ([]models.StoreOrder, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreOrder
	for _, o := range r.orders {
		if (o.Status == models.StoreOrderCharging || o.Status == models.StoreOrderProcessing) &&
			(o.Status == models.StoreOrderProcessing || o.ChargedMicros > 0 || o.GraphKitOrderID != nil || uncharged) {
			out = append(out, *o)
		}
	}
	return out[:min(limit, len(out))], nil
}

func (r *memRepo) withCount(d *models.StoreDomain) *models.StoreDomain {
	c := cp(d)
	c.MailboxCount = 0
	for _, m := range r.mailboxes {
		if m.DomainID == d.ID && m.Status != "cancelled" && m.Status != "failed" {
			c.MailboxCount++
		}
	}
	return c
}

func (r *memRepo) UpsertDomain(_ context.Context, d *models.StoreDomain) (*models.StoreDomain, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ex := range r.domains {
		if ex.GraphKitDomainID == d.GraphKitDomainID {
			if ex.OrganizationID != d.OrganizationID {
				return nil, nil
			}
			if d.ExpiresAt != nil && (ex.ExpiresAt == nil || !ex.ExpiresAt.Equal(*d.ExpiresAt)) {
				ex.RenewalNoticeAt, ex.LastNotice = nil, ""
			}
			ex.Name, ex.Status, ex.ForwardingURL, ex.MailAuth, ex.Details = d.Name, d.Status, d.ForwardingURL, d.MailAuth, d.Details
			if d.Provider != "" {
				ex.Provider = d.Provider
			}
			if d.ExpiresAt != nil {
				ex.ExpiresAt = d.ExpiresAt
			}
			if d.RenewRetailMicros != 0 {
				ex.RenewRetailMicros = d.RenewRetailMicros
			}
			if ex.OrderID == nil {
				ex.OrderID = d.OrderID
			}
			return r.withCount(ex), nil
		}
	}
	n := cp(d)
	if n.Source == "" {
		n.Source = models.StoreDomainPurchased
	}
	n.AutoRenew, n.LastSeenRenewMicros, n.CreatedAt = n.Source == models.StoreDomainPurchased, d.RenewRetailMicros, r.now()
	r.domains[n.ID] = n
	return r.withCount(n), nil
}

func (r *memRepo) domain(orgID, id uuid.UUID) *models.StoreDomain {
	d := r.domains[id]
	if d == nil || d.OrganizationID != orgID {
		return nil
	}
	return d
}

func (r *memRepo) GetDomain(_ context.Context, orgID, id uuid.UUID) (*models.StoreDomain, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d := r.domain(orgID, id); d != nil {
		return r.withCount(d), nil
	}
	return nil, nil
}

func (r *memRepo) GetDomainByGraphKitID(_ context.Context, orgID uuid.UUID, gk string) (*models.StoreDomain, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.domains {
		if d.OrganizationID == orgID && d.GraphKitDomainID == gk {
			return r.withCount(d), nil
		}
	}
	return nil, nil
}

func (r *memRepo) ListDomains(_ context.Context, orgID uuid.UUID, _ *models.StorePage, limit int) ([]models.StoreDomain, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreDomain
	for _, d := range r.domains {
		if d.OrganizationID == orgID {
			out = append(out, *r.withCount(d))
		}
	}
	return out[:min(limit, len(out))], nil
}

func (r *memRepo) ListDomainsByOrder(_ context.Context, orgID, orderID uuid.UUID) ([]models.StoreDomain, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreDomain
	for _, d := range r.domains {
		if d.OrganizationID == orgID && d.OrderID != nil && *d.OrderID == orderID {
			out = append(out, *r.withCount(d))
		}
	}
	return out, nil
}

func (r *memRepo) SetDomainAutoRenew(_ context.Context, orgID, id uuid.UUID, on bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d := r.domain(orgID, id); d != nil {
		d.AutoRenew = on
		if on {
			if d.RenewalChargeKey == nil {
				d.RenewalFailures = 0
			}
			d.RenewalAttemptAt = nil
		}
	}
	return nil
}

func (r *memRepo) SetDomainForwarding(_ context.Context, orgID, id uuid.UUID, url string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d := r.domain(orgID, id); d != nil {
		d.ForwardingURL = url
	}
	return nil
}

func (r *memRepo) SetDomainTrackingHost(_ context.Context, orgID, id uuid.UUID, host string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d := r.domain(orgID, id); d != nil {
		d.TrackingHost = host
	}
	return nil
}

func (r *memRepo) DeleteConnectedDomain(_ context.Context, orgID, id uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := r.domain(orgID, id)
	if d == nil || d.Source != models.StoreDomainConnected || r.withCount(d).MailboxCount > 0 {
		return false, nil
	}
	delete(r.domains, id)
	return true, nil
}

func (r *memRepo) GetDomainByName(_ context.Context, orgID uuid.UUID, name string) (*models.StoreDomain, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.domains {
		if d.OrganizationID == orgID && d.Name == name && d.Status != "failed" && d.Status != "transferred_out" && d.Status != "removed" {
			return r.withCount(d), nil
		}
	}
	return nil, nil
}

func (r *memRepo) DomainsNeedingTracking(_ context.Context, limit int) ([]models.StoreDomain, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreDomain
	for _, d := range r.domains {
		if d.Status != "active" || d.TrackingHost != "" {
			continue
		}
		for _, m := range r.mailboxes {
			if m.DomainID == d.ID && m.EmailAccountID != nil {
				out = append(out, *r.withCount(d))
				break
			}
		}
	}
	return out[:min(limit, len(out))], nil
}

func (r *memRepo) RecordDomainRenewal(_ context.Context, orgID, id uuid.UUID, exp time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d := r.domain(orgID, id); d != nil {
		if d.ExpiresAt == nil || exp.After(*d.ExpiresAt) {
			d.ExpiresAt = &exp
		}
		now := r.now()
		d.RenewalFailures, d.RenewalAttemptAt, d.LastNotice, d.RenewalNoticeAt, d.RenewalChargeKey = 0, &now, "", nil, nil
	}
	return nil
}

func (r *memRepo) RecordDomainRenewalFailure(_ context.Context, orgID, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d := r.domain(orgID, id); d != nil {
		now := r.now()
		d.RenewalFailures++
		d.RenewalAttemptAt, d.RenewalChargeKey = &now, nil
	}
	return nil
}

func (r *memRepo) SetRenewalCharge(_ context.Context, orgID, id uuid.UUID, key *string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d := r.domain(orgID, id); d != nil {
		d.RenewalChargeKey = cp(key)
	}
	return nil
}

func (r *memRepo) DueDomainRenewals(_ context.Context, horizon, attemptedBefore, noticedBefore, urgent time.Time, limit int) ([]models.StoreDomain, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreDomain
	for _, d := range r.domains {
		due := d.AutoRenew && !d.ExpiresAt.After(horizon) &&
			(d.RenewalAttemptAt == nil || d.RenewalAttemptAt.Before(attemptedBefore)) &&
			((d.RenewalNoticeAt != nil && !d.RenewalNoticeAt.After(noticedBefore)) || !d.ExpiresAt.After(urgent))
		if d.ExpiresAt != nil && ((due && (d.Status == "active" || d.Status == "expired")) || d.RenewalChargeKey != nil) {
			out = append(out, *r.withCount(d))
		}
	}
	return out[:min(limit, len(out))], nil
}

func (r *memRepo) DomainsExpiringBefore(_ context.Context, horizon time.Time, limit int) ([]models.StoreDomain, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreDomain
	for _, d := range r.domains {
		if (d.Status == "active" || d.Status == "expired") && d.ExpiresAt != nil && !d.ExpiresAt.After(horizon) {
			out = append(out, *r.withCount(d))
		}
	}
	return out[:min(limit, len(out))], nil
}

func (r *memRepo) SetDomainNotice(_ context.Context, id uuid.UUID, notice string, stated models.Micro, upcoming bool) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := r.domains[id]
	if d == nil || d.LastNotice == notice {
		return false, nil
	}
	d.LastNotice, d.LastSeenRenewMicros = notice, stated
	if upcoming {
		now := r.now()
		d.RenewalNoticeAt = &now
	}
	return true, nil
}

func (r *memRepo) UpsertMailbox(_ context.Context, m *models.StoreMailbox) (*models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ex := range r.mailboxes {
		if ex.GraphKitMailboxID == m.GraphKitMailboxID {
			if ex.OrganizationID != m.OrganizationID {
				return nil, nil
			}
			ex.Email, ex.Status, ex.FirstName, ex.LastName, ex.CancelsAt, ex.ConnectMethod = m.Email, m.Status, m.FirstName, m.LastName, m.CancelsAt, m.ConnectMethod
			ex.IsAdmin = m.IsAdmin
			if m.PeriodEnd != nil {
				if !r.aligned[ex.ID] {
					ex.PaidThrough = *m.PeriodEnd
					r.aligned[ex.ID] = true
				}
				pe := *m.PeriodEnd
				ex.PeriodEnd = &pe
			}
			if ex.OrderID == nil {
				ex.OrderID = m.OrderID
			}
			return cp(ex), nil
		}
	}
	n := cp(m)
	n.ConnectState, n.CreatedAt = models.StoreConnectWaiting, r.now()
	if m.PeriodEnd != nil {
		n.PaidThrough = *m.PeriodEnd
		r.aligned[n.ID] = true
	}
	r.mailboxes[n.ID] = n
	return cp(n), nil
}

func (r *memRepo) mailbox(orgID, id uuid.UUID) *models.StoreMailbox {
	m := r.mailboxes[id]
	if m == nil || m.OrganizationID != orgID {
		return nil
	}
	return m
}

func (r *memRepo) GetMailbox(_ context.Context, orgID, id uuid.UUID) (*models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cp(r.mailbox(orgID, id)), nil
}

func (r *memRepo) GetMailboxByGraphKitID(_ context.Context, orgID uuid.UUID, gk string) (*models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.mailboxes {
		if m.OrganizationID == orgID && m.GraphKitMailboxID == gk {
			return cp(m), nil
		}
	}
	return nil, nil
}

func (r *memRepo) GetMailboxByEmail(_ context.Context, orgID uuid.UUID, email string) (*models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.mailboxes {
		if m.OrganizationID == orgID && m.Email == email && m.Status != "cancelled" && m.Status != "failed" {
			return cp(m), nil
		}
	}
	return nil, nil
}

// newestFirst orders mailboxes as the repository does: created_at, then id, descending.
func newestFirst(out []models.StoreMailbox) {
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID.String() > out[j].ID.String()
	})
}

func (r *memRepo) ListMailboxes(_ context.Context, orgID uuid.UUID, _ *models.StorePage, limit int) ([]models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreMailbox
	for _, m := range r.mailboxes {
		if m.OrganizationID == orgID {
			out = append(out, *m)
		}
	}
	newestFirst(out)
	return out[:min(limit, len(out))], nil
}

func unsettledCancel(m *models.StoreMailbox) bool {
	return m.UnpaidCancel && m.BillingFailures == 0 && endedMailbox(m)
}

func (r *memRepo) OpenMailboxes(_ context.Context, orgID uuid.UUID) ([]models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreMailbox
	for _, m := range r.mailboxes {
		if m.OrganizationID == orgID && (!endedMailbox(m) || unsettledCancel(m)) {
			out = append(out, *m)
		}
	}
	newestFirst(out)
	return out, nil
}

func (r *memRepo) OrgsWithUnsettledCancels(context.Context, int) ([]uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[uuid.UUID]bool{}
	out := []uuid.UUID{}
	for _, m := range r.mailboxes {
		if unsettledCancel(m) && !seen[m.OrganizationID] {
			seen[m.OrganizationID] = true
			out = append(out, m.OrganizationID)
		}
	}
	return out, nil
}

func (r *memRepo) ListMailboxesByOrder(_ context.Context, orgID, orderID uuid.UUID) ([]models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreMailbox
	for _, m := range r.mailboxes {
		if m.OrganizationID == orgID && m.OrderID != nil && *m.OrderID == orderID {
			out = append(out, *m)
		}
	}
	return out, nil
}

func (r *memRepo) SetMailboxNames(_ context.Context, orgID, id uuid.UUID, first, last string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m := r.mailbox(orgID, id); m != nil {
		m.FirstName, m.LastName = first, last
	}
	return nil
}

func (r *memRepo) SetMailboxCancel(_ context.Context, orgID, id uuid.UUID, status string, at *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m := r.mailbox(orgID, id); m != nil {
		m.Status, m.CancelsAt = status, at
		m.UnpaidCancel = false
	}
	return nil
}

func (r *memRepo) GetMailboxByAccount(_ context.Context, orgID, accountID uuid.UUID) (*models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.mailboxes {
		if m.OrganizationID == orgID && m.EmailAccountID != nil && *m.EmailAccountID == accountID {
			return cp(m), nil
		}
	}
	return nil, nil
}

func endedMailbox(m *models.StoreMailbox) bool {
	return m.Status == "cancelled" || m.Status == "failed"
}

func (r *memRepo) MarkMailboxEnded(_ context.Context, orgID, id uuid.UUID, at time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.mailbox(orgID, id)
	if m == nil || m.CancelledAt != nil || !endedMailbox(m) {
		return false, nil
	}
	m.CancelledAt = &at
	return true, nil
}

func (r *memRepo) UnmarkedEndedMailboxes(context.Context, int) ([]models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreMailbox
	for _, m := range r.mailboxes {
		if m.CancelledAt == nil && endedMailbox(m) {
			out = append(out, *m)
		}
	}
	return out, nil
}

// detach is the email account's ON DELETE SET NULL.
func (r *memRepo) detach(accountID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.mailboxes {
		if m.EmailAccountID != nil && *m.EmailAccountID == accountID {
			m.EmailAccountID = nil
		}
	}
}

// claimable mirrors storeConnectRetry, with connectAt standing in for connect_attempted_at.
func (r *memRepo) claimable(m *models.StoreMailbox, now time.Time) bool {
	at, tried := r.connectAt[m.ID]
	return m.EmailAccountID == nil && m.Status == "active" && m.CancelledAt == nil && ((m.ConnectState == models.StoreConnectWaiting &&
		(m.ActionRequired == nil || !tried || at.Before(now.Add(-30*time.Minute)))) ||
		(m.ConnectState == models.StoreConnectFailed && m.ConnectAttempts < 5 && at.Before(now.Add(-10*time.Minute*time.Duration(m.ConnectAttempts)))))
}

func (r *memRepo) ClaimConnect(_ context.Context, orgID, id uuid.UUID, now time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.mailbox(orgID, id)
	if m == nil || !r.claimable(m, now) {
		return false, nil
	}
	m.ConnectState = models.StoreConnectConnecting
	m.ConnectAttempts++
	r.connectAt[m.ID] = now
	return true, nil
}

func (r *memRepo) FinishConnect(_ context.Context, orgID, id uuid.UUID, acc *uuid.UUID, state, msg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m := r.mailbox(orgID, id); m != nil {
		if acc != nil {
			m.EmailAccountID = acc
		}
		m.ConnectState, m.ConnectError, m.ActionRequired, m.ActionReason = state, msg, nil, nil
	}
	return nil
}

func (r *memRepo) AwaitConnectAction(_ context.Context, orgID, id uuid.UUID, action, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m := r.mailbox(orgID, id); m != nil && m.EmailAccountID == nil {
		m.ConnectState, m.ConnectError, m.ConnectAttempts, m.ActionRequired, m.ActionReason = models.StoreConnectWaiting, "", 0, &action, nil
		if reason != "" {
			m.ActionReason = &reason
		}
	}
	return nil
}

func (r *memRepo) DomainActionReason(_ context.Context, orgID, domainID uuid.UUID, action string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.mailboxes {
		if m.OrganizationID == orgID && m.DomainID == domainID && m.EmailAccountID == nil && liveMailbox(m) &&
			m.ActionRequired != nil && *m.ActionRequired == action && m.ActionReason != nil {
			return *m.ActionReason, nil
		}
	}
	return "", nil
}

func (r *memRepo) WakeConnectAction(_ context.Context, orgID uuid.UUID, action string, domains []string) ([]models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreMailbox
	for _, m := range r.mailboxes {
		at := strings.LastIndex(m.Email, "@")
		if m.OrganizationID == orgID && m.EmailAccountID == nil && m.ActionRequired != nil && *m.ActionRequired == action && slices.Contains(domains, m.Email[at+1:]) {
			m.ActionRequired, m.ActionReason = nil, nil
			out = append(out, *m)
		}
	}
	return out, nil
}

func (r *memRepo) PendingConnects(_ context.Context, now time.Time, _ int) ([]models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreMailbox
	for _, m := range r.mailboxes {
		if r.claimable(m, now) {
			out = append(out, *m)
		}
	}
	return out, nil
}

func billable(m *models.StoreMailbox, now time.Time) bool {
	return m.PendingChargeKey == nil && kept(m, now)
}

// kept is a mailbox whose next month will start once paid.
func kept(m *models.StoreMailbox, now time.Time) bool {
	if (m.Status == "active" || m.Status == "suspended") && m.CancelsAt == nil {
		return true
	}
	return m.UnpaidCancel && m.Status == "cancelling" && m.CancelsAt != nil && m.CancelsAt.After(now.Add(time.Hour))
}

func (r *memRepo) MailboxesDue(_ context.Context, orgID uuid.UUID, before, now time.Time) ([]models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreMailbox
	for _, m := range r.mailboxes {
		if m.OrganizationID == orgID && billable(m, now) && m.PaidThrough.Before(before) {
			out = append(out, *m)
		}
	}
	return out, nil
}

func (r *memRepo) UnpaidMailboxes(_ context.Context, orgID uuid.UUID, now, attemptedBefore time.Time, withPending bool) ([]models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreMailbox
	for _, m := range r.mailboxes {
		last, tried := r.attempted[m.ID]
		if m.OrganizationID == orgID && (m.PendingChargeKey == nil || withPending) && kept(m, now) && m.BillingFailures > 0 &&
			(!tried || last.Before(attemptedBefore) || withPending) {
			out = append(out, *m)
		}
	}
	return out, nil
}

func (r *memRepo) SetUnpaidCancel(_ context.Context, orgID, id uuid.UUID, status string, at *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m := r.mailbox(orgID, id); m != nil {
		m.Status, m.CancelsAt, m.UnpaidCancel = status, cp(at), true
	}
	return nil
}

func (r *memRepo) MarkMonthPaid(_ context.Context, orgID, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m := r.mailbox(orgID, id); m != nil {
		m.PaidThrough, m.BillingFailures, m.LastNotice, m.PaidKey, m.PaidMicros = m.PaidThrough.AddDate(0, 1, 0), 0, "", nil, 0
	}
	return nil
}

func (r *memRepo) RefundMonth(_ context.Context, orgID, id uuid.UUID, paidKey string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.mailbox(orgID, id)
	if m == nil || m.PaidKey == nil || *m.PaidKey != paidKey || m.PendingChargeKey != nil {
		return false, nil
	}
	m.PaidThrough, m.PaidKey, m.PaidMicros, m.UnpaidCancel = m.PaidThrough.AddDate(0, -1, 0), nil, 0, false
	return true, nil
}

func (r *memRepo) SetMailboxRetail(_ context.Context, orgID, id uuid.UUID, monthly models.Micro) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m := r.mailbox(orgID, id); m != nil {
		m.MonthlyRetailMicros = monthly
	}
	return nil
}

func memLive(m *models.StoreMailbox) bool {
	return m.Status == "active" || m.Status == "suspended" || m.Status == "cancelling"
}

func (r *memRepo) MailboxesPastPeriod(_ context.Context, now time.Time, _ int) ([]models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreMailbox
	for _, m := range r.mailboxes {
		if memLive(m) && m.PeriodEnd != nil && !m.PeriodEnd.After(now) {
			out = append(out, *m)
		}
	}
	return out, nil
}

func (r *memRepo) UncollectedMailboxes(_ context.Context, _ int) ([]models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreMailbox
	for _, m := range r.mailboxes {
		if memLive(m) && m.PeriodEnd != nil && m.PeriodEnd.After(m.PaidThrough.Add(time.Hour)) {
			out = append(out, *m)
		}
	}
	return out, nil
}

func (r *memRepo) SetReloadState(_ context.Context, orgID uuid.UUID, managed bool, threshold, amount models.Micro) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a := r.accounts[orgID]; a != nil {
		a.ReloadDecided, a.ReloadManaged, a.ReloadThreshold, a.ReloadAmount = true, managed, threshold, amount
	}
	return nil
}

func (r *memRepo) SetBillAnchor(_ context.Context, orgID uuid.UUID, day int, next time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a := r.accounts[orgID]; a != nil {
		a.BillDay, a.NextBillAnchor = day, &next
	}
	return nil
}

func (r *memRepo) ReserveMailboxCharge(_ context.Context, orgID uuid.UUID, rows []models.StoreMailbox, key string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.reserveErr; err != nil {
		r.reserveErr = nil
		return 0, err
	}
	var n int64
	for i := range rows {
		if m := r.mailbox(orgID, rows[i].ID); m != nil && m.PendingChargeKey == nil && m.PaidThrough.Equal(rows[i].PaidThrough) {
			k := key
			m.PendingChargeKey, m.PendingChargeMicros = &k, m.MonthlyRetailMicros
			r.attempted[m.ID] = r.now()
			n++
		}
	}
	return n, nil
}

func (r *memRepo) ReserveRun(ctx context.Context, orgID uuid.UUID, rows []models.StoreMailbox, key string, anchor, next time.Time) (bool, error) {
	r.mu.Lock()
	if err := r.reserveErr; err != nil {
		r.reserveErr = nil
		r.mu.Unlock()
		return false, err
	}
	a := r.accounts[orgID]
	if a == nil || a.NextBillAnchor == nil || !a.NextBillAnchor.Equal(anchor) {
		r.mu.Unlock()
		return false, nil
	}
	a.NextBillAnchor = &next
	r.mu.Unlock()
	_, err := r.ReserveMailboxCharge(ctx, orgID, rows, key)
	return true, err
}

func (r *memRepo) MailboxesByChargeKey(_ context.Context, orgID uuid.UUID, key string) ([]models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreMailbox
	for _, m := range r.mailboxes {
		if m.OrganizationID == orgID && m.PendingChargeKey != nil && *m.PendingChargeKey == key {
			out = append(out, *m)
		}
	}
	return out, nil
}

func (r *memRepo) PendingChargeKeys(_ context.Context, orgID uuid.UUID, before time.Time) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, m := range r.mailboxes {
		at, tried := r.attempted[m.ID]
		if m.OrganizationID == orgID && m.PendingChargeKey != nil && (!tried || at.Before(before)) && !slices.Contains(out, *m.PendingChargeKey) {
			out = append(out, *m.PendingChargeKey)
		}
	}
	return out, nil
}

func (r *memRepo) SettleMailboxCharge(_ context.Context, orgID uuid.UUID, key string, now time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for _, m := range r.mailboxes {
		if m.OrganizationID == orgID && m.PendingChargeKey != nil && *m.PendingChargeKey == key && kept(m, now) {
			k := key
			m.PaidThrough, m.PendingChargeKey, m.BillingFailures, m.LastNotice = addMonth(m.PaidThrough), nil, 0, ""
			m.PaidKey, m.PaidMicros = &k, m.PendingChargeMicros
			n++
		}
	}
	return n, nil
}

func (r *memRepo) DetachMailboxCharge(_ context.Context, orgID uuid.UUID, key string, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m := r.mailbox(orgID, id); m != nil && m.PendingChargeKey != nil && *m.PendingChargeKey == key {
		m.PendingChargeKey = nil
	}
	return nil
}

func (r *memRepo) ReleaseMailboxCharge(_ context.Context, orgID uuid.UUID, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.mailboxes {
		if m.OrganizationID == orgID && m.PendingChargeKey != nil && *m.PendingChargeKey == key {
			m.PendingChargeKey = nil
			m.BillingFailures++
		}
	}
	return nil
}

func (r *memRepo) ExtendUnmetered(_ context.Context, orgID uuid.UUID, ids []uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range ids {
		if m := r.mailbox(orgID, id); m != nil {
			m.PaidThrough = addMonth(m.PaidThrough)
		}
	}
	return nil
}

func (r *memRepo) SetMailboxNotice(_ context.Context, id uuid.UUID, notice string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.mailboxes[id]
	if m == nil || m.LastNotice == notice {
		return false, nil
	}
	m.LastNotice = notice
	return true, nil
}

func (r *memRepo) DomainAdminMailbox(_ context.Context, orgID, domainID uuid.UUID) (*models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.mailboxes {
		if m.OrganizationID == orgID && m.DomainID == domainID && m.IsAdmin && liveMailbox(m) {
			cp := *m
			return &cp, nil
		}
	}
	return nil, nil
}

func liveMailbox(m *models.StoreMailbox) bool {
	switch m.Status {
	case "provisioning", "active", "suspended", "cancelling":
		return true
	}
	return false
}

func (r *memRepo) OrgsWithLiveMailboxes(context.Context, int) ([]uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[uuid.UUID]bool{}
	out := []uuid.UUID{}
	for _, m := range r.mailboxes {
		if liveMailbox(m) && !seen[m.OrganizationID] {
			seen[m.OrganizationID] = true
			out = append(out, m.OrganizationID)
		}
	}
	return out, nil
}

func (r *memRepo) HasLiveMailboxes(_ context.Context, orgID uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.mailboxes {
		if m.OrganizationID == orgID && liveMailbox(m) {
			return true, nil
		}
	}
	return false, nil
}

func (r *memRepo) ClearBillingRetry(_ context.Context, orgID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.mailboxes {
		if m.OrganizationID == orgID && m.BillingFailures > 0 && m.PendingChargeKey == nil {
			delete(r.attempted, m.ID)
		}
	}
	for _, d := range r.domains {
		if d.OrganizationID == orgID && d.RenewalFailures > 0 && d.AutoRenew {
			d.RenewalAttemptAt = nil
		}
	}
	return nil
}

func (r *memRepo) WebhookEventSeen(_ context.Context, id string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen[id], nil
}

func (r *memRepo) RecordWebhookEvent(_ context.Context, id string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fresh := !r.seen[id]
	r.seen[id] = true
	return fresh, nil
}

func (r *memRepo) PruneWebhookEvents(context.Context, time.Time) (int64, error) { return 0, nil }

func (r *memRepo) GetEventsCursor(context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cursor, nil
}

func (r *memRepo) SetEventsCursor(_ context.Context, c string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cursor = c
	return nil
}

// ---- a ledger with the credits package's idempotency rules --------------

type ledgerEntry struct {
	org      uuid.UUID
	amount   models.Micro
	reason   string
	refunded bool
}

type memCredits struct {
	mu        sync.Mutex
	unmetered bool
	// balance is the purchased pool, money the workspace added; monthly is the plan's AI allowance, which the store never spends.
	balance map[uuid.UUID]models.Micro
	monthly map[uuid.UUID]models.Micro
	charges map[string]*ledgerEntry
	order   []string
	// failNext fails the next debit, as a crash between the card charge and the debit would.
	failNext error
	// failRefund fails every capped refund while set, as a ledger outage would.
	failRefund error
	// onDebit runs after each fresh debit, as the balance's monitor hook does.
	onDebit func(org uuid.UUID)
}

func newMemCredits() *memCredits {
	return &memCredits{balance: map[uuid.UUID]models.Micro{}, monthly: map[uuid.UUID]models.Micro{}, charges: map[string]*ledgerEntry{}}
}

func (c *memCredits) Unmetered() bool { return c.unmetered }

func (c *memCredits) GetBalance(_ context.Context, org uuid.UUID) (models.Micro, *errx.Error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.monthly[org] + c.balance[org], nil
}

func (c *memCredits) GetPurchasedBalance(_ context.Context, org uuid.UUID) (models.Micro, *errx.Error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.balance[org], nil
}

func (c *memCredits) ChargeAmount(_ context.Context, org uuid.UUID, amount models.Micro, reason, key string) (models.Micro, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.charges[key]; e != nil {
		return e.amount, nil
	}
	if err := c.failNext; err != nil {
		c.failNext = nil
		return 0, err
	}
	if c.balance[org] < amount {
		return 0, credits.ErrInsufficientCredits
	}
	c.balance[org] -= amount
	c.charges[key] = &ledgerEntry{org: org, amount: amount, reason: reason}
	c.order = append(c.order, key)
	if hook := c.onDebit; hook != nil {
		defer func() { c.mu.Unlock(); hook(org); c.mu.Lock() }()
	}
	return amount, nil
}

func (c *memCredits) RefundCharge(_ context.Context, org uuid.UUID, key, _ string) (models.Micro, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.failRefund; err != nil {
		return 0, err
	}
	e := c.charges[key]
	if e == nil || e.org != org {
		return 0, nil
	}
	if !e.refunded {
		e.refunded = true
		c.balance[org] += e.amount
	}
	return e.amount, nil
}

func (c *memCredits) ChargeOf(_ context.Context, org uuid.UUID, key string) (*models.KeyedCharge, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.charges[key]
	if e == nil || e.org != org || e.amount <= 0 {
		return nil, nil
	}
	out := &models.KeyedCharge{Taken: e.amount}
	if e.refunded {
		out.Refunded = e.amount
	}
	return out, nil
}

// RefundStore follows the repository: once per refund key, never past what the debits under prefix took.
func (c *memCredits) RefundStore(_ context.Context, org uuid.UUID, prefix, spendKey, refundKey string, amount models.Micro, reason string) (models.Micro, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.failRefund; err != nil {
		return 0, err
	}
	if e := c.charges[refundKey]; e != nil {
		return -e.amount, nil
	}
	if spendKey != "" {
		e := c.charges[spendKey]
		if e == nil || e.org != org || e.amount <= 0 {
			return 0, nil
		}
		amount = e.amount
	}
	var left models.Micro
	for k, e := range c.charges {
		if e.org == org && strings.HasPrefix(k, prefix) {
			left += e.amount
		}
	}
	give := min(amount, left)
	if give <= 0 {
		return 0, nil
	}
	c.balance[org] += give
	c.charges[refundKey] = &ledgerEntry{org: org, amount: -give, reason: reason}
	c.order = append(c.order, refundKey)
	return give, nil
}

// net is what the workspace paid after refunds.
func (c *memCredits) net(org uuid.UUID) models.Micro {
	c.mu.Lock()
	defer c.mu.Unlock()
	var total models.Micro
	for _, e := range c.charges {
		if e.org == org && !e.refunded {
			total += e.amount
		}
	}
	return total
}

// memCards is the saved card: a charge is keyed like balance.ChargeStore, so a
// key seen before replays its outcome and never reaches the card again.
type memCards struct {
	mu      sync.Mutex
	credits *memCredits
	card    bool
	// decline refuses new charges with this code until the card is replaced.
	decline string
	topUps  []models.Micro
	keys    []string
	seen    map[string]error
	reload  map[uuid.UUID]*models.AISpendSettings
	// inFlight answers every new charge as another one still going through; fail answers a key's charge with an error.
	inFlight bool
	fail     map[string]error
}

func (m *memCards) ChargeStore(_ context.Context, org uuid.UUID, amount models.Micro, key, _ string, _ bool) (*balance.TopUpResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seen == nil {
		m.seen = map[string]error{}
	}
	if err, ok := m.seen[key]; ok {
		if err != nil {
			return nil, err
		}
		return &balance.TopUpResult{Status: "paid", Amount: amount}, nil
	}
	if !m.card {
		return nil, balance.ErrNoPaymentMethod
	}
	if m.inFlight {
		return nil, balance.ErrChargeInFlight
	}
	if err := m.fail[key]; err != nil {
		return nil, err
	}
	m.keys = append(m.keys, key)
	if m.decline != "" {
		err := &balance.DeclinedError{Code: m.decline}
		m.seen[key] = err
		return nil, err
	}
	m.seen[key] = nil
	m.topUps = append(m.topUps, amount)
	m.credits.mu.Lock()
	m.credits.balance[org] += amount
	m.credits.mu.Unlock()
	return &balance.TopUpResult{Status: "paid", Amount: amount}, nil
}

func (m *memCards) State(_ context.Context, org uuid.UUID) (*balance.State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := &balance.State{Settings: m.settings(org)}
	if m.card {
		st.PaymentMethod = &stripeapp.PaymentMethodSummary{Brand: "visa", Last4: "4242"}
	}
	return st, nil
}

func (m *memCards) settings(org uuid.UUID) *models.AISpendSettings {
	if m.reload == nil {
		m.reload = map[uuid.UUID]*models.AISpendSettings{}
	}
	if m.reload[org] == nil {
		m.reload[org] = credits.DefaultSpendSettings(org)
	}
	c := *m.reload[org]
	return &c
}

func (m *memCards) SetAutoReload(_ context.Context, org uuid.UUID, enabled bool, threshold, amount, limit models.Micro) (*models.AISpendSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if enabled && !m.card {
		return nil, balance.ErrNoPaymentMethod
	}
	cfg := m.settings(org)
	cfg.AutoReloadEnabled, cfg.AutoReloadThreshold, cfg.AutoReloadAmount, cfg.AutoReloadMonthlyMax = enabled, threshold, amount, limit
	m.reload[org] = cfg
	return cfg, nil
}

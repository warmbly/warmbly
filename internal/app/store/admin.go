package store

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
	"github.com/warmbly/warmbly/internal/repository"
)

// The operator's side of the store: instance-wide lists, one workspace's
// purchases, health, unit economics, and the actions that repair a purchase.
// Every action converges, so repeating one changes nothing further.

// Admin error codes.
const (
	CodeRefundExceedsCharge = "refund_exceeds_charge"
	CodeUnmetered           = "store_unmetered"
)

// Store mode names for the operator.
const (
	ModeOff       = "off"
	ModeLive      = "live"
	ModeSandbox   = "sandbox"
	ModeSimulator = "simulator"
)

const (
	metaLastWebhook = "store.last_webhook_at"
	metaAlerts      = "store.alerts"
	maxAlerts       = 20
	// webhookStampEvery bounds the writes recording when a webhook last arrived.
	webhookStampEvery = time.Minute
)

// adminStore is the instance-wide repository; the Postgres one implements it.
func (s *Service) adminStore() (repository.StoreAdminRepository, error) {
	if !s.Enabled() {
		return nil, errDisabled
	}
	a, ok := s.Repo.(repository.StoreAdminRepository)
	if !ok {
		return nil, errors.New("store: repository has no operator view")
	}
	return a, nil
}

// Mode is how the store reaches GraphKit: live, sandbox, the in-process simulator, or off.
func (s *Service) Mode() string {
	switch {
	case !s.Enabled():
		return ModeOff
	case isFake(s.api()):
		return ModeSimulator
	case s.simulating():
		return ModeSandbox
	}
	return ModeLive
}

func isFake(api graphkit.API) bool {
	_, ok := api.(*graphkit.Fake)
	return ok
}

// ---- views --------------------------------------------------------------

// AdminOrder is an order as the operator sees it.
type AdminOrder struct {
	ID               uuid.UUID   `json:"id"`
	OrganizationID   uuid.UUID   `json:"organization_id"`
	OrganizationName string      `json:"organization_name"`
	Status           string      `json:"status"`
	FailureCode      string      `json:"failure_code"`
	DueNowMicros     int64       `json:"due_now_micros"`
	ChargedMicros    int64       `json:"charged_micros"`
	RefundedMicros   int64       `json:"refunded_micros"`
	RefundedLines    []int       `json:"refunded_lines"`
	Placed           bool        `json:"placed"`
	UpstreamID       string      `json:"upstream_id"`
	Stuck            bool        `json:"stuck"`
	Steps            []Step      `json:"steps"`
	Lines            []QuoteLine `json:"lines"`
	CreatedBy        *uuid.UUID  `json:"created_by"`
	CreatedAt        time.Time   `json:"created_at"`
	UpdatedAt        time.Time   `json:"updated_at"`
	CompletedAt      *time.Time  `json:"completed_at"`
}

// AdminDomain is a domain as the operator sees it.
type AdminDomain struct {
	ID               uuid.UUID  `json:"id"`
	OrganizationID   uuid.UUID  `json:"organization_id"`
	OrganizationName string     `json:"organization_name"`
	Name             string     `json:"name"`
	Status           string     `json:"status"`
	Source           string     `json:"source"`
	Provider         string     `json:"provider"`
	ExpiresAt        *time.Time `json:"expires_at"`
	AutoRenew        bool       `json:"auto_renew"`
	RenewMicros      int64      `json:"renew_micros"`
	RenewalFailures  int        `json:"renewal_failures"`
	RenewalAttemptAt *time.Time `json:"renewal_attempt_at"`
	LastNotice       string     `json:"last_notice"`
	MailboxCount     int        `json:"mailbox_count"`
	TrackingHost     string     `json:"tracking_host"`
	UpstreamID       string     `json:"upstream_id"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// AdminMailbox is a purchased mailbox as the operator sees it.
type AdminMailbox struct {
	ID               uuid.UUID  `json:"id"`
	OrganizationID   uuid.UUID  `json:"organization_id"`
	OrganizationName string     `json:"organization_name"`
	DomainID         uuid.UUID  `json:"domain_id"`
	Email            string     `json:"email"`
	Provider         string     `json:"provider"`
	Status           string     `json:"status"`
	IsAdmin          bool       `json:"is_admin"`
	MonthlyMicros    int64      `json:"monthly_micros"`
	PaidThrough      time.Time  `json:"paid_through"`
	CancelsAt        *time.Time `json:"cancels_at"`
	EmailAccountID   *uuid.UUID `json:"email_account_id"`
	ConnectMethod    string     `json:"connect_method"`
	ConnectState     string     `json:"connect_state"`
	ConnectError     string     `json:"connect_error"`
	ConnectAttempts  int        `json:"connect_attempts"`
	BillingFailures  int        `json:"billing_failures"`
	PendingCharge    bool       `json:"pending_charge"`
	UpstreamID       string     `json:"upstream_id"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// stuckOrder mirrors the repository's stuck predicate for one row.
func stuckOrder(o *models.StoreOrder, now time.Time) bool {
	switch o.Status {
	case models.StoreOrderCharging:
		return now.Sub(o.UpdatedAt) > 30*time.Minute
	case models.StoreOrderProcessing:
		// GraphKit may wait 24 h on DNS and then 48 h on the mailbox vendor before it settles an order.
		return now.Sub(o.CreatedAt) > 72*time.Hour
	}
	return false
}

func (s *Service) adminOrderView(o *models.StoreOrder, names map[uuid.UUID]string) AdminOrder {
	var snap quoteSnapshot
	_ = json.Unmarshal(o.Quote, &snap)
	out := AdminOrder{ID: o.ID, OrganizationID: o.OrganizationID, OrganizationName: names[o.OrganizationID], Status: o.Status,
		FailureCode: o.FailureCode, DueNowMicros: int64(o.DueNowMicros), ChargedMicros: int64(o.ChargedMicros),
		RefundedMicros: int64(o.RefundedMicros), RefundedLines: []int{}, Placed: o.GraphKitOrderID != nil,
		Stuck: stuckOrder(o, s.now()), Lines: snap.Lines, CreatedBy: o.CreatedBy, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt,
		CompletedAt: o.CompletedAt}
	if out.Lines == nil {
		out.Lines = []QuoteLine{}
	}
	if o.GraphKitOrderID != nil {
		out.UpstreamID = *o.GraphKitOrderID
	}
	_ = json.Unmarshal(o.Steps, &out.Steps)
	if out.Steps == nil {
		out.Steps = []Step{}
	}
	prefix := o.ChargeKey + ":"
	for _, item := range o.RefundedItems {
		if rest, ok := strings.CutPrefix(item, prefix); ok {
			if i, err := strconv.Atoi(rest); err == nil {
				out.RefundedLines = append(out.RefundedLines, i)
			}
		}
	}
	slices.Sort(out.RefundedLines)
	return out
}

func adminDomainView(d *models.StoreDomain, names map[uuid.UUID]string) AdminDomain {
	return AdminDomain{ID: d.ID, OrganizationID: d.OrganizationID, OrganizationName: names[d.OrganizationID], Name: d.Name,
		Status: d.Status, Source: d.Source, Provider: d.Provider, ExpiresAt: d.ExpiresAt, AutoRenew: d.AutoRenew,
		RenewMicros: int64(d.RenewRetailMicros), RenewalFailures: d.RenewalFailures, RenewalAttemptAt: d.RenewalAttemptAt,
		LastNotice: d.LastNotice, MailboxCount: d.MailboxCount, TrackingHost: d.TrackingHost, UpstreamID: d.GraphKitDomainID,
		CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt}
}

func adminMailboxView(m *models.StoreMailbox, names map[uuid.UUID]string) AdminMailbox {
	return AdminMailbox{ID: m.ID, OrganizationID: m.OrganizationID, OrganizationName: names[m.OrganizationID], DomainID: m.DomainID,
		Email: m.Email, Provider: m.Provider, Status: m.Status, IsAdmin: m.IsAdmin, MonthlyMicros: int64(m.MonthlyRetailMicros),
		PaidThrough: m.PaidThrough, CancelsAt: m.CancelsAt, EmailAccountID: m.EmailAccountID, ConnectMethod: m.ConnectMethod,
		ConnectState: m.ConnectState, ConnectError: m.ConnectError, ConnectAttempts: m.ConnectAttempts,
		BillingFailures: m.BillingFailures, PendingCharge: m.PendingChargeKey != nil, UpstreamID: m.GraphKitMailboxID,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
}

func (s *Service) names(ctx context.Context, a repository.StoreAdminRepository, ids []uuid.UUID) map[uuid.UUID]string {
	seen := map[uuid.UUID]bool{}
	uniq := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			uniq = append(uniq, id)
		}
	}
	names, err := a.OrgNames(ctx, uniq)
	if err != nil {
		log.Warn().Err(err).Msg("store: reading workspace names failed")
		return map[uuid.UUID]string{}
	}
	return names
}

// ---- lists --------------------------------------------------------------

// AdminListOrders is every workspace's orders, newest first.
func (s *Service) AdminListOrders(ctx context.Context, f models.StoreAdminOrderFilter, cursor string, limit int) (*Page[AdminOrder], error) {
	a, err := s.adminStore()
	if err != nil {
		return nil, err
	}
	after, limit, err := pageArgs(cursor, limit)
	if err != nil {
		return nil, err
	}
	rows, err := a.AdminListOrders(ctx, f, after, limit+1, s.now())
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, o := range rows {
		ids = append(ids, o.OrganizationID)
	}
	names := s.names(ctx, a, ids)
	out := &Page[AdminOrder]{Data: []AdminOrder{}}
	for i := range rows {
		if i == limit {
			break
		}
		out.Data = append(out.Data, s.adminOrderView(&rows[i], names))
	}
	out.Pagination = nextPage(len(rows) > limit, func() models.StorePage {
		return models.StorePage{CreatedAt: rows[limit-1].CreatedAt, ID: rows[limit-1].ID}
	})
	return out, nil
}

// AdminListDomains is every workspace's domains, newest first.
func (s *Service) AdminListDomains(ctx context.Context, f models.StoreAdminDomainFilter, cursor string, limit int) (*Page[AdminDomain], error) {
	a, err := s.adminStore()
	if err != nil {
		return nil, err
	}
	after, limit, err := pageArgs(cursor, limit)
	if err != nil {
		return nil, err
	}
	rows, err := a.AdminListDomains(ctx, f, after, limit+1)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, d := range rows {
		ids = append(ids, d.OrganizationID)
	}
	names := s.names(ctx, a, ids)
	out := &Page[AdminDomain]{Data: []AdminDomain{}}
	for i := range rows {
		if i == limit {
			break
		}
		out.Data = append(out.Data, adminDomainView(&rows[i], names))
	}
	out.Pagination = nextPage(len(rows) > limit, func() models.StorePage {
		return models.StorePage{CreatedAt: rows[limit-1].CreatedAt, ID: rows[limit-1].ID}
	})
	return out, nil
}

// AdminListMailboxes is every workspace's purchased mailboxes, newest first.
func (s *Service) AdminListMailboxes(ctx context.Context, f models.StoreAdminMailboxFilter, cursor string, limit int) (*Page[AdminMailbox], error) {
	a, err := s.adminStore()
	if err != nil {
		return nil, err
	}
	after, limit, err := pageArgs(cursor, limit)
	if err != nil {
		return nil, err
	}
	rows, err := a.AdminListMailboxes(ctx, f, after, limit+1)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, m := range rows {
		ids = append(ids, m.OrganizationID)
	}
	names := s.names(ctx, a, ids)
	out := &Page[AdminMailbox]{Data: []AdminMailbox{}}
	for i := range rows {
		if i == limit {
			break
		}
		out.Data = append(out.Data, adminMailboxView(&rows[i], names))
	}
	out.Pagination = nextPage(len(rows) > limit, func() models.StorePage {
		return models.StorePage{CreatedAt: rows[limit-1].CreatedAt, ID: rows[limit-1].ID}
	})
	return out, nil
}

// ---- one row ------------------------------------------------------------

func (s *Service) adminOrderRow(ctx context.Context, id uuid.UUID) (repository.StoreAdminRepository, *models.StoreOrder, error) {
	a, err := s.adminStore()
	if err != nil {
		return nil, nil, err
	}
	org, ok, err := a.OrderOrg(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, errx.ErrNotFound
	}
	row, err := s.Repo.GetOrder(ctx, org, id)
	if err != nil {
		return nil, nil, err
	}
	if row == nil {
		return nil, nil, errx.ErrNotFound
	}
	return a, row, nil
}

func (s *Service) adminOrder(ctx context.Context, a repository.StoreAdminRepository, orgID, id uuid.UUID) (*AdminOrder, error) {
	row, err := s.Repo.GetOrder(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, errx.ErrNotFound
	}
	v := s.adminOrderView(row, s.names(ctx, a, []uuid.UUID{orgID}))
	return &v, nil
}

// AdminGetOrder is one order.
func (s *Service) AdminGetOrder(ctx context.Context, id uuid.UUID) (*AdminOrder, error) {
	a, row, err := s.adminOrderRow(ctx, id)
	if err != nil {
		return nil, err
	}
	v := s.adminOrderView(row, s.names(ctx, a, []uuid.UUID{row.OrganizationID}))
	return &v, nil
}

func (s *Service) adminDomainRow(ctx context.Context, id uuid.UUID) (repository.StoreAdminRepository, *models.StoreDomain, error) {
	a, err := s.adminStore()
	if err != nil {
		return nil, nil, err
	}
	org, ok, err := a.DomainOrg(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, errx.ErrNotFound
	}
	d, err := s.domain(ctx, org, id)
	return a, d, err
}

func (s *Service) adminDomain(ctx context.Context, a repository.StoreAdminRepository, orgID, id uuid.UUID) (*AdminDomain, error) {
	d, err := s.domain(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	v := adminDomainView(d, s.names(ctx, a, []uuid.UUID{orgID}))
	return &v, nil
}

func (s *Service) adminMailboxRow(ctx context.Context, id uuid.UUID) (repository.StoreAdminRepository, *models.StoreMailbox, error) {
	a, err := s.adminStore()
	if err != nil {
		return nil, nil, err
	}
	org, ok, err := a.MailboxOrg(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, errx.ErrNotFound
	}
	m, err := s.mailbox(ctx, org, id)
	return a, m, err
}

func (s *Service) adminMailbox(ctx context.Context, a repository.StoreAdminRepository, orgID, id uuid.UUID) (*AdminMailbox, error) {
	m, err := s.mailbox(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	v := adminMailboxView(m, s.names(ctx, a, []uuid.UUID{orgID}))
	return &v, nil
}

// ---- order actions ------------------------------------------------------

// AdminRefreshOrder reads an order from GraphKit again and applies it.
func (s *Service) AdminRefreshOrder(ctx context.Context, id uuid.UUID) (*AdminOrder, error) {
	a, row, err := s.adminOrderRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.GraphKitOrderID != nil {
		o, gerr := s.api().GetOrder(ctx, externalID(row.OrganizationID), *row.GraphKitOrderID)
		if gerr != nil {
			return nil, s.upstream(ctx, "order.refresh", gerr)
		}
		if err := s.applyOrder(ctx, row, o); err != nil {
			log.Warn().Err(err).Str("order_id", row.ID.String()).Msg("store: operator refresh did not apply fully")
		}
	}
	return s.adminOrder(ctx, a, row.OrganizationID, row.ID)
}

// AdminPlaceOrder places a charged order that never reached GraphKit, under its original key.
func (s *Service) AdminPlaceOrder(ctx context.Context, id uuid.UUID) (*AdminOrder, error) {
	a, row, err := s.adminOrderRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.GraphKitOrderID != nil || row.Status != models.StoreOrderCharging {
		return nil, errx.NewWithIdentifier(errx.Conflict, CodeStoreStateConflict, "This order was already placed.")
	}
	if s.metered() && row.ChargedMicros == 0 {
		return nil, errx.NewWithIdentifier(errx.Conflict, CodeStoreStateConflict, "Nothing was charged for this order, so there is nothing to place.")
	}
	var snap quoteSnapshot
	if err := json.Unmarshal(row.Quote, &snap); err != nil {
		return nil, err
	}
	if _, err := s.submit(ctx, row, snap); err != nil {
		return nil, err
	}
	return s.adminOrder(ctx, a, row.OrganizationID, row.ID)
}

// RefundRequest is one operator refund: a quote line, or an amount.
type RefundRequest struct {
	Line         *int   `json:"line"`
	AmountMicros int64  `json:"amount_micros"`
	Reason       string `json:"reason"`
	// Key is the request's Idempotency-Key; an amount refund needs one.
	Key string `json:"-"`
}

// RefundResult is the order after a refund and what this call gave back.
type RefundResult struct {
	Order          *AdminOrder `json:"order"`
	RefundedMicros int64       `json:"refunded_micros"`
}

// AdminRefund gives part of an order back to the workspace balance. A line is
// refunded at most once, together with any refund GraphKit triggered for it;
// an amount is refunded once per key and never past what was charged.
func (s *Service) AdminRefund(ctx context.Context, id uuid.UUID, in RefundRequest) (*RefundResult, error) {
	a, row, err := s.adminOrderRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if !s.metered() {
		return nil, errx.NewWithIdentifier(errx.Conflict, CodeUnmetered, "This instance charges nothing for the store, so there is nothing to refund.")
	}
	left := row.ChargedMicros - row.RefundedMicros
	var back models.Micro
	switch {
	case in.Line != nil:
		back, err = s.refundLine(ctx, row, *in.Line, left)
	case in.AmountMicros > 0:
		back, err = s.refundAmount(ctx, row, in, left)
	default:
		return nil, errx.NewWithIdentifier(errx.BadRequest, "invalid_body", "Name a line or a positive amount to refund.")
	}
	if err != nil {
		return nil, err
	}
	if back > 0 {
		s.notify(ctx, row.OrganizationID, "A store refund was added to your balance",
			usd(int64(back))+" from a domains and mailboxes order was refunded to your balance.",
			"/app/emails/buy/orders/"+row.ID.String(), "store_refund_"+row.ID.String())
		s.publish(ctx, row.OrganizationID, pubsub.StoreEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventStoreOrderUpdated}, ID: row.ID.String(), Status: row.Status})
	}
	v, err := s.adminOrder(ctx, a, row.OrganizationID, row.ID)
	if err != nil {
		return nil, err
	}
	return &RefundResult{Order: v, RefundedMicros: int64(back)}, nil
}

func (s *Service) refundLine(ctx context.Context, row *models.StoreOrder, i int, left models.Micro) (models.Micro, error) {
	var snap quoteSnapshot
	if err := json.Unmarshal(row.Quote, &snap); err != nil {
		return 0, err
	}
	if i < 0 || i >= len(snap.Lines) || snap.Lines[i].AmountMicros <= 0 {
		return 0, errx.NewWithIdentifier(errx.BadRequest, "invalid_line", "That order has no charged line with this number.")
	}
	key := lineKey(row.ChargeKey, i)
	if slices.Contains(row.RefundedItems, key) || left <= 0 {
		return 0, nil
	}
	// Capped on the ledger by what the order has left, whatever was refunded before and however.
	got, err := s.refundOrderLine(ctx, row, i, "Refund · "+lineReason(snap.Lines[i], row.CreatedAt))
	if err != nil {
		return 0, err
	}
	got = min(got, left)
	won, err := s.Repo.AddOrderRefund(ctx, row.OrganizationID, row.ID, key, got)
	if err != nil || !won {
		return 0, err
	}
	return got, nil
}

func (s *Service) refundAmount(ctx context.Context, row *models.StoreOrder, in RefundRequest, left models.Micro) (models.Micro, error) {
	if strings.TrimSpace(in.Key) == "" {
		return 0, errx.NewWithIdentifier(errx.BadRequest, "idempotency_key_required", "Send an Idempotency-Key header with an amount refund.")
	}
	item := "admin:" + in.Key
	if slices.Contains(row.RefundedItems, item) {
		return 0, nil
	}
	if models.Micro(in.AmountMicros) > left {
		return 0, errx.NewWithIdentifier(errx.Unprocessable, CodeRefundExceedsCharge, "That is more than is left to refund on this order ("+usd(int64(max(left, 0)))+").")
	}
	amount, err := s.Credits.RefundStore(context.WithoutCancel(ctx), row.OrganizationID, orderPrefix(row), "",
		row.ChargeKey+":admin-refund:"+in.Key, models.Micro(in.AmountMicros), "Refund · Domains and mailboxes order")
	if err != nil {
		return 0, err
	}
	won, err := s.Repo.AddOrderRefund(ctx, row.OrganizationID, row.ID, item, amount)
	if err != nil || !won {
		return 0, err
	}
	return amount, nil
}

// ---- mailbox actions ----------------------------------------------------

// AdminRefreshMailbox reads a mailbox from GraphKit again, and connects it when it became ready.
func (s *Service) AdminRefreshMailbox(ctx context.Context, id uuid.UUID) (*AdminMailbox, error) {
	a, row, err := s.adminMailboxRow(ctx, id)
	if err != nil {
		return nil, err
	}
	m, gerr := s.api().GetMailbox(ctx, row.GraphKitMailboxID)
	if gerr != nil {
		return nil, s.upstream(ctx, "mailbox.refresh", gerr)
	}
	if fresh, err := s.upsertMailbox(ctx, row.OrganizationID, nil, m, 0); err == nil && fresh != nil {
		s.connectLater(ctx, fresh, m)
	}
	return s.adminMailbox(ctx, a, row.OrganizationID, row.ID)
}

// AdminRetryConnect clears a failed connect's attempts and connects the mailbox now.
func (s *Service) AdminRetryConnect(ctx context.Context, id uuid.UUID) (*AdminMailbox, error) {
	a, row, err := s.adminMailboxRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.EmailAccountID != nil {
		return s.adminMailbox(ctx, a, row.OrganizationID, row.ID)
	}
	if _, err := a.ResetConnect(ctx, row.OrganizationID, row.ID); err != nil {
		return nil, err
	}
	m, gerr := s.api().GetMailbox(ctx, row.GraphKitMailboxID)
	if gerr != nil {
		return nil, s.upstream(ctx, "mailbox.refresh", gerr)
	}
	fresh, err := s.upsertMailbox(ctx, row.OrganizationID, nil, m, 0)
	if err != nil {
		return nil, err
	}
	if fresh != nil && connectable(fresh, m) {
		cctx, cancel := context.WithTimeout(ctx, connectBudget)
		s.connect(cctx, fresh, m)
		cancel()
	}
	return s.adminMailbox(ctx, a, row.OrganizationID, row.ID)
}

// AdminCancelMailbox cancels a mailbox at the end of its paid month, as the workspace would.
func (s *Service) AdminCancelMailbox(ctx context.Context, id uuid.UUID) (*AdminMailbox, error) {
	a, row, err := s.adminMailboxRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.CancelsAt == nil {
		if _, err := s.CancelMailbox(ctx, row.OrganizationID, row.ID); err != nil {
			return nil, err
		}
	}
	return s.adminMailbox(ctx, a, row.OrganizationID, row.ID)
}

// AdminResumeMailbox undoes a pending cancellation.
func (s *Service) AdminResumeMailbox(ctx context.Context, id uuid.UUID) (*AdminMailbox, error) {
	a, row, err := s.adminMailboxRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, err := s.ResumeMailbox(ctx, row.OrganizationID, row.ID); err != nil {
		return nil, err
	}
	return s.adminMailbox(ctx, a, row.OrganizationID, row.ID)
}

// ---- domain actions -----------------------------------------------------

// AdminRefreshDomain reads a domain from GraphKit again.
func (s *Service) AdminRefreshDomain(ctx context.Context, id uuid.UUID) (*AdminDomain, error) {
	a, d, err := s.adminDomainRow(ctx, id)
	if err != nil {
		return nil, err
	}
	live, gerr := s.api().GetDomain(ctx, d.GraphKitDomainID)
	if gerr != nil {
		return nil, s.upstream(ctx, "domain.refresh", gerr)
	}
	if _, err := s.upsertDomain(ctx, d.OrganizationID, nil, live); err != nil {
		return nil, err
	}
	return s.adminDomain(ctx, a, d.OrganizationID, d.ID)
}

// AdminRenewDomain renews a domain now from the workspace balance, as the
// billing loop would: no card is charged, and a refusal refunds.
func (s *Service) AdminRenewDomain(ctx context.Context, id uuid.UUID) (*AdminDomain, error) {
	a, d, err := s.adminDomainRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, err := s.renew(ctx, d, false); err != nil {
		return nil, err
	}
	return s.adminDomain(ctx, a, d.OrganizationID, d.ID)
}

// AdminSetAutoRenew turns a purchased domain's auto-renew on or off.
func (s *Service) AdminSetAutoRenew(ctx context.Context, id uuid.UUID, on bool) (*AdminDomain, error) {
	a, d, err := s.adminDomainRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if !renewable(d) {
		return nil, registeredElsewhere()
	}
	if d.AutoRenew != on {
		if err := s.Repo.SetDomainAutoRenew(ctx, d.OrganizationID, d.ID, on); err != nil {
			return nil, err
		}
		s.publish(ctx, d.OrganizationID, pubsub.StoreEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventStoreDomainUpdated}, ID: d.ID.String(), Status: d.Status})
	}
	return s.adminDomain(ctx, a, d.OrganizationID, d.ID)
}

// ---- one workspace ------------------------------------------------------

// AdminWorkspace is one workspace's store account and everything it bought.
type AdminWorkspace struct {
	Organization struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	} `json:"organization"`
	Account struct {
		Exists        bool       `json:"exists"`
		CreatedAt     *time.Time `json:"created_at"`
		HasRegistrant bool       `json:"has_registrant"`
	} `json:"account"`
	PlanActive         bool           `json:"plan_active"`
	ManagedByPartner   bool           `json:"managed_by_partner"`
	BalanceMicros      *int64         `json:"balance_micros"`
	MonthlySpendMicros int64          `json:"monthly_spend_micros"`
	Orders             []AdminOrder   `json:"orders"`
	Domains            []AdminDomain  `json:"domains"`
	Mailboxes          []AdminMailbox `json:"mailboxes"`
}

// AdminWorkspace is one workspace's purchases for the operator.
func (s *Service) AdminWorkspace(ctx context.Context, orgID uuid.UUID) (*AdminWorkspace, error) {
	a, err := s.adminStore()
	if err != nil {
		return nil, err
	}
	names := s.names(ctx, a, []uuid.UUID{orgID})
	name, ok := names[orgID]
	if !ok {
		return nil, errx.ErrNotFound
	}
	out := &AdminWorkspace{PlanActive: true, Orders: []AdminOrder{}, Domains: []AdminDomain{}, Mailboxes: []AdminMailbox{}}
	out.Organization.ID, out.Organization.Name = orgID, name
	acct, err := s.Repo.GetAccount(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if acct != nil {
		out.Account.Exists = true
		created := acct.CreatedAt
		out.Account.CreatedAt = &created
		out.Account.HasRegistrant = len(acct.Registrant) > 0 && string(acct.Registrant) != "null"
	}
	out.ManagedByPartner = s.managed(ctx, orgID)
	if active, err := s.planActive(ctx, orgID); err == nil {
		out.PlanActive = active
	}
	if s.metered() {
		if b, xerr := s.Credits.GetBalance(ctx, orgID); xerr == nil {
			v := int64(b)
			out.BalanceMicros = &v
		}
	}
	if sums, err := a.OrgSummaries(ctx, []uuid.UUID{orgID}, s.now()); err == nil && len(sums) == 1 {
		out.MonthlySpendMicros = int64(sums[0].MonthlyMicros)
	}
	org := orgID
	orders, err := a.AdminListOrders(ctx, models.StoreAdminOrderFilter{OrgID: &org}, nil, 50, s.now())
	if err != nil {
		return nil, err
	}
	for i := range orders {
		out.Orders = append(out.Orders, s.adminOrderView(&orders[i], names))
	}
	domains, err := a.AdminListDomains(ctx, models.StoreAdminDomainFilter{OrgID: &org}, nil, 200)
	if err != nil {
		return nil, err
	}
	for i := range domains {
		out.Domains = append(out.Domains, adminDomainView(&domains[i], names))
	}
	boxes, err := a.AdminListMailboxes(ctx, models.StoreAdminMailboxFilter{OrgID: &org}, nil, 500)
	if err != nil {
		return nil, err
	}
	for i := range boxes {
		out.Mailboxes = append(out.Mailboxes, adminMailboxView(&boxes[i], names))
	}
	return out, nil
}

// ---- health -------------------------------------------------------------

// LoopState is one background loop's last run.
type LoopState struct {
	LastStartedAt  *time.Time `json:"last_started_at"`
	LastFinishedAt *time.Time `json:"last_finished_at"`
	LastStatus     string     `json:"last_status"`
	LastError      string     `json:"last_error"`
	// LagSeconds is how long ago the loop last finished.
	LagSeconds int64 `json:"lag_seconds"`
}

// Alert is one store.unavailable operator alert.
type Alert struct {
	Code  string    `json:"code"`
	Title string    `json:"title"`
	At    time.Time `json:"at"`
}

// Health is the store at a glance for the operator.
type Health struct {
	Enabled             bool                     `json:"enabled"`
	Mode                string                   `json:"mode"`
	Metered             bool                     `json:"metered"`
	PartnerBalance      *graphkit.PartnerBalance `json:"partner_balance"`
	PartnerBalanceError string                   `json:"partner_balance_error"`
	LastWebhookAt       *time.Time               `json:"last_webhook_at"`
	EventsCursorSet     bool                     `json:"events_cursor_set"`
	Reconciler          *LoopState               `json:"reconciler"`
	Billing             *LoopState               `json:"billing"`
	Counts              models.StoreAdminCounts  `json:"counts"`
	Alerts              []Alert                  `json:"alerts"`
	// Uncollected are mailboxes whose GraphKit period started without Warmbly having collected it. Should stay empty.
	Uncollected []UncollectedMailbox `json:"uncollected"`
}

// Loop names, as jobrun records them.
const (
	LoopReconcile = "store_reconcile"
	LoopBilling   = "store_billing"
)

// AdminHealth reads the partner balance live and the store's counts; the caller adds the loops.
func (s *Service) AdminHealth(ctx context.Context) (*Health, error) {
	out := &Health{Mode: s.Mode(), Alerts: []Alert{}}
	if !s.Enabled() {
		return out, nil
	}
	out.Enabled, out.Metered = true, s.metered()
	a, err := s.adminStore()
	if err != nil {
		return nil, err
	}
	bctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	bal, gerr := s.api().Balance(bctx)
	cancel()
	if gerr != nil {
		logUpstream("partner.balance", gerr)
		s.alertIfAccountLevel(gerr)
		out.PartnerBalanceError = CodeUnavailable
	} else {
		out.PartnerBalance = bal
	}
	counts, err := a.AdminCounts(ctx, s.now())
	if err != nil {
		return nil, err
	}
	out.Counts = *counts
	if raw, err := a.GetMeta(ctx, metaLastWebhook); err == nil && len(raw) > 0 {
		var at time.Time
		if json.Unmarshal(raw, &at) == nil {
			out.LastWebhookAt = &at
		}
	}
	if cursor, err := s.Repo.GetEventsCursor(ctx); err == nil {
		out.EventsCursorSet = cursor != ""
	}
	out.Alerts = s.recentAlerts(ctx, a)
	if out.Uncollected, err = s.Uncollected(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

var webhookStamp struct {
	mu sync.Mutex
	at time.Time
}

// NoteWebhook records that a verified webhook arrived, at most once a minute per process.
func (s *Service) NoteWebhook(ctx context.Context) {
	a, err := s.adminStore()
	if err != nil {
		return
	}
	now := s.now()
	webhookStamp.mu.Lock()
	if now.Sub(webhookStamp.at) < webhookStampEvery {
		webhookStamp.mu.Unlock()
		return
	}
	webhookStamp.at = now
	webhookStamp.mu.Unlock()
	raw, _ := json.Marshal(now)
	if err := a.SetMeta(context.WithoutCancel(ctx), metaLastWebhook, raw); err != nil {
		log.Warn().Err(err).Msg("store: recording the last webhook failed")
	}
}

// recordAlert keeps the latest store.unavailable alerts for the admin panel.
func (s *Service) recordAlert(code, title string) {
	a, err := s.adminStore()
	if err != nil {
		return
	}
	at := s.now()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		list := s.recentAlerts(ctx, a)
		list = append([]Alert{{Code: code, Title: title, At: at}}, list...)
		if len(list) > maxAlerts {
			list = list[:maxAlerts]
		}
		raw, _ := json.Marshal(list)
		if err := a.SetMeta(ctx, metaAlerts, raw); err != nil {
			log.Warn().Err(err).Msg("store: recording an operator alert failed")
		}
	}()
}

func (s *Service) recentAlerts(ctx context.Context, a repository.StoreAdminRepository) []Alert {
	out := []Alert{}
	raw, err := a.GetMeta(ctx, metaAlerts)
	if err != nil || len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

// ---- economics ----------------------------------------------------------

// MailboxEconomics is one provider's mailbox cost and price.
type MailboxEconomics struct {
	Provider                string     `json:"provider"`
	WholesaleMonthlyMicros  int64      `json:"wholesale_monthly_micros"`
	RetailMonthlyMicros     int64      `json:"retail_monthly_micros"`
	MarginMicros            int64      `json:"margin_micros"`
	ScheduledWholesaleMicro *int64     `json:"scheduled_wholesale_monthly_micros"`
	ScheduledFrom           *time.Time `json:"scheduled_from"`
	LiveCount               int        `json:"live_count"`
}

// DomainEconomics is one TLD's cost and price.
type DomainEconomics struct {
	TLD                     string `json:"tld"`
	MinYears                int    `json:"min_years"`
	WholesaleRegisterMicros int64  `json:"wholesale_register_micros"`
	RetailRegisterMicros    int64  `json:"retail_register_micros"`
	WholesaleRenewMicros    int64  `json:"wholesale_renew_micros"`
	RetailRenewMicros       int64  `json:"retail_renew_micros"`
}

// EconomicsTotals is what the live mailboxes bill and cost a month.
type EconomicsTotals struct {
	LiveMailboxes          int   `json:"live_mailboxes"`
	MonthlyRetailMicros    int64 `json:"monthly_retail_micros"`
	MonthlyWholesaleMicros int64 `json:"monthly_wholesale_micros"`
	MonthlyMarginMicros    int64 `json:"monthly_margin_micros"`
}

// Economics is GraphKit's cost against this instance's retail.
type Economics struct {
	Currency  string             `json:"currency"`
	UpdatedAt time.Time          `json:"updated_at"`
	Mailboxes []MailboxEconomics `json:"mailboxes"`
	Domains   []DomainEconomics  `json:"domains"`
	Totals    EconomicsTotals    `json:"totals"`
}

// AdminEconomics reads GraphKit's live prices and sets them against retail and the live mailboxes.
func (s *Service) AdminEconomics(ctx context.Context) (*Economics, error) {
	a, err := s.adminStore()
	if err != nil {
		return nil, err
	}
	p, gerr := s.api().Pricing(ctx)
	if gerr != nil {
		return nil, s.upstream(ctx, "pricing", gerr)
	}
	live, err := a.LiveMailboxesByProvider(ctx)
	if err != nil {
		return nil, err
	}
	r := s.retail(ctx)
	out := &Economics{Currency: p.Currency, UpdatedAt: p.UpdatedAt, Mailboxes: []MailboxEconomics{}, Domains: []DomainEconomics{}}
	wholesale := map[string]int64{}
	for _, m := range p.Mailboxes {
		e := MailboxEconomics{Provider: m.Provider, WholesaleMonthlyMicros: m.MonthlyMicros, RetailMonthlyMicros: r.MailboxMonthly()}
		e.MarginMicros = e.RetailMonthlyMicros - e.WholesaleMonthlyMicros
		if m.Scheduled != nil {
			v, at := m.Scheduled.MonthlyMicros, m.Scheduled.EffectiveFrom
			e.ScheduledWholesaleMicro, e.ScheduledFrom = &v, &at
		}
		for _, l := range live {
			if l.Provider == m.Provider {
				e.LiveCount = l.Count
			}
		}
		wholesale[m.Provider] = m.MonthlyMicros
		out.Mailboxes = append(out.Mailboxes, e)
	}
	for _, l := range live {
		out.Totals.LiveMailboxes += l.Count
		out.Totals.MonthlyRetailMicros += int64(l.MonthlyMicros)
		out.Totals.MonthlyWholesaleMicros += int64(l.Count) * wholesale[l.Provider]
	}
	out.Totals.MonthlyMarginMicros = out.Totals.MonthlyRetailMicros - out.Totals.MonthlyWholesaleMicros
	for _, d := range p.Domains {
		out.Domains = append(out.Domains, DomainEconomics{TLD: d.TLD, MinYears: max(d.MinYears, 1),
			WholesaleRegisterMicros: d.RegisterMicros, RetailRegisterMicros: retailDomain(r, d.RegisterMicros),
			WholesaleRenewMicros: d.RenewMicros, RetailRenewMicros: retailDomain(r, d.RenewMicros)})
	}
	return out, nil
}

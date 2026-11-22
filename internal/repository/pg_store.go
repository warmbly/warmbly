package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// StoreEventsCursorKey is the admin_settings row holding the last GraphKit event applied.
const StoreEventsCursorKey = "store.events_cursor"

// StoreRepository holds the domains and mailboxes workspaces buy. Every read
// and write a workspace drives is scoped by organization_id; only the sweeps
// read across workspaces, and each row they return carries its own.
type StoreRepository interface {
	GetAccount(ctx context.Context, orgID uuid.UUID) (*models.StoreAccount, error)
	// UpsertAccount records the GraphKit account; a nil registrant keeps the stored one.
	UpsertAccount(ctx context.Context, orgID uuid.UUID, graphkitAccountID string, registrant json.RawMessage) error
	// SetIdentityDigest records the connection identities last set on the workspace's account.
	SetIdentityDigest(ctx context.Context, orgID uuid.UUID, digest string) error
	// AccountOrgs is every workspace with a store account.
	AccountOrgs(ctx context.Context, limit int) ([]uuid.UUID, error)
	// SetPlanNotice stamps (at non-nil) or clears the plan-ended notice; it reports whether the row changed.
	SetPlanNotice(ctx context.Context, orgID uuid.UUID, at *time.Time) (bool, error)

	CreateQuote(ctx context.Context, q *models.StoreQuote) error
	GetQuote(ctx context.Context, orgID, id uuid.UUID) (*models.StoreQuote, error)
	// ReplaceQuote records by as the quote an expired quote was priced again as, once: it returns
	// the replacement on record (by, or one recorded first), or ordered when the quote already has an order.
	ReplaceQuote(ctx context.Context, orgID, id, by uuid.UUID) (replacement uuid.UUID, ordered bool, err error)
	PruneQuotes(ctx context.Context, before time.Time) (int64, error)

	// CreateOrder inserts o, or returns the workspace's order already started for the same quote.
	// ErrStoreQuoteReplaced refuses a quote already priced again as another.
	CreateOrder(ctx context.Context, o *models.StoreOrder) (*models.StoreOrder, bool, error)
	GetOrder(ctx context.Context, orgID, id uuid.UUID) (*models.StoreOrder, error)
	GetOrderByGraphKitID(ctx context.Context, orgID uuid.UUID, graphkitID string) (*models.StoreOrder, error)
	GetOrderByQuote(ctx context.Context, orgID, quoteID uuid.UUID) (*models.StoreOrder, error)
	ListOrders(ctx context.Context, orgID uuid.UUID, after *models.StorePage, limit int) ([]models.StoreOrder, error)
	// DeleteUnchargedOrder removes an order nothing was charged for, so the buyer can retry.
	DeleteUnchargedOrder(ctx context.Context, orgID, id uuid.UUID) error
	MarkOrderCharged(ctx context.Context, orgID, id uuid.UUID, charged models.Micro) error
	MarkOrderPlaced(ctx context.Context, orgID, id uuid.UUID, graphkitID, status string, steps json.RawMessage) error
	// UpdateOrderState stores GraphKit's view of the order and returns the status it replaced.
	UpdateOrderState(ctx context.Context, orgID, id uuid.UUID, status string, steps json.RawMessage, completedAt *time.Time) (string, error)
	// MarkOrderFailed fails an order; refundPending keeps it for the reconciler to finish its refund.
	MarkOrderFailed(ctx context.Context, orgID, id uuid.UUID, code string, refunded models.Micro, refundPending bool) error
	// MarkOrderRefunded records a pending refund as done.
	MarkOrderRefunded(ctx context.Context, orgID, id uuid.UUID, refunded models.Micro) error
	// RefundPendingOrders are orders whose refund has not gone through yet, across workspaces.
	RefundPendingOrders(ctx context.Context, before time.Time, limit int) ([]models.StoreOrder, error)
	// StrandedOrders are orders still charging with no charge recorded and untouched since before, across workspaces.
	StrandedOrders(ctx context.Context, before time.Time, limit int) ([]models.StoreOrder, error)
	// AddOrderRefund records one item's refund once; false when it was already recorded.
	AddOrderRefund(ctx context.Context, orgID, id uuid.UUID, itemKey string, amount models.Micro) (bool, error)
	// ListOpenOrders is every order still in flight and untouched since before, across workspaces;
	// uncharged adds orders that took no money, for an instance that charges nothing.
	ListOpenOrders(ctx context.Context, before time.Time, limit int, uncharged bool) ([]models.StoreOrder, error)

	UpsertDomain(ctx context.Context, d *models.StoreDomain) (*models.StoreDomain, error)
	GetDomain(ctx context.Context, orgID, id uuid.UUID) (*models.StoreDomain, error)
	GetDomainByGraphKitID(ctx context.Context, orgID uuid.UUID, graphkitID string) (*models.StoreDomain, error)
	ListDomains(ctx context.Context, orgID uuid.UUID, after *models.StorePage, limit int) ([]models.StoreDomain, error)
	ListDomainsByOrder(ctx context.Context, orgID, orderID uuid.UUID) ([]models.StoreDomain, error)
	SetDomainAutoRenew(ctx context.Context, orgID, id uuid.UUID, on bool) error
	SetDomainForwarding(ctx context.Context, orgID, id uuid.UUID, url string) error
	SetDomainTrackingHost(ctx context.Context, orgID, id uuid.UUID, host string) error
	// DeleteConnectedDomain removes a connected domain with no live mailboxes; false when it kept it.
	DeleteConnectedDomain(ctx context.Context, orgID, id uuid.UUID) (bool, error)
	GetDomainByName(ctx context.Context, orgID uuid.UUID, name string) (*models.StoreDomain, error)
	// DomainsNeedingTracking are active domains with a connected mailbox and no tracking host yet.
	DomainsNeedingTracking(ctx context.Context, limit int) ([]models.StoreDomain, error)
	// RecordDomainRenewal and RecordDomainRenewalFailure also end the renewal charge in flight.
	RecordDomainRenewal(ctx context.Context, orgID, id uuid.UUID, expiresAt time.Time) error
	RecordDomainRenewalFailure(ctx context.Context, orgID, id uuid.UUID) error
	// SetRenewalCharge marks a renewal charged and undecided (key), or no longer in flight (nil).
	SetRenewalCharge(ctx context.Context, orgID, id uuid.UUID, key *string) error
	// DueDomainRenewals are auto-renewing domains expiring before horizon, not attempted since
	// attemptedBefore, whose upcoming-renewal notice went out before noticedBefore or that expire before
	// urgent, and every domain with a renewal charge in flight.
	DueDomainRenewals(ctx context.Context, horizon, attemptedBefore, noticedBefore, urgent time.Time, limit int) ([]models.StoreDomain, error)
	// DomainsExpiringBefore are live domains expiring before horizon, across workspaces.
	DomainsExpiringBefore(ctx context.Context, horizon time.Time, limit int) ([]models.StoreDomain, error)
	// SetDomainNotice records a notice once per key, with the renewal price it stated;
	// upcoming also stamps renewal_notice_at, which the renewal charge waits on.
	SetDomainNotice(ctx context.Context, id uuid.UUID, notice string, statedRenew models.Micro, upcoming bool) (bool, error)

	UpsertMailbox(ctx context.Context, m *models.StoreMailbox) (*models.StoreMailbox, error)
	GetMailbox(ctx context.Context, orgID, id uuid.UUID) (*models.StoreMailbox, error)
	GetMailboxByGraphKitID(ctx context.Context, orgID uuid.UUID, graphkitID string) (*models.StoreMailbox, error)
	GetMailboxByEmail(ctx context.Context, orgID uuid.UUID, email string) (*models.StoreMailbox, error)
	ListMailboxes(ctx context.Context, orgID uuid.UUID, after *models.StorePage, limit int) ([]models.StoreMailbox, error)
	ListMailboxesByOrder(ctx context.Context, orgID, orderID uuid.UUID) ([]models.StoreMailbox, error)
	SetMailboxNames(ctx context.Context, orgID, id uuid.UUID, first, last string) error
	SetMailboxCancel(ctx context.Context, orgID, id uuid.UUID, status string, cancelsAt *time.Time) error
	// GetMailboxByAccount is the purchase behind one of the workspace's connected mailboxes, nil for any other.
	GetMailboxByAccount(ctx context.Context, orgID, emailAccountID uuid.UUID) (*models.StoreMailbox, error)
	// MarkMailboxEnded stamps cancelled_at on a cancelled or failed mailbox once; false when it already was.
	MarkMailboxEnded(ctx context.Context, orgID, id uuid.UUID, at time.Time) (bool, error)
	// UnmarkedEndedMailboxes are cancelled or failed mailboxes whose Warmbly mailbox is not switched off yet.
	UnmarkedEndedMailboxes(ctx context.Context, limit int) ([]models.StoreMailbox, error)
	// ClaimConnect takes a mailbox for one connect attempt; false when it is connected,
	// another attempt holds it, or it failed too often or too recently.
	ClaimConnect(ctx context.Context, orgID, id uuid.UUID, now time.Time) (bool, error)
	FinishConnect(ctx context.Context, orgID, id uuid.UUID, emailAccountID *uuid.UUID, state, errMsg string) error
	// AwaitConnectAction parks a connect on a step only a person can take, and why; it is claimable again half an hour after its last try.
	AwaitConnectAction(ctx context.Context, orgID, id uuid.UUID, action, reason string) error
	// DomainActionReason is why a live mailbox on the domain waits on action, "" when none does or none knows why.
	DomainActionReason(ctx context.Context, orgID, domainID uuid.UUID, action string) (string, error)
	// WakeConnectAction makes a workspace's connects waiting on a step for these domains claimable now, returning them.
	WakeConnectAction(ctx context.Context, orgID uuid.UUID, action string, domains []string) ([]models.StoreMailbox, error)
	// PendingConnects are active mailboxes a connect attempt could claim, across workspaces.
	PendingConnects(ctx context.Context, now time.Time, limit int) ([]models.StoreMailbox, error)

	// SetBillAnchor records the workspace's billing day and its next billing date.
	SetBillAnchor(ctx context.Context, orgID uuid.UUID, day int, next time.Time) error
	// SetReloadState records the store's decision about the workspace's automatic reload and the values it set.
	SetReloadState(ctx context.Context, orgID uuid.UUID, managed bool, threshold, amount models.Micro) error
	// MailboxesDue are the workspace's billable mailboxes whose next GraphKit period starts before before.
	MailboxesDue(ctx context.Context, orgID uuid.UUID, before, now time.Time) ([]models.StoreMailbox, error)
	// UnpaidMailboxes are the workspace's billable mailboxes whose last month did not go through.
	// Only those whose last attempt was before attemptedBefore, so a failed month is tried once a day;
	// withPending adds those whose retry is still being charged.
	UnpaidMailboxes(ctx context.Context, orgID uuid.UUID, now, attemptedBefore time.Time, withPending bool) ([]models.StoreMailbox, error)
	// SetUnpaidCancel records a cancellation sent to GraphKit because the month went unpaid.
	SetUnpaidCancel(ctx context.Context, orgID, id uuid.UUID, status string, cancelsAt *time.Time) error
	// MarkMonthPaid moves one mailbox a month on with no charge of its own, on an instance that meters nothing.
	MarkMonthPaid(ctx context.Context, orgID, id uuid.UUID) error
	// RefundMonth moves a mailbox back the month paidKey collected and ends an unpaid cancellation, once
	// per charge; false when that charge no longer holds the month.
	RefundMonth(ctx context.Context, orgID, id uuid.UUID, paidKey string) (bool, error)
	SetMailboxRetail(ctx context.Context, orgID, id uuid.UUID, monthly models.Micro) error
	// MailboxesPastPeriod are live mailboxes whose recorded GraphKit period has ended, to read again.
	MailboxesPastPeriod(ctx context.Context, now time.Time, limit int) ([]models.StoreMailbox, error)
	// UncollectedMailboxes are live mailboxes whose GraphKit period runs past what Warmbly collected.
	UncollectedMailboxes(ctx context.Context, limit int) ([]models.StoreMailbox, error)
	// ReserveMailboxCharge stamps key and the month's price on the listed mailboxes that are in no
	// batch and still paid through what the listing read, so a month is never reserved twice.
	ReserveMailboxCharge(ctx context.Context, orgID uuid.UUID, rows []models.StoreMailbox, key string) (int64, error)
	// ReserveRun is a billing run: it moves the workspace's next billing date from anchor to next and
	// reserves the run's mailboxes under key in one write, so a run happens once and leaves none behind.
	// ran is false when another run already moved the date.
	ReserveRun(ctx context.Context, orgID uuid.UUID, rows []models.StoreMailbox, key string, anchor, next time.Time) (ran bool, err error)
	MailboxesByChargeKey(ctx context.Context, orgID uuid.UUID, key string) ([]models.StoreMailbox, error)
	// PendingChargeKeys are the workspace's batches reserved before reservedBefore and not finished.
	PendingChargeKeys(ctx context.Context, orgID uuid.UUID, reservedBefore time.Time) ([]string, error)
	// SettleMailboxCharge moves the batch's mailboxes whose next month will start a calendar month on,
	// recording key as what paid it, and clears the key; a mailbox cancelled since keeps it.
	SettleMailboxCharge(ctx context.Context, orgID uuid.UUID, key string, now time.Time) (int64, error)
	// DetachMailboxCharge takes one mailbox out of a batch without counting a failure.
	DetachMailboxCharge(ctx context.Context, orgID uuid.UUID, key string, id uuid.UUID) error
	// ReleaseMailboxCharge clears a batch nothing was charged for and counts the failure.
	ReleaseMailboxCharge(ctx context.Context, orgID uuid.UUID, key string) error
	// ExtendUnmetered moves due mailboxes on with no charge, on an instance that meters nothing.
	ExtendUnmetered(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) error
	SetMailboxNotice(ctx context.Context, id uuid.UUID, notice string) (bool, error)
	// ClearBillingRetry makes a workspace's unpaid mailbox months and refused renewals due again now.
	ClearBillingRetry(ctx context.Context, orgID uuid.UUID) error
	// OrgsWithLiveMailboxes are the workspaces holding a purchased mailbox that is not cancelled or failed.
	OrgsWithLiveMailboxes(ctx context.Context, limit int) ([]uuid.UUID, error)
	// OrgsWithUnsettledCancels are the workspaces holding an ended mailbox whose unpaid cancellation still holds a paid month.
	OrgsWithUnsettledCancels(ctx context.Context, limit int) ([]uuid.UUID, error)
	// OpenMailboxes is every mailbox of the workspace not cancelled or failed, plus an ended one still holding an unpaid cancellation, newest first.
	OpenMailboxes(ctx context.Context, orgID uuid.UUID) ([]models.StoreMailbox, error)
	HasLiveMailboxes(ctx context.Context, orgID uuid.UUID) (bool, error)
	// CreatePhoto records an uploaded photo whose bytes are already stored.
	CreatePhoto(ctx context.Context, p *models.StorePhoto) error
	// GetPhoto is one of the workspace's photos, nil when it has no such photo.
	GetPhoto(ctx context.Context, orgID, id uuid.UUID) (*models.StorePhoto, error)
	// CountPhotosSince counts the workspace's uploads since since, for the upload cap.
	CountPhotosSince(ctx context.Context, orgID uuid.UUID, since time.Time) (int, error)
	// MarkPhotosUsed stamps the photos an order or a mailbox took, so the orphan sweep leaves them.
	MarkPhotosUsed(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) error
	// SetMailboxPhoto makes photoID the mailbox's photo; onlyUnset keeps a photo already chosen.
	SetMailboxPhoto(ctx context.Context, orgID, id, photoID uuid.UUID, onlyUnset bool) (bool, error)
	// MarkPhotoApplied records that GraphKit accepted photoID, unless the mailbox moved on to another photo.
	MarkPhotoApplied(ctx context.Context, orgID, id, photoID uuid.UUID) error
	// RecordPhotoFailure counts a failed set; giveUp stops the retries.
	RecordPhotoFailure(ctx context.Context, orgID, id uuid.UUID, giveUp bool) error
	// PendingPhotos are live mailboxes whose photo GraphKit has not accepted, last tried before triedBefore.
	PendingPhotos(ctx context.Context, triedBefore time.Time, maxAttempts, limit int) ([]models.StoreMailbox, error)
	// StalePhotos are photos no mailbox carries: never used and created before unusedBefore, or used before usedBefore.
	StalePhotos(ctx context.Context, unusedBefore, usedBefore time.Time, limit int) ([]models.StorePhoto, error)
	DeletePhoto(ctx context.Context, orgID, id uuid.UUID) error
	// SetMailboxSignature records the signature an order line chose, unless the mailbox already has one.
	SetMailboxSignature(ctx context.Context, orgID, id uuid.UUID, html, plain string) (bool, error)
	// MarkSignatureApplied stamps the halves now set: Warmbly's copy, the provider's, or both.
	MarkSignatureApplied(ctx context.Context, orgID, id uuid.UUID, local, provider bool) error
	// RecordSignatureFailure counts a failed set; giveUp stops the retries.
	RecordSignatureFailure(ctx context.Context, orgID, id uuid.UUID, giveUp bool) error
	// PendingSignatures are live connected mailboxes with a chosen signature not set everywhere, last tried before triedBefore.
	PendingSignatures(ctx context.Context, triedBefore time.Time, maxAttempts, limit int) ([]models.StoreMailbox, error)
	// ClaimSignature takes a pending signature for one attempt and returns the row as claimed; nil when it is not pending or another attempt holds it.
	ClaimSignature(ctx context.Context, orgID, id uuid.UUID, triedBefore time.Time, maxAttempts int) (*models.StoreMailbox, error)
	// DomainAdminMailbox is the domain's live admin mailbox, nil when it has none.
	DomainAdminMailbox(ctx context.Context, orgID, domainID uuid.UUID) (*models.StoreMailbox, error)

	WebhookEventSeen(ctx context.Context, eventID string) (bool, error)
	// RecordWebhookEvent reports whether the event is new.
	RecordWebhookEvent(ctx context.Context, eventID string) (bool, error)
	PruneWebhookEvents(ctx context.Context, before time.Time) (int64, error)
	GetEventsCursor(ctx context.Context) (string, error)
	SetEventsCursor(ctx context.Context, cursor string) error
}

type storeRepository struct{ db *db.DB }

// NewStoreRepository builds the Postgres store repository.
func NewStoreRepository(d *db.DB) StoreRepository { return &storeRepository{db: d} }

func (r *storeRepository) exec(ctx context.Context, query string, args ...any) error {
	if _, err := r.db.Exec(ctx, query, args...); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *storeRepository) affected(ctx context.Context, query string, args ...any) (int64, error) {
	tag, err := r.db.Exec(ctx, query, args...)
	if err != nil {
		db.CaptureError(err, query, nil, "exec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func noRows[T any](v *T, err error) (*T, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return v, nil
}

// ---- accounts -----------------------------------------------------------

func (r *storeRepository) GetAccount(ctx context.Context, orgID uuid.UUID) (*models.StoreAccount, error) {
	var a models.StoreAccount
	var day *int16
	err := r.db.QueryRow(ctx, `SELECT organization_id, graphkit_account_id, registrant, identity_digest, plan_notice_at, bill_day, next_bill_anchor,
		reload_decided, reload_managed, reload_threshold_micros, reload_amount_micros, created_at, updated_at
		FROM store_accounts WHERE organization_id = $1`, orgID).
		Scan(&a.OrganizationID, &a.GraphKitAccountID, &a.Registrant, &a.IdentityDigest, &a.PlanNoticeAt, &day, &a.NextBillAnchor,
			&a.ReloadDecided, &a.ReloadManaged, &a.ReloadThreshold, &a.ReloadAmount, &a.CreatedAt, &a.UpdatedAt)
	if day != nil {
		a.BillDay = int(*day)
	}
	return noRows(&a, err)
}

func (r *storeRepository) SetReloadState(ctx context.Context, orgID uuid.UUID, managed bool, threshold, amount models.Micro) error {
	return r.exec(ctx, `UPDATE store_accounts SET reload_decided = true, reload_managed = $2, reload_threshold_micros = $3,
		reload_amount_micros = $4, updated_at = now() WHERE organization_id = $1`, orgID, managed, threshold, amount)
}

func (r *storeRepository) SetBillAnchor(ctx context.Context, orgID uuid.UUID, day int, next time.Time) error {
	return r.exec(ctx, `UPDATE store_accounts SET bill_day = $2, next_bill_anchor = $3, updated_at = now() WHERE organization_id = $1`, orgID, day, next)
}

func (r *storeRepository) SetIdentityDigest(ctx context.Context, orgID uuid.UUID, digest string) error {
	return r.exec(ctx, `UPDATE store_accounts SET identity_digest = $2, updated_at = now() WHERE organization_id = $1`, orgID, digest)
}

func (r *storeRepository) uuids(ctx context.Context, query string, args ...any) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *storeRepository) AccountOrgs(ctx context.Context, limit int) ([]uuid.UUID, error) {
	return r.uuids(ctx, `SELECT organization_id FROM store_accounts ORDER BY created_at LIMIT $1`, limit)
}

func (r *storeRepository) SetPlanNotice(ctx context.Context, orgID uuid.UUID, at *time.Time) (bool, error) {
	n, err := r.affected(ctx, `UPDATE store_accounts SET plan_notice_at = $2, updated_at = now()
		WHERE organization_id = $1 AND (plan_notice_at IS NULL) = ($2::timestamptz IS NOT NULL)`, orgID, at)
	return n > 0, err
}

func (r *storeRepository) UpsertAccount(ctx context.Context, orgID uuid.UUID, graphkitAccountID string, registrant json.RawMessage) error {
	var reg any
	if registrant != nil {
		reg = []byte(registrant)
	}
	return r.exec(ctx, `
		INSERT INTO store_accounts (organization_id, graphkit_account_id, registrant)
		VALUES ($1, $2, $3::jsonb)
		ON CONFLICT (organization_id) DO UPDATE
		SET graphkit_account_id = CASE WHEN EXCLUDED.graphkit_account_id = '' THEN store_accounts.graphkit_account_id ELSE EXCLUDED.graphkit_account_id END,
		    registrant = COALESCE(EXCLUDED.registrant, store_accounts.registrant),
		    updated_at = now()`, orgID, graphkitAccountID, reg)
}

// ---- quotes -------------------------------------------------------------

func (r *storeRepository) CreateQuote(ctx context.Context, q *models.StoreQuote) error {
	return r.exec(ctx, `INSERT INTO store_quotes (id, organization_id, graphkit_quote_id, snapshot, expires_at)
		VALUES ($1, $2, $3, $4, $5)`, q.ID, q.OrganizationID, q.GraphKitQuoteID, []byte(q.Snapshot), q.ExpiresAt)
}

func (r *storeRepository) GetQuote(ctx context.Context, orgID, id uuid.UUID) (*models.StoreQuote, error) {
	var q models.StoreQuote
	err := r.db.QueryRow(ctx, `SELECT id, organization_id, graphkit_quote_id, snapshot, expires_at, replaced_by, created_at
		FROM store_quotes WHERE organization_id = $1 AND id = $2`, orgID, id).
		Scan(&q.ID, &q.OrganizationID, &q.GraphKitQuoteID, &q.Snapshot, &q.ExpiresAt, &q.ReplacedBy, &q.CreatedAt)
	return noRows(&q, err)
}

// ErrStoreQuoteReplaced refuses an order on a quote that was already priced again as another.
var ErrStoreQuoteReplaced = errors.New("store quote was priced again")

// lockQuote row-locks the workspace's quote; every order and replacement of it serializes here.
func lockQuote(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID) (replacedBy *uuid.UUID, found bool, err error) {
	err = tx.QueryRow(ctx, `SELECT replaced_by FROM store_quotes WHERE organization_id = $1 AND id = $2 FOR UPDATE`, orgID, id).Scan(&replacedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	return replacedBy, err == nil, err
}

func (r *storeRepository) ReplaceQuote(ctx context.Context, orgID, id, by uuid.UUID) (uuid.UUID, bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, false, err
	}
	defer tx.Rollback(ctx)
	replaced, found, err := lockQuote(ctx, tx, orgID, id)
	if err != nil || !found {
		return uuid.Nil, false, err
	}
	if replaced != nil {
		return *replaced, false, tx.Commit(ctx)
	}
	var ordered bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM store_orders WHERE organization_id = $1 AND quote_id = $2)`, orgID, id).Scan(&ordered); err != nil {
		return uuid.Nil, false, err
	}
	if ordered {
		return uuid.Nil, true, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE store_quotes SET replaced_by = $3 WHERE organization_id = $1 AND id = $2`, orgID, id, by); err != nil {
		return uuid.Nil, false, err
	}
	return by, false, tx.Commit(ctx)
}

func (r *storeRepository) PruneQuotes(ctx context.Context, before time.Time) (int64, error) {
	// A quote an order points at stays: the order's snapshot is its own copy, but keep the link readable.
	return r.affected(ctx, `DELETE FROM store_quotes q WHERE q.expires_at < $1
		AND NOT EXISTS (SELECT 1 FROM store_orders o WHERE o.quote_id = q.id)`, before)
}

// ---- orders -------------------------------------------------------------

const storeOrderColumns = `id, organization_id, quote_id, graphkit_order_id, status, quote, steps, due_now_micros,
	charge_key, charged_micros, refunded_micros, refunded_items, forwarding_url, failure_code, refund_pending, created_by,
	created_at, updated_at, completed_at`

func scanStoreOrder(row pgx.Row) (*models.StoreOrder, error) {
	var o models.StoreOrder
	err := row.Scan(&o.ID, &o.OrganizationID, &o.QuoteID, &o.GraphKitOrderID, &o.Status, &o.Quote, &o.Steps, &o.DueNowMicros,
		&o.ChargeKey, &o.ChargedMicros, &o.RefundedMicros, &o.RefundedItems, &o.ForwardingURL, &o.FailureCode, &o.RefundPending, &o.CreatedBy,
		&o.CreatedAt, &o.UpdatedAt, &o.CompletedAt)
	return noRows(&o, err)
}

func (r *storeRepository) orders(ctx context.Context, query string, args ...any) ([]models.StoreOrder, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.StoreOrder{}
	for rows.Next() {
		o, err := scanStoreOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

func (r *storeRepository) CreateOrder(ctx context.Context, o *models.StoreOrder) (*models.StoreOrder, bool, error) {
	steps := o.Steps
	if steps == nil {
		steps = json.RawMessage(`[]`)
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)
	if replaced, _, err := lockQuote(ctx, tx, o.OrganizationID, o.QuoteID); err != nil {
		return nil, false, err
	} else if replaced != nil {
		return nil, false, ErrStoreQuoteReplaced
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO store_orders (id, organization_id, quote_id, status, quote, steps, due_now_micros, charge_key, forwarding_url, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (quote_id) DO NOTHING`,
		o.ID, o.OrganizationID, o.QuoteID, o.Status, []byte(o.Quote), []byte(steps), o.DueNowMicros, o.ChargeKey, o.ForwardingURL, o.CreatedBy)
	if err != nil {
		db.CaptureError(err, "insert store_orders", nil, "exec")
		return nil, false, err
	}
	got, err := scanStoreOrder(tx.QueryRow(ctx, `SELECT `+storeOrderColumns+` FROM store_orders
		WHERE organization_id = $1 AND quote_id = $2`, o.OrganizationID, o.QuoteID))
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	if got == nil {
		// The quote id belongs to another workspace's order; quote ids are random, so this is tampering.
		return nil, false, errors.New("store order quote is held by another organization")
	}
	return got, tag.RowsAffected() == 0, nil
}

func (r *storeRepository) GetOrder(ctx context.Context, orgID, id uuid.UUID) (*models.StoreOrder, error) {
	return scanStoreOrder(r.db.QueryRow(ctx, `SELECT `+storeOrderColumns+` FROM store_orders
		WHERE organization_id = $1 AND id = $2`, orgID, id))
}

func (r *storeRepository) GetOrderByGraphKitID(ctx context.Context, orgID uuid.UUID, graphkitID string) (*models.StoreOrder, error) {
	return scanStoreOrder(r.db.QueryRow(ctx, `SELECT `+storeOrderColumns+` FROM store_orders
		WHERE organization_id = $1 AND graphkit_order_id = $2`, orgID, graphkitID))
}

func (r *storeRepository) GetOrderByQuote(ctx context.Context, orgID, quoteID uuid.UUID) (*models.StoreOrder, error) {
	return scanStoreOrder(r.db.QueryRow(ctx, `SELECT `+storeOrderColumns+` FROM store_orders
		WHERE organization_id = $1 AND quote_id = $2`, orgID, quoteID))
}

func keyset(after *models.StorePage) (any, any) {
	if after == nil {
		return nil, nil
	}
	return after.CreatedAt, after.ID
}

func (r *storeRepository) ListOrders(ctx context.Context, orgID uuid.UUID, after *models.StorePage, limit int) ([]models.StoreOrder, error) {
	at, id := keyset(after)
	return r.orders(ctx, `SELECT `+storeOrderColumns+` FROM store_orders
		WHERE organization_id = $1 AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3::uuid))
		ORDER BY created_at DESC, id DESC LIMIT $4`, orgID, at, id, limit)
}

func (r *storeRepository) DeleteUnchargedOrder(ctx context.Context, orgID, id uuid.UUID) error {
	return r.exec(ctx, `DELETE FROM store_orders WHERE organization_id = $1 AND id = $2
		AND status = 'charging' AND charged_micros = 0 AND graphkit_order_id IS NULL`, orgID, id)
}

func (r *storeRepository) MarkOrderCharged(ctx context.Context, orgID, id uuid.UUID, charged models.Micro) error {
	return r.exec(ctx, `UPDATE store_orders SET charged_micros = $3, updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, charged)
}

func (r *storeRepository) MarkOrderPlaced(ctx context.Context, orgID, id uuid.UUID, graphkitID, status string, steps json.RawMessage) error {
	return r.exec(ctx, `UPDATE store_orders SET graphkit_order_id = $3, status = $4, steps = $5, updated_at = now()
		WHERE organization_id = $1 AND id = $2 AND (graphkit_order_id IS NULL OR graphkit_order_id = $3)`,
		orgID, id, graphkitID, status, []byte(steps))
}

func (r *storeRepository) UpdateOrderState(ctx context.Context, orgID, id uuid.UUID, status string, steps json.RawMessage, completedAt *time.Time) (string, error) {
	// The row lock makes the replaced status exact, so one caller sees a transition and a concurrent one does not.
	var before string
	err := r.db.QueryRow(ctx, `
		UPDATE store_orders o SET status = $3, steps = $4, completed_at = COALESCE(o.completed_at, $5), updated_at = now()
		FROM (SELECT id, status FROM store_orders WHERE organization_id = $1 AND id = $2 FOR UPDATE) prev
		WHERE o.id = prev.id AND o.status <> 'failed'
		RETURNING prev.status`, orgID, id, status, []byte(steps), completedAt).Scan(&before)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		db.CaptureError(err, "update store_orders state", nil, "queryrow")
	}
	return before, err
}

func (r *storeRepository) MarkOrderFailed(ctx context.Context, orgID, id uuid.UUID, code string, refunded models.Micro, refundPending bool) error {
	return r.exec(ctx, `UPDATE store_orders SET status = 'failed', failure_code = $3, refund_pending = $5,
		refunded_micros = GREATEST(refunded_micros, $4), completed_at = COALESCE(completed_at, now()), updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, code, refunded, refundPending)
}

func (r *storeRepository) MarkOrderRefunded(ctx context.Context, orgID, id uuid.UUID, refunded models.Micro) error {
	return r.exec(ctx, `UPDATE store_orders SET refund_pending = false, refunded_micros = GREATEST(refunded_micros, $3), updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, refunded)
}

func (r *storeRepository) RefundPendingOrders(ctx context.Context, before time.Time, limit int) ([]models.StoreOrder, error) {
	return r.orders(ctx, `SELECT `+storeOrderColumns+` FROM store_orders
		WHERE refund_pending AND updated_at < $1 ORDER BY updated_at LIMIT $2`, before, limit)
}

func (r *storeRepository) StrandedOrders(ctx context.Context, before time.Time, limit int) ([]models.StoreOrder, error) {
	return r.orders(ctx, `SELECT `+storeOrderColumns+` FROM store_orders
		WHERE status = 'charging' AND charged_micros = 0 AND graphkit_order_id IS NULL AND updated_at < $1
		ORDER BY updated_at LIMIT $2`, before, limit)
}

func (r *storeRepository) AddOrderRefund(ctx context.Context, orgID, id uuid.UUID, itemKey string, amount models.Micro) (bool, error) {
	n, err := r.affected(ctx, `UPDATE store_orders
		SET refunded_items = array_append(refunded_items, $3), refunded_micros = refunded_micros + $4, updated_at = now()
		WHERE organization_id = $1 AND id = $2 AND NOT ($3 = ANY(refunded_items))`, orgID, id, itemKey, amount)
	return n > 0, err
}

func (r *storeRepository) ListOpenOrders(ctx context.Context, before time.Time, limit int, uncharged bool) ([]models.StoreOrder, error) {
	return r.orders(ctx, `SELECT `+storeOrderColumns+` FROM store_orders
		WHERE status IN ('charging', 'processing') AND updated_at < $1
		  AND (status = 'processing' OR charged_micros > 0 OR graphkit_order_id IS NOT NULL OR $3)
		ORDER BY updated_at LIMIT $2`, before, limit, uncharged)
}

// ---- domains ------------------------------------------------------------

const storeDomainColumns = `d.id, d.organization_id, d.order_id, d.graphkit_domain_id, d.name, d.status, d.source, d.provider, d.expires_at,
	d.auto_renew, d.renew_retail_micros, d.last_seen_renew_micros, d.renewal_notice_at, d.forwarding_url, d.mail_auth,
	d.details, d.tracking_host, d.renewal_attempt_at, d.renewal_failures, d.renewal_charge_key, d.last_notice,
	(SELECT count(*) FROM store_mailboxes m WHERE m.domain_id = d.id AND m.status NOT IN ('cancelled', 'failed')),
	d.created_at, d.updated_at`

// storeDomainReturning is storeDomainColumns for a RETURNING clause on store_domains.
var storeDomainReturning = strings.ReplaceAll(storeDomainColumns, "d.", "store_domains.")

func scanStoreDomain(row pgx.Row) (*models.StoreDomain, error) {
	var d models.StoreDomain
	err := row.Scan(&d.ID, &d.OrganizationID, &d.OrderID, &d.GraphKitDomainID, &d.Name, &d.Status, &d.Source, &d.Provider, &d.ExpiresAt,
		&d.AutoRenew, &d.RenewRetailMicros, &d.LastSeenRenewMicros, &d.RenewalNoticeAt, &d.ForwardingURL, &d.MailAuth,
		&d.Details, &d.TrackingHost, &d.RenewalAttemptAt, &d.RenewalFailures, &d.RenewalChargeKey, &d.LastNotice,
		&d.MailboxCount, &d.CreatedAt, &d.UpdatedAt)
	return noRows(&d, err)
}

func (r *storeRepository) domains(ctx context.Context, query string, args ...any) ([]models.StoreDomain, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.StoreDomain{}
	for rows.Next() {
		d, err := scanStoreDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (r *storeRepository) UpsertDomain(ctx context.Context, d *models.StoreDomain) (*models.StoreDomain, error) {
	mailAuth := d.MailAuth
	if mailAuth == nil {
		mailAuth = json.RawMessage(`{}`)
	}
	details := d.Details
	if details == nil {
		details = json.RawMessage(`{}`)
	}
	source := d.Source
	if source == "" {
		source = models.StoreDomainPurchased
	}
	// The organization guard keeps one workspace's event from rewriting another's row.
	// RETURNING, not a CTE read: a statement does not see the rows its own CTE wrote.
	return scanStoreDomain(r.db.QueryRow(ctx, `
			INSERT INTO store_domains (id, organization_id, order_id, graphkit_domain_id, name, status, provider, expires_at,
			                           renew_retail_micros, last_seen_renew_micros, forwarding_url, mail_auth, details, source, auto_renew)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9, $10, $11, $12, $13, $13 = 'purchased')
			ON CONFLICT (graphkit_domain_id) DO UPDATE
			SET name = EXCLUDED.name, status = EXCLUDED.status,
			    -- A new expiry starts its notices over.
			    renewal_notice_at = CASE WHEN EXCLUDED.expires_at IS DISTINCT FROM store_domains.expires_at AND EXCLUDED.expires_at IS NOT NULL
			                             THEN NULL ELSE store_domains.renewal_notice_at END,
			    last_notice = CASE WHEN EXCLUDED.expires_at IS DISTINCT FROM store_domains.expires_at AND EXCLUDED.expires_at IS NOT NULL
			                       THEN '' ELSE store_domains.last_notice END,
			    provider = CASE WHEN EXCLUDED.provider = '' THEN store_domains.provider ELSE EXCLUDED.provider END,
			    expires_at = COALESCE(EXCLUDED.expires_at, store_domains.expires_at),
			    renew_retail_micros = CASE WHEN EXCLUDED.renew_retail_micros = 0 THEN store_domains.renew_retail_micros ELSE EXCLUDED.renew_retail_micros END,
			    forwarding_url = EXCLUDED.forwarding_url, mail_auth = EXCLUDED.mail_auth, details = EXCLUDED.details,
			    order_id = COALESCE(store_domains.order_id, EXCLUDED.order_id), updated_at = now()
			WHERE store_domains.organization_id = EXCLUDED.organization_id
			RETURNING `+storeDomainReturning,
		d.ID, d.OrganizationID, d.OrderID, d.GraphKitDomainID, d.Name, d.Status, d.Provider, d.ExpiresAt,
		d.RenewRetailMicros, d.ForwardingURL, []byte(mailAuth), []byte(details), source))
}

func (r *storeRepository) GetDomain(ctx context.Context, orgID, id uuid.UUID) (*models.StoreDomain, error) {
	return scanStoreDomain(r.db.QueryRow(ctx, `SELECT `+storeDomainColumns+` FROM store_domains d
		WHERE d.organization_id = $1 AND d.id = $2`, orgID, id))
}

func (r *storeRepository) GetDomainByGraphKitID(ctx context.Context, orgID uuid.UUID, graphkitID string) (*models.StoreDomain, error) {
	return scanStoreDomain(r.db.QueryRow(ctx, `SELECT `+storeDomainColumns+` FROM store_domains d
		WHERE d.organization_id = $1 AND d.graphkit_domain_id = $2`, orgID, graphkitID))
}

func (r *storeRepository) ListDomains(ctx context.Context, orgID uuid.UUID, after *models.StorePage, limit int) ([]models.StoreDomain, error) {
	at, id := keyset(after)
	return r.domains(ctx, `SELECT `+storeDomainColumns+` FROM store_domains d
		WHERE d.organization_id = $1 AND ($2::timestamptz IS NULL OR (d.created_at, d.id) < ($2, $3::uuid))
		ORDER BY d.created_at DESC, d.id DESC LIMIT $4`, orgID, at, id, limit)
}

func (r *storeRepository) ListDomainsByOrder(ctx context.Context, orgID, orderID uuid.UUID) ([]models.StoreDomain, error) {
	return r.domains(ctx, `SELECT `+storeDomainColumns+` FROM store_domains d
		WHERE d.organization_id = $1 AND d.order_id = $2 ORDER BY d.created_at, d.id`, orgID, orderID)
}

func (r *storeRepository) SetDomainAutoRenew(ctx context.Context, orgID, id uuid.UUID, on bool) error {
	// A renewal in flight keeps its attempt count, which its charge key carries.
	return r.exec(ctx, `UPDATE store_domains SET auto_renew = $3,
		renewal_failures = CASE WHEN $3 AND renewal_charge_key IS NULL THEN 0 ELSE renewal_failures END,
		renewal_attempt_at = CASE WHEN $3 THEN NULL ELSE renewal_attempt_at END, updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, on)
}

func (r *storeRepository) SetDomainForwarding(ctx context.Context, orgID, id uuid.UUID, url string) error {
	return r.exec(ctx, `UPDATE store_domains SET forwarding_url = $3, updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, url)
}

func (r *storeRepository) SetDomainTrackingHost(ctx context.Context, orgID, id uuid.UUID, host string) error {
	return r.exec(ctx, `UPDATE store_domains SET tracking_host = $3, updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, host)
}

func (r *storeRepository) DeleteConnectedDomain(ctx context.Context, orgID, id uuid.UUID) (bool, error) {
	n, err := r.affected(ctx, `DELETE FROM store_domains d WHERE d.organization_id = $1 AND d.id = $2 AND d.source = 'connected'
		AND NOT EXISTS (SELECT 1 FROM store_mailboxes m WHERE m.domain_id = d.id AND m.status NOT IN ('cancelled', 'failed'))`, orgID, id)
	return n > 0, err
}

func (r *storeRepository) GetDomainByName(ctx context.Context, orgID uuid.UUID, name string) (*models.StoreDomain, error) {
	return scanStoreDomain(r.db.QueryRow(ctx, `SELECT `+storeDomainColumns+` FROM store_domains d
		WHERE d.organization_id = $1 AND d.name = lower($2) AND d.status NOT IN ('failed', 'transferred_out', 'removed')
		ORDER BY d.created_at DESC LIMIT 1`, orgID, name))
}

func (r *storeRepository) DomainsNeedingTracking(ctx context.Context, limit int) ([]models.StoreDomain, error) {
	return r.domains(ctx, `SELECT `+storeDomainColumns+` FROM store_domains d
		WHERE d.status = 'active' AND d.tracking_host = ''
		  AND NOT (d.source = 'connected' AND d.details->>'dns_host' = 'customer')
		  AND EXISTS (SELECT 1 FROM store_mailboxes m WHERE m.domain_id = d.id AND m.email_account_id IS NOT NULL)
		ORDER BY d.updated_at LIMIT $1`, limit)
}

func (r *storeRepository) RecordDomainRenewal(ctx context.Context, orgID, id uuid.UUID, expiresAt time.Time) error {
	return r.exec(ctx, `UPDATE store_domains SET expires_at = GREATEST(COALESCE(expires_at, $3), $3), renewal_failures = 0,
		renewal_attempt_at = now(), last_notice = '', renewal_notice_at = NULL, renewal_charge_key = NULL,
		status = CASE WHEN status = 'expired' THEN 'active' ELSE status END, updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, expiresAt)
}

func (r *storeRepository) RecordDomainRenewalFailure(ctx context.Context, orgID, id uuid.UUID) error {
	return r.exec(ctx, `UPDATE store_domains SET renewal_failures = renewal_failures + 1, renewal_attempt_at = now(),
		renewal_charge_key = NULL, updated_at = now() WHERE organization_id = $1 AND id = $2`, orgID, id)
}

func (r *storeRepository) SetRenewalCharge(ctx context.Context, orgID, id uuid.UUID, key *string) error {
	return r.exec(ctx, `UPDATE store_domains SET renewal_charge_key = $3, updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, key)
}

func (r *storeRepository) DueDomainRenewals(ctx context.Context, horizon, attemptedBefore, noticedBefore, urgent time.Time, limit int) ([]models.StoreDomain, error) {
	return r.domains(ctx, `SELECT `+storeDomainColumns+` FROM store_domains d
		WHERE d.expires_at IS NOT NULL
		  AND (d.renewal_charge_key IS NOT NULL OR (d.status IN ('active', 'expired') AND d.auto_renew AND d.expires_at <= $1
		    AND (d.renewal_attempt_at IS NULL OR d.renewal_attempt_at < $2)
		    AND (d.renewal_notice_at <= $3 OR d.expires_at <= $4)))
		ORDER BY d.expires_at LIMIT $5`, horizon, attemptedBefore, noticedBefore, urgent, limit)
}

func (r *storeRepository) DomainsExpiringBefore(ctx context.Context, horizon time.Time, limit int) ([]models.StoreDomain, error) {
	return r.domains(ctx, `SELECT `+storeDomainColumns+` FROM store_domains d
		WHERE d.status IN ('active', 'expired') AND d.expires_at IS NOT NULL AND d.expires_at <= $1
		ORDER BY d.expires_at LIMIT $2`, horizon, limit)
}

func (r *storeRepository) SetDomainNotice(ctx context.Context, id uuid.UUID, notice string, statedRenew models.Micro, upcoming bool) (bool, error) {
	n, err := r.affected(ctx, `UPDATE store_domains SET last_notice = $2, last_seen_renew_micros = $3,
		renewal_notice_at = CASE WHEN $4 THEN now() ELSE renewal_notice_at END
		WHERE id = $1 AND last_notice <> $2`, id, notice, statedRenew, upcoming)
	return n > 0, err
}

// ---- mailboxes ----------------------------------------------------------

const storeMailboxColumns = `id, organization_id, domain_id, order_id, graphkit_mailbox_id, email, provider, status, first_name,
	last_name, is_admin, monthly_retail_micros, paid_through, period_end, unpaid_cancel, cancels_at, cancelled_at, email_account_id, connect_method, connect_state, connect_error,
	connect_attempts, billing_failures, pending_charge_key, pending_charge_micros, paid_key, paid_micros, last_notice, photo_id, photo_applied_id, signature_html, signature_plain,
	signature_set_at, signature_provider_at, created_at, updated_at, action_required, action_reason`

func scanStoreMailbox(row pgx.Row) (*models.StoreMailbox, error) {
	var m models.StoreMailbox
	err := row.Scan(&m.ID, &m.OrganizationID, &m.DomainID, &m.OrderID, &m.GraphKitMailboxID, &m.Email, &m.Provider, &m.Status,
		&m.FirstName, &m.LastName, &m.IsAdmin, &m.MonthlyRetailMicros, &m.PaidThrough, &m.PeriodEnd, &m.UnpaidCancel, &m.CancelsAt, &m.CancelledAt, &m.EmailAccountID, &m.ConnectMethod,
		&m.ConnectState, &m.ConnectError, &m.ConnectAttempts, &m.BillingFailures, &m.PendingChargeKey, &m.PendingChargeMicros, &m.PaidKey, &m.PaidMicros, &m.LastNotice,
		&m.PhotoID, &m.PhotoAppliedID, &m.SignatureHTML, &m.SignaturePlain, &m.SignatureSetAt, &m.SignatureProviderAt, &m.CreatedAt, &m.UpdatedAt, &m.ActionRequired, &m.ActionReason)
	return noRows(&m, err)
}

func (r *storeRepository) mailboxes(ctx context.Context, query string, args ...any) ([]models.StoreMailbox, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.StoreMailbox{}
	for rows.Next() {
		m, err := scanStoreMailbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (r *storeRepository) UpsertMailbox(ctx context.Context, m *models.StoreMailbox) (*models.StoreMailbox, error) {
	return scanStoreMailbox(r.db.QueryRow(ctx, `
		INSERT INTO store_mailboxes (id, organization_id, domain_id, order_id, graphkit_mailbox_id, email, provider, status,
		                             first_name, last_name, monthly_retail_micros, paid_through, cancels_at, connect_method, is_admin,
		                             period_end, paid_aligned)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, COALESCE($16::timestamptz, $12::timestamptz), $13, $14, $15, $16::timestamptz, $16::timestamptz IS NOT NULL)
		ON CONFLICT (graphkit_mailbox_id) DO UPDATE
		SET email = EXCLUDED.email, status = EXCLUDED.status, first_name = EXCLUDED.first_name, last_name = EXCLUDED.last_name,
		    is_admin = EXCLUDED.is_admin,
		    -- The checkout paid GraphKit's first period, whenever provisioning started it: align to it once known.
		    paid_through = CASE WHEN NOT store_mailboxes.paid_aligned AND EXCLUDED.period_end IS NOT NULL
		                        THEN EXCLUDED.period_end ELSE store_mailboxes.paid_through END,
		    paid_aligned = store_mailboxes.paid_aligned OR EXCLUDED.period_end IS NOT NULL,
		    period_end = COALESCE(EXCLUDED.period_end, store_mailboxes.period_end),
		    cancels_at = EXCLUDED.cancels_at, connect_method = EXCLUDED.connect_method,
		    order_id = COALESCE(store_mailboxes.order_id, EXCLUDED.order_id), updated_at = now()
		WHERE store_mailboxes.organization_id = EXCLUDED.organization_id
		RETURNING `+storeMailboxColumns,
		m.ID, m.OrganizationID, m.DomainID, m.OrderID, m.GraphKitMailboxID, m.Email, m.Provider, m.Status,
		m.FirstName, m.LastName, m.MonthlyRetailMicros, m.PaidThrough, m.CancelsAt, m.ConnectMethod, m.IsAdmin, m.PeriodEnd))
}

func (r *storeRepository) GetMailbox(ctx context.Context, orgID, id uuid.UUID) (*models.StoreMailbox, error) {
	return scanStoreMailbox(r.db.QueryRow(ctx, `SELECT `+storeMailboxColumns+` FROM store_mailboxes
		WHERE organization_id = $1 AND id = $2`, orgID, id))
}

func (r *storeRepository) GetMailboxByGraphKitID(ctx context.Context, orgID uuid.UUID, graphkitID string) (*models.StoreMailbox, error) {
	return scanStoreMailbox(r.db.QueryRow(ctx, `SELECT `+storeMailboxColumns+` FROM store_mailboxes
		WHERE organization_id = $1 AND graphkit_mailbox_id = $2`, orgID, graphkitID))
}

func (r *storeRepository) GetMailboxByEmail(ctx context.Context, orgID uuid.UUID, email string) (*models.StoreMailbox, error) {
	return scanStoreMailbox(r.db.QueryRow(ctx, `SELECT `+storeMailboxColumns+` FROM store_mailboxes
		WHERE organization_id = $1 AND email = lower($2) AND status NOT IN ('cancelled', 'failed')
		ORDER BY created_at DESC LIMIT 1`, orgID, email))
}

func (r *storeRepository) ListMailboxes(ctx context.Context, orgID uuid.UUID, after *models.StorePage, limit int) ([]models.StoreMailbox, error) {
	at, id := keyset(after)
	return r.mailboxes(ctx, `SELECT `+storeMailboxColumns+` FROM store_mailboxes
		WHERE organization_id = $1 AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3::uuid))
		ORDER BY created_at DESC, id DESC LIMIT $4`, orgID, at, id, limit)
}

func (r *storeRepository) ListMailboxesByOrder(ctx context.Context, orgID, orderID uuid.UUID) ([]models.StoreMailbox, error) {
	return r.mailboxes(ctx, `SELECT `+storeMailboxColumns+` FROM store_mailboxes
		WHERE organization_id = $1 AND order_id = $2 ORDER BY created_at, email`, orgID, orderID)
}

func (r *storeRepository) SetMailboxNames(ctx context.Context, orgID, id uuid.UUID, first, last string) error {
	return r.exec(ctx, `UPDATE store_mailboxes SET first_name = $3, last_name = $4, updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, first, last)
}

func (r *storeRepository) SetMailboxCancel(ctx context.Context, orgID, id uuid.UUID, status string, cancelsAt *time.Time) error {
	// Any cancellation recorded here is the customer's or GraphKit's; only SetUnpaidCancel makes one unpaid.
	return r.exec(ctx, `UPDATE store_mailboxes SET status = $3, cancels_at = $4, unpaid_cancel = false,
		updated_at = now() WHERE organization_id = $1 AND id = $2`, orgID, id, status, cancelsAt)
}

func (r *storeRepository) GetMailboxByAccount(ctx context.Context, orgID, emailAccountID uuid.UUID) (*models.StoreMailbox, error) {
	return scanStoreMailbox(r.db.QueryRow(ctx, `SELECT `+storeMailboxColumns+` FROM store_mailboxes
		WHERE organization_id = $1 AND email_account_id = $2 ORDER BY created_at DESC LIMIT 1`, orgID, emailAccountID))
}

// storeEndedMailbox is a purchased mailbox the provider no longer keeps.
const storeEndedMailbox = `status IN ('cancelled', 'failed')`

func (r *storeRepository) MarkMailboxEnded(ctx context.Context, orgID, id uuid.UUID, at time.Time) (bool, error) {
	n, err := r.affected(ctx, `UPDATE store_mailboxes SET cancelled_at = $3, updated_at = now()
		WHERE organization_id = $1 AND id = $2 AND cancelled_at IS NULL AND `+storeEndedMailbox, orgID, id, at)
	return n > 0, err
}

func (r *storeRepository) UnmarkedEndedMailboxes(ctx context.Context, limit int) ([]models.StoreMailbox, error) {
	return r.mailboxes(ctx, `SELECT `+storeMailboxColumns+` FROM store_mailboxes
		WHERE cancelled_at IS NULL AND `+storeEndedMailbox+` ORDER BY updated_at LIMIT $1`, limit)
}

// storeConnectRetry is the claimable set: never tried (or waiting on a person's
// step and not tried in the last half hour), failed fewer than five times and
// not in the last attempt's backoff, or a claim abandoned by a crash.
const storeConnectRetry = `email_account_id IS NULL AND status = 'active' AND cancelled_at IS NULL AND (
	(connect_state = 'waiting' AND (action_required IS NULL OR connect_attempted_at IS NULL
		OR connect_attempted_at < $NOW - interval '30 minutes'))
	OR (connect_state = 'failed' AND connect_attempts < 5 AND connect_attempted_at < $NOW - make_interval(mins => 10 * connect_attempts))
	OR (connect_state = 'connecting' AND connect_attempted_at < $NOW - interval '10 minutes'))`

func connectRetry(param string) string { return strings.ReplaceAll(storeConnectRetry, "$NOW", param) }

func (r *storeRepository) ClaimConnect(ctx context.Context, orgID, id uuid.UUID, now time.Time) (bool, error) {
	n, err := r.affected(ctx, `UPDATE store_mailboxes SET connect_state = 'connecting', connect_attempts = connect_attempts + 1,
		connect_attempted_at = $3, updated_at = now()
		WHERE organization_id = $1 AND id = $2 AND `+connectRetry("$3::timestamptz"), orgID, id, now)
	return n > 0, err
}

func (r *storeRepository) FinishConnect(ctx context.Context, orgID, id uuid.UUID, emailAccountID *uuid.UUID, state, errMsg string) error {
	return r.exec(ctx, `UPDATE store_mailboxes SET email_account_id = COALESCE($3, email_account_id), connect_state = $4,
		connect_error = $5, action_required = NULL, action_reason = NULL, updated_at = now() WHERE organization_id = $1 AND id = $2`, orgID, id, emailAccountID, state, errMsg)
}

func (r *storeRepository) AwaitConnectAction(ctx context.Context, orgID, id uuid.UUID, action, reason string) error {
	return r.exec(ctx, `UPDATE store_mailboxes SET connect_state = 'waiting', action_required = $3, action_reason = NULLIF($4, ''),
		connect_attempts = 0, connect_error = '', updated_at = now() WHERE organization_id = $1 AND id = $2 AND email_account_id IS NULL`, orgID, id, action, reason)
}

func (r *storeRepository) DomainActionReason(ctx context.Context, orgID, domainID uuid.UUID, action string) (string, error) {
	var reason string
	err := r.db.QueryRow(ctx, `SELECT action_reason FROM store_mailboxes WHERE organization_id = $1 AND domain_id = $2
		AND action_required = $3 AND action_reason IS NOT NULL AND email_account_id IS NULL AND `+storeLiveMailbox+` LIMIT 1`,
		orgID, domainID, action).Scan(&reason)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return reason, err
}

func (r *storeRepository) WakeConnectAction(ctx context.Context, orgID uuid.UUID, action string, domains []string) ([]models.StoreMailbox, error) {
	return r.mailboxes(ctx, `UPDATE store_mailboxes SET action_required = NULL, action_reason = NULL, updated_at = now()
		WHERE organization_id = $1 AND action_required = $2 AND email_account_id IS NULL AND split_part(email, '@', 2) = ANY($3)
		RETURNING `+storeMailboxColumns, orgID, action, domains)
}

func (r *storeRepository) PendingConnects(ctx context.Context, now time.Time, limit int) ([]models.StoreMailbox, error) {
	return r.mailboxes(ctx, `SELECT `+storeMailboxColumns+` FROM store_mailboxes
		WHERE `+connectRetry("$1::timestamptz")+` ORDER BY updated_at LIMIT $2`, now, limit)
}

// storeMailboxBillable is a mailbox whose months are collected: one the workspace
// keeps, or one cancelled for an unpaid month while there is still time to pay and resume it.
const storeMailboxBillable = `pending_charge_key IS NULL AND ` + storeMailboxKept

// storeMailboxKept is a mailbox whose next month will start once paid.
const storeMailboxKept = `(
	(status IN ('active', 'suspended') AND cancels_at IS NULL)
	OR (unpaid_cancel AND status = 'cancelling' AND cancels_at > $2::timestamptz + interval '1 hour'))`

func (r *storeRepository) MailboxesDue(ctx context.Context, orgID uuid.UUID, before, now time.Time) ([]models.StoreMailbox, error) {
	return r.mailboxes(ctx, `SELECT `+storeMailboxColumns+` FROM store_mailboxes
		WHERE organization_id = $1 AND `+storeMailboxBillable+` AND paid_through < $3 ORDER BY paid_through, id`, orgID, now, before)
}

func (r *storeRepository) UnpaidMailboxes(ctx context.Context, orgID uuid.UUID, now, attemptedBefore time.Time, withPending bool) ([]models.StoreMailbox, error) {
	return r.mailboxes(ctx, `SELECT `+storeMailboxColumns+` FROM store_mailboxes
		WHERE organization_id = $1 AND (pending_charge_key IS NULL OR $4) AND `+storeMailboxKept+` AND billing_failures > 0
		  AND (billing_attempted_at IS NULL OR billing_attempted_at < $3 OR $4) ORDER BY paid_through, id`, orgID, now, attemptedBefore, withPending)
}

func (r *storeRepository) SetUnpaidCancel(ctx context.Context, orgID, id uuid.UUID, status string, cancelsAt *time.Time) error {
	return r.exec(ctx, `UPDATE store_mailboxes SET status = $3, cancels_at = $4, unpaid_cancel = true, updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, status, cancelsAt)
}

func (r *storeRepository) MarkMonthPaid(ctx context.Context, orgID, id uuid.UUID) error {
	return r.exec(ctx, `UPDATE store_mailboxes SET paid_through = paid_through + interval '1 month', billing_failures = 0,
		paid_key = NULL, paid_micros = 0, last_notice = '', updated_at = now() WHERE organization_id = $1 AND id = $2`, orgID, id)
}

func (r *storeRepository) RefundMonth(ctx context.Context, orgID, id uuid.UUID, paidKey string) (bool, error) {
	n, err := r.affected(ctx, `UPDATE store_mailboxes SET paid_through = paid_through - interval '1 month', paid_key = NULL, paid_micros = 0,
		unpaid_cancel = false, updated_at = now()
		WHERE organization_id = $1 AND id = $2 AND paid_key = $3 AND pending_charge_key IS NULL`, orgID, id, paidKey)
	return n > 0, err
}

func (r *storeRepository) SetMailboxRetail(ctx context.Context, orgID, id uuid.UUID, monthly models.Micro) error {
	return r.exec(ctx, `UPDATE store_mailboxes SET monthly_retail_micros = $3, updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, monthly)
}

// storeMailboxLive is a mailbox GraphKit still bills a period for.
const storeMailboxLive = `status IN ('active', 'suspended', 'cancelling')`

func (r *storeRepository) MailboxesPastPeriod(ctx context.Context, now time.Time, limit int) ([]models.StoreMailbox, error) {
	return r.mailboxes(ctx, `SELECT `+storeMailboxColumns+` FROM store_mailboxes
		WHERE `+storeMailboxLive+` AND period_end IS NOT NULL AND period_end <= $1 ORDER BY period_end LIMIT $2`, now, limit)
}

func (r *storeRepository) UncollectedMailboxes(ctx context.Context, limit int) ([]models.StoreMailbox, error) {
	return r.mailboxes(ctx, `SELECT `+storeMailboxColumns+` FROM store_mailboxes
		WHERE `+storeMailboxLive+` AND period_end > paid_through + interval '1 hour' ORDER BY period_end LIMIT $1`, limit)
}

// reserveMailboxes stamps key on the rows still paid through what was read and in no batch.
const reserveMailboxes = `UPDATE store_mailboxes m SET pending_charge_key = $4, pending_charge_micros = m.monthly_retail_micros,
	billing_attempted_at = now(), updated_at = now()
	FROM unnest($2::uuid[], $3::timestamptz[]) AS v(id, paid_through)
	WHERE m.organization_id = $1 AND m.id = v.id AND m.paid_through = v.paid_through AND m.pending_charge_key IS NULL`

func reservation(rows []models.StoreMailbox) ([]uuid.UUID, []time.Time) {
	ids := make([]uuid.UUID, 0, len(rows))
	paid := make([]time.Time, 0, len(rows))
	for i := range rows {
		ids, paid = append(ids, rows[i].ID), append(paid, rows[i].PaidThrough)
	}
	return ids, paid
}

func (r *storeRepository) ReserveMailboxCharge(ctx context.Context, orgID uuid.UUID, rows []models.StoreMailbox, key string) (int64, error) {
	ids, paid := reservation(rows)
	return r.affected(ctx, reserveMailboxes, orgID, ids, paid, key)
}

func (r *storeRepository) ReserveRun(ctx context.Context, orgID uuid.UUID, rows []models.StoreMailbox, key string, anchor, next time.Time) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE store_accounts SET next_bill_anchor = $3, updated_at = now()
		WHERE organization_id = $1 AND next_bill_anchor = $2`, orgID, anchor, next)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if len(rows) > 0 {
		ids, paid := reservation(rows)
		if _, err := tx.Exec(ctx, reserveMailboxes, orgID, ids, paid, key); err != nil {
			return false, err
		}
	}
	return true, tx.Commit(ctx)
}

func (r *storeRepository) MailboxesByChargeKey(ctx context.Context, orgID uuid.UUID, key string) ([]models.StoreMailbox, error) {
	return r.mailboxes(ctx, `SELECT `+storeMailboxColumns+` FROM store_mailboxes
		WHERE organization_id = $1 AND pending_charge_key = $2 ORDER BY id`, orgID, key)
}

func (r *storeRepository) PendingChargeKeys(ctx context.Context, orgID uuid.UUID, reservedBefore time.Time) ([]string, error) {
	query := `SELECT DISTINCT pending_charge_key FROM store_mailboxes WHERE organization_id = $1 AND pending_charge_key IS NOT NULL
		AND (billing_attempted_at IS NULL OR billing_attempted_at < $2)`
	rows, err := r.db.Query(ctx, query, orgID, reservedBefore)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (r *storeRepository) SettleMailboxCharge(ctx context.Context, orgID uuid.UUID, key string, now time.Time) (int64, error) {
	return r.affected(ctx, `UPDATE store_mailboxes SET paid_through = paid_through + interval '1 month', pending_charge_key = NULL,
		paid_key = $3, paid_micros = pending_charge_micros, billing_failures = 0, last_notice = '', updated_at = now()
		WHERE organization_id = $1 AND pending_charge_key = $3 AND `+storeMailboxKept, orgID, now, key)
}

func (r *storeRepository) DetachMailboxCharge(ctx context.Context, orgID uuid.UUID, key string, id uuid.UUID) error {
	return r.exec(ctx, `UPDATE store_mailboxes SET pending_charge_key = NULL, updated_at = now()
		WHERE organization_id = $1 AND id = $2 AND pending_charge_key = $3`, orgID, id, key)
}

func (r *storeRepository) ReleaseMailboxCharge(ctx context.Context, orgID uuid.UUID, key string) error {
	return r.exec(ctx, `UPDATE store_mailboxes SET pending_charge_key = NULL, billing_failures = billing_failures + 1, updated_at = now()
		WHERE organization_id = $1 AND pending_charge_key = $2`, orgID, key)
}

func (r *storeRepository) ClearBillingRetry(ctx context.Context, orgID uuid.UUID) error {
	if err := r.exec(ctx, `UPDATE store_mailboxes SET billing_attempted_at = NULL, updated_at = now()
		WHERE organization_id = $1 AND billing_failures > 0 AND pending_charge_key IS NULL`, orgID); err != nil {
		return err
	}
	return r.exec(ctx, `UPDATE store_domains SET renewal_attempt_at = NULL, updated_at = now()
		WHERE organization_id = $1 AND renewal_failures > 0 AND auto_renew`, orgID)
}

func (r *storeRepository) ExtendUnmetered(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) error {
	return r.exec(ctx, `UPDATE store_mailboxes SET paid_through = paid_through + interval '1 month', updated_at = now()
		WHERE organization_id = $1 AND id = ANY($2) AND pending_charge_key IS NULL`, orgID, ids)
}

func (r *storeRepository) SetMailboxNotice(ctx context.Context, id uuid.UUID, notice string) (bool, error) {
	n, err := r.affected(ctx, `UPDATE store_mailboxes SET last_notice = $2 WHERE id = $1 AND last_notice <> $2`, id, notice)
	return n > 0, err
}

// ---- webhooks -----------------------------------------------------------

// storeLiveMailbox is a purchased mailbox the workspace still has or is still being set up.
const storeLiveMailbox = `status IN ('provisioning', 'active', 'suspended', 'cancelling')`

func (r *storeRepository) OrgsWithLiveMailboxes(ctx context.Context, limit int) ([]uuid.UUID, error) {
	return r.uuids(ctx, `SELECT DISTINCT organization_id FROM store_mailboxes WHERE `+storeLiveMailbox+` LIMIT $1`, limit)
}

// storeUnsettledCancel is an ended mailbox whose unpaid cancellation is not settled, with nothing left unpaid.
const storeUnsettledCancel = `(unpaid_cancel AND billing_failures = 0 AND ` + storeEndedMailbox + `)`

func (r *storeRepository) OrgsWithUnsettledCancels(ctx context.Context, limit int) ([]uuid.UUID, error) {
	return r.uuids(ctx, `SELECT DISTINCT organization_id FROM store_mailboxes WHERE `+storeUnsettledCancel+` LIMIT $1`, limit)
}

func (r *storeRepository) OpenMailboxes(ctx context.Context, orgID uuid.UUID) ([]models.StoreMailbox, error) {
	return r.mailboxes(ctx, `SELECT `+storeMailboxColumns+` FROM store_mailboxes
		WHERE organization_id = $1 AND (NOT `+storeEndedMailbox+` OR `+storeUnsettledCancel+`)
		ORDER BY created_at DESC, id DESC`, orgID)
}

func (r *storeRepository) HasLiveMailboxes(ctx context.Context, orgID uuid.UUID) (bool, error) {
	var ok bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM store_mailboxes WHERE organization_id = $1 AND `+storeLiveMailbox+`)`, orgID).Scan(&ok)
	return ok, err
}

func (r *storeRepository) DomainAdminMailbox(ctx context.Context, orgID, domainID uuid.UUID) (*models.StoreMailbox, error) {
	return scanStoreMailbox(r.db.QueryRow(ctx, `SELECT `+storeMailboxColumns+` FROM store_mailboxes WHERE organization_id = $1
		AND domain_id = $2 AND is_admin AND `+storeLiveMailbox+` ORDER BY created_at LIMIT 1`, orgID, domainID))
}

func (r *storeRepository) WebhookEventSeen(ctx context.Context, eventID string) (bool, error) {
	var seen bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM store_webhook_events WHERE event_id = $1)`, eventID).Scan(&seen)
	return seen, err
}

func (r *storeRepository) RecordWebhookEvent(ctx context.Context, eventID string) (bool, error) {
	n, err := r.affected(ctx, `INSERT INTO store_webhook_events (event_id) VALUES ($1) ON CONFLICT DO NOTHING`, eventID)
	return n > 0, err
}

func (r *storeRepository) PruneWebhookEvents(ctx context.Context, before time.Time) (int64, error) {
	return r.affected(ctx, `DELETE FROM store_webhook_events WHERE received_at < $1`, before)
}

func (r *storeRepository) GetEventsCursor(ctx context.Context) (string, error) {
	var raw []byte
	err := r.db.QueryRow(ctx, `SELECT value FROM admin_settings WHERE key = $1`, StoreEventsCursorKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var cursor string
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return "", nil
	}
	return cursor, nil
}

func (r *storeRepository) SetEventsCursor(ctx context.Context, cursor string) error {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return err
	}
	return r.exec(ctx, `INSERT INTO admin_settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, StoreEventsCursorKey, raw)
}

// ---- photos -------------------------------------------------------------

const storePhotoColumns = `id, organization_id, blob_key, size_bytes, created_by, created_at, used_at`

func scanStorePhoto(row pgx.Row) (*models.StorePhoto, error) {
	var p models.StorePhoto
	err := row.Scan(&p.ID, &p.OrganizationID, &p.BlobKey, &p.SizeBytes, &p.CreatedBy, &p.CreatedAt, &p.UsedAt)
	return noRows(&p, err)
}

func (r *storeRepository) CreatePhoto(ctx context.Context, p *models.StorePhoto) error {
	return r.exec(ctx, `INSERT INTO store_photos (id, organization_id, blob_key, size_bytes, created_by)
		VALUES ($1, $2, $3, $4, $5)`, p.ID, p.OrganizationID, p.BlobKey, p.SizeBytes, p.CreatedBy)
}

func (r *storeRepository) GetPhoto(ctx context.Context, orgID, id uuid.UUID) (*models.StorePhoto, error) {
	return scanStorePhoto(r.db.QueryRow(ctx, `SELECT `+storePhotoColumns+` FROM store_photos
		WHERE organization_id = $1 AND id = $2`, orgID, id))
}

func (r *storeRepository) CountPhotosSince(ctx context.Context, orgID uuid.UUID, since time.Time) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM store_photos WHERE organization_id = $1 AND created_at >= $2`, orgID, since).Scan(&n)
	return n, err
}

func (r *storeRepository) MarkPhotosUsed(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	return r.exec(ctx, `UPDATE store_photos SET used_at = now() WHERE organization_id = $1 AND id = ANY($2)`, orgID, ids)
}

func (r *storeRepository) SetMailboxPhoto(ctx context.Context, orgID, id, photoID uuid.UUID, onlyUnset bool) (bool, error) {
	n, err := r.affected(ctx, `UPDATE store_mailboxes SET photo_id = $3, photo_attempts = 0, photo_attempt_at = NULL, updated_at = now()
		WHERE organization_id = $1 AND id = $2 AND (NOT $4 OR photo_id IS NULL)
		  AND EXISTS (SELECT 1 FROM store_photos p WHERE p.id = $3 AND p.organization_id = $1)`, orgID, id, photoID, onlyUnset)
	return n > 0, err
}

func (r *storeRepository) MarkPhotoApplied(ctx context.Context, orgID, id, photoID uuid.UUID) error {
	return r.exec(ctx, `UPDATE store_mailboxes SET photo_applied_id = $3, photo_attempts = 0, photo_attempt_at = now(), updated_at = now()
		WHERE organization_id = $1 AND id = $2 AND photo_id = $3`, orgID, id, photoID)
}

func (r *storeRepository) RecordPhotoFailure(ctx context.Context, orgID, id uuid.UUID, giveUp bool) error {
	return r.exec(ctx, `UPDATE store_mailboxes
		SET photo_attempts = CASE WHEN $3 THEN 1000 ELSE photo_attempts + 1 END, photo_attempt_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, giveUp)
}

func (r *storeRepository) PendingPhotos(ctx context.Context, triedBefore time.Time, maxAttempts, limit int) ([]models.StoreMailbox, error) {
	return r.mailboxes(ctx, `SELECT `+storeMailboxColumns+` FROM store_mailboxes
		WHERE photo_id IS NOT NULL AND photo_applied_id IS DISTINCT FROM photo_id
		  AND status IN ('provisioning', 'active', 'suspended', 'cancelling')
		  AND photo_attempts < $2 AND (photo_attempt_at IS NULL OR photo_attempt_at < $1)
		ORDER BY photo_attempt_at NULLS FIRST LIMIT $3`, triedBefore, maxAttempts, limit)
}

func (r *storeRepository) SetMailboxSignature(ctx context.Context, orgID, id uuid.UUID, html, plain string) (bool, error) {
	n, err := r.affected(ctx, `UPDATE store_mailboxes SET signature_html = $3, signature_plain = $4, signature_attempts = 0, signature_attempt_at = NULL,
		updated_at = now() WHERE organization_id = $1 AND id = $2 AND signature_html IS NULL`, orgID, id, html, plain)
	return n > 0, err
}

func (r *storeRepository) MarkSignatureApplied(ctx context.Context, orgID, id uuid.UUID, local, provider bool) error {
	return r.exec(ctx, `UPDATE store_mailboxes SET
		signature_set_at = CASE WHEN $3 THEN COALESCE(signature_set_at, now()) ELSE signature_set_at END,
		signature_provider_at = CASE WHEN $4 THEN COALESCE(signature_provider_at, now()) ELSE signature_provider_at END,
		signature_attempt_at = now(), updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, local, provider)
}

func (r *storeRepository) RecordSignatureFailure(ctx context.Context, orgID, id uuid.UUID, giveUp bool) error {
	return r.exec(ctx, `UPDATE store_mailboxes
		SET signature_attempts = CASE WHEN $3 THEN 1000 ELSE signature_attempts + 1 END, signature_attempt_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, giveUp)
}

// storeSignaturePending is a live connected mailbox with a chosen signature not set everywhere, not tried since $BEFORE.
const storeSignaturePending = `signature_html IS NOT NULL AND email_account_id IS NOT NULL AND (signature_set_at IS NULL OR signature_provider_at IS NULL)
	AND status IN ('active', 'suspended', 'cancelling')
	AND signature_attempts < $MAX AND (signature_attempt_at IS NULL OR signature_attempt_at < $BEFORE)`

func signaturePending(before, maxAttempts string) string {
	return strings.NewReplacer("$BEFORE", before, "$MAX", maxAttempts).Replace(storeSignaturePending)
}

func (r *storeRepository) PendingSignatures(ctx context.Context, triedBefore time.Time, maxAttempts, limit int) ([]models.StoreMailbox, error) {
	return r.mailboxes(ctx, `SELECT `+storeMailboxColumns+` FROM store_mailboxes
		WHERE `+signaturePending("$1", "$2")+`
		ORDER BY signature_attempt_at NULLS FIRST LIMIT $3`, triedBefore, maxAttempts, limit)
}

func (r *storeRepository) ClaimSignature(ctx context.Context, orgID, id uuid.UUID, triedBefore time.Time, maxAttempts int) (*models.StoreMailbox, error) {
	return scanStoreMailbox(r.db.QueryRow(ctx, `UPDATE store_mailboxes SET signature_attempt_at = now(), updated_at = now()
		WHERE organization_id = $1 AND id = $2 AND `+signaturePending("$3", "$4")+`
		RETURNING `+storeMailboxColumns, orgID, id, triedBefore, maxAttempts))
}

func (r *storeRepository) StalePhotos(ctx context.Context, unusedBefore, usedBefore time.Time, limit int) ([]models.StorePhoto, error) {
	query := `SELECT ` + storePhotoColumns + ` FROM store_photos p
		WHERE ((p.used_at IS NULL AND p.created_at < $1) OR p.used_at < $2)
		  AND NOT EXISTS (SELECT 1 FROM store_mailboxes m WHERE m.photo_id = p.id)
		ORDER BY p.created_at LIMIT $3`
	rows, err := r.db.Query(ctx, query, unusedBefore, usedBefore, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.StorePhoto{}
	for rows.Next() {
		p, err := scanStorePhoto(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (r *storeRepository) DeletePhoto(ctx context.Context, orgID, id uuid.UUID) error {
	return r.exec(ctx, `DELETE FROM store_photos WHERE organization_id = $1 AND id = $2`, orgID, id)
}

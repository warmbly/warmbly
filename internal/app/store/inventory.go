package store

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// ---- pagination ---------------------------------------------------------

const (
	defaultPageLimit = 25
	maxPageLimit     = 100
)

func invalidCursor() *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, "invalid_cursor", "The cursor is not valid.")
}

func pageArgs(cursor string, limit int) (*models.StorePage, int, error) {
	if limit == 0 {
		limit = defaultPageLimit
	}
	if limit < 1 || limit > maxPageLimit {
		return nil, 0, errx.NewWithIdentifier(errx.BadRequest, "invalid_limit", "limit must be between 1 and 100.")
	}
	if cursor == "" {
		return nil, limit, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, 0, invalidCursor()
	}
	ts, id, ok := strings.Cut(string(raw), "|")
	nanos, err := strconv.ParseInt(ts, 10, 64)
	if !ok || err != nil {
		return nil, 0, invalidCursor()
	}
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, 0, invalidCursor()
	}
	return &models.StorePage{CreatedAt: time.Unix(0, nanos).UTC(), ID: uid}, limit, nil
}

func nextPage(more bool, last func() models.StorePage) Pagination {
	if !more {
		return Pagination{}
	}
	p := last()
	c := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(p.CreatedAt.UnixNano(), 10) + "|" + p.ID.String()))
	return Pagination{NextCursor: &c, HasMore: true}
}

// opKey is a fresh Idempotency-Key for one user action. The route's own
// Idempotency-Key replays a retried request, so GraphKit's key only has to be
// unique: a repeated cancel, rename or resume is a new action, not a replay.
func opKey(op string, id uuid.UUID) string {
	return "store:" + op + ":" + id.String() + ":" + uuid.NewString()
}

// ---- sync from GraphKit -------------------------------------------------

// addMonth is one calendar month on, clamped to the month's last day.
func addMonth(t time.Time) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m+1, 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	last := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, min(d, last)-1)
}

// upsertMailbox stores GraphKit's current mailbox. A new row is paid for its
// first month (the checkout took it) at monthly; an existing row keeps its price.
func (s *Service) upsertMailbox(ctx context.Context, orgID uuid.UUID, orderID *uuid.UUID, m *graphkit.Mailbox, monthly int64) (*models.StoreMailbox, error) {
	dom, err := s.Repo.GetDomainByGraphKitID(ctx, orgID, m.DomainID)
	if err != nil {
		return nil, err
	}
	if dom == nil {
		d, gerr := s.api().GetDomain(ctx, m.DomainID)
		if gerr != nil {
			return nil, gerr
		}
		if dom, err = s.upsertDomain(ctx, orgID, orderID, d); err != nil || dom == nil {
			return nil, err
		}
	}
	if monthly <= 0 {
		monthly = s.retail(ctx).MailboxMonthly()
	}
	created := m.CreatedAt
	if created.IsZero() {
		created = s.now()
	}
	row, err := s.Repo.UpsertMailbox(ctx, &models.StoreMailbox{ID: uuid.New(), OrganizationID: orgID, DomainID: dom.ID, OrderID: orderID,
		GraphKitMailboxID: m.ID, Email: strings.ToLower(m.Email), Provider: m.Provider, Status: m.Status, FirstName: m.FirstName, LastName: m.LastName, IsAdmin: m.IsAdmin,
		MonthlyRetailMicros: models.Micro(monthly), PaidThrough: addMonth(created.UTC()), PeriodEnd: m.CurrentPeriodEnd,
		CancelsAt: m.CancelsAt, ConnectMethod: m.Connection.Method})
	if err != nil || row == nil {
		return row, err
	}
	s.applyPriceChange(ctx, row, m)
	// The first mailbox sets the workspace's billing day.
	if _, err := s.ensureAnchor(ctx, orgID); err != nil {
		log.Warn().Err(err).Str("org_id", orgID.String()).Msg("store: setting the billing day failed")
	}
	s.publishMailbox(ctx, row)
	return s.retire(ctx, row), nil
}

func (s *Service) publishMailbox(ctx context.Context, m *models.StoreMailbox) {
	ev := pubsub.StoreEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventStoreMailboxUpdated}, ID: m.ID.String(), Status: m.Status,
		DomainID: m.DomainID.String(), ConnectState: m.ConnectState}
	if m.EmailAccountID != nil {
		ev.EmailAccountID = m.EmailAccountID.String()
	}
	s.publish(ctx, m.OrganizationID, ev)
}

// ---- views --------------------------------------------------------------

// mailboxView is a purchased mailbox; unplanned marks a workspace without a plan, which pauses a live one.
func mailboxView(m *models.StoreMailbox, unplanned bool) Mailbox {
	return Mailbox{ID: m.ID, Email: m.Email, DomainID: m.DomainID, Provider: m.Provider, Status: m.Status, FirstName: m.FirstName,
		LastName: m.LastName, IsAdmin: m.IsAdmin, Paused: unplanned && liveStatus(m.Status), MonthlyMicros: int64(m.MonthlyRetailMicros),
		PaidThrough: m.PaidThrough, CancelsAt: m.CancelsAt, CancelledAt: m.CancelledAt, EmailAccountID: m.EmailAccountID, ConnectState: m.ConnectState,
		ActionRequired: actionRequired(m), ActionReason: actionReason(m), Photo: photoState(m), CreatedAt: m.CreatedAt}
}

// photoState is none, pending (chosen, not taken by the provider yet) or set.
func photoState(m *models.StoreMailbox) string {
	switch {
	case m.PhotoID == nil:
		return "none"
	case m.PhotoAppliedID != nil && *m.PhotoAppliedID == *m.PhotoID:
		return "set"
	}
	return "pending"
}

// liveStatus is a mailbox the workspace still has: sending and warmup on it follow the plan.
func liveStatus(status string) bool {
	switch status {
	case graphkit.MailboxActive, graphkit.MailboxSuspended, graphkit.MailboxCancelling:
		return true
	}
	return false
}

// unplanned reports a workspace whose purchased mailboxes are paused for lack of a
// plan. A partner-managed workspace follows its partner, and an unreadable plan pauses nothing.
func (s *Service) unplanned(ctx context.Context, orgID uuid.UUID) bool {
	if s.managed(ctx, orgID) {
		return false
	}
	ok, err := s.planActive(ctx, orgID)
	return err == nil && !ok
}

// ---- mailboxes ----------------------------------------------------------

// ListMailboxes is the workspace's purchased mailboxes, newest first.
func (s *Service) ListMailboxes(ctx context.Context, orgID uuid.UUID, cursor string, limit int) (*Page[Mailbox], error) {
	if !s.readable() {
		return nil, errDisabled
	}
	after, limit, err := pageArgs(cursor, limit)
	if err != nil {
		return nil, err
	}
	rows, err := s.Repo.ListMailboxes(ctx, orgID, after, limit+1)
	if err != nil {
		return nil, err
	}
	out := &Page[Mailbox]{Data: []Mailbox{}}
	unplanned := s.unplanned(ctx, orgID)
	for i := range rows {
		if i == limit {
			break
		}
		out.Data = append(out.Data, mailboxView(&rows[i], unplanned))
	}
	out.Pagination = nextPage(len(rows) > limit, func() models.StorePage {
		return models.StorePage{CreatedAt: rows[limit-1].CreatedAt, ID: rows[limit-1].ID}
	})
	return out, nil
}

func (s *Service) mailbox(ctx context.Context, orgID, id uuid.UUID) (*models.StoreMailbox, error) {
	if !s.Enabled() {
		return nil, errDisabled
	}
	m, err := s.Repo.GetMailbox(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errx.ErrNotFound
	}
	return m, nil
}

// GetMailbox is one purchased mailbox.
func (s *Service) GetMailbox(ctx context.Context, orgID, id uuid.UUID) (*Mailbox, error) {
	m, err := s.mailbox(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	out := mailboxView(m, s.unplanned(ctx, orgID))
	return &out, nil
}

// RenameMailbox changes the sender name on the mailbox itself.
func (s *Service) RenameMailbox(ctx context.Context, orgID, id uuid.UUID, p MailboxPatch) (*Mailbox, error) {
	m, err := s.mailbox(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	first, last := m.FirstName, m.LastName
	if p.FirstName != nil {
		v, xerr := displayname.Validate("First name", *p.FirstName, displayname.Person, false)
		if xerr != nil {
			return nil, xerr
		}
		first = v
	}
	if p.LastName != nil {
		v, xerr := displayname.Validate("Last name", *p.LastName, displayname.Person, false)
		if xerr != nil {
			return nil, xerr
		}
		last = v
	}
	patch := graphkit.MailboxPatch{FirstName: &first, LastName: &last}
	if _, gerr := s.api().UpdateMailbox(ctx, m.GraphKitMailboxID, patch, opKey("rename", m.ID)); gerr != nil {
		return nil, s.upstream(ctx, "mailbox.rename", gerr)
	}
	if err := s.Repo.SetMailboxNames(ctx, orgID, id, first, last); err != nil {
		return nil, err
	}
	return s.reloadMailbox(ctx, orgID, id)
}

func (s *Service) reloadMailbox(ctx context.Context, orgID, id uuid.UUID) (*Mailbox, error) {
	m, err := s.mailbox(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	// A mailbox still being set up is cancelled at once.
	m = s.retire(ctx, m)
	s.publishMailbox(ctx, m)
	out := mailboxView(m, s.unplanned(ctx, orgID))
	return &out, nil
}

// CancelMailbox cancels at the end of the paid month; nothing more is charged for it.
func (s *Service) CancelMailbox(ctx context.Context, orgID, id uuid.UUID) (*Mailbox, error) {
	m, err := s.mailbox(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if m.Status != graphkit.MailboxActive && m.Status != graphkit.MailboxSuspended && m.Status != graphkit.MailboxCancelling {
		return nil, errx.NewWithIdentifier(errx.Conflict, CodeMailboxNotCancellable, "This mailbox can't be cancelled now.")
	}
	got, gerr := s.api().CancelMailbox(ctx, m.GraphKitMailboxID, opKey("cancel", m.ID))
	if gerr != nil {
		return nil, s.upstream(ctx, "mailbox.cancel", gerr)
	}
	cancels := got.CancelsAt
	if cancels == nil {
		t := m.PaidThrough
		cancels = &t
	}
	// The customer's own cancellation: an unpaid one it replaces is no longer billed or resumed.
	if err := s.Repo.SetMailboxCancel(ctx, orgID, id, got.Status, cancels); err != nil {
		return nil, err
	}
	// Read after the write: a month a bundle collected meanwhile is seen and goes back to the balance.
	if row, err := s.Repo.GetMailbox(ctx, orgID, id); err == nil && row != nil {
		s.refundUnstarted(ctx, row)
	}
	return s.reloadMailbox(ctx, orgID, id)
}

// ResumeMailbox undoes a pending cancellation; billing picks the mailbox up again.
func (s *Service) ResumeMailbox(ctx context.Context, orgID, id uuid.UUID) (*Mailbox, error) {
	m, err := s.mailbox(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if m.CancelsAt == nil {
		return s.reloadMailbox(ctx, orgID, id)
	}
	if m.Status == graphkit.MailboxCancelled {
		return nil, errx.NewWithIdentifier(errx.Conflict, CodeMailboxNotCancellable, "This mailbox is already cancelled.")
	}
	if m.PendingChargeKey != nil {
		return nil, chargeInProgress()
	}
	// Resuming makes GraphKit bill the next period, so a month no charge covered is paid first.
	key, err := s.payMonth(ctx, m)
	if err != nil {
		return nil, err
	}
	got, gerr := s.api().ResumeMailbox(ctx, m.GraphKitMailboxID, opKey("resume", m.ID))
	if gerr != nil {
		if key != "" && graphkit.Definitive(gerr) {
			// Refused: the month will not start, so its share goes back and the mailbox leaves the charge.
			_ = s.finishBatch(context.WithoutCancel(ctx), orgID, key, []models.StoreMailbox{*m})
		}
		// No answer: the charge stays reserved and the billing pass settles or refunds it once GraphKit's state is known.
		return nil, s.upstream(ctx, "mailbox.resume", gerr)
	}
	if err := s.Repo.SetMailboxCancel(ctx, orgID, id, got.Status, nil); err != nil {
		return nil, err
	}
	if key != "" {
		bg := context.WithoutCancel(ctx)
		if _, err := s.Repo.SettleMailboxCharge(bg, orgID, key, s.now()); err != nil {
			return nil, err
		}
		if err := s.finishBatch(bg, orgID, key, []models.StoreMailbox{*m}); err != nil {
			return nil, err
		}
	}
	return s.reloadMailbox(ctx, orgID, id)
}

// payMonth collects a resumed mailbox's next month on its own when no charge
// covered it, under a key reserved on the mailbox first: the month is credited
// only by settling that key, so a retry, a second resume or a lost answer can
// neither charge twice nor give the month away. "" when nothing was due.
func (s *Service) payMonth(ctx context.Context, m *models.StoreMailbox) (string, error) {
	if !s.metered() || m.MonthlyRetailMicros <= 0 || !m.PaidThrough.Before(s.paidHorizon(ctx, m.OrganizationID)) {
		return "", nil
	}
	key := "store:month:" + m.ID.String() + ":" + uuid.NewString()[:8]
	n, err := s.Repo.ReserveMailboxCharge(ctx, m.OrganizationID, []models.StoreMailbox{*m}, key)
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", chargeInProgress()
	}
	m.PendingChargeMicros = m.MonthlyRetailMicros
	reason := fmt.Sprintf("Mailbox · %s · %s · %s", m.Email, usd(int64(m.MonthlyRetailMicros)), monthLabel(m.PaidThrough))
	err = s.collect(ctx, m.OrganizationID, int64(m.MonthlyRetailMicros), key, reason, false)
	if err == nil {
		_, err = s.Credits.ChargeAmount(ctx, m.OrganizationID, m.MonthlyRetailMicros, reason, key)
	}
	if err == nil {
		return key, nil
	}
	bg := context.WithoutCancel(ctx)
	if prior, perr := s.Credits.ChargeOf(bg, m.OrganizationID, key); perr != nil || prior != nil {
		// Taken after all, or unknown: the billing pass finishes the reservation.
		return "", err
	}
	// Nothing was taken: the reservation goes, and the next resume starts on a fresh key.
	_ = s.Repo.DetachMailboxCharge(bg, m.OrganizationID, key, m.ID)
	if errors.Is(err, credits.ErrInsufficientCredits) {
		return "", errx.NewWithIdentifier(errx.PaymentRequired, CodeInsufficientBalance, "Your balance can't cover this mailbox's month. Add money and try again.")
	}
	return "", err
}

func chargeInProgress() error {
	return errx.NewWithIdentifier(errx.Conflict, CodeChargeInProgress, "A payment for this mailbox is still going through. Try again in a minute.")
}

// SignIn reads a mailbox's sign-in once: for the admin mailbox, the Google
// Workspace or Microsoft 365 admin. Nothing is kept or logged.
func (s *Service) SignIn(ctx context.Context, orgID, id uuid.UUID) (*SignIn, error) {
	m, err := s.mailbox(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if m.Status != graphkit.MailboxActive && m.Status != graphkit.MailboxCancelling {
		return nil, errx.NewWithIdentifier(errx.Conflict, CodeSignInUnavailable, "This mailbox has no sign-in to show right now.")
	}
	c, gerr := s.api().MailboxCredentials(ctx, m.GraphKitMailboxID)
	if gerr != nil {
		return nil, s.upstream(ctx, "mailbox.signin", gerr)
	}
	out := &SignIn{Email: c.Email, Username: c.Username, Password: c.Password, LoginURL: c.LoginURL}
	if out.Username == "" {
		out.Username = m.Email
	}
	if c.AdminConsoleURL != "" {
		admin := c.AdminConsoleURL
		out.AdminConsoleURL = &admin
	}
	return out, nil
}

// actionRequired is the step a mailbox waits on, only while it is still waiting to connect.
func actionRequired(m *models.StoreMailbox) *string {
	if m.ActionRequired == nil || m.EmailAccountID != nil || m.ConnectState != models.StoreConnectWaiting {
		return nil
	}
	a := *m.ActionRequired
	return &a
}

// actionReason is why the step fell to a person, alongside actionRequired.
func actionReason(m *models.StoreMailbox) *string {
	if actionRequired(m) == nil || m.ActionReason == nil {
		return nil
	}
	r := *m.ActionReason
	return &r
}

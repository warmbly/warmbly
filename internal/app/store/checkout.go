package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/balance"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
	"github.com/warmbly/warmbly/internal/repository"
)

var (
	domainRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,24}$`)
	localRe  = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]{0,62}[a-z0-9])?$`)
)

// chargeWindow is how long an order's own request may still be charging it.
const chargeWindow = 2 * time.Minute

// quoteMargin treats a quote this close to expiry as expired, so GraphKit never sees it lapse mid-order.
const quoteMargin = 30 * time.Second

func itemsErr(msg string) *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, CodeInvalidItems, msg)
}

// cleanItems validates and normalizes what the buyer asked for.
func cleanItems(in []graphkit.QuoteItem) ([]graphkit.QuoteItem, bool, error) {
	if len(in) == 0 || len(in) > 20 {
		return nil, false, itemsErr("Add between 1 and 20 items.")
	}
	out := make([]graphkit.QuoteItem, 0, len(in))
	hasDomain := false
	for _, it := range in {
		it.Domain = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(it.Domain, ".")))
		if !domainRe.MatchString(it.Domain) {
			return nil, false, itemsErr("Enter a valid domain name.")
		}
		switch it.Type {
		case graphkit.ItemDomain:
			// No term means the TLD's minimum; GraphKit refuses one outside the TLD's range.
			if it.Years < 0 || it.Years > 10 {
				return nil, false, itemsErr("A domain is registered for 1 to 10 years.")
			}
			it.Provider, it.Mailboxes = "", nil
			hasDomain = true
		case graphkit.ItemMailboxes:
			if it.Provider != graphkit.ProviderGoogle && it.Provider != graphkit.ProviderMicrosoft {
				return nil, false, itemsErr("Choose Google or Microsoft for the mailboxes.")
			}
			if len(it.Mailboxes) == 0 || len(it.Mailboxes) > 5 {
				return nil, false, errx.NewWithIdentifier(errx.Unprocessable, CodeMailboxLimit, "A domain holds 1 to 5 mailboxes.")
			}
			it.Years = 0
			admins := 0
			for _, m := range it.Mailboxes {
				if m.Admin {
					admins++
				}
			}
			if admins > 1 {
				return nil, false, itemsErr("Pick one admin mailbox per domain.")
			}
			seen := map[string]bool{}
			for i, m := range it.Mailboxes {
				m.LocalPart = strings.ToLower(strings.TrimSpace(m.LocalPart))
				if !localRe.MatchString(m.LocalPart) {
					return nil, false, errx.NewWithIdentifier(errx.BadRequest, CodeAddressInvalid, m.LocalPart+"@"+it.Domain+" can't be used. Use letters, digits, dots, dashes or underscores.")
				}
				if seen[m.LocalPart] {
					return nil, false, errx.NewWithIdentifier(errx.Conflict, CodeAddressTaken, m.LocalPart+"@"+it.Domain+" is listed twice.")
				}
				seen[m.LocalPart] = true
				first, xerr := displayname.Validate("First name", m.FirstName, displayname.Person, false)
				if xerr != nil {
					return nil, false, xerr
				}
				last, xerr := displayname.Validate("Last name", m.LastName, displayname.Person, false)
				if xerr != nil {
					return nil, false, xerr
				}
				m.FirstName, m.LastName = first, last
				it.Mailboxes[i] = m
			}
		default:
			return nil, false, itemsErr("An item is a domain or mailboxes.")
		}
		out = append(out, it)
	}
	return out, hasDomain, nil
}

// retailQuote prices a GraphKit quote for the buyer.
func retailQuote(r instancesettings.StoreRetail, q *graphkit.Quote, items []graphkit.QuoteItem) quoteSnapshot {
	snap := quoteSnapshot{GraphKitQuoteID: q.ID, Items: items, Wholesale: q.Lines, MailboxMonthly: r.MailboxMonthly(), ExpiresAt: q.ExpiresAt}
	for _, l := range q.Lines {
		line := QuoteLine{Type: l.Type, Domain: l.Domain, Years: l.Years, Provider: l.Provider, Count: l.Count, NameserversChange: l.NameserversChange}
		if l.AdminEmail != "" {
			admin := l.AdminEmail
			line.AdminEmail = &admin
		}
		switch l.Type {
		case graphkit.ItemDomain:
			years := int64(max(l.Years, 1))
			line.AmountMicros = retailDomain(r, l.AmountMicros/years) * years
			line.RenewMicros = retailDomain(r, l.RenewMicros)
			snap.RenewalMicros += line.RenewMicros
		case graphkit.ItemMailboxes:
			line.MonthlyMicros = r.MailboxMonthly()
			line.AmountMicros = int64(l.Count) * line.MonthlyMicros
			snap.RecurringMicros += line.AmountMicros
		}
		snap.DueNowMicros += line.AmountMicros
		snap.Lines = append(snap.Lines, line)
	}
	return snap
}

// photoIDs are the photos the order's mailboxes take.
func (snap quoteSnapshot) photoIDs() []uuid.UUID {
	out := make([]uuid.UUID, 0, len(snap.Photos))
	seen := map[uuid.UUID]bool{}
	for _, id := range snap.Photos {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func (snap quoteSnapshot) view(id uuid.UUID, pay Payment) *Quote {
	return &Quote{ID: id, ExpiresAt: snap.ExpiresAt, Lines: snap.Lines, DueNowMicros: snap.DueNowMicros,
		RecurringMonthlyMicros: snap.RecurringMicros, YearlyDomainRenewalMicros: snap.RenewalMicros, Payment: pay}
}

// payment is how the workspace would pay due now; addMonthly is what the purchase adds to its monthly mailboxes.
func (s *Service) payment(ctx context.Context, orgID uuid.UUID, due, addMonthly int64) Payment {
	if !s.metered() {
		return Payment{}
	}
	bal, card := s.balanceAndCard(ctx, orgID)
	cfg := s.reloadSettings(ctx, orgID)
	p := plan(due, bal, card != nil, s.cardFloor(ctx, cfg))
	if p.CardChargeMicros > 0 && card != nil {
		// The reload the debit would set off rides on this card charge, so the card is charged once.
		extra := s.reloadHeadroom(ctx, orgID, cfg, p.CardChargeMicros-due)
		p.CardChargeMicros += extra
		p.BalanceAfterMicros += extra
	}
	p.Card = card
	ar := s.reloadPlan(ctx, orgID, cfg, card != nil, addMonthly)
	p.AutoReload = &ar
	return p
}

// CreateQuote prices what the buyer asked for and keeps the price for fifteen minutes.
func (s *Service) CreateQuote(ctx context.Context, orgID uuid.UUID, in QuoteRequest) (*Quote, error) {
	if err := s.requireBuyer(ctx, orgID); err != nil {
		return nil, err
	}
	items, hasDomain, err := cleanItems(in.Items)
	if err != nil {
		return nil, err
	}
	photos, err := s.checkPhotos(ctx, orgID, in.Photos, items)
	if err != nil {
		return nil, err
	}
	sigs, err := cleanSignatures(in.Signatures, items)
	if err != nil {
		return nil, err
	}
	if hasDomain {
		reg, err := s.savedRegistrant(ctx, orgID)
		if err != nil {
			return nil, err
		}
		if reg == nil {
			return nil, errx.NewWithIdentifier(errx.Unprocessable, CodeRegistrantRequired, "Add the domain owner's contact details first.")
		}
	}
	if err := s.ensureAccount(ctx, orgID); err != nil {
		return nil, err
	}
	snap, id, err := s.quote(ctx, orgID, items, photos, sigs)
	if err != nil {
		return nil, err
	}
	return snap.view(id, s.payment(ctx, orgID, snap.DueNowMicros, snap.RecurringMicros)), nil
}

// quote asks GraphKit for a quote and keeps its retail snapshot with the photos and signatures the mailboxes take.
func (s *Service) quote(ctx context.Context, orgID uuid.UUID, items []graphkit.QuoteItem, photos map[string]uuid.UUID, sigs map[string]MailboxSignature) (quoteSnapshot, uuid.UUID, error) {
	id := uuid.New()
	q, gerr := s.api().CreateQuote(ctx, externalID(orgID), items, "store:quote:"+id.String())
	if graphkit.CodeOf(gerr) == graphkit.CodeAccountNotFound {
		// The account we recorded is gone upstream (another partner environment, a reset simulator): open it again.
		if err := s.reopenAccount(ctx, orgID); err != nil {
			return quoteSnapshot{}, uuid.Nil, err
		}
		q, gerr = s.api().CreateQuote(ctx, externalID(orgID), items, "store:quote:"+id.String()+":reopened")
	}
	if gerr != nil {
		return quoteSnapshot{}, uuid.Nil, s.upstream(ctx, "quote", gerr)
	}
	snap := retailQuote(s.retail(ctx), q, items)
	snap.Photos, snap.Signatures = photos, sigs
	raw, _ := json.Marshal(snap)
	if err := s.Repo.CreateQuote(ctx, &models.StoreQuote{ID: id, OrganizationID: orgID, GraphKitQuoteID: q.ID, Snapshot: raw, ExpiresAt: q.ExpiresAt}); err != nil {
		return quoteSnapshot{}, uuid.Nil, err
	}
	return snap, id, nil
}

// PlaceOrder charges the workspace and orders what the quote holds. The buyer
// is only ever charged the total they saw: a quote that expired is priced
// again, and a different total is refused with the fresh quote. One quote,
// however often it is retried or priced again, yields at most one order.
func (s *Service) PlaceOrder(ctx context.Context, orgID, userID uuid.UUID, in OrderRequest) (*Order, error) {
	// A retry of an order already placed finishes it while sales are paused.
	if err := s.requireBuyer(ctx, orgID); err != nil && (!errors.Is(err, errSalesPaused) || !s.quoteOrdered(ctx, orgID, in.QuoteID)) {
		return nil, err
	}
	fwd, xerr := cleanForwarding(in.ForwardingURL)
	if xerr != nil {
		return nil, xerr
	}
	for range 3 {
		o, err := s.placeOrder(ctx, orgID, userID, in, fwd)
		if !errors.Is(err, repository.ErrStoreQuoteReplaced) {
			return o, err
		}
		// Another request priced the quote again first: follow it to the replacement.
	}
	return nil, errx.NewWithIdentifier(errx.Conflict, CodeChargeInProgress, "This order is already being placed. Try again in a minute.")
}

// currentQuote follows a quote to the one it was last priced again as.
func (s *Service) currentQuote(ctx context.Context, orgID, id uuid.UUID) (*models.StoreQuote, error) {
	q, err := s.Repo.GetQuote(ctx, orgID, id)
	for range 10 {
		if err != nil || q == nil || q.ReplacedBy == nil {
			break
		}
		next, nerr := s.Repo.GetQuote(ctx, orgID, *q.ReplacedBy)
		if nerr != nil || next == nil {
			return q, nerr
		}
		q = next
	}
	return q, err
}

func (s *Service) placeOrder(ctx context.Context, orgID, userID uuid.UUID, in OrderRequest, fwd string) (*Order, error) {
	stored, err := s.currentQuote(ctx, orgID, in.QuoteID)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, errx.NewWithIdentifier(errx.NotFound, CodeQuoteNotFound, "This price has expired. Review your cart again.")
	}
	var snap quoteSnapshot
	if err := json.Unmarshal(stored.Snapshot, &snap); err != nil {
		return nil, err
	}
	quoteID := stored.ID
	expected := snap.DueNowMicros
	if in.ExpectedDueNowMicros != nil {
		expected = *in.ExpectedDueNowMicros
	}
	if !s.now().Add(quoteMargin).Before(stored.ExpiresAt) {
		// A retried checkout of a quote already ordered resumes that order.
		if existing, err := s.orderForQuote(ctx, orgID, stored.ID); err != nil || existing != nil {
			return existing, err
		}
		fresh, id, err := s.quote(ctx, orgID, snap.Items, snap.Photos, snap.Signatures)
		if err != nil {
			return nil, err
		}
		won, ordered, err := s.Repo.ReplaceQuote(ctx, orgID, stored.ID, id)
		switch {
		case err != nil:
			return nil, err
		case ordered:
			return s.orderForQuote(ctx, orgID, stored.ID)
		case won != id:
			// Priced again by another request first: use its quote.
			return nil, repository.ErrStoreQuoteReplaced
		}
		snap, quoteID = fresh, id
	}
	if snap.DueNowMicros != expected {
		return nil, &PriceChangedError{Quote: snap.view(quoteID, s.payment(ctx, orgID, snap.DueNowMicros, snap.RecurringMicros))}
	}
	raw, _ := json.Marshal(snap)
	rowID := uuid.New()
	row, existed, err := s.Repo.CreateOrder(ctx, &models.StoreOrder{ID: rowID, OrganizationID: orgID, QuoteID: quoteID,
		Status: models.StoreOrderCharging, Quote: raw, DueNowMicros: models.Micro(snap.DueNowMicros),
		ChargeKey: "store:order:" + rowID.String(), ForwardingURL: fwd, CreatedBy: &userID})
	if err != nil {
		return nil, err
	}
	if existed && (row.Status != models.StoreOrderCharging || row.GraphKitOrderID != nil) {
		return s.orderView(ctx, row)
	}
	if err := s.Repo.MarkPhotosUsed(ctx, orgID, snap.photoIDs()); err != nil {
		return nil, err
	}
	if !existed || row.ChargedMicros == 0 {
		if err := s.charge(ctx, row, snap, !existed); err != nil {
			return nil, err
		}
	}
	o, err := s.submit(ctx, row, snap)
	if err == nil {
		s.adoptAutoReload(ctx, orgID, in.EnableAutoReload)
	}
	return o, err
}

// quoteOrdered reports a quote that already made an order.
func (s *Service) quoteOrdered(ctx context.Context, orgID, quoteID uuid.UUID) bool {
	q, err := s.currentQuote(ctx, orgID, quoteID)
	if err != nil || q == nil {
		return false
	}
	row, err := s.Repo.GetOrderByQuote(ctx, orgID, q.ID)
	return err == nil && row != nil
}

func (s *Service) orderForQuote(ctx context.Context, orgID, quoteID uuid.UUID) (*Order, error) {
	row, err := s.Repo.GetOrderByQuote(ctx, orgID, quoteID)
	if err != nil || row == nil {
		return nil, err
	}
	return s.orderView(ctx, row)
}

func lineKey(chargeKey string, i int) string { return chargeKey + ":" + strconv.Itoa(i) }

func monthLabel(t time.Time) string { return t.Format("Jan 2006") }

func usd(m int64) string { return fmt.Sprintf("$%.2f", float64(m)/1_000_000) }

// lineReason is the ledger line a workspace sees on its balance and invoice.
func lineReason(l QuoteLine, at time.Time) string {
	if l.Type == graphkit.ItemDomain {
		years := "1 year"
		if l.Years > 1 {
			years = strconv.Itoa(l.Years) + " years"
		}
		return "Domain registration · " + l.Domain + " · " + years
	}
	return fmt.Sprintf("Mailboxes · %d × %s · %s · %s", l.Count, usd(l.MonthlyMicros), l.Domain, monthLabel(at))
}

// describe is the receipt line for several ledger lines.
func describe(lines []QuoteLine, at time.Time) string {
	parts := make([]string, 0, len(lines))
	for _, l := range lines {
		if l.AmountMicros > 0 {
			parts = append(parts, lineReason(l, at))
		}
	}
	return strings.Join(parts, "; ")
}

// charge takes due now from the balance, charging the saved card for the
// shortfall first. One ledger line per quote line, each under its own key,
// so a failed item refunds exactly what it cost and a retry never charges twice.
// created is false for an order another request started: that request owns
// the order's fate, so this one never deletes it or refunds what it charged.
func (s *Service) charge(ctx context.Context, row *models.StoreOrder, snap quoteSnapshot, created bool) error {
	bg := context.WithoutCancel(ctx)
	if !s.metered() || snap.DueNowMicros == 0 {
		return s.Repo.MarkOrderCharged(bg, row.OrganizationID, row.ID, 0)
	}
	orgID := row.OrganizationID
	// Lines already on the ledger, after a crash, are not collected again.
	var owed int64
	for i, l := range snap.Lines {
		if l.AmountMicros <= 0 {
			continue
		}
		prior, err := s.Credits.ChargeOf(ctx, orgID, lineKey(row.ChargeKey, i))
		if err != nil {
			return err
		}
		if prior == nil {
			owed += l.AmountMicros
		}
	}
	if !created && owed > 0 && s.now().Sub(row.UpdatedAt) < chargeWindow {
		// The request that started this order may still be charging it.
		return errx.NewWithIdentifier(errx.Conflict, CodeChargeInProgress, "This order is already being placed. Try again in a minute.")
	}
	if err := s.collect(ctx, orgID, owed, row.ChargeKey, describe(snap.Lines, row.CreatedAt), false); err != nil {
		// A card payment still going through keeps the order, so the retry resumes the same charge.
		if created && owed == snap.DueNowMicros && errCode(err) != CodePaymentProcessing {
			_ = s.Repo.DeleteUnchargedOrder(bg, orgID, row.ID)
		}
		return err
	}
	var taken models.Micro
	for i, l := range snap.Lines {
		if l.AmountMicros <= 0 {
			continue
		}
		got, err := s.Credits.ChargeAmount(ctx, orgID, micro(l.AmountMicros), lineReason(l, row.CreatedAt), lineKey(row.ChargeKey, i))
		if err != nil {
			if created {
				s.abandonCharge(bg, row, snap)
			}
			if errors.Is(err, credits.ErrInsufficientCredits) {
				return errx.NewWithIdentifier(errx.PaymentRequired, CodeInsufficientBalance, "Your balance can't cover this order. Add money and try again.")
			}
			return err
		}
		taken += got
	}
	row.ChargedMicros = taken
	return s.Repo.MarkOrderCharged(bg, orgID, row.ID, taken)
}

// abandonCharge gives back the lines of an order that could not be fully
// charged. Refunded in full, the order goes so the buyer can retry it; a refund
// that did not go through keeps it as failed for the reconciler to finish.
func (s *Service) abandonCharge(ctx context.Context, row *models.StoreOrder, snap quoteSnapshot) {
	back, ok := s.refundLines(ctx, row, snap)
	if ok {
		_ = s.Repo.DeleteUnchargedOrder(ctx, row.OrganizationID, row.ID)
		return
	}
	_ = s.Repo.MarkOrderFailed(ctx, row.OrganizationID, row.ID, CodeInsufficientBalance, back, true)
}

// collect is the one path every store charge takes before its debit: what
// the balance covers stays there, and the saved card is charged for the rest
// in the same pass, whatever the workspace's automatic reload says. The card
// charge is keyed by the store charge's key, so a retry after a crash between
// the card and the debit finds the money already on the balance, or resumes
// the same card charge, and never charges the card twice. offSession is a
// renewal nobody is present for. An agency's client is charged on its own
// balance (with what the agency lets it draw) and its own card.
func (s *Service) collect(ctx context.Context, orgID uuid.UUID, due int64, key, description string, offSession bool) error {
	if !s.metered() || due <= 0 {
		return nil
	}
	pay := s.payment(ctx, orgID, due, 0)
	if pay.CardChargeMicros == 0 {
		return nil
	}
	if s.Cards == nil || pay.NeedsPaymentMethod {
		return &PaymentMethodError{ShortMicros: pay.CardChargeMicros}
	}
	res, err := s.Cards.ChargeStore(ctx, orgID, micro(pay.CardChargeMicros), "store:card:"+key, description, offSession)
	var declined *balance.DeclinedError
	switch {
	case errors.As(err, &declined):
		return &CardError{Code: declined.Code}
	case errors.Is(err, balance.ErrNoPaymentMethod):
		return &PaymentMethodError{ShortMicros: pay.CardChargeMicros}
	case errors.Is(err, balance.ErrChargeInFlight):
		return errx.NewWithIdentifier(errx.Conflict, CodeChargeInProgress, "Another payment is still going through. Try again in a minute.")
	case err != nil:
		return err
	case res.Status != "paid":
		return errx.NewWithIdentifier(errx.Conflict, CodePaymentProcessing, "Your card payment is still going through. Try again in a minute.")
	}
	return nil
}

// errCode is the Warmbly code behind err, "" when it is not an errx error.
func errCode(err error) string {
	var xerr *errx.Error
	if errors.As(err, &xerr) {
		return xerr.ResponseCode()
	}
	return ""
}

// refundLines gives back every line the order charged, each once and never
// past what the order took; ok is false while any refund did not go through.
func (s *Service) refundLines(ctx context.Context, row *models.StoreOrder, snap quoteSnapshot) (models.Micro, bool) {
	if !s.metered() {
		return 0, true
	}
	ctx = context.WithoutCancel(ctx)
	var back models.Micro
	ok := true
	for i, l := range snap.Lines {
		if l.AmountMicros <= 0 {
			continue
		}
		got, err := s.refundOrderLine(ctx, row, i, "Refund · "+lineReason(l, row.CreatedAt))
		if err != nil {
			log.Error().Err(err).Str("order_id", row.ID.String()).Int("line", i).Msg("store: refund failed; the reconciler retries")
			ok = false
			continue
		}
		back += got
	}
	return back, ok
}

// refundOrderLine gives back one line's debit, capped by what the whole order has left to refund.
func (s *Service) refundOrderLine(ctx context.Context, row *models.StoreOrder, i int, reason string) (models.Micro, error) {
	key := lineKey(row.ChargeKey, i)
	return s.Credits.RefundStore(context.WithoutCancel(ctx), row.OrganizationID, orderPrefix(row), key, key+":refund", 0, reason)
}

// orderPrefix starts the key of every line an order charged and every refund of it.
func orderPrefix(row *models.StoreOrder) string { return row.ChargeKey + ":" }

// submit places the charged order with GraphKit. The order's charge key is
// its Idempotency-Key, so a retry after an ambiguous failure never orders twice.
func (s *Service) submit(ctx context.Context, row *models.StoreOrder, snap quoteSnapshot) (*Order, error) {
	orgID := row.OrganizationID
	order, gerr := s.api().CreateOrder(ctx, externalID(orgID), snap.GraphKitQuoteID, row.ForwardingURL, row.ChargeKey)
	if gerr != nil {
		ge, _ := graphkit.AsError(gerr)
		switch {
		case ge != nil && ge.Code == graphkit.CodeQuoteMismatch && ge.ExistingOrderID() != "":
			order, gerr = s.api().GetOrder(ctx, externalID(orgID), ge.ExistingOrderID())
			if gerr != nil {
				logUpstream("order.adopt", gerr)
				return s.orderView(ctx, row)
			}
		case graphkit.Definitive(gerr):
			logUpstream("order", gerr)
			back, ok := s.refundLines(ctx, row, snap)
			code := CodeUnavailable
			switch {
			case ge != nil && ge.Code == graphkit.CodeQuoteExpired:
				code = CodePriceChanged
			case ge != nil && ge.UnavailableProvider() != "":
				code = CodeProviderUnavailable
			}
			if err := s.Repo.MarkOrderFailed(context.WithoutCancel(ctx), orgID, row.ID, code, back, !ok); err != nil {
				return nil, err
			}
			s.publish(ctx, orgID, pubsub.StoreEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventStoreOrderUpdated}, ID: row.ID.String(), Status: models.StoreOrderFailed})
			if code == CodePriceChanged {
				fresh, id, err := s.quote(ctx, orgID, snap.Items, snap.Photos, snap.Signatures)
				if err != nil {
					return nil, err
				}
				return nil, &PriceChangedError{Quote: fresh.view(id, s.payment(ctx, orgID, fresh.DueNowMicros, fresh.RecurringMicros))}
			}
			return nil, s.upstream(ctx, "order", gerr)
		default:
			// No answer: the charge stands and the reconciler places the order with the same key.
			logUpstream("order.pending", gerr)
			return s.orderView(ctx, row)
		}
	}
	if err := s.Repo.MarkOrderPlaced(context.WithoutCancel(ctx), orgID, row.ID, order.ID, models.StoreOrderProcessing, stepsJSON(order.Steps)); err != nil {
		return nil, err
	}
	gkID := order.ID
	row.GraphKitOrderID = &gkID
	if err := s.applyOrder(ctx, row, order); err != nil {
		log.Warn().Err(err).Str("order_id", row.ID.String()).Msg("store: applying the placed order failed; the reconciler retries")
	}
	fresh, err := s.Repo.GetOrder(ctx, orgID, row.ID)
	if err != nil || fresh == nil {
		return s.orderView(ctx, row)
	}
	return s.orderView(ctx, fresh)
}

func stepsJSON(steps []graphkit.OrderStep) json.RawMessage {
	out := make([]Step, 0, len(graphkit.StepKeys))
	byKey := map[string]graphkit.OrderStep{}
	for _, st := range steps {
		byKey[st.Key] = st
	}
	for _, k := range graphkit.StepKeys {
		st, ok := byKey[k]
		if !ok {
			out = append(out, Step{Key: k, Status: graphkit.StepPending})
			continue
		}
		out = append(out, Step{Key: k, Status: st.Status, At: st.At})
	}
	raw, _ := json.Marshal(out)
	return raw
}

func orderStatus(s string) string {
	switch s {
	case graphkit.OrderCompleted:
		return models.StoreOrderCompleted
	case graphkit.OrderPartiallyFailed:
		return models.StoreOrderPartiallyFailed
	case graphkit.OrderFailed:
		return models.StoreOrderFailed
	}
	return models.StoreOrderProcessing
}

// applyOrder brings an order, its domains and mailboxes up to GraphKit's
// state, and refunds each item GraphKit refunded.
func (s *Service) applyOrder(ctx context.Context, row *models.StoreOrder, o *graphkit.Order) error {
	orgID := row.OrganizationID
	var snap quoteSnapshot
	_ = json.Unmarshal(row.Quote, &snap)
	var firstErr, refundErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, it := range o.Items {
		switch it.Type {
		case graphkit.ItemDomain:
			if it.DomainID != "" {
				if d, err := s.api().GetDomain(ctx, it.DomainID); err == nil {
					_, err = s.upsertDomain(ctx, orgID, &row.ID, d)
					keep(err)
				} else {
					keep(err)
				}
			}
		case graphkit.ItemMailboxes:
			for _, id := range it.MailboxIDs {
				m, err := s.api().GetMailbox(ctx, id)
				if err != nil {
					keep(err)
					continue
				}
				box, err := s.upsertMailbox(ctx, orgID, &row.ID, m, snap.MailboxMonthly)
				if err != nil {
					keep(err)
				} else if box != nil {
					s.adoptOrderPhoto(ctx, box, snap)
					s.adoptOrderSignature(ctx, box, snap)
				}
			}
		}
		if it.RefundedMicros > 0 {
			if err := s.refundItem(ctx, row, snap, it); err != nil {
				refundErr = err
				keep(err)
			}
		}
	}
	status := orderStatus(o.Status)
	if refundErr != nil {
		// Kept open until every refund went through, so the reconciler asks again.
		status = models.StoreOrderProcessing
	}
	var completed *time.Time
	if status != models.StoreOrderProcessing {
		completed = o.CompletedAt
		if completed == nil {
			now := s.now()
			completed = &now
		}
	}
	before, err := s.Repo.UpdateOrderState(ctx, orgID, row.ID, status, stepsJSON(o.Steps), completed)
	keep(err)
	if err == nil && before == models.StoreOrderProcessing && status != models.StoreOrderProcessing {
		s.notifyOrderDone(ctx, row, status)
	}
	s.publish(ctx, orgID, pubsub.StoreEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventStoreOrderUpdated}, ID: row.ID.String(), Status: status})
	return firstErr
}

// notifyOrderDone tells the workspace an order finished; live mailbox setup can take days, so nobody is watching.
func (s *Service) notifyOrderDone(ctx context.Context, row *models.StoreOrder, status string) {
	link := "/app/emails/buy/orders/" + row.ID.String()
	switch status {
	case models.StoreOrderCompleted:
		s.notifyMailboxes(ctx, row.OrganizationID, models.NotifBillingAlert, "Your new domains and mailboxes are ready",
			"Everything in your order is set up. Purchased mailboxes connect to Warmbly on their own and start warming up.", link, "store_order_"+row.ID.String())
	case models.StoreOrderPartiallyFailed:
		s.notifyMailboxes(ctx, row.OrganizationID, models.NotifBillingAlert, "Part of your order could not be set up",
			"Some items in your order could not be set up and were refunded to your balance. The rest is ready.", link, "store_order_"+row.ID.String())
	case models.StoreOrderFailed:
		s.notifyMailboxes(ctx, row.OrganizationID, models.NotifBillingAlert, "Your order could not be set up",
			"We could not set up your order, and it was refunded to your balance.", link, "store_order_"+row.ID.String())
	}
}

// refundItem gives back the retail line behind an item GraphKit refunded, once.
func (s *Service) refundItem(ctx context.Context, row *models.StoreOrder, snap quoteSnapshot, it graphkit.OrderItem) error {
	for i, l := range snap.Lines {
		if l.Type != it.Type || l.Domain != it.Domain || l.AmountMicros <= 0 {
			continue
		}
		key := lineKey(row.ChargeKey, i)
		var back models.Micro
		if s.metered() && row.ChargedMicros > 0 {
			got, err := s.refundOrderLine(ctx, row, i, "Refund · "+lineReason(l, row.CreatedAt))
			if err != nil {
				return err
			}
			back = got
		}
		if _, err := s.Repo.AddOrderRefund(ctx, row.OrganizationID, row.ID, key, back); err != nil {
			return err
		}
	}
	return nil
}

// ---- views --------------------------------------------------------------

func (s *Service) orderView(ctx context.Context, row *models.StoreOrder) (*Order, error) {
	out := &Order{ID: row.ID, Status: row.Status, CreatedAt: row.CreatedAt, CompletedAt: row.CompletedAt,
		DueNowMicros: int64(row.DueNowMicros), ChargedMicros: int64(row.ChargedMicros), RefundedMicros: int64(row.RefundedMicros),
		Domains: []Domain{}, Mailboxes: []Mailbox{}}
	_ = json.Unmarshal(row.Steps, &out.Steps)
	if len(out.Steps) == 0 {
		for _, k := range graphkit.StepKeys {
			out.Steps = append(out.Steps, Step{Key: k, Status: graphkit.StepPending})
		}
	}
	domains, err := s.Repo.ListDomainsByOrder(ctx, row.OrganizationID, row.ID)
	if err != nil {
		return nil, err
	}
	for i := range domains {
		out.Domains = append(out.Domains, domainView(&domains[i]))
	}
	boxes, err := s.Repo.ListMailboxesByOrder(ctx, row.OrganizationID, row.ID)
	if err != nil {
		return nil, err
	}
	unplanned := len(boxes) > 0 && s.unplanned(ctx, row.OrganizationID)
	for i := range boxes {
		out.Mailboxes = append(out.Mailboxes, mailboxView(&boxes[i], unplanned))
	}
	return out, nil
}

// GetOrder is one of the workspace's orders.
func (s *Service) GetOrder(ctx context.Context, orgID, id uuid.UUID) (*Order, error) {
	if !s.readable() {
		return nil, errDisabled
	}
	row, err := s.Repo.GetOrder(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, errx.ErrNotFound
	}
	return s.orderView(ctx, row)
}

// ListOrders is the workspace's orders, newest first.
func (s *Service) ListOrders(ctx context.Context, orgID uuid.UUID, cursor string, limit int) (*Page[Order], error) {
	if !s.readable() {
		return nil, errDisabled
	}
	after, limit, err := pageArgs(cursor, limit)
	if err != nil {
		return nil, err
	}
	rows, err := s.Repo.ListOrders(ctx, orgID, after, limit+1)
	if err != nil {
		return nil, err
	}
	out := &Page[Order]{Data: []Order{}}
	for i := range rows {
		if i == limit {
			break
		}
		o, err := s.orderView(ctx, &rows[i])
		if err != nil {
			return nil, err
		}
		out.Data = append(out.Data, *o)
	}
	out.Pagination = nextPage(len(rows) > limit, func() models.StorePage {
		last := rows[limit-1]
		return models.StorePage{CreatedAt: last.CreatedAt, ID: last.ID}
	})
	return out, nil
}

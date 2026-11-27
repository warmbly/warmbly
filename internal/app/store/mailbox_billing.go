package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// Mailbox months are collected in advance, in one charge per workspace.
//
// GraphKit charges the instance each mailbox's next month when its
// current_period_end comes round, and charges nothing more once a cancellation
// reaches it before then. So every mailbox is paid here before its next
// GraphKit period starts: a workspace has a billing day (its first mailbox's
// day of the month, 1-28), and a few days before it one charge covers every
// mailbox whose next period starts before the following billing day plus the
// lead. A charge that keeps failing cancels the unpaid mailboxes at GraphKit a
// day before the earliest of those periods, and a late payment before the
// cancellation takes effect resumes them.

const (
	// unpaidStopLead is how long before an unpaid period starts its mailboxes are cancelled at GraphKit.
	unpaidStopLead = 24 * time.Hour
	// resumeMargin keeps a late payment clear of the moment a cancellation becomes final.
	resumeMargin = time.Hour
	// replayAfter is how long a batch may stay reserved before a pass finishes it, so a request still charging keeps it.
	replayAfter = 10 * time.Minute
)

// farFuture lists unpaid mailboxes whatever their last attempt.
var farFuture = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

func (s *Service) billLead(ctx context.Context) time.Duration {
	return time.Duration(s.retail(ctx).MailboxBillLead()) * 24 * time.Hour
}

// anchorOn is the billing day in a month, at midnight UTC.
func anchorOn(day, year int, month time.Month) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// anchorAfter is the first billing date strictly after t.
func anchorAfter(day int, t time.Time) time.Time {
	t = t.UTC()
	a := anchorOn(day, t.Year(), t.Month())
	for !a.After(t) {
		a = anchorOn(day, a.Year(), a.Month()+1)
	}
	return a
}

// billDay is the day of the month a time falls on, kept to 1-28 so every month has it.
func billDay(t time.Time) int { return max(1, min(t.UTC().Day(), 28)) }

// ensureAnchor gives a workspace its billing day from its first mailbox, once.
func (s *Service) ensureAnchor(ctx context.Context, orgID uuid.UUID) (*models.StoreAccount, error) {
	acct, err := s.Repo.GetAccount(ctx, orgID)
	if err != nil || acct == nil {
		return acct, err
	}
	if acct.BillDay > 0 && acct.NextBillAnchor != nil {
		return acct, nil
	}
	boxes, err := s.Repo.OpenMailboxes(ctx, orgID)
	if err != nil {
		return nil, err
	}
	first := s.now()
	for i := range boxes {
		if boxes[i].CreatedAt.Before(first) {
			first = boxes[i].CreatedAt
		}
	}
	if seat := s.firstSeatAt(ctx, orgID); seat != nil && seat.Before(first) {
		first = *seat
	}
	day := billDay(first)
	next := anchorAfter(day, s.now().Add(s.billLead(ctx)))
	if err := s.Repo.SetBillAnchor(ctx, orgID, day, next); err != nil {
		return nil, err
	}
	acct.BillDay, acct.NextBillAnchor = day, &next
	return acct, nil
}

// paidHorizon is how far the workspace's last billing run collected: every
// mailbox whose next period starts before it was in a charge already.
func (s *Service) paidHorizon(ctx context.Context, orgID uuid.UUID) time.Time {
	lead := s.billLead(ctx)
	if acct, err := s.Repo.GetAccount(ctx, orgID); err == nil && acct != nil && acct.NextBillAnchor != nil {
		return acct.NextBillAnchor.Add(lead)
	}
	return s.now().Add(lead)
}

func (s *Service) billMailboxes(ctx context.Context) error {
	orgs, err := s.Repo.OrgsWithLiveMailboxes(ctx, 10_000)
	if err != nil {
		return err
	}
	var errs []error
	billed := map[uuid.UUID]bool{}
	for _, orgID := range orgs {
		billed[orgID] = true
		errs = append(errs, s.billOrg(ctx, orgID, false))
	}
	// Social seats ride on the same run, also in a workspace with no mailboxes.
	if sc := s.socialAddon(); sc != nil {
		seated, err := sc.OrgsWithSeats(ctx, 10_000)
		errs = append(errs, err)
		for _, orgID := range seated {
			if !billed[orgID] {
				billed[orgID] = true
				errs = append(errs, s.billOrg(ctx, orgID, false))
			}
		}
	}
	// A workspace whose last mailboxes ended still gets back a month they paid for.
	unsettled, err := s.Repo.OrgsWithUnsettledCancels(ctx, 10_000)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, orgID := range unsettled {
		if !billed[orgID] {
			errs = append(errs, s.settleCancels(ctx, orgID))
		}
	}
	return errors.Join(errs...)
}

// billOrg runs a workspace's mailbox billing: a charge left half done is
// finished under its own key, the billing run charges what its window holds,
// and between runs an unpaid charge is tried again once a day (at once when
// retryNow, after the card changed). One batch that cannot finish never holds
// up the rest.
func (s *Service) billOrg(ctx context.Context, orgID uuid.UUID, retryNow bool) error {
	now, lead := s.now(), s.billLead(ctx)
	var errs []error
	keys, err := s.Repo.PendingChargeKeys(ctx, orgID, now.Add(-replayAfter))
	if err == nil {
		keys, err = s.pendingSeatKeys(ctx, orgID, keys, now.Add(-replayAfter))
	}
	if err != nil {
		return err
	}
	for _, key := range keys {
		errs = append(errs, s.chargeBatch(ctx, orgID, key))
	}
	acct, err := s.ensureAnchor(ctx, orgID)
	if err != nil || acct == nil {
		return errors.Join(append(errs, err)...)
	}
	s.syncAutoReload(ctx, orgID)
	s.reloadBeforeBundle(ctx, orgID, acct)
	run := !acct.NextBillAnchor.Add(-lead).After(now)
	next := anchorAfter(acct.BillDay, *acct.NextBillAnchor)
	var rows []models.StoreMailbox
	// Seats due before seatsDue, and unpaid ones last tried before retryBefore, join the charge.
	seatsDue, retryBefore := next.Add(lead), farFuture
	if run {
		rows, err = s.Repo.MailboxesDue(ctx, orgID, next.Add(lead), now)
	} else {
		before := now.Add(-20 * time.Hour)
		if retryNow {
			before = now
		}
		seatsDue, retryBefore = now.Add(2*unpaidStopLead), before
		rows, err = s.Repo.UnpaidMailboxes(ctx, orgID, now, before, false)
		if err == nil {
			// A mailbox no run collected for (a month given back, then resumed at GraphKit) is still paid before its period starts.
			var due []models.StoreMailbox
			due, err = s.Repo.MailboxesDue(ctx, orgID, now.Add(2*unpaidStopLead), now)
			for i := range due {
				if due[i].BillingFailures == 0 {
					rows = append(rows, due[i])
				}
			}
		}
	}
	key := ""
	switch {
	case err != nil:
	case !s.metered():
		// Nothing is charged here; the months still move on, so nothing ever reads as unpaid.
		for i := range rows {
			if err = s.Repo.MarkMonthPaid(ctx, orgID, rows[i].ID); err != nil {
				break
			}
		}
		if err == nil {
			if sc := s.socialAddon(); sc != nil {
				err = sc.ExtendUnmeteredSeats(ctx, orgID, seatsDue)
			}
		}
		if err == nil && run {
			err = s.Repo.SetBillAnchor(ctx, orgID, acct.BillDay, next)
		}
	case run:
		// The run's date moves with its reservation, so a run happens once and leaves no mailbox a month behind.
		key = "store:mailboxes:" + orgID.String() + ":" + now.Format("20060102") + ":" + uuid.NewString()[:8]
		var ran bool
		ran, err = s.Repo.ReserveRun(ctx, orgID, rows, key, *acct.NextBillAnchor, next)
		seats := 0
		if ran && err == nil {
			seats, err = s.reserveSeats(ctx, orgID, key, seatsDue, retryBefore)
		}
		if !ran || (len(rows) == 0 && seats == 0) {
			key = ""
		}
	default:
		key = "store:mailboxes:" + orgID.String() + ":" + now.Format("20060102") + ":" + uuid.NewString()[:8]
		if len(rows) > 0 {
			if _, err = s.Repo.ReserveMailboxCharge(ctx, orgID, rows, key); err != nil {
				key = ""
			}
		}
		if key != "" {
			seats, serr := s.reserveSeats(ctx, orgID, key, seatsDue, retryBefore)
			err = errors.Join(err, serr)
			if len(rows) == 0 && seats == 0 {
				key = ""
			}
		}
	}
	errs = append(errs, err)
	if key != "" {
		errs = append(errs, s.chargeBatch(ctx, orgID, key))
	}
	errs = append(errs, s.settleCancels(ctx, orgID), s.enforceUnpaid(ctx, orgID))
	if sc := s.socialAddon(); sc != nil {
		errs = append(errs, sc.EnforceUnpaidSeats(ctx, orgID))
	}
	return errors.Join(errs...)
}

// nextBundle is what the workspace's next billing run will charge, from what is known now.
func (s *Service) nextBundle(ctx context.Context, acct *models.StoreAccount) *NextBundle {
	if acct.NextBillAnchor == nil || acct.BillDay == 0 {
		return nil
	}
	lead := s.billLead(ctx)
	window := anchorAfter(acct.BillDay, *acct.NextBillAnchor).Add(lead)
	rows, err := s.Repo.MailboxesDue(ctx, acct.OrganizationID, window, s.now())
	if err != nil {
		return nil
	}
	var seats SeatBatch
	if sc := s.socialAddon(); sc != nil {
		seats, _ = sc.SeatsDue(ctx, acct.OrganizationID, window)
	}
	if len(rows) == 0 && seats.Count == 0 {
		return nil
	}
	out := &NextBundle{ChargeOn: acct.NextBillAnchor.Add(-lead), Mailboxes: len(rows), SocialSeats: seats.Count, AmountMicros: seats.Micros}
	if out.ChargeOn.Before(s.now()) {
		out.ChargeOn = s.now()
	}
	for i := range rows {
		out.AmountMicros += int64(rows[i].MonthlyRetailMicros)
	}
	return out
}

// bundleReason is the ledger and invoice line for a month's mailboxes.
func bundleReason(rows []models.StoreMailbox) string {
	latest := rows[0].PaidThrough
	same := true
	for _, m := range rows {
		if m.PaidThrough.After(latest) {
			latest = m.PaidThrough
		}
		same = same && m.MonthlyRetailMicros == rows[0].MonthlyRetailMicros
	}
	if same {
		return fmt.Sprintf("Mailboxes · %d × %s · %s", len(rows), usd(int64(rows[0].MonthlyRetailMicros)), monthLabel(latest))
	}
	return fmt.Sprintf("Mailboxes · %d · %s", len(rows), monthLabel(latest))
}

// chargeBatch collects one bundle: the balance, then the card for the rest,
// then one debit, all under the bundle's key, so a pass that stops anywhere in
// between finishes it later without charging twice. Only the mailboxes whose
// next month will start are moved on; the rest get their share back.
func (s *Service) chargeBatch(ctx context.Context, orgID uuid.UUID, key string) error {
	rows, err := s.Repo.MailboxesByChargeKey(ctx, orgID, key)
	if err != nil {
		return err
	}
	seats, err := s.seatBatch(ctx, orgID, key)
	if err != nil || (len(rows) == 0 && seats.Count == 0) {
		return err
	}
	if err := s.refreshCancelled(ctx, rows); err != nil {
		// Whether a cancelled mailbox was resumed decides the batch: ask again next pass.
		return err
	}
	var boxes int64
	for i := range rows {
		boxes += reserved(&rows[i])
	}
	total := boxes + seats.Micros
	if s.metered() && total > 0 {
		prior, err := s.Credits.ChargeOf(ctx, orgID, key)
		if err != nil {
			return err
		}
		if prior == nil {
			if seats.Count == 0 && !s.anyStarting(rows) {
				// Nothing in it will start a month: nothing to collect.
				return s.finishBatch(ctx, orgID, key, rows)
			}
			reason := batchReason(rows, seats)
			err := s.collect(ctx, orgID, total, key, reason, true)
			if err == nil {
				_, err = s.Credits.ChargeAmount(ctx, orgID, micro(total), reason, key)
			}
			if err != nil {
				if !paymentFailed(err) {
					// A card payment still going through, or an outage: the next pass replays the same key.
					return err
				}
				if err := s.Repo.ReleaseMailboxCharge(ctx, orgID, key); err != nil {
					return err
				}
				if seats.Count > 0 {
					if err := s.socialAddon().SeatsUnpaid(ctx, orgID, key, err); err != nil {
						return err
					}
				}
				if len(rows) > 0 {
					s.mailboxesUnpaid(ctx, orgID, rows, boxes, err)
				}
				return nil
			}
		}
	}
	if _, err := s.Repo.SettleMailboxCharge(ctx, orgID, key, s.now()); err != nil {
		return err
	}
	if seats.Count > 0 {
		if err := s.socialAddon().SettleSeats(ctx, orgID, key); err != nil {
			return err
		}
	}
	return s.finishBatch(ctx, orgID, key, rows)
}

// reserved is what a batch takes for one mailbox: the price fixed when it was reserved.
func reserved(m *models.StoreMailbox) int64 {
	if m.PendingChargeMicros > 0 {
		return int64(m.PendingChargeMicros)
	}
	return int64(m.MonthlyRetailMicros)
}

// refreshCancelled reads again from GraphKit the batch's mailboxes cancelled by the
// customer, so a resume whose answer never arrived is known before the batch settles.
func (s *Service) refreshCancelled(ctx context.Context, rows []models.StoreMailbox) error {
	for i := range rows {
		m := &rows[i]
		if m.CancelsAt == nil || m.UnpaidCancel {
			continue
		}
		live, gerr := s.api().GetMailbox(ctx, m.GraphKitMailboxID)
		if gerr != nil {
			logUpstream("mailbox.batch", gerr)
			if !graphkit.Definitive(gerr) {
				return gerr
			}
			continue
		}
		fresh, err := s.upsertMailbox(ctx, m.OrganizationID, nil, live, 0)
		if err != nil {
			return err
		}
		if fresh != nil {
			*m = *fresh
		}
	}
	return nil
}

// anyStarting reports a mailbox in the batch whose next month will start once paid.
func (s *Service) anyStarting(rows []models.StoreMailbox) bool {
	now := s.now()
	for i := range rows {
		m := &rows[i]
		if ((m.Status == graphkit.MailboxActive || m.Status == graphkit.MailboxSuspended) && m.CancelsAt == nil) ||
			(m.UnpaidCancel && m.Status == graphkit.MailboxCancelling && m.CancelsAt != nil && m.CancelsAt.After(now.Add(resumeMargin))) {
			return true
		}
	}
	return false
}

// finishBatch gives back the share of every mailbox in the batch that the charge
// did not move on, and takes it out of the batch. A share is refunded once, under
// the batch's key, whichever pass or replica gets there first.
func (s *Service) finishBatch(ctx context.Context, orgID uuid.UUID, key string, rows []models.StoreMailbox) error {
	var errs []error
	for i := range rows {
		m, err := s.Repo.GetMailbox(ctx, orgID, rows[i].ID)
		if err != nil || m == nil {
			errs = append(errs, err)
			continue
		}
		held := m.PendingChargeKey != nil && *m.PendingChargeKey == key
		if !held && ((m.PaidKey != nil && *m.PaidKey == key) || m.PaidThrough.After(rows[i].PaidThrough)) {
			// Moved on by this charge.
			s.publishMailbox(ctx, m)
			continue
		}
		if s.metered() && reserved(&rows[i]) > 0 {
			if _, err := s.Credits.RefundStore(context.WithoutCancel(ctx), orgID, key, "", key+":refund:"+m.ID.String(), models.Micro(reserved(&rows[i])),
				"Refund · Mailbox · "+m.Email+" · "+monthLabel(rows[i].PaidThrough)); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		if held {
			errs = append(errs, s.Repo.DetachMailboxCharge(ctx, orgID, key, m.ID))
		}
	}
	return errors.Join(errs...)
}

// earliestStart is the first of the unpaid periods, which is when their mailboxes stop.
func earliestStart(rows []models.StoreMailbox) time.Time {
	at := rows[0].PaidThrough
	for _, m := range rows {
		if m.PaidThrough.Before(at) {
			at = m.PaidThrough
		}
	}
	return at
}

// mailboxesUnpaid tells the workspace, on every attempt, that its mailboxes'
// month did not go through, how to fix it, and when they stop if it is not.
func (s *Service) mailboxesUnpaid(ctx context.Context, orgID uuid.UUID, rows []models.StoreMailbox, total int64, cause error) {
	stop := earliestStart(rows)
	if won, _ := s.Repo.SetMailboxNotice(ctx, rows[0].ID, "unpaid:"+s.now().Format("20060102")); !won {
		return
	}
	s.notify(ctx, orgID, "Mailbox payment failed",
		fmt.Sprintf("We could not charge %s for %d mailboxes: %s We try again tomorrow. If it stays unpaid, they stop on %s.",
			usd(total), len(rows), payFix(cause), stop.Format("Jan 2, 2006")),
		payLink, "store_mailboxes_unpaid")
}

// enforceUnpaid cancels every unpaid mailbox at GraphKit a day before the
// earliest unpaid period starts, so GraphKit never starts a month nobody paid
// for. They keep working until then, and a payment before then resumes them.
func (s *Service) enforceUnpaid(ctx context.Context, orgID uuid.UUID) error {
	all, err := s.Repo.UnpaidMailboxes(ctx, orgID, s.now(), farFuture, true)
	if err != nil {
		return err
	}
	var rows []models.StoreMailbox
	for _, m := range all {
		if !m.UnpaidCancel {
			rows = append(rows, m)
		}
	}
	if len(rows) == 0 || s.now().Before(earliestStart(rows).Add(-unpaidStopLead)) {
		return nil
	}
	var stops time.Time
	for i := range rows {
		m := &rows[i]
		got, gerr := s.api().CancelMailbox(ctx, m.GraphKitMailboxID, "store:unpaid-cancel:"+m.ID.String()+":"+strconv.FormatInt(m.PaidThrough.Unix(), 10))
		if gerr != nil {
			logUpstream("mailbox.cancel.unpaid", gerr)
			continue
		}
		cancels := got.CancelsAt
		if cancels == nil {
			t := s.now()
			cancels = &t
		}
		if stops.IsZero() || cancels.Before(stops) {
			stops = *cancels
		}
		if endedStatus(got.Status) {
			if err := s.Repo.SetMailboxCancel(ctx, orgID, m.ID, got.Status, cancels); err == nil {
				if row, err := s.Repo.GetMailbox(ctx, orgID, m.ID); err == nil && row != nil {
					s.retire(ctx, row)
				}
			}
			continue
		}
		if err := s.Repo.SetUnpaidCancel(ctx, orgID, m.ID, got.Status, cancels); err != nil {
			return err
		}
		s.publish(ctx, orgID, pubsub.StoreEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventStoreMailboxUpdated}, ID: m.ID.String(), Status: got.Status})
	}
	if stops.IsZero() {
		return nil
	}
	if won, _ := s.Repo.SetMailboxNotice(ctx, rows[0].ID, "stop:"+strconv.FormatInt(stops.Unix(), 10)); won {
		s.notify(ctx, orgID, "Your mailboxes stop on "+stops.Format("Jan 2")+" unless your card is updated",
			fmt.Sprintf("We still could not charge for %d mailboxes, so they stop on %s. Update your card before then and we charge it and keep them running.",
				len(rows), stops.Format("Jan 2, 2006")),
			payLink, "store_mailboxes_stop")
	}
	return nil
}

// settleCancels resumes at GraphKit the mailboxes an unpaid month cancelled
// once that month is paid, while the cancellation can still be undone, and
// gives back a month collected for a period a cancellation keeps from starting.
func (s *Service) settleCancels(ctx context.Context, orgID uuid.UUID) error {
	boxes, err := s.Repo.OpenMailboxes(ctx, orgID)
	if err != nil {
		return err
	}
	for i := range boxes {
		m := &boxes[i]
		switch {
		case m.PendingChargeKey != nil:
		case m.UnpaidCancel && m.BillingFailures == 0 && endedStatus(m.Status):
			// The cancellation took effect, so the month paid late never starts.
			s.refundMonth(ctx, m)
		case m.CancelsAt == nil:
		case m.UnpaidCancel && m.BillingFailures == 0 && m.Status == graphkit.MailboxCancelling:
			s.resumePaid(ctx, m)
		case !m.UnpaidCancel:
			s.refundUnstarted(ctx, m)
		}
	}
	return nil
}

// resumePaid undoes the cancellation of a mailbox whose month was paid late,
// and gives that month back when the cancellation can no longer be undone.
func (s *Service) resumePaid(ctx context.Context, m *models.StoreMailbox) {
	key := "store:unpaid-resume:" + m.ID.String() + ":" + strconv.FormatInt(m.PaidThrough.Unix(), 10)
	if m.CancelsAt != nil && s.now().Before(m.CancelsAt.Add(-resumeMargin)) {
		got, gerr := s.api().ResumeMailbox(ctx, m.GraphKitMailboxID, key)
		if gerr == nil {
			if err := s.Repo.SetMailboxCancel(ctx, m.OrganizationID, m.ID, got.Status, nil); err != nil {
				log.Warn().Err(err).Str("store_mailbox_id", m.ID.String()).Msg("store: recording a resumed mailbox failed")
			}
			s.publish(ctx, m.OrganizationID, pubsub.StoreEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventStoreMailboxUpdated}, ID: m.ID.String(), Status: got.Status})
			return
		}
		logUpstream("mailbox.resume.paid", gerr)
		if !graphkit.Definitive(gerr) {
			return
		}
	}
	// Too late to keep it: the month it paid for will not happen.
	s.refundMonth(ctx, m)
}

// refundUnstarted gives back a month collected ahead for a period that a cancellation keeps from starting.
func (s *Service) refundUnstarted(ctx context.Context, m *models.StoreMailbox) {
	if m.CancelsAt == nil || m.UnpaidCancel || m.PaidKey == nil || m.PendingChargeKey != nil {
		return
	}
	if !m.PaidThrough.Before(m.CancelsAt.AddDate(0, 1, 0).Add(-resumeMargin)) {
		s.refundMonth(ctx, m)
	}
}

// refundMonth gives back the month m's last charge collected, moves the mailbox
// back that month and ends an unpaid cancellation, once per charge: the refund
// is keyed by the charge, and the write only lands while that charge still holds the month.
func (s *Service) refundMonth(ctx context.Context, m *models.StoreMailbox) {
	ctx = context.WithoutCancel(ctx)
	if m.PaidKey == nil {
		// No charge of its own collected the month, so there is nothing to give back.
		if m.UnpaidCancel {
			_ = s.Repo.SetMailboxCancel(ctx, m.OrganizationID, m.ID, m.Status, m.CancelsAt)
		}
		return
	}
	paid := *m.PaidKey
	if s.metered() && m.PaidMicros > 0 {
		if _, err := s.Credits.RefundStore(ctx, m.OrganizationID, paid, "", paid+":refund:"+m.ID.String(), m.PaidMicros,
			"Refund · Mailbox · "+m.Email+" · "+monthLabel(m.PaidThrough.AddDate(0, -1, 0))); err != nil {
			log.Error().Err(err).Str("store_mailbox_id", m.ID.String()).Msg("store: refunding a mailbox month failed; the billing pass retries")
			return
		}
	}
	if _, err := s.Repo.RefundMonth(ctx, m.OrganizationID, m.ID, paid); err != nil {
		log.Error().Err(err).Str("store_mailbox_id", m.ID.String()).Msg("store: moving a refunded month back failed; the billing pass retries")
	}
}

// applyPriceChange keeps a mailbox's price above what GraphKit charges for
// its next period: a rise carries the margin over and is announced first.
func (s *Service) applyPriceChange(ctx context.Context, row *models.StoreMailbox, m *graphkit.Mailbox) {
	if m.NextPeriodMonthlyMicros == nil || *m.NextPeriodMonthlyMicros <= m.MonthlyMicros {
		return
	}
	margin := max(int64(row.MonthlyRetailMicros)-m.MonthlyMicros, 0)
	next := ceilCent(*m.NextPeriodMonthlyMicros + margin)
	if next <= int64(row.MonthlyRetailMicros) {
		return
	}
	before := row.MonthlyRetailMicros
	if err := s.Repo.SetMailboxRetail(ctx, row.OrganizationID, row.ID, models.Micro(next)); err != nil {
		log.Warn().Err(err).Str("store_mailbox_id", row.ID.String()).Msg("store: recording a price change failed")
		return
	}
	row.MonthlyRetailMicros = models.Micro(next)
	from := row.PaidThrough
	if row.PeriodEnd != nil {
		from = *row.PeriodEnd
	}
	s.notify(ctx, row.OrganizationID, "Mailbox price change for "+row.Email,
		fmt.Sprintf("The provider's price for %s goes up, so its monthly price changes from %s to %s from %s.",
			row.Email, usd(int64(before)), usd(next), from.Format("Jan 2, 2006")),
		mailboxesLink, "store_mailbox_price_"+row.ID.String())
}

// UncollectedMailbox is one mailbox GraphKit bills a period for that Warmbly did not collect.
type UncollectedMailbox struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Email          string    `json:"email"`
	// PeriodEnd is GraphKit's current period end; PaidThrough is how far Warmbly collected.
	PeriodEnd   time.Time `json:"period_end"`
	PaidThrough time.Time `json:"paid_through"`
}

// Uncollected is the reconciliation of GraphKit's periods against what Warmbly collected.
func (s *Service) Uncollected(ctx context.Context) ([]UncollectedMailbox, error) {
	out := []UncollectedMailbox{}
	if !s.Enabled() {
		return out, nil
	}
	rows, err := s.Repo.UncollectedMailboxes(ctx, 100)
	if err != nil {
		return nil, err
	}
	for _, m := range rows {
		out = append(out, UncollectedMailbox{ID: m.ID, OrganizationID: m.OrganizationID, Email: m.Email, PeriodEnd: *m.PeriodEnd, PaidThrough: m.PaidThrough})
	}
	return out, nil
}

// refreshPeriods reads again the mailboxes whose recorded GraphKit period ended, so their next one is known.
func (s *Service) refreshPeriods(ctx context.Context) error {
	rows, err := s.Repo.MailboxesPastPeriod(ctx, s.now(), 200)
	if err != nil {
		return err
	}
	for i := range rows {
		m, gerr := s.api().GetMailbox(ctx, rows[i].GraphKitMailboxID)
		if gerr != nil {
			logUpstream("mailbox.period", gerr)
			continue
		}
		if _, err := s.upsertMailbox(ctx, rows[i].OrganizationID, nil, m, 0); err != nil {
			log.Warn().Err(err).Str("store_mailbox_id", rows[i].ID.String()).Msg("store: recording a new period failed")
		}
	}
	return nil
}

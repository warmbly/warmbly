package social

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/store"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// unpaidRemoveAfter is how long past its paid month an unpaid seat stays paused before it is removed.
const unpaidRemoveAfter = 7 * 24 * time.Hour

// FirstSeatAt implements store.Social.
func (s *Service) FirstSeatAt(ctx context.Context, orgID uuid.UUID) (*time.Time, error) {
	return s.Repo.FirstSeatAt(ctx, orgID)
}

// OrgsWithSeats implements store.Social.
func (s *Service) OrgsWithSeats(ctx context.Context, limit int) ([]uuid.UUID, error) {
	return s.Repo.OrgsWithSeats(ctx, limit)
}

// PendingSeatKeys implements store.Social.
func (s *Service) PendingSeatKeys(ctx context.Context, orgID uuid.UUID, before time.Time) ([]string, error) {
	return s.Repo.PendingSeatKeys(ctx, orgID, before)
}

// ReserveSeats implements store.Social, at the price the workspace's plan sets now.
func (s *Service) ReserveSeats(ctx context.Context, orgID uuid.UUID, key string, dueBefore, retryBefore time.Time) (int, error) {
	price, err := s.seatPrice(ctx, orgID)
	if err != nil {
		return 0, err
	}
	return s.Repo.ReserveSeats(ctx, orgID, key, models.Micro(price.MonthlyMicros), dueBefore, retryBefore)
}

// SeatBatch implements store.Social.
func (s *Service) SeatBatch(ctx context.Context, orgID uuid.UUID, key string) (store.SeatBatch, error) {
	rows, err := s.Repo.SeatsByChargeKey(ctx, orgID, key)
	if err != nil {
		return store.SeatBatch{}, err
	}
	return seatBatch(rows, func(a *models.SocialAccount) int64 { return int64(a.PendingChargeMicros) }), nil
}

// SeatsDue implements store.Social.
func (s *Service) SeatsDue(ctx context.Context, orgID uuid.UUID, dueBefore time.Time) (store.SeatBatch, error) {
	rows, err := s.Repo.SeatsDue(ctx, orgID, dueBefore)
	if err != nil {
		return store.SeatBatch{}, err
	}
	price, err := s.seatPrice(ctx, orgID)
	if err != nil {
		return store.SeatBatch{}, err
	}
	return seatBatch(rows, func(*models.SocialAccount) int64 { return price.MonthlyMicros }), nil
}

func seatBatch(rows []models.SocialAccount, amount func(*models.SocialAccount) int64) store.SeatBatch {
	if len(rows) == 0 {
		return store.SeatBatch{}
	}
	b := store.SeatBatch{Count: len(rows)}
	latest := time.Time{}
	same := true
	first := amount(&rows[0])
	for i := range rows {
		a := amount(&rows[i])
		b.Micros += a
		same = same && a == first
		if rows[i].PaidThrough != nil && rows[i].PaidThrough.After(latest) {
			latest = *rows[i].PaidThrough
		}
	}
	if same {
		b.Reason = fmt.Sprintf("Social accounts · %d × %s · %s", b.Count, store.USD(first), store.MonthLabel(latest))
	} else {
		b.Reason = fmt.Sprintf("Social accounts · %d · %s", b.Count, store.MonthLabel(latest))
	}
	return b
}

// SettleSeats implements store.Social: the seats the charge paid move a month on and resume if unpaid had paused
// them; a seat removed since its reservation gets its share back.
func (s *Service) SettleSeats(ctx context.Context, orgID uuid.UUID, key string) error {
	reserved, err := s.Repo.SeatsByChargeKey(ctx, orgID, key)
	if err != nil {
		return err
	}
	settled, err := s.Repo.SettleSeats(ctx, orgID, key)
	if err != nil {
		return err
	}
	moved := make(map[uuid.UUID]bool, len(settled))
	for i := range settled {
		a := &settled[i]
		moved[a.ID] = true
		if a.UnpaidPaused {
			s.resumeUnpaid(ctx, a)
		}
		s.publishAccount(ctx, a)
	}
	for i := range reserved {
		a := &reserved[i]
		if moved[a.ID] {
			continue
		}
		if a.PendingChargeMicros > 0 {
			if _, err := s.Credits.RefundStore(context.WithoutCancel(ctx), orgID, key, "", key+":refund:social:"+a.ID.String(),
				a.PendingChargeMicros, "Refund · Social account · removed"); err != nil {
				log.Error().Err(err).Str("account_id", a.ID.String()).Msg("social: refunding a removed seat's month failed; retried with the batch")
				return err
			}
		}
		if err := s.Repo.DetachSeat(ctx, orgID, key, a.ID); err != nil {
			return err
		}
	}
	return nil
}

// resumeUnpaid resumes at GraphKit a seat unpaid had paused, now that it is paid.
func (s *Service) resumeUnpaid(ctx context.Context, a *models.SocialAccount) {
	sa, err := s.api().ResumeSocialAccount(ctx, ext(a.OrganizationID), a.GraphKitID,
		"social:unpaid-resume:"+a.ID.String()+":"+strconv.FormatInt(a.PaidThrough.Unix(), 10))
	if err != nil {
		log.Warn().Err(err).Str("account_id", a.ID.String()).Msg("social: resuming a paid seat failed; retried on the next pass")
		return
	}
	if err := s.Repo.SetUnpaidPaused(ctx, a.OrganizationID, a.ID, false); err != nil {
		return
	}
	a.UnpaidPaused = false
	if got, err := s.record(ctx, a.OrganizationID, sa, nil); err == nil && got != nil {
		*a = *got
	}
}

// SeatsUnpaid implements store.Social: the seats are released as unpaid and the workspace told, once a day.
func (s *Service) SeatsUnpaid(ctx context.Context, orgID uuid.UUID, key string, cause error) error {
	rows, err := s.Repo.ReleaseSeats(ctx, orgID, key)
	if err != nil || len(rows) == 0 {
		return err
	}
	var total int64
	stop := *rows[0].PaidThrough
	for i := range rows {
		total += int64(rows[i].PendingChargeMicros)
		if rows[i].PaidThrough.Before(stop) {
			stop = *rows[i].PaidThrough
		}
		s.publishAccount(ctx, &rows[i])
	}
	if won, _ := s.Repo.SetSeatNotice(ctx, rows[0].ID, "unpaid:"+s.now().Format("20060102")); won {
		s.Store.NotifyBilling(ctx, orgID, "Social account payment failed",
			fmt.Sprintf("We could not charge %s for %d social accounts: %s We try again tomorrow. If it stays unpaid, they pause on %s.",
				store.USD(total), len(rows), store.PayFix(cause), stop.Format("Jan 2, 2006")),
			store.PayLink, "social_seats_unpaid")
	}
	return nil
}

// ExtendUnmeteredSeats implements store.Social.
func (s *Service) ExtendUnmeteredSeats(ctx context.Context, orgID uuid.UUID, dueBefore time.Time) error {
	return s.Repo.ExtendUnmeteredSeats(ctx, orgID, dueBefore)
}

// EnforceUnpaidSeats implements store.Social: a seat whose month went unpaid is paused at GraphKit when that
// month ends and removed a week later. A later payment resumes a paused seat (SettleSeats).
func (s *Service) EnforceUnpaidSeats(ctx context.Context, orgID uuid.UUID) error {
	// A seat paid since unpaid paused it, whose resume did not go through when it was paid, is resumed again.
	paid, err := s.Repo.PausedPaidSeats(ctx, orgID)
	if err != nil {
		return err
	}
	for i := range paid {
		s.resumeUnpaid(ctx, &paid[i])
	}
	rows, err := s.Repo.UnpaidSeats(ctx, orgID)
	if err != nil || len(rows) == 0 {
		return err
	}
	now := s.now()
	for i := range rows {
		a := &rows[i]
		if a.PaidThrough == nil || now.Before(*a.PaidThrough) {
			continue
		}
		stamp := strconv.FormatInt(a.PaidThrough.Unix(), 10)
		if !now.Before(a.PaidThrough.Add(unpaidRemoveAfter)) {
			sa, err := s.api().RemoveSocialAccount(ctx, ext(orgID), a.GraphKitID, "social:unpaid-remove:"+a.ID.String()+":"+stamp)
			if ge, ok := graphkit.AsError(err); ok && ge.Code == graphkit.CodeNotFound {
				err = nil
			}
			if err != nil {
				log.Warn().Err(err).Str("account_id", a.ID.String()).Msg("social: removing an unpaid seat failed; retried on the next pass")
				continue
			}
			if sa != nil {
				_, _ = s.record(ctx, orgID, sa, nil)
			}
			if err := s.Repo.MarkRemoved(ctx, orgID, a.ID, now); err != nil {
				return err
			}
			if won, _ := s.Repo.SetSeatNotice(ctx, a.ID, "removed:"+stamp); won {
				s.Store.NotifyBilling(ctx, orgID, models.SocialChannelLabel(channelOr(a.Channel))+" account "+accountName(a)+" was removed",
					fmt.Sprintf("Its seat stayed unpaid for a week after %s, so the account was disconnected from Warmbly. Connect it again once your card is updated.",
						a.PaidThrough.Format("Jan 2, 2006")),
					"/app/settings/social", "social_seat_removed_"+a.ID.String())
			}
			continue
		}
		if a.UnpaidPaused {
			continue
		}
		sa, err := s.api().PauseSocialAccount(ctx, ext(orgID), a.GraphKitID, "social:unpaid-pause:"+a.ID.String()+":"+stamp)
		if err != nil {
			log.Warn().Err(err).Str("account_id", a.ID.String()).Msg("social: pausing an unpaid seat failed; retried on the next pass")
			continue
		}
		if err := s.Repo.SetUnpaidPaused(ctx, orgID, a.ID, true); err != nil {
			return err
		}
		_, _ = s.record(ctx, orgID, sa, nil)
		if won, _ := s.Repo.SetSeatNotice(ctx, a.ID, "paused:"+stamp); won {
			s.Store.NotifyBilling(ctx, orgID, models.SocialChannelLabel(channelOr(a.Channel))+" account "+accountName(a)+" is paused",
				fmt.Sprintf("Its seat's month could not be charged, so it paused on %s. Update your card before %s and we charge it and resume the account; after that it is removed.",
					a.PaidThrough.Format("Jan 2"), a.PaidThrough.Add(unpaidRemoveAfter).Format("Jan 2, 2006")),
				store.PayLink, "social_seat_paused_"+a.ID.String())
		}
	}
	return nil
}

// refundRemovedSeats gives back, across workspaces, the paid months of removed seats that had not started.
func (s *Service) refundRemovedSeats(ctx context.Context) error {
	rows, err := s.Repo.RemovedSeatsPaidAhead(ctx, nil, stopBatch)
	if err != nil {
		return err
	}
	for i := range rows {
		s.refundUnstartedMonth(ctx, &rows[i])
	}
	return nil
}

// refundUnstartedMonth refunds a removed seat's month charged ahead of its start, keyed by the charge that paid it.
func (s *Service) refundUnstartedMonth(ctx context.Context, a *models.SocialAccount) {
	if a.PaidKey == nil || a.PaidMicros <= 0 || a.PaidThrough == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	paid := *a.PaidKey
	if _, err := s.Credits.RefundStore(ctx, a.OrganizationID, paid, "", paid+":refund:social:removed:"+a.ID.String(), a.PaidMicros,
		"Refund · Social account · "+accountName(a)+" · "+store.MonthLabel(a.PaidThrough.AddDate(0, -1, 0))); err != nil {
		log.Error().Err(err).Str("account_id", a.ID.String()).Msg("social: refunding a removed seat's unstarted month failed; the reconcile pass retries")
		return
	}
	if err := s.Repo.ReturnSeatMonth(ctx, a.OrganizationID, a.ID, paid); err != nil {
		log.Error().Err(err).Str("account_id", a.ID.String()).Msg("social: moving a refunded seat month back failed; the reconcile pass retries")
	}
}

// accountName is an account name safe to put in a notification or email.
func accountName(a *models.SocialAccount) string {
	return displayname.DisplayableOr(a.DisplayName, "(unnamed)")
}

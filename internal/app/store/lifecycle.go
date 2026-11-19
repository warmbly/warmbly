package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// endedStatus is a purchased mailbox the provider no longer keeps.
func endedStatus(status string) bool {
	return status == graphkit.MailboxCancelled || status == graphkit.MailboxFailed
}

// MailboxLocked reports a connected mailbox the workspace bought and still has:
// removing it would not stop its billing, so members cancel it instead.
func (s *Service) MailboxLocked(ctx context.Context, orgID, emailAccountID uuid.UUID) (bool, error) {
	if s == nil || s.Repo == nil {
		return false, nil
	}
	m, err := s.Repo.GetMailboxByAccount(ctx, orgID, emailAccountID)
	if err != nil || m == nil {
		return false, err
	}
	return !endedStatus(m.Status), nil
}

// retire switches off the Warmbly mailbox of a purchase that ended, keeping its
// history, and says so once. A failed switch-off leaves the row for the sweep.
func (s *Service) retire(ctx context.Context, row *models.StoreMailbox) *models.StoreMailbox {
	if row == nil || !endedStatus(row.Status) || row.CancelledAt != nil {
		return row
	}
	switched := false
	if row.EmailAccountID != nil {
		if s.Mailboxes == nil {
			return row
		}
		off := "inactive"
		_, xerr := s.Mailboxes.Update(ctx, row.OrganizationID.String(), s.purchaser(ctx, row).String(), row.EmailAccountID.String(), &models.UpdateEmail{Status: &off})
		if xerr != nil && xerr.Code != errx.NotFound {
			log.Warn().Str("store_mailbox_id", row.ID.String()).Str("reason", xerr.ResponseCode()).Msg("store: switching off an ended purchased mailbox failed")
			return row
		}
		switched = xerr == nil
	}
	won, err := s.Repo.MarkMailboxEnded(ctx, row.OrganizationID, row.ID, s.now())
	if err != nil || !won {
		return row
	}
	if switched && row.Status == graphkit.MailboxCancelled {
		s.notify(ctx, row.OrganizationID, row.Email+" was cancelled and switched off",
			"Its subscription ended and the mailbox no longer exists, so we switched it off: sending, syncing and warmup stopped. Its history stays in Warmbly; disconnect it to delete that too.",
			mailboxesLink, "store_mailbox_ended_"+row.ID.String())
	}
	fresh, err := s.Repo.GetMailbox(ctx, row.OrganizationID, row.ID)
	if err != nil || fresh == nil {
		return row
	}
	s.publishMailbox(ctx, fresh)
	return fresh
}

// retireEnded catches ended mailboxes whose switch-off did not go through yet.
func (s *Service) retireEnded(ctx context.Context) error {
	rows, err := s.Repo.UnmarkedEndedMailboxes(ctx, 100)
	if err != nil {
		return err
	}
	for i := range rows {
		s.retire(ctx, &rows[i])
	}
	return nil
}

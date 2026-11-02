package warmupramp

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// ReplyShareWindowDays is how many UTC days of warmup sends a mailbox's reply share is read over.
const ReplyShareWindowDays = 7

// ReplyDeficit is how many replies a mailbox owes, counting its next send, for
// replies to be target percent of its warmup sends; zero or less is at or ahead.
func ReplyDeficit(target, sent, replied int) float64 {
	if target <= 0 {
		return 0
	}
	return float64(min(target, 100))/100*float64(sent+1) - float64(replied)
}

// ReplyDue reports whether the next send should be a reply. roll is uniform in
// [0,1), so a fractional deficit is a chance and a whole one is certain.
func ReplyDue(deficit, roll float64) bool {
	return deficit >= 1 || (deficit > 0 && roll < deficit)
}

// RecoverySignals are the reads that decide whether a sender gets recovery
// engagement. Any of them may be nil.
type RecoverySignals struct {
	Health interface {
		GetHealthState(context.Context, uuid.UUID) (models.WarmupHealthState, *time.Time, error)
	}
	Mailboxes interface {
		GetByID(context.Context, uuid.UUID) (*models.Email, *errx.Error)
	}
	Campaigns interface {
		CountActiveCampaignsForAccount(context.Context, uuid.UUID) (int, error)
	}
}

// Recovering reports whether the sender's warmup mail is answered and rescued
// first: its standing is watch or throttled, it is a consumer Gmail or
// Outlook.com mailbox, or it backs a live campaign, whose cold mail draws few replies.
func (r RecoverySignals) Recovering(ctx context.Context, sender uuid.UUID) bool {
	if r.Health != nil {
		state, _, err := r.Health.GetHealthState(ctx, sender)
		if err == nil && (state == models.WarmupHealthWatch || state == models.WarmupHealthThrottled) {
			return true
		}
	}
	if r.Mailboxes != nil {
		if m, xerr := r.Mailboxes.GetByID(ctx, sender); xerr == nil && m != nil && m.ConsumerMailbox() {
			return true
		}
	}
	if r.Campaigns == nil {
		return false
	}
	n, err := r.Campaigns.CountActiveCampaignsForAccount(ctx, sender)
	return err == nil && n > 0
}

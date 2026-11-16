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

// ReplyDue reports whether a reply as the next send leaves the share no farther from target than a new thread.
func ReplyDue(target, sent, replied int) bool {
	if target <= 0 {
		return false
	}
	return float64(replied)+0.5 < float64(min(target, 100))/100*float64(sent+1)
}

// ReplyChance is the chance the next plain send answers owed mail: half again the target while a reply is due, else zero.
func ReplyChance(target, sent, replied int) float64 {
	if !ReplyDue(target, sent, replied) {
		return 0
	}
	return min(1.5*float64(min(target, 100))/100, 1)
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

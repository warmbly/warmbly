package scheduler

import (
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// followUpRotation is how a follow-up is placed on a campaign that rotates
// them instead of pinning the lead to one mailbox.
type followUpRotation struct {
	active bool
	// previous is the mailbox that sent the lead's last step, when it is still
	// in the pool.
	previous *models.Email
	// domain limits the draw to mailboxes sending as this domain (same_domain).
	domain string
}

// rotatedFollowUp reads the campaign's follow-up setting for one lead. A first
// email is always placed by rotation, so it is never a rotated follow-up.
func rotatedFollowUp(campaign *models.Campaign, pair *repository.ContactSequencePair, previous *models.Email) followUpRotation {
	if !campaign.FollowUpSender.Rotates() || pair.IsNewLead {
		return followUpRotation{}
	}
	r := followUpRotation{active: true, previous: previous}
	if campaign.FollowUpSender == models.FollowUpSameDomain && previous != nil {
		r.domain = previous.SenderDomain()
	}
	return r
}

// sameDomain narrows the candidates to the lead's domain. When none of them is
// on it but a mailbox on it is only paced, the lead waits for it (busy); when
// the domain has no mailbox left that could send, the whole pool is used.
func (r followUpRotation) sameDomain(candidates []AccountCandidate, accounts []models.Email, gates map[uuid.UUID]mailboxGate) ([]AccountCandidate, time.Time, bool) {
	var same []AccountCandidate
	for _, c := range candidates {
		if c.Account.SenderDomain() == r.domain {
			same = append(same, c)
		}
	}
	if len(same) > 0 {
		return same, time.Time{}, false
	}
	var back time.Time
	busy := false
	for i := range accounts {
		if accounts[i].SenderDomain() != r.domain {
			continue
		}
		gate, ok := gates[accounts[i].ID]
		if !ok || !gate.paced {
			continue
		}
		busy = true
		if !gate.reopensAt.IsZero() && (back.IsZero() || gate.reopensAt.Before(back)) {
			back = gate.reopensAt
		}
	}
	return nil, back, busy
}

// avoidPrevious drops the mailbox that sent the last step when another one is
// free, so a rotated follow-up comes from a different address.
func (r followUpRotation) avoidPrevious(pick []AccountCandidate) []AccountCandidate {
	if !r.active || r.previous == nil || len(pick) < 2 {
		return pick
	}
	out := make([]AccountCandidate, 0, len(pick))
	for _, c := range pick {
		if c.Account.ID != r.previous.ID {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return pick
	}
	return out
}

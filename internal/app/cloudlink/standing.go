package cloudlink

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// knownHealthState keeps an unrecognised state from a newer cloud out of the gates.
func knownHealthState(state string) bool {
	switch models.WarmupHealthState(state) {
	case models.WarmupHealthHealthy, models.WarmupHealthWatch, models.WarmupHealthThrottled,
		models.WarmupHealthQuarantined, models.WarmupHealthBlocked:
		return true
	}
	return false
}

// recordStanding stores what the cloud reported. An absent standing leaves
// the recorded one in place: a cloud that could not read it has not lifted it.
// initial records only a first standing; a change is the sync's to report.
func (s *service) recordStanding(ctx context.Context, accountID uuid.UUID, h *models.WarmupHealthInfo, initial bool) (models.WarmupHealthState, bool) {
	if h == nil || !knownHealthState(h.State) {
		return "", false
	}
	prev, err := s.repo.SetStanding(ctx, accountID, h, initial)
	if err != nil {
		log.Warn().Err(err).Str("account_id", accountID.String()).Msg("cloud link: warmup standing could not be recorded")
		return "", false
	}
	return prev, true
}

// carryStanding keeps a cloud quarantine or block in force on the local pool
// row a mailbox rejoins when it leaves the cloud.
func (s *service) carryStanding(ctx context.Context, m models.CloudLinkMailbox) error {
	h := m.Standing
	if h == nil {
		return nil
	}
	switch models.WarmupHealthState(h.State) {
	case models.WarmupHealthBlocked:
		// A block with no end requires review; it carries as one.
		if h.BlockedUntil != nil && !h.BlockedUntil.After(time.Now()) {
			return nil
		}
	case models.WarmupHealthQuarantined:
		if h.BlockedUntil != nil && !h.BlockedUntil.After(time.Now()) {
			return nil
		}
	default:
		return nil
	}
	return s.repo.CarryStanding(ctx, m.EmailAccountID, h)
}

func mailboxGroups(rows []models.CloudLinkMailbox) map[uuid.UUID][]models.CloudLinkMailbox {
	groups := map[uuid.UUID][]models.CloudLinkMailbox{}
	for _, m := range rows {
		groups[m.InstanceID] = append(groups[m.InstanceID], m)
	}
	return groups
}

func (s *service) SyncStanding(ctx context.Context) ([]models.CloudLinkStandingChange, *errx.Error) {
	if !reconciliationLocked(ctx) {
		var changes []models.CloudLinkStandingChange
		xerr := s.reconcileLocked(ctx, func(ctx context.Context) *errx.Error {
			var xerr *errx.Error
			changes, xerr = s.SyncStanding(ctx)
			return xerr
		})
		return changes, xerr
	}
	links, err := s.repo.ListLinks(ctx)
	if err != nil {
		return nil, errx.InternalError()
	}
	enrolled, err := s.repo.List(ctx)
	if err != nil {
		return nil, errx.InternalError()
	}
	var changes []models.CloudLinkStandingChange
	var lastError *errx.Error
	groups := mailboxGroups(enrolled)
	for i := range links {
		l := &links[i]
		part, xerr := s.reconcileLinkStanding(ctx, l, groups[l.InstanceID])
		if xerr != nil {
			lastError = xerr
			continue
		}
		changes = append(changes, part...)
	}
	return changes, lastError
}

func (s *service) reconcileLinkStanding(ctx context.Context, l *models.CloudLink, enrolled []models.CloudLinkMailbox) ([]models.CloudLinkStandingChange, *errx.Error) {
	if l.DisconnectPending {
		orgID := uuid.Nil
		if l.OrganizationID != nil {
			orgID = *l.OrganizationID
		}
		return nil, s.Disconnect(ctx, orgID, l.OrganizationID == nil)
	}
	if xerr := s.reconcileManagedConsents(ctx, l); xerr != nil {
		return nil, xerr
	}
	if xerr := s.reconcileEnrollments(ctx, l, enrolled); xerr != nil {
		return nil, xerr
	}
	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, errx.InternalError()
	}
	enrolled = mailboxGroups(rows)[l.InstanceID]
	if len(enrolled) == 0 {
		return nil, nil
	}
	return s.syncStanding(ctx, l, enrolled)
}

func (s *service) syncStanding(ctx context.Context, l *models.CloudLink, enrolled []models.CloudLinkMailbox) ([]models.CloudLinkStandingChange, *errx.Error) {
	byRemote, xerr := s.fetchStanding(ctx, l)
	if xerr != nil {
		return nil, xerr
	}
	var changes []models.CloudLinkStandingChange
	for _, m := range enrolled {
		h := byRemote[m.RemoteID]
		if h == nil || !knownHealthState(h.State) {
			if err := s.repo.InvalidateStanding(ctx, m.EmailAccountID); err != nil {
				return changes, errx.InternalError()
			}
			continue
		}
		prev, ok := s.recordStanding(ctx, m.EmailAccountID, h, false)
		if !ok {
			return changes, errx.InternalError()
		}
		// A first reading is measured against the unrestricted mailbox this
		// instance saw until now, so a hold it starts enforcing is announced.
		if prev == "" {
			prev = models.WarmupHealthHealthy
		}
		if prev == models.WarmupHealthState(h.State) {
			continue
		}
		changes = append(changes, models.CloudLinkStandingChange{
			EmailAccountID: m.EmailAccountID,
			Previous:       prev,
			Current:        models.WarmupHealthState(h.State),
			Reason:         h.Reason,
		})
	}
	return changes, nil
}

// fetchStanding reads every enrolled mailbox's standing, falling back to the
// full mailbox listing on a cloud that predates the standing route.
func (s *service) fetchStanding(ctx context.Context, l *models.CloudLink) (map[uuid.UUID]*models.WarmupHealthInfo, *errx.Error) {
	out := map[uuid.UUID]*models.WarmupHealthInfo{}
	var standing []models.PoolLinkMailboxStanding
	xerr := s.clientFor(l).do(ctx, http.MethodGet, "/instance/standing", nil, &standing)
	if xerr == nil {
		for _, st := range standing {
			out[st.RemoteID] = st.Health
		}
		return out, nil
	}
	if xerr.Code != errx.NotFound || strings.HasPrefix(xerr.Identifier, "pool_link_") {
		return nil, xerr
	}
	var states []models.PoolLinkMailboxState
	if xerr := s.clientFor(l).do(ctx, http.MethodGet, "/instance/mailboxes", nil, &states); xerr != nil {
		return nil, xerr
	}
	for i := range states {
		out[states[i].RemoteID] = states[i].Health
	}
	return out, nil
}

// sameStanding skips the write when the cloud reports what is already recorded.
func sameStanding(cur, next *models.WarmupHealthInfo) bool {
	if cur == nil || next == nil {
		return false
	}
	return cur.State == next.State && cur.Reason == next.Reason && cur.PoolType == next.PoolType &&
		cur.Score == next.Score && sameTime(cur.BlockedUntil, next.BlockedUntil)
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

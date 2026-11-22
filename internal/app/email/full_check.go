package email

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
)

// fullCheckAllowanceTTL is how long one workspace's mailbox allowance is reused across its mailboxes' loads.
const fullCheckAllowanceTTL = 5 * time.Minute

// orgAllowance is a workspace's mailboxes held against its allowance, as last read.
type orgAllowance struct {
	used      int
	allowance *int
	readAt    time.Time
}

// fullCheckAllowances caches each workspace's allowance so a load pass reads it once per workspace, not per mailbox.
type fullCheckAllowances struct {
	mu   sync.Mutex
	byID map[uuid.UUID]orgAllowance
}

// fullCheckInterval is how often the workspace's mailboxes get a full check.
// Without a workspace or an allowance source it is the configured minimum.
func (s *emailService) fullCheckInterval(ctx context.Context, budget instancesettings.Sync, orgID *uuid.UUID) time.Duration {
	if orgID == nil || s.allowance == nil {
		return budget.FullCheckInterval(0, nil)
	}
	now := time.Now()
	c := &s.fullCheck
	c.mu.Lock()
	cached, ok := c.byID[*orgID]
	c.mu.Unlock()
	if ok && now.Sub(cached.readAt) < fullCheckAllowanceTTL {
		return budget.FullCheckInterval(cached.used, cached.allowance)
	}
	a, xerr := s.allowance.MailboxAllowance(ctx, *orgID)
	if xerr != nil {
		log.Debug().Str("organization_id", orgID.String()).Str("error", xerr.Message).Msg("full check interval: allowance unavailable; using the minimum")
		return budget.FullCheckInterval(0, nil)
	}
	c.mu.Lock()
	if c.byID == nil || len(c.byID) > 100_000 {
		c.byID = map[uuid.UUID]orgAllowance{}
	}
	c.byID[*orgID] = orgAllowance{used: a.Used, allowance: a.Allowance, readAt: now}
	c.mu.Unlock()
	return budget.FullCheckInterval(a.Used, a.Allowance)
}

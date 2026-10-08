package typesafe

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

const billingCooldown = 5 * time.Minute
const billingProbeLease = 3 * time.Minute
const billingStateTTL = 24 * time.Hour

var ErrBillingCooldown = errors.New("typesafe: paid judgments paused after a billing failure")

type BillingCooldownError struct{ RetryAfter time.Duration }

func (e *BillingCooldownError) Error() string {
	return fmt.Sprintf("%s; retry in %s", ErrBillingCooldown, e.RetryAfter.Round(time.Second))
}

func (e *BillingCooldownError) Unwrap() error { return ErrBillingCooldown }

type billingPermit struct {
	owner  string
	shared bool
}

type billingGate struct {
	mu               sync.Mutex
	until            time.Time
	owner            string
	now              func() time.Time
	shared           *redis.Client
	key              string
	lastStoreWarning time.Time
}

func newBillingGate(apiKey string) *billingGate {
	return &billingGate{now: time.Now, key: fmt.Sprintf("typesafe:billing:%x", sha256.Sum256([]byte(apiKey)))}
}

// WithBillingRedis coordinates backend and consumer recovery for the same key.
func WithBillingRedis(client *redis.Client) Option {
	return func(c *Client) { c.billing.shared = client }
}

var acquireBilling = redis.NewScript(`
local until_ms = tonumber(redis.call('HGET', KEYS[1], 'until'))
if not until_ms then return 0 end
local clock = redis.call('TIME')
local now_ms = clock[1] * 1000 + math.floor(clock[2] / 1000)
if until_ms > now_ms then return until_ms - now_ms end
redis.call('HSET', KEYS[1], 'until', now_ms + ARGV[1], 'owner', ARGV[2])
redis.call('PEXPIRE', KEYS[1], ARGV[3])
return -1
`)

var finishBilling = redis.NewScript(`
if ARGV[1] == 'block' or redis.call('HGET', KEYS[1], 'owner') == ARGV[2] then
  if ARGV[1] == 'recover' then
    redis.call('DEL', KEYS[1])
  else
    local clock = redis.call('TIME')
    local now_ms = clock[1] * 1000 + math.floor(clock[2] / 1000)
    redis.call('HSET', KEYS[1], 'until', now_ms + ARGV[3], 'owner', '')
    redis.call('PEXPIRE', KEYS[1], ARGV[4])
  end
  return 1
end
return 0
`)

func (g *billingGate) acquire(ctx context.Context) (billingPermit, error) {
	if err := ctx.Err(); err != nil {
		return billingPermit{}, err
	}
	if g.shared != nil {
		owner := uuid.NewString()
		sctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		wait, err := acquireBilling.Run(sctx, g.shared, []string{g.key}, billingProbeLease.Milliseconds(), owner, billingStateTTL.Milliseconds()).Int64()
		cancel()
		if err == nil {
			if wait > 0 {
				return billingPermit{}, &BillingCooldownError{RetryAfter: time.Duration(wait) * time.Millisecond}
			}
			if wait == -1 {
				return billingPermit{owner: owner, shared: true}, nil
			}
			return billingPermit{shared: true}, nil
		}
		g.storeWarning()
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.until.IsZero() {
		return billingPermit{}, nil
	}
	now := g.now()
	if now.Before(g.until) {
		return billingPermit{}, &BillingCooldownError{RetryAfter: g.until.Sub(now)}
	}
	g.owner = uuid.NewString()
	g.until = now.Add(billingProbeLease)
	return billingPermit{owner: g.owner}, nil
}

func (g *billingGate) finish(_ context.Context, p billingPermit, outcome error) {
	var apiErr *APIError
	billing := asAPIError(outcome, &apiErr) && apiErr.Status == http.StatusPaymentRequired
	if !billing && p.owner == "" {
		return
	}
	action := "recover"
	if outcome != nil {
		action = "defer"
	}
	if billing {
		action = "block"
	}
	recovered := false
	g.mu.Lock()
	if billing || (!p.shared && g.owner == p.owner) {
		g.owner = ""
		if outcome == nil {
			g.until = time.Time{}
			recovered = true
		} else {
			g.until = g.now().Add(billingCooldown)
		}
	}
	g.mu.Unlock()
	if g.shared != nil {
		// A cancelled feature request must still publish its billing result.
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		applied, err := finishBilling.Run(ctx, g.shared, []string{g.key}, action, p.owner, billingCooldown.Milliseconds(), billingStateTTL.Milliseconds()).Int64()
		cancel()
		if err != nil {
			g.storeWarning()
		}
		if p.shared && action == "recover" && (err != nil || applied == 0) {
			return
		}
		if p.shared && action == "recover" {
			recovered = true
		}
	}
	if billing {
		log.Warn().Str("event", "typesafe_billing_paused").Dur("cooldown", billingCooldown).Msg("TypeSafe paid judgments paused; mail ingestion continues")
	} else if recovered {
		log.Info().Str("event", "typesafe_billing_recovered").Msg("TypeSafe paid judgments resumed")
	}
}

func (g *billingGate) storeWarning() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.now().Sub(g.lastStoreWarning) < time.Minute {
		return
	}
	g.lastStoreWarning = g.now()
	log.Warn().Str("event", "typesafe_billing_store_unavailable").Msg("TypeSafe billing cooldown is process-local while Redis is unavailable")
}

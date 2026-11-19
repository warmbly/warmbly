package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// Where the GraphKit connection comes from. The environment wins over the admin panel.
const (
	SourceNone        = "none"
	SourceEnvironment = "environment"
	SourceSettings    = "settings"
)

// upstream is one GraphKit connection, swapped whole so a reader never sees half of a change.
type upstream struct {
	// api is kept after a disconnect so a call already past Enabled finishes against the old client instead of a nil one.
	api     graphkit.API
	on      bool
	secrets []string
	// previous verifies until previousUntil, read at each delivery so the overlap ends without a reload.
	previous      string
	previousUntil time.Time
	simulate      bool
	source        string
	// fingerprint tells a stored configuration that changed from one that did not; never logged.
	fingerprint string
	stop        context.CancelFunc
}

func (s *Service) current() *upstream {
	if s == nil {
		return nil
	}
	return s.up.Load()
}

// api is the GraphKit client in use; callers check Enabled first.
func (s *Service) api() graphkit.API {
	if u := s.current(); u != nil {
		return u.api
	}
	return nil
}

func (s *Service) webhookSecrets() []string {
	u := s.current()
	if u == nil {
		return nil
	}
	if u.previous != "" && s.now().Before(u.previousUntil) {
		return append(append([]string(nil), u.secrets...), u.previous)
	}
	return u.secrets
}

func (s *Service) simulating() bool {
	u := s.current()
	return u != nil && u.on && u.simulate
}

// Source is where the connection in use came from, SourceNone when the store is off.
func (s *Service) Source() string {
	if u := s.current(); u != nil && u.on {
		return u.source
	}
	return SourceNone
}

func fingerprint(cfg config.GraphKit) string {
	var rotated string
	if cfg.WebhookSecretRotatedAt != nil {
		rotated = cfg.WebhookSecretRotatedAt.UTC().Format(time.RFC3339Nano)
	}
	h := sha256.Sum256([]byte(cfg.APIURL + "\x00" + cfg.APIKey + "\x00" + cfg.WebhookSecret + "\x00" + cfg.WebhookSecretPrevious + "\x00" + rotated))
	return hex.EncodeToString(h[:])
}

// Connect points the store at a GraphKit account, or turns it off when cfg names none, without a restart.
func (s *Service) Connect(cfg config.GraphKit, source string) error {
	api, fake, err := NewAPI(cfg)
	if err != nil {
		return err
	}
	next := &upstream{api: api, on: api != nil, secrets: nonEmpty(cfg.WebhookSecret),
		simulate: fake != nil || cfg.Sandbox(), source: source, fingerprint: fingerprint(cfg)}
	switch {
	case source != SourceSettings:
		// GRAPHKIT_WEBHOOK_SECRET_PREVIOUS verifies until the operator removes it.
		next.secrets = nonEmpty(cfg.WebhookSecret, cfg.WebhookSecretPrevious)
	case cfg.WebhookSecretPrevious != "" && cfg.WebhookSecretRotatedAt != nil:
		next.previous, next.previousUntil = cfg.WebhookSecretPrevious, cfg.WebhookSecretRotatedAt.Add(webhookPreviousSecretTTL)
	}
	if api == nil {
		next.source = SourceNone
	}
	if fake != nil {
		ctx, cancel := context.WithCancel(s.baseContext())
		next.stop = cancel
		fake.OnEvent(func(ev graphkit.Event) {
			if err := s.ApplyEvent(context.WithoutCancel(ctx), ev); err != nil {
				log.Warn().Err(err).Str("event_type", ev.Type).Msg("store simulator: applying an event failed")
			}
		})
		go fake.Run(ctx)
	}
	s.swap(next)
	return nil
}

// Disconnect turns the store off; purchases stay recorded and resume with the next connection.
func (s *Service) Disconnect() {
	s.swap(&upstream{source: SourceNone})
}

func (s *Service) swap(next *upstream) {
	prev := s.up.Swap(next)
	if prev != nil {
		if next.api == nil {
			next.api = prev.api
		}
		if prev.stop != nil {
			prev.stop()
		}
	}
	s.mu.Lock()
	s.pricing = nil
	s.mu.Unlock()
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

func nonEmpty(vals ...string) []string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func (s *Service) baseContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.base == nil {
		return context.Background()
	}
	return s.base
}

// Start keeps the store's loops running while a connection is in use, starting and stopping them as it changes, until ctx ends.
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	s.base = ctx
	s.mu.Unlock()
	go s.run(ctx)
}

func (s *Service) run(ctx context.Context) {
	var stop context.CancelFunc
	var seen *upstream
	defer func() {
		if stop != nil {
			stop()
		}
	}()
	for {
		u := s.current()
		on := u != nil && u.on
		switch {
		case on && stop == nil:
			lctx, cancel := context.WithCancel(ctx)
			stop = cancel
			go jobrun.Loop(lctx, LoopReconcile, time.Minute, true, s.Reconcile)
			go jobrun.Loop(lctx, LoopBilling, time.Hour, true, s.Bill)
		case !on && stop != nil:
			stop()
			stop = nil
		}
		if on && u != seen {
			// A new account learns this instance's delegation identities now; the billing loop retries a failure.
			go func() {
				if err := s.PushDelegation(ctx); err != nil {
					log.Warn().Msg("store: registering delegation identities failed; the billing loop retries")
				}
			}()
		}
		seen = u
		select {
		case <-ctx.Done():
			return
		case <-s.changed:
		}
	}
}

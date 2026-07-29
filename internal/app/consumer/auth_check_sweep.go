package jobs

import (
	"context"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/pkg/dnsauth"
)

const authCheckSweepBatch = 500

// StartAuthCheckSweep periodically evaluates the SPF/DKIM/DMARC state of each
// active mailbox's sending domain and persists it. Observe-only: the state is
// recorded and surfaced (and failing domains are logged), but sending and
// warmup are NOT gated on it yet.
//
// Each tick claims the oldest-checked active mailboxes whose domain has not been
// evaluated within staleAfter, dedupes them by sending domain (auth is a
// per-domain property), and runs one DNS lookup per unique domain.
func (s *JobsService) StartAuthCheckSweep(ctx context.Context, interval, staleAfter time.Duration) {
	if s.EmailRepository == nil {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			s.runAuthCheckSweep(sweepCtx, staleAfter)
			cancel()
		}
	}
}

func (s *JobsService) runAuthCheckSweep(ctx context.Context, staleAfter time.Duration) {
	staleBefore := time.Now().Add(-staleAfter)
	targets, xerr := s.EmailRepository.ListAuthCheckDue(ctx, staleBefore, authCheckSweepBatch)
	if xerr != nil {
		log.Warn().Str("error", xerr.Error()).Msg("auth-check sweep: failed to list due mailboxes")
		return
	}
	if len(targets) == 0 {
		return
	}

	// Dedupe by sending domain so each unique domain is resolved once per tick.
	seen := make(map[string]struct{}, len(targets))
	checkedAt := time.Now()
	var checked, failing int

	for _, t := range targets {
		domain := authDomainOf(t.Email)
		if domain == "" {
			continue
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}

		res := dnsauth.Check(ctx, domain, nil)
		state := res.State()
		if uerr := s.EmailRepository.UpdateDomainAuthState(ctx, domain, state, res.SPFFound, res.DKIMFound, res.DMARCFound, res.DMARCPolicy, res.Summary, checkedAt); uerr != nil {
			log.Warn().Str("domain", domain).Str("error", uerr.Error()).Msg("auth-check sweep: failed to persist auth state")
			continue
		}
		checked++
		if state == "failing" {
			failing++
			log.Warn().Str("domain", domain).Str("summary", res.Summary).Msg("auth-check sweep: sending domain is unauthenticated")
		}
	}

	if checked > 0 {
		log.Info().Int("domains_checked", checked).Int("failing", failing).Msg("auth-check sweep completed")
	}
}

// authDomainOf extracts the lowercased domain part of an email address.
func authDomainOf(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at+1 >= len(email) {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(email[at+1:]))
}

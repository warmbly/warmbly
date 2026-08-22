package jobs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/dnsauth"
)

const authCheckSweepBatch = 500

// StartAuthCheckSweep periodically evaluates the SPF/DKIM/DMARC state of each
// active mailbox's sending domain and persists it. The sweep only records
// state; enforcement lives in the send and warmup paths, so an operator can
// turn the gate off without losing the signal.
//
// Each tick claims the oldest-checked active mailboxes not evaluated within
// staleAfter and runs one DNS lookup per unique sending domain.
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
		transitions, uerr := s.EmailRepository.UpdateDomainAuthState(ctx, domain, state, res.SPFFound, res.DKIMFound, res.DMARCFound, res.DMARCPolicy, res.Summary, checkedAt)
		if uerr != nil {
			log.Warn().Str("domain", domain).Str("error", uerr.Error()).Msg("auth-check sweep: failed to persist auth state")
			continue
		}
		checked++
		if state == models.AuthStateFailing {
			failing++
			log.Warn().Str("domain", domain).Str("summary", res.Summary).Msg("auth-check sweep: sending domain is unauthenticated")
		}
		s.notifyDomainAuthFailing(ctx, domain, res.Summary, transitions)
	}

	if checked > 0 {
		log.Info().Int("domains_checked", checked).Int("failing", failing).Msg("auth-check sweep completed")
	}
}

// notifyDomainAuthFailing warns each affected organization the moment its
// sending domain starts failing, which is also the moment the grace clock
// starts. Getting this out early is the whole point of the grace window: the
// owner should have days of warning before anything stops sending, not a
// silently paused campaign to discover later.
//
// Dedupe is structural rather than time-based: transitions only contains
// mailboxes that entered the failing state on this pass, so a domain that stays
// failing across sweeps produces nothing.
func (s *JobsService) notifyDomainAuthFailing(ctx context.Context, domain, summary string, transitions []models.EmailAuthTransition) {
	if s.Notifier == nil || len(transitions) == 0 {
		return
	}

	counts := map[uuid.UUID]int{}
	for _, t := range transitions {
		if t.OrganizationID == nil {
			continue
		}
		counts[*t.OrganizationID]++
	}

	for orgID, n := range counts {
		subject := fmt.Sprintf("%d mailboxes on %s are", n, domain)
		if n == 1 {
			subject = fmt.Sprintf("A mailbox on %s is", domain)
		}
		body := fmt.Sprintf(
			"%s sending from a domain that is failing authentication (%s). Gmail, Yahoo, and Outlook reject or spam-filter unauthenticated mail, so add the missing DNS records at your registrar. If the domain is still failing after the grace period, cold sending and warmup from those mailboxes stop automatically.",
			subject, summary)
		s.Notifier.NotifyOrg(ctx, orgID, models.PermManageEmails, uuid.Nil, models.NotifDomainAuth,
			"Sending domain is failing authentication", body, "/app/emails",
			map[string]any{"domain": domain, "mailboxes": n, "summary": summary},
			"domain_auth:"+domain)
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

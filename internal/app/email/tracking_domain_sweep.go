package email

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/pkg/trackdns"
)

const trackingDomainSweepBatch = 500

// StartTrackingDomainSweep periodically re-resolves every custom tracking
// domain and records the verdict.
//
// Verification used to happen once, on save, and never again. That is wrong in
// both directions: a customer whose record has just propagated stays "pending"
// until they think to press a button, and a domain whose record later changes
// or disappears keeps routing every tracked link in their campaigns at a host
// that no longer answers. DNS is not a one-time fact.
//
// Runs in the backend rather than the consumer because the tracking host it
// compares against is the backend's own TRACKING_DOMAIN.
func (s *emailService) StartTrackingDomainSweep(ctx context.Context, interval, staleAfter time.Duration) {
	if s.emailRepository == nil {
		return
	}

	jobrun.Loop(ctx, "tracking_domain_sweep", interval, false, func(ctx context.Context) error {
		sweepCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		return s.runTrackingDomainSweep(sweepCtx, staleAfter)
	})
}

func (s *emailService) runTrackingDomainSweep(ctx context.Context, staleAfter time.Duration) error {
	target := config.TrackingHostname()
	if target == "" {
		// Nothing to compare against. Unverifying every mailbox because this
		// install has no tracking host would be the worst of both worlds.
		return nil
	}

	due, xerr := s.emailRepository.ListTrackingDomainCheckDue(ctx, time.Now().Add(-staleAfter), trackingDomainSweepBatch)
	if xerr != nil {
		return fmt.Errorf("tracking-domain sweep: list due mailboxes: %s", xerr.Error())
	}

	var verified, revoked, failed int
	for _, t := range due {
		if ctx.Err() != nil {
			break
		}

		lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		res := trackdns.VerifyWith(lookupCtx, trackdns.DefaultResolver, t.Domain, target)
		cancel()
		// A mailbox that is not verified yet needs the workspace's TXT proof too.
		res = s.customHosts.Gate(ctx, res, t.OrganizationID, t.Verified)
		if res.Code == trackdns.CodeLookupError {
			// A resolver hiccup is not evidence of a broken record, and
			// revoking on one would move a working customer's links back to
			// the shared host for an hour.
			continue
		}

		switch {
		case res.Verified:
			now := time.Now().UTC()
			if err := s.emailRepository.SetTrackingDomainVerified(ctx, t.ID, true, &now); err != nil {
				if err == errx.ErrTrackingDomainTaken {
					// Another workspace holds this host; it stays unverified here.
					continue
				}
				log.Warn().Str("domain", t.Domain).Str("error", err.Error()).Msg("tracking-domain sweep: failed to persist")
				failed++
				continue
			}
			if !t.Verified {
				verified++
			}
		case t.Verified:
			if err := s.emailRepository.SetTrackingDomainVerified(ctx, t.ID, false, nil); err != nil {
				log.Warn().Str("domain", t.Domain).Str("error", err.Error()).Msg("tracking-domain sweep: failed to persist")
				failed++
				continue
			}
			revoked++
			log.Warn().Str("domain", t.Domain).Str("reason", res.Reason).Msg("tracking-domain sweep: a verified tracking domain no longer resolves to the tracking host")
		}
	}

	if verified > 0 || revoked > 0 {
		log.Info().Int("checked", len(due)).Int("newly_verified", verified).Int("revoked", revoked).Msg("tracking-domain sweep completed")
	}
	if failed > 0 {
		return fmt.Errorf("tracking-domain sweep: %d of %d verdicts could not be saved", failed, len(due))
	}
	return nil
}

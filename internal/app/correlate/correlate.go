// Package correlate runs the nightly abuse sweep: the pass that watches a group
// of accounts rather than one subject, and the pass that reads what an
// organization's mail did to the people who received it.
package correlate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/orgrisk"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/repository"
)

// Weights. Small on purpose: sharing an address is common (an office, a
// co-working space, a VPN), so this contributes evidence rather than a verdict.
const (
	weightSharedIP       = 15
	weightSharedIdentity = 25
	weightMailboxBurst   = 15

	// LookbackWindow bounds how far back accounts are correlated. Two
	// organizations opened from one office a year apart are not a cluster.
	LookbackWindow = 30 * 24 * time.Hour
	// MinClusterMembers is the group size below which nothing is recorded.
	MinClusterMembers = 3
	// MailboxBurstCount and MailboxBurstWindow describe a sending fleet being
	// stood up rather than a business connecting its inboxes.
	MailboxBurstCount  = 15
	MailboxBurstWindow = 24 * time.Hour
)

// Recipient-outcome bands: what the organization did to real people, which is
// the evidence a band is allowed to act on.
const (
	// HarmSample is the send count below which a rate means nothing, and
	// HarmWindow is how far back outcomes are read.
	HarmSample = 100
	HarmWindow = 30 * 24 * time.Hour

	// Complaints: Google asks senders to stay under 0.10% and never reach
	// 0.30%; SES puts an account under review at 0.1%.
	complaintRateWarning  = 0.1
	complaintRateCritical = 0.3
	// Bounces: SES reviews an account at 5% and can pause sending at 10%.
	bounceRateWarning  = 5.0
	bounceRateCritical = 10.0

	// A warning band alone reaches watch and takes nothing away; combined with
	// any other finding it restricts. The critical band restricts on its own.
	weightHarmWarning  = 25
	weightHarmCritical = 50
)

// Service runs the sweep.
type Service struct {
	repo    repository.CorrelationRepository
	conduct repository.OrgConductRepository
	orgRisk orgrisk.Service
}

// NewService builds the sweep. conduct may be nil, in which case the
// recipient-outcome pass is skipped and its findings are left untouched.
func NewService(repo repository.CorrelationRepository, conduct repository.OrgConductRepository, risk orgrisk.Service) *Service {
	return &Service{repo: repo, conduct: conduct, orgRisk: risk}
}

// Start runs the sweep on an interval until the context ends.
func (s *Service) Start(ctx context.Context, interval time.Duration) {
	if s == nil || s.repo == nil || s.orgRisk == nil {
		return
	}
	jobrun.Loop(ctx, "correlation_sweep", interval, false, func(ctx context.Context) error {
		s.Run(ctx)
		return nil
	})
}

// finding is one detector's verdict on one organization.
type finding struct {
	weight int
	detail string
}

// Run performs one sweep, filing each finding as a signal on every member
// organization. Signals are retracted from organizations that no longer match
// before current ones are recorded, so a score can fall as well as climb.
func (s *Service) Run(ctx context.Context) {
	since := time.Now().Add(-LookbackWindow)
	recorded := 0

	if clusters, err := s.repo.ClustersBySignupIP(ctx, MinClusterMembers, since); err != nil {
		log.Warn().Err(err).Msg("correlation sweep: signup-ip clustering failed")
	} else {
		recorded += s.record(ctx, "cluster_signup_ip", orgrisk.ClassCircumstantial,
			clusterFindings(clusters, MinClusterMembers, weightSharedIP, func(others int) string {
				return fmt.Sprintf("opened alongside %d other workspaces from one address", others)
			}))
	}

	if clusters, err := s.repo.ClustersBySignupIdentity(ctx, MinClusterMembers, since); err != nil {
		log.Warn().Err(err).Msg("correlation sweep: identity clustering failed")
	} else {
		recorded += s.record(ctx, "cluster_signup_identity", orgrisk.ClassCircumstantial,
			clusterFindings(clusters, MinClusterMembers, weightSharedIdentity, func(others int) string {
				return fmt.Sprintf("opened alongside %d other workspaces by the same email identity", others)
			}))
	}

	if bursts, err := s.repo.OrgsConnectingMailboxesFast(ctx, MailboxBurstCount, MailboxBurstWindow); err != nil {
		log.Warn().Err(err).Msg("correlation sweep: mailbox burst query failed")
	} else {
		burst := fmt.Sprintf("connected %d or more mailboxes within %d hours",
			MailboxBurstCount, int(MailboxBurstWindow.Hours()))
		// A burst is one organization, so its cluster has a single member.
		recorded += s.record(ctx, "mailbox_burst", orgrisk.ClassCircumstantial,
			clusterFindings(bursts, 1, weightMailboxBurst, func(int) string { return burst }))
	}

	if s.conduct != nil {
		if outcomes, err := s.conduct.OrgRecipientOutcomes(ctx, HarmSample, HarmWindow); err != nil {
			log.Warn().Err(err).Msg("correlation sweep: recipient outcome query failed")
		} else {
			recorded += s.record(ctx, "recipient_harm", orgrisk.ClassSubstantive, harmFindings(outcomes))
		}
	}

	if recorded > 0 {
		log.Info().Int("findings", recorded).Msg("correlation sweep recorded cross-account findings")
	}
}

// clusterFindings turns one detector's clusters into a finding per member.
func clusterFindings(clusters []repository.Cluster, minMembers, weight int, detail func(others int) string) map[uuid.UUID]finding {
	out := make(map[uuid.UUID]finding)
	for _, c := range clusters {
		if len(c.OrganizationIDs) < minMembers || len(c.OrganizationIDs) == 0 {
			continue
		}
		// "%d OTHER workspaces", so the sentence reads correctly to a member.
		f := finding{weight: weight, detail: detail(len(c.OrganizationIDs) - 1)}
		for _, orgID := range c.OrganizationIDs {
			out[orgID] = f
		}
	}
	return out
}

// harmFindings reads each organization's bounce and complaint rates against the
// provider bands. Both are measured on the same sends.
func harmFindings(outcomes []repository.OrgConduct) map[uuid.UUID]finding {
	out := make(map[uuid.UUID]finding)
	for _, o := range outcomes {
		if o.Sent < HarmSample {
			continue
		}
		complaints := rate(o.Complained, o.Sent)
		bounces := rate(o.Bounced, o.Sent)

		weight := 0
		details := make([]string, 0, 2)
		if w := harmWeight(complaints, complaintRateWarning, complaintRateCritical); w > 0 {
			weight = w
			details = append(details, fmt.Sprintf("recipients reported %.2f%% of the last %d sends as spam", complaints, o.Sent))
		}
		if w := harmWeight(bounces, bounceRateWarning, bounceRateCritical); w > 0 {
			if w > weight {
				weight = w
			}
			details = append(details, fmt.Sprintf("%.1f%% of the last %d sends bounced", bounces, o.Sent))
		}
		if weight == 0 {
			continue
		}
		out[o.OrganizationID] = finding{weight: weight, detail: strings.Join(details, "; ")}
	}
	return out
}

// harmWeight maps one rate onto its band. The two bands are not added: a rate
// past both is one failure, not two.
func harmWeight(value, warning, critical float64) int {
	switch {
	case value >= critical:
		return weightHarmCritical
	case value >= warning:
		return weightHarmWarning
	default:
		return 0
	}
}

func rate(part, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

// record files a detector's current findings and retracts it from every
// organization that carried it and no longer matches.
func (s *Service) record(ctx context.Context, key string, class orgrisk.SignalClass, matched map[uuid.UUID]finding) int {
	// Retract first, so an organization that dropped out of every cluster does
	// not keep the weight.
	if previous, err := s.orgRisk.OrgsWithSignal(ctx, key); err != nil {
		log.Warn().Str("signal", key).Msg("correlation sweep: could not list previous holders")
	} else {
		for _, orgID := range previous {
			if _, still := matched[orgID]; still {
				continue
			}
			if _, cerr := s.orgRisk.ClearSignal(ctx, orgID, key); cerr != nil {
				log.Warn().Str("organization_id", orgID.String()).Str("signal", key).
					Msg("correlation sweep: could not retract a finding that no longer holds")
			}
		}
	}

	for orgID, f := range matched {
		if _, err := s.orgRisk.RecordSignal(ctx, orgID, orgrisk.Signal{
			Key: key, Weight: f.weight, Detail: f.detail, Class: class,
		}); err != nil {
			log.Warn().Str("organization_id", orgID.String()).Str("signal", key).
				Msg("correlation sweep: could not record signal")
		}
	}
	return len(matched)
}

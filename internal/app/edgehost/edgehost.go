// Package edgehost keeps every customer hostname this instance serves
// registered at the edge provider in front of its tracking host, so a domain
// that verifies starts answering without anyone adding it by hand.
package edgehost

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/pkg/cfsaas"
	"github.com/warmbly/warmbly/internal/repository"
)

// Edge is the provider's custom-hostname API.
type Edge interface {
	Ensure(ctx context.Context, name string) (*cfsaas.Hostname, error)
	Remove(ctx context.Context, name string) error
}

const (
	sweepInterval = 2 * time.Minute
	// pendingRecheck is how often a name the provider has not activated is asked about again.
	pendingRecheck = 2 * time.Minute
	// activeRecheck confirms an active name is still there, so one deleted at the provider comes back.
	activeRecheck = 24 * time.Hour
	// removeAfter is how long a name stays registered after nothing serves it, so a brief unverify does not drop its certificate.
	removeAfter = 24 * time.Hour
	// callsPerSweep keeps one sweep well inside the provider's API rate limit.
	callsPerSweep = 100
)

// Service registers and retires customer hostnames at the edge.
type Service struct {
	repo repository.EdgeHostnameRepository
	edge Edge
	// skip are this instance's own hosts, which live on the zone itself.
	skip func() []string
}

// New is nil without an edge, so callers need no switch of their own.
func New(repo repository.EdgeHostnameRepository, edge Edge, skip func() []string) *Service {
	if repo == nil || edge == nil {
		return nil
	}
	return &Service{repo: repo, edge: edge, skip: skip}
}

// Enabled reports whether this instance registers hostnames at an edge provider.
func (s *Service) Enabled() bool { return s != nil }

// Start runs the sweep on one replica per tick.
func (s *Service) Start(ctx context.Context) {
	if s == nil {
		return
	}
	jobrun.Loop(ctx, "edge_hostname_sweep", sweepInterval, true, func(ctx context.Context) error {
		sweepCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		return s.Sweep(sweepCtx)
	})
}

// Register asks the edge for hosts now rather than at the next sweep; a failure is left to the sweep.
func (s *Service) Register(ctx context.Context, hosts ...string) {
	if s == nil {
		return
	}
	have, err := s.repo.Get(ctx, normalizeAll(hosts))
	if err != nil {
		return
	}
	known := make(map[string]repository.EdgeHostname, len(have))
	for _, h := range have {
		known[h.Hostname] = h
	}
	for _, host := range normalizeAll(hosts) {
		if s.own(host) {
			continue
		}
		if row, ok := known[host]; ok && !s.due(row, time.Now()) {
			continue
		}
		s.ensure(ctx, host)
	}
}

// Pending is the TXT record the edge still needs for each of hosts it has not activated.
func (s *Service) Pending(ctx context.Context, hosts ...string) map[string]cfsaas.TXTRecord {
	if s == nil {
		return nil
	}
	rows, err := s.repo.Get(ctx, normalizeAll(hosts))
	if err != nil {
		return nil
	}
	out := map[string]cfsaas.TXTRecord{}
	for _, r := range rows {
		if r.Status != "active" && r.OwnershipName != "" && r.UnwantedSince == nil {
			out[r.Hostname] = cfsaas.TXTRecord{Name: r.OwnershipName, Value: r.OwnershipValue}
		}
	}
	return out
}

// Sweep registers what is served and missing, rechecks what is pending, and retires what nothing serves.
func (s *Service) Sweep(ctx context.Context) error {
	wanted, err := s.repo.Wanted(ctx)
	if err != nil {
		return err
	}
	rows, err := s.repo.List(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	known := make(map[string]repository.EdgeHostname, len(rows))
	for _, r := range rows {
		known[r.Hostname] = r
	}
	want := map[string]bool{}
	for _, h := range normalizeAll(wanted) {
		if !s.own(h) {
			want[h] = true
		}
	}

	hosts := make([]string, 0, len(want))
	for h := range want {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	calls, registered := 0, 0
	for _, h := range hosts {
		if row, ok := known[h]; ok && !s.due(row, now) {
			continue
		}
		if calls >= callsPerSweep || ctx.Err() != nil {
			break
		}
		calls++
		if s.ensure(ctx, h) {
			registered++
		}
	}

	// An empty read next to a populated table is more likely a fault than every domain leaving at once.
	if len(want) == 0 && len(rows) > 0 {
		log.Warn().Int("registered", len(rows)).Msg("edge hostnames: nothing is served, keeping every registration")
		return nil
	}
	var fresh []string
	removed := 0
	for _, r := range rows {
		if want[r.Hostname] {
			continue
		}
		if r.UnwantedSince == nil {
			fresh = append(fresh, r.Hostname)
			continue
		}
		if now.Sub(*r.UnwantedSince) < removeAfter || calls >= callsPerSweep || ctx.Err() != nil {
			continue
		}
		calls++
		if err := s.edge.Remove(ctx, r.Hostname); err != nil {
			log.Warn().Err(err).Str("host", r.Hostname).Msg("edge hostnames: could not remove")
			continue
		}
		if err := s.repo.Delete(ctx, r.Hostname); err == nil {
			removed++
		}
	}
	if err := s.repo.MarkUnwanted(ctx, fresh); err != nil {
		return err
	}
	if registered > 0 || removed > 0 {
		log.Info().Int("checked", registered).Int("removed", removed).Msg("edge hostnames: swept")
	}
	return nil
}

func (s *Service) due(r repository.EdgeHostname, now time.Time) bool {
	if r.UnwantedSince != nil || r.CheckedAt == nil {
		return true
	}
	if r.Status != "active" {
		return now.Sub(*r.CheckedAt) >= pendingRecheck
	}
	return now.Sub(*r.CheckedAt) >= activeRecheck
}

// ensure registers one host and records the verdict; it reports whether the edge answered.
func (s *Service) ensure(ctx context.Context, host string) bool {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	h, err := s.edge.Ensure(cctx, host)
	if err != nil || h == nil {
		log.Warn().Err(err).Str("host", host).Msg("edge hostnames: could not register")
		return false
	}
	row := repository.EdgeHostname{Hostname: host, Status: h.Status, LastError: strings.Join(h.Errors, "; ")}
	if h.Ownership != nil {
		row.OwnershipName, row.OwnershipValue = h.Ownership.Name, h.Ownership.Value
	}
	if err := s.repo.Save(ctx, row); err != nil {
		log.Warn().Err(err).Str("host", host).Msg("edge hostnames: could not record")
		return false
	}
	return true
}

func (s *Service) own(host string) bool {
	if s.skip == nil {
		return false
	}
	for _, h := range s.skip() {
		if h = normalize(h); h != "" && host == h {
			return true
		}
	}
	return false
}

func normalizeAll(hosts []string) []string {
	out := make([]string, 0, len(hosts))
	seen := map[string]bool{}
	for _, h := range hosts {
		if h = normalize(h); h != "" && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

func normalize(v string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(v)), ".")
}

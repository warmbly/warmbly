package integration

import (
	"context"
	"sort"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

// popularityTTL bounds how stale the instance-wide usage counts may be.
const (
	popularityTTL   = 10 * time.Minute
	popularityRetry = 30 * time.Second
)

// curatedOrder breaks ties, so a new instance with no connections still lists
// the integrations most outbound teams reach for first.
var curatedOrder = []models.IntegrationProvider{
	models.IntegrationHubSpot,
	models.IntegrationSlack,
	models.IntegrationApollo,
	models.IntegrationCalendly,
	models.IntegrationClay,
	models.IntegrationCompanyEnrich,
	models.IntegrationZapier,
	models.IntegrationSalesforce,
	models.IntegrationPipedrive,
	models.IntegrationMake,
	models.IntegrationN8N,
	models.IntegrationCalCom,
	models.IntegrationClose,
	models.IntegrationMillionVerifier,
	models.IntegrationDiscord,
	models.IntegrationCleanMyList,
}

// popularity returns workspaces per provider from a cache. A stale cache is
// refreshed by one request at a time outside the lock, and the rest answer from
// the old counts meanwhile; a failed refresh retries after popularityRetry.
func (s *service) popularity(ctx context.Context) map[models.IntegrationProvider]int {
	s.popMu.Lock()
	counts, fresh := s.pop, time.Since(s.popAt) < popularityTTL
	if fresh || s.popRefreshing {
		s.popMu.Unlock()
		return counts
	}
	s.popRefreshing = true
	s.popMu.Unlock()

	next, err := s.repo.WorkspacesByProvider(ctx)

	s.popMu.Lock()
	defer s.popMu.Unlock()
	s.popRefreshing = false
	if err != nil {
		s.popAt = time.Now().Add(popularityRetry - popularityTTL)
		return s.pop
	}
	s.pop, s.popAt = next, time.Now()
	return next
}

// rankByPopularity sets each entry's Rank without reordering the slice.
func rankByPopularity(entries []models.IntegrationCatalogEntry, counts map[models.IntegrationProvider]int) {
	curated := make(map[models.IntegrationProvider]int, len(curatedOrder))
	for i, p := range curatedOrder {
		curated[p] = i
	}
	tie := func(p models.IntegrationProvider) int {
		if i, ok := curated[p]; ok {
			return i
		}
		return len(curatedOrder)
	}
	idx := make([]int, len(entries))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		pa, pb := entries[idx[a]].Provider, entries[idx[b]].Provider
		if counts[pa] != counts[pb] {
			return counts[pa] > counts[pb]
		}
		return tie(pa) < tie(pb)
	})
	for rank, i := range idx {
		entries[i].Rank = rank + 1
	}
}

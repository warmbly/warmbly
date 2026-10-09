package monitoring

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/warmbly/warmbly/internal/models"
)

const (
	CacheTTL             = time.Minute
	RetainLastGood       = 5 * time.Minute
	CollectionTimeout    = 8 * time.Second
	SourceTimeout        = 3 * time.Second
	MaxConcurrentSources = 4
)

type Source struct {
	ID         string
	Permission models.AdminPermission
	Collect    func(context.Context, time.Time) (models.MonitoringSource, error)
}

// Service shares sanitized, immutable snapshots and filters permissions after lookup.
type Service struct {
	sources    []Source
	mu         sync.Mutex
	snapshot   models.MonitoringSnapshot
	refreshing chan struct{}
	now        func() time.Time
}

func New(sources []Source) *Service { return &Service{sources: sources, now: time.Now} }

func (s *Service) Snapshot(ctx context.Context, permissions models.AdminPermission) models.MonitoringSnapshot {
	if s == nil {
		return models.MonitoringSnapshot{Version: "1", Coverage: "unavailable", Sources: []models.MonitoringSource{}}
	}
	s.mu.Lock()
	if !s.snapshot.CheckedAt.IsZero() && s.now().Before(s.snapshot.RefreshAfter) {
		out := s.project(s.snapshot, permissions)
		s.mu.Unlock()
		return out
	}
	if s.refreshing == nil {
		s.refreshing = make(chan struct{})
		go s.refresh(s.refreshing)
	}
	ready := s.refreshing
	s.mu.Unlock()
	select {
	case <-ready:
		s.mu.Lock()
		out := s.project(s.snapshot, permissions)
		s.mu.Unlock()
		return out
	case <-ctx.Done():
		out := models.MonitoringSnapshot{Version: "1", CheckedAt: s.now().UTC(), Coverage: "unavailable", Sources: []models.MonitoringSource{}}
		for _, source := range s.sources {
			out.Sources = append(out.Sources, unavailable(source.ID, out.CheckedAt, "timeout"))
		}
		return s.project(out, permissions)
	}
}

func unavailable(id string, at time.Time, reason string) models.MonitoringSource {
	return models.MonitoringSource{ID: id, CheckedAt: at, Availability: models.MonitoringUnavailable, Coverage: "unavailable", Reason: reason, Metrics: []models.MonitoringMetric{}}
}

func (s *Service) refresh(ready chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), CollectionTimeout)
	defer cancel()
	now := s.now().UTC()
	out := models.MonitoringSnapshot{Version: "1", CheckedAt: now, RefreshAfter: now.Add(CacheTTL), Sources: make([]models.MonitoringSource, len(s.sources))}
	sem := make(chan struct{}, MaxConcurrentSources)
	var wg sync.WaitGroup
	for i, source := range s.sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out.Sources[i] = unavailable(source.ID, now, "dependency_missing")
			defer func() {
				if recover() != nil {
					out.Sources[i] = unavailable(source.ID, now, "collection_failed")
				}
			}()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				out.Sources[i] = unavailable(source.ID, now, "timeout")
				return
			}
			defer func() { <-sem }()
			if source.Collect == nil {
				return
			}
			cctx, stop := context.WithTimeout(ctx, SourceTimeout)
			defer stop()
			result, err := source.Collect(cctx, now)
			if err == nil {
				err = cctx.Err()
			}
			if err != nil {
				out.Sources[i] = unavailable(source.ID, now, ErrorReason(err))
				return
			}
			result.ID, result.CheckedAt = source.ID, now
			if result.Availability == "" {
				result.Availability = models.MonitoringFresh
			}
			if result.Coverage == "" {
				result.Coverage = "complete"
			}
			if result.Availability == models.MonitoringFresh {
				result.ObservedAt = &now
			}
			if result.Metrics == nil {
				result.Metrics = []models.MonitoringMetric{}
			}
			for _, metric := range result.Metrics {
				if metric.Availability != models.MonitoringFresh && result.Coverage == "complete" {
					result.Coverage = "partial"
				}
			}
			out.Sources[i] = result
		}()
	}
	wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range out.Sources {
		cur := &out.Sources[i]
		if cur.Availability != models.MonitoringUnavailable || i >= len(s.snapshot.Sources) {
			continue
		}
		old := s.snapshot.Sources[i]
		if old.ObservedAt == nil || now.Sub(*old.ObservedAt) > RetainLastGood {
			continue
		}
		old.CheckedAt, old.Availability, old.Reason = now, models.MonitoringStale, cur.Reason
		old.Metrics = append([]models.MonitoringMetric(nil), old.Metrics...)
		for j := range old.Metrics {
			old.Metrics[j].Availability = models.MonitoringStale
			old.Metrics[j].Reason = cur.Reason
			age(&old.Metrics[j], now)
		}
		*cur = old
	}
	s.snapshot = out
	s.refreshing = nil
	close(ready)
}

func (s *Service) project(snapshot models.MonitoringSnapshot, permissions models.AdminPermission) models.MonitoringSnapshot {
	snapshot.Sources = append([]models.MonitoringSource(nil), snapshot.Sources...)
	known := 0
	for i, source := range s.sources {
		if !permissions.HasPermission(source.Permission) {
			snapshot.Sources[i] = unavailable(source.ID, snapshot.CheckedAt, "permission_denied")
			continue
		}
		v := &snapshot.Sources[i]
		v.Metrics = append([]models.MonitoringMetric{}, v.Metrics...)
		for j := range v.Metrics {
			age(&v.Metrics[j], s.now())
		}
		if v.Availability == models.MonitoringFresh && v.Coverage == "complete" {
			known++
		}
	}
	snapshot.Coverage = "partial"
	if known == 0 {
		snapshot.Coverage = "unavailable"
	} else if known == len(s.sources) {
		snapshot.Coverage = "complete"
	}
	return snapshot
}

func age(m *models.MonitoringMetric, now time.Time) {
	m.EvidenceAgeSeconds = nil
	if m.EvidenceAt != nil && !m.EvidenceAt.After(now) {
		v := int64(now.Sub(*m.EvidenceAt).Seconds())
		m.EvidenceAgeSeconds = &v
	}
}

func ErrorReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout"
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "42501" {
			return "permission_denied"
		}
		if pgErr.Code == "57014" {
			return "timeout"
		}
	}
	return "query_failed"
}

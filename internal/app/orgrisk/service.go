// Package orgrisk fuses an organization's abuse signals into one posture.
// Every other control watches a single subject, so an actor weak on several
// axes at once stays under all of them. This is the subject that sees them.
package orgrisk

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Signal is one piece of evidence about an organization.
type Signal struct {
	// Key identifies the detector, e.g. "signup_email_risk". Recording the same
	// key again replaces its previous value rather than accumulating duplicates.
	Key string
	// Weight is how many points this contributes, 0-100.
	Weight int
	// Detail is the human sentence an admin reads.
	Detail string
}

// Band thresholds, deliberately far apart: nothing is taken away until several
// detectors agree.
const (
	WatchScore      = 25
	RestrictedScore = 50
	SuspendedScore  = 85
)

// BandFor maps a fused score to its posture.
func BandFor(score int) models.OrgRiskState {
	switch {
	case score >= SuspendedScore:
		return models.OrgRiskSuspended
	case score >= RestrictedScore:
		return models.OrgRiskRestricted
	case score >= WatchScore:
		return models.OrgRiskWatch
	default:
		return models.OrgRiskTrusted
	}
}

// Service is the risk posture of organizations.
type Service interface {
	// Get returns the organization's record. A row that has never been
	// evaluated reads as trusted rather than as an error.
	Get(ctx context.Context, orgID uuid.UUID) (*models.OrgRisk, *errx.Error)
	// RecordSignal adds or replaces one detector's finding and re-derives the
	// band. Returns the record after the change.
	RecordSignal(ctx context.Context, orgID uuid.UUID, sig Signal) (*models.OrgRisk, *errx.Error)
	// ClearSignal removes a detector's finding, for when it no longer holds.
	ClearSignal(ctx context.Context, orgID uuid.UUID, key string) (*models.OrgRisk, *errx.Error)
	// SetState is an operator's manual override, which outranks the score.
	SetState(ctx context.Context, orgID uuid.UUID, state models.OrgRiskState, reason string) (*models.OrgRisk, *errx.Error)
}

type service struct {
	repo  repository.OrgRiskRepository
	audit AuditLogger
}

// AuditLogger is the narrow slice of the audit service a transition needs. It
// is declared here so this package does not depend on the audit package.
type AuditLogger interface {
	LogAction(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction,
		entityType models.AuditEntityType, entityID *uuid.UUID, ipAddress, userAgent string,
		changes, metadata map[string]string)
}

func NewService(repo repository.OrgRiskRepository) Service {
	return &service{repo: repo}
}

// WireAudit attaches the audit logger. A transition rides the audit spine, so
// every teammate's dashboard reflects it without a bespoke emit site.
func (s *service) WireAudit(a AuditLogger) { s.audit = a }

// AuditAware is the optional capability the caller uses to attach the logger.
type AuditAware interface {
	WireAudit(a AuditLogger)
}

// auditTransition records a band change. Only a CHANGE is logged: a detector
// re-recording the same finding must not fill the feed with no-ops.
func (s *service) auditTransition(ctx context.Context, orgID uuid.UUID, before, after *models.OrgRisk, actor uuid.UUID) {
	if s.audit == nil || after == nil {
		return
	}
	if before != nil && before.State == after.State {
		return
	}
	from := string(models.OrgRiskTrusted)
	if before != nil {
		from = string(before.State)
	}
	s.audit.LogAction(ctx, orgID, actor, models.AuditActionUpdate, models.AuditEntityOrgRisk, &orgID, "", "",
		map[string]string{"risk_state": from + " -> " + string(after.State)},
		map[string]string{"reason": after.Reason, "score": strconv.Itoa(after.Score)})
}

func (s *service) Get(ctx context.Context, orgID uuid.UUID) (*models.OrgRisk, *errx.Error) {
	risk, err := s.repo.GetOrgRisk(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	return risk, nil
}

func (s *service) RecordSignal(ctx context.Context, orgID uuid.UUID, sig Signal) (*models.OrgRisk, *errx.Error) {
	if sig.Key == "" {
		return nil, errx.New(errx.BadRequest, "a signal needs a key")
	}
	if sig.Weight < 0 {
		sig.Weight = 0
	}
	if sig.Weight > 100 {
		sig.Weight = 100
	}
	return s.apply(ctx, orgID, func(signals map[string]any) map[string]any {
		signals[sig.Key] = map[string]any{"weight": sig.Weight, "detail": sig.Detail}
		return signals
	})
}

func (s *service) ClearSignal(ctx context.Context, orgID uuid.UUID, key string) (*models.OrgRisk, *errx.Error) {
	return s.apply(ctx, orgID, func(signals map[string]any) map[string]any {
		delete(signals, key)
		return signals
	})
}

func (s *service) SetState(ctx context.Context, orgID uuid.UUID, state models.OrgRiskState, reason string) (*models.OrgRisk, *errx.Error) {
	if !state.Valid() {
		return nil, errx.New(errx.BadRequest, "unknown risk state")
	}
	before, _ := s.repo.GetOrgRisk(ctx, orgID)
	risk, err := s.repo.SetOrgRiskState(ctx, orgID, state, reason)
	if err != nil {
		return nil, errx.InternalError()
	}
	s.auditTransition(ctx, orgID, before, risk, uuid.Nil)
	return risk, nil
}

// apply mutates the signal set and re-derives score, band and reason from it,
// inside the repository's transaction so two detectors firing at once cannot
// each write a band derived from a stale signal set.
func (s *service) apply(ctx context.Context, orgID uuid.UUID, mutate func(map[string]any) map[string]any) (*models.OrgRisk, *errx.Error) {
	before, _ := s.repo.GetOrgRisk(ctx, orgID)
	risk, err := s.repo.UpdateOrgRiskSignals(ctx, orgID, func(signals map[string]any) (map[string]any, models.OrgRiskState, int, string) {
		signals = mutate(signals)
		score := Score(signals)
		return signals, BandFor(score), score, Reason(signals)
	})
	if err != nil {
		return nil, errx.InternalError()
	}
	s.auditTransition(ctx, orgID, before, risk, uuid.Nil)
	return risk, nil
}

// Score sums the recorded weights, capped at 100.
func Score(signals map[string]any) int {
	total := 0
	for _, raw := range signals {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		total += weightOf(entry["weight"])
	}
	if total > 100 {
		return 100
	}
	return total
}

// Reason is the heaviest signals, in a sentence, so an operator sees WHY
// without opening the evidence blob.
func Reason(signals map[string]any) string {
	type entry struct {
		detail string
		weight int
	}
	entries := make([]entry, 0, len(signals))
	for key, raw := range signals {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		detail, _ := m["detail"].(string)
		if detail == "" {
			detail = key
		}
		entries = append(entries, entry{detail: detail, weight: weightOf(m["weight"])})
	}
	if len(entries) == 0 {
		return ""
	}
	// Heaviest first, then by text so the sentence is stable across runs
	// rather than reordering with Go's map iteration.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].weight != entries[j].weight {
			return entries[i].weight > entries[j].weight
		}
		return entries[i].detail < entries[j].detail
	})
	if len(entries) > 3 {
		entries = entries[:3]
	}
	out := ""
	for i, e := range entries {
		if i > 0 {
			out += "; "
		}
		out += e.detail
	}
	return out
}

// weightOf reads a weight back out of jsonb, where a Go int round-trips as a
// float64.
func weightOf(raw any) int {
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case fmt.Stringer:
		return 0
	default:
		return 0
	}
}

// Package orgrisk fuses an organization's abuse signals into one posture.
// Every other control watches a single subject, so an actor weak on several
// axes at once stays under all of them. This is the subject that sees them.
package orgrisk

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Signal is one piece of evidence about an organization.
type Signal struct {
	// Key identifies the detector. Re-recording a key replaces its value,
	// which also restarts its TTL: the detector saw it again.
	Key string
	// Weight is how many points this contributes, 0-100.
	Weight int
	// Detail is the human sentence an admin reads.
	Detail string
	// Class is what kind of evidence this is. An empty class falls back to the
	// key, so a detector that forgets it cannot accidentally restrict anyone.
	Class SignalClass
	// Evidence is structured facts a detector reads back on its next run, such
	// as the counts a running assessment accumulates. Never scored.
	Evidence map[string]any
	// TTL is how long the finding stands on its own. A detector that cannot
	// retract its own finding (a signup's origin, a run of odd sign-ins) MUST
	// set one, or its weight is permanent and a workspace can never recover
	// without an operator. Zero means it stands until something retracts it,
	// which is right for a sweep and for a running assessment that withdraws
	// its own finding when the evidence stops holding.
	TTL time.Duration
}

// DefaultSignalTTL is how long a one-shot finding stands. Long enough that a
// month of bad behaviour still fuses into a band, short enough that a
// workspace which has since behaved is not held by evidence about its first
// day forever.
const DefaultSignalTTL = 30 * 24 * time.Hour

// ExpirySweepInterval is how often expired evidence is swept out. The stored
// score only falls when the row is re-derived, so without this a signal would
// keep its weight past its own expiry until some other detector happened to
// write.
const ExpirySweepInterval = 6 * time.Hour

// SignalClass separates how a workspace looks from what it did. An agency and
// a ring look alike, so only the second kind may cost a customer anything.
type SignalClass string

const (
	// ClassCircumstantial is shape: a shared address, a shared operator
	// identity, a burst of mailboxes. All have ordinary explanations, so shape
	// accumulates as evidence and restricts nothing.
	ClassCircumstantial SignalClass = "circumstantial"
	// ClassSubstantive is what an ordinary customer does not do: a throwaway
	// signup domain, a dead list, complaints from recipients.
	ClassSubstantive SignalClass = "substantive"
)

// Band thresholds, deliberately far apart: nothing is taken away until several
// detectors agree.
const (
	WatchScore      = 25
	RestrictedScore = 50
	SuspendedScore  = 85

	// CircumstantialCap bounds what shape contributes in total, so several
	// benign-explainable findings cannot add up to a verdict.
	CircumstantialCap = 40
	// SubstantiveFloor is the substantive evidence a band needs before it takes
	// anything away. Below it the posture tops out at watch.
	SubstantiveFloor = 25
)

// substantiveKeys classifies findings whose class did not travel with them.
// Anything not listed reads as circumstantial.
var substantiveKeys = map[string]bool{
	// The aggregate written before the disposable finding was split out.
	"signup":            true,
	"signup_disposable": true,
	"list_quality":      true,
	"recipient_harm":    true,
}

// families are detectors that describe one fact from two angles: one operator
// opening several workspaces is found by address and by identity. A family
// contributes its heaviest member once per class rather than twice.
var families = map[string]string{
	"cluster_signup_ip":       "signup_cluster",
	"cluster_signup_identity": "signup_cluster",
}

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
	// actor is the operator who asked, or uuid.Nil for a sweep.
	ClearSignal(ctx context.Context, orgID uuid.UUID, key string) (*models.OrgRisk, *errx.Error)
	// OrgsWithSignal lists organizations carrying a detector's finding, so a
	// recurring sweep can retract the ones that no longer match.
	OrgsWithSignal(ctx context.Context, key string) ([]uuid.UUID, *errx.Error)
	// SetOverride pins the posture to an operator's decision. It outranks the
	// score and survives every later detector write, until ClearOverride.
	// actor is the operator, recorded on the record but never in the
	// organization's own audit feed. See auditTransition.
	SetOverride(ctx context.Context, orgID uuid.UUID, state models.OrgRiskState, reason string, actor uuid.UUID) (*models.OrgRisk, *errx.Error)
	// ClearOverride removes the pin and hands the posture back to the score.
	ClearOverride(ctx context.Context, orgID uuid.UUID) (*models.OrgRisk, *errx.Error)
	// SweepExpired re-derives every organization holding evidence that has
	// aged out, and returns how many it touched.
	SweepExpired(ctx context.Context) int
}

type service struct {
	repo      repository.OrgRiskRepository
	audit     AuditLogger
	opsNotify OperatorNotifier
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

// OperatorNotifier is the instance-wide operator alert surface, injected
// post-construction so this package needs no import of it. Nil disables it.
type OperatorNotifier interface {
	NotifyOperator(key, title, summary string, fields map[string]string)
}

// WireOperatorNotifier attaches the operator alert channel.
func (s *service) WireOperatorNotifier(n OperatorNotifier) { s.opsNotify = n }

// AuditAware is the optional capability the caller uses to attach the logger.
type AuditAware interface {
	WireAudit(a AuditLogger)
}

// auditTransition records a band change on the ORGANIZATION's feed. Only a
// CHANGE is logged: a detector re-recording the same finding must not fill the
// feed with no-ops.
//
// The actor is always the platform, never the operator who decided. That feed
// resolves an actor to a name and an email for the workspace's own members, so
// naming the operator would hand a tenant the identity of the person who
// reviewed them. Who acted is recorded where it belongs: the platform admin
// trail, and risk_override_by on the record itself.
func (s *service) auditTransition(ctx context.Context, orgID uuid.UUID, before, after *models.OrgRisk) {
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
	// The same "only a real change" guard already applied above, so this
	// cannot re-fire while a detector keeps recording the same finding.
	if s.opsNotify != nil {
		s.opsNotify.NotifyOperator(
			"org_risk.escalated",
			"Workspace risk changed to "+string(after.State),
			"Risk scoring moved a workspace into a different posture.",
			map[string]string{
				"Workspace": orgID.String(),
				"From":      from,
				"To":        string(after.State),
				"Score":     strconv.Itoa(after.Score),
				"Reason":    after.Reason,
			},
		)
	}
	s.audit.LogAction(ctx, orgID, uuid.Nil, models.AuditActionUpdate, models.AuditEntityOrgRisk, &orgID, "", "",
		map[string]string{"risk_state": from + " -> " + string(after.State)},
		map[string]string{"reason": after.Reason, "score": strconv.Itoa(after.Score)})
}

func (s *service) Get(ctx context.Context, orgID uuid.UUID) (*models.OrgRisk, *errx.Error) {
	risk, err := s.repo.GetOrgRisk(ctx, orgID)
	if xerr := resolved(risk, err); xerr != nil {
		return nil, xerr
	}
	return risk, nil
}

// resolved turns a repository answer into the error the caller should give. A
// missing row is a missing organization, not an internal failure: an operator
// working from a stale id deserves to be told which it is.
func resolved(risk *models.OrgRisk, err error) *errx.Error {
	if err != nil {
		return errx.InternalError()
	}
	if risk == nil {
		return errx.New(errx.NotFound, "no such organization")
	}
	return nil
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
	class := sig.Class
	if class != ClassCircumstantial && class != ClassSubstantive {
		class = classOf(sig.Key, nil)
	}
	return s.apply(ctx, orgID, func(signals map[string]any) map[string]any {
		entry := map[string]any{
			"weight": sig.Weight, "detail": sig.Detail, "class": string(class),
		}
		if len(sig.Evidence) > 0 {
			entry["evidence"] = sig.Evidence
		}
		if sig.TTL > 0 {
			entry["expires_at"] = time.Now().Add(sig.TTL).UTC().Format(time.RFC3339)
		}
		signals[sig.Key] = entry
		return signals
	})
}

func (s *service) ClearSignal(ctx context.Context, orgID uuid.UUID, key string) (*models.OrgRisk, *errx.Error) {
	return s.apply(ctx, orgID, func(signals map[string]any) map[string]any {
		delete(signals, key)
		return signals
	})
}

func (s *service) OrgsWithSignal(ctx context.Context, key string) ([]uuid.UUID, *errx.Error) {
	ids, err := s.repo.OrgsWithSignal(ctx, key)
	if err != nil {
		return nil, errx.InternalError()
	}
	return ids, nil
}

func (s *service) SetOverride(ctx context.Context, orgID uuid.UUID, state models.OrgRiskState, reason string, actor uuid.UUID) (*models.OrgRisk, *errx.Error) {
	if !state.Valid() {
		return nil, errx.New(errx.BadRequest, "unknown risk state")
	}
	before, _ := s.repo.GetOrgRisk(ctx, orgID)
	risk, err := s.repo.SetOrgRiskOverride(ctx, orgID, state, reason, actor)
	if xerr := resolved(risk, err); xerr != nil {
		return nil, xerr
	}
	s.auditTransition(ctx, orgID, before, risk)
	return risk, nil
}

func (s *service) ClearOverride(ctx context.Context, orgID uuid.UUID) (*models.OrgRisk, *errx.Error) {
	before, _ := s.repo.GetOrgRisk(ctx, orgID)
	risk, err := s.repo.ClearOrgRiskOverride(ctx, orgID, s.derive)
	if xerr := resolved(risk, err); xerr != nil {
		return nil, xerr
	}
	s.auditTransition(ctx, orgID, before, risk)
	return risk, nil
}

// SweepExpired ages evidence out. Only a re-derive moves the stored score, so
// a finding past its expiry keeps its weight until this runs.
func (s *service) SweepExpired(ctx context.Context) int {
	orgs, err := s.repo.OrgsWithExpiringSignals(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("risk expiry sweep: could not list organizations holding dated evidence")
		return 0
	}
	swept := 0
	for _, orgID := range orgs {
		retired := false
		if _, aerr := s.apply(ctx, orgID, func(signals map[string]any) map[string]any {
			held := len(signals)
			signals = PruneExpired(signals, time.Now())
			retired = len(signals) != held
			return signals
		}); aerr != nil {
			log.Warn().Str("organization_id", orgID.String()).Msg("risk expiry sweep: could not re-derive a posture")
			continue
		}
		if retired {
			swept++
		}
	}
	if swept > 0 {
		log.Info().Int("organizations", swept).Msg("risk expiry sweep retired evidence that aged out")
	}
	return swept
}

// StartExpirySweep runs SweepExpired on an interval until the context ends.
func StartExpirySweep(ctx context.Context, svc Service, interval time.Duration) {
	if svc == nil || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			svc.SweepExpired(ctx)
		}
	}
}

// apply mutates the signal set and re-derives score, band and reason from it,
// inside the repository's transaction so two detectors firing at once cannot
// each write a band derived from a stale signal set.
func (s *service) apply(ctx context.Context, orgID uuid.UUID, mutate func(map[string]any) map[string]any) (*models.OrgRisk, *errx.Error) {
	before, _ := s.repo.GetOrgRisk(ctx, orgID)
	risk, err := s.repo.UpdateOrgRiskSignals(ctx, orgID, func(signals map[string]any) (map[string]any, models.OrgRiskState, int, string) {
		return s.derive(mutate(signals))
	})
	if xerr := resolved(risk, err); xerr != nil {
		return nil, xerr
	}
	s.auditTransition(ctx, orgID, before, risk)
	return risk, nil
}

// derive is the one place a stored posture is computed: evidence that has aged
// out is dropped first, so a score can fall on its own rather than only when a
// detector happens to retract.
func (s *service) derive(signals map[string]any) (map[string]any, models.OrgRiskState, int, string) {
	signals = PruneExpired(signals, time.Now())
	state, score := Evaluate(signals)
	return signals, state, score, Reason(signals)
}

// PruneExpired drops every finding whose expires_at has passed. An entry with
// an unreadable expiry is left alone: dated evidence that cannot be read is
// evidence, and silently discarding it would let a malformed write clear a
// workspace's record.
func PruneExpired(signals map[string]any, now time.Time) map[string]any {
	for key, raw := range signals {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		text, ok := entry["expires_at"].(string)
		if !ok || text == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339, text)
		if err != nil {
			continue
		}
		if now.After(at) {
			delete(signals, key)
		}
	}
	return signals
}

// Evaluate fuses the evidence into the band and score to store. Shape raises
// the score; only substantive evidence lets a band take something away.
func Evaluate(signals map[string]any) (models.OrgRiskState, int) {
	circumstantial, substantive := fuse(signals)
	if circumstantial > CircumstantialCap {
		circumstantial = CircumstantialCap
	}
	score := circumstantial + substantive
	if score > 100 {
		score = 100
	}
	band := BandFor(score)
	if substantive < SubstantiveFloor {
		band = atMost(band, models.OrgRiskWatch)
	}
	return band, score
}

// Score is the fused weight behind the band: one weight per family, shape
// capped, total capped at 100.
func Score(signals map[string]any) int {
	_, score := Evaluate(signals)
	return score
}

// fuse reduces the evidence to one weight per family and totals each class.
// Heaviest per class, so shape can never hide conduct filed in the same family.
func fuse(signals map[string]any) (circumstantial, substantive int) {
	type member struct {
		family string
		class  SignalClass
	}
	heaviest := make(map[member]int, len(signals))
	for key, raw := range signals {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		m := member{family: key, class: classOf(key, entry["class"])}
		if f, named := families[key]; named {
			m.family = f
		}
		if weight := weightOf(entry["weight"]); weight > heaviest[m] {
			heaviest[m] = weight
		}
	}
	for m, weight := range heaviest {
		if m.class == ClassSubstantive {
			substantive += weight
			continue
		}
		circumstantial += weight
	}
	return circumstantial, substantive
}

// classOf reads a finding's class, falling back to the key for rows written
// before the class travelled with the weight.
func classOf(key string, raw any) SignalClass {
	if s, ok := raw.(string); ok {
		switch SignalClass(s) {
		case ClassSubstantive:
			return ClassSubstantive
		case ClassCircumstantial:
			return ClassCircumstantial
		}
	}
	if substantiveKeys[key] {
		return ClassSubstantive
	}
	return ClassCircumstantial
}

// atMost lowers a band to a ceiling, so a rule can hold a verdict back without
// knowing which band the score produced.
func atMost(band, ceiling models.OrgRiskState) models.OrgRiskState {
	if bandRank(band) > bandRank(ceiling) {
		return ceiling
	}
	return band
}

func bandRank(state models.OrgRiskState) int {
	switch state {
	case models.OrgRiskWatch:
		return 1
	case models.OrgRiskRestricted:
		return 2
	case models.OrgRiskSuspended:
		return 3
	default:
		return 0
	}
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

// HasSignal reports whether a detector's finding is currently recorded.
func HasSignal(risk *models.OrgRisk, key string) bool {
	if risk == nil {
		return false
	}
	_, ok := risk.Signals[key].(map[string]any)
	return ok
}

// EvidenceInt reads one number a detector stored beside its finding. Zero when
// the finding, the evidence or the field is absent.
func EvidenceInt(risk *models.OrgRisk, key, field string) int {
	if risk == nil {
		return 0
	}
	entry, ok := risk.Signals[key].(map[string]any)
	if !ok {
		return 0
	}
	evidence, ok := entry["evidence"].(map[string]any)
	if !ok {
		return 0
	}
	return weightOf(evidence[field])
}

// weightOf reads a number back out of jsonb, where a Go int round-trips as a
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

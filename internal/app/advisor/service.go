package advisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// AuditLogger fires the audit entry that carries this change onto every
// teammate's screen. The advisor writes through the same spine as everything
// else rather than inventing its own realtime event.
type AuditLogger interface {
	LogAction(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, ip, userAgent string, changes, metadata map[string]string)
}

// ToolRunner is the slice of the AI tool registry the advisor uses to apply a
// fix. Going through the registry (rather than calling services directly) is
// what makes "the Advisor can never do more than you could by hand" true by
// construction: the registry enforces the caller's own permission bits and
// audits the write.
type ToolRunner interface {
	Call(ctx context.Context, inv aitools.Invocation, name string, args json.RawMessage) (string, error)
}

// MemberResolver returns a member's permission mask. Autopilot needs it so it
// can act as the person who switched it on rather than as an unbounded system
// actor: same permission gates, same audit attribution, and it stops working
// the moment that member leaves the organization.
type MemberResolver interface {
	MemberPermissions(ctx context.Context, orgID, userID uuid.UUID) (models.OrganizationPermission, error)
}

// Service is the advisor application API.
type Service interface {
	// Evaluate runs every detector for one org, reconciles the results against
	// what is already stored, and narrates whatever is new.
	Evaluate(ctx context.Context, orgID uuid.UUID, trigger string) (*models.AdvisorSummary, error)

	List(ctx context.Context, orgID uuid.UUID, f repository.AdvisorFindingFilter) ([]models.AdvisorFinding, *errx.Error)
	Summary(ctx context.Context, orgID uuid.UUID) (*models.AdvisorSummary, *errx.Error)
	Get(ctx context.Context, orgID, id uuid.UUID) (*models.AdvisorFinding, *errx.Error)

	// Apply executes the finding's one-click fix as the calling user. The
	// caller is expected to have shown them the action's rendered preview
	// first; this is the confirm step, not the whole interaction.
	Apply(ctx context.Context, inv aitools.Invocation, id uuid.UUID) (*models.AdvisorFinding, *errx.Error)
	// Undo reverts a previously applied fix.
	Undo(ctx context.Context, inv aitools.Invocation, id uuid.UUID) (*models.AdvisorFinding, *errx.Error)

	// FixWithAgent resolves a finding that has no deterministic action by
	// running a bounded agent against it, as the calling member.
	FixWithAgent(ctx context.Context, inv aitools.Invocation, id uuid.UUID) (*models.AdvisorAgentResult, *errx.Error)

	Snooze(ctx context.Context, orgID, userID, id uuid.UUID, days int) *errx.Error
	Dismiss(ctx context.Context, orgID, userID, id uuid.UUID, reason string) *errx.Error
	Feedback(ctx context.Context, orgID, userID, id uuid.UUID, helpful bool, reason string) *errx.Error

	GetSettings(ctx context.Context, orgID uuid.UUID) (*models.AdvisorSettings, *errx.Error)
	// UpdateSettings records userID as the autopilot actor when autopilot is on,
	// so unattended fixes always run with a named member's permissions.
	UpdateSettings(ctx context.Context, orgID, userID uuid.UUID, s *models.AdvisorSettings) *errx.Error
}

type service struct {
	repo     repository.AdvisorRepository
	tools    ToolRunner
	narrator *Narrator
	audit    AuditLogger
	members  MemberResolver

	// The agent-fix path. All four are optional together: without them the
	// advisor still detects, explains, and applies its one-click fixes.
	agent    AgentRunner
	toolList ToolLister
	credits  CreditCharger
	tier     TierSource

	// trackingHost is this install's tracking host, for the CNAME template.
	trackingHost string
	// domainAuth resolves the sending-domain authentication gate, so the
	// domain-auth finding describes what actually happens on this install
	// rather than what happens on a default one. Optional/nil-safe.
	domainAuth DomainAuthPolicy
}

// DomainAuthPolicy resolves whether the sending-domain authentication gate is
// enforced, and how long a domain must stay failing before it applies. Narrow
// and primitive-typed so the advisor does not depend on the settings package;
// instancesettings.Service satisfies it.
type DomainAuthPolicy interface {
	DomainAuth(ctx context.Context) (enforce bool, grace time.Duration)
}

// AgentDeps carries the optional agent-fix wiring.
type AgentDeps struct {
	Agent   AgentRunner
	Tools   ToolLister
	Credits CreditCharger
	Tier    TierSource
}

// NewService wires the advisor. Every dependency past repo is optional: the
// advisor without them loses AI-written copy, the live cross-client refresh,
// autopilot, and the agent fix respectively, but still detects and explains.
func NewService(repo repository.AdvisorRepository, tools ToolRunner, narrator *Narrator, audit AuditLogger, members MemberResolver, agent *AgentDeps, opts ...Option) Service {
	s := &service{repo: repo, tools: tools, narrator: narrator, audit: audit, members: members}
	if agent != nil {
		s.agent, s.toolList, s.credits, s.tier = agent.Agent, agent.Tools, agent.Credits, agent.Tier
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Option configures the service without another positional parameter.
type Option func(*service)

// WithTrackingHost supplies the platform's tracking host so the tracking-domain
// finding can hand over a complete CNAME instead of telling somebody to go and
// look the target up.
func WithTrackingHost(host string) Option {
	return func(s *service) { s.trackingHost = host }
}

// WithDomainAuthPolicy supplies the sending-domain authentication gate so the
// domain-auth finding can say whether sending has already stopped.
func WithDomainAuthPolicy(p DomainAuthPolicy) Option {
	return func(s *service) { s.domainAuth = p }
}

// maxNarrationsPerRun bounds how many completions one evaluation can spend.
// New findings are narrated most-severe-first, so the cap only ever costs the
// least important cards their rewrite, and the next run picks them up.
const maxNarrationsPerRun = 12

func (s *service) Evaluate(ctx context.Context, orgID uuid.UUID, trigger string) (*models.AdvisorSummary, error) {
	settings, err := s.repo.GetSettings(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return s.repo.Summary(ctx, orgID)
	}

	runID, err := s.repo.StartRun(ctx, orgID, trigger)
	if err != nil {
		// A missing run row is an observability loss, not a reason to skip the
		// evaluation the user is waiting on.
		log.Printf("advisor: start run for org %s: %v", orgID, err)
	}

	snapshot, err := s.repo.LoadSnapshot(ctx, orgID, time.Now().UTC())
	if err != nil {
		s.finishRun(ctx, runID, 0, 0, 0, 0, err.Error())
		return nil, err
	}

	snapshot.TrackingHost = s.trackingHost
	if s.domainAuth != nil {
		snapshot.DomainAuthEnforced, snapshot.DomainAuthGrace = s.domainAuth.DomainAuth(ctx)
	}
	findings := Detect(snapshot, settings)

	keep := make([]string, 0, len(findings))
	stored := make([]*models.AdvisorFinding, 0, len(findings))
	newCount := 0
	for _, f := range findings {
		keep = append(keep, f.Fingerprint())
		row, inserted, err := s.repo.UpsertFinding(ctx, f.toModel(orgID))
		if err != nil {
			log.Printf("advisor: upsert %s for org %s: %v", f.Key, orgID, err)
			continue
		}
		if inserted {
			newCount++
		}
		stored = append(stored, row)
	}

	// Anything the detectors did not produce this pass is no longer true.
	closed, err := s.repo.ResolveMissing(ctx, orgID, keep)
	if err != nil {
		log.Printf("advisor: resolve missing for org %s: %v", orgID, err)
	}

	// Autopilot runs before the summary so the score reflects what it fixed,
	// rather than reporting problems that no longer exist.
	fixed := s.autopilot(ctx, orgID, settings, stored)

	narrated := s.narrateBatch(ctx, orgID, stored)

	summary, err := s.repo.Summary(ctx, orgID)
	if err != nil {
		s.finishRun(ctx, runID, newCount, 0, closed, narrated, err.Error())
		return nil, err
	}
	s.finishRun(ctx, runID, newCount, summary.Total, closed, narrated, "")

	// One audit entry per run, only when something actually changed. The spine
	// turns this into a live refresh of every teammate's advisor strips and
	// nav badges without a bespoke realtime event.
	if (newCount > 0 || closed > 0 || fixed > 0) && s.audit != nil {
		s.audit.LogAction(ctx, orgID, uuid.Nil, models.AuditActionUpdate, models.AuditEntityAdvisorFinding, nil, "", "",
			nil, map[string]string{
				"new":       fmt.Sprint(newCount),
				"resolved":  fmt.Sprint(closed),
				"open":      fmt.Sprint(summary.Total),
				"autofixed": fmt.Sprint(fixed),
				"trigger":   trigger,
			})
	}
	return summary, nil
}

// autopilot applies the auto-safe fixes among this run's findings, as the
// member who switched it on. It returns how many landed.
//
// Everything here fails closed. No actor, no resolver, or a member who has left
// the organization means autopilot does nothing at all, because the alternative
// is a background process making sending changes with nobody accountable for
// them. Individual failures are logged and skipped rather than aborting the
// pass: one tool that a member lacks permission for should not stop the rest.
func (s *service) autopilot(ctx context.Context, orgID uuid.UUID, settings *models.AdvisorSettings, findings []*models.AdvisorFinding) int {
	if settings == nil || !settings.Autopilot || settings.AutopilotActorID == nil || s.members == nil {
		return 0
	}
	actor := *settings.AutopilotActorID

	perms, err := s.members.MemberPermissions(ctx, orgID, actor)
	if err != nil {
		log.Printf("advisor: autopilot actor %s is not a member of org %s, skipping: %v", actor, orgID, err)
		return 0
	}

	inv := aitools.Invocation{
		OrgID:    orgID,
		UserID:   actor,
		OrgPerms: perms,
		// A JWT-shaped invocation: autopilot is a member acting, not an API key,
		// so it is gated on org permissions like any dashboard action.
		IsAPIKey:  false,
		UserAgent: "warmbly-advisor-autopilot",
	}

	applied := 0
	for _, f := range findings {
		if applied >= models.AutopilotMaxPerRun {
			log.Printf("advisor: autopilot hit the per-run cap of %d for org %s", models.AutopilotMaxPerRun, orgID)
			break
		}
		if f.Status != models.AdvisorStatusOpen || f.Action == nil || !f.Action.Auto {
			continue
		}
		if _, xerr := s.Apply(ctx, inv, f.ID); xerr != nil {
			log.Printf("advisor: autopilot could not apply %s for org %s: %s", f.DetectorKey, orgID, xerr.Message)
			continue
		}
		applied++
	}
	return applied
}

// narrateBatch rewrites the findings that still carry fallback copy, most
// severe first, and writes the results back. Failures are silent by design:
// the fallback copy is already complete.
func (s *service) narrateBatch(ctx context.Context, orgID uuid.UUID, findings []*models.AdvisorFinding) int {
	if s.narrator == nil || !s.narrator.Enabled() {
		return 0
	}
	// stored comes back in detector order, which is already severity-sorted by
	// Detect, so the cap spends the budget on what matters.
	count := 0
	for _, f := range findings {
		if count >= maxNarrationsPerRun {
			break
		}
		if f.Narrated {
			continue
		}
		if !s.narrator.Narrate(ctx, orgID, f) {
			continue
		}
		if err := s.repo.UpdateNarration(ctx, orgID, f.ID, f.Title, f.Detail, f.Remedy); err != nil {
			log.Printf("advisor: save narration for %s: %v", describe(f), err)
			continue
		}
		count++
	}
	return count
}

func (s *service) finishRun(ctx context.Context, runID uuid.UUID, newCount, open, closed, narrated int, runErr string) {
	if runID == uuid.Nil {
		return
	}
	if err := s.repo.FinishRun(ctx, runID, newCount, open, closed, narrated, runErr); err != nil {
		log.Printf("advisor: finish run %s: %v", runID, err)
	}
}

func (s *service) List(ctx context.Context, orgID uuid.UUID, f repository.AdvisorFindingFilter) ([]models.AdvisorFinding, *errx.Error) {
	out, err := s.repo.ListFindings(ctx, orgID, f)
	if err != nil {
		return nil, errx.InternalError()
	}
	// Stamped on read rather than stored: which checks an agent can resolve is
	// a property of this build, and a stored copy would go stale the moment the
	// allowlist changes.
	for i := range out {
		out[i].AgentFixable = s.agent != nil && CanAgentFix(&out[i])
	}
	return out, nil
}

func (s *service) Summary(ctx context.Context, orgID uuid.UUID) (*models.AdvisorSummary, *errx.Error) {
	out, err := s.repo.Summary(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	return out, nil
}

func (s *service) Get(ctx context.Context, orgID, id uuid.UUID) (*models.AdvisorFinding, *errx.Error) {
	f, err := s.repo.GetFinding(ctx, orgID, id)
	if err != nil {
		return nil, errx.InternalError()
	}
	if f == nil {
		return nil, errx.ErrNotFound
	}
	f.AgentFixable = s.agent != nil && CanAgentFix(f)
	return f, nil
}

// ErrNoAction is returned when a finding has no automated remedy. Most
// findings that need a human judgement call (rewrite this copy, fix this DNS
// record) deliberately have none.
var ErrNoAction = errors.New("this recommendation has no one-click fix")

func (s *service) Apply(ctx context.Context, inv aitools.Invocation, id uuid.UUID) (*models.AdvisorFinding, *errx.Error) {
	f, xerr := s.Get(ctx, inv.OrgID, id)
	if xerr != nil {
		return nil, xerr
	}
	if f.Action == nil || f.Action.Tool == "" {
		return nil, errx.ErrAdvisorNoAction
	}
	// Applying twice is a no-op rather than a second write. That is what makes
	// a retried request safe without an idempotency key: the second call
	// returns the first call's outcome.
	if f.Status == models.AdvisorStatusApplied {
		return f, nil
	}

	result, err := s.runTool(ctx, inv, f.Action.Tool, f.Action.Args)
	if err != nil {
		return nil, toolErr(err)
	}

	if err := s.repo.MarkApplied(ctx, inv.OrgID, id, inv.UserID, result); err != nil {
		// The change landed; failing the request now would invite the user to
		// apply it a second time. Report success and let the next evaluation
		// reconcile the status.
		log.Printf("advisor: mark applied %s: %v", id, err)
	}
	s.auditFinding(ctx, inv, f, "apply")

	f.Status = models.AdvisorStatusApplied
	now := time.Now().UTC()
	f.AppliedAt = &now
	f.AppliedBy = &inv.UserID
	f.AppliedResult = result
	return f, nil
}

func (s *service) Undo(ctx context.Context, inv aitools.Invocation, id uuid.UUID) (*models.AdvisorFinding, *errx.Error) {
	f, xerr := s.Get(ctx, inv.OrgID, id)
	if xerr != nil {
		return nil, xerr
	}
	if f.Action == nil || f.Action.Undo == nil {
		return nil, errx.ErrAdvisorNoAction
	}
	if f.Status != models.AdvisorStatusApplied {
		return nil, errx.ErrAdvisorNotApplied
	}

	if _, err := s.runTool(ctx, inv, f.Action.Undo.Tool, f.Action.Undo.Args); err != nil {
		return nil, toolErr(err)
	}

	// Back to open: the condition the detector fired on is true again, and the
	// next evaluation will confirm it.
	if err := s.repo.SetStatus(ctx, inv.OrgID, id, models.AdvisorStatusOpen); err != nil {
		return nil, errx.InternalError()
	}
	s.auditFinding(ctx, inv, f, "undo")

	f.Status = models.AdvisorStatusOpen
	f.AppliedAt, f.AppliedBy, f.AppliedResult = nil, nil, ""
	return f, nil
}

// runTool executes one registry tool under the caller's identity.
func (s *service) runTool(ctx context.Context, inv aitools.Invocation, name string, args json.RawMessage) (string, error) {
	if s.tools == nil {
		return "", ErrNoAction
	}
	return s.tools.Call(ctx, inv, name, args)
}

// toolErr maps a registry failure onto the right HTTP shape. A permission
// failure here means the member is allowed to see the advice but not to make
// the change, which is a legitimate state worth reporting precisely.
func toolErr(err error) *errx.Error {
	switch {
	case errors.Is(err, aitools.ErrToolForbidden):
		return errx.ErrAdvisorFixForbidden
	case errors.Is(err, aitools.ErrToolNotFound), errors.Is(err, ErrNoAction):
		return errx.ErrAdvisorNoAction
	default:
		log.Printf("advisor: apply failed: %v", err)
		return errx.InternalError()
	}
}

func (s *service) Snooze(ctx context.Context, orgID, userID, id uuid.UUID, days int) *errx.Error {
	if days < 1 || days > 90 {
		return errx.ErrAdvisorSnoozeRange
	}
	f, xerr := s.Get(ctx, orgID, id)
	if xerr != nil {
		return xerr
	}
	until := time.Now().UTC().AddDate(0, 0, days)
	if err := s.repo.Snooze(ctx, orgID, id, until); err != nil {
		return errx.InternalError()
	}
	s.auditFinding(ctx, aitools.Invocation{OrgID: orgID, UserID: userID}, f, "snooze")
	return nil
}

func (s *service) Dismiss(ctx context.Context, orgID, userID, id uuid.UUID, reason string) *errx.Error {
	f, xerr := s.Get(ctx, orgID, id)
	if xerr != nil {
		return xerr
	}
	if err := s.repo.Dismiss(ctx, orgID, id, userID, reason); err != nil {
		return errx.InternalError()
	}
	s.auditFinding(ctx, aitools.Invocation{OrgID: orgID, UserID: userID}, f, "dismiss")
	return nil
}

func (s *service) Feedback(ctx context.Context, orgID, userID, id uuid.UUID, helpful bool, reason string) *errx.Error {
	f, xerr := s.Get(ctx, orgID, id)
	if xerr != nil {
		return xerr
	}
	uid := &userID
	if userID == uuid.Nil {
		uid = nil
	}
	if err := s.repo.RecordFeedback(ctx, orgID, id, uid, f.DetectorKey, helpful, reason); err != nil {
		return errx.InternalError()
	}
	return nil
}

func (s *service) GetSettings(ctx context.Context, orgID uuid.UUID) (*models.AdvisorSettings, *errx.Error) {
	out, err := s.repo.GetSettings(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	return out, nil
}

func (s *service) UpdateSettings(ctx context.Context, orgID, userID uuid.UUID, in *models.AdvisorSettings) *errx.Error {
	if in == nil {
		return errx.ErrInvalid
	}
	if in.MinSeverity == "" {
		in.MinSeverity = models.AdvisorLow
	}
	if in.MinSeverity.Rank() == 0 {
		return errx.ErrAdvisorBadSeverity
	}
	if in.MutedCategories == nil {
		in.MutedCategories = []string{}
	}
	if in.MutedDetectors == nil {
		in.MutedDetectors = []string{}
	}
	// Autopilot always acts as whoever last switched it on, and the client never
	// gets to nominate somebody else: that would be a way to borrow a
	// colleague's permissions for unattended writes.
	in.AutopilotActorID = nil
	if in.Autopilot && userID != uuid.Nil {
		actor := userID
		in.AutopilotActorID = &actor
	}

	in.OrganizationID = orgID
	if err := s.repo.UpdateSettings(ctx, in); err != nil {
		return errx.InternalError()
	}
	return nil
}

// auditFinding records a member's action on a recommendation, which both keeps
// the trail complete and pushes the change to every open dashboard.
func (s *service) auditFinding(ctx context.Context, inv aitools.Invocation, f *models.AdvisorFinding, action string) {
	if s.audit == nil {
		return
	}
	s.audit.LogAction(ctx, inv.OrgID, inv.UserID, models.AuditActionUpdate,
		models.AuditEntityAdvisorFinding, &f.ID, inv.IP, inv.UserAgent, nil,
		map[string]string{
			"advisor_action": action,
			"detector":       f.DetectorKey,
			"severity":       string(f.Severity),
			"surface":        string(f.Surface),
			"entity":         f.EntityLabel,
		})
}

// Package research runs the AI contact-research agent: it drives the M2 web
// tools (search_web, fetch_url) through the M1 provider loop, validates the
// strict save_research output, charges 2 credits per run on save, and persists a
// cited findings blob. Sync runs execute in the request; batch runs queue and
// drain through a bounded in-process worker pool (no new Kafka).
package research

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// MaxBatch caps a single batch request.
	MaxBatch = 500
	// poolConcurrency is the bounded number of runs processed at once.
	poolConcurrency = 4
	// defaultSearchBudget / defaultFetchBudget bound one run's tool spend.
	defaultSearchBudget = 5
	defaultFetchBudget  = 6
	// maxIterations bounds the agent loop (searches + fetches + save + slack).
	maxIterations = 16
)

// FeatureGate is the slice of the feature service used to route the model tier.
type FeatureGate interface {
	IsPaidOrganization(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error)
}

// SkillPreamble renders the org's enabled playbooks for the research prompt.
type SkillPreamble interface {
	EnabledPreamble(ctx context.Context, orgID uuid.UUID) string
}

// Service is the contact-research application API.
type Service interface {
	// RunResearch executes one research run synchronously and returns the
	// terminal run. idempotencyKey (optional) makes a retried POST return the
	// prior run instead of running + charging again.
	RunResearch(ctx context.Context, inv aitools.Invocation, contactID uuid.UUID, objective, idempotencyKey string) (*models.ContactResearchRun, *errx.Error)
	// ListRuns returns a contact's research history, newest first.
	ListRuns(ctx context.Context, orgID, contactID uuid.UUID, limit int) ([]models.ContactResearchRun, *errx.Error)
	// Batch queues research for up to MaxBatch contacts and wakes the drain
	// pool. Returns the number queued.
	Batch(ctx context.Context, inv aitools.Invocation, contactIDs []uuid.UUID, objective string) (int, *errx.Error)
	// StartDrainPool launches the background workers that drain queued runs.
	StartDrainPool(ctx context.Context)
}

type service struct {
	repo      repository.ResearchRepository
	registry  *aitools.Registry
	provider  generation.Provider
	credits   credits.CreditService
	feature   FeatureGate
	contacts  contact.ContactService
	orgs      organization.OrganizationService
	publisher *pubsub.StreamingPublisher
	skills    SkillPreamble
	wake      chan struct{}
}

func NewService(repo repository.ResearchRepository, registry *aitools.Registry, provider generation.Provider, creditSvc credits.CreditService, feature FeatureGate, contacts contact.ContactService, orgs organization.OrganizationService, publisher *pubsub.StreamingPublisher, skills SkillPreamble) Service {
	return &service{
		repo: repo, registry: registry, provider: provider, credits: creditSvc,
		feature: feature, contacts: contacts, orgs: orgs, publisher: publisher, skills: skills,
		wake: make(chan struct{}, 1),
	}
}

func (s *service) ListRuns(ctx context.Context, orgID, contactID uuid.UUID, limit int) ([]models.ContactResearchRun, *errx.Error) {
	runs, err := s.repo.ListByContact(ctx, orgID, contactID, limit)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to list research runs")
	}
	return runs, nil
}

func (s *service) RunResearch(ctx context.Context, inv aitools.Invocation, contactID uuid.UUID, objective, idempotencyKey string) (*models.ContactResearchRun, *errx.Error) {
	if s.provider == nil {
		return nil, errx.New(errx.ServiceUnavailable, "AI research is not configured")
	}
	// Pre-check balance AND the abuse caps so an out-of-credits or rate-capped
	// org never does free research work (the charge happens on save, after the
	// expensive agent loop).
	if bal, xerr := s.credits.GetBalance(ctx, inv.OrgID); xerr != nil {
		return nil, xerr
	} else if bal < credits.CostResearchRun {
		return nil, errx.New(errx.PaymentRequired, "insufficient credits")
	}
	if err := s.credits.CheckUsageCaps(ctx, inv.OrgID); err != nil {
		return nil, errx.New(errx.TooManyRequests, "AI usage limit reached, please try again later.")
	}

	run := &models.ContactResearchRun{
		OrgID: inv.OrgID, ContactID: contactID, RequestedBy: &inv.UserID,
		Status: models.ResearchRunning, Objective: strings.TrimSpace(objective),
	}
	created, replayed, err := s.repo.CreateRun(ctx, run, idempotencyKey)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to create research run")
	}
	if replayed {
		return created, nil
	}
	return s.execute(ctx, inv, created), nil
}

func (s *service) Batch(ctx context.Context, inv aitools.Invocation, contactIDs []uuid.UUID, objective string) (int, *errx.Error) {
	if len(contactIDs) == 0 {
		return 0, errx.New(errx.BadRequest, "no contacts provided")
	}
	if len(contactIDs) > MaxBatch {
		return 0, errx.New(errx.BadRequest, fmt.Sprintf("a batch is limited to %d contacts", MaxBatch))
	}
	// Pre-check: at least one run must be fundable.
	if bal, xerr := s.credits.GetBalance(ctx, inv.OrgID); xerr != nil {
		return 0, xerr
	} else if bal < credits.CostResearchRun {
		return 0, errx.New(errx.PaymentRequired, "insufficient credits")
	}

	queued := 0
	for _, cid := range contactIDs {
		run := &models.ContactResearchRun{
			OrgID: inv.OrgID, ContactID: cid, RequestedBy: &inv.UserID,
			Status: models.ResearchQueued, Objective: strings.TrimSpace(objective),
		}
		if _, _, err := s.repo.CreateRun(ctx, run, ""); err == nil {
			queued++
		}
	}
	s.signalWake()
	return queued, nil
}

func (s *service) signalWake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *service) StartDrainPool(ctx context.Context) {
	for i := 0; i < poolConcurrency; i++ {
		go s.drainWorker(ctx)
	}
}

func (s *service) drainWorker(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		for {
			run, err := s.repo.ClaimNextQueued(ctx)
			if err != nil || run == nil {
				break
			}
			inv := aitools.Invocation{OrgID: run.OrgID}
			if run.RequestedBy != nil {
				inv.UserID = *run.RequestedBy
			}
			s.execute(ctx, inv, run)
		}
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		}
	}
}

// execute runs one claimed/created run to a terminal state, charging on save.
func (s *service) execute(ctx context.Context, inv aitools.Invocation, run *models.ContactResearchRun) *models.ContactResearchRun {
	finish := func() *models.ContactResearchRun {
		_ = s.repo.UpdateRun(ctx, run)
		s.publisher.PublishAIResearchProgress(ctx, run.OrgID, inv.UserID, run.ContactID.String(), run.ID.String(), string(run.Status))
		return run
	}

	// Per-run balance + cap pre-check (batch runs are only pre-checked in
	// aggregate). This gates the expensive agent loop so a cap trip at charge
	// time can never discard a completed run's work.
	if bal, xerr := s.credits.GetBalance(ctx, run.OrgID); xerr != nil || bal < credits.CostResearchRun {
		run.Status = models.ResearchFailed
		run.Error = "insufficient credits"
		return finish()
	}
	if err := s.credits.CheckUsageCaps(ctx, run.OrgID); err != nil {
		run.Status = models.ResearchFailed
		run.Error = "AI usage limit reached, please try again later"
		return finish()
	}

	orgID := run.OrgID
	detail, cxerr := s.contacts.GetDetail(ctx, inv.UserID, &orgID, run.ContactID)
	if cxerr != nil || detail == nil {
		run.Status = models.ResearchFailed
		run.Error = "contact not found"
		return finish()
	}

	pd := PromptData{
		FirstName:    detail.FirstName,
		LastName:     detail.LastName,
		Email:        detail.Email,
		Company:      detail.Company,
		Title:        detail.CustomFields["title"],
		CustomFields: detail.CustomFields,
		Objective:    run.Objective,
		SearchBudget: defaultSearchBudget,
		FetchBudget:  defaultFetchBudget,
	}
	if org, oerr := s.orgs.Get(ctx, orgID); oerr == nil && org != nil {
		pd.ProductDescription = org.ProductDescription
		pd.ICPNotes = org.ICPNotes
		pd.VoiceProfile = org.VoiceProfile
	}
	if s.skills != nil {
		pd.Skills = s.skills.EnabledPreamble(ctx, orgID)
	}

	// Tools: budgeted web tools + load_skill + save_research.
	searchBudget, fetchBudget := defaultSearchBudget, defaultFetchBudget
	var captured *models.ResearchResult
	saveAttempts := 0
	tools := make([]generation.ToolDef, 0, 4)
	for _, t := range s.registry.ToolDefsByName(inv, "search_web", "fetch_url", "load_skill") {
		switch t.Name {
		case "search_web":
			tools = append(tools, budgeted(t, &searchBudget))
		case "fetch_url":
			tools = append(tools, budgeted(t, &fetchBudget))
		default: // load_skill: unbudgeted
			tools = append(tools, t)
		}
	}
	tools = append(tools, saveResearchTool(&captured, &saveAttempts))

	paid, _ := s.feature.IsPaidOrganization(ctx, orgID)
	model := s.provider.ModelForTier(paid)
	run.ModelUsed = model

	result, rerr := s.provider.RunAgent(ctx, generation.AgentRequest{
		System:        BuildSystemPrompt(pd),
		Messages:      []generation.AgentMessage{{Role: "user", Content: "Research this contact now. Use search_web and fetch_url within budget, then call save_research exactly once."}},
		Tools:         tools,
		Model:         model,
		MaxIterations: maxIterations,
	})
	if result != nil {
		run.TokensUsed = result.TokensUsed
	}

	switch {
	case rerr != nil:
		// Provider failure before any charge: nothing to refund (research is
		// charged on save, which did not happen).
		run.Status = models.ResearchFailed
		run.Error = "the research agent hit an error"
	case captured == nil:
		run.Status = models.ResearchFailed
		run.Error = "no findings were saved"
	default:
		// Charge on save, idempotent per run. A free/local model (AI_FREE)
		// runs un-metered, so skip the charge entirely.
		if !s.provider.IsLocal() {
			if _, cerr := s.credits.Consume(ctx, orgID, credits.CostResearchRun, "research_run", model, run.TokensUsed, "research:"+run.ID.String()); cerr != nil {
				run.Status = models.ResearchFailed
				if errors.Is(cerr, credits.ErrInsufficientCredits) {
					run.Error = "insufficient credits"
				} else {
					run.Error = "failed to charge credits"
				}
				break
			}
			run.CreditsCharged = credits.CostResearchRun
		}
		run.Result = *captured
		if captured.NothingFound {
			run.Status = models.ResearchNothingFound
		} else {
			run.Status = models.ResearchSucceeded
		}
	}
	return finish()
}

// budgeted wraps a tool so it refuses (returns an error result, not a hard
// failure) once its per-run budget is spent, nudging the agent to save.
func budgeted(def generation.ToolDef, remaining *int) generation.ToolDef {
	orig := def.Handler
	def.Handler = func(ctx context.Context, args json.RawMessage) (string, error) {
		if *remaining <= 0 {
			return `{"error":"budget exhausted for this tool; save your findings now with save_research"}`, nil
		}
		*remaining--
		return orig(ctx, args)
	}
	return def
}

// saveResearchTool builds the save_research tool. It validates the strict
// schema (every signal/artifact must carry a url; caps; confidence enum) and
// captures the result. On the first invalid call it returns the error to the
// model to reprompt; a second invalid call fails hard (captured stays nil).
func saveResearchTool(captured **models.ResearchResult, attempts *int) generation.ToolDef {
	return generation.ToolDef{
		Name:        "save_research",
		Description: "Save your final findings. Call exactly once. Every signal and public_artifact MUST include a url you actually fetched. Set nothing_found true if you found no cited signal.",
		InputSchema: saveResearchSchema(),
		Risk:        generation.RiskRead, // research auto-runs; no human approval
		Handler: func(_ context.Context, args json.RawMessage) (string, error) {
			var res models.ResearchResult
			if err := json.Unmarshal(args, &res); err != nil {
				*attempts++
				if *attempts >= 2 {
					return "", fmt.Errorf("save_research arguments could not be parsed twice; giving up")
				}
				return `{"error":"could not parse arguments; call save_research again with valid JSON matching the schema"}`, nil
			}
			if verr := validateResearch(&res); verr != nil {
				*attempts++
				if *attempts >= 2 {
					return "", fmt.Errorf("save_research validation failed again: %v", verr)
				}
				return fmt.Sprintf(`{"error":"validation failed: %s. Fix and call save_research again; every signal and artifact needs a url and a confidence of high, medium, or low."}`, verr), nil
			}
			*captured = &res
			return `{"status":"saved"}`, nil
		},
	}
}

// validateResearch enforces the strict contract: cited urls, caps, confidence
// enum. A nothing_found result needs no signals.
func validateResearch(r *models.ResearchResult) error {
	if r.NothingFound {
		return nil
	}
	if len(r.Signals) > 5 {
		return fmt.Errorf("at most 5 signals (got %d)", len(r.Signals))
	}
	if len(r.Hooks) > 3 {
		return fmt.Errorf("at most 3 hooks (got %d)", len(r.Hooks))
	}
	for i, sig := range r.Signals {
		if strings.TrimSpace(sig.URL) == "" {
			return fmt.Errorf("signal %d is missing its url", i+1)
		}
		if !validConfidence(sig.Confidence) {
			return fmt.Errorf("signal %d has an invalid confidence %q (use high, medium, or low)", i+1, sig.Confidence)
		}
	}
	if r.Person != nil {
		for i, a := range r.Person.PublicArtifacts {
			if strings.TrimSpace(a.URL) == "" {
				return fmt.Errorf("public_artifact %d is missing its url", i+1)
			}
		}
	}
	// A non-nothing-found result should carry at least one cited signal.
	if len(r.Signals) == 0 && (r.Person == nil || len(r.Person.PublicArtifacts) == 0) {
		return errors.New("no cited signals or artifacts; set nothing_found true instead")
	}
	return nil
}

func validConfidence(c string) bool {
	switch strings.ToLower(strings.TrimSpace(c)) {
	case "high", "medium", "low":
		return true
	}
	return false
}

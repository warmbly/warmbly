package aitools

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/analytics"
	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/campaign"
	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/crm"
	"github.com/warmbly/warmbly/internal/app/feature"
	"github.com/warmbly/warmbly/internal/app/integration"
	"github.com/warmbly/warmbly/internal/app/unibox"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// Deps are the SERVICE-LAYER dependencies the tool handlers close over. Every
// handler calls one of these exactly as the matching HTTP handler would, so a
// tool never bypasses business rules, validation, or org scoping.
type Deps struct {
	Contacts    contact.ContactService
	CRM         crm.CRMService
	Campaigns   campaign.CampaignService
	Analytics   analytics.AnalyticsService
	Unibox      unibox.UniboxService
	Automations integration.Service
	Audit       audit.AuditService
	Search      generation.SearchClient
	Cache       *cache.Cache
	// FeatureGate applies the same subscription entitlement checks the HTTP
	// handlers do (e.g. unibox requires an active trial/paid plan), so a tool
	// can never read data a 403'd HTTP route would refuse.
	FeatureGate feature.FeatureGateService
	// Skills backs the load_skill tool (org playbooks). Optional.
	Skills SkillLookup
	// AppBaseURL is the dashboard origin used to build deep links in draft
	// artifacts (e.g. https://app.warmbly.com). Empty falls back to a relative
	// path.
	AppBaseURL string
}

// SkillLookup returns an enabled org playbook's full content by name (backs the
// load_skill tool). *skills.service satisfies it.
type SkillLookup interface {
	GetByName(ctx context.Context, orgID uuid.UUID, name string) (*models.AISkill, error)
}

// BuildRegistry constructs the registry with every initial tool registered.
// Called once at startup; the returned registry is queried per invocation.
func BuildRegistry(d Deps) *Registry {
	r := NewRegistry()
	d.registerContactTools(r)
	d.registerCRMTools(r)
	d.registerCampaignTools(r)
	d.registerAnalyticsTools(r)
	d.registerUniboxTools(r)
	d.registerAutomationTools(r)
	d.registerWebTools(r)
	d.registerSkillTools(r)
	return r
}

// logAudit fires an AUDIT_CREATED event via the normal audit path so the
// dashboard spine refreshes for every teammate after a write-class tool runs.
func (d Deps) logAudit(ctx context.Context, inv Invocation, action models.AuditAction, entity models.AuditEntityType, entityID *uuid.UUID, meta map[string]string) {
	if d.Audit != nil {
		d.Audit.LogAction(ctx, inv.OrgID, inv.UserID, action, entity, entityID, inv.IP, inv.UserAgent, nil, meta)
	}
}

// --- shared helpers ---

// decodeArgs unmarshals the model's tool arguments into T, mapping a decode
// failure to ErrInvalidArgs (fed back to the model so it can correct itself).
func decodeArgs[T any](args json.RawMessage) (T, error) {
	var v T
	if len(args) == 0 {
		return v, nil
	}
	if err := json.Unmarshal(args, &v); err != nil {
		return v, ErrInvalidArgs
	}
	return v, nil
}

// jsonResult marshals a tool result to the compact JSON string fed to the model.
func jsonResult(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// fromErrx converts a service *errx.Error into a plain error for the tool
// result (nil stays nil). Only the human-readable message is surfaced.
func fromErrx(xerr *errx.Error) error {
	if xerr == nil {
		return nil
	}
	return errors.New(xerr.Message)
}

// parseUUIDArg parses a required UUID argument, returning ErrInvalidArgs on a
// bad/empty value.
func parseUUIDArg(s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, ErrInvalidArgs
	}
	return id, nil
}

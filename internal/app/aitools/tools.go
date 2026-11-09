package aitools

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/analytics"
	"github.com/warmbly/warmbly/internal/app/apikey"
	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/campaign"
	"github.com/warmbly/warmbly/internal/app/compose"
	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/crm"
	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/app/emailsend"
	"github.com/warmbly/warmbly/internal/app/feature"
	"github.com/warmbly/warmbly/internal/app/form"
	"github.com/warmbly/warmbly/internal/app/integration"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/segment"
	"github.com/warmbly/warmbly/internal/app/sequence"
	"github.com/warmbly/warmbly/internal/app/subscription"
	"github.com/warmbly/warmbly/internal/app/unibox"
	"github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/pkg/webread"
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
	// WorkspaceSearch is a workspace's own web search (an MCP tool it picked,
	// its Jina key), what search_web uses when the instance has none.
	WorkspaceSearch func(ctx context.Context, orgID uuid.UUID) generation.SearchClient
	// Reader fetches a page through a reader service after a site turns
	// fetch_url away.
	Reader webread.Reader
	Cache  *cache.Cache
	// Emails / EmailSend / Compose / Warmup back the mailbox-management and
	// unified-inbox send tools. EmailSend is the only path that transmits mail
	// to a real recipient (its tools are RiskSend).
	Emails    email.EmailService
	EmailSend emailsend.EmailSendService
	Compose   compose.Service
	Warmup    warmup.Service
	// Sequences backs the campaign-step (sequence node) tools.
	Sequences sequence.SequenceService
	// Org backs the team, invitation, and org-settings tools (JWT-only: these
	// HTTP routes have no API-key permission bit).
	Org organization.OrganizationService
	// APIKeys / Webhooks back the developer-surface management tools.
	APIKeys  apikey.APIKeyService
	Webhooks webhook.Service
	// Subscription backs the read-only billing tools (JWT-only).
	Subscription subscription.SubscriptionService
	// Advanced is the suppression checker mirroring the compose handler's guard
	// (a bounced/complained/unsubscribed recipient is unreachable). Optional; a
	// nil checker skips the pre-send suppression gate.
	Advanced SuppressionChecker
	// FeatureGate applies the same subscription entitlement checks the HTTP
	// handlers do (e.g. unibox requires an active trial/paid plan), so a tool
	// can never read data a 403'd HTTP route would refuse.
	FeatureGate feature.FeatureGateService
	// Segments and Forms back the audience and lead-capture tools, both gated
	// on the contact permissions.
	Segments segment.Service
	Forms    form.Service
	// Suppressions edits the do-not-contact list. A second field rather than a
	// wider Advanced, so a deployment can keep the send guard above without
	// exposing the list to agents.
	Suppressions SuppressionManager
	// Skills backs the load_skill tool (org playbooks). Optional.
	Skills SkillLookup
	// Advisor backs the read-only recommendation tools, so the assistant can
	// ground an answer in what the Advisor has already diagnosed rather than
	// re-deriving it from raw stats. Optional.
	Advisor AdvisorReader
	// WarmupScheduler seeds a mailbox's warmup chain after a tool turns it on. Optional.
	WarmupScheduler func(ctx context.Context, accountID uuid.UUID) error
	// Slack backs the Slack settings tools, added by RegisterSlackTools once
	// the Slack app is built.
	Slack SlackControl
	// AppBaseURL is the dashboard origin used to build deep links in draft
	// artifacts (e.g. https://app.warmbly.com). Empty falls back to a relative
	// path.
	AppBaseURL string
}

// SuppressionChecker reports whether a recipient address is suppressed org-wide
// (bounced, complained, or unsubscribed). *advanced.service satisfies it. Kept
// as a local interface so aitools does not import the advanced package.
type SuppressionChecker interface {
	ShouldSuppressRecipient(ctx context.Context, orgID uuid.UUID, recipient string) (bool, string, *errx.Error)
}

// SuppressionManager reads and edits the do-not-contact list. *advanced.service
// satisfies it, the same value that satisfies SuppressionChecker above.
type SuppressionManager interface {
	ListSuppressions(ctx context.Context, organizationID uuid.UUID, q string, beforeAt *time.Time, beforeID *uuid.UUID, limit int) ([]models.SuppressedRecipient, *errx.Error)
	AddSuppressions(ctx context.Context, organizationID, actorID uuid.UUID, req *models.AddSuppressionsRequest) (*models.AddSuppressionsResult, *errx.Error)
	RemoveSuppression(ctx context.Context, organizationID, id uuid.UUID) (*models.SuppressedRecipient, *errx.Error)
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
	d.registerSequenceTools(r)
	d.registerSegmentTools(r)
	d.registerFormTools(r)
	d.registerSuppressionTools(r)
	d.registerAnalyticsTools(r)
	d.registerUniboxTools(r)
	d.registerInboxActionTools(r)
	d.registerInboxSendTools(r)
	d.registerMailboxTools(r)
	d.registerAutomationTools(r)
	d.registerTeamTools(r)
	d.registerSettingsTools(r)
	d.registerBillingTools(r)
	d.registerAPIKeyTools(r)
	d.registerWebhookTools(r)
	d.registerWebTools(r)
	d.registerSkillTools(r)
	d.registerIntegrationTools(r)
	// Advisor tools are registered separately, after the advisor service is
	// constructed against this registry (see aitools.RegisterAdvisorTools).
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

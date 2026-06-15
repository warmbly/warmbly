package advanced

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/replyclassify"
	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/gtasks"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

type Service interface {
	GetOrganizationSettings(ctx context.Context, organizationID uuid.UUID) (*models.AdvancedOutreachSettings, *errx.Error)
	UpdateOrganizationSettings(ctx context.Context, organizationID, updatedBy uuid.UUID, settings *models.AdvancedOutreachSettings) *errx.Error

	GetCampaignSettings(ctx context.Context, campaignID uuid.UUID) (*models.CampaignAdvancedSettings, *errx.Error)
	UpdateCampaignSettings(ctx context.Context, campaignID uuid.UUID, settings *models.AdvancedOutreachSettings) *errx.Error

	ListABVariants(ctx context.Context, campaignID uuid.UUID) ([]models.CampaignABVariant, *errx.Error)
	CreateABVariant(ctx context.Context, campaignID uuid.UUID, req *models.CreateCampaignABVariantRequest) (*models.CampaignABVariant, *errx.Error)
	UpdateABVariant(ctx context.Context, campaignID, variantID uuid.UUID, req *models.UpdateCampaignABVariantRequest) (*models.CampaignABVariant, *errx.Error)
	DeleteABVariant(ctx context.Context, campaignID, variantID uuid.UUID) *errx.Error

	RunPreflight(ctx context.Context, organizationID, campaignID uuid.UUID) (*models.PreflightReport, *errx.Error)
	GetDeliverabilityDashboard(ctx context.Context, organizationID uuid.UUID, from, to time.Time) (*models.DeliverabilityDashboard, *errx.Error)

	IngestDeliverabilityEvent(ctx context.Context, organizationID uuid.UUID, req *models.IngestDeliverabilityEventRequest) *errx.Error

	ShouldSuppressRecipient(ctx context.Context, organizationID uuid.UUID, recipient string) (bool, string, *errx.Error)
	// Unsubscribe suppresses a contact in response to a List-Unsubscribe action
	// (one-click POST or the manual link). Always suppresses — it's an explicit
	// recipient request, independent of the auto-suppress settings.
	Unsubscribe(ctx context.Context, campaignID, contactID uuid.UUID) *errx.Error
	SelectVariant(ctx context.Context, organizationID, campaignID, contactID, sequenceID uuid.UUID, subject, bodyHTML, bodyPlain string) (*models.VariantSelection, *errx.Error)
	OptimizeSendTime(ctx context.Context, organizationID uuid.UUID, contact *models.Contact, base time.Time) (time.Time, *errx.Error)

	StartTaskExecution(ctx context.Context, taskID uuid.UUID, executionKey string, metadata map[string]interface{}) (bool, *errx.Error)
	CompleteTaskExecution(ctx context.Context, taskID uuid.UUID, executionKey, status string, metadata map[string]interface{}) *errx.Error
	CaptureTaskDeadLetter(ctx context.Context, taskID uuid.UUID, taskType string, payload map[string]interface{}, lastError string, attempts int) *errx.Error
	ListDeadLetters(ctx context.Context, organizationID uuid.UUID, status string, limit int) ([]models.TaskDeadLetter, *errx.Error)
	ReplayDeadLetter(ctx context.Context, organizationID, deadLetterID uuid.UUID) *errx.Error

	ProcessIncomingReply(ctx context.Context, emailAccountID uuid.UUID, msg *models.EmailMessageStoreData) *errx.Error
	GetABWinnerAnalysis(ctx context.Context, organizationID, campaignID uuid.UUID) (*models.ABWinnerAnalysis, *errx.Error)

	// CreateContactTask creates a CRM task for a contact, used by the campaign
	// "create task" action node. createdBy is the campaign owner; the task's
	// AssignedTo (set in data) is the teammate chosen on the step. Records a
	// task_created activity on the contact.
	CreateContactTask(ctx context.Context, orgID, createdBy uuid.UUID, data *models.CreateCRMTask) (*models.CRMTask, *errx.Error)

	// CreateContactDeal opens a CRM deal for a contact, used by the campaign
	// "create_deal" action node (typically off a positive reply branch). data
	// carries pipeline/stage/name/value/currency; CampaignID is stamped for deal
	// attribution. Records a deal-created activity on the contact.
	CreateContactDeal(ctx context.Context, orgID uuid.UUID, createdBy uuid.UUID, data *models.CreateDeal) (*models.Deal, *errx.Error)

	// MoveContactDealStage moves the contact's most-recent OPEN deal in
	// pipelineID to stageID, used by the campaign "move_deal_stage" action node.
	// When the contact has no open deal in that pipeline it is a no-op (returns
	// nil, nil) rather than an error, so a chained reply automation doesn't fail
	// just because a deal hasn't been created yet.
	MoveContactDealStage(ctx context.Context, orgID, contactID, pipelineID, stageID uuid.UUID) (*models.Deal, *errx.Error)

	// LabelThread additively applies unibox conversation labels (categories owned
	// by userID) to a thread, for the "label_email" automation action. No-op on
	// empty input; categories not owned by userID are silently ignored.
	LabelThread(ctx context.Context, userID uuid.UUID, threadID string, categoryIDs []uuid.UUID) error
	// LabelLatestThreadForContact finds the contact's most recent conversation in
	// userID's unibox and labels it, for the "label_email" campaign step action
	// (which knows the contact but not the thread id). Returns the labeled thread
	// id, or "" when the contact has no conversation yet.
	LabelLatestThreadForContact(ctx context.Context, userID uuid.UUID, contactEmail string, categoryIDs []uuid.UUID) (string, error)

	// WireDispatcher attaches the event dispatcher that fans classified
	// replies + deliverability events out to customer webhooks and third-party
	// integration actions (Slack ping, CRM upsert).
	WireDispatcher(d EventDispatcher)
	// WireNotifier attaches the in-app notification gate (reply/bounce/complaint).
	WireNotifier(n Notifier)
	// WireRealtime attaches the org-scoped EMAIL_REPLIED realtime pulse.
	WireRealtime(p ReplyRealtimePublisher)
	// WireAutomationRunner attaches the automation runner so instant
	// "run_automation" action nodes (reply/open/click branches) can launch a flow.
	WireAutomationRunner(r AutomationRunner)

	// EmitCampaignEvent dispatches a campaign event (e.g. from a sequence
	// "notify" action node) to customer webhooks and wired integrations.
	EmitCampaignEvent(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data map[string]any)

	// FireCampaignEvent publishes a developer-defined "fire event" to the realtime
	// gateway from a campaign step (subscribers receive it over the API websocket).
	FireCampaignEvent(ctx context.Context, orgID uuid.UUID, sourceID, name string, fields []models.ActionKV, contact *models.Contact)

	// FireInstantActions runs the matched INSTANT branch's action chain for a
	// contact the moment an engagement signal lands for them, instead of waiting
	// for the next scheduled step boundary. eventKind is "reply", "open", or
	// "click" and selects which branch fields can fire (reply -> reply_* intent
	// fields; open -> "opened"; click -> "clicked"). The signal must already be
	// recorded on the contact's progress row before this is called. Best-effort
	// and non-blocking: it never returns an error and must never block the caller's
	// hot path. The tracking consumer calls this after RecordEmailOpened /
	// RecordEmailClicked; ProcessIncomingReply calls the unexported path with
	// "reply".
	FireInstantActions(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID, eventKind string)

	// DLQ auto-retry
	ProcessRetryableDeadLetters(ctx context.Context) (int, *errx.Error)
}

type service struct {
	repo                 repository.AdvancedOutreachRepository
	campaignRepo         repository.CampaignRepository
	emailRepo            repository.EmailRepository
	taskRepo             repository.TaskRepository
	contactRepo          repository.ContactRepository
	campaignProgressRepo repository.CampaignProgressRepository
	crmRepo              repository.CRMRepository
	uniboxRepo           repository.UniboxRepository
	tasksClient          *gtasks.Client
	warmupService        warmupapp.Service
	dispatcher           EventDispatcher
	notifier             Notifier
	realtime             ReplyRealtimePublisher
	automationRunner     AutomationRunner
}

func NewService(
	repo repository.AdvancedOutreachRepository,
	campaignRepo repository.CampaignRepository,
	emailRepo repository.EmailRepository,
	taskRepo repository.TaskRepository,
	contactRepo repository.ContactRepository,
	campaignProgressRepo repository.CampaignProgressRepository,
	crmRepo repository.CRMRepository,
	uniboxRepo repository.UniboxRepository,
	tasksClient *gtasks.Client,
	warmupService warmupapp.Service,
) Service {
	return &service{
		repo:                 repo,
		campaignRepo:         campaignRepo,
		emailRepo:            emailRepo,
		taskRepo:             taskRepo,
		contactRepo:          contactRepo,
		campaignProgressRepo: campaignProgressRepo,
		crmRepo:              crmRepo,
		uniboxRepo:           uniboxRepo,
		tasksClient:          tasksClient,
		warmupService:        warmupService,
	}
}

func toErrx(err error) *errx.Error {
	if err == nil {
		return nil
	}
	if xerr, ok := err.(*errx.Error); ok {
		return xerr
	}
	return errx.InternalError()
}

func (s *service) GetOrganizationSettings(ctx context.Context, organizationID uuid.UUID) (*models.AdvancedOutreachSettings, *errx.Error) {
	settings, err := s.repo.GetOutreachSettings(ctx, organizationID)
	if err != nil {
		return nil, toErrx(err)
	}
	return settings, nil
}

func (s *service) UpdateOrganizationSettings(ctx context.Context, organizationID, updatedBy uuid.UUID, settings *models.AdvancedOutreachSettings) *errx.Error {
	if settings == nil {
		return errx.New(errx.BadRequest, "settings are required")
	}
	if err := s.repo.UpsertOutreachSettings(ctx, organizationID, updatedBy, settings); err != nil {
		return toErrx(err)
	}
	return nil
}

func (s *service) GetCampaignSettings(ctx context.Context, campaignID uuid.UUID) (*models.CampaignAdvancedSettings, *errx.Error) {
	cfg, err := s.repo.GetCampaignAdvancedSettings(ctx, campaignID)
	if err != nil {
		return nil, toErrx(err)
	}
	if cfg == nil {
		return &models.CampaignAdvancedSettings{
			CampaignID: campaignID,
			Overrides:  models.DefaultAdvancedOutreachSettings(),
			UpdatedAt:  time.Now().UTC(),
		}, nil
	}
	return cfg, nil
}

func (s *service) UpdateCampaignSettings(ctx context.Context, campaignID uuid.UUID, settings *models.AdvancedOutreachSettings) *errx.Error {
	if settings == nil {
		return errx.New(errx.BadRequest, "settings are required")
	}
	if err := s.repo.UpsertCampaignAdvancedSettings(ctx, campaignID, settings); err != nil {
		return toErrx(err)
	}
	return nil
}

func (s *service) ListABVariants(ctx context.Context, campaignID uuid.UUID) ([]models.CampaignABVariant, *errx.Error) {
	out, err := s.repo.ListABVariants(ctx, campaignID)
	if err != nil {
		return nil, toErrx(err)
	}
	return out, nil
}

func (s *service) CreateABVariant(ctx context.Context, campaignID uuid.UUID, req *models.CreateCampaignABVariantRequest) (*models.CampaignABVariant, *errx.Error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, errx.New(errx.BadRequest, "variant name is required")
	}
	out, err := s.repo.CreateABVariant(ctx, campaignID, req)
	if err != nil {
		return nil, toErrx(err)
	}
	return out, nil
}

func (s *service) UpdateABVariant(ctx context.Context, campaignID, variantID uuid.UUID, req *models.UpdateCampaignABVariantRequest) (*models.CampaignABVariant, *errx.Error) {
	out, err := s.repo.UpdateABVariant(ctx, campaignID, variantID, req)
	if err != nil {
		return nil, toErrx(err)
	}
	return out, nil
}

func (s *service) DeleteABVariant(ctx context.Context, campaignID, variantID uuid.UUID) *errx.Error {
	if err := s.repo.DeleteABVariant(ctx, campaignID, variantID); err != nil {
		return toErrx(err)
	}
	return nil
}

func (s *service) effectiveSettings(ctx context.Context, organizationID, campaignID uuid.UUID) (*models.AdvancedOutreachSettings, *errx.Error) {
	orgSettings, err := s.repo.GetOutreachSettings(ctx, organizationID)
	if err != nil {
		return nil, toErrx(err)
	}
	campaignSettings, err := s.repo.GetCampaignAdvancedSettings(ctx, campaignID)
	if err != nil {
		return nil, toErrx(err)
	}
	if campaignSettings == nil {
		return orgSettings, nil
	}
	merged, err := mergeSettings(orgSettings, &campaignSettings.Overrides)
	if err != nil {
		return nil, toErrx(err)
	}
	return merged, nil
}

func mergeSettings(base, override *models.AdvancedOutreachSettings) (*models.AdvancedOutreachSettings, error) {
	if base == nil {
		def := models.DefaultAdvancedOutreachSettings()
		base = &def
	}
	if override == nil {
		copy := *base
		return &copy, nil
	}

	baseRaw, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	overrideRaw, err := json.Marshal(override)
	if err != nil {
		return nil, err
	}

	var baseMap map[string]interface{}
	var overrideMap map[string]interface{}
	if err := json.Unmarshal(baseRaw, &baseMap); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(overrideRaw, &overrideMap); err != nil {
		return nil, err
	}
	mergedMap := mergeMap(baseMap, overrideMap)
	outRaw, err := json.Marshal(mergedMap)
	if err != nil {
		return nil, err
	}
	var out models.AdvancedOutreachSettings
	if err := json.Unmarshal(outRaw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func mergeMap(base, override map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(base))
	for k, v := range base {
		out[k] = v
	}
	for k, ov := range override {
		if ovm, ok := ov.(map[string]interface{}); ok {
			if bv, exists := out[k]; exists {
				if bvm, ok := bv.(map[string]interface{}); ok {
					out[k] = mergeMap(bvm, ovm)
					continue
				}
			}
		}
		out[k] = ov
	}
	return out
}

func (s *service) ShouldSuppressRecipient(ctx context.Context, organizationID uuid.UUID, recipient string) (bool, string, *errx.Error) {
	entry, err := s.repo.IsRecipientSuppressed(ctx, organizationID, recipient)
	if err != nil {
		return false, "", toErrx(err)
	}
	if entry == nil {
		return false, "", nil
	}
	return true, entry.Reason, nil
}

// Unsubscribe resolves the campaign + contact behind a List-Unsubscribe link and
// suppresses the recipient org-wide. Always suppresses (an explicit recipient
// request), then fans out the campaign.unsubscribed event for Slack/CRM.
func (s *service) CreateContactTask(ctx context.Context, orgID, createdBy uuid.UUID, data *models.CreateCRMTask) (*models.CRMTask, *errx.Error) {
	if s.crmRepo == nil {
		return nil, errx.InternalError()
	}
	task, err := s.crmRepo.CreateCRMTask(ctx, orgID, createdBy, data)
	if err != nil {
		return nil, errx.InternalError()
	}
	if task.ContactID != nil {
		_ = s.crmRepo.RecordActivity(ctx, orgID, *task.ContactID, &createdBy, models.ActivityTaskCreated, map[string]interface{}{
			"task_id":    task.ID.String(),
			"task_title": task.Title,
			"source":     "campaign",
		})
	}
	return task, nil
}

// CreateContactDeal opens a deal for the contact (campaign "create_deal" node)
// and records a deal_created activity. Mirrors CreateContactTask.
func (s *service) CreateContactDeal(ctx context.Context, orgID uuid.UUID, createdBy uuid.UUID, data *models.CreateDeal) (*models.Deal, *errx.Error) {
	if s.crmRepo == nil {
		return nil, errx.InternalError()
	}
	deal, err := s.crmRepo.CreateDeal(ctx, orgID, data)
	if err != nil {
		return nil, toErrx(err)
	}
	if deal.ContactID != nil {
		_ = s.crmRepo.RecordActivity(ctx, orgID, *deal.ContactID, &createdBy, models.ActivityDealCreated, map[string]interface{}{
			"deal_id":   deal.ID.String(),
			"deal_name": deal.Name,
			"source":    "campaign",
		})
	}
	return deal, nil
}

// MoveContactDealStage moves the contact's most-recent OPEN deal in pipelineID
// to stageID. "Most-recent open deal" is resolved by scanning the contact's
// deals (GetDealsByContact returns them created_at DESC) and taking the first
// one whose pipeline matches and whose status is still "open". No open deal in
// that pipeline is a deliberate no-op (returns nil, nil) so a chained reply
// automation doesn't error just because nothing has been created yet.
func (s *service) MoveContactDealStage(ctx context.Context, orgID, contactID, pipelineID, stageID uuid.UUID) (*models.Deal, *errx.Error) {
	if s.crmRepo == nil {
		return nil, errx.InternalError()
	}
	deals, err := s.crmRepo.GetDealsByContact(ctx, orgID, contactID)
	if err != nil {
		return nil, toErrx(err)
	}
	var target *models.Deal
	for i := range deals {
		d := deals[i]
		if d.PipelineID == pipelineID && d.Status == models.DealStatusOpen {
			target = &d
			break // GetDealsByContact is created_at DESC => first match is newest
		}
	}
	if target == nil {
		// No open deal in this pipeline: documented no-op (not an error).
		return nil, nil
	}
	if target.StageID == stageID {
		return target, nil // already there
	}
	updated, uerr := s.crmRepo.UpdateDeal(ctx, orgID, target.ID, &models.UpdateDeal{StageID: &stageID})
	if uerr != nil {
		return nil, toErrx(uerr)
	}
	_ = s.crmRepo.RecordActivity(ctx, orgID, contactID, nil, models.ActivityDealStageChange, map[string]interface{}{
		"deal_id": updated.ID.String(),
		"from":    target.StageID.String(),
		"to":      stageID.String(),
		"source":  "campaign",
	})
	return updated, nil
}

func (s *service) Unsubscribe(ctx context.Context, campaignID, contactID uuid.UUID) *errx.Error {
	campaign, err := s.campaignRepo.GetByID(ctx, campaignID)
	if err != nil || campaign == nil || campaign.OrganizationID == nil {
		return errx.New(errx.BadRequest, "invalid unsubscribe link")
	}
	contact, cerr := s.contactRepo.GetByID(ctx, contactID)
	if cerr != nil || contact == nil || contact.Email == "" {
		return errx.New(errx.BadRequest, "invalid unsubscribe link")
	}

	if err := s.repo.UpsertSuppressedRecipient(ctx, &models.SuppressedRecipient{
		OrganizationID: *campaign.OrganizationID,
		Email:          contact.Email,
		Reason:         "one-click unsubscribe",
		Source:         models.DeliverabilityEventUnsubscribe,
		CampaignID:     &campaignID,
	}); err != nil {
		return toErrx(err)
	}

	s.emit(ctx, *campaign.OrganizationID, models.WebhookEventCampaignUnsubscribed, map[string]any{
		"campaign_id":   campaignID.String(),
		"contact_id":    contactID.String(),
		"contact_email": contact.Email,
		"source":        "one_click",
	})
	return nil
}

// pickVariantWeightedRandom does a weighted random draw over active variants.
func pickVariantWeightedRandom(variants []models.CampaignABVariant) *models.CampaignABVariant {
	total := 0
	for i := range variants {
		if variants[i].Weight <= 0 {
			variants[i].Weight = 100
		}
		total += variants[i].Weight
	}
	if total <= 0 {
		return nil
	}
	pick := rand.Intn(total)
	running := 0
	for i := range variants {
		running += variants[i].Weight
		if pick < running {
			return &variants[i]
		}
	}
	return &variants[len(variants)-1]
}

// abControlWeight is the implicit weight of a step's original content (the
// control arm) in a step-scoped A/B split, matching the default variant weight
// so one variant yields an even 50/50 split with the original.
const abControlWeight = 100

// pickVariantDeterministic does a weighted draw seeded by a stable string, so
// the same seed always picks the same variant (used for per-step assignment).
func pickVariantDeterministic(variants []models.CampaignABVariant, seed string) *models.CampaignABVariant {
	total := 0
	for i := range variants {
		if variants[i].Weight <= 0 {
			variants[i].Weight = 100
		}
		total += variants[i].Weight
	}
	if total <= 0 {
		return nil
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	pick := int(h.Sum32() % uint32(total))
	running := 0
	for i := range variants {
		running += variants[i].Weight
		if pick < running {
			return &variants[i]
		}
	}
	return &variants[len(variants)-1]
}

func (s *service) SelectVariant(ctx context.Context, organizationID, campaignID, contactID, sequenceID uuid.UUID, subject, bodyHTML, bodyPlain string) (*models.VariantSelection, *errx.Error) {
	settings, xerr := s.effectiveSettings(ctx, organizationID, campaignID)
	if xerr != nil {
		return nil, xerr
	}
	if !settings.ABTesting.Enabled {
		return &models.VariantSelection{Subject: subject, BodyHTML: bodyHTML, BodyPlain: bodyPlain}, nil
	}

	variants, err := s.repo.ListABVariants(ctx, campaignID)
	if err != nil {
		return nil, toErrx(err)
	}
	// Partition active variants into step-scoped (this step) and campaign-level.
	var stepVariants, campaignVariants []models.CampaignABVariant
	for _, v := range variants {
		if !v.IsActive {
			continue
		}
		if v.SequenceID != nil {
			if *v.SequenceID == sequenceID {
				stepVariants = append(stepVariants, v)
			}
		} else {
			campaignVariants = append(campaignVariants, v)
		}
	}

	var selected *models.CampaignABVariant
	if len(stepVariants) > 0 {
		// Step-scoped: the step's own content is the control arm. Contacts split
		// across the original PLUS the active variants by weight, deterministically
		// per (contact, step). The original's share is the weight of its is_control
		// row when the user has set one, otherwise the default control weight. The
		// control arm carries the zero id (or an is_control row, whose content is
		// ignored); if it wins we send the step's own content.
		controlWeight := abControlWeight
		arms := make([]models.CampaignABVariant, 0, len(stepVariants))
		for _, v := range stepVariants {
			if v.IsControl {
				controlWeight = v.Weight
				continue
			}
			arms = append(arms, v)
		}
		pool := append([]models.CampaignABVariant{{Weight: controlWeight}}, arms...)
		selected = pickVariantDeterministic(pool, contactID.String()+":"+sequenceID.String())
		if selected != nil && (selected.ID == uuid.Nil || selected.IsControl) {
			return &models.VariantSelection{Subject: subject, BodyHTML: bodyHTML, BodyPlain: bodyPlain}, nil
		}
	} else if len(campaignVariants) > 0 {
		// Campaign-level (legacy): keep the assignment-based selection so a
		// contact stays on one variant across the whole campaign.
		assigned, aerr := s.repo.GetAssignedVariant(ctx, campaignID, contactID)
		if aerr != nil {
			return nil, toErrx(aerr)
		}
		if assigned != nil && assigned.IsActive && assigned.SequenceID == nil {
			selected = assigned
		} else {
			selected = pickVariantWeightedRandom(campaignVariants)
			if selected != nil {
				_ = s.repo.AssignVariant(ctx, campaignID, contactID, selected.ID)
			}
		}
	}

	if selected == nil {
		return &models.VariantSelection{Subject: subject, BodyHTML: bodyHTML, BodyPlain: bodyPlain}, nil
	}

	finalSubject := subject
	if selected.Subject != "" {
		finalSubject = selected.Subject
	}
	finalHTML := bodyHTML
	if selected.BodyHTML != "" {
		finalHTML = selected.BodyHTML
	}
	finalPlain := bodyPlain
	if selected.BodyPlain != "" {
		finalPlain = selected.BodyPlain
	}
	return &models.VariantSelection{
		VariantID: &selected.ID,
		Subject:   finalSubject,
		BodyHTML:  finalHTML,
		BodyPlain: finalPlain,
	}, nil
}

func parseSenderEmail(addrs []string) string {
	if len(addrs) == 0 {
		return ""
	}
	primary := strings.TrimSpace(addrs[0])
	if primary == "" {
		return ""
	}
	if parsed, err := mail.ParseAddress(primary); err == nil {
		return strings.ToLower(strings.TrimSpace(parsed.Address))
	}
	return strings.ToLower(strings.Trim(primary, "<>"))
}

func cleanMessageID(mid string) string {
	return strings.TrimSpace(strings.Trim(mid, "<>"))
}

// buildReplyHeaders synthesizes the header map the reply classifier's Layer 1
// (header) scan reads. EmailMessageStoreData does not carry the full raw header
// block, but it does carry the structured fields the deterministic markers care
// about (From, Reply-To) plus any custom headers the worker folded into Flags
// using the "Header-Name:value" convention (the same encoding extractHeaderValue
// relies on for the warmup token). That lets RFC 3834 / Precedence / X-Autoreply
// markers reach the classifier when the worker forwarded them, while the From
// (mailer-daemon / no-reply) and Subject ("Automatic reply") signals work from
// the always-present structured fields.
func buildReplyHeaders(msg *models.EmailMessageStoreData) map[string][]string {
	if msg == nil {
		return nil
	}
	h := map[string][]string{}
	if len(msg.FromAddr) > 0 {
		h["From"] = msg.FromAddr
	}
	if len(msg.ReplyTo) > 0 {
		h["Reply-To"] = msg.ReplyTo
	}
	if msg.Subject != "" {
		h["Subject"] = []string{msg.Subject}
	}
	// Custom headers the worker stored as "Header-Name:value" flags (auto-reply
	// markers, Precedence, etc.). Split on the FIRST colon so header values that
	// contain ':' survive intact.
	for _, flag := range msg.Flags {
		if i := strings.Index(flag, ":"); i > 0 {
			name := strings.TrimSpace(flag[:i])
			val := strings.TrimSpace(flag[i+1:])
			if name != "" && !strings.HasPrefix(name, "\\") {
				h[name] = append(h[name], val)
			}
		}
	}
	return h
}

func containsAnyKeyword(text string, keywords []string) bool {
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	for _, k := range keywords {
		if k == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(k)) {
			return true
		}
	}
	return false
}

func classifyReply(text string, cfg models.ReplyIntentSettings) (models.ReplyIntentType, float64) {
	lower := strings.ToLower(text)
	if strings.TrimSpace(lower) == "" {
		return models.ReplyIntentNeutral, 0.2
	}

	score := map[models.ReplyIntentType]float64{
		models.ReplyIntentPositive:    0,
		models.ReplyIntentNegative:    0,
		models.ReplyIntentOutOfOffice: 0,
		models.ReplyIntentQuestion:    0,
	}

	for _, kw := range cfg.PositiveKeywords {
		if kw != "" && strings.Contains(lower, strings.ToLower(kw)) {
			score[models.ReplyIntentPositive] += 1.2
		}
	}
	for _, kw := range cfg.NegativeKeywords {
		if kw != "" && strings.Contains(lower, strings.ToLower(kw)) {
			score[models.ReplyIntentNegative] += 1.5
		}
	}
	for _, kw := range cfg.OutOfOfficeKeywords {
		if kw != "" && strings.Contains(lower, strings.ToLower(kw)) {
			score[models.ReplyIntentOutOfOffice] += 2
		}
	}
	for _, kw := range cfg.QuestionKeywords {
		if kw != "" && strings.Contains(lower, strings.ToLower(kw)) {
			score[models.ReplyIntentQuestion] += 1
		}
	}
	if strings.Contains(lower, "?") {
		score[models.ReplyIntentQuestion] += 0.8
	}

	best := models.ReplyIntentNeutral
	bestScore := 0.0
	for intent, sc := range score {
		if sc > bestScore {
			best = intent
			bestScore = sc
		}
	}
	if bestScore == 0 {
		return models.ReplyIntentNeutral, 0.35
	}
	conf := bestScore / (bestScore + 1.5)
	if conf > 0.99 {
		conf = 0.99
	}
	return best, conf
}

func (s *service) ProcessIncomingReply(ctx context.Context, emailAccountID uuid.UUID, msg *models.EmailMessageStoreData) *errx.Error {
	account, xerr := s.emailRepo.GetByID(ctx, emailAccountID)
	if xerr != nil {
		return xerr
	}
	if account == nil || account.OrganizationID == nil {
		return nil
	}

	settings, err := s.repo.GetOutreachSettings(ctx, *account.OrganizationID)
	if err != nil {
		return toErrx(err)
	}
	if !settings.ReplyIntent.Enabled {
		return nil
	}

	sender := parseSenderEmail(msg.FromAddr)
	if sender == "" {
		return nil
	}

	text := strings.TrimSpace(msg.Snippet)
	text = strings.TrimSpace(text + "\n" + msg.Subject)
	intent, confidence := classifyReply(text, settings.ReplyIntent)

	// Layered reply classification (header -> lexicon -> optional model) is run
	// further down, once the campaign context is known to store it on. Classifying
	// only inside that block means a reply with no campaign match never spends a
	// model call.

	var campaignID *uuid.UUID
	var sequenceID *uuid.UUID
	var contactID *uuid.UUID
	var taskID *uuid.UUID

	// First, try exact message threading via In-Reply-To.
	for _, mid := range msg.InReplyTo {
		candidate := cleanMessageID(mid)
		if candidate == "" {
			continue
		}
		task, err := s.taskRepo.GetTaskByMessageID(ctx, candidate)
		if err != nil || task == nil || task.TaskType != "campaign" {
			continue
		}
		taskID = &task.ID
		ct, err := s.taskRepo.GetCampaignTask(ctx, task.ID)
		if err == nil && ct != nil {
			campaignID = ct.CampaignID
			contactID = ct.ContactID
			sequenceID = ct.SequenceID
		}
		break
	}

	if contactID == nil {
		contact, xerr := s.contactRepo.GetByEmailAndOrganization(ctx, *account.OrganizationID, sender)
		if xerr != nil {
			return xerr
		}
		if contact != nil {
			contactID = &contact.ID
		}
	}

	if campaignID == nil && contactID != nil {
		latest, err := s.campaignProgressRepo.GetLatestCampaignSequenceForContact(ctx, *contactID)
		if err == nil && latest != nil {
			campaignID = &latest.CampaignID
			sequenceID = &latest.SequenceID
		}
	}

	if campaignID != nil && contactID != nil && sequenceID != nil {
		cID, ctID, sID := *campaignID, *contactID, *sequenceID

		// Cost guard for the optional AI layer (Layer 3). Layers 1-2 (headers +
		// lexicon) are free and always run; the paid model is only spent on a
		// genuinely new, non-trivial, human-looking reply from a contact we have
		// not already classified. So one contact spamming replies costs at most a
		// single model call, and trivial/boilerplate bodies never reach the model.
		gate := func() bool {
			if !replyclassify.WorthModeling(replyclassify.Input{BodyText: msg.Snippet}) {
				return false
			}
			if prior, perr := s.campaignProgressRepo.GetLatestReplyClass(ctx, ctID, cID); perr == nil &&
				prior != "" && prior != replyclassify.ClassUnknown {
				return false
			}
			return true
		}

		replyResult := replyclassify.ClassifyGated(ctx, replyclassify.Input{
			Headers:  buildReplyHeaders(msg),
			Subject:  msg.Subject,
			BodyText: msg.Snippet,
		}, gate)

		// Always persist the classifier verdict so reply_* branches can route on
		// it (including reply_automated for OOO / autoresponders). Layers 1-2 run
		// for every reply, so OOO/unsubscribe stay correct even when the gate
		// skipped the model.
		_ = s.campaignProgressRepo.RecordReplyClassification(ctx, cID, ctID, sID, replyResult.Class, replyResult.Source, replyResult.Confidence)

		// OOO trap fix: only a HUMAN reply stamps replied_at. An auto_reply /
		// out_of_office must NOT count as a reply, or it would (a) trip
		// stop_on_reply and silently halt the sequence, and (b) match the plain
		// "replied" branch. Both stop_on_reply and the "replied" condition key off
		// replied_at IS NOT NULL, so gating the stamp here fixes both at once.
		if !replyclassify.IsAutomated(replyResult.Class) {
			_ = s.campaignProgressRepo.RecordEmailReplied(ctx, cID, ctID, sID)
			_ = s.repo.MarkVariantEvent(ctx, cID, ctID, string(models.DeliverabilityEventReply))

			// Live org-wide pulse: the team sees the reply land on the
			// campaign without a refresh.
			if s.realtime != nil && account.OrganizationID != nil {
				s.realtime.PublishEmailReplied(ctx, account.OrganizationID.String(), account.UserID, cID.String(), ctID.String(), sender, sID.String())
			}
		}

		// INSTANT reply trigger: if the contact's CURRENT step has a reply_* intent
		// branch matching this just-classified reply, run that branch's
		// action chain for THIS contact right now (instead of waiting for the next
		// scheduled step boundary). Best-effort and non-blocking like the rest of
		// reply handling — a failure must never block inbox ingest. Fires for both
		// human and automated replies (reply_automated drives the auto-reply case)
		// and exactly once per reply event via the instant_fired["reply"] gate. The
		// just-classified reply_class and (human-only) replied_at have already been
		// persisted above, so the matcher reads them off the loaded progress row.
		s.fireInstantActions(ctx, cID, ctID, sID, "reply")
	}

	actionTaken := ""
	if settings.ReplyIntent.AutoPauseOnNegative && intent == models.ReplyIntentNegative && campaignID != nil {
		_ = s.campaignRepo.UpdateStatus(ctx, *campaignID, "paused")
		actionTaken = "paused_campaign"
	}

	if settings.ReplyIntent.AutoSuppressOnUnsubWord &&
		containsAnyKeyword(text, []string{"unsubscribe", "remove me", "stop"}) {
		_ = s.repo.UpsertSuppressedRecipient(ctx, &models.SuppressedRecipient{
			OrganizationID: *account.OrganizationID,
			Email:          sender,
			Reason:         "reply intent unsubscribe detected",
			Source:         models.DeliverabilityEventUnsubscribe,
			CampaignID:     campaignID,
			Metadata: map[string]interface{}{
				"via": "reply_intent",
			},
		})
		if actionTaken == "" {
			actionTaken = "suppressed_recipient"
		} else {
			actionTaken += ",suppressed_recipient"
		}
	}

	if settings.ReplyIntent.AutoCreateCRMTask && s.crmRepo != nil && contactID != nil {
		owner, parseErr := uuid.Parse(account.UserID)
		if parseErr == nil {
			title := fmt.Sprintf("Follow up reply intent: %s (%s)", intent, sender)
			_, _ = s.crmRepo.CreateCRMTask(ctx, *account.OrganizationID, owner, &models.CreateCRMTask{
				ContactID:  contactID,
				Title:      title,
				Priority:   "high",
				DueDate:    ptrTime(time.Now().UTC().Add(24 * time.Hour)),
				AssignedTo: &owner,
			})
			if actionTaken == "" {
				actionTaken = "created_crm_task"
			} else {
				actionTaken += ",created_crm_task"
			}
		}
	}

	_ = s.repo.CreateReplyIntent(ctx, &models.ReplyIntentRecord{
		OrganizationID: *account.OrganizationID,
		ContactEmail:   sender,
		CampaignID:     campaignID,
		TaskID:         taskID,
		Intent:         intent,
		Confidence:     confidence,
		ActionTaken:    actionTaken,
		Metadata: map[string]interface{}{
			"subject": msg.Subject,
		},
	})

	// Fan the classified reply out to customer webhooks + integration actions
	// (Slack ping, CRM upsert). The intent/confidence fields let an integration
	// automation filter for e.g. only "positive" replies. This is the trigger
	// behind "notify me when a prospect replies".
	payload := map[string]any{
		"contact_email": sender,
		"intent":        string(intent),
		"confidence":    confidence,
		"subject":       msg.Subject,
		"snippet":       msg.Snippet,
		"action_taken":  actionTaken,
		"trigger":       "campaign_reply",
		// thread_id lets a "label email" automation action tag the conversation
		// this reply belongs to. _user_id is the mailbox owner (categories are per
		// user); the leading underscore keeps it out of outbound customer webhook
		// bodies (publicEventData strips _-prefixed keys) while staying available
		// to native actions, which read the raw event data.
		"thread_id": msg.ThreadID,
		"_user_id":  account.UserID,
	}
	if campaignID != nil {
		payload["campaign_id"] = campaignID.String()
	}
	if contactID != nil {
		payload["contact_id"] = contactID.String()
	}
	s.emit(ctx, *account.OrganizationID, models.WebhookEventCampaignReplyReceived, payload)
	if intent == models.ReplyIntentNegative {
		// Negative/unsubscribe-leaning replies also fire the unsubscribe event
		// only when we actually suppressed; otherwise the reply event is enough.
		if strings.Contains(actionTaken, "suppressed_recipient") {
			s.emit(ctx, *account.OrganizationID, models.WebhookEventCampaignUnsubscribed, payload)
		}
	}

	// Raise an in-app notification to the mailbox owner (gated by their prefs).
	if uid, perr := uuid.Parse(account.UserID); perr == nil {
		cat := models.NotifInboundReply
		title := "New reply from " + sender
		if intent == models.ReplyIntentOutOfOffice {
			cat = models.NotifInboundOOO
			title = "Out-of-office from " + sender
		}
		s.notify(uid, account.OrganizationID, cat, title, msg.Subject, "/app/unibox", map[string]any{"intent": string(intent)})
	}

	return nil
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

func (s *service) IngestDeliverabilityEvent(ctx context.Context, organizationID uuid.UUID, req *models.IngestDeliverabilityEventRequest) *errx.Error {
	if req == nil {
		return errx.New(errx.BadRequest, "event payload is required")
	}
	if req.RecipientEmail == "" {
		return errx.New(errx.BadRequest, "recipient_email is required")
	}

	eventType := req.EventType
	switch eventType {
	case models.DeliverabilityEventBounce,
		models.DeliverabilityEventComplaint,
		models.DeliverabilityEventUnsubscribe,
		models.DeliverabilityEventOpen,
		models.DeliverabilityEventClick,
		models.DeliverabilityEventReply:
	default:
		return errx.New(errx.BadRequest, "invalid event_type")
	}

	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = uuid.NewString()
	}

	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		provider = "manual"
	}

	if err := s.repo.CreateDeliverabilityEvent(ctx, &models.DeliverabilityEvent{
		OrganizationID: organizationID,
		CampaignID:     req.CampaignID,
		TaskID:         req.TaskID,
		ContactID:      req.ContactID,
		EventType:      eventType,
		Provider:       provider,
		RecipientEmail: req.RecipientEmail,
		Reason:         req.Reason,
		IdempotencyKey: idempotencyKey,
		Metadata:       req.Metadata,
	}); err != nil {
		return toErrx(err)
	}

	settings, err := s.repo.GetOutreachSettings(ctx, organizationID)
	if err != nil {
		return toErrx(err)
	}

	shouldSuppress := (eventType == models.DeliverabilityEventBounce && settings.BouncePipeline.AutoSuppressOnBounce) ||
		(eventType == models.DeliverabilityEventComplaint && settings.BouncePipeline.AutoSuppressOnComplaint) ||
		(eventType == models.DeliverabilityEventUnsubscribe && settings.BouncePipeline.AutoSuppressOnUnsubscribe)

	if shouldSuppress {
		_ = s.repo.UpsertSuppressedRecipient(ctx, &models.SuppressedRecipient{
			OrganizationID: organizationID,
			Email:          req.RecipientEmail,
			Reason:         fmt.Sprintf("%s: %s", eventType, req.Reason),
			Source:         eventType,
			CampaignID:     req.CampaignID,
			Metadata:       req.Metadata,
		})
	}

	if req.CampaignID != nil && req.ContactID != nil {
		_ = s.repo.MarkVariantEvent(ctx, *req.CampaignID, *req.ContactID, string(eventType))
	}

	// Record bounces + complaints in campaign progress so analytics and the
	// breaker work correctly. Complaints were previously never recorded, which
	// is why complaint-rate auto-pause could never fire.
	if req.CampaignID != nil && req.ContactID != nil && req.TaskID != nil &&
		(eventType == models.DeliverabilityEventBounce || eventType == models.DeliverabilityEventComplaint) {
		campaignTask, cErr := s.taskRepo.GetCampaignTask(ctx, *req.TaskID)
		if cErr == nil && campaignTask != nil && campaignTask.SequenceID != nil {
			switch eventType {
			case models.DeliverabilityEventBounce:
				_ = s.campaignProgressRepo.RecordEmailBounced(ctx, *req.CampaignID, *req.ContactID, *campaignTask.SequenceID)
			case models.DeliverabilityEventComplaint:
				_ = s.campaignProgressRepo.RecordEmailComplained(ctx, *req.CampaignID, *req.ContactID, *campaignTask.SequenceID)
			}
		}
	}

	if req.CampaignID != nil &&
		(eventType == models.DeliverabilityEventBounce || eventType == models.DeliverabilityEventComplaint) {
		s.evaluateCampaignBreaker(ctx, organizationID, *req.CampaignID, settings)
	}

	// Trigger warmup health re-evaluation on bounce or complaint events.
	// This connects deliverability signals to the warmup pool health system.
	if s.warmupService != nil && req.TaskID != nil &&
		(eventType == models.DeliverabilityEventBounce || eventType == models.DeliverabilityEventComplaint) {
		task, tErr := s.taskRepo.GetTask(ctx, *req.TaskID)
		if tErr == nil && task != nil {
			_, _ = s.warmupService.ApplySpamReport(ctx, uuid.Nil, task.EmailAccountID, req.IdempotencyKey, string(eventType))
		}
	}

	// Fan bounce / complaint / unsubscribe out to customer webhooks +
	// integration actions so a Slack channel or CRM can react in real time.
	payload := map[string]any{
		"contact_email": req.RecipientEmail,
		"recipient":     req.RecipientEmail,
		"event_type":    string(eventType),
		"provider":      provider,
		"reason":        req.Reason,
	}
	if req.CampaignID != nil {
		payload["campaign_id"] = req.CampaignID.String()
	}
	if req.ContactID != nil {
		payload["contact_id"] = req.ContactID.String()
	}
	switch eventType {
	case models.DeliverabilityEventBounce:
		s.emit(ctx, organizationID, models.WebhookEventCampaignEmailBounced, payload)
		s.emit(ctx, organizationID, models.WebhookEventDeliverabilityBounce, payload)
	case models.DeliverabilityEventComplaint:
		s.emit(ctx, organizationID, models.WebhookEventDeliverabilityComplaint, payload)
	case models.DeliverabilityEventUnsubscribe:
		s.emit(ctx, organizationID, models.WebhookEventCampaignUnsubscribed, payload)
	}

	// In-app notification to the campaign owner for bounce/complaint (gated by
	// their prefs). Resolvable only when the event is tied to a campaign.
	if (eventType == models.DeliverabilityEventBounce || eventType == models.DeliverabilityEventComplaint) && req.CampaignID != nil {
		if camp, cerr := s.campaignRepo.GetByID(ctx, *req.CampaignID); cerr == nil && camp != nil {
			if uid, perr := uuid.Parse(camp.UserID); perr == nil {
				cat := models.NotifHealthBounce
				title := "Bounce: " + req.RecipientEmail
				if eventType == models.DeliverabilityEventComplaint {
					cat = models.NotifHealthComplaint
					title = "Spam complaint: " + req.RecipientEmail
				}
				org := organizationID
				s.notify(uid, &org, cat, title, req.Reason, "/app/deliverability", map[string]any{"provider": provider})
			}
		}
	}

	return nil
}

// Campaign deliverability circuit breaker tuning. Mirrors Instantly's safeguards:
// a minimum sample before acting (so a single early bounce can't pause a
// campaign) and a rolling window so the breaker reacts to recent behaviour
// rather than a campaign's lifetime average.
const (
	campaignBreakerWindow    = 7 * 24 * time.Hour
	campaignBreakerMinSample = 50
	// Early-warning band: emit a warning webhook at half the pause threshold.
	campaignBreakerWarnRatio = 0.5
)

// evaluateCampaignBreaker auto-pauses a campaign when its rolling bounce or
// complaint rate breaches the configured threshold, and emits an early-warning
// webhook in the band below. Rolling-first with a cumulative fallback when the
// recent window is too small a sample.
func (s *service) evaluateCampaignBreaker(ctx context.Context, orgID, campaignID uuid.UUID, settings *models.AdvancedOutreachSettings) {
	if settings == nil || !settings.BouncePipeline.AutoPauseCampaignOnSpike {
		return
	}
	bounceThresh := settings.BouncePipeline.PauseBounceRateThreshold
	complaintThresh := settings.BouncePipeline.PauseComplaintRateThreshold
	if bounceThresh <= 0 && complaintThresh <= 0 {
		return
	}

	sent, bounced, complained := 0, 0, 0
	if rolling, err := s.campaignProgressRepo.GetCampaignRollingRates(ctx, campaignID, time.Now().Add(-campaignBreakerWindow)); err == nil && rolling != nil && rolling.Sent >= campaignBreakerMinSample {
		sent, bounced, complained = rolling.Sent, rolling.Bounced, rolling.Complained
	} else if progress, pErr := s.campaignProgressRepo.GetCampaignProgress(ctx, campaignID); pErr == nil && progress != nil {
		sent, bounced, complained = progress.EmailsSent, progress.EmailsBounced, progress.EmailsComplained
	}

	// Not enough delivered volume yet to judge — never pause on a tiny sample.
	if sent < campaignBreakerMinSample {
		return
	}

	bounceRate := float64(bounced) / float64(sent) * 100
	complaintRate := float64(complained) / float64(sent) * 100

	pauseBounce := bounceThresh > 0 && bounceRate >= bounceThresh
	pauseComplaint := complaintThresh > 0 && complaintRate >= complaintThresh
	if pauseBounce || pauseComplaint {
		if err := s.campaignRepo.UpdateStatus(ctx, campaignID, "paused"); err == nil {
			s.emit(ctx, orgID, models.WebhookEventCampaignPaused, map[string]any{
				"campaign_id":    campaignID.String(),
				"reason":         "deliverability_auto_pause",
				"bounce_rate":    bounceRate,
				"complaint_rate": complaintRate,
				"sample_size":    sent,
				"breached":       breachLabel(pauseBounce, pauseComplaint),
			})
		}
		return
	}

	warnBounce := bounceThresh > 0 && bounceRate >= bounceThresh*campaignBreakerWarnRatio
	warnComplaint := complaintThresh > 0 && complaintRate >= complaintThresh*campaignBreakerWarnRatio
	if warnBounce || warnComplaint {
		s.emit(ctx, orgID, models.WebhookEventCampaignDeliverabilityWarning, map[string]any{
			"campaign_id":    campaignID.String(),
			"bounce_rate":    bounceRate,
			"complaint_rate": complaintRate,
			"sample_size":    sent,
		})
	}
}

func breachLabel(bounce, complaint bool) string {
	switch {
	case bounce && complaint:
		return "bounce_and_complaint"
	case bounce:
		return "bounce"
	default:
		return "complaint"
	}
}

func (s *service) OptimizeSendTime(ctx context.Context, organizationID uuid.UUID, contact *models.Contact, base time.Time) (time.Time, *errx.Error) {
	settings, err := s.repo.GetOutreachSettings(ctx, organizationID)
	if err != nil {
		return base, toErrx(err)
	}
	if !settings.SendTimeOptimization.Enabled {
		return base, nil
	}

	targetTZ := settings.SendTimeOptimization.DefaultContactTimezone
	if targetTZ == "" {
		targetTZ = "UTC"
	}
	if settings.SendTimeOptimization.UseContactTimezone && contact != nil {
		if tz, ok := contact.CustomFields["timezone"]; ok && strings.TrimSpace(tz) != "" {
			targetTZ = strings.TrimSpace(tz)
		}
	}
	loc, lerr := time.LoadLocation(targetTZ)
	if lerr != nil {
		loc = time.UTC
	}

	candidate := base.In(loc)
	preferred := settings.SendTimeOptimization.PreferredHours
	if len(preferred) == 0 {
		preferred = []int{9, 10, 11, 14, 15, 16}
	}

	hour := candidate.Hour()
	chosenHour := -1
	for _, h := range preferred {
		if h >= hour {
			chosenHour = h
			break
		}
	}
	if chosenHour == -1 {
		candidate = candidate.Add(24 * time.Hour)
		chosenHour = preferred[0]
	}
	candidate = time.Date(candidate.Year(), candidate.Month(), candidate.Day(), chosenHour, 0, 0, 0, loc)

	if candidate.Weekday() == time.Saturday || candidate.Weekday() == time.Sunday {
		if settings.SendTimeOptimization.WeekendWeightMultiplier < 1 {
			for candidate.Weekday() == time.Saturday || candidate.Weekday() == time.Sunday {
				candidate = candidate.Add(24 * time.Hour)
			}
			candidate = time.Date(candidate.Year(), candidate.Month(), candidate.Day(), preferred[0], 0, 0, 0, loc)
		}
	}

	return candidate.UTC(), nil
}

func (s *service) StartTaskExecution(ctx context.Context, taskID uuid.UUID, executionKey string, metadata map[string]interface{}) (bool, *errx.Error) {
	duplicate, err := s.repo.StartTaskExecution(ctx, taskID, executionKey, metadata)
	if err != nil {
		return false, toErrx(err)
	}
	return duplicate, nil
}

func (s *service) CompleteTaskExecution(ctx context.Context, taskID uuid.UUID, executionKey, status string, metadata map[string]interface{}) *errx.Error {
	if err := s.repo.CompleteTaskExecution(ctx, taskID, executionKey, status, metadata); err != nil {
		return toErrx(err)
	}
	return nil
}

func (s *service) CaptureTaskDeadLetter(ctx context.Context, taskID uuid.UUID, taskType string, payload map[string]interface{}, lastError string, attempts int) *errx.Error {
	maxAttempts := 5

	// Compute next retry time using exponential backoff: 30s * 2^attempts
	var nextRetryAt *time.Time
	if attempts < maxAttempts {
		backoff := time.Duration(30*(1<<uint(attempts))) * time.Second
		t := time.Now().UTC().Add(backoff)
		nextRetryAt = &t
	}

	item := &models.TaskDeadLetter{
		TaskID:      taskID,
		TaskType:    taskType,
		Payload:     payload,
		LastError:   lastError,
		Attempts:    attempts,
		MaxAttempts: maxAttempts,
		Status:      "pending",
		NextRetryAt: nextRetryAt,
	}
	if err := s.repo.CreateTaskDeadLetter(ctx, item); err != nil {
		return toErrx(err)
	}
	return nil
}

func (s *service) ListDeadLetters(ctx context.Context, organizationID uuid.UUID, status string, limit int) ([]models.TaskDeadLetter, *errx.Error) {
	out, err := s.repo.ListTaskDeadLetters(ctx, organizationID, status, limit)
	if err != nil {
		return nil, toErrx(err)
	}
	return out, nil
}

func (s *service) ReplayDeadLetter(ctx context.Context, organizationID, deadLetterID uuid.UUID) *errx.Error {
	if s.tasksClient == nil {
		return errx.New(errx.BadRequest, "cloud tasks client not configured")
	}

	dlq, err := s.repo.GetTaskDeadLetter(ctx, deadLetterID, organizationID)
	if err != nil {
		return toErrx(err)
	}
	if dlq == nil {
		return errx.ErrNotFound
	}

	task, err := s.taskRepo.GetTask(ctx, dlq.TaskID)
	if err != nil {
		return toErrx(err)
	}
	if task == nil {
		return errx.ErrNotFound
	}

	scheduleAt := time.Now().UTC().Add(10 * time.Second)
	cloudTaskName, err := s.tasksClient.CreateTask(ctx, &proto.ProcessTask{TaskId: task.ID.String()}, scheduleAt)
	if err != nil {
		return toErrx(err)
	}
	if err := s.taskRepo.UpdateTaskScheduledAt(ctx, task.ID, scheduleAt, cloudTaskName); err != nil {
		return toErrx(err)
	}
	if err := s.taskRepo.UpdateTaskStatus(ctx, task.ID, "pending"); err != nil {
		return toErrx(err)
	}
	if err := s.repo.MarkTaskDeadLetterReplayed(ctx, deadLetterID); err != nil {
		return toErrx(err)
	}
	return nil
}

func (s *service) RunPreflight(ctx context.Context, organizationID, campaignID uuid.UUID) (*models.PreflightReport, *errx.Error) {
	campaign, err := s.campaignRepo.GetByID(ctx, campaignID)
	if err != nil || campaign == nil {
		return nil, errx.ErrNotFound
	}
	if campaign.OrganizationID == nil || *campaign.OrganizationID != organizationID {
		return nil, errx.ErrForbidden
	}

	settings, xerr := s.effectiveSettings(ctx, organizationID, campaignID)
	if xerr != nil {
		return nil, xerr
	}

	checks := make([]models.PreflightCheckResult, 0, 6)
	recommendations := make([]string, 0, 6)

	readyErr := s.campaignRepo.ValidateCampaignReady(ctx, campaignID)
	if readyErr != nil {
		checks = append(checks, models.PreflightCheckResult{
			Key:         "campaign_ready",
			Passed:      false,
			Severity:    "error",
			Message:     "Campaign has missing prerequisites (contacts, sequences, or sender accounts).",
			Remediation: "Add contacts, sequences, and at least one sender account tag match.",
		})
		recommendations = append(recommendations, "Complete core campaign setup before start.")
	} else {
		checks = append(checks, models.PreflightCheckResult{
			Key:      "campaign_ready",
			Passed:   true,
			Severity: "info",
			Message:  "Campaign has required entities configured.",
		})
	}

	if settings.Preflight.CheckScheduleWindow {
		// Per-day windows (when set) define the schedule; otherwise fall back to
		// the legacy start_time/end_time check.
		pass := !campaign.ScheduleWindows.IsEmpty() || campaign.StartTime < campaign.EndTime
		check := models.PreflightCheckResult{
			Key:      "schedule_window",
			Passed:   pass,
			Severity: "error",
			Message:  "Campaign schedule window is valid.",
		}
		if !pass {
			check.Message = "Campaign start_time must be before end_time."
			check.Remediation = "Update campaign schedule window."
			recommendations = append(recommendations, "Fix send window: start_time must be earlier than end_time.")
		}
		checks = append(checks, check)
	}

	if settings.Preflight.CheckDailyLimit {
		pass := campaign.DailyLimit > 0
		check := models.PreflightCheckResult{
			Key:      "daily_limit",
			Passed:   pass,
			Severity: "error",
			Message:  "Daily limit is configured.",
		}
		if !pass {
			check.Message = "Daily limit must be greater than zero."
			check.Remediation = "Set daily_limit > 0."
			recommendations = append(recommendations, "Set a valid daily limit to avoid burst sends.")
		}
		checks = append(checks, check)
	}

	if settings.Preflight.CheckUnsubscribeHeader {
		pass := campaign.UnsubscribeHeader
		check := models.PreflightCheckResult{
			Key:      "unsubscribe_header",
			Passed:   pass,
			Severity: "warning",
			Message:  "Unsubscribe header is enabled.",
		}
		if !pass {
			check.Message = "Unsubscribe header is disabled."
			check.Remediation = "Enable unsubscribe_header for compliance and deliverability."
			recommendations = append(recommendations, "Enable unsubscribe header.")
		}
		checks = append(checks, check)
	}

	if settings.Preflight.CheckTrackingDomain && (campaign.OpenTracking || campaign.LinkTracking) {
		accounts, err := s.emailRepo.GetByTags(ctx, campaign.UserID, campaign.EmailTags)
		if err != nil || len(accounts) == 0 {
			checks = append(checks, models.PreflightCheckResult{
				Key:         "tracking_domain",
				Passed:      false,
				Severity:    "error",
				Message:     "No sender accounts available for tracking validation.",
				Remediation: "Attach sender accounts to campaign tags.",
			})
			recommendations = append(recommendations, "Attach at least one sender account with tracking domain.")
		} else {
			missing := 0
			for _, account := range accounts {
				if strings.TrimSpace(account.TrackingDomain) == "" {
					missing++
				}
			}
			pass := missing == 0
			check := models.PreflightCheckResult{
				Key:      "tracking_domain",
				Passed:   pass,
				Severity: "warning",
				Message:  "Tracking domain configured for all senders.",
			}
			if !pass {
				check.Message = fmt.Sprintf("%d sender account(s) missing tracking domain.", missing)
				check.Remediation = "Set tracking_domain on every sender account used by this campaign."
				recommendations = append(recommendations, "Configure tracking domains on all sender accounts.")
			}
			checks = append(checks, check)
		}
	}

	if settings.Preflight.CheckABVariantConfigured && settings.ABTesting.Enabled {
		variants, err := s.repo.ListABVariants(ctx, campaignID)
		if err != nil {
			return nil, toErrx(err)
		}
		active := 0
		for _, v := range variants {
			if v.IsActive {
				active++
			}
		}
		pass := active >= 2
		check := models.PreflightCheckResult{
			Key:      "ab_variants",
			Passed:   pass,
			Severity: "warning",
			Message:  "A/B variants are configured.",
		}
		if !pass {
			check.Message = "At least two active A/B variants are required."
			check.Remediation = "Create at least two active variants or disable AB testing."
			recommendations = append(recommendations, "Create more A/B variants.")
		}
		checks = append(checks, check)
	}

	passedCount := 0
	for _, c := range checks {
		if c.Passed {
			passedCount++
		}
	}
	score := 100
	if len(checks) > 0 {
		score = int(float64(passedCount) / float64(len(checks)) * 100.0)
	}
	passed := passedCount == len(checks)

	report := &models.PreflightReport{
		ID:              uuid.New(),
		OrganizationID:  organizationID,
		CampaignID:      campaignID,
		Passed:          passed,
		Score:           score,
		Checks:          checks,
		Recommendations: recommendations,
		CreatedAt:       time.Now().UTC(),
	}
	if err := s.repo.CreatePreflightReport(ctx, report); err != nil {
		return nil, toErrx(err)
	}
	return report, nil
}

func (s *service) GetDeliverabilityDashboard(ctx context.Context, organizationID uuid.UUID, from, to time.Time) (*models.DeliverabilityDashboard, *errx.Error) {
	out, err := s.repo.GetDeliverabilityDashboard(ctx, organizationID, from, to)
	if err != nil {
		return nil, toErrx(err)
	}
	return out, nil
}

func (s *service) GetABWinnerAnalysis(ctx context.Context, organizationID, campaignID uuid.UUID) (*models.ABWinnerAnalysis, *errx.Error) {
	campaign, err := s.campaignRepo.GetByID(ctx, campaignID)
	if err != nil || campaign == nil {
		return nil, errx.ErrNotFound
	}
	if campaign.OrganizationID == nil || *campaign.OrganizationID != organizationID {
		return nil, errx.ErrForbidden
	}

	settings, xerr := s.effectiveSettings(ctx, organizationID, campaignID)
	if xerr != nil {
		return nil, xerr
	}

	stats, err := s.repo.GetABVariantStats(ctx, campaignID)
	if err != nil {
		return nil, toErrx(err)
	}

	analysis := &models.ABWinnerAnalysis{
		CampaignID:  campaignID,
		Variants:    stats,
		WinningRule: settings.ABTesting.DefaultWinningRule,
	}

	if len(stats) == 0 {
		analysis.Confidence = "none"
		return analysis, nil
	}

	// Determine winner based on the winning rule
	var bestIdx int
	var bestScore float64
	for i, v := range stats {
		var score float64
		switch settings.ABTesting.DefaultWinningRule {
		case "reply_rate":
			score = v.ReplyRate
		case "click_rate":
			score = v.ClickRate
		case "open_rate":
			score = v.OpenRate
		default:
			score = v.ReplyRate
		}
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	minSample := settings.ABTesting.MinSampleSize
	if minSample <= 0 {
		minSample = 30
	}

	winner := stats[bestIdx]
	if winner.TotalSent >= minSample {
		analysis.WinnerID = &winner.VariantID
		analysis.WinnerName = winner.VariantName
		if winner.TotalSent >= minSample*3 {
			analysis.Confidence = "high"
		} else if winner.TotalSent >= minSample {
			analysis.Confidence = "medium"
		}
	} else {
		analysis.Confidence = "low"
	}

	return analysis, nil
}

func (s *service) ProcessRetryableDeadLetters(ctx context.Context) (int, *errx.Error) {
	if s.tasksClient == nil {
		return 0, nil
	}

	items, err := s.repo.ListRetryableDeadLetters(ctx, 10)
	if err != nil {
		return 0, toErrx(err)
	}

	retried := 0
	for _, dlq := range items {
		task, err := s.taskRepo.GetTask(ctx, dlq.TaskID)
		if err != nil || task == nil {
			// Mark as exhausted if the task no longer exists
			_ = s.repo.MarkTaskDeadLetterReplayed(ctx, dlq.ID)
			continue
		}

		// Check if attempts exceeded
		if dlq.Attempts >= dlq.MaxAttempts {
			_ = s.repo.IncrementDeadLetterAttempt(ctx, dlq.ID, nil)
			continue
		}

		// Schedule retry via Cloud Tasks
		scheduleAt := time.Now().UTC().Add(10 * time.Second)
		cloudTaskName, err := s.tasksClient.CreateTask(ctx, &proto.ProcessTask{TaskId: task.ID.String()}, scheduleAt)
		if err != nil {
			// Compute next retry with exponential backoff
			backoff := time.Duration(30*(1<<uint(dlq.Attempts+1))) * time.Second
			nextRetry := time.Now().UTC().Add(backoff)
			_ = s.repo.IncrementDeadLetterAttempt(ctx, dlq.ID, &nextRetry)
			continue
		}

		if err := s.taskRepo.UpdateTaskScheduledAt(ctx, task.ID, scheduleAt, cloudTaskName); err != nil {
			continue
		}
		_ = s.taskRepo.UpdateTaskStatus(ctx, task.ID, "pending")
		_ = s.repo.MarkTaskDeadLetterReplayed(ctx, dlq.ID)
		retried++
	}

	return retried, nil
}

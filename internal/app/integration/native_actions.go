package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// validateNativeActionConfig checks a native action node's config at write time
// (defense in depth — the dashboard validates too, but API callers may not).
func validateNativeActionConfig(action models.IntegrationAction, raw json.RawMessage) error {
	cfg := parseNativeConfig(raw)
	switch action {
	case models.IntegrationActionAddTag, models.IntegrationActionRemoveTag:
		if strings.TrimSpace(cfg.CategoryID) == "" {
			return fmt.Errorf("a tag action needs a tag")
		}
	case models.IntegrationActionCreateDeal, models.IntegrationActionMoveDealStage:
		if strings.TrimSpace(cfg.DealPipelineID) == "" || strings.TrimSpace(cfg.DealStageID) == "" {
			return fmt.Errorf("a deal action needs a pipeline and stage")
		}
	case models.IntegrationActionRunAutomation:
		if strings.TrimSpace(cfg.AutomationID) == "" {
			return fmt.Errorf("a run-automation action needs a target automation")
		}
	case models.IntegrationActionLabelEmail:
		if len(parseUUIDList(cfg.LabelIDs)) == 0 {
			return fmt.Errorf("a label action needs at least one label")
		}
	case models.IntegrationActionSetVariables:
		hasOne := false
		for _, v := range cfg.SetVars {
			if strings.TrimSpace(v.Key) != "" {
				hasOne = true
				break
			}
		}
		if !hasOne {
			return fmt.Errorf("set variables needs at least one named variable")
		}
	case models.IntegrationActionFireEvent:
		if strings.TrimSpace(cfg.EventName) == "" {
			return fmt.Errorf("a fire-event action needs an event name")
		}
	case models.IntegrationActionUpsertContact:
		return validateUpsertContactConfig(cfg)
	case models.IntegrationActionAddToCampaign:
		if _, err := uuid.Parse(strings.TrimSpace(cfg.CampaignID)); err != nil {
			return fmt.Errorf("an add-to-campaign action needs a campaign")
		}
	case models.IntegrationActionAIStep:
		return validateAIStepConfig(raw)
	case models.IntegrationActionAISwitch:
		return validateAISwitchConfig(raw)
	}
	return nil
}

// validateAISwitchConfig validates the AI switch router by decider mode
// (mirrors the campaign switch): "value" mode needs a value template and at
// least two cases (no model call, so no instruction); "ai" mode needs an
// instruction and at least two cases.
func validateAISwitchConfig(raw json.RawMessage) error {
	ai := parseAIConfig(raw)
	if len(nonEmptyStrings(ai.Cases)) < 2 {
		return fmt.Errorf("an AI switch needs at least two cases")
	}
	if strings.TrimSpace(ai.SwitchOn) == "value" {
		if strings.TrimSpace(ai.SwitchValue) == "" {
			return fmt.Errorf("a value switch needs a value to match")
		}
		return nil
	}
	if strings.TrimSpace(ai.Instruction) == "" {
		return fmt.Errorf("an AI switch needs an instruction")
	}
	return nil
}

// isAllowlistedAIAction is the closed set of REVERSIBLE native actions an
// agent-mode AI step may call as tools. Defined as its own switch (NOT derived
// from IsNativeAction) so it can never drift to include run_automation,
// fire_event, a connection-backed action, or any future send/reply action.
func isAllowlistedAIAction(a models.IntegrationAction) bool {
	switch a {
	case models.IntegrationActionAddTag,
		models.IntegrationActionRemoveTag,
		models.IntegrationActionCreateTask,
		models.IntegrationActionCreateDeal,
		models.IntegrationActionMoveDealStage,
		models.IntegrationActionLabelEmail,
		models.IntegrationActionSetVariables,
		models.IntegrationActionUnsubscribe:
		return true
	default:
		return false
	}
}

// validateAIStepConfig validates the unified AI step by mode. Routing lives in
// the AI switch node, so decide is not a step mode. Agent mode only checks the
// guarded allowlist: every entry must pass isAllowlistedAIAction, and the agent
// supplies each action's target (which tag/task/deal) as a tool argument at run
// time, so no pinned per-action config is required here.
func validateAIStepConfig(raw json.RawMessage) error {
	ai := parseAIConfig(raw)
	if strings.TrimSpace(ai.Instruction) == "" {
		return fmt.Errorf("an AI step needs an instruction")
	}
	switch strings.TrimSpace(ai.Mode) {
	case "classify":
		if len(nonEmptyStrings(ai.Labels)) < 2 {
			return fmt.Errorf("classify mode needs at least two labels")
		}
	case "extract":
		if len(nonEmptyStrings(ai.OutputKeys)) == 0 {
			return fmt.Errorf("extract mode needs at least one output key")
		}
	case "agent":
		enabled := nonEmptyStrings(ai.Allowlist)
		if len(enabled) == 0 {
			return fmt.Errorf("agent mode needs at least one allowed action")
		}
		for _, raw2 := range enabled {
			id := models.IntegrationAction(strings.TrimSpace(raw2))
			if !isAllowlistedAIAction(id) {
				return fmt.Errorf("%q is not an allowed agent action", raw2)
			}
		}
	case "generate", "":
		// generate needs only the instruction (already checked).
	default:
		return fmt.Errorf("unknown AI step mode %q", ai.Mode)
	}
	return nil
}

// parseUUIDList parses a slice of string ids into uuids, dropping any that don't
// parse. Used by the label_email action's category list.
func parseUUIDList(ids []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	for _, s := range ids {
		if id, err := uuid.Parse(strings.TrimSpace(s)); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// NativeActions runs Warmbly-internal CRM/contact mutations for automation
// action nodes (no external connection). It's a consumer-side interface so the
// integration package needs no import of advanced/tasks/repository — a thin
// adapter in cmd/backend satisfies it (converting *errx.Error -> error and
// resolving the contact + org owner). Mirrors the AutomationRunner pattern.
type NativeActions interface {
	// ResolveContact finds the contact the action operates on, by id then email.
	ResolveContact(ctx context.Context, orgID uuid.UUID, contactID, email string) (*models.Contact, error)
	// OrgOwner returns the organization owner's user id (the actor for created
	// deals/tasks, which require a creator).
	OrgOwner(ctx context.Context, orgID uuid.UUID) (uuid.UUID, error)
	AddTag(ctx context.Context, orgID, actorID, contactID, categoryID uuid.UUID) error
	RemoveTag(ctx context.Context, orgID, actorID, contactID, categoryID uuid.UUID) error
	CreateTask(ctx context.Context, orgID, createdBy uuid.UUID, data *models.CreateCRMTask) error
	CreateDeal(ctx context.Context, orgID, createdBy uuid.UUID, data *models.CreateDeal) error
	MoveDealStage(ctx context.Context, orgID, contactID, pipelineID, stageID uuid.UUID) error
	Unsubscribe(ctx context.Context, campaignID, contactID uuid.UUID) error
	// LabelThread additively applies unibox conversation labels to a thread, on
	// behalf of the mailbox-owner userID (categories are per user). Backs the
	// "label_email" action; userID + threadID come from the reply event data.
	LabelThread(ctx context.Context, orgID uuid.UUID, threadID string, categoryIDs []uuid.UUID) error

	// UpsertContact creates the contact or enriches the one already holding
	// its email (the same write the contacts API does), owned by actorID.
	UpsertContact(ctx context.Context, orgID, actorID uuid.UUID, in models.AddContact) (*models.Contact, error)
	// AddToCampaign enrols an existing contact in a campaign and wakes it.
	AddToCampaign(ctx context.Context, orgID, actorID, contactID, campaignID uuid.UUID) error
	// KeepCampaignRunning turns on a campaign's "Keep running for new leads"
	// setting because an automation now feeds it leads.
	KeepCampaignRunning(ctx context.Context, orgID, campaignID uuid.UUID, reason string) error

	// ListCategories / CreateCategory / ListPipelines back the AI agent step's
	// argument-based tools: the model picks a tag/label/pipeline by name and the
	// executor resolves it live (empty pool = any of the workspace's tags). All
	// three are org-scoped; pipelines hydrate their stages in position order.
	// Mirrors the campaign agent tools.
	ListCategories(ctx context.Context, orgID uuid.UUID) ([]models.MiniCategory, error)
	CreateCategory(ctx context.Context, orgID uuid.UUID, title, color string) (models.MiniCategory, error)
	ListPipelines(ctx context.Context, orgID uuid.UUID) ([]models.Pipeline, error)
}

// nativeActionConfig is the per-node config for native action nodes (mirrors the
// relevant subset of the campaign ActionConfig keys, stored in the node config).
type nativeActionConfig struct {
	CategoryID         string   `json:"category_id"`
	DealPipelineID     string   `json:"deal_pipeline_id"`
	DealStageID        string   `json:"deal_stage_id"`
	DealName           string   `json:"deal_name"`
	DealValue          *float64 `json:"deal_value"`
	DealCurrency       string   `json:"deal_currency"`
	TaskTitle          string   `json:"task_title"`
	TaskType           string   `json:"task_type"`
	TaskPriority       string   `json:"task_priority"`
	TaskDueOffsetDays  *int     `json:"task_due_offset_days"`
	TaskAssignedTo     string   `json:"task_assigned_to"`
	TaskAssignedTeamID string   `json:"task_assigned_team_id"`
	// run_automation: the automation to launch.
	AutomationID string `json:"automation_id"`
	// label_email: the unibox conversation labels to apply (category-registry ids).
	LabelIDs []string `json:"label_ids"`
	// set_variables: named values computed from templates and written back into
	// the event data for later nodes to reuse.
	SetVars []setVar `json:"set_vars"`
	// fire_event: a developer-defined custom event published to the realtime
	// gateway. EventName + each field value are Go-templated against the event data.
	EventName   string   `json:"event_name"`
	EventFields []setVar `json:"event_fields"`
	// upsert_contact: templated contact fields, the tags and campaign the
	// contact lands in, and what to do when the email already exists.
	Email        string   `json:"email"`
	FirstName    string   `json:"first_name"`
	LastName     string   `json:"last_name"`
	Company      string   `json:"company"`
	Phone        string   `json:"phone"`
	CustomFields []setVar `json:"custom_fields"`
	CategoryIDs  []string `json:"category_ids"`
	SegmentIDs   []string `json:"segment_ids"`
	IfExists     string   `json:"if_exists"`
	// campaign_id is shared by upsert_contact (optional) and add_to_campaign
	// (required).
	CampaignID string `json:"campaign_id"`
}

// Values of nativeActionConfig.IfExists for upsert_contact.
const (
	upsertIfExistsUpdate = "update"
	upsertIfExistsSkip   = "skip"
)

// validateUpsertContactConfig checks the lead-intake action at write time: an
// email template is the one thing it cannot do without, and every id it
// references must at least parse.
func validateUpsertContactConfig(cfg nativeActionConfig) error {
	if strings.TrimSpace(cfg.Email) == "" {
		return fmt.Errorf("a create-or-update-contact action needs an email")
	}
	switch strings.TrimSpace(cfg.IfExists) {
	case "", upsertIfExistsUpdate, upsertIfExistsSkip:
	default:
		return fmt.Errorf("unknown if_exists value %q", cfg.IfExists)
	}
	if id := strings.TrimSpace(cfg.CampaignID); id != "" {
		if _, err := uuid.Parse(id); err != nil {
			return fmt.Errorf("the campaign id is not valid")
		}
	}
	for _, raw := range append(append([]string{}, cfg.CategoryIDs...), cfg.SegmentIDs...) {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if _, err := uuid.Parse(strings.TrimSpace(raw)); err != nil {
			return fmt.Errorf("a tag or segment id is not valid")
		}
	}
	return nil
}

// buildUpsertContact renders the action's templates against the event data
// into the contact to write. A blank rendered field is left empty so the
// upsert's enrich-never-erase rule keeps whatever the contact already has.
func buildUpsertContact(a models.Automation, cfg nativeActionConfig, data map[string]any) (models.AddContact, error) {
	email := strings.ToLower(strings.TrimSpace(renderTemplate(cfg.Email, data)))
	if email == "" || !strings.Contains(email, "@") {
		return models.AddContact{}, fmt.Errorf("the email rendered empty or invalid (%q)", truncate(email, 80))
	}
	custom := map[string]string{}
	for _, f := range cfg.CustomFields {
		key := strings.TrimSpace(f.Key)
		if key == "" {
			continue
		}
		if v := strings.TrimSpace(renderTemplate(f.Value, data)); v != "" {
			custom[key] = v
		}
	}
	in := models.AddContact{
		Email:        email,
		FirstName:    strings.TrimSpace(renderTemplate(cfg.FirstName, data)),
		LastName:     strings.TrimSpace(renderTemplate(cfg.LastName, data)),
		Company:      strings.TrimSpace(renderTemplate(cfg.Company, data)),
		Phone:        strings.TrimSpace(renderTemplate(cfg.Phone, data)),
		CustomFields: custom,
		Categories:   uuidStrings(cfg.CategoryIDs),
		Segments:     uuidStrings(cfg.SegmentIDs),
		Source:       models.ContactSourceAutomation,
		SourceDetail: a.Name,
	}
	if id := strings.TrimSpace(cfg.CampaignID); id != "" {
		in.Campaigns = []string{id}
	}
	return in, nil
}

// fedCampaignIDs lists the campaigns an automation enrols leads in: every
// add-to-campaign node and every create-or-update-contact node with a campaign.
func fedCampaignIDs(a *models.Automation) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := []uuid.UUID{}
	for _, n := range a.Graph.Nodes {
		if n.Type != "action" {
			continue
		}
		if n.Action != models.IntegrationActionAddToCampaign && n.Action != models.IntegrationActionUpsertContact {
			continue
		}
		id, err := uuid.Parse(strings.TrimSpace(parseNativeConfig(n.Config).CampaignID))
		if err != nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// keepFedCampaignsRunning precedes an automation save: a campaign an
// automation feeds must wait for leads instead of finishing between runs,
// exactly as a linked segment or a form does. A failure fails the save, so a
// campaign that is not this organization's, or a flip that did not land, is
// never hidden behind a successful response.
func (s *service) keepFedCampaignsRunning(ctx context.Context, a *models.Automation) error {
	if s.native == nil || a == nil {
		return nil
	}
	for _, id := range fedCampaignIDs(a) {
		reason := "the automation \"" + a.Name + "\" adds its leads to this campaign"
		if err := s.native.KeepCampaignRunning(ctx, a.OrganizationID, id, reason); err != nil {
			return fmt.Errorf("the campaign this automation adds leads to could not be kept running: %w", err)
		}
	}
	return nil
}

// uuidStrings keeps the entries of a saved id list that parse, trimmed.
func uuidStrings(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range parseUUIDList(ids) {
		out = append(out, id.String())
	}
	return out
}

type setVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func parseNativeConfig(raw json.RawMessage) nativeActionConfig {
	var c nativeActionConfig
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &c)
	}
	return c
}

// execNativeAction resolves the contact from the event data and runs the native
// CRM/contact mutation. Returns an error (recorded in run history); never panics.
func (s *service) execNativeAction(ctx context.Context, a models.Automation, n models.AutomationNode, data map[string]any) error {
	if s.native == nil {
		return fmt.Errorf("native actions are not available")
	}
	cfg := parseNativeConfig(n.Config)

	// run_automation launches another automation against the same event data; it
	// does not need a resolved contact, so handle it before contact resolution.
	// The chain-depth guard in RunAutomationByID bounds recursion/compute.
	if n.Action == models.IntegrationActionRunAutomation {
		targetID, perr := uuid.Parse(strings.TrimSpace(cfg.AutomationID))
		if perr != nil {
			return fmt.Errorf("run-automation needs a target automation")
		}
		if targetID == a.ID {
			return fmt.Errorf("an automation cannot run itself")
		}
		return s.RunAutomationByID(ctx, a.OrganizationID, targetID, data)
	}

	// set_variables computes named values from templates and writes them back
	// into the event data for later nodes. No contact, no external call.
	if n.Action == models.IntegrationActionSetVariables {
		for _, v := range cfg.SetVars {
			key := strings.TrimSpace(v.Key)
			if key == "" {
				continue
			}
			data[key] = renderTemplate(v.Value, data)
		}
		return nil
	}

	// fire_event publishes a developer-defined custom event to the realtime
	// gateway (org-scoped). Subscribers receive it over the websocket with no
	// public URL. Name + each field value are templated against the event data.
	if n.Action == models.IntegrationActionFireEvent {
		name := strings.TrimSpace(renderTemplate(cfg.EventName, data))
		if name == "" {
			return fmt.Errorf("fire-event needs an event name")
		}
		payload := make(map[string]string, len(cfg.EventFields))
		for _, f := range cfg.EventFields {
			key := strings.TrimSpace(f.Key)
			if key == "" {
				continue
			}
			payload[key] = renderTemplate(f.Value, data)
		}
		if s.publisher != nil {
			s.publisher.PublishCustomEvent(ctx, a.OrganizationID, uuid.Nil, name, payload, "automation", a.ID.String())
		}
		return nil
	}

	// label_email tags the conversation the event belongs to; it needs the
	// thread (carried by reply triggers), not a resolved contact.
	if n.Action == models.IntegrationActionLabelEmail {
		threadID := stringFromMap(data, "thread_id")
		if threadID == "" {
			return fmt.Errorf("label-email needs a reply thread (use it on a reply trigger)")
		}
		catIDs := parseUUIDList(cfg.LabelIDs)
		if len(catIDs) == 0 {
			return fmt.Errorf("a label action needs at least one label")
		}
		return s.native.LabelThread(ctx, a.OrganizationID, threadID, catIDs)
	}

	contactID := stringFromMap(data, "contact_id")
	email := stringFromMap(data, "contact_email", "invitee_email", "email")

	// upsert_contact is the one action that may run with no contact yet: it
	// makes one from the event. The written contact becomes the event's
	// contact so the nodes after it (tag, task, deal) find it.
	if n.Action == models.IntegrationActionUpsertContact {
		in, berr := buildUpsertContact(a, cfg, data)
		if berr != nil {
			return berr
		}
		if strings.TrimSpace(cfg.IfExists) == upsertIfExistsSkip {
			existing, rerr := s.native.ResolveContact(ctx, a.OrganizationID, "", in.Email)
			if rerr != nil {
				return fmt.Errorf("look up the existing contact: %w", rerr)
			}
			if existing != nil {
				data["contact_id"] = existing.ID.String()
				data["contact_email"] = existing.Email
				data["contact_created"] = false
				return nil
			}
		}
		owner, oerr := s.native.OrgOwner(ctx, a.OrganizationID)
		if oerr != nil {
			return oerr
		}
		written, werr := s.native.UpsertContact(ctx, a.OrganizationID, owner, in)
		if werr != nil {
			return werr
		}
		if written == nil {
			return fmt.Errorf("the contact write returned nothing")
		}
		data["contact_id"] = written.ID.String()
		data["contact_email"] = written.Email
		data["contact_created"] = written.IsNew
		return nil
	}

	c, err := s.native.ResolveContact(ctx, a.OrganizationID, contactID, email)
	if err != nil {
		return fmt.Errorf("look up the event's contact: %w", err)
	}
	if c == nil {
		return fmt.Errorf("no contact matched the event (need contact_id or contact_email)")
	}

	switch n.Action {
	case models.IntegrationActionAddToCampaign:
		campID, perr := uuid.Parse(strings.TrimSpace(cfg.CampaignID))
		if perr != nil {
			return fmt.Errorf("an add-to-campaign action needs a campaign")
		}
		owner, oerr := s.native.OrgOwner(ctx, a.OrganizationID)
		if oerr != nil {
			return oerr
		}
		return s.native.AddToCampaign(ctx, a.OrganizationID, owner, c.ID, campID)

	case models.IntegrationActionUnsubscribe:
		campID, perr := uuid.Parse(stringFromMap(data, "campaign_id"))
		if perr != nil {
			return fmt.Errorf("unsubscribe needs a campaign_id in the event data")
		}
		return s.native.Unsubscribe(ctx, campID, c.ID)

	case models.IntegrationActionAddTag, models.IntegrationActionRemoveTag:
		catID, perr := uuid.Parse(cfg.CategoryID)
		if perr != nil {
			return fmt.Errorf("a tag action needs a tag")
		}
		owner, oerr := s.native.OrgOwner(ctx, a.OrganizationID)
		if oerr != nil {
			return oerr
		}
		if n.Action == models.IntegrationActionAddTag {
			return s.native.AddTag(ctx, a.OrganizationID, owner, c.ID, catID)
		}
		return s.native.RemoveTag(ctx, a.OrganizationID, owner, c.ID, catID)

	case models.IntegrationActionMoveDealStage:
		pid, e1 := uuid.Parse(cfg.DealPipelineID)
		sid, e2 := uuid.Parse(cfg.DealStageID)
		if e1 != nil || e2 != nil {
			return fmt.Errorf("move-deal-stage needs a pipeline and stage")
		}
		return s.native.MoveDealStage(ctx, a.OrganizationID, c.ID, pid, sid)

	case models.IntegrationActionCreateDeal:
		pid, e1 := uuid.Parse(cfg.DealPipelineID)
		sid, e2 := uuid.Parse(cfg.DealStageID)
		if e1 != nil || e2 != nil {
			return fmt.Errorf("create-deal needs a pipeline and stage")
		}
		owner, oerr := s.native.OrgOwner(ctx, a.OrganizationID)
		if oerr != nil {
			return oerr
		}
		name := renderTemplate(cfg.DealName, data)
		if name == "" {
			name = "Deal: " + c.Email
		}
		currency := cfg.DealCurrency
		if currency == "" {
			currency = "USD"
		}
		cid := c.ID
		return s.native.CreateDeal(ctx, a.OrganizationID, owner, &models.CreateDeal{
			PipelineID: pid,
			StageID:    sid,
			ContactID:  &cid,
			Name:       name,
			Value:      cfg.DealValue,
			Currency:   currency,
		})

	case models.IntegrationActionCreateTask:
		owner, oerr := s.native.OrgOwner(ctx, a.OrganizationID)
		if oerr != nil {
			return oerr
		}
		title := renderTemplate(cfg.TaskTitle, data)
		if title == "" {
			title = "Follow up: " + c.Email
		}
		cid := c.ID
		task := &models.CreateCRMTask{
			ContactID: &cid,
			Title:     title,
			Type:      cfg.TaskType,
			Priority:  cfg.TaskPriority,
		}
		// Assignee: a specific user, or a whole team, or (default) the org owner.
		if tid, perr := uuid.Parse(strings.TrimSpace(cfg.TaskAssignedTeamID)); perr == nil {
			task.AssignedTeamID = &tid
		}
		if uid, perr := uuid.Parse(strings.TrimSpace(cfg.TaskAssignedTo)); perr == nil {
			task.AssignedTo = &uid
		} else if task.AssignedTeamID == nil {
			task.AssignedTo = &owner
		}
		if cfg.TaskDueOffsetDays != nil {
			due := time.Now().UTC().AddDate(0, 0, *cfg.TaskDueOffsetDays)
			task.DueDate = &due
		}
		return s.native.CreateTask(ctx, a.OrganizationID, owner, task)
	}
	return fmt.Errorf("unknown native action: %s", n.Action)
}

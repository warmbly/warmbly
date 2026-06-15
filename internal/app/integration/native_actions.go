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
	LabelThread(ctx context.Context, userID uuid.UUID, threadID string, categoryIDs []uuid.UUID) error
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
	// thread + mailbox owner (carried by reply triggers), not a resolved contact.
	if n.Action == models.IntegrationActionLabelEmail {
		threadID := stringFromMap(data, "thread_id")
		ownerID, perr := uuid.Parse(stringFromMap(data, "_user_id"))
		if threadID == "" || perr != nil {
			return fmt.Errorf("label-email needs a reply thread (use it on a reply trigger)")
		}
		catIDs := parseUUIDList(cfg.LabelIDs)
		if len(catIDs) == 0 {
			return fmt.Errorf("a label action needs at least one label")
		}
		return s.native.LabelThread(ctx, ownerID, threadID, catIDs)
	}

	contactID := stringFromMap(data, "contact_id")
	email := stringFromMap(data, "contact_email", "invitee_email", "email")

	c, err := s.native.ResolveContact(ctx, a.OrganizationID, contactID, email)
	if err != nil || c == nil {
		return fmt.Errorf("no contact matched the event (need contact_id or contact_email)")
	}

	switch n.Action {
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

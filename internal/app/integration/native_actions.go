package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	}
	return nil
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
}

// nativeActionConfig is the per-node config for native action nodes (mirrors the
// relevant subset of the campaign ActionConfig keys, stored in the node config).
type nativeActionConfig struct {
	CategoryID     string   `json:"category_id"`
	DealPipelineID string   `json:"deal_pipeline_id"`
	DealStageID    string   `json:"deal_stage_id"`
	DealName       string   `json:"deal_name"`
	DealValue      *float64 `json:"deal_value"`
	DealCurrency   string   `json:"deal_currency"`
	TaskTitle      string   `json:"task_title"`
	TaskType       string   `json:"task_type"`
	TaskPriority   string   `json:"task_priority"`
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
		assignee := owner
		return s.native.CreateTask(ctx, a.OrganizationID, owner, &models.CreateCRMTask{
			ContactID:  &cid,
			Title:      title,
			Type:       cfg.TaskType,
			Priority:   cfg.TaskPriority,
			AssignedTo: &assignee,
		})
	}
	return fmt.Errorf("unknown native action: %s", n.Action)
}

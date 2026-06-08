package models

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SyncDirection is the data-flow direction for a connection or field mapping.
type SyncDirection string

const (
	SyncDirectionPush SyncDirection = "push" // Warmbly -> provider (the default)
	SyncDirectionPull SyncDirection = "pull" // provider -> Warmbly (Phase Later)
	SyncDirectionBoth SyncDirection = "both"
)

// FieldTransform names a value transform applied while projecting a Warmbly
// field onto a provider field.
type FieldTransform string

const (
	FieldTransformNone      FieldTransform = "none"
	FieldTransformStatic    FieldTransform = "static" // ignore source; write StaticValue
	FieldTransformUppercase FieldTransform = "uppercase"
	FieldTransformLowercase FieldTransform = "lowercase"
	FieldTransformTrim      FieldTransform = "trim"
)

// WritePolicy controls create-vs-update behaviour for an upsert action.
type WritePolicy string

const (
	WritePolicyCreateUpdate WritePolicy = "create_update" // upsert (default)
	WritePolicyCreateOnly   WritePolicy = "create_only"
	WritePolicyUpdateOnly   WritePolicy = "update_only"
)

// FieldMapEntry maps one Warmbly source field to one provider destination field.
// A "custom:<key>" WarmblyField reads from the contact's custom_fields map.
type FieldMapEntry struct {
	WarmblyField  string         `json:"warmbly_field"`
	ExternalField string         `json:"external_field"`
	Transform     FieldTransform `json:"transform,omitempty"`
	StaticValue   string         `json:"static_value,omitempty"`
}

// OwnerAssignment describes how a created CRM record's owner is chosen.
type OwnerAssignment struct {
	Mode  string `json:"mode,omitempty"`  // "" | "fixed" | "provider" (provider-side rules)
	Value string `json:"value,omitempty"` // provider owner/user id when Mode == "fixed"
}

// AutomationConfig is the typed view over the integration_event_subscriptions
// .config jsonb. It is free-form, evolving, read-then-execute config, validated
// at the app boundary on write (per CLAUDE.md jsonb guidance). Every field is
// optional so notification-only automations and legacy rows stay valid.
type AutomationConfig struct {
	// Routing / filters.
	Intents       []string `json:"intents,omitempty"`        // e.g. ["positive"]
	MinConfidence float64  `json:"min_confidence,omitempty"` // 0..1

	// Destination (notifications).
	Channel string `json:"channel,omitempty"`
	URL     string `json:"url,omitempty"`

	// CRM write policy.
	TargetObject      string      `json:"target_object,omitempty"` // "contact" | "deal" | "lead"
	DedupeKey         string      `json:"dedupe_key,omitempty"`    // "email" | "domain"
	WritePolicy       WritePolicy `json:"write_policy,omitempty"`
	OverwriteNonEmpty bool        `json:"overwrite_non_empty,omitempty"`

	// CRM pickers (provider ids).
	PipelineID string          `json:"pipeline_id,omitempty"`
	StageID    string          `json:"stage_id,omitempty"`
	Owner      OwnerAssignment `json:"owner,omitempty"`

	// Inline field map (overrides connection defaults for this automation).
	FieldMap []FieldMapEntry `json:"field_map,omitempty"`

	// Notification message template ("{{contact_email}} replied to {{subject}}").
	MessageTemplate string `json:"message_template,omitempty"`
}

// ParseAutomationConfig unmarshals a subscription's jsonb config. An empty blob
// yields the zero config (a valid, no-op-filter automation).
func ParseAutomationConfig(raw json.RawMessage) (AutomationConfig, error) {
	cfg := AutomationConfig{}
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Validate checks an AutomationConfig against the provider capability descriptor
// for the chosen action. Errors are human-readable (suitable for a 400). When pc
// is nil (provider has no descriptor) only the shape is checked.
func (c AutomationConfig) Validate(pc *ProviderCapability, action IntegrationAction) error {
	if c.MinConfidence < 0 || c.MinConfidence > 1 {
		return fmt.Errorf("min_confidence must be between 0 and 1")
	}
	switch c.WritePolicy {
	case "", WritePolicyCreateUpdate, WritePolicyCreateOnly, WritePolicyUpdateOnly:
	default:
		return fmt.Errorf("invalid write_policy %q", c.WritePolicy)
	}
	for _, fm := range c.FieldMap {
		if fm.ExternalField == "" {
			return fmt.Errorf("a field mapping is missing its destination field")
		}
		if fm.Transform == FieldTransformStatic {
			if fm.StaticValue == "" {
				return fmt.Errorf("the static mapping for %q needs a value", fm.ExternalField)
			}
		} else if fm.WarmblyField == "" {
			return fmt.Errorf("the mapping for %q is missing a Warmbly field", fm.ExternalField)
		}
	}
	if pc == nil {
		return nil
	}
	act := pc.Action(action)
	if act == nil {
		return nil // unknown action is rejected by the executor, not here
	}
	if obj := pc.Object(act.Object); obj != nil {
		mapped := map[string]bool{}
		for _, fm := range c.FieldMap {
			mapped[fm.ExternalField] = true
		}
		for _, req := range obj.Required {
			if !mapped[req] && !defaultCovers(req) {
				return fmt.Errorf("%s needs a value mapped for %q", act.Label, req)
			}
		}
	}
	if act.NeedsPipeline && c.PipelineID == "" {
		return fmt.Errorf("%s needs a pipeline selected", act.Label)
	}
	return nil
}

// defaultCovers reports whether a required provider field is satisfied by the
// implicit default projection (email/name/company/phone) when left unmapped, so
// a user isn't forced to hand-map fields that already map by name.
func defaultCovers(field string) bool {
	switch strings.ToLower(field) {
	case "email", "lastname", "last_name", "name", "firstname", "first_name", "company":
		return true
	}
	return false
}

package models

// ProviderCapability is the static, code-defined descriptor the dashboard renders
// the onboarding + settings UI from, and that AutomationConfig.Validate checks
// against. Adding support for a new provider action is "add a descriptor entry +
// a handler", with no bespoke per-provider React. The descriptor only lists what
// Warmbly can actually execute today, so the UI never offers a dead action.
type ProviderCapability struct {
	Provider   IntegrationProvider `json:"provider"`
	Directions []SyncDirection     `json:"directions"`
	Objects    []CapabilityObject  `json:"objects,omitempty"`
	Actions    []CapabilityAction  `json:"actions,omitempty"`
	Pickers    []CapabilityPicker  `json:"pickers,omitempty"`

	// SupportsBookingLink is true for scheduling providers (Calendly, Cal.com):
	// the connection stores a scheduling_url that contextual "Book a call"
	// buttons open, prefilled with the contact's email/name.
	SupportsBookingLink bool `json:"supports_booking_link"`
}

// FieldDef is one selectable field in the field-map editor (either a Warmbly
// source field or a provider destination field).
type FieldDef struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// CapabilityObject describes one provider object Warmbly can write (contact,
// person, lead, deal). WarmblyFields are the source fields a user can map FROM;
// ExternalFields are the provider destination fields they can map TO. When
// DynamicFields is true the destination list can be augmented by live discovery
// (Phase Later); today the static ExternalFields cover the common case.
type CapabilityObject struct {
	Name           string     `json:"name"`
	Label          string     `json:"label"`
	DedupeKeys     []string   `json:"dedupe_keys"`
	Required       []string   `json:"required"`
	WarmblyFields  []FieldDef `json:"warmbly_fields"`
	ExternalFields []FieldDef `json:"external_fields"`
	DynamicFields  bool       `json:"dynamic_fields"`
}

// CapabilityAction is a descriptor-level action that maps 1:1 to an
// IntegrationAction handler.
type CapabilityAction struct {
	ID            IntegrationAction `json:"id"`
	Label         string            `json:"label"`
	Description   string            `json:"description"`
	Object        string            `json:"object,omitempty"`
	NeedsPipeline bool              `json:"needs_pipeline,omitempty"`
	NeedsChannel  bool              `json:"needs_channel,omitempty"`
	NeedsURL      bool              `json:"needs_url,omitempty"`
}

// CapabilityPicker is a value selector the UI renders (channel, pipeline, stage,
// owner). Endpoint is the discovery route key (empty = manual entry for now).
type CapabilityPicker struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Endpoint  string `json:"endpoint,omitempty"`
	DependsOn string `json:"depends_on,omitempty"`
}

// Action returns the descriptor for an action id, or nil.
func (p *ProviderCapability) Action(id IntegrationAction) *CapabilityAction {
	for i := range p.Actions {
		if p.Actions[i].ID == id {
			return &p.Actions[i]
		}
	}
	return nil
}

// Object returns the descriptor for an object name, or nil.
func (p *ProviderCapability) Object(name string) *CapabilityObject {
	for i := range p.Objects {
		if p.Objects[i].Name == name {
			return &p.Objects[i]
		}
	}
	return nil
}

// warmblyContactFields are the Warmbly contact source fields offered in the
// field-map editor across every CRM provider.
func warmblyContactFields() []FieldDef {
	return []FieldDef{
		{Key: "email", Label: "Email"},
		{Key: "first_name", Label: "First name"},
		{Key: "last_name", Label: "Last name"},
		{Key: "name", Label: "Full name"},
		{Key: "company", Label: "Company"},
		{Key: "phone", Label: "Phone"},
	}
}

// CapabilityFor returns the descriptor for a provider, or nil if the provider
// has no configurable capability surface.
func CapabilityFor(provider IntegrationProvider) *ProviderCapability {
	caps := Capabilities()
	if c, ok := caps[provider]; ok {
		return &c
	}
	return nil
}

// Capabilities is the static descriptor registry. It only describes actions with
// a real execAction handler, so the dashboard never renders a dead control.
func Capabilities() map[IntegrationProvider]ProviderCapability {
	pushOnly := []SyncDirection{SyncDirectionPush}

	hubspotContact := CapabilityObject{
		Name: "contact", Label: "Contact",
		DedupeKeys: []string{"email"}, Required: []string{"email"},
		WarmblyFields: warmblyContactFields(),
		ExternalFields: []FieldDef{
			{Key: "email", Label: "Email"},
			{Key: "firstname", Label: "First name"},
			{Key: "lastname", Label: "Last name"},
			{Key: "company", Label: "Company"},
			{Key: "phone", Label: "Phone"},
			{Key: "jobtitle", Label: "Job title"},
			{Key: "website", Label: "Website"},
			{Key: "lifecyclestage", Label: "Lifecycle stage"},
		},
	}
	salesforceContact := CapabilityObject{
		Name: "contact", Label: "Contact",
		DedupeKeys: []string{"email"}, Required: []string{"LastName"},
		WarmblyFields: warmblyContactFields(),
		ExternalFields: []FieldDef{
			{Key: "Email", Label: "Email"},
			{Key: "FirstName", Label: "First name"},
			{Key: "LastName", Label: "Last name"},
			{Key: "Phone", Label: "Phone"},
			{Key: "Title", Label: "Title"},
		},
	}
	pipedrivePerson := CapabilityObject{
		Name: "person", Label: "Person",
		DedupeKeys: []string{"email"}, Required: []string{"name"},
		WarmblyFields: warmblyContactFields(),
		ExternalFields: []FieldDef{
			{Key: "name", Label: "Name"},
			{Key: "email", Label: "Email"},
			{Key: "phone", Label: "Phone"},
		},
	}
	closeLead := CapabilityObject{
		Name: "lead", Label: "Lead",
		DedupeKeys: []string{"email"}, Required: []string{"email"},
		WarmblyFields: warmblyContactFields(),
		ExternalFields: []FieldDef{
			{Key: "name", Label: "Contact name"},
			{Key: "email", Label: "Email"},
			{Key: "phone", Label: "Phone"},
			{Key: "company", Label: "Company / lead name"},
		},
	}

	return map[IntegrationProvider]ProviderCapability{
		IntegrationHubSpot: {
			Provider: IntegrationHubSpot, Directions: pushOnly,
			Objects: []CapabilityObject{hubspotContact},
			Actions: []CapabilityAction{{
				ID: IntegrationActionHubSpotUpsert, Label: "Create or update contact",
				Description: "Upsert a HubSpot contact (matched by email) and log a timeline note.",
				Object:      "contact",
			}},
		},
		IntegrationSalesforce: {
			Provider: IntegrationSalesforce, Directions: pushOnly,
			Objects: []CapabilityObject{salesforceContact},
			Actions: []CapabilityAction{{
				ID: IntegrationActionSalesforceUpsert, Label: "Create or update contact",
				Description: "Upsert a Salesforce Contact (matched by email).",
				Object:      "contact",
			}},
		},
		IntegrationPipedrive: {
			Provider: IntegrationPipedrive, Directions: pushOnly,
			Objects: []CapabilityObject{pipedrivePerson},
			Actions: []CapabilityAction{{
				ID: IntegrationActionPipedriveUpsert, Label: "Create or update person",
				Description: "Upsert a Pipedrive person (matched by email).",
				Object:      "person",
			}},
		},
		IntegrationClose: {
			Provider: IntegrationClose, Directions: pushOnly,
			Objects: []CapabilityObject{closeLead},
			Actions: []CapabilityAction{{
				ID: IntegrationActionCloseUpsert, Label: "Create or update lead",
				Description: "Upsert a Close lead with an embedded contact (matched by email).",
				Object:      "lead",
			}},
		},
		IntegrationSlack: {
			Provider: IntegrationSlack, Directions: pushOnly,
			Actions: []CapabilityAction{{
				ID: IntegrationActionSlackNotify, Label: "Send a Slack message",
				Description: "Post to a channel when the event fires.", NeedsChannel: true,
			}},
			Pickers: []CapabilityPicker{{Key: "channel", Label: "Channel"}},
		},
		IntegrationDiscord: {
			Provider: IntegrationDiscord, Directions: pushOnly,
			Actions: []CapabilityAction{{
				ID: IntegrationActionDiscordNotify, Label: "Send a Discord message",
				Description: "Post to the channel webhook when the event fires.", NeedsURL: true,
			}},
		},
		IntegrationZapier: automationCapability(IntegrationZapier),
		IntegrationMake:   automationCapability(IntegrationMake),
		IntegrationN8N:    automationCapability(IntegrationN8N),
		IntegrationCalendly: {
			Provider: IntegrationCalendly, Directions: pushOnly, SupportsBookingLink: true,
		},
		IntegrationCalCom: {
			Provider: IntegrationCalCom, Directions: pushOnly, SupportsBookingLink: true,
		},
	}
}

// automationCapability is the shared descriptor for generic outbound-webhook
// automation providers (Zapier / Make / n8n): a single "send webhook" action.
func automationCapability(provider IntegrationProvider) ProviderCapability {
	return ProviderCapability{
		Provider: provider, Directions: []SyncDirection{SyncDirectionPush},
		Actions: []CapabilityAction{{
			ID: IntegrationActionGenericWebhookPing, Label: "Send to a webhook",
			Description: "POST the event payload to your automation webhook.", NeedsURL: true,
		}},
	}
}

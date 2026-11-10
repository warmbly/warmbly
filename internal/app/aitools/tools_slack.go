package aitools

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// SlackControl is the Slack app's settings surface, implemented by slackapp.
type SlackControl interface {
	CurrentSettings(ctx context.Context, orgID uuid.UUID) (*models.SlackSettings, string, *errx.Error)
	UpdateSettings(ctx context.Context, orgID uuid.UUID, in models.SlackSettings) (*models.SlackSettings, *errx.Error)
}

// RegisterSlackTools adds the Slack settings tools once the Slack app exists;
// it is built after the registry because it runs the assistant through it.
// They carry the Slack panel's own gates: any member reads, Manage settings
// changes.
func RegisterSlackTools(r *Registry, slack SlackControl, audit audit.AuditService) {
	if r == nil || slack == nil {
		return
	}
	d := Deps{Slack: slack, Audit: audit}

	r.Register(Tool{
		Name:            "get_slack_settings",
		Description:     "Read how Warmbly is set up in the connected Slack workspace: the default notification channel, per-category channels, the inbox channel and what it posts, and whether the assistant answers everywhere, only in DMs, or not at all. Channels are Slack channel ids; mention one as <#ID>.",
		InputSchema:     objectSchema(map[string]any{}),
		Risk:            generation.RiskRead,
		JWTOnly:         true,
		RequiredOrgPerm: 0,
		Handler:         d.getSlackSettings,
	})

	r.Register(Tool{
		Name: "update_slack_settings",
		Description: "Change how Warmbly is set up in Slack. Only the fields you pass change. A channel is a Slack channel id (from the conversation, or from get_slack_settings) or a #name; an empty string clears it. " +
			"Notification categories: " + strings.Join(slackCategoryNames(), ", ") + ". " +
			"The inbox channel receives the unified inbox, one Slack thread per conversation, and everyone in it can read that mail: confirm the user means it.",
		InputSchema: objectSchema(map[string]any{
			"notification_channel": strProp("Default channel for every notification category without its own channel."),
			"category_channels":    objProp("Map of notification category to channel. An empty string sends that category back to the default channel."),
			"inbox_channel":        strProp("Channel the unified inbox is posted to (a channel id). Empty turns the inbox off."),
			"inbox_scope":          enumProp("What the inbox channel posts.", "replies", "all"),
			"assistant":            enumProp("Where the assistant answers in Slack.", "on", "dm_only", "off"),
		}),
		Risk:            generation.RiskWrite,
		JWTOnly:         true,
		RequiredOrgPerm: models.PermManageSettings,
		Handler:         d.updateSlackSettings,
	})
}

func slackCategoryNames() []string {
	return []string{
		string(models.NotifInboundReply), string(models.NotifInboundPositiveReply), string(models.NotifInboundOOO), string(models.NotifInboxActionRequired),
		string(models.NotifCampaignPaused), string(models.NotifHealthBounce), string(models.NotifHealthComplaint),
		string(models.NotifDomainAuth), string(models.NotifPlacementFinished), string(models.NotifPlacementAlert),
		string(models.NotifWorkerDowntime), string(models.NotifSecuritySignIn), string(models.NotifBillingAlert), string(models.NotifTeamActivity),
	}
}

type slackSettingsView struct {
	SlackWorkspace      string            `json:"slack_workspace,omitempty"`
	NotificationChannel string            `json:"notification_channel"`
	CategoryChannels    map[string]string `json:"category_channels"`
	InboxChannel        string            `json:"inbox_channel"`
	InboxScope          string            `json:"inbox_scope"`
	Assistant           string            `json:"assistant"`
}

func slackView(st *models.SlackSettings, team string) slackSettingsView {
	v := slackSettingsView{
		SlackWorkspace: team, NotificationChannel: st.Channel, InboxChannel: st.InboxChannel,
		InboxScope: st.InboxScope, Assistant: "on", CategoryChannels: map[string]string{},
	}
	if v.InboxScope == "" {
		v.InboxScope = models.SlackInboxScopeReplies
	}
	switch {
	case st.AssistantDisabled:
		v.Assistant = "off"
	case st.AssistantDMOnly:
		v.Assistant = "dm_only"
	}
	for k, ch := range st.Routes {
		v.CategoryChannels[string(k)] = ch
	}
	return v
}

func (d Deps) getSlackSettings(ctx context.Context, inv Invocation, _ json.RawMessage) (string, error) {
	st, team, xerr := d.Slack.CurrentSettings(ctx, inv.OrgID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(slackView(st, team))
}

func (d Deps) updateSlackSettings(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		NotificationChannel *string           `json:"notification_channel"`
		CategoryChannels    map[string]string `json:"category_channels"`
		InboxChannel        *string           `json:"inbox_channel"`
		InboxScope          *string           `json:"inbox_scope"`
		Assistant           *string           `json:"assistant"`
	}](args)
	if err != nil {
		return "", err
	}
	cur, team, xerr := d.Slack.CurrentSettings(ctx, inv.OrgID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	next := *cur
	next.Routes = map[models.NotificationCategory]string{}
	for k, ch := range cur.Routes {
		next.Routes[k] = ch
	}
	if in.NotificationChannel != nil {
		next.Channel = *in.NotificationChannel
	}
	for k, ch := range in.CategoryChannels {
		if strings.TrimSpace(ch) == "" {
			delete(next.Routes, models.NotificationCategory(k))
		} else {
			next.Routes[models.NotificationCategory(k)] = ch
		}
	}
	if in.InboxChannel != nil {
		next.InboxChannel = *in.InboxChannel
	}
	if in.InboxScope != nil {
		next.InboxScope = *in.InboxScope
	}
	if in.Assistant != nil {
		switch *in.Assistant {
		case "on":
			next.AssistantDisabled, next.AssistantDMOnly = false, false
		case "dm_only":
			next.AssistantDisabled, next.AssistantDMOnly = false, true
		case "off":
			next.AssistantDisabled, next.AssistantDMOnly = true, false
		default:
			return "", ErrInvalidArgs
		}
	}
	out, xerr := d.Slack.UpdateSettings(ctx, inv.OrgID, next)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityIntegration, nil, map[string]string{"slack": "settings"})
	return jsonResult(slackView(out, team))
}

// Integrations: what is connected, and a link that opens the connect popup.
// Connecting itself needs the member in a browser, so the assistant hands
// over a link rather than doing it.
func (d Deps) registerIntegrationTools(r *Registry) {
	if d.Automations == nil {
		return
	}
	r.Register(Tool{
		Name:        "list_integrations",
		Description: "List the integrations this workspace has connected (with their status and account) and the ones available to connect.",
		InputSchema: objectSchema(map[string]any{}),
		Risk:        generation.RiskRead,
		JWTOnly:     true,
		Handler:     d.listIntegrations,
	})
	r.Register(Tool{
		Name:            "integration_connect_link",
		Description:     "Get a link that opens the connect window for a built-in integration (provider id from list_integrations, like slack, hubspot, salesforce, calendly). Connecting needs the user in a browser: give them the link.",
		InputSchema:     objectSchema(map[string]any{"provider": strProp("Integration provider id.")}, "provider"),
		Risk:            generation.RiskRead,
		JWTOnly:         true,
		RequiredOrgPerm: models.PermManageSettings,
		Handler:         d.integrationConnectLink,
	})
}

// canSeeIntegrations is the Integrations page's own gate: either permission.
func canSeeIntegrations(inv Invocation) bool {
	return inv.OrgPerms.HasPermission(models.PermManageSettings) || inv.OrgPerms.HasPermission(models.PermUseIntegrations)
}

func (d Deps) listIntegrations(ctx context.Context, inv Invocation, _ json.RawMessage) (string, error) {
	if !canSeeIntegrations(inv) {
		return "", ErrToolForbidden
	}
	conns, err := d.Automations.ListConnections(ctx, inv.OrgID)
	if err != nil {
		return "", errors.New("could not list integrations")
	}
	type connected struct {
		Provider string `json:"provider"`
		Label    string `json:"label"`
		Account  string `json:"account,omitempty"`
		Status   string `json:"status"`
	}
	type available struct {
		Provider string `json:"provider"`
		Name     string `json:"name"`
		Category string `json:"category"`
	}
	out := struct {
		Connected []connected `json:"connected"`
		Available []available `json:"available"`
	}{Connected: []connected{}, Available: []available{}}
	for _, c := range conns {
		if c.Status == models.IntegrationStatusDisconnected {
			continue
		}
		out.Connected = append(out.Connected, connected{Provider: string(c.Provider), Label: c.Label, Account: c.ExternalAccountName, Status: string(c.Status)})
	}
	for _, e := range d.Automations.Catalog(ctx) {
		// An app this instance has no credentials for cannot be connected.
		if !e.Configured {
			continue
		}
		out.Available = append(out.Available, available{Provider: string(e.Provider), Name: e.Name, Category: string(e.Category)})
	}
	return jsonResult(out)
}

func (d Deps) integrationConnectLink(ctx context.Context, _ Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Provider string `json:"provider"`
	}](args)
	if err != nil {
		return "", err
	}
	p := strings.TrimSpace(in.Provider)
	for _, e := range d.Automations.Catalog(ctx) {
		if string(e.Provider) == p {
			if !e.Configured {
				return "", errors.New(e.Name + " is not set up on this Warmbly instance yet: an admin has to add its app credentials first")
			}
			return jsonResult(map[string]string{
				"name": e.Name,
				"url":  d.link("/app/integrations/" + url.PathEscape(p) + "?connect=1"),
			})
		}
	}
	return "", errors.New("unknown integration: use a provider id from list_integrations")
}

// Package opsnotify delivers instance-wide operator alerts to the channels an
// admin configured in the settings document (Discord, Slack, a generic signed
// webhook, or email).
//
// This is the operator's channel, not the customer's. Customer-facing event
// delivery is internal/app/webhook, which is organization-scoped, queued and
// retried. Operator alerts are best-effort and fire-and-forget by design: an
// unreachable chat server must never fail or slow the request that triggered
// it, and a missed alert is not a correctness bug.
package opsnotify

// Event keys. Adding one means adding it to Catalog too, otherwise the admin
// panel cannot subscribe a channel to it.
const (
	EventEnterpriseInquiry     = "enterprise_inquiry.created"
	EventLimitRequest          = "limit_request.created"
	EventWarmupAppeal          = "warmup_appeal.created"
	EventOrganizationNew       = "organization.created"
	EventUserRegistered        = "user.registered"
	EventWorkerOffline         = "worker.offline"
	EventWarmupLoading         = "warmup.worker_loading"
	EventWarmupDispatchOverdue = "warmup.dispatch_overdue"
	EventOrgRisk               = "org_risk.escalated"
	EventSubscriptionIssue     = "subscription.payment_failed"
	EventAdminSignIn           = "admin.signed_in"
	EventAdminAccess           = "admin.access_changed"
	EventTest                  = "test"
)

// Severity drives the colour a chat transport renders.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityUrgent  Severity = "urgent"
)

// EventDef describes one subscribable event for the admin panel.
type EventDef struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Group       string   `json:"group"`
	Severity    Severity `json:"severity"`
	// SelfHostRelevant is false for events that only mean something on a
	// commercial deployment, so a self-hosted panel can hide them.
	SelfHostRelevant bool `json:"self_host_relevant"`
	// OptIn events reach only channels that tick them, never a catch-all one.
	OptIn bool `json:"opt_in"`
}

// Catalog is the inventory the admin panel renders. Declaration order is
// display order.
var Catalog = []EventDef{
	{Key: EventWarmupLoading, Group: "Operations", Label: "Persistent warmup loading failures", Description: "Repeated recent worker-loading failures span at least an hour without a later confirmed warmup send. Grouped by worker or fleet and deduplicated for six hours.", Severity: SeverityUrgent, SelfHostRelevant: true},
	{Key: EventWarmupDispatchOverdue, Group: "Operations", Label: "Warmup dispatch is overdue", Description: "Active mailboxes have warmup tasks over an hour past their intended schedule. Grouped by worker or fleet and deduplicated for six hours.", Severity: SeverityUrgent, SelfHostRelevant: true},
	{
		Key: EventEnterpriseInquiry, Group: "Sales",
		Label:       "Enterprise inquiry submitted",
		Description: "Someone asked for enterprise pricing from the plan chooser.",
		Severity:    SeverityInfo,
	},
	{
		Key: EventLimitRequest, Group: "Sales",
		Label:       "Limit increase requested",
		Description: "A workspace asked for more capacity than its plan allows.",
		Severity:    SeverityInfo, SelfHostRelevant: true,
	},
	{
		Key: EventSubscriptionIssue, Group: "Sales",
		Label:       "Subscription billing issue",
		Description: "A subscription invoice could not be finalized or paid, so sending may be at risk.",
		Severity:    SeverityWarning,
	},
	{
		Key: EventOrganizationNew, Group: "Growth",
		Label:       "Workspace created",
		Description: "A new organization was created on this instance.",
		Severity:    SeverityInfo, SelfHostRelevant: true,
	},
	{
		Key: EventUserRegistered, Group: "Growth",
		Label:       "User registered",
		Description: "A new account finished signing up.",
		Severity:    SeverityInfo, SelfHostRelevant: true,
	},
	{
		Key: EventWorkerOffline, Group: "Infrastructure",
		Label:       "Worker went offline",
		Description: "A worker stopped heartbeating, so its mailboxes cannot send until it returns or they are reassigned.",
		Severity:    SeverityUrgent, SelfHostRelevant: true,
	},
	{
		Key: EventAdminSignIn, Group: "Security",
		Label:       "Admin signed in",
		Description: "An account holding platform admin access started a new session, in the admin panel or the dashboard. Sent only to channels that tick it.",
		Severity:    SeverityInfo, SelfHostRelevant: true, OptIn: true,
	},
	{
		Key: EventAdminAccess, Group: "Security",
		Label:       "Admin access changed",
		Description: "Platform admin permissions were granted to, changed for, or revoked from an account in the admin panel.",
		Severity:    SeverityWarning, SelfHostRelevant: true,
	},
	{
		Key: EventWarmupAppeal, Group: "Abuse",
		Label:       "Warmup ban appealed",
		Description: "A blocked mailbox asked to be let back into the warmup pool.",
		Severity:    SeverityInfo, SelfHostRelevant: true,
	},
	{
		Key: EventOrgRisk, Group: "Abuse",
		Label:       "Workspace risk escalated",
		Description: "Risk scoring moved a workspace into a stricter posture.",
		Severity:    SeverityWarning, SelfHostRelevant: true,
	},
}

// Def returns the catalog entry for a key.
func Def(key string) (EventDef, bool) {
	for _, d := range Catalog {
		if d.Key == key {
			return d, true
		}
	}
	return EventDef{}, false
}

// Event is one alert on its way out.
type Event struct {
	Key      string
	Title    string
	Summary  string
	Severity Severity
	// Fields render as a small key/value table on chat transports and as the
	// payload body on the generic webhook.
	Fields []Field
	// Link is an absolute URL an operator can click, usually into the admin panel.
	Link string
}

// Field is one labelled value on an event.
type Field struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// NewEvent builds an event, defaulting its severity from the catalog.
func NewEvent(key, title, summary string, fields ...Field) Event {
	sev := SeverityInfo
	if d, ok := Def(key); ok {
		sev = d.Severity
	}
	return Event{Key: key, Title: title, Summary: summary, Severity: sev, Fields: fields}
}

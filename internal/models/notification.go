package models

import (
	"time"

	"github.com/google/uuid"
)

// NotificationCategory identifies a kind of in-app notification. The set is
// fixed (mirrors the settings UI); new categories are added here + in the
// preference struct + default merge.
type NotificationCategory string

const (
	NotifInboundReply    NotificationCategory = "inbound_reply"
	NotifInboundOOO      NotificationCategory = "inbound_out_of_office"
	NotifHealthBounce    NotificationCategory = "health_bounce"
	NotifHealthComplaint NotificationCategory = "health_complaint"
	NotifWorkerDowntime  NotificationCategory = "health_worker_downtime"
	NotifSecuritySignIn  NotificationCategory = "security_new_signin"
)

// ChannelPrefs is the per-category delivery toggles. Only InApp is delivered
// today across in-app, email, and a connected Slack workspace.
type ChannelPrefs struct {
	InApp bool `json:"in_app"`
	Email bool `json:"email"`
	Slack bool `json:"slack"`
}

// CategoryPref is the enable flag + channel toggles for one category.
type CategoryPref struct {
	Enabled  bool         `json:"enabled"`
	Channels ChannelPrefs `json:"channels"`
}

// NotificationPreferences is the per-user singleton (jsonb on users). Always
// returned fully populated via DefaultNotificationPreferences merge.
type NotificationPreferences struct {
	InboundReply    CategoryPref `json:"inbound_reply"`
	InboundOOO      CategoryPref `json:"inbound_out_of_office"`
	HealthBounce    CategoryPref `json:"health_bounce"`
	HealthComplaint CategoryPref `json:"health_complaint"`
	WorkerDowntime  CategoryPref `json:"health_worker_downtime"`
	SecuritySignIn  CategoryPref `json:"security_new_signin"`
}

// DefaultNotificationPreferences is the merge base. Health categories default ON
// (operationally important + low volume); inbound categories default OFF (a big
// campaign would otherwise flood the feed with a notification per recipient).
func DefaultNotificationPreferences() NotificationPreferences {
	on := CategoryPref{Enabled: true, Channels: ChannelPrefs{InApp: true}}
	off := CategoryPref{Enabled: false, Channels: ChannelPrefs{InApp: true}}
	return NotificationPreferences{
		InboundReply:    off,
		InboundOOO:      off,
		HealthBounce:    on,
		HealthComplaint: on,
		WorkerDowntime:  on,
		SecuritySignIn:  on,
	}
}

// CategoryPref returns the preference for a category (zero value if unknown).
func (p NotificationPreferences) CategoryPref(c NotificationCategory) CategoryPref {
	switch c {
	case NotifInboundReply:
		return p.InboundReply
	case NotifInboundOOO:
		return p.InboundOOO
	case NotifHealthBounce:
		return p.HealthBounce
	case NotifHealthComplaint:
		return p.HealthComplaint
	case NotifWorkerDowntime:
		return p.WorkerDowntime
	case NotifSecuritySignIn:
		return p.SecuritySignIn
	default:
		return CategoryPref{}
	}
}

// Notification is one row in the in-app feed.
type Notification struct {
	ID             uuid.UUID            `json:"id"`
	UserID         uuid.UUID            `json:"user_id"`
	OrganizationID *uuid.UUID           `json:"organization_id,omitempty"`
	Category       NotificationCategory `json:"category"`
	Title          string               `json:"title"`
	Body           string               `json:"body,omitempty"`
	Link           string               `json:"link,omitempty"`
	Metadata       map[string]any       `json:"metadata,omitempty"`
	ReadAt         *time.Time           `json:"read_at,omitempty"`
	CreatedAt      time.Time            `json:"created_at"`
}

// UpdateNotificationPreferencesRequest is the PUT payload.
type UpdateNotificationPreferencesRequest struct {
	Preferences NotificationPreferences `json:"preferences"`
}

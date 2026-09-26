package models

import (
	"strings"
	"time"
)

// Dashboard lists whose layout a member can save. A new list gets a constant
// here before the dashboard can persist a layout for it.
const (
	ViewContacts      = "contacts"
	ViewCampaignLeads = "campaign_leads"
)

// KnownViews is the set of view names GET/PUT /me/views/:view accepts.
var KnownViews = map[string]bool{
	ViewContacts:      true,
	ViewCampaignLeads: true,
}

// ViewBuiltinColumns lists the built-in column ids each view can render, in
// step with the dashboard's column registry (web/src/components/app/contacts/
// columns.tsx). A saved layout may name only these and custom fields.
var ViewBuiltinColumns = map[string][]string{
	ViewContacts:      {"name", "company", "phone", "mail_host", "status", "campaigns", "created_at", "updated_at"},
	ViewCampaignLeads: {"name", "company", "phone", "mail_host", "progress", "opened", "clicked", "replied", "current_step", "sender", "last_activity", "created_at", "updated_at"},
}

// ContactBuiltinSorts are the sort_by values the contacts search knows besides
// "custom:<key>", the same set contactSorts holds in the repository.
var ContactBuiltinSorts = map[string]bool{
	"created_at": true, "updated_at": true, "first_name": true, "last_name": true,
	"email": true, "company": true, "phone": true, "campaign_count": true, "mail_host": true,
}

// ViewPreferencesMaxColumns bounds one layout; no list here has anywhere near
// this many columns to show.
const ViewPreferencesMaxColumns = 64

// ViewColumnIDMaxLength fits "custom:" plus the longest custom-field key.
const ViewColumnIDMaxLength = 300

// ViewColumnCustomPrefix marks a column that shows a contact custom field, the
// same addressing the export uses ("custom:Industry").
const ViewColumnCustomPrefix = "custom:"

// ViewSort is the saved ordering of a list, in the contacts search's terms.
type ViewSort struct {
	By      string `json:"by"`
	Reverse bool   `json:"reverse"`
}

// ViewPreferences is one member's saved layout for one list in one workspace.
// An empty Columns means "the default layout"; a nil Sort means "the default
// sort".
type ViewPreferences struct {
	View      string     `json:"view"`
	Columns   []string   `json:"columns"`
	Sort      *ViewSort  `json:"sort,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// ViewPreferencesUpdate is a partial write: a nil field keeps what is saved,
// so a sort click made before the layout has loaded cannot erase the columns.
type ViewPreferencesUpdate struct {
	Columns *[]string
	Sort    *ViewSort
}

// ContactSortCustomPrefix is the sort_by form that orders the contacts list on
// a custom field: "custom:<key>". The key is normalized the way custom-field
// names are on write, so "custom:Company  Mobile" and "custom:Company Mobile"
// are the same sort.
const ContactSortCustomPrefix = "custom:"

// ContactSortCustomField returns the custom-field key a sort_by names, or
// false when sort_by is not the custom form. Whether the key is a valid
// custom-field name is the caller's check.
func ContactSortCustomField(sortBy string) (string, bool) {
	if !strings.HasPrefix(sortBy, ContactSortCustomPrefix) {
		return "", false
	}
	key := strings.Join(strings.Fields(strings.TrimPrefix(sortBy, ContactSortCustomPrefix)), " ")
	return key, true
}

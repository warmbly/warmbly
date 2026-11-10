package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/pkg/tmplvars"
)

// FormLinkMarkerRE matches the {{form_link:<public_id>}} marker; see tmplvars.FormLinkMarker.
var FormLinkMarkerRE = tmplvars.FormLinkMarker

// FormLink is a per-contact form URL ticket: the row id is the opaque token
// in ?t=, so possession of the link identifies the contact (same posture as
// tracked_links click tickets).
type FormLink struct {
	ID             uuid.UUID  `json:"id"`
	FormID         uuid.UUID  `json:"form_id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	ContactID      uuid.UUID  `json:"contact_id"`
	CampaignID     *uuid.UUID `json:"campaign_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// FormEventType is one step of the visitor funnel.
type FormEventType string

const (
	FormEventView   FormEventType = "view"
	FormEventStart  FormEventType = "start"
	FormEventPage   FormEventType = "page"
	FormEventSubmit FormEventType = "submit"
)

// Valid reports whether the value is one the database accepts.
func (t FormEventType) Valid() bool {
	switch t {
	case FormEventView, FormEventStart, FormEventPage, FormEventSubmit:
		return true
	}
	return false
}

// FormEvent is one funnel event, enriched server-side. The visitor IP is
// used for the country lookup and then discarded, never stored.
type FormEvent struct {
	OrganizationID uuid.UUID
	FormID         uuid.UUID
	Type           FormEventType
	VisitorKey     string
	PageIndex      int
	PagesTotal     int
	ContactID      *uuid.UUID
	CampaignID     *uuid.UUID
	ReferrerDomain string
	CountryCode    string
	Device         string
}

// FormStats is the analytics payload for one form over a date range.
type FormStats struct {
	Totals     FormStatsTotals         `json:"totals"`
	Daily      []FormStatsDay          `json:"daily"`
	Pages      []FormFunnelPage        `json:"pages"`
	Sources    []FormStatsBucket       `json:"sources"`
	Countries  []FormStatsBucket       `json:"countries"`
	Devices    []FormStatsBucket       `json:"devices"`
	Campaigns  []FormStatsBucket       `json:"campaigns"`
	Identified []FormIdentifiedVisitor `json:"identified"`
}

type FormStatsTotals struct {
	Views              int64   `json:"views"`
	Starts             int64   `json:"starts"`
	Submissions        int64   `json:"submissions"`
	CompletionRate     float64 `json:"completion_rate"`
	IdentifiedVisitors int64   `json:"identified_visitors"`
}

type FormStatsDay struct {
	Date        string `json:"date"`
	Views       int64  `json:"views"`
	Starts      int64  `json:"starts"`
	Submissions int64  `json:"submissions"`
}

// FormFunnelPage is one row of the page funnel: how many visitors reached
// the page, and how many of those went on to submit.
type FormFunnelPage struct {
	PageIndex     int    `json:"page_index"`
	Title         string `json:"title"`
	Reached       int64  `json:"reached"`
	CompletedFrom int64  `json:"completed_from"`
}

type FormStatsBucket struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

type FormIdentifiedVisitor struct {
	ContactID    uuid.UUID `json:"contact_id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	LastSeen     time.Time `json:"last_seen"`
	FurthestPage int       `json:"furthest_page"`
	Completed    bool      `json:"completed"`
	// Campaign names the campaign whose email brought this contact here.
	Campaign string `json:"campaign,omitempty"`
}

// CampaignFormStats is one form's performance inside a single campaign: how
// many recipients were given a personalized link, and what they did with it.
type CampaignFormStats struct {
	FormID      uuid.UUID `json:"form_id"`
	FormName    string    `json:"form_name"`
	PublicID    string    `json:"public_id"`
	Status      string    `json:"status"`
	LinksSent   int64     `json:"links_sent"`
	Viewers     int64     `json:"viewers"`
	Starters    int64     `json:"starters"`
	Submissions int64     `json:"submissions"`
	ShareURL    string    `json:"share_url,omitempty"`
}

// FormsDomainStatus is the state of an organization's custom forms domain,
// shaped like TrackingDomainStatus so the dashboard renders both the same
// way. Only a verified domain is used to build form URLs: an unresolved host
// would make every form link in a sent email dead, so it falls back to the
// install's shared forms host instead.
type FormsDomainStatus struct {
	FormsDomain           string     `json:"forms_domain"`
	FormsDomainVerified   bool       `json:"forms_domain_verified"`
	FormsDomainVerifiedAt *time.Time `json:"forms_domain_verified_at"`

	// CNAMETarget is the value to put in the CNAME: this install's forms host
	// (FORMS_DOMAIN). Empty means the install has none, so nothing can verify.
	CNAMETarget string `json:"cname_target"`
	// TXTName and TXTValue are the ownership record a new verification needs beside the CNAME.
	TXTName  string `json:"txt_name,omitempty"`
	TXTValue string `json:"txt_value,omitempty"`

	// Status is stable and machine-readable, straight from trackdns: verified,
	// unset, no_target, not_found, wrong_target, proof_missing or lookup_error.
	Status  string `json:"status"`
	Message string `json:"message"`
	// Observed is what DNS actually returned, so a customer can spot their own
	// typo by comparing it with what they typed.
	Observed string `json:"observed,omitempty"`
	// FormsHostUnresolvable reports that the install's own forms host does not
	// resolve, which is an operator problem and not the customer's typo.
	FormsHostUnresolvable bool `json:"forms_host_unresolvable"`
	// EdgeRecords are the TXT records the edge provider in front of the forms service still needs before it serves the domain over HTTPS.
	EdgeRecords []DNSRecord `json:"edge_records,omitempty"`
}

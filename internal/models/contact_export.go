package models

// ContactExportFormat is the on-disk encoding of an export. CSV is the
// industry default (everyone — Salesforce, HubSpot, Mailchimp — defaults
// to it), XLSX is what users with mostly-Excel workflows expect, and
// JSON is what developers reach for when piping into something else.
type ContactExportFormat string

const (
	ContactExportFormatCSV  ContactExportFormat = "csv"
	ContactExportFormatXLSX ContactExportFormat = "xlsx"
	ContactExportFormatJSON ContactExportFormat = "json"
)

// ContactExportScope is which slice of contacts we're exporting. We
// model "selected" and "filtered" as distinct paths so the client can
// hit either one without juggling filter state through the selection
// API. "all" is a convenience for "every contact this user owns".
type ContactExportScope string

const (
	ContactExportScopeAll      ContactExportScope = "all"
	ContactExportScopeFiltered ContactExportScope = "filtered"
	ContactExportScopeSelected ContactExportScope = "selected"
)

// Hard ceiling so a runaway export can't tie up a request goroutine for
// minutes. 50k covers ~all realistic single-export workloads; if a user
// has more contacts than this they should use a paginated export job.
const MaxContactExportRows = 50000

// ContactExportRequest is the body the export endpoint accepts. The
// validator on the handler is intentionally strict: an unknown field,
// scope, or format is a 400 instead of silent success with a half-broken
// download.
type ContactExportRequest struct {
	Format     ContactExportFormat `json:"format"`
	Scope      ContactExportScope  `json:"scope"`
	ContactIDs []string            `json:"contact_ids,omitempty"`
	Filters    *SearchContacts     `json:"filters,omitempty"`

	// Fields is a list of column identifiers in display order. See
	// ContactExportFieldXxx constants and the "custom:<key>" form for
	// arbitrary custom-field columns. An empty list means "use the
	// recommended default columns".
	Fields []string `json:"fields,omitempty"`

	// Filename without extension. Sanitized server-side. Empty falls
	// back to "contacts-<YYYY-MM-DD>".
	Filename string `json:"filename,omitempty"`
}

// Built-in field identifiers. Custom fields are addressed as
// "custom:<key>" so the import + export paths can round-trip arbitrary
// JSONB columns without the client knowing how they're stored.
const (
	ContactExportFieldID         = "id"
	ContactExportFieldEmail      = "email"
	ContactExportFieldFirstName  = "first_name"
	ContactExportFieldLastName   = "last_name"
	ContactExportFieldCompany    = "company"
	ContactExportFieldPhone      = "phone"
	ContactExportFieldSubscribed = "subscribed"
	ContactExportFieldCategories = "categories"
	ContactExportFieldCampaigns  = "campaigns"
	ContactExportFieldCreatedAt  = "created_at"
	ContactExportFieldUpdatedAt  = "updated_at"
)

// DefaultExportFields is what the UI lands on when the user just clicks
// "Export". Mirrors the columns shown on the table; users can opt into
// "Full" or build a custom set from the dialog.
var DefaultExportFields = []string{
	ContactExportFieldEmail,
	ContactExportFieldFirstName,
	ContactExportFieldLastName,
	ContactExportFieldCompany,
	ContactExportFieldPhone,
	ContactExportFieldSubscribed,
	ContactExportFieldCategories,
	ContactExportFieldCampaigns,
	ContactExportFieldCreatedAt,
}

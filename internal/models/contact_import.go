package models

import "time"

// MaxContactImportRows caps a single import. Large lists should be
// chunked client-side or routed through a future async-jobs pipeline —
// blocking a request goroutine on a 500k-row upload is a 504 waiting to
// happen.
const MaxContactImportRows = 50000

// MaxContactImportPreviewRows is the row count returned by the preview
// endpoint so the UI can show real data to inform column mapping. Kept
// small to avoid leaking the entire file back when the user just wants
// to see "what does this look like".
const MaxContactImportPreviewRows = 20

// ContactImportDedupStrategy decides what happens when a row's email
// matches an existing contact. "skip" is conservative (HubSpot default),
// "update" merges new values onto the existing row (Mailchimp's "update
// existing"), "create_duplicate" forces a new row anyway — useful when
// the same email is intentionally tracked twice but rare in practice.
type ContactImportDedupStrategy string

const (
	ContactImportDedupSkip            ContactImportDedupStrategy = "skip"
	ContactImportDedupUpdate          ContactImportDedupStrategy = "update"
	ContactImportDedupCreateDuplicate ContactImportDedupStrategy = "create_duplicate"
)

// ContactImportColumnTarget enumerates where a CSV/XLSX column can be
// mapped. "ignore" is the no-op that lets users dump a 30-column CRM
// export and only keep what matters. "custom:<key>" routes the column
// into Contact.CustomFields.
type ContactImportColumnTarget string

const (
	ContactImportTargetIgnore     ContactImportColumnTarget = "ignore"
	ContactImportTargetEmail      ContactImportColumnTarget = "email"
	ContactImportTargetFirstName  ContactImportColumnTarget = "first_name"
	ContactImportTargetLastName   ContactImportColumnTarget = "last_name"
	ContactImportTargetCompany    ContactImportColumnTarget = "company"
	ContactImportTargetPhone      ContactImportColumnTarget = "phone"
	ContactImportTargetSubscribed ContactImportColumnTarget = "subscribed"
	ContactImportTargetCategories ContactImportColumnTarget = "categories"
	// ContactImportTargetCustom routes the column into Contact.CustomFields
	// under ContactImportColumnMapping.CustomKey. "custom:<key>" is accepted
	// as an equivalent legacy spelling.
	ContactImportTargetCustom ContactImportColumnTarget = "custom"
)

// ContactImportColumnMapping says "the column at this index maps to
// this target". For custom fields use a "custom:<key>" target. The key
// becomes the JSONB column key. The index is zero-based and matches
// what the preview endpoint returned in `columns`.
type ContactImportColumnMapping struct {
	Index  int                       `json:"index"`
	Target ContactImportColumnTarget `json:"target"`

	// CustomKey is only used when Target == "custom:<anything>". It
	// is split out so the client can render a nicer label without
	// having to parse the target string.
	CustomKey string `json:"custom_key,omitempty"`
}

type ContactImportPreview struct {
	// Echo what we detected about the file. Filename + Format help the
	// UI render a confirmation; total rows is for "1,243 rows detected"
	// banners.
	Filename  string `json:"filename"`
	Format    string `json:"format"`
	TotalRows int    `json:"total_rows"`

	// Columns are the headers we found. If the file has no header row,
	// we synthesise "Column 1", "Column 2", ... so the user can still
	// map them. HasHeader records what we decided so the UI can offer
	// a toggle.
	Columns   []string `json:"columns"`
	HasHeader bool     `json:"has_header"`

	// Sample rows verbatim. Length is min(N, MaxContactImportPreviewRows).
	SampleRows [][]string `json:"sample_rows"`

	// Suggested mapping based on header heuristics. The client should
	// treat this as a default the user can override, not a binding
	// decision.
	SuggestedMapping []ContactImportColumnMapping `json:"suggested_mapping"`
}

// ContactImportCommit is the full configuration for committing an
// import: how to map columns, how to treat collisions, what categories
// to assign, what the default subscription state is, and which
// campaign(s) the imported contacts should join.
type ContactImportCommit struct {
	Mapping     []ContactImportColumnMapping `json:"mapping"`
	Dedup       ContactImportDedupStrategy   `json:"dedup"`
	HasHeader   bool                         `json:"has_header"`
	CategoryIDs []string                     `json:"category_ids,omitempty"`
	CampaignIDs []string                     `json:"campaign_ids,omitempty"`

	// SubscribedDefault is what new contacts inherit when no
	// subscribed column was mapped. Defaults to true server-side.
	SubscribedDefault *bool `json:"subscribed_default,omitempty"`
}

// ContactImportRowError is a row that couldn't be imported. The line
// number is the 1-based index into the source file (after the header
// if HasHeader was true) so the user can find it in Excel.
type ContactImportRowError struct {
	Line   int      `json:"line"`
	Email  string   `json:"email,omitempty"`
	Values []string `json:"values,omitempty"`
	Reason string   `json:"reason"`
}

// MaxContactImportReportedErrors caps how many per-row entries travel back in
// the response. The counters still count every row; without the cap a 50k-row
// file of bad addresses would echo the whole file back as JSON.
const MaxContactImportReportedErrors = 1000

type ContactImportResult struct {
	Total     int       `json:"total"`
	Imported  int       `json:"imported"`
	Updated   int       `json:"updated"`
	Skipped   int       `json:"skipped"`
	Failed    int       `json:"failed"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`

	// Errors holds per-row failures and per-row notes, capped at
	// MaxContactImportReportedErrors entries.
	Errors []ContactImportRowError `json:"errors,omitempty"`
	// ErrorsTruncated is true when that cap was reached, so the UI can say
	// "showing the first N of M" instead of implying it listed everything.
	ErrorsTruncated bool `json:"errors_truncated,omitempty"`

	// Quality is what the uploaded addresses look like, measured at import.
	// Advisory: a bad list is reported here and stopped at launch, never
	// refused here, because these are the customer's own records.
	Quality *ContactImportQuality `json:"quality,omitempty"`
}

// ContactImportQuality is an import's address-level assessment.
type ContactImportQuality struct {
	Malformed  int `json:"malformed"`
	Disposable int `json:"disposable"`
	// Role counts shared inboxes. Reported, not counted as bad: mailing info@
	// is a choice, and many legitimate B2B lists are mostly role addresses.
	Role        int     `json:"role"`
	BadSharePct float64 `json:"bad_share_pct"`
	Flagged     bool    `json:"flagged"`
	Summary     string  `json:"summary,omitempty"`
}

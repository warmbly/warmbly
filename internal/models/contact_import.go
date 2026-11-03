package models

import (
	"time"

	"github.com/google/uuid"
)

// MaxContactImportRows caps a single import. The background import runs a
// file this size in chunks; the synchronous commit endpoint holds the same cap.
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
	// ContactImportTargetVerificationStatus reads a verdict column written by
	// Warmbly or another verification service (ZeroBounce, MillionVerifier,
	// NeverBounce, ...). Values are recognised by vocabulary; a value nobody
	// knows leaves the contact unverified rather than failing the row.
	ContactImportTargetVerificationStatus ContactImportColumnTarget = "verification_status"
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

	// VerificationProvider names the vocabulary of a verification_status
	// column when the header or its values made it clear (e.g. "zerobounce").
	// Optional; without it each value is recognised by itself.
	VerificationProvider string `json:"verification_provider,omitempty"`

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

	// InferredColumns are the indexes whose suggestion came from the TypeSafe
	// judgment rather than the header or the values, worth a second look.
	InferredColumns []int `json:"inferred_columns,omitempty"`

	// ColumnStats describes every column over the whole file, not the sample,
	// so the mapper can show how full a column is. Set by the background import.
	ColumnStats []ContactImportColumnStats `json:"column_stats,omitempty"`
	// MappingSource says where SuggestedMapping came from: "saved" when the
	// workspace confirmed a mapping for these exact headers before.
	MappingSource string `json:"mapping_source,omitempty"`
}

// ContactImportColumnStats is one column's fill across the whole file.
type ContactImportColumnStats struct {
	Filled int `json:"filled"`
	// Distinct counts different non-empty values, capped at
	// MaxContactImportDistinctTracked.
	Distinct int `json:"distinct"`
	// Samples are a few distinct non-empty values, in file order.
	Samples []string `json:"samples"`
}

// MaxContactImportDistinctTracked bounds the distinct-value count per column.
const MaxContactImportDistinctTracked = 1000

// Mapping sources reported on a preview.
const (
	ContactImportMappingSuggested = "suggested"
	ContactImportMappingSaved     = "saved"
)

// ContactImportCommit is the full configuration for committing an
// import: how to map columns, how to treat collisions, what categories
// to assign, what the default subscription state is, and which
// campaign(s) and segment(s) the imported contacts should join.
type ContactImportCommit struct {
	Mapping     []ContactImportColumnMapping `json:"mapping"`
	Dedup       ContactImportDedupStrategy   `json:"dedup"`
	HasHeader   bool                         `json:"has_header"`
	CategoryIDs []string                     `json:"category_ids,omitempty"`
	CampaignIDs []string                     `json:"campaign_ids,omitempty"`
	// SkipActiveInOtherCampaigns keeps an existing contact out of
	// CampaignIDs while their sequence is still running in another campaign.
	// The row itself is imported as usual.
	SkipActiveInOtherCampaigns bool `json:"skip_active_in_other_campaigns,omitempty"`
	// SegmentIDs pins every imported row into these segments as a manual
	// include override, the same write the "Add to segment" bulk action does.
	SegmentIDs []string `json:"segment_ids,omitempty"`
	// SkipMissingSegments drops a target segment that no longer exists instead
	// of refusing the import. Set by saved recurring sources (the Google Sheets
	// sync), where a segment deleted months later must not stop every run; an
	// interactive import still gets a 400 on a segment the user just picked.
	SkipMissingSegments bool `json:"-"`

	// SubscribedDefault is what new contacts inherit when no
	// subscribed column was mapped. Defaults to true server-side.
	SubscribedDefault *bool `json:"subscribed_default,omitempty"`

	// Source / SourceDetail stamp new contacts' first-touch attribution. Set by
	// the caller (file import, Google Sheets sync), never from the request.
	Source       ContactSource `json:"-"`
	SourceDetail string        `json:"-"`
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

	// SegmentsPinned is nil when the import had no segment targets to write.
	// With targets it is true when every membership write landed and false
	// when one did not, with the reason among the notes. A plain bool could
	// not carry that third state: omitempty drops false, so a failed pin
	// looked exactly like an import that never asked for one.
	SegmentsPinned *bool `json:"segments_pinned,omitempty"`

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

// ContactImportStatus is where a background import is in its life.
type ContactImportStatus string

const (
	// ContactImportDraft is uploaded and waiting for a mapping and a start.
	ContactImportDraft     ContactImportStatus = "draft"
	ContactImportQueued    ContactImportStatus = "queued"
	ContactImportRunning   ContactImportStatus = "running"
	ContactImportCompleted ContactImportStatus = "completed"
	ContactImportFailed    ContactImportStatus = "failed"
	ContactImportCancelled ContactImportStatus = "cancelled"
)

// Terminal reports whether the import will never change again.
func (s ContactImportStatus) Terminal() bool {
	return s == ContactImportCompleted || s == ContactImportFailed || s == ContactImportCancelled
}

// Row outcomes of a background import.
const (
	ContactImportRowPending  = "pending"
	ContactImportRowImported = "imported"
	ContactImportRowUpdated  = "updated"
	ContactImportRowSkipped  = "skipped"
	ContactImportRowFailed   = "failed"
)

// ContactImport is one background import of a contact file.
type ContactImport struct {
	ID             uuid.UUID           `json:"id"`
	OrganizationID uuid.UUID           `json:"organization_id"`
	CreatedBy      *uuid.UUID          `json:"created_by,omitempty"`
	Filename       string              `json:"filename"`
	Format         string              `json:"format"`
	Status         ContactImportStatus `json:"status"`
	HasHeader      bool                `json:"has_header"`
	Columns        []string            `json:"columns"`
	// Total is the number of data rows; Processed how many have settled.
	Total     int `json:"total"`
	Processed int `json:"processed"`
	Imported  int `json:"imported"`
	Updated   int `json:"updated"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`

	Options        *ContactImportCommit  `json:"options,omitempty"`
	Quality        *ContactImportQuality `json:"quality,omitempty"`
	SegmentsPinned *bool                 `json:"segments_pinned,omitempty"`
	Notes          []string              `json:"notes"`
	Error          string                `json:"error,omitempty"`

	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`

	// Preview is what the mapper shows, carried by a draft only, so a reload resumes it.
	Preview *ContactImportPreview `json:"preview,omitempty"`
	// Failures are the first failed rows once the import has finished.
	Failures []ContactImportRowError `json:"failures,omitempty"`
}

// MaxContactImportListedFailures bounds ContactImport.Failures; the failed-rows
// download carries every one of them.
const MaxContactImportListedFailures = 200

// ContactImportAnalyzeRequest asks what a draft would do under a mapping.
type ContactImportAnalyzeRequest struct {
	Mapping   []ContactImportColumnMapping `json:"mapping"`
	HasHeader bool                         `json:"has_header"`
}

// ContactImportAnalysis is what an import would do, read over the whole file
// before anything is written.
type ContactImportAnalysis struct {
	// Rows is every data row; each lands in exactly one of the buckets below.
	Rows int `json:"rows"`
	// New addresses the workspace does not have yet.
	New int `json:"new"`
	// Existing addresses the workspace already has a contact for.
	Existing int `json:"existing"`
	// DuplicatesInFile are rows repeating an address an earlier row holds.
	DuplicatesInFile int `json:"duplicates_in_file"`
	// Invalid rows have no usable address or a value that cannot be read.
	Invalid int `json:"invalid"`
	// Conflicts are addresses the importing member already holds as a contact
	// in another workspace, which cannot be created here.
	Conflicts int `json:"conflicts"`
	// InvalidSamples are the first invalid or conflicting rows, with reasons.
	InvalidSamples []ContactImportRowError `json:"invalid_samples"`
	Quality        *ContactImportQuality   `json:"quality,omitempty"`
	// Problem is why starting this import would be refused as a whole (the
	// plan's contact limit, too many new categories), empty when it would run.
	Problem string `json:"problem,omitempty"`
}

// MaxContactImportAnalysisSamples bounds ContactImportAnalysis.InvalidSamples.
const MaxContactImportAnalysisSamples = 25

// ContactImportRowOutcome is what became of one row of a background import.
type ContactImportRowOutcome struct {
	Line      int
	Status    string
	Email     string
	Reason    string
	ContactID *uuid.UUID
}

// ContactImportList is a page of imports, newest first.
type ContactImportList struct {
	Data       []ContactImport `json:"data"`
	Pagination Pagination      `json:"pagination"`
}

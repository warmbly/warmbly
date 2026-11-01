package models

import "github.com/google/uuid"

// CompanyEnrichQuery selects one fixed CompanyEnrich endpoint. Body and Query
// use the provider's field names; filters are forwarded without projections.
type CompanyEnrichQuery struct {
	Operation string         `json:"operation"`
	Body      map[string]any `json:"body,omitempty"`
	Query     map[string]any `json:"query,omitempty"`
}

// CompanyEnrichResult retains the complete provider payload. Large integer
// values are strings so browser clients do not round person identifiers.
type CompanyEnrichResult struct {
	Data            any      `json:"data"`
	CreditsConsumed *float64 `json:"credits_consumed,omitempty"`
}

// CompanyEnrichContactRequest enriches one existing workspace contact. The
// lookup identity always comes from that contact, never from this request.
type CompanyEnrichContactRequest struct {
	ContactID uuid.UUID `json:"contact_id"`
	Kind      string    `json:"kind"`
	Overwrite bool      `json:"overwrite"`
}

// CompanyEnrichContactResult reports exactly which profile fields were saved.
type CompanyEnrichContactResult struct {
	CompanyEnrichResult
	Contact       *Contact `json:"contact"`
	UpdatedFields []string `json:"updated_fields"`
}

// CompanyEnrichProfilePatch is server-only and contains no address, consent,
// verification or campaign fields. Nil fields leave the current value intact.
type CompanyEnrichProfilePatch struct {
	FirstName    *string
	LastName     *string
	Company      *string
	CustomFields map[string]string
}

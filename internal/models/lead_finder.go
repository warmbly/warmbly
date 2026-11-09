package models

import (
	"time"

	"github.com/google/uuid"
)

// LeadProvider is where a lead's business record came from. Mirrors the CHECK
// on leads.provider and lead_searches.provider.
type LeadProvider string

const (
	// LeadProviderOSM is OpenStreetMap through an Overpass endpoint. Free, no key.
	LeadProviderOSM LeadProvider = "osm"
	// LeadProviderGooglePlaces is the Google Places API (New).
	LeadProviderGooglePlaces LeadProvider = "google_places"
	// LeadProviderFoursquare is the Foursquare Places API.
	LeadProviderFoursquare LeadProvider = "foursquare"
	// LeadProviderOutscraper is Outscraper's Google Maps search.
	LeadProviderOutscraper LeadProvider = "outscraper"
)

// LeadProviders lists every provider in the order the finder offers them.
var LeadProviders = []LeadProvider{LeadProviderOSM, LeadProviderGooglePlaces, LeadProviderFoursquare, LeadProviderOutscraper}

// Valid reports whether the value is one the database accepts.
func (p LeadProvider) Valid() bool {
	for _, v := range LeadProviders {
		if p == v {
			return true
		}
	}
	return false
}

// LeadStatus is where a lead stands in someone's prospecting.
type LeadStatus string

const (
	LeadStatusNew       LeadStatus = "new"
	LeadStatusSaved     LeadStatus = "saved"
	LeadStatusContacted LeadStatus = "contacted"
	LeadStatusDismissed LeadStatus = "dismissed"
)

// Valid reports whether the value is one the database accepts.
func (s LeadStatus) Valid() bool {
	switch s {
	case LeadStatusNew, LeadStatusSaved, LeadStatusContacted, LeadStatusDismissed:
		return true
	}
	return false
}

// LeadEnrichmentStatus is how far reading a lead's website got.
type LeadEnrichmentStatus string

const (
	LeadEnrichmentNone      LeadEnrichmentStatus = "none"
	LeadEnrichmentQueued    LeadEnrichmentStatus = "queued"
	LeadEnrichmentDone      LeadEnrichmentStatus = "done"
	LeadEnrichmentNoWebsite LeadEnrichmentStatus = "no_website"
	LeadEnrichmentFailed    LeadEnrichmentStatus = "failed"
)

// Valid reports whether the value is one the database accepts.
func (s LeadEnrichmentStatus) Valid() bool {
	switch s {
	case LeadEnrichmentNone, LeadEnrichmentQueued, LeadEnrichmentDone, LeadEnrichmentNoWebsite, LeadEnrichmentFailed:
		return true
	}
	return false
}

// LatLng is one point on the map.
type LatLng struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// LeadAreaKind is the shape a search covers.
type LeadAreaKind string

const (
	LeadAreaBBox    LeadAreaKind = "bbox"
	LeadAreaCircle  LeadAreaKind = "circle"
	LeadAreaPolygon LeadAreaKind = "polygon"
)

// LeadBounds is a south-west to north-east rectangle.
type LeadBounds struct {
	South float64 `json:"south"`
	West  float64 `json:"west"`
	North float64 `json:"north"`
	East  float64 `json:"east"`
}

// LeadArea is the part of the map a search covers. Bounds is always set: for a
// circle or polygon it is the shape's bounding box.
type LeadArea struct {
	Kind    LeadAreaKind `json:"kind"`
	Bounds  LeadBounds   `json:"bounds"`
	Center  *LatLng      `json:"center,omitempty"`
	RadiusM float64      `json:"radius_m,omitempty"`
	Polygon []LatLng     `json:"polygon,omitempty"`
}

// LeadEmail is one address found for a lead and what checking it said.
type LeadEmail struct {
	Email string `json:"email"`
	// Source is "website" when read off the business's site, "search" when a
	// web search showed it on a page about the business, else the provider.
	Source      string `json:"source"`
	URL         string `json:"url,omitempty"`
	DomainMatch bool   `json:"domain_match"`
	Status      string `json:"status,omitempty"`
	SubStatus   string `json:"sub_status,omitempty"`
}

// LeadSite is what reading a lead's website found besides addresses.
type LeadSite struct {
	Reachable   bool     `json:"reachable"`
	FinalURL    string   `json:"final_url,omitempty"`
	HTTPS       bool     `json:"https"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Platform    string   `json:"platform,omitempty"`
	Signals     []string `json:"signals,omitempty"`
	Phones      []string `json:"phones,omitempty"`
	Pages       int      `json:"pages,omitempty"`
	// EmailPattern is how the domain builds a person's address ("first.last"),
	// learned from addresses found or a verified guess.
	EmailPattern string `json:"email_pattern,omitempty"`
	// AcceptsAll is set when the domain's mail server accepts any address, so a
	// guessed address cannot be confirmed.
	AcceptsAll bool `json:"accepts_all,omitempty"`
	// FoundWebsite is set when the listing had no website and the finder
	// confirmed one worked out from the business's name.
	FoundWebsite bool `json:"found_website,omitempty"`
	// ReadVia names the reader service that fetched the site after it turned
	// a direct visit away; empty when it was read directly.
	ReadVia string `json:"read_via,omitempty"`
	// Unread says why a site that could not be read was not: "blocked" (it
	// turned automated visitors away), "robots" (its robots.txt asks not to)
	// or "unreachable"; empty when it was read.
	Unread    LeadSiteUnread `json:"unread,omitempty"`
	Error     string         `json:"error,omitempty"`
	CheckedAt *time.Time     `json:"checked_at,omitempty"`
}

// LeadSiteUnread is why a lead's website could not be read.
type LeadSiteUnread string

const (
	LeadSiteBlocked     LeadSiteUnread = "blocked"
	LeadSiteRobots      LeadSiteUnread = "robots"
	LeadSiteUnreachable LeadSiteUnread = "unreachable"
)

// Lead is one business in a workspace's lead finder.
type Lead struct {
	ID               uuid.UUID            `json:"id"`
	OrganizationID   uuid.UUID            `json:"-"`
	Provider         LeadProvider         `json:"provider"`
	ExternalID       string               `json:"external_id"`
	Name             string               `json:"name"`
	Category         string               `json:"category"`
	Categories       []string             `json:"categories"`
	Address          string               `json:"address"`
	Street           string               `json:"street"`
	City             string               `json:"city"`
	Region           string               `json:"region"`
	Postcode         string               `json:"postcode"`
	Country          string               `json:"country"`
	Latitude         float64              `json:"latitude"`
	Longitude        float64              `json:"longitude"`
	Phone            string               `json:"phone"`
	Website          string               `json:"website"`
	Domain           string               `json:"domain"`
	Email            string               `json:"email"`
	Emails           []LeadEmail          `json:"emails"`
	Socials          map[string]string    `json:"socials"`
	Rating           *float64             `json:"rating"`
	ReviewCount      *int                 `json:"review_count"`
	PriceLevel       *int                 `json:"price_level"`
	Hours            []string             `json:"hours"`
	BusinessStatus   string               `json:"business_status"`
	MapsURL          string               `json:"maps_url"`
	Description      string               `json:"description"`
	Chain            string               `json:"chain"`
	Site             LeadSite             `json:"site"`
	Score            int                  `json:"score"`
	Status           LeadStatus           `json:"status"`
	EnrichmentStatus LeadEnrichmentStatus `json:"enrichment_status"`
	EnrichedAt       *time.Time           `json:"enriched_at"`
	ContactID        *uuid.UUID           `json:"contact_id"`
	Tags             []string             `json:"tags"`
	Notes            string               `json:"notes"`
	PeopleCount      int                  `json:"people_count"`
	// People is loaded only where one lead is read in full.
	People      []LeadPerson `json:"people,omitempty"`
	FirstSeenAt time.Time    `json:"first_seen_at"`
	LastSeenAt  time.Time    `json:"last_seen_at"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// LeadSeniority places a person in their business. Mirrors the CHECK on
// lead_people.seniority.
type LeadSeniority string

const (
	LeadSeniorityOwner     LeadSeniority = "owner"
	LeadSeniorityExecutive LeadSeniority = "executive"
	LeadSeniorityManager   LeadSeniority = "manager"
	LeadSeniorityStaff     LeadSeniority = "staff"
)

// LeadPersonSource is where a person was found. Mirrors the CHECK on lead_people.source.
type LeadPersonSource string

const (
	// LeadPersonSchema is structured data (schema.org Person) on the website.
	LeadPersonSchema LeadPersonSource = "schema"
	// LeadPersonPage is a name and role read off a team or about page.
	LeadPersonPage LeadPersonSource = "page"
	// LeadPersonImprint is a legal notice naming the owner or managing director.
	LeadPersonImprint LeadPersonSource = "imprint"
	// LeadPersonEmail is a personal address on the site whose owner was named.
	LeadPersonEmail LeadPersonSource = "email"
	// LeadPersonLinkedIn is a LinkedIn profile linked from the website.
	LeadPersonLinkedIn LeadPersonSource = "linkedin"
	// LeadPersonManual is someone a teammate typed in.
	LeadPersonManual LeadPersonSource = "manual"
)

// LeadEmailSource says how a person's address was arrived at. Mirrors the CHECK
// on lead_people.email_source.
type LeadEmailSource string

const (
	LeadEmailNone LeadEmailSource = ""
	// LeadEmailWebsite was published on the business's own site.
	LeadEmailWebsite LeadEmailSource = "website"
	// LeadEmailPattern was built from the name and the domain's address pattern.
	LeadEmailPattern LeadEmailSource = "pattern"
	// LeadEmailManual was typed in by a teammate.
	LeadEmailManual LeadEmailSource = "manual"
)

// LeadPersonStatusGuessed marks a pattern-built address nothing could confirm.
const LeadPersonStatusGuessed = "guessed"

// LeadPerson is one person who works at a lead's business.
type LeadPerson struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"-"`
	LeadID         uuid.UUID `json:"lead_id"`
	Name           string    `json:"name"`
	// NameKey is the folded "first last" two mentions of one person share.
	NameKey         string           `json:"-"`
	FirstName       string           `json:"first_name"`
	LastName        string           `json:"last_name"`
	Title           string           `json:"title"`
	Seniority       LeadSeniority    `json:"seniority"`
	Email           string           `json:"email"`
	EmailSource     LeadEmailSource  `json:"email_source"`
	EmailStatus     string           `json:"email_status"`
	EmailConfidence int              `json:"email_confidence"`
	LinkedInURL     string           `json:"linkedin_url"`
	Phone           string           `json:"phone"`
	Source          LeadPersonSource `json:"source"`
	SourceURL       string           `json:"source_url"`
	ContactID       *uuid.UUID       `json:"contact_id"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// LeadSearch is one search someone ran on the lead finder map.
type LeadSearch struct {
	ID              uuid.UUID    `json:"id"`
	OrganizationID  uuid.UUID    `json:"-"`
	CreatedByUserID *uuid.UUID   `json:"created_by_user_id"`
	Name            string       `json:"name"`
	Provider        LeadProvider `json:"provider"`
	Query           string       `json:"query"`
	Category        string       `json:"category"`
	LocationLabel   string       `json:"location_label"`
	Area            LeadArea     `json:"area"`
	Saved           bool         `json:"saved"`
	ResultCount     int          `json:"result_count"`
	PagesFetched    int          `json:"pages_fetched"`
	HasMore         bool         `json:"has_more"`
	NextCursor      string       `json:"-"`
	LastRunAt       time.Time    `json:"last_run_at"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

// LeadEnrichmentRun is one background pass reading leads' websites.
type LeadEnrichmentRun struct {
	ID                uuid.UUID   `json:"id"`
	OrganizationID    uuid.UUID   `json:"-"`
	RequestedByUserID *uuid.UUID  `json:"requested_by_user_id"`
	LeadIDs           []uuid.UUID `json:"-"`
	Verify            bool        `json:"verify"`
	People            bool        `json:"people"`
	// WebSearch lets the run search the web for a missing site or for the
	// addresses a site that could not be read publishes elsewhere.
	WebSearch    bool       `json:"web_search"`
	Status       string     `json:"status"`
	Total        int        `json:"total"`
	Processed    int        `json:"processed"`
	EmailsFound  int        `json:"emails_found"`
	PeopleFound  int        `json:"people_found"`
	NoWebsite    int        `json:"no_website"`
	Failed       int        `json:"failed"`
	ChargedMicro Micro      `json:"-"`
	ChargedUSD   float64    `json:"charged_usd"`
	LastError    string     `json:"last_error"`
	StartedAt    *time.Time `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// LeadSort orders a lead list.
type LeadSort string

const (
	LeadSortRecent  LeadSort = "recent"
	LeadSortScore   LeadSort = "score"
	LeadSortRating  LeadSort = "rating"
	LeadSortReviews LeadSort = "reviews"
	LeadSortName    LeadSort = "name"
)

// Valid reports whether the sort is one the list understands.
func (s LeadSort) Valid() bool {
	switch s {
	case LeadSortRecent, LeadSortScore, LeadSortRating, LeadSortReviews, LeadSortName:
		return true
	}
	return false
}

// LeadFilter narrows a workspace's leads. Every field is optional.
type LeadFilter struct {
	SearchID     *uuid.UUID             `json:"search_id,omitempty"`
	Q            string                 `json:"q,omitempty"`
	Statuses     []LeadStatus           `json:"statuses,omitempty"`
	Providers    []LeadProvider         `json:"providers,omitempty"`
	Category     string                 `json:"category,omitempty"`
	Tag          string                 `json:"tag,omitempty"`
	HasEmail     *bool                  `json:"has_email,omitempty"`
	HasWebsite   *bool                  `json:"has_website,omitempty"`
	HasPhone     *bool                  `json:"has_phone,omitempty"`
	HasSocials   *bool                  `json:"has_socials,omitempty"`
	HasPeople    *bool                  `json:"has_people,omitempty"`
	InContacts   *bool                  `json:"in_contacts,omitempty"`
	Enrichment   []LeadEnrichmentStatus `json:"enrichment,omitempty"`
	MinRating    *float64               `json:"min_rating,omitempty"`
	MinReviews   *int                   `json:"min_reviews,omitempty"`
	MaxReviews   *int                   `json:"max_reviews,omitempty"`
	MinScore     *int                   `json:"min_score,omitempty"`
	Bounds       *LeadBounds            `json:"bounds,omitempty"`
	ExcludeChain bool                   `json:"exclude_chains,omitempty"`
}

// LeadSelection names a set of leads: explicit ids, or every lead matching a
// filter.
type LeadSelection struct {
	LeadIDs []uuid.UUID `json:"lead_ids,omitempty"`
	All     bool        `json:"all,omitempty"`
	Filter  *LeadFilter `json:"filter,omitempty"`
}

// LeadPatch is the editable part of a lead. Nil fields are left alone.
type LeadPatch struct {
	Status     *LeadStatus `json:"status,omitempty"`
	Notes      *string     `json:"notes,omitempty"`
	Tags       *[]string   `json:"tags,omitempty"`
	AddTags    []string    `json:"add_tags,omitempty"`
	RemoveTags []string    `json:"remove_tags,omitempty"`
}

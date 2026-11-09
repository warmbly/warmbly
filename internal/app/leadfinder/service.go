package leadfinder

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/emailverify"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/pkg/webread"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// SelectionCap is the most leads one bulk action takes.
	SelectionCap = 5000
	// PointsCap is the most pins the map draws at once.
	PointsCap = 5000
	// searchTimeout bounds one provider page.
	searchTimeout = 55 * time.Second
	// defaultUserAgent identifies the finder to providers and websites.
	defaultUserAgent = "WarmblyBot/1.0 (+https://warmbly.com/bot)"
)

// Config is the instance-level setup: endpoints, the operator's own provider
// keys and the map style. Every value is optional.
type Config struct {
	OverpassURL   string
	GeocoderURL   string
	GoogleKey     string
	FoursquareKey string
	OutscraperKey string
	MapStyleLight string
	MapStyleDark  string
	UserAgent     string
}

// Integrations is the slice of the integration service the finder reads keys through.
type Integrations interface {
	ListConnections(ctx context.Context, orgID uuid.UUID) ([]models.IntegrationConnection, error)
	ConnectionConfig(ctx context.Context, orgID, connID uuid.UUID) (map[string]any, *models.IntegrationConnection, error)
}

// Verifier checks one address without writing anything.
type Verifier interface {
	VerifyAddress(ctx context.Context, orgID uuid.UUID, email string) emailverify.Result
}

// Credits charges priced units to the workspace balance.
type Credits interface {
	Quote(ctx context.Context, u credits.Usage) models.Micro
	ChargeUpfront(ctx context.Context, orgID uuid.UUID, u credits.Usage, reason, idempotencyKey string) (models.Micro, error)
	RefundCharge(ctx context.Context, orgID uuid.UUID, chargeKey, reason string) (models.Micro, error)
	// Unmetered is true when the instance charges nothing.
	Unmetered() bool
}

// Deps is everything the finder talks to.
type Deps struct {
	Repo         repository.LeadFinderRepository
	Integrations Integrations
	Contacts     contact.ContactService
	Verifier     Verifier
	Credits      Credits
	Audit        AuditLogger
	Config       Config
	// Reader fetches a page a site turned a direct visit away from (WEB_READER).
	Reader webread.Reader
	// Search is the instance's web search (SEARCH_PROVIDER), charged per search.
	WebSearch generation.SearchClient
	// WorkspaceSearch is a workspace's own web search, used before the instance's.
	WorkspaceSearch WorkspaceSearch
}

// Service finds, enriches, exports and imports leads.
type Service struct {
	Deps
	geocoder *Geocoder
	crawler  *Crawler
	osm      Provider
	// newProvider builds a keyed provider; tests replace it.
	newProvider func(id models.LeadProvider, key string) Provider
}

// NewService builds the lead finder.
func NewService(d Deps) *Service {
	ua := d.Config.UserAgent
	if ua == "" {
		ua = defaultUserAgent
	}
	d.Config.UserAgent = ua
	return &Service{
		Deps:        d,
		geocoder:    NewGeocoder(d.Config.GeocoderURL, ua),
		crawler:     NewCrawler(ua),
		osm:         NewOSM(d.Config.OverpassURL, ua),
		newProvider: keyedProvider,
	}
}

// metered is whether this instance charges the balance at all.
func (s *Service) metered() bool {
	return s.Credits != nil && !s.Credits.Unmetered()
}

func keyedProvider(id models.LeadProvider, key string) Provider {
	switch id {
	case models.LeadProviderGooglePlaces:
		return NewGooglePlaces(key)
	case models.LeadProviderFoursquare:
		return NewFoursquare(key)
	case models.LeadProviderOutscraper:
		return NewOutscraper(key)
	}
	return nil
}

// integrationFor is the workspace integration that holds a provider's key.
var integrationFor = map[models.LeadProvider]models.IntegrationProvider{
	models.LeadProviderGooglePlaces: models.IntegrationGooglePlaces,
	models.LeadProviderFoursquare:   models.IntegrationFoursquare,
	models.LeadProviderOutscraper:   models.IntegrationOutscraper,
}

var providerMeta = map[models.LeadProvider]ProviderInfo{
	models.LeadProviderOSM: {
		Name:        "OpenStreetMap",
		Description: "Free and open map data. Strong on names, places and websites; phone and email coverage varies by region.",
		PageSize:    osmMaxResults, MaxPages: 1,
		Fields: []string{"name", "address", "category", "website", "phone", "email", "hours", "socials"},
	},
	models.LeadProviderGooglePlaces: {
		Name:        "Google Places",
		Description: "Google's business listings with ratings, review counts, hours and websites. Uses a Google Maps Platform key.",
		PageSize:    20, MaxPages: 3,
		Fields: []string{"name", "address", "category", "website", "phone", "rating", "reviews", "price", "hours", "status"},
	},
	models.LeadProviderFoursquare: {
		Name:        "Foursquare",
		Description: "Foursquare's global places database with categories, chains, socials, ratings and hours.",
		PageSize:    50, MaxPages: 10,
		Fields: []string{"name", "address", "category", "website", "phone", "email", "rating", "reviews", "price", "hours", "socials", "chain"},
	},
	models.LeadProviderOutscraper: {
		Name:        "Outscraper",
		Description: "Google Maps results through Outscraper, with emails and social profiles from the business's domain.",
		PageSize:    outscraperPageSize, MaxPages: outscraperMaxSkip / outscraperPageSize,
		Fields: []string{"name", "address", "category", "website", "phone", "email", "rating", "reviews", "price", "hours", "socials", "status"},
	},
}

func (s *Service) instanceKey(id models.LeadProvider) string {
	switch id {
	case models.LeadProviderGooglePlaces:
		return s.Config.GoogleKey
	case models.LeadProviderFoursquare:
		return s.Config.FoursquareKey
	case models.LeadProviderOutscraper:
		return s.Config.OutscraperKey
	}
	return ""
}

// workspaceKey is the key from the workspace's own connection, or "".
func (s *Service) workspaceKey(ctx context.Context, orgID uuid.UUID, id models.LeadProvider, conns []models.IntegrationConnection) string {
	want, ok := integrationFor[id]
	if !ok {
		return ""
	}
	return s.integrationKey(ctx, orgID, want, conns)
}

// integrationKey is the API key of the workspace's live connection to want, or "".
func (s *Service) integrationKey(ctx context.Context, orgID uuid.UUID, want models.IntegrationProvider, conns []models.IntegrationConnection) string {
	if s.Integrations == nil {
		return ""
	}
	for _, c := range conns {
		if c.Provider != want || c.Status == models.IntegrationStatusDisconnected || c.Status == models.IntegrationStatusReauthRequired {
			continue
		}
		cfg, _, err := s.Integrations.ConnectionConfig(ctx, orgID, c.ID)
		if err != nil {
			continue
		}
		if key, _ := cfg["api_key"].(string); strings.TrimSpace(key) != "" {
			return strings.TrimSpace(key)
		}
	}
	return ""
}

func (s *Service) connections(ctx context.Context, orgID uuid.UUID) []models.IntegrationConnection {
	if s.Integrations == nil {
		return nil
	}
	conns, err := s.Integrations.ListConnections(ctx, orgID)
	if err != nil {
		log.Warn().Err(err).Msg("leadfinder: could not list integrations")
		return nil
	}
	return conns
}

// resolve picks the provider client for a search and where its key came
// from. A workspace's own key wins over the operator's.
func (s *Service) resolve(ctx context.Context, orgID uuid.UUID, id models.LeadProvider) (Provider, string, error) {
	if id == models.LeadProviderOSM {
		return s.osm, "none", nil
	}
	if !id.Valid() {
		return nil, "", errx.New(errx.BadRequest, "unknown lead provider")
	}
	if key := s.workspaceKey(ctx, orgID, id, s.connections(ctx, orgID)); key != "" {
		return s.newProvider(id, key), "workspace", nil
	}
	if key := s.instanceKey(id); key != "" {
		return s.newProvider(id, key), "instance", nil
	}
	return nil, "", errx.NewWithIdentifier(errx.BadRequest, "lead_provider_not_connected",
		fmt.Sprintf("Connect %s under Integrations to search with it", providerMeta[id].Name))
}

// FinderConfig is what the dashboard needs before its first search.
type FinderConfig struct {
	Providers       []ProviderInfo `json:"providers"`
	Categories      []Category     `json:"categories"`
	DefaultProvider string         `json:"default_provider"`
	EnrichCost      float64        `json:"enrich_cost_usd"`
	PersonEmailCost float64        `json:"person_email_cost_usd"`
	ExportColumns   []ExportColumn `json:"export_columns"`
	PersonColumns   []PersonColumn `json:"person_columns"`
	DefaultColumns  []string       `json:"default_export_columns"`
	Map             MapConfig      `json:"map"`
	// WebReader is how a site that turns a direct visit away still gets read.
	WebReader WebFallback `json:"web_reader"`
	// WebSearch is how a missing site, or a blocked site's addresses, are searched for.
	WebSearch WebFallback `json:"web_search"`
}

// WebFallback is one way past a site that turns the finder away.
type WebFallback struct {
	Available bool `json:"available"`
	// Sources are tried in order: reader or search names, "mcp" for a
	// workspace's MCP tool, "workspace_jina" for its Jina key, "instance" for
	// the operator's search.
	Sources []string `json:"sources"`
	// CostUSD is what one instance search costs the balance; 0 when free.
	CostUSD float64 `json:"cost_usd"`
}

// MapConfig is the vector style the dashboard's map draws.
type MapConfig struct {
	StyleLight string `json:"style_light"`
	StyleDark  string `json:"style_dark"`
}

// Default map styles: OpenFreeMap, free and keyless. Operators can host their own.
const (
	DefaultMapStyleLight = "https://tiles.openfreemap.org/styles/positron"
	DefaultMapStyleDark  = "https://tiles.openfreemap.org/styles/dark"
)

// FinderConfig lists the providers this workspace can search and how.
func (s *Service) FinderConfig(ctx context.Context, orgID uuid.UUID) *FinderConfig {
	conns := s.connections(ctx, orgID)
	out := &FinderConfig{Categories: Categories, DefaultProvider: string(models.LeadProviderOSM), ExportColumns: ExportColumns, PersonColumns: PersonColumns, DefaultColumns: DefaultExportColumns}
	pageCost := 0.0
	if s.metered() {
		out.EnrichCost = s.Credits.Quote(ctx, credits.UnitUsage(instancesettings.UnitLeadWebsiteRead, 1)).USD()
		out.PersonEmailCost = s.Credits.Quote(ctx, credits.UnitUsage(instancesettings.UnitLeadPersonEmail, 1)).USD()
		pageCost = s.Credits.Quote(ctx, credits.UnitUsage(instancesettings.UnitLeadSearchPage, 1)).USD()
	}
	for _, id := range models.LeadProviders {
		info := providerMeta[id]
		info.ID = id
		info.KeySource = "none"
		if id == models.LeadProviderOSM {
			info.Available = true
		} else {
			info.NeedsKey = true
			info.Integration = string(integrationFor[id])
			switch {
			case s.workspaceKey(ctx, orgID, id, conns) != "":
				info.Available, info.KeySource = true, "workspace"
			case s.instanceKey(id) != "":
				info.Available, info.KeySource = true, "instance"
				info.CostUSD = pageCost
			}
		}
		out.Providers = append(out.Providers, info)
	}
	out.DefaultProvider = string(defaultProvider(out.Providers))
	out.WebReader, out.WebSearch = s.fallbacks(ctx, orgID)
	out.Map = MapConfig{StyleLight: s.Config.MapStyleLight, StyleDark: s.Config.MapStyleDark}
	if out.Map.StyleLight == "" {
		out.Map.StyleLight = DefaultMapStyleLight
	}
	if out.Map.StyleDark == "" {
		out.Map.StyleDark = DefaultMapStyleDark
	}
	return out
}

// defaultProvider is the richest provider available, OpenStreetMap at worst.
func defaultProvider(ps []ProviderInfo) models.LeadProvider {
	for _, id := range []models.LeadProvider{models.LeadProviderGooglePlaces, models.LeadProviderOutscraper, models.LeadProviderFoursquare} {
		for _, p := range ps {
			if p.ID == id && p.Available {
				return id
			}
		}
	}
	return models.LeadProviderOSM
}

// Geocode looks up a typed location.
func (s *Service) Geocode(ctx context.Context, q string, near *models.LatLng, lang string) ([]GeocodeResult, error) {
	if len([]rune(q)) > 200 {
		return nil, errx.New(errx.BadRequest, "the location is too long")
	}
	res, err := s.geocoder.Search(ctx, q, near, 6, lang)
	if err != nil {
		log.Warn().Err(err).Msg("leadfinder: geocoding failed")
		return nil, errx.New(errx.ServiceUnavailable, "Places could not be looked up right now. Try again shortly.")
	}
	return res, nil
}

// SearchRequest starts a search or fetches the next page of one.
type SearchRequest struct {
	SearchID      *uuid.UUID          `json:"search_id,omitempty"`
	Provider      models.LeadProvider `json:"provider"`
	Query         string              `json:"query"`
	Category      string              `json:"category"`
	LocationLabel string              `json:"location_label"`
	Area          models.LeadArea     `json:"area"`
	Save          bool                `json:"save"`
	Name          string              `json:"name"`
	Language      string              `json:"language"`
}

// SearchResult is one page of a search.
type SearchResult struct {
	Search      *models.LeadSearch `json:"search"`
	Leads       []models.Lead      `json:"leads"`
	Found       int                `json:"found"`
	OutsideArea int                `json:"outside_area"`
	ChargedUSD  float64            `json:"charged_usd"`
}

// Search runs a provider search over an area and stores what it returns.
func (s *Service) Search(ctx context.Context, orgID, userID uuid.UUID, req SearchRequest) (*SearchResult, error) {
	var search *models.LeadSearch
	if req.SearchID != nil {
		got, err := s.Repo.GetSearch(ctx, orgID, *req.SearchID)
		if err != nil {
			return nil, err
		}
		if got == nil {
			return nil, errx.New(errx.NotFound, "search not found")
		}
		// A search with no page recorded fetches its first page again.
		if got.NextCursor == "" && got.PagesFetched > 0 {
			return nil, errx.NewWithIdentifier(errx.BadRequest, "lead_search_exhausted", "This search has no more results")
		}
		search = got
	} else {
		var err error
		if search, err = s.newSearch(orgID, userID, req); err != nil {
			return nil, err
		}
	}
	var category *Category
	if search.Category != "" {
		category = CategoryByID(search.Category)
	}
	provider, keySource, err := s.resolve(ctx, orgID, search.Provider)
	if err != nil {
		return nil, err
	}
	if search.PagesFetched >= providerMeta[search.Provider].MaxPages {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "lead_search_exhausted", "This search has no more results")
	}

	var charged models.Micro
	chargeKey := ""
	if keySource == "instance" && s.metered() {
		chargeKey = fmt.Sprintf("lead_search:%s:%d", search.ID, search.PagesFetched)
		taken, err := s.Credits.ChargeUpfront(ctx, orgID, credits.UnitUsage(instancesettings.UnitLeadSearchPage, 1), "lead_search", chargeKey)
		if err != nil {
			return nil, creditErr(err)
		}
		charged = taken
	}
	pctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	page, err := provider.Search(pctx, Query{
		Text: search.Query, Category: category, Area: search.Area, Cursor: search.NextCursor,
		Limit: providerMeta[search.Provider].PageSize, Language: req.Language,
	})
	refund := func() {
		if chargeKey == "" {
			return
		}
		if _, rerr := s.Credits.RefundCharge(context.WithoutCancel(ctx), orgID, chargeKey, "lead_search_refund"); rerr != nil {
			log.Warn().Err(rerr).Msg("leadfinder: could not refund a failed search")
		}
	}
	if err != nil {
		refund()
		return nil, providerErr(search.Provider, err)
	}
	// A search is kept once its source answered, so a failed one leaves nothing behind.
	if req.SearchID == nil {
		if err := s.Repo.CreateSearch(ctx, search); err != nil {
			refund()
			return nil, err
		}
	}

	res := &SearchResult{Search: search, ChargedUSD: charged.USD()}
	keep := make([]models.Lead, 0, len(page.Leads))
	seen := map[string]bool{}
	for _, l := range page.Leads {
		if l.ExternalID == "" || seen[l.ExternalID] {
			continue
		}
		seen[l.ExternalID] = true
		if !Contains(search.Area, models.LatLng{Lat: l.Latitude, Lng: l.Longitude}) {
			res.OutsideArea++
			continue
		}
		keep = append(keep, sanitizeLead(l))
	}
	ids, err := s.Repo.UpsertLeads(ctx, orgID, &search.ID, keep)
	if err != nil {
		return nil, err
	}
	search.PagesFetched++
	search.NextCursor = page.NextCursor
	if search.PagesFetched >= providerMeta[search.Provider].MaxPages {
		search.NextCursor = ""
	}
	if err := s.Repo.RecordSearchPage(ctx, search); err != nil {
		return nil, err
	}
	search.HasMore = search.NextCursor != ""
	res.Found = len(ids)
	if res.Leads, err = s.Repo.LeadsByIDs(ctx, orgID, ids); err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Service) newSearch(orgID, userID uuid.UUID, req SearchRequest) (*models.LeadSearch, error) {
	if req.Provider == "" {
		req.Provider = models.LeadProviderOSM
	} else if !req.Provider.Valid() {
		return nil, errx.New(errx.BadRequest, "unknown lead provider")
	}
	req.Query = strings.TrimSpace(req.Query)
	if len([]rune(req.Query)) > 120 {
		return nil, errx.New(errx.BadRequest, "the search is too long; keep it under 120 characters")
	}
	if req.Category != "" && CategoryByID(req.Category) == nil {
		return nil, errx.New(errx.BadRequest, "unknown category")
	}
	if req.Query == "" && req.Category == "" {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "lead_search_empty", "Pick a category or type what to look for")
	}
	area, err := NormalizeArea(req.Area)
	if err != nil {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "lead_search_area", err.Error())
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = searchName(req.Query, req.Category, req.LocationLabel)
	}
	uid := userID
	return &models.LeadSearch{
		ID:              uuid.New(),
		OrganizationID:  orgID,
		CreatedByUserID: &uid,
		Name:            truncate(name, 120),
		Provider:        req.Provider,
		Query:           req.Query,
		Category:        req.Category,
		LocationLabel:   truncate(strings.TrimSpace(req.LocationLabel), 200),
		Area:            area,
		Saved:           req.Save,
	}, nil
}

func searchName(query, category, location string) string {
	what := query
	if c := CategoryByID(category); c != nil {
		what = strings.TrimSpace(c.Label + " " + query)
	}
	if location = strings.TrimSpace(location); location != "" {
		return what + " in " + location
	}
	return what
}

// sanitizeLead bounds every free-text field a provider sent.
func sanitizeLead(l models.Lead) models.Lead {
	l.Name = truncate(l.Name, 300)
	l.Category = truncate(l.Category, 120)
	cats := make([]string, 0, len(l.Categories))
	seenCat := map[string]bool{}
	for _, c := range l.Categories {
		c = truncate(strings.TrimSpace(c), 120)
		if c != "" && !seenCat[strings.ToLower(c)] && len(cats) < 12 {
			seenCat[strings.ToLower(c)] = true
			cats = append(cats, c)
		}
	}
	l.Categories = cats
	l.Address = truncate(l.Address, 500)
	l.Street, l.City, l.Region = truncate(l.Street, 200), truncate(l.City, 120), truncate(l.Region, 120)
	l.Postcode, l.Country = truncate(l.Postcode, 32), truncate(l.Country, 80)
	l.Phone = truncate(l.Phone, 64)
	l.Website, l.MapsURL = truncate(l.Website, 1000), truncate(l.MapsURL, 1000)
	l.Description = truncate(l.Description, 2000)
	l.Chain = truncate(l.Chain, 200)
	if len(l.Hours) > 14 {
		l.Hours = l.Hours[:14]
	}
	for i := range l.Hours {
		l.Hours[i] = truncate(l.Hours[i], 200)
	}
	return l
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func creditErr(err error) error {
	switch {
	case errors.Is(err, credits.ErrInsufficientCredits):
		return errx.NewWithIdentifier(errx.PaymentRequired, "insufficient_credits", "The workspace balance is too low. Add money under Billing to keep going.")
	case errors.Is(err, credits.ErrCapExceeded):
		return errx.NewWithIdentifier(errx.TooManyRequests, "usage_cap_exceeded", "The workspace reached its spend limit for now.")
	}
	return err
}

func providerErr(id models.LeadProvider, err error) error {
	name := providerMeta[id].Name
	switch {
	case errors.Is(err, ErrProviderKey):
		return errx.NewWithIdentifier(errx.BadRequest, "lead_provider_key", name+" refused the API key. Check it under Integrations.")
	case errors.Is(err, ErrProviderQuota):
		return errx.NewWithIdentifier(errx.TooManyRequests, "lead_provider_quota", name+" is rate limiting or out of quota. Try again later.")
	case errors.Is(err, ErrAreaTooLarge):
		return errx.NewWithIdentifier(errx.BadRequest, "lead_search_area", "Zoom in: the area is too large to search at once.")
	case errors.Is(err, context.DeadlineExceeded):
		return errx.NewWithIdentifier(errx.ServiceUnavailable, "lead_provider_unavailable", name+" took too long. Try a smaller area.")
	}
	log.Warn().Err(err).Str("provider", name).Msg("leadfinder: provider search failed")
	return errx.NewWithIdentifier(errx.ServiceUnavailable, "lead_provider_unavailable", name+" could not be reached. Try again shortly.")
}

// --- leads ------------------------------------------------------------------

// List returns a page of leads and how many match.
func (s *Service) List(ctx context.Context, orgID uuid.UUID, f *models.LeadFilter, sort models.LeadSort, limit, offset int) ([]models.Lead, int64, error) {
	return s.Repo.ListLeads(ctx, orgID, f, sort, limit, offset)
}

// Points returns the pins for the map.
func (s *Service) Points(ctx context.Context, orgID uuid.UUID, f *models.LeadFilter) ([]repository.LeadPoint, error) {
	return s.Repo.LeadPoints(ctx, orgID, f, PointsCap)
}

// Facets returns the filter values for the scope.
func (s *Service) Facets(ctx context.Context, orgID uuid.UUID, f *models.LeadFilter) (*repository.LeadFacets, error) {
	return s.Repo.LeadFacets(ctx, orgID, f)
}

// Get returns one lead.
func (s *Service) Get(ctx context.Context, orgID, id uuid.UUID) (*models.Lead, error) {
	l, err := s.Repo.GetLead(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, errx.New(errx.NotFound, "lead not found")
	}
	people, err := s.Repo.PeopleForLeads(ctx, orgID, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	l.People = people[id]
	if l.People == nil {
		l.People = []models.LeadPerson{}
	}
	return l, nil
}

// Resolve turns a selection into lead ids within the cap.
func (s *Service) Resolve(ctx context.Context, orgID uuid.UUID, sel *models.LeadSelection) ([]uuid.UUID, error) {
	if sel == nil || (!sel.All && len(sel.LeadIDs) == 0) {
		return nil, errx.New(errx.BadRequest, "select at least one lead")
	}
	if !sel.All && len(sel.LeadIDs) > SelectionCap {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "lead_selection_too_large", "Select at most 5,000 leads at a time")
	}
	ids, err := s.Repo.ResolveSelection(ctx, orgID, sel, SelectionCap)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, errx.New(errx.NotFound, "none of the selected leads were found")
	}
	return ids, nil
}

// Patch edits one or many leads.
func (s *Service) Patch(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID, p *models.LeadPatch) (int64, error) {
	if p.Status != nil && !p.Status.Valid() {
		return 0, errx.New(errx.BadRequest, "unknown lead status")
	}
	if p.Notes != nil && len([]rune(*p.Notes)) > 5000 {
		return 0, errx.New(errx.BadRequest, "notes are limited to 5,000 characters")
	}
	clean := func(tags []string) ([]string, error) {
		out := make([]string, 0, len(tags))
		for _, t := range tags {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			if len([]rune(t)) > 40 {
				return nil, errx.New(errx.BadRequest, "a tag is limited to 40 characters")
			}
			out = append(out, t)
		}
		if len(out) > 20 {
			return nil, errx.New(errx.BadRequest, "a lead takes at most 20 tags")
		}
		return out, nil
	}
	var err error
	if p.Tags != nil {
		t, cerr := clean(*p.Tags)
		if cerr != nil {
			return 0, cerr
		}
		p.Tags = &t
	}
	if p.AddTags, err = clean(p.AddTags); err != nil {
		return 0, err
	}
	if p.RemoveTags, err = clean(p.RemoveTags); err != nil {
		return 0, err
	}
	return s.Repo.PatchLeads(ctx, orgID, ids, p)
}

// Delete removes leads. Contacts made from them stay.
func (s *Service) Delete(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) (int64, error) {
	return s.Repo.DeleteLeads(ctx, orgID, ids)
}

// --- searches ---------------------------------------------------------------

// Searches lists recent and saved searches.
func (s *Service) Searches(ctx context.Context, orgID uuid.UUID, savedOnly bool) ([]models.LeadSearch, error) {
	return s.Repo.ListSearches(ctx, orgID, savedOnly, 50)
}

// UpdateSearch renames a search or saves it.
func (s *Service) UpdateSearch(ctx context.Context, orgID, id uuid.UUID, name *string, saved *bool) (*models.LeadSearch, error) {
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" || len([]rune(n)) > 120 {
			return nil, errx.New(errx.BadRequest, "a search name is 1 to 120 characters")
		}
		name = &n
	}
	out, err := s.Repo.UpdateSearch(ctx, orgID, id, name, saved)
	if errors.Is(err, repository.ErrLeadSearchNotFound) {
		return nil, errx.New(errx.NotFound, "search not found")
	}
	return out, err
}

// DeleteSearch removes a search. Its leads stay.
func (s *Service) DeleteSearch(ctx context.Context, orgID, id uuid.UUID) error {
	ok, err := s.Repo.DeleteSearch(ctx, orgID, id)
	if err != nil {
		return err
	}
	if !ok {
		return errx.New(errx.NotFound, "search not found")
	}
	return nil
}

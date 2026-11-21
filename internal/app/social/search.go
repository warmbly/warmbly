package social

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// Outreach states a hit is annotated with.
const (
	OutreachNone          = "none"
	OutreachInvitePending = "invite_pending"
	OutreachConnected     = "connected"
	OutreachMessaged      = "messaged"
	OutreachReplied       = "replied"
)

const (
	hitTTL       = time.Hour
	hitsPerOrg   = 5000
	hitCacheOrgs = 2000
	maxPrompt    = 2000
	// aiEstimate is what the search builder is reserved at before GraphKit reports its cost.
	aiEstimate = models.Micro(5_000)
)

// Marks is what Warmbly knows about a hit.
type Marks struct {
	ContactID       *uuid.UUID                 `json:"contact_id"`
	Campaigns       []models.SocialCampaignRef `json:"campaigns"`
	Outreach        string                     `json:"outreach"`
	CompanyContacts int                        `json:"company_contacts"`
}

// Hit is one search result with Warmbly's marks.
type Hit struct {
	graphkit.Hit
	Warmbly *Marks `json:"warmbly"`
}

// AccountRef names the account a search ran on.
type AccountRef struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
}

// SearchPage is POST /v1/social/search.
type SearchPage struct {
	Data        []Hit                `json:"data"`
	NextCursor  *string              `json:"next_cursor"`
	Total       *int                 `json:"total"`
	Cap         int                  `json:"cap"`
	EndReason   *string              `json:"end_reason"`
	Account     *AccountRef          `json:"account"`
	Usage       graphkit.SearchUsage `json:"usage"`
	LinkedInURL *string              `json:"linkedin_url"`
}

// SearchRequest is POST /v1/social/search.
type SearchRequest struct {
	AccountID *uuid.UUID      `json:"account_id"`
	Query     json.RawMessage `json:"query"`
	Cursor    *string         `json:"cursor"`
	Limit     int             `json:"limit"`
}

type cachedHit struct {
	hit graphkit.Hit
	at  time.Time
}

type hitCache struct {
	byKey map[string]cachedHit
	used  time.Time
}

// requireSearch refuses search to a workspace with neither a connected account nor a linked GraphKit workspace.
func (s *Service) requireSearch(ctx context.Context, orgID uuid.UUID) error {
	if err := s.ready(ctx, orgID); err != nil {
		return err
	}
	mode, err := s.mode(ctx, orgID)
	if err != nil {
		return err
	}
	if mode == models.SocialModeLinked {
		return nil
	}
	n, err := s.Repo.CountAccounts(ctx, orgID)
	if err != nil {
		return err
	}
	if n == 0 {
		return newErr(errx.Conflict, CodeAccountUnavailable, "Connect a LinkedIn account to search.")
	}
	return nil
}

func validQuery(q json.RawMessage) bool {
	q = bytes.TrimSpace(q)
	return len(q) > 1 && q[0] == '{' && json.Valid(q)
}

// Catalog is the search sources, their filters and caps.
func (s *Service) Catalog(ctx context.Context, orgID uuid.UUID) (json.RawMessage, error) {
	if err := s.ready(ctx, orgID); err != nil {
		return nil, err
	}
	raw, err := s.api().SearchCatalog(ctx, ext(orgID))
	if err != nil {
		return nil, s.upstream(ctx, "social.search.catalog", err)
	}
	var body struct {
		Sources json.RawMessage `json:"sources"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || len(body.Sources) == 0 {
		body.Sources = json.RawMessage(`[]`)
	}
	return mustJSON(map[string]json.RawMessage{"data": body.Sources}), nil
}

// Search runs a query at GraphKit and marks each hit with the workspace's contacts, campaigns and conversations.
func (s *Service) Search(ctx context.Context, orgID uuid.UUID, in SearchRequest) (*SearchPage, error) {
	if !validQuery(in.Query) {
		return nil, errBadQuery
	}
	if in.Limit < 0 || in.Limit > maxLimit {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "invalid_limit", "limit must be between 1 and 100.")
	}
	if err := s.requireSearch(ctx, orgID); err != nil {
		return nil, err
	}
	gk, err := s.graphkitAccount(ctx, orgID, in.AccountID)
	if err != nil {
		return nil, err
	}
	page, err := s.api().SearchPeople(ctx, ext(orgID), graphkit.SearchInput{AccountID: gk, Query: in.Query, Cursor: in.Cursor, Limit: in.Limit}, opKey("search"))
	if err != nil {
		return nil, s.upstream(ctx, "social.search", err)
	}
	s.remember(orgID, page.Data)
	out := &SearchPage{Data: s.annotate(ctx, orgID, page.Data), NextCursor: page.NextCursor, Total: page.Total, Cap: page.Cap,
		EndReason: page.EndReason, Usage: page.Usage, LinkedInURL: page.LinkedInURL}
	if page.Account != nil {
		if a, err := s.Repo.GetAccountByGraphKitID(ctx, orgID, page.Account.ID); err == nil && a != nil {
			out.Account = &AccountRef{ID: a.ID, DisplayName: a.DisplayName}
		}
	}
	return out, nil
}

// remember keeps a page's hits so an import of them needs no second search.
func (s *Service) remember(orgID uuid.UUID, hits []graphkit.Hit) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.hits[orgID]
	if c == nil {
		if len(s.hits) >= hitCacheOrgs {
			for id, old := range s.hits {
				if now.Sub(old.used) > hitTTL || len(s.hits) >= hitCacheOrgs {
					delete(s.hits, id)
				}
				if len(s.hits) < hitCacheOrgs {
					break
				}
			}
		}
		c = &hitCache{byKey: map[string]cachedHit{}}
		s.hits[orgID] = c
	}
	c.used = now
	if len(c.byKey)+len(hits) > hitsPerOrg {
		for k, h := range c.byKey {
			if now.Sub(h.at) > hitTTL || len(c.byKey)+len(hits) > hitsPerOrg {
				delete(c.byKey, k)
			}
		}
	}
	for _, h := range hits {
		if h.Key != "" {
			c.byKey[h.Key] = cachedHit{hit: h, at: now}
		}
	}
}

func (s *Service) recall(orgID uuid.UUID, key string) (graphkit.Hit, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.hits[orgID]
	if c == nil {
		return graphkit.Hit{}, false
	}
	h, ok := c.byKey[key]
	if !ok || s.now().Sub(h.at) > hitTTL {
		return graphkit.Hit{}, false
	}
	return h.hit, true
}

func hitPublicID(h *graphkit.Hit) string {
	if h.Person == nil {
		return ""
	}
	if id := strings.ToLower(strings.TrimSpace(h.Person.PublicID)); id != "" {
		return id
	}
	return models.LinkedInPublicID(h.Person.ProfileURL)
}

func hitCompany(h *graphkit.Hit) string {
	if h.Insights != nil && h.Insights.CurrentCompany != "" {
		return h.Insights.CurrentCompany
	}
	if h.Company != nil {
		return h.Company.Name
	}
	return ""
}

// annotate marks hits; a lookup that fails leaves the marks empty rather than failing the search.
func (s *Service) annotate(ctx context.Context, orgID uuid.UUID, hits []graphkit.Hit) []Hit {
	var ids, companies []string
	for i := range hits {
		if id := hitPublicID(&hits[i]); id != "" {
			ids = append(ids, id)
		}
		if c := hitCompany(&hits[i]); c != "" {
			companies = append(companies, c)
		}
	}
	matches, err := s.Repo.ContactMatches(ctx, orgID, ids)
	if err != nil {
		matches = nil
	}
	convs, err := s.Repo.ConversationStates(ctx, orgID, ids)
	if err != nil {
		convs = nil
	}
	counts, err := s.Repo.CompanyContacts(ctx, orgID, companies)
	if err != nil {
		counts = nil
	}
	invites, err := s.Repo.InviteStates(ctx, orgID, ids)
	if err != nil {
		invites = nil
	}
	out := make([]Hit, len(hits))
	for i := range hits {
		h := &hits[i]
		m := &Marks{Campaigns: []models.SocialCampaignRef{}, Outreach: OutreachNone}
		id := hitPublicID(h)
		match, matched := matches[id]
		if matched {
			cid := match.ContactID
			m.ContactID = &cid
			m.Campaigns = match.Campaigns
		}
		replied, conversed := convs[id]
		switch {
		case (conversed && replied) || match.Replied:
			m.Outreach = OutreachReplied
		case conversed || match.Contacted:
			m.Outreach = OutreachMessaged
		case (h.Person != nil && h.Person.NetworkDistance == "1st") || invites[id]:
			m.Outreach = OutreachConnected
		default:
			if _, pending := invites[id]; pending {
				m.Outreach = OutreachInvitePending
			}
		}
		if c := hitCompany(h); c != "" {
			m.CompanyContacts = counts[strings.ToLower(strings.TrimSpace(c))]
		}
		out[i] = Hit{Hit: *h, Warmbly: m}
	}
	return out
}

// TypeaheadItem is one suggestion for a filter.
type TypeaheadItem struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Label string `json:"label"`
}

// Typeahead suggests values for a filter.
func (s *Service) Typeahead(ctx context.Context, orgID uuid.UUID, source, filter, q string, accountID *uuid.UUID) ([]TypeaheadItem, error) {
	if source == "" || filter == "" || len(q) > 200 {
		return nil, invalid("source and filter are required, and q is at most 200 characters.")
	}
	if err := s.requireSearch(ctx, orgID); err != nil {
		return nil, err
	}
	gk, err := s.graphkitAccount(ctx, orgID, accountID)
	if err != nil {
		return nil, err
	}
	var acct string
	if gk != nil {
		acct = *gk
	}
	items, err := s.api().Typeahead(ctx, ext(orgID), source, filter, q, acct)
	if err != nil {
		return nil, s.upstream(ctx, "social.search.typeahead", err)
	}
	out := make([]TypeaheadItem, len(items))
	for i, it := range items {
		out[i] = TypeaheadItem{ID: it.ID, Name: it.Name, Label: it.Name}
	}
	return out, nil
}

// ParseURL turns a LinkedIn search address into a query.
func (s *Service) ParseURL(ctx context.Context, orgID uuid.UUID, rawURL string) (json.RawMessage, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" || len(rawURL) > 8000 {
		return nil, invalid("url must be a LinkedIn search address.")
	}
	if err := s.ready(ctx, orgID); err != nil {
		return nil, err
	}
	raw, err := s.api().ParseSearchURL(ctx, ext(orgID), rawURL, opKey("search.parse"))
	if err != nil {
		return nil, s.upstream(ctx, "social.search.parse_url", err)
	}
	return raw, nil
}

// AIRequest is POST /v1/social/search/ai.
type AIRequest struct {
	Prompt       string          `json:"prompt"`
	Source       string          `json:"source"`
	CurrentQuery json.RawMessage `json:"current_query"`
}

// BuildQuery has GraphKit turn a description into a query, charged to the workspace's AI balance at its cost.
func (s *Service) BuildQuery(ctx context.Context, orgID uuid.UUID, in AIRequest, key string) (*graphkit.AIResult, error) {
	in.Prompt = strings.TrimSpace(in.Prompt)
	if in.Prompt == "" || utf8.RuneCountInString(in.Prompt) > maxPrompt {
		return nil, invalid("prompt must be 1 to 2000 characters.")
	}
	if len(in.CurrentQuery) > 0 && string(bytes.TrimSpace(in.CurrentQuery)) != "null" && !validQuery(in.CurrentQuery) {
		return nil, errBadQuery
	}
	if err := s.ready(ctx, orgID); err != nil {
		return nil, err
	}
	if err := s.Credits.Reserve(ctx, orgID, aiEstimate); err != nil {
		return nil, err
	}
	if key == "" {
		key = uuid.NewString()
	}
	chargeKey := "social:ai:" + orgID.String() + ":" + key
	res, err := s.api().BuildQuery(ctx, ext(orgID), graphkit.AIInput{Prompt: in.Prompt, Source: in.Source, CurrentQuery: in.CurrentQuery}, chargeKey)
	if err != nil {
		return nil, s.upstream(ctx, "social.search.ai", err)
	}
	if res.CostMicros > 0 {
		// The query is already built; a charge that fails is logged, not turned into a failed request.
		if _, err := s.Credits.ChargeAICost(context.WithoutCancel(ctx), orgID, models.Micro(res.CostMicros), "LinkedIn search builder", chargeKey); err != nil {
			log.Warn().Err(err).Str("org_id", orgID.String()).Int64("cost_micros", res.CostMicros).Msg("social: charging the search builder's AI cost failed")
		}
	}
	return res, nil
}

// Profile is one LinkedIn profile.
func (s *Service) Profile(ctx context.Context, orgID uuid.UUID, identifier string, accountID *uuid.UUID) (*graphkit.Profile, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" || len(identifier) > 500 {
		return nil, invalid("identifier is a LinkedIn profile address or public id.")
	}
	if err := s.requireSearch(ctx, orgID); err != nil {
		return nil, err
	}
	gk, err := s.graphkitAccount(ctx, orgID, accountID)
	if err != nil {
		return nil, err
	}
	var acct string
	if gk != nil {
		acct = *gk
	}
	p, err := s.api().GetProfile(ctx, ext(orgID), identifier, acct)
	if err != nil {
		return nil, s.upstream(ctx, "social.profile", err)
	}
	return p, nil
}

// ---- saved searches -------------------------------------------------------

// SavedSearchInput creates or changes a saved search.
type SavedSearchInput struct {
	Name  *string         `json:"name"`
	Query json.RawMessage `json:"query"`
}

func cleanSavedName(name *string) (*string, error) {
	if name == nil {
		return nil, nil
	}
	n := strings.TrimSpace(*name)
	if n == "" || utf8.RuneCountInString(n) > 120 {
		return nil, invalid("name must be 1 to 120 characters.")
	}
	return &n, nil
}

// ListSavedSearches is the workspace's saved searches, newest first.
func (s *Service) ListSavedSearches(ctx context.Context, orgID uuid.UUID, cursor string, limit int) (*List[models.LinkedInSavedSearch], error) {
	after, limit, err := pageArgs(cursor, limit)
	if err != nil {
		return nil, err
	}
	rows, err := s.Repo.ListSavedSearches(ctx, orgID, after, limit+1)
	if err != nil {
		return nil, err
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	out := &List[models.LinkedInSavedSearch]{Data: rows}
	if out.Data == nil {
		out.Data = []models.LinkedInSavedSearch{}
	}
	if more {
		last := rows[len(rows)-1]
		out.Pagination = nextPage(true, last.CreatedAt, last.ID)
	}
	return out, nil
}

// CreateSavedSearch saves a search.
func (s *Service) CreateSavedSearch(ctx context.Context, orgID, userID uuid.UUID, in SavedSearchInput) (*models.LinkedInSavedSearch, error) {
	name, err := cleanSavedName(in.Name)
	if err != nil {
		return nil, err
	}
	if name == nil {
		return nil, invalid("name must be 1 to 120 characters.")
	}
	if !validQuery(in.Query) || len(in.Query) > 64<<10 {
		return nil, errBadQuery
	}
	row := &models.LinkedInSavedSearch{ID: uuid.New(), OrganizationID: orgID, Name: *name, Query: in.Query, CreatedBy: &userID}
	if err := s.Repo.CreateSavedSearch(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// UpdateSavedSearch renames a saved search or replaces its query.
func (s *Service) UpdateSavedSearch(ctx context.Context, orgID, id uuid.UUID, in SavedSearchInput) (*models.LinkedInSavedSearch, error) {
	name, err := cleanSavedName(in.Name)
	if err != nil {
		return nil, err
	}
	if len(in.Query) > 0 && (!validQuery(in.Query) || len(in.Query) > 64<<10) {
		return nil, errBadQuery
	}
	row, err := s.Repo.UpdateSavedSearch(ctx, orgID, id, name, in.Query)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, errNotFound
	}
	return row, nil
}

// DeleteSavedSearch deletes a saved search.
func (s *Service) DeleteSavedSearch(ctx context.Context, orgID, id uuid.UUID) error {
	ok, err := s.Repo.DeleteSavedSearch(ctx, orgID, id)
	if err != nil {
		return err
	}
	if !ok {
		return errNotFound
	}
	return nil
}

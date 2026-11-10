package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// ErrLeadSearchNotFound means the search is not in the organization.
var ErrLeadSearchNotFound = errors.New("lead search not found")

// ErrLeadNotFound is returned when a lead is not the organization's.
var ErrLeadNotFound = errors.New("lead not found")

// LeadPoint is the slice of a lead the map draws.
type LeadPoint struct {
	ID        uuid.UUID         `json:"id"`
	Name      string            `json:"name"`
	Category  string            `json:"category"`
	Latitude  float64           `json:"latitude"`
	Longitude float64           `json:"longitude"`
	Status    models.LeadStatus `json:"status"`
	Score     int               `json:"score"`
	HasEmail  bool              `json:"has_email"`
	HasSite   bool              `json:"has_website"`
	HasPhone  bool              `json:"has_phone"`
	Rating    *float64          `json:"rating"`
	Reviews   *int              `json:"review_count"`
	InContact bool              `json:"in_contacts"`
}

// LeadFacet is one value of a filterable field and how many leads carry it.
type LeadFacet struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// LeadFacets are the values the filter panel offers for the current scope.
type LeadFacets struct {
	Categories []LeadFacet `json:"categories"`
	Tags       []LeadFacet `json:"tags"`
	Cities     []LeadFacet `json:"cities"`
}

// LeadFinderRepository persists leads, the searches that found them and the
// runs that read their websites. Every org-scoped call takes the organization.
type LeadFinderRepository interface {
	UpsertLeads(ctx context.Context, orgID uuid.UUID, searchID *uuid.UUID, leads []models.Lead) ([]uuid.UUID, error)
	GetLead(ctx context.Context, orgID, id uuid.UUID) (*models.Lead, error)
	LeadsByIDs(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) ([]models.Lead, error)
	ListLeads(ctx context.Context, orgID uuid.UUID, f *models.LeadFilter, sort models.LeadSort, limit, offset int) ([]models.Lead, int64, error)
	LeadPoints(ctx context.Context, orgID uuid.UUID, f *models.LeadFilter, limit int) ([]LeadPoint, error)
	LeadFacets(ctx context.Context, orgID uuid.UUID, f *models.LeadFilter) (*LeadFacets, error)
	ResolveSelection(ctx context.Context, orgID uuid.UUID, sel *models.LeadSelection, limit int) ([]uuid.UUID, error)
	PatchLeads(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID, p *models.LeadPatch) (int64, error)
	DeleteLeads(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) (int64, error)
	MarkQueued(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) error
	SaveEnrichment(ctx context.Context, l *models.Lead) error
	LinkContacts(ctx context.Context, orgID uuid.UUID, links map[uuid.UUID]uuid.UUID) error
	SuppressedEmails(ctx context.Context, orgID uuid.UUID, emails []string) (map[string]bool, error)
	SaveSite(ctx context.Context, orgID, leadID uuid.UUID, site models.LeadSite) error

	PeopleForLeads(ctx context.Context, orgID uuid.UUID, leadIDs []uuid.UUID) (map[uuid.UUID][]models.LeadPerson, error)
	GetPerson(ctx context.Context, orgID, leadID, id uuid.UUID) (*models.LeadPerson, error)
	SavePeople(ctx context.Context, orgID, leadID uuid.UUID, people []models.LeadPerson) ([]models.LeadPerson, error)
	DeletePerson(ctx context.Context, orgID, leadID, id uuid.UUID) (bool, error)
	LinkPersonContacts(ctx context.Context, orgID uuid.UUID, links map[uuid.UUID]uuid.UUID) error

	CreateSearch(ctx context.Context, s *models.LeadSearch) error
	GetSearch(ctx context.Context, orgID, id uuid.UUID) (*models.LeadSearch, error)
	ListSearches(ctx context.Context, orgID uuid.UUID, savedOnly bool, limit int) ([]models.LeadSearch, error)
	RecordSearchPage(ctx context.Context, s *models.LeadSearch) error
	UpdateSearch(ctx context.Context, orgID, id uuid.UUID, name *string, saved *bool) (*models.LeadSearch, error)
	DeleteSearch(ctx context.Context, orgID, id uuid.UUID) (bool, error)

	CreateEnrichmentRun(ctx context.Context, run *models.LeadEnrichmentRun) error
	GetEnrichmentRun(ctx context.Context, orgID, id uuid.UUID) (*models.LeadEnrichmentRun, error)
	ActiveEnrichmentRuns(ctx context.Context, orgID uuid.UUID) ([]models.LeadEnrichmentRun, error)
	ClaimEnrichmentRun(ctx context.Context, id uuid.UUID) (*models.LeadEnrichmentRun, error)
	DueEnrichmentRuns(ctx context.Context, limit int) ([]uuid.UUID, error)
	UpdateEnrichmentProgress(ctx context.Context, run *models.LeadEnrichmentRun) error
	FinishEnrichmentRun(ctx context.Context, run *models.LeadEnrichmentRun) error
}

type leadFinderRepository struct {
	db *pgxpool.Pool
}

// NewLeadFinderRepository builds the Postgres lead finder repository.
func NewLeadFinderRepository(db *pgxpool.Pool) LeadFinderRepository {
	return &leadFinderRepository{db: db}
}

// --- leads ------------------------------------------------------------------

const leadColumns = `l.id, l.organization_id, l.provider, l.external_id, l.name, l.category, l.categories, l.address,
	l.street, l.city, l.region, l.postcode, l.country, l.latitude, l.longitude, l.phone, l.website, l.domain,
	l.email, l.emails, l.socials, l.rating, l.review_count, l.price_level, l.hours, l.business_status, l.maps_url,
	l.description, l.chain, l.site, l.score, l.status, l.enrichment_status, l.enriched_at, l.contact_id, l.tags,
	l.notes, l.first_seen_at, l.last_seen_at, l.created_at, l.updated_at,
	(SELECT COUNT(*) FROM lead_people lp WHERE lp.lead_id = l.id)`

// leadScoreSQL rates how reachable a lead is, 0 to 100, from the row itself.
const leadScoreSQL = `LEAST(100,
	(CASE WHEN website <> '' THEN 15 ELSE 0 END) +
	(CASE WHEN email <> '' THEN 25 ELSE 0 END) +
	(CASE WHEN EXISTS (SELECT 1 FROM jsonb_array_elements(emails) e WHERE e->>'email' = email AND e->>'status' = 'valid') THEN 15 ELSE 0 END) +
	(CASE WHEN phone <> '' THEN 15 ELSE 0 END) +
	(CASE WHEN socials <> '{}'::jsonb THEN 10 ELSE 0 END) +
	(CASE WHEN rating >= 4 THEN 10 WHEN rating >= 3 THEN 5 ELSE 0 END) +
	(CASE WHEN review_count >= 25 THEN 10 WHEN review_count >= 5 THEN 5 ELSE 0 END))`

func scanLead(row pgx.Row) (models.Lead, error) {
	var l models.Lead
	var emails, socials, hours, site []byte
	var rating *float64
	var reviews, price *int32
	err := row.Scan(&l.ID, &l.OrganizationID, &l.Provider, &l.ExternalID, &l.Name, &l.Category, &l.Categories,
		&l.Address, &l.Street, &l.City, &l.Region, &l.Postcode, &l.Country, &l.Latitude, &l.Longitude, &l.Phone,
		&l.Website, &l.Domain, &l.Email, &emails, &socials, &rating, &reviews, &price, &hours, &l.BusinessStatus,
		&l.MapsURL, &l.Description, &l.Chain, &site, &l.Score, &l.Status, &l.EnrichmentStatus, &l.EnrichedAt,
		&l.ContactID, &l.Tags, &l.Notes, &l.FirstSeenAt, &l.LastSeenAt, &l.CreatedAt, &l.UpdatedAt, &l.PeopleCount)
	if err != nil {
		return l, err
	}
	l.Rating = rating
	if reviews != nil {
		v := int(*reviews)
		l.ReviewCount = &v
	}
	if price != nil {
		v := int(*price)
		l.PriceLevel = &v
	}
	l.Emails = []models.LeadEmail{}
	l.Socials = map[string]string{}
	l.Hours = []string{}
	_ = json.Unmarshal(emails, &l.Emails)
	_ = json.Unmarshal(socials, &l.Socials)
	_ = json.Unmarshal(hours, &l.Hours)
	_ = json.Unmarshal(site, &l.Site)
	if l.Categories == nil {
		l.Categories = []string{}
	}
	if l.Tags == nil {
		l.Tags = []string{}
	}
	return l, nil
}

func jsonOr(v any, empty string) []byte {
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return []byte(empty)
	}
	return b
}

// UpsertLeads writes what a provider returned. A field the provider left empty
// never blanks one already known, and nothing a person set is touched.
func (r *leadFinderRepository) UpsertLeads(ctx context.Context, orgID uuid.UUID, searchID *uuid.UUID, leads []models.Lead) ([]uuid.UUID, error) {
	if len(leads) == 0 {
		return []uuid.UUID{}, nil
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	batch := &pgx.Batch{}
	for i := range leads {
		l := &leads[i]
		cats := l.Categories
		if cats == nil {
			cats = []string{}
		}
		batch.Queue(`
			INSERT INTO leads (organization_id, provider, external_id, name, category, categories, address, street,
				city, region, postcode, country, latitude, longitude, phone, website, domain, email, emails, socials,
				rating, review_count, price_level, hours, business_status, maps_url, description, chain)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28)
			ON CONFLICT (organization_id, provider, external_id) DO UPDATE SET
				name = COALESCE(NULLIF(EXCLUDED.name, ''), leads.name),
				category = COALESCE(NULLIF(EXCLUDED.category, ''), leads.category),
				categories = CASE WHEN cardinality(EXCLUDED.categories) > 0 THEN EXCLUDED.categories ELSE leads.categories END,
				address = COALESCE(NULLIF(EXCLUDED.address, ''), leads.address),
				street = COALESCE(NULLIF(EXCLUDED.street, ''), leads.street),
				city = COALESCE(NULLIF(EXCLUDED.city, ''), leads.city),
				region = COALESCE(NULLIF(EXCLUDED.region, ''), leads.region),
				postcode = COALESCE(NULLIF(EXCLUDED.postcode, ''), leads.postcode),
				country = COALESCE(NULLIF(EXCLUDED.country, ''), leads.country),
				latitude = EXCLUDED.latitude,
				longitude = EXCLUDED.longitude,
				phone = COALESCE(NULLIF(EXCLUDED.phone, ''), leads.phone),
				website = COALESCE(NULLIF(EXCLUDED.website, ''), leads.website),
				domain = COALESCE(NULLIF(EXCLUDED.domain, ''), leads.domain),
				email = COALESCE(NULLIF(leads.email, ''), EXCLUDED.email),
				emails = CASE WHEN leads.emails = '[]'::jsonb THEN EXCLUDED.emails ELSE leads.emails END,
				socials = leads.socials || EXCLUDED.socials,
				rating = COALESCE(EXCLUDED.rating, leads.rating),
				review_count = COALESCE(EXCLUDED.review_count, leads.review_count),
				price_level = COALESCE(EXCLUDED.price_level, leads.price_level),
				hours = CASE WHEN EXCLUDED.hours <> '[]'::jsonb THEN EXCLUDED.hours ELSE leads.hours END,
				business_status = COALESCE(NULLIF(EXCLUDED.business_status, ''), leads.business_status),
				maps_url = COALESCE(NULLIF(EXCLUDED.maps_url, ''), leads.maps_url),
				description = COALESCE(NULLIF(EXCLUDED.description, ''), leads.description),
				chain = COALESCE(NULLIF(EXCLUDED.chain, ''), leads.chain),
				last_seen_at = NOW(),
				updated_at = NOW()
			RETURNING id`,
			orgID, l.Provider, l.ExternalID, l.Name, l.Category, cats, l.Address, l.Street, l.City, l.Region,
			l.Postcode, l.Country, l.Latitude, l.Longitude, l.Phone, l.Website, l.Domain, l.Email,
			jsonOr(l.Emails, "[]"), jsonOr(l.Socials, "{}"), l.Rating, l.ReviewCount, l.PriceLevel,
			jsonOr(l.Hours, "[]"), l.BusinessStatus, l.MapsURL, l.Description, l.Chain)
	}
	res := tx.SendBatch(ctx, batch)
	ids := make([]uuid.UUID, 0, len(leads))
	for range leads {
		var id uuid.UUID
		if err := res.QueryRow().Scan(&id); err != nil {
			_ = res.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := res.Close(); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE leads SET score = `+leadScoreSQL+` WHERE id = ANY($1)`, ids); err != nil {
		return nil, err
	}
	if searchID != nil {
		if _, err := tx.Exec(ctx, `
			INSERT INTO lead_search_hits (search_id, lead_id)
			SELECT $1, UNNEST($2::uuid[])
			ON CONFLICT (search_id, lead_id) DO UPDATE SET seen_at = NOW()`, *searchID, ids); err != nil {
			return nil, err
		}
	}
	return ids, tx.Commit(ctx)
}

func (r *leadFinderRepository) GetLead(ctx context.Context, orgID, id uuid.UUID) (*models.Lead, error) {
	l, err := scanLead(r.db.QueryRow(ctx, `SELECT `+leadColumns+` FROM leads l WHERE l.organization_id = $1 AND l.id = $2`, orgID, id))
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (r *leadFinderRepository) LeadsByIDs(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) ([]models.Lead, error) {
	out := []models.Lead{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT `+leadColumns+` FROM leads l WHERE l.organization_id = $1 AND l.id = ANY($2)
		ORDER BY array_position($2::uuid[], l.id)`, orgID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		l, err := scanLead(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// leadWhere renders a filter as a WHERE clause over leads l. $1 is always the
// organization; further arguments are appended to args.
func leadWhere(orgID uuid.UUID, f *models.LeadFilter) (string, []any) {
	args := []any{orgID}
	conds := []string{"l.organization_id = $1"}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	boolCond := func(v *bool, yes string) {
		if v == nil {
			return
		}
		if *v {
			conds = append(conds, yes)
		} else {
			conds = append(conds, "NOT ("+yes+")")
		}
	}
	if f == nil {
		return strings.Join(conds, " AND "), args
	}
	if f.SearchID != nil {
		conds = append(conds, "EXISTS (SELECT 1 FROM lead_search_hits h WHERE h.lead_id = l.id AND h.search_id = "+arg(*f.SearchID)+")")
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		p := arg("%" + escapeLike(q) + "%")
		conds = append(conds, "(l.name ILIKE "+p+" OR l.address ILIKE "+p+" OR l.domain ILIKE "+p+" OR l.email ILIKE "+p+" OR l.category ILIKE "+p+")")
	}
	if len(f.Statuses) > 0 {
		s := make([]string, 0, len(f.Statuses))
		for _, v := range f.Statuses {
			s = append(s, string(v))
		}
		conds = append(conds, "l.status = ANY("+arg(s)+")")
	}
	if len(f.Providers) > 0 {
		s := make([]string, 0, len(f.Providers))
		for _, v := range f.Providers {
			s = append(s, string(v))
		}
		conds = append(conds, "l.provider = ANY("+arg(s)+")")
	}
	if len(f.Enrichment) > 0 {
		s := make([]string, 0, len(f.Enrichment))
		for _, v := range f.Enrichment {
			s = append(s, string(v))
		}
		conds = append(conds, "l.enrichment_status = ANY("+arg(s)+")")
	}
	if c := strings.TrimSpace(f.Category); c != "" {
		p := arg(c)
		conds = append(conds, "(l.category = "+p+" OR "+p+" = ANY(l.categories))")
	}
	if t := strings.TrimSpace(f.Tag); t != "" {
		conds = append(conds, arg(t)+" = ANY(l.tags)")
	}
	boolCond(f.HasEmail, "l.email <> ''")
	boolCond(f.HasWebsite, "l.website <> ''")
	boolCond(f.HasPhone, "l.phone <> ''")
	boolCond(f.HasSocials, "l.socials <> '{}'::jsonb")
	boolCond(f.InContacts, "l.contact_id IS NOT NULL")
	boolCond(f.HasPeople, "EXISTS (SELECT 1 FROM lead_people lp WHERE lp.lead_id = l.id)")
	if f.MinRating != nil {
		conds = append(conds, "l.rating >= "+arg(*f.MinRating))
	}
	if f.MinReviews != nil {
		conds = append(conds, "COALESCE(l.review_count, 0) >= "+arg(*f.MinReviews))
	}
	if f.MaxReviews != nil {
		conds = append(conds, "COALESCE(l.review_count, 0) <= "+arg(*f.MaxReviews))
	}
	if f.MinScore != nil {
		conds = append(conds, "l.score >= "+arg(*f.MinScore))
	}
	if f.ExcludeChain {
		conds = append(conds, "l.chain = ''")
	}
	if b := f.Bounds; b != nil {
		conds = append(conds, "l.latitude BETWEEN "+arg(b.South)+" AND "+arg(b.North))
		if b.West <= b.East {
			conds = append(conds, "l.longitude BETWEEN "+arg(b.West)+" AND "+arg(b.East))
		} else {
			conds = append(conds, "(l.longitude >= "+arg(b.West)+" OR l.longitude <= "+arg(b.East)+")")
		}
	}
	return strings.Join(conds, " AND "), args
}

func leadOrder(sort models.LeadSort) string {
	switch sort {
	case models.LeadSortScore:
		return "l.score DESC, l.last_seen_at DESC, l.id DESC"
	case models.LeadSortRating:
		return "l.rating DESC NULLS LAST, l.review_count DESC NULLS LAST, l.id DESC"
	case models.LeadSortReviews:
		return "l.review_count DESC NULLS LAST, l.id DESC"
	case models.LeadSortName:
		return "lower(l.name), l.id"
	}
	return "l.last_seen_at DESC, l.id DESC"
}

func (r *leadFinderRepository) ListLeads(ctx context.Context, orgID uuid.UUID, f *models.LeadFilter, sort models.LeadSort, limit, offset int) ([]models.Lead, int64, error) {
	where, args := leadWhere(orgID, f)
	var total int64
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM leads l WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	rows, err := r.db.Query(ctx, fmt.Sprintf(`SELECT %s FROM leads l WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d`,
		leadColumns, where, leadOrder(sort), len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.Lead{}
	for rows.Next() {
		l, err := scanLead(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, l)
	}
	return out, total, rows.Err()
}

func (r *leadFinderRepository) LeadPoints(ctx context.Context, orgID uuid.UUID, f *models.LeadFilter, limit int) ([]LeadPoint, error) {
	where, args := leadWhere(orgID, f)
	args = append(args, limit)
	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT l.id, l.name, l.category, l.latitude, l.longitude, l.status, l.score, l.email <> '', l.website <> '',
			l.phone <> '', l.rating, l.review_count, l.contact_id IS NOT NULL
		FROM leads l WHERE %s ORDER BY l.score DESC, l.id LIMIT $%d`, where, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LeadPoint{}
	for rows.Next() {
		var p LeadPoint
		var reviews *int32
		if err := rows.Scan(&p.ID, &p.Name, &p.Category, &p.Latitude, &p.Longitude, &p.Status, &p.Score, &p.HasEmail,
			&p.HasSite, &p.HasPhone, &p.Rating, &reviews, &p.InContact); err != nil {
			return nil, err
		}
		if reviews != nil {
			v := int(*reviews)
			p.Reviews = &v
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *leadFinderRepository) LeadFacets(ctx context.Context, orgID uuid.UUID, f *models.LeadFilter) (*LeadFacets, error) {
	where, args := leadWhere(orgID, f)
	facet := func(expr string) ([]LeadFacet, error) {
		rows, err := r.db.Query(ctx, `SELECT v, COUNT(*) FROM (SELECT `+expr+` AS v FROM leads l WHERE `+where+`) s
			WHERE v <> '' GROUP BY v ORDER BY COUNT(*) DESC, v LIMIT 50`, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []LeadFacet{}
		for rows.Next() {
			var fc LeadFacet
			if err := rows.Scan(&fc.Value, &fc.Count); err != nil {
				return nil, err
			}
			out = append(out, fc)
		}
		return out, rows.Err()
	}
	var out LeadFacets
	var err error
	// Counts the primary and secondary categories once per lead, matching the category filter.
	if out.Categories, err = facet("UNNEST(ARRAY(SELECT DISTINCT c FROM UNNEST(array_prepend(l.category, l.categories)) c))"); err != nil {
		return nil, err
	}
	if out.Tags, err = facet("UNNEST(l.tags)"); err != nil {
		return nil, err
	}
	if out.Cities, err = facet("l.city"); err != nil {
		return nil, err
	}
	return &out, nil
}

// ResolveSelection turns a selection into the organization's lead ids, at most limit.
func (r *leadFinderRepository) ResolveSelection(ctx context.Context, orgID uuid.UUID, sel *models.LeadSelection, limit int) ([]uuid.UUID, error) {
	var rows pgx.Rows
	var err error
	if sel.All {
		where, args := leadWhere(orgID, sel.Filter)
		args = append(args, limit)
		rows, err = r.db.Query(ctx, fmt.Sprintf(`SELECT l.id FROM leads l WHERE %s ORDER BY l.score DESC, l.id LIMIT $%d`, where, len(args)), args...)
	} else {
		if len(sel.LeadIDs) == 0 {
			return []uuid.UUID{}, nil
		}
		rows, err = r.db.Query(ctx, `SELECT id FROM leads WHERE organization_id = $1 AND id = ANY($2)
			ORDER BY array_position($2::uuid[], id) LIMIT $3`, orgID, sel.LeadIDs, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *leadFinderRepository) PatchLeads(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID, p *models.LeadPatch) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var status, notes *string
	if p.Status != nil {
		s := string(*p.Status)
		status = &s
	}
	notes = p.Notes
	var tags []string
	if p.Tags != nil {
		tags = *p.Tags
		if tags == nil {
			tags = []string{}
		}
	}
	add := p.AddTags
	if add == nil {
		add = []string{}
	}
	remove := p.RemoveTags
	if remove == nil {
		remove = []string{}
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE leads SET
			status = COALESCE($3, status),
			notes = COALESCE($4, notes),
			tags = ARRAY(
				SELECT DISTINCT t FROM UNNEST(COALESCE($5::text[], tags) || $6::text[]) AS t
				WHERE t <> '' AND NOT (t = ANY($7::text[])) ORDER BY t),
			updated_at = NOW()
		WHERE organization_id = $1 AND id = ANY($2)`, orgID, ids, status, notes, tags, add, remove)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *leadFinderRepository) DeleteLeads(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := r.db.Exec(ctx, `DELETE FROM leads WHERE organization_id = $1 AND id = ANY($2)`, orgID, ids)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *leadFinderRepository) MarkQueued(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE leads SET enrichment_status = 'queued', updated_at = NOW()
		WHERE organization_id = $1 AND id = ANY($2)`, orgID, ids)
	return err
}

// SaveEnrichment stores what reading a lead's website found and rescores it.
func (r *leadFinderRepository) SaveEnrichment(ctx context.Context, l *models.Lead) error {
	_, err := r.db.Exec(ctx, `
		UPDATE leads SET
			email = $3, emails = $4, socials = $5, site = $6, phone = COALESCE(NULLIF($7, ''), phone), enrichment_status = $8,
			website = COALESCE(NULLIF($9, ''), website), domain = COALESCE(NULLIF($10, ''), domain),
			enriched_at = NOW(), updated_at = NOW()
		WHERE organization_id = $1 AND id = $2`,
		l.OrganizationID, l.ID, l.Email, jsonOr(l.Emails, "[]"), jsonOr(l.Socials, "{}"), jsonOr(l.Site, "{}"),
		l.Phone, l.EnrichmentStatus, l.Website, l.Domain)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `UPDATE leads SET score = `+leadScoreSQL+` WHERE id = $1`, l.ID)
	return err
}

// LinkContacts records which contact each lead became.
func (r *leadFinderRepository) LinkContacts(ctx context.Context, orgID uuid.UUID, links map[uuid.UUID]uuid.UUID) error {
	if len(links) == 0 {
		return nil
	}
	leadIDs := make([]uuid.UUID, 0, len(links))
	contactIDs := make([]uuid.UUID, 0, len(links))
	for l, c := range links {
		leadIDs = append(leadIDs, l)
		contactIDs = append(contactIDs, c)
	}
	_, err := r.db.Exec(ctx, `
		UPDATE leads l SET contact_id = m.contact_id,
			status = CASE WHEN l.status = 'new' THEN 'saved' ELSE l.status END, updated_at = NOW()
		FROM UNNEST($2::uuid[], $3::uuid[]) AS m(lead_id, contact_id)
		WHERE l.organization_id = $1 AND l.id = m.lead_id
		  AND EXISTS (SELECT 1 FROM contacts c WHERE c.id = m.contact_id AND c.organization_id = $1)`,
		orgID, leadIDs, contactIDs)
	return err
}

// SuppressedEmails reports which addresses the organization has suppressed,
// with the same predicate the send gates use.
func (r *leadFinderRepository) SuppressedEmails(ctx context.Context, orgID uuid.UUID, emails []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(emails) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT addr FROM UNNEST($2::text[]) AS a(addr) WHERE recipient_suppressed($1, addr)`, orgID, emails)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var addr string
		if err := rows.Scan(&addr); err != nil {
			return nil, err
		}
		out[strings.ToLower(addr)] = true
	}
	return out, rows.Err()
}

// --- searches ---------------------------------------------------------------

const leadSearchColumns = `id, organization_id, created_by_user_id, name, provider, query, category, location_label,
	area, saved, result_count, pages_fetched, next_cursor, last_run_at, created_at, updated_at`

func scanLeadSearch(row pgx.Row) (models.LeadSearch, error) {
	var s models.LeadSearch
	var area []byte
	err := row.Scan(&s.ID, &s.OrganizationID, &s.CreatedByUserID, &s.Name, &s.Provider, &s.Query, &s.Category,
		&s.LocationLabel, &area, &s.Saved, &s.ResultCount, &s.PagesFetched, &s.NextCursor, &s.LastRunAt,
		&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return s, err
	}
	_ = json.Unmarshal(area, &s.Area)
	s.HasMore = s.NextCursor != "" || s.PagesFetched == 0
	return s, nil
}

func (r *leadFinderRepository) CreateSearch(ctx context.Context, s *models.LeadSearch) error {
	out, err := scanLeadSearch(r.db.QueryRow(ctx, `
		INSERT INTO lead_searches (id, organization_id, created_by_user_id, name, provider, query, category, location_label, area, saved)
		VALUES (COALESCE($10, gen_random_uuid()),$1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING `+leadSearchColumns,
		s.OrganizationID, s.CreatedByUserID, s.Name, s.Provider, s.Query, s.Category, s.LocationLabel,
		jsonOr(s.Area, "{}"), s.Saved, nullableID(s.ID)))
	if err != nil {
		return err
	}
	*s = out
	return nil
}

func (r *leadFinderRepository) GetSearch(ctx context.Context, orgID, id uuid.UUID) (*models.LeadSearch, error) {
	s, err := scanLeadSearch(r.db.QueryRow(ctx, `SELECT `+leadSearchColumns+` FROM lead_searches WHERE organization_id = $1 AND id = $2`, orgID, id))
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *leadFinderRepository) ListSearches(ctx context.Context, orgID uuid.UUID, savedOnly bool, limit int) ([]models.LeadSearch, error) {
	rows, err := r.db.Query(ctx, `SELECT `+leadSearchColumns+` FROM lead_searches
		WHERE organization_id = $1 AND (saved OR NOT $2) ORDER BY saved DESC, last_run_at DESC LIMIT $3`, orgID, savedOnly, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.LeadSearch{}
	for rows.Next() {
		s, err := scanLeadSearch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// RecordSearchPage stores where a search's paging stands after a fetch.
func (r *leadFinderRepository) RecordSearchPage(ctx context.Context, s *models.LeadSearch) error {
	_, err := r.db.Exec(ctx, `
		UPDATE lead_searches SET
			result_count = (SELECT COUNT(*) FROM lead_search_hits WHERE search_id = $2),
			pages_fetched = $3, next_cursor = $4, last_run_at = NOW(), updated_at = NOW()
		WHERE organization_id = $1 AND id = $2`, s.OrganizationID, s.ID, s.PagesFetched, s.NextCursor)
	if err != nil {
		return err
	}
	return r.db.QueryRow(ctx, `SELECT result_count FROM lead_searches WHERE id = $1`, s.ID).Scan(&s.ResultCount)
}

func (r *leadFinderRepository) UpdateSearch(ctx context.Context, orgID, id uuid.UUID, name *string, saved *bool) (*models.LeadSearch, error) {
	s, err := scanLeadSearch(r.db.QueryRow(ctx, `
		UPDATE lead_searches SET name = COALESCE($3, name), saved = COALESCE($4, saved), updated_at = NOW()
		WHERE organization_id = $1 AND id = $2 RETURNING `+leadSearchColumns, orgID, id, name, saved))
	if isNoRows(err) {
		return nil, ErrLeadSearchNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *leadFinderRepository) DeleteSearch(ctx context.Context, orgID, id uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM lead_searches WHERE organization_id = $1 AND id = $2`, orgID, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// --- enrichment runs --------------------------------------------------------

const leadRunColumns = `id, organization_id, requested_by_user_id, lead_ids, verify, people, web_search, status, total, processed,
	emails_found, people_found, no_website, failed, charged_micro, last_error, started_at, finished_at, created_at, updated_at`

func scanLeadRun(row pgx.Row) (models.LeadEnrichmentRun, error) {
	var run models.LeadEnrichmentRun
	err := row.Scan(&run.ID, &run.OrganizationID, &run.RequestedByUserID, &run.LeadIDs, &run.Verify, &run.People, &run.WebSearch, &run.Status,
		&run.Total, &run.Processed, &run.EmailsFound, &run.PeopleFound, &run.NoWebsite, &run.Failed, &run.ChargedMicro,
		&run.LastError, &run.StartedAt, &run.FinishedAt, &run.CreatedAt, &run.UpdatedAt)
	run.ChargedUSD = run.ChargedMicro.USD()
	return run, err
}

func (r *leadFinderRepository) CreateEnrichmentRun(ctx context.Context, run *models.LeadEnrichmentRun) error {
	out, err := scanLeadRun(r.db.QueryRow(ctx, `
		INSERT INTO lead_enrichment_runs (organization_id, requested_by_user_id, lead_ids, verify, people, web_search, total)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING `+leadRunColumns,
		run.OrganizationID, run.RequestedByUserID, run.LeadIDs, run.Verify, run.People, run.WebSearch, run.Total))
	if err != nil {
		return err
	}
	*run = out
	return nil
}

func (r *leadFinderRepository) GetEnrichmentRun(ctx context.Context, orgID, id uuid.UUID) (*models.LeadEnrichmentRun, error) {
	run, err := scanLeadRun(r.db.QueryRow(ctx, `SELECT `+leadRunColumns+` FROM lead_enrichment_runs WHERE organization_id = $1 AND id = $2`, orgID, id))
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// ActiveEnrichmentRuns lists the organization's unfinished runs and those that
// finished in the last minute, so a dashboard sees a run through to the end.
func (r *leadFinderRepository) ActiveEnrichmentRuns(ctx context.Context, orgID uuid.UUID) ([]models.LeadEnrichmentRun, error) {
	rows, err := r.db.Query(ctx, `SELECT `+leadRunColumns+` FROM lead_enrichment_runs
		WHERE organization_id = $1 AND (status IN ('queued', 'running') OR finished_at > NOW() - INTERVAL '1 minute')
		ORDER BY created_at DESC LIMIT 20`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.LeadEnrichmentRun{}
	for rows.Next() {
		run, err := scanLeadRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// ClaimEnrichmentRun moves a queued run, or a running one nobody has touched
// for 15 minutes, to running and returns it; nil when someone else holds it.
func (r *leadFinderRepository) ClaimEnrichmentRun(ctx context.Context, id uuid.UUID) (*models.LeadEnrichmentRun, error) {
	run, err := scanLeadRun(r.db.QueryRow(ctx, `
		UPDATE lead_enrichment_runs SET status = 'running', started_at = COALESCE(started_at, NOW()), updated_at = NOW()
		WHERE id = $1 AND (status = 'queued' OR (status = 'running' AND updated_at < NOW() - INTERVAL '15 minutes'))
		RETURNING `+leadRunColumns, id))
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// DueEnrichmentRuns lists runs waiting to start or abandoned mid-way.
func (r *leadFinderRepository) DueEnrichmentRuns(ctx context.Context, limit int) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id FROM lead_enrichment_runs
		WHERE (status = 'queued' AND updated_at < NOW() - INTERVAL '1 minute')
		   OR (status = 'running' AND updated_at < NOW() - INTERVAL '15 minutes')
		ORDER BY created_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *leadFinderRepository) UpdateEnrichmentProgress(ctx context.Context, run *models.LeadEnrichmentRun) error {
	_, err := r.db.Exec(ctx, `
		UPDATE lead_enrichment_runs SET
			processed = $2, emails_found = $3, no_website = $4, failed = $5, charged_micro = $6, people_found = $7,
			updated_at = NOW()
		WHERE id = $1 AND status = 'running'`,
		run.ID, run.Processed, run.EmailsFound, run.NoWebsite, run.Failed, run.ChargedMicro, run.PeopleFound)
	return err
}

func (r *leadFinderRepository) FinishEnrichmentRun(ctx context.Context, run *models.LeadEnrichmentRun) error {
	_, err := r.db.Exec(ctx, `
		UPDATE lead_enrichment_runs SET
			status = $2, processed = $3, emails_found = $4, no_website = $5, failed = $6, charged_micro = $7,
			last_error = $8, people_found = $9, finished_at = NOW(), updated_at = NOW()
		WHERE id = $1`,
		run.ID, run.Status, run.Processed, run.EmailsFound, run.NoWebsite, run.Failed, run.ChargedMicro,
		truncateRunes(run.LastError, 500), run.PeopleFound)
	if err != nil {
		return err
	}
	// A lead the run never reached is no longer waiting on it, unless another open run holds it.
	_, err = r.db.Exec(ctx, `UPDATE leads l SET enrichment_status = 'none', updated_at = NOW()
		WHERE l.organization_id = $1 AND l.id = ANY($2) AND l.enrichment_status = 'queued'
		AND NOT EXISTS (SELECT 1 FROM lead_enrichment_runs o WHERE o.organization_id = $1 AND o.id <> $3
			AND o.status IN ('queued', 'running') AND l.id = ANY(o.lead_ids))`, run.OrganizationID, run.LeadIDs, run.ID)
	return err
}

func nullableID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

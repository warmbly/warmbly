package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/email"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/emailverify"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/utils"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

type ContactRepository interface {
	Add(ctx context.Context, userID string, orgID uuid.UUID, contacts []models.AddContact) ([]models.Contact, *errx.Error)
	GetByID(ctx context.Context, contactID uuid.UUID) (*models.Contact, *errx.Error)
	GetByEmailAndOrganization(ctx context.Context, organizationID uuid.UUID, email string) (*models.Contact, *errx.Error)
	// GetByIDsAndOrganization fetches the org's contacts for a set of IDs. Used
	// by the synchronous "push to CRM" action so a member can only push contacts
	// that belong to their organization. Foreign/missing IDs are omitted.
	GetByIDsAndOrganization(ctx context.Context, organizationID uuid.UUID, ids []uuid.UUID) ([]models.Contact, *errx.Error)
	// OwnerUserID returns the user that owns a contact (contacts.user_id), used
	// to route a per-user realtime event (e.g. "a lead booked a meeting"). nil
	// when the contact is missing or not in the org.
	OwnerUserID(ctx context.Context, organizationID, contactID uuid.UUID) (*uuid.UUID, error)

	// Pre-send email verification round-trip. UpdateContactVerification stores
	// the outcome of a verify pass; ListUnverifiedContacts returns contacts that
	// have never been conclusively checked (status 'unknown', never verified) so
	// the batch scheduler can work them off a cap per tick.
	UpdateContactVerification(ctx context.Context, contactID uuid.UUID, res emailverify.Result) *errx.Error
	ListUnverifiedContacts(ctx context.Context, limit int) ([]models.Contact, *errx.Error)
	// SetContactESP caches the recipient ESP/provider resolved from the contact's
	// domain (control-plane only, no MX dial). Best-effort: a failure should not
	// block sending.
	SetContactESP(ctx context.Context, contactID uuid.UUID, provider string) error
	GetByEmailsAndUser(ctx context.Context, userID uuid.UUID, emails []string) (map[string]models.Contact, *errx.Error)
	Search(ctx context.Context, userID string, category, cursor *string, filters models.SearchContacts, limit int32) (*models.ContactsResult, *errx.Error)
	// SearchCounts returns org-wide contact facet totals for the browse
	// sidebar (independent of any search filters), mirroring campaigns-overview.
	SearchCounts(ctx context.Context, orgID string) (*models.ContactsCounts, *errx.Error)
	// CampaignLeadCounts returns per-status lead totals for one campaign (the
	// Leads-view scope chips), independent of the request's lead_status filter.
	CampaignLeadCounts(ctx context.Context, orgID, campaignID string) (*models.CampaignLeadCounts, *errx.Error)
	ExportAll(ctx context.Context, userID string, filters *models.SearchContacts, contactIDs []string, max int) ([]models.Contact, *errx.Error)
	BulkUpdate(ctx context.Context, userID string, orgID uuid.UUID, data *models.BulkEditContactsData) ([]models.Contact, *errx.Error)
	Update(ctx context.Context, userID, contactID string, orgID uuid.UUID, data *models.UpdateContact) (*models.Contact, *errx.Error)
	BulkDelete(ctx context.Context, userID string, orgID uuid.UUID, contactIDs []string) *errx.Error
	Delete(ctx context.Context, userID string, orgID uuid.UUID, contactID string) *errx.Error
	GetContactCount(ctx context.Context, userID string) (int, *errx.Error)

	// 360 view read paths. orgID is optional — when nil, the suppression
	// + deliverability + reply joins are skipped (they're org-scoped).
	GetDetail(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, contactID uuid.UUID) (*models.ContactDetail, *errx.Error)
	ListSentEmails(ctx context.Context, userID, contactID uuid.UUID, limit int, beforeSentAt *time.Time, beforeTaskID *uuid.UUID) (*models.ContactSentEmailsResult, *errx.Error)
	ListTimeline(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, contactID uuid.UUID, limit int, before *time.Time) (*models.ContactTimelineResult, *errx.Error)
}

type contactRepository struct {
	DB      *db.DB
	Encrypt *encrypt.Encrypter
}

func NewContactRepostory(db *db.DB) ContactRepository {
	return &contactRepository{
		DB: db,
	}
}

// parseCategoryIDs accepts string IDs from a JSON body and returns a
// deduped slice of uuid.UUIDs. Empty strings are skipped silently —
// they're a normal artifact of clients sending [""] to "clear" a list.
// A malformed (non-UUID) entry is a client bug worth surfacing as 400.
func parseCategoryIDs(raw []string) ([]uuid.UUID, *errx.Error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[uuid.UUID]struct{}, len(raw))
	out := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, errx.ErrUuid
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

func (r *contactRepository) Add(ctx context.Context, userID string, orgID uuid.UUID, contacts []models.AddContact) ([]models.Contact, *errx.Error) {
	// Validate userID up front. The handler should have caught a
	// malformed JWT subject, but a defensive check here keeps any
	// invalid value from blowing up pgx as "InternalError 500".
	if _, perr := uuid.Parse(userID); perr != nil {
		return nil, errx.ErrUuid
	}

	// Normalize + validate every contact before opening a transaction.
	// Catching bad input here lets us return 400 instead of letting
	// pgx fail mid-batch (which used to surface as a generic 500).
	normalized := make([]models.AddContact, 0, len(contacts))
	campaignIDs := make([][]uuid.UUID, 0, len(contacts))
	categoryIDs := make([][]uuid.UUID, 0, len(contacts))
	for _, lead := range contacts {
		lead.Email = strings.TrimSpace(lead.Email)
		if !email.IsValid(lead.Email) {
			return nil, errx.ErrEmail
		}
		lead.FirstName = strings.TrimSpace(lead.FirstName)
		lead.LastName = strings.TrimSpace(lead.LastName)
		lead.Company = strings.TrimSpace(lead.Company)
		lead.Phone = strings.TrimSpace(lead.Phone)

		// JSONB column is NOT NULL; encoding a nil map sends NULL.
		// Replace nil with an empty map so the INSERT can't violate
		// the constraint.
		if lead.CustomFields == nil {
			lead.CustomFields = map[string]string{}
		}
		for key := range lead.CustomFields {
			if !utils.IsValidJSONKey(key) {
				return nil, errx.ErrJSONKey
			}
		}

		// Approximate size check using JSON payload.
		data, jerr := json.Marshal(lead)
		if jerr != nil {
			return nil, errx.ErrContactSerialize
		}
		if len(data) > config.MaxContactSize {
			return nil, errx.ErrContactSize
		}

		// Parse + dedupe campaign IDs. Skip blanks. Invalid UUIDs are
		// a user error → 400, not a server crash.
		cidSet := make(map[uuid.UUID]struct{}, len(lead.Campaigns))
		cids := make([]uuid.UUID, 0, len(lead.Campaigns))
		for _, raw := range lead.Campaigns {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			cid, cerr := uuid.Parse(raw)
			if cerr != nil {
				return nil, errx.ErrUuid
			}
			if _, dup := cidSet[cid]; dup {
				continue
			}
			cidSet[cid] = struct{}{}
			cids = append(cids, cid)
		}

		// Parse + dedupe category IDs. Same rules as campaigns.
		catSet := make(map[uuid.UUID]struct{}, len(lead.Categories))
		cats := make([]uuid.UUID, 0, len(lead.Categories))
		for _, raw := range lead.Categories {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			cid, cerr := uuid.Parse(raw)
			if cerr != nil {
				return nil, errx.ErrUuid
			}
			if _, dup := catSet[cid]; dup {
				continue
			}
			catSet[cid] = struct{}{}
			cats = append(cats, cid)
		}

		normalized = append(normalized, lead)
		campaignIDs = append(campaignIDs, cids)
		categoryIDs = append(categoryIDs, cats)
	}

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)

	// Upsert contacts in a single batch round-trip.
	insertBatch := pgx.Batch{}
	for _, lead := range normalized {
		insertBatch.Queue(
			`INSERT INTO contacts (
			 id, user_id, organization_id, first_name, last_name, email, company, phone, custom_fields
			 ) VALUES (
			  gen_random_uuid(), $1, $2, $3, $4, LOWER($5), $6, $7, $8
			 )
			 ON CONFLICT (user_id, (LOWER(email))) DO UPDATE SET
			  organization_id = COALESCE(contacts.organization_id, EXCLUDED.organization_id),
			  first_name = EXCLUDED.first_name,
			  last_name = EXCLUDED.last_name,
			  company = EXCLUDED.company,
			  phone = EXCLUDED.phone,
			  custom_fields = contacts.custom_fields || EXCLUDED.custom_fields,
			  updated_at = NOW()
			 RETURNING id, first_name, last_name, email, company, phone, custom_fields, subscribed, updated_at, created_at`,
			userID, orgID, lead.FirstName, lead.LastName, lead.Email, lead.Company, lead.Phone, lead.CustomFields,
		)
	}

	br := tx.SendBatch(ctx, &insertBatch)

	ncontacts := make([]models.Contact, 0, len(normalized))
	for range normalized {
		ncon := models.Contact{
			Campaigns:  []models.MiniCampaign{},
			Categories: []models.MiniCategory{},
			Subscribed: true,
		}
		if err := br.QueryRow().Scan(
			&ncon.ID, &ncon.FirstName, &ncon.LastName, &ncon.Email, &ncon.Company,
			&ncon.Phone, &ncon.CustomFields, &ncon.Subscribed, &ncon.UpdatedAt, &ncon.CreatedAt,
		); err != nil {
			br.Close()
			db.CaptureError(err, "", nil, "batch queryrow")
			return nil, errx.InternalError()
		}
		// Defensive: backend code occasionally returns nil custom_fields
		// from older rows. Normalize for the JSON response.
		if ncon.CustomFields == nil {
			ncon.CustomFields = map[string]string{}
		}
		ncontacts = append(ncontacts, ncon)
	}
	if err := br.Close(); err != nil {
		db.CaptureError(err, "", nil, "batch close")
		return nil, errx.InternalError()
	}

	// Link campaigns. Original code's RETURNING clause referenced a
	// non-inserted table, which is invalid SQL; resolve by inserting
	// first, then SELECTing the name back from `campaigns` in a
	// separate statement. Scoped to the user's own campaigns.
	for i, cids := range campaignIDs {
		if len(cids) == 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO campaign_leads (contact_id, campaign_id)
			SELECT $1, c.id
			FROM   campaigns c
			WHERE  c.id = ANY($2) AND c.user_id = $3
			ON CONFLICT (campaign_id, contact_id) DO NOTHING
		`, ncontacts[i].ID, cids, userID); err != nil {
			db.CaptureError(err, "", nil, "campaign_leads insert")
			return nil, errx.InternalError()
		}

		rows, err := tx.Query(ctx, `
			SELECT c.id, c.name
			FROM   campaigns c
			JOIN   campaign_leads cl ON cl.campaign_id = c.id
			WHERE  cl.contact_id = $1 AND c.user_id = $2
		`, ncontacts[i].ID, userID)
		if err != nil {
			db.CaptureError(err, "", nil, "campaign_leads select")
			return nil, errx.InternalError()
		}
		linked := make([]models.MiniCampaign, 0)
		for rows.Next() {
			var mc models.MiniCampaign
			if err := rows.Scan(&mc.ID, &mc.Name); err != nil {
				rows.Close()
				db.CaptureError(err, "", nil, "campaign scan")
				return nil, errx.InternalError()
			}
			linked = append(linked, mc)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			db.CaptureError(err, "", nil, "campaign rows")
			return nil, errx.InternalError()
		}
		ncontacts[i].Campaigns = linked
	}

	// Link categories. Scoped to the user's own categories so a
	// malicious or stale ID can't attach foreign data.
	for i, cats := range categoryIDs {
		if len(cats) == 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO contact_categories (contact_id, category_id)
			SELECT $1, cat.id
			FROM   categories cat
			WHERE  cat.id = ANY($2) AND cat.user_id = $3
			ON CONFLICT (contact_id, category_id) DO NOTHING
		`, ncontacts[i].ID, cats, userID); err != nil {
			db.CaptureError(err, "", nil, "contact_categories insert")
			return nil, errx.InternalError()
		}

		rows, err := tx.Query(ctx, `
			SELECT cat.id, cat.title, cat.color
			FROM   categories cat
			JOIN   contact_categories cc ON cc.category_id = cat.id
			WHERE  cc.contact_id = $1 AND cat.user_id = $2
			ORDER BY cat.position ASC, cat.title ASC
		`, ncontacts[i].ID, userID)
		if err != nil {
			db.CaptureError(err, "", nil, "contact_categories select")
			return nil, errx.InternalError()
		}
		linked := make([]models.MiniCategory, 0)
		for rows.Next() {
			var mc models.MiniCategory
			if err := rows.Scan(&mc.ID, &mc.Title, &mc.Color); err != nil {
				rows.Close()
				db.CaptureError(err, "", nil, "category scan")
				return nil, errx.InternalError()
			}
			linked = append(linked, mc)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			db.CaptureError(err, "", nil, "category rows")
			return nil, errx.InternalError()
		}
		ncontacts[i].Categories = linked
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, errx.InternalError()
	}

	return ncontacts, nil
}

// GetByID retrieves a contact by ID without requiring userID (for internal service use)
func (r *contactRepository) GetByID(ctx context.Context, contactID uuid.UUID) (*models.Contact, *errx.Error) {
	query := `
		SELECT
			c.id, c.first_name, c.last_name, c.email, c.company, c.phone,
			c.custom_fields, c.subscribed, c.updated_at, c.created_at,
			c.verification_status, c.verification_reason, c.is_catch_all, c.verification_checked_at,
			c.esp_provider, c.esp_resolved_at
		FROM contacts c
		WHERE c.id = $1
	`

	var contact models.Contact
	err := r.DB.QueryRow(ctx, query, contactID).Scan(
		&contact.ID, &contact.FirstName, &contact.LastName, &contact.Email,
		&contact.Company, &contact.Phone, &contact.CustomFields, &contact.Subscribed,
		&contact.UpdatedAt, &contact.CreatedAt,
		&contact.VerificationStatus, &contact.VerificationReason, &contact.IsCatchAll, &contact.VerificationCheckedAt,
		&contact.ESPProvider, &contact.ESPResolvedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, query, []any{contactID}, "queryrow")
		return nil, errx.InternalError()
	}

	contact.Campaigns = []models.MiniCampaign{}
	contact.Categories = []models.MiniCategory{}
	return &contact, nil
}

// SetContactESP caches the recipient ESP/provider on the contact row. It is a
// single keyed UPDATE and intentionally tolerant: callers treat any error as a
// best-effort cache miss and fall back to deriving the provider on the fly.
func (r *contactRepository) SetContactESP(ctx context.Context, contactID uuid.UUID, provider string) error {
	query := `
		UPDATE contacts
		SET esp_provider = $2, esp_resolved_at = NOW()
		WHERE id = $1
	`
	_, err := r.DB.Exec(ctx, query, contactID, provider)
	return err
}

// UpdateContactVerification stores the outcome of a verification pass on the
// contact. It is keyed only by contact id (the verifier runs in the control
// plane, not in a user request) and is a no-op-safe single UPDATE.
func (r *contactRepository) UpdateContactVerification(ctx context.Context, contactID uuid.UUID, res emailverify.Result) *errx.Error {
	status := string(res.Status)
	if status == "" {
		status = string(emailverify.StatusUnknown)
	}
	checkedAt := res.CheckedAt
	if checkedAt.IsZero() {
		checkedAt = time.Now().UTC()
	}

	query := `
		UPDATE contacts
		SET verification_status = $2,
		    verification_reason = $3,
		    is_catch_all = $4,
		    verification_checked_at = $5,
		    updated_at = NOW()
		WHERE id = $1
	`
	params := []any{contactID, status, res.Reason, res.IsCatchAll, checkedAt}
	cmd, err := r.DB.Exec(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}
	if cmd.RowsAffected() == 0 {
		return errx.ErrNotFound
	}
	return nil
}

// ListUnverifiedContacts returns up to `limit` contacts that have never been
// conclusively verified (status 'unknown' and no recorded check). Oldest
// contacts first so a backlog drains in creation order. The pre-send gate only
// drops 'invalid', so 'risky'/'valid'/already-checked rows are intentionally
// excluded here — they don't need re-verification on every tick.
func (r *contactRepository) ListUnverifiedContacts(ctx context.Context, limit int) ([]models.Contact, *errx.Error) {
	if limit <= 0 {
		limit = 100
	}
	query := `
		SELECT
			c.id, c.first_name, c.last_name, c.email, c.company, c.phone,
			c.custom_fields, c.subscribed, c.updated_at, c.created_at,
			c.verification_status, c.verification_reason, c.is_catch_all, c.verification_checked_at
		FROM contacts c
		WHERE c.verification_status = 'unknown' AND c.verification_checked_at IS NULL
		ORDER BY c.created_at ASC
		LIMIT $1
	`
	rows, err := r.DB.Query(ctx, query, limit)
	if err != nil {
		db.CaptureError(err, query, []any{limit}, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	out := make([]models.Contact, 0, limit)
	for rows.Next() {
		var c models.Contact
		if err := rows.Scan(
			&c.ID, &c.FirstName, &c.LastName, &c.Email,
			&c.Company, &c.Phone, &c.CustomFields, &c.Subscribed,
			&c.UpdatedAt, &c.CreatedAt,
			&c.VerificationStatus, &c.VerificationReason, &c.IsCatchAll, &c.VerificationCheckedAt,
		); err != nil {
			db.CaptureError(err, "", nil, "ListUnverifiedContacts scan")
			return nil, errx.InternalError()
		}
		c.Campaigns = []models.MiniCampaign{}
		c.Categories = []models.MiniCategory{}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, "", nil, "ListUnverifiedContacts rows")
		return nil, errx.InternalError()
	}
	return out, nil
}

func (r *contactRepository) GetByEmailAndOrganization(ctx context.Context, organizationID uuid.UUID, email string) (*models.Contact, *errx.Error) {
	query := `
		SELECT
			c.id, c.first_name, c.last_name, c.email, c.company, c.phone,
			c.custom_fields, c.subscribed, c.updated_at, c.created_at
		FROM contacts c
		WHERE c.organization_id = $1
		  AND LOWER(c.email) = LOWER($2)
		ORDER BY c.updated_at DESC
		LIMIT 1
	`

	var contact models.Contact
	err := r.DB.QueryRow(ctx, query, organizationID, strings.TrimSpace(email)).Scan(
		&contact.ID, &contact.FirstName, &contact.LastName, &contact.Email,
		&contact.Company, &contact.Phone, &contact.CustomFields, &contact.Subscribed,
		&contact.UpdatedAt, &contact.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		db.CaptureError(err, query, []any{organizationID, email}, "queryrow")
		return nil, errx.InternalError()
	}
	contact.Campaigns = []models.MiniCampaign{}
	contact.Categories = []models.MiniCategory{}
	return &contact, nil
}

func (r *contactRepository) OwnerUserID(ctx context.Context, organizationID, contactID uuid.UUID) (*uuid.UUID, error) {
	var userID uuid.UUID
	err := r.DB.QueryRow(ctx,
		`SELECT user_id FROM contacts WHERE id = $1 AND organization_id = $2`,
		contactID, organizationID,
	).Scan(&userID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &userID, nil
}

func (r *contactRepository) GetByIDsAndOrganization(ctx context.Context, organizationID uuid.UUID, ids []uuid.UUID) ([]models.Contact, *errx.Error) {
	if len(ids) == 0 {
		return []models.Contact{}, nil
	}
	query := `
		SELECT
			c.id, c.first_name, c.last_name, c.email, c.company, c.phone,
			c.custom_fields, c.subscribed, c.updated_at, c.created_at
		FROM contacts c
		WHERE c.organization_id = $1 AND c.id = ANY($2)
	`
	rows, err := r.DB.Query(ctx, query, organizationID, ids)
	if err != nil {
		db.CaptureError(err, query, []any{organizationID, ids}, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	out := make([]models.Contact, 0, len(ids))
	for rows.Next() {
		var c models.Contact
		if err := rows.Scan(
			&c.ID, &c.FirstName, &c.LastName, &c.Email,
			&c.Company, &c.Phone, &c.CustomFields, &c.Subscribed,
			&c.UpdatedAt, &c.CreatedAt,
		); err != nil {
			db.CaptureError(err, "", nil, "GetByIDsAndOrganization scan")
			return nil, errx.InternalError()
		}
		c.Campaigns = []models.MiniCampaign{}
		c.Categories = []models.MiniCategory{}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, "", nil, "GetByIDsAndOrganization rows")
		return nil, errx.InternalError()
	}
	return out, nil
}

func (r *contactRepository) Search(
	ctx context.Context,
	orgID string,
	category,
	cursor *string,
	filters models.SearchContacts,
	limit int32,
) (*models.ContactsResult, *errx.Error) {
	var whereClauses []string
	var args []any
	argIndex := 1

	// -----------------------------
	// Base filter: user_id
	// -----------------------------
	whereClauses = append(whereClauses, fmt.Sprintf("c.organization_id = $%d", argIndex))
	args = append(args, orgID)
	argIndex++

	// -----------------------------
	// Text search across core fields
	// -----------------------------
	if filters.Query != "" {
		q := "%" + filters.Query + "%"
		whereClauses = append(whereClauses, fmt.Sprintf(`
			(c.first_name ILIKE $%d OR
			 c.last_name ILIKE $%d OR
			 c.email ILIKE $%d OR
			 c.company ILIKE $%d OR
			 c.phone ILIKE $%d)
		`, argIndex, argIndex+1, argIndex+2, argIndex+3, argIndex+4))
		args = append(args, q, q, q, q, q)
		argIndex += 5
	}

	// -----------------------------
	// Custom field filters (JSONB)
	// -----------------------------
	for _, f := range filters.CustomFieldFilters {
		if f.Name == "" || f.Value == "" || !utils.IsValidJSONKey(f.Name) {
			continue
		}
		var op, val string
		switch f.Type {
		case models.SearchContactsFilterTypeEqual:
			op = "="
			val = f.Value
		case models.SearchContactsFilterTypeStartsWith:
			op = "ILIKE"
			val = f.Value + "%"
		case models.SearchContactsFilterTypeEndsWith:
			op = "ILIKE"
			val = "%" + f.Value
		case models.SearchContactsFilterTypeContains:
			op = "ILIKE"
			val = "%" + f.Value + "%"
		default:
			op = "ILIKE"
			val = "%" + f.Value + "%"
		}
		whereClauses = append(whereClauses, fmt.Sprintf(`c.custom_fields ->> '%s' %s $%d`, f.Name, op, argIndex))
		args = append(args, val)
		argIndex++
	}

	// -----------------------------
	// Subscription filter
	// -----------------------------
	if filters.Subscribed != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("c.subscribed = $%d", argIndex))
		args = append(args, *filters.Subscribed)
		argIndex++
	}

	// -----------------------------
	// Date filters
	// -----------------------------
	if filters.CreatedAfter != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("c.created_at > $%d", argIndex))
		args = append(args, *filters.CreatedAfter)
		argIndex++
	}
	if filters.CreatedBefore != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("c.created_at < $%d", argIndex))
		args = append(args, *filters.CreatedBefore)
		argIndex++
	}
	if filters.UpdatedAfter != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("c.updated_at > $%d", argIndex))
		args = append(args, *filters.UpdatedAfter)
		argIndex++
	}
	if filters.UpdatedBefore != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("c.updated_at < $%d", argIndex))
		args = append(args, *filters.UpdatedBefore)
		argIndex++
	}

	// -----------------------------
	// Campaign IDs filter (must be in ALL specified campaigns)
	// -----------------------------
	// When filtering by exactly one campaign (the campaign Leads view), we also
	// surface each contact's per-campaign processing state. Capture that single
	// campaign's bound placeholder so the progress subquery can reuse it without
	// appending another arg.
	singleCampaignPlaceholder := ""
	if len(filters.CampaignIDs) > 0 {
		placeholders := make([]string, len(filters.CampaignIDs))
		for i, id := range filters.CampaignIDs {
			placeholders[i] = fmt.Sprintf("$%d", argIndex)
			args = append(args, id)
			argIndex++
		}
		if len(filters.CampaignIDs) == 1 {
			singleCampaignPlaceholder = placeholders[0]
		}
		campaignClause := fmt.Sprintf(`
			c.id IN (
				SELECT contact_id
				FROM campaign_leads
				WHERE campaign_id IN (%s)
				GROUP BY contact_id
				HAVING COUNT(DISTINCT campaign_id) = %d
			)
		`, strings.Join(placeholders, ","), len(filters.CampaignIDs))
		whereClauses = append(whereClauses, campaignClause)
	}

	// -----------------------------
	// Lead status filter (single-campaign Leads view only)
	// -----------------------------
	// The derived status has no stored column, so reproduce the same priority
	// chain used on read (unsubscribed > bounced > replied > processing >
	// queued) as a boolean predicate over the campaign's progress rows. Only
	// meaningful with exactly one campaign bound; ignored otherwise.
	if filters.LeadStatus != "" && singleCampaignPlaceholder != "" {
		if clause := leadStatusClause(filters.LeadStatus, singleCampaignPlaceholder); clause != "" {
			whereClauses = append(whereClauses, clause)
		}
	}

	// -----------------------------
	// Category IDs filter (must have ALL specified categories)
	// -----------------------------
	if len(filters.CategoryIDs) > 0 {
		placeholders := make([]string, len(filters.CategoryIDs))
		for i, id := range filters.CategoryIDs {
			placeholders[i] = fmt.Sprintf("$%d", argIndex)
			args = append(args, id)
			argIndex++
		}
		categoryClause := fmt.Sprintf(`
			c.id IN (
				SELECT contact_id
				FROM contact_categories
				WHERE category_id IN (%s)
				GROUP BY contact_id
				HAVING COUNT(DISTINCT category_id) = %d
			)
		`, strings.Join(placeholders, ","), len(filters.CategoryIDs))
		whereClauses = append(whereClauses, categoryClause)
	}

	// -----------------------------
	// Sort logic
	// -----------------------------
	sortBy := "c.created_at"
	direction := "DESC"
	allowedSorts := map[string]bool{
		"first_name":     true,
		"last_name":      true,
		"email":          true,
		"created_at":     true,
		"updated_at":     true,
		"campaign_count": true,
	}

	if filters.SortBy != "" && allowedSorts[filters.SortBy] {
		if filters.SortBy == "campaign_count" {
			sortBy = "campaign_count"
		} else {
			sortBy = "c." + filters.SortBy
		}
	}
	if filters.Reverse {
		direction = "ASC"
	} else {
		direction = "DESC"
	}

	// -----------------------------
	// Cursor pagination
	// -----------------------------
	if cursor != nil && *cursor != "" {
		cursorOp := ">"
		if direction == "DESC" {
			cursorOp = "<"
		}
		sortSub := fmt.Sprintf("(SELECT %s FROM contacts WHERE id = $%d)", sortBy, argIndex)
		args = append(args, *cursor)
		argIndex++

		whereClauses = append(whereClauses, fmt.Sprintf(`
			(
				(%s %s %s)
				OR (%s = %s AND c.id >= $%d)
			)
		`, sortBy, cursorOp, sortSub, sortBy, sortSub, argIndex))
		args = append(args, *cursor)
		argIndex++
	}

	// -----------------------------
	// Campaign count filters (min/max)
	// -----------------------------
	campaignCountClauses := []string{}
	if filters.MinCampaigns != nil {
		campaignCountClauses = append(campaignCountClauses, fmt.Sprintf("COALESCE(cl.campaign_count,0) >= $%d", argIndex))
		args = append(args, *filters.MinCampaigns)
		argIndex++
	}
	if filters.MaxCampaigns != nil {
		campaignCountClauses = append(campaignCountClauses, fmt.Sprintf("COALESCE(cl.campaign_count,0) <= $%d", argIndex))
		args = append(args, *filters.MaxCampaigns)
		argIndex++
	}

	// -----------------------------
	// Build WHERE SQL
	// -----------------------------
	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}
	if len(campaignCountClauses) > 0 {
		if whereSQL == "" {
			whereSQL = "WHERE " + strings.Join(campaignCountClauses, " AND ")
		} else {
			whereSQL += " AND " + strings.Join(campaignCountClauses, " AND ")
		}
	}

	// Per-campaign lead progress. Only computed in the single-campaign (Leads
	// view) case; otherwise the column is NULL so the scan list stays fixed.
	// `last_at` is the latest of any touchpoint timestamp (GREATEST skips NULLs);
	// counts aggregate across every step the contact was sent in this campaign.
	leadProgressSelect := "NULL::json"
	if singleCampaignPlaceholder != "" {
		leadProgressSelect = fmt.Sprintf(`(
			SELECT json_build_object(
				'sent',    COUNT(*) FILTER (WHERE p.sent_at IS NOT NULL),
				'opened',  COUNT(*) FILTER (WHERE p.opened_at IS NOT NULL),
				'clicked', COUNT(*) FILTER (WHERE p.clicked_at IS NOT NULL),
				'replied', COUNT(*) FILTER (WHERE p.replied_at IS NOT NULL),
				'bounced', COUNT(*) FILTER (WHERE p.bounced_at IS NOT NULL),
				'last_at', MAX(GREATEST(p.sent_at, p.opened_at, p.clicked_at, p.replied_at, p.bounced_at)),
				-- The step the contact is on now = the latest step actually sent.
				-- Labelled the same way the canvas does: custom name, else
				-- "Email N" (Nth email-kind step by position), else action label.
				'step', (
					SELECT CASE
						WHEN NULLIF(BTRIM(s.name), '') IS NOT NULL THEN s.name
						WHEN s.kind = 'email' THEN 'Email ' || (
							SELECT COUNT(*) FROM sequences s2
							WHERE s2.campaign_id = s.campaign_id AND s2.kind = 'email'
							  AND (s2.position < s.position
							       OR (s2.position = s.position AND s2.created_at <= s.created_at))
						)::text
						WHEN s.kind = 'action' THEN (CASE s.action->>'type'
							WHEN 'add_tag'     THEN 'Add tag'
							WHEN 'remove_tag'  THEN 'Remove tag'
							WHEN 'unsubscribe' THEN 'Unsubscribe'
							WHEN 'notify'      THEN 'Notify'
							ELSE 'Action' END)
						ELSE 'Step'
					END
					FROM campaign_contact_progress lp
					JOIN sequences s ON s.id = lp.sequence_id
					WHERE lp.campaign_id = %[1]s AND lp.contact_id = c.id AND lp.sent_at IS NOT NULL
					ORDER BY lp.sent_at DESC
					LIMIT 1
				)
			)
			FROM campaign_contact_progress p
			WHERE p.campaign_id = %[1]s AND p.contact_id = c.id
		)`, singleCampaignPlaceholder)
	}

	// Main query.
	//
	// Both the `campaigns` and `categories` agg subqueries need the
	// user_id so they can't leak rows from other users that happen to
	// share a contact id (theoretically impossible thanks to the outer
	// WHERE, but cheap defence-in-depth). They reuse the same $%d
	// placeholder so we only append userID once.
	query := fmt.Sprintf(`
		SELECT
			c.id, c.first_name, c.last_name, c.email, c.company, c.phone,
			c.custom_fields, c.subscribed, c.updated_at, c.created_at,
			COALESCE(cl.campaign_count,0) AS campaign_count,
			COALESCE(
				(
					SELECT json_agg(json_build_object('id', cam.id, 'name', cam.name))
					FROM campaign_leads cl2
					JOIN campaigns cam ON cl2.campaign_id = cam.id
					WHERE cl2.contact_id = c.id
					AND cam.organization_id = $%d
				), '[]'::json
			) AS campaigns,
			COALESCE(
				(
					SELECT json_agg(json_build_object('id', cat.id, 'title', cat.title, 'color', cat.color) ORDER BY cat.position ASC, cat.title ASC)
					FROM contact_categories cc
					JOIN categories cat ON cc.category_id = cat.id
					WHERE cc.contact_id = c.id
					AND cat.user_id = $%d
				), '[]'::json
			) AS categories,
			%s AS lead_progress
		FROM contacts c
		LEFT JOIN (
			SELECT contact_id, COUNT(campaign_id) AS campaign_count
			FROM campaign_leads
			GROUP BY contact_id
		) cl ON c.id = cl.contact_id
		%s
		ORDER BY %s %s, c.id ASC
		LIMIT $%d
	`, argIndex, argIndex, leadProgressSelect, whereSQL, sortBy, direction, argIndex+1)

	args = append(args, orgID, limit+1)

	// Skip total count if cursor exists
	var totalCount *int64
	if cursor == nil || *cursor == "" {
		countQuery := fmt.Sprintf(`
			SELECT COUNT(*)
			FROM contacts c
			LEFT JOIN (
				SELECT contact_id, COUNT(campaign_id) AS campaign_count
				FROM campaign_leads
				GROUP BY contact_id
			) cl ON c.id = cl.contact_id
			%s
		`, whereSQL)
		var tmp int64
		if err := r.DB.QueryRow(ctx, countQuery, args[:argIndex-1]...).Scan(&tmp); err != nil {
			db.CaptureError(err, "countQuery", args, "queryrow")
			return nil, errx.InternalError()
		}
		totalCount = &tmp
	}

	// -----------------------------
	// Execute query
	// -----------------------------
	rows, err := r.DB.Query(ctx, query, args...)
	if err != nil {
		db.CaptureError(err, query, args, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	// Initialize as non-nil so JSON marshals to [] on zero rows. A nil
	// slice marshals to `null`, and the frontend's flatMap((p) => p.data)
	// then produces [null], which crashes any downstream `.subscribed`
	// access. Always return an array.
	contacts := make([]models.Contact, 0, limit+1)
	for rows.Next() {
		var c models.Contact
		var campaignCount int
		var campaignsJSON []byte
		var categoriesJSON []byte
		var leadProgressJSON []byte

		if err := rows.Scan(
			&c.ID, &c.FirstName, &c.LastName, &c.Email,
			&c.Company, &c.Phone, &c.CustomFields, &c.Subscribed,
			&c.UpdatedAt, &c.CreatedAt, &campaignCount, &campaignsJSON, &categoriesJSON, &leadProgressJSON,
		); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}

		// Per-campaign lead progress (single-campaign Leads view only). Derive
		// the single display status from the counts + subscription flag.
		if len(leadProgressJSON) > 0 {
			var lp struct {
				Sent    int        `json:"sent"`
				Opened  int        `json:"opened"`
				Clicked int        `json:"clicked"`
				Replied int        `json:"replied"`
				Bounced int        `json:"bounced"`
				LastAt  *time.Time `json:"last_at"`
				Step    *string    `json:"step"`
			}
			if err := json.Unmarshal(leadProgressJSON, &lp); err != nil {
				sentry.CaptureException(err)
				return nil, errx.InternalError()
			}
			status := models.LeadStatusPending
			switch {
			case !c.Subscribed:
				status = models.LeadStatusUnsubscribed
			case lp.Bounced > 0:
				status = models.LeadStatusBounced
			case lp.Replied > 0:
				status = models.LeadStatusReplied
			case lp.Sent > 0:
				status = models.LeadStatusActive
			}
			currentStep := ""
			if lp.Step != nil {
				currentStep = *lp.Step
			}
			c.CampaignLead = &models.ContactCampaignProgress{
				Status:         status,
				Sent:           lp.Sent,
				Opened:         lp.Opened,
				Clicked:        lp.Clicked,
				Replied:        lp.Replied,
				Bounced:        lp.Bounced,
				LastActivityAt: lp.LastAt,
				CurrentStep:    currentStep,
			}
		}

		if len(campaignsJSON) > 0 {
			var campaigns []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if err := json.Unmarshal(campaignsJSON, &campaigns); err != nil {
				sentry.CaptureException(err)
				return nil, errx.InternalError()
			}
			c.Campaigns = make([]models.MiniCampaign, len(campaigns))
			for i, cm := range campaigns {
				c.Campaigns[i] = models.MiniCampaign{ID: cm.ID, Name: cm.Name}
			}
		} else {
			c.Campaigns = []models.MiniCampaign{}
		}

		if len(categoriesJSON) > 0 {
			if err := json.Unmarshal(categoriesJSON, &c.Categories); err != nil {
				sentry.CaptureException(err)
				return nil, errx.InternalError()
			}
		}
		if c.Categories == nil {
			c.Categories = []models.MiniCategory{}
		}

		contacts = append(contacts, c)
	}

	// Next cursor
	var nextCursor *string
	var hasMore bool
	if len(contacts) > int(limit) {
		hasMore = true
		nextID := contacts[limit].ID
		nextCursor = paging.EncodeUUID(nextID)
		contacts = contacts[:limit]
	}

	return &models.ContactsResult{
		Data: contacts,
		Pagination: models.Pagination{
			Total:      totalCount,
			NextCursor: nextCursor,
			HasMore:    hasMore,
		},
	}, nil
}

// SearchCounts returns org-wide contact facet totals for the browse sidebar.
// Two small aggregates: the scalar facets over contacts (subscription +
// campaign membership derived from the campaign_leads count), and per-category
// contact counts joined through the org's contacts. Independent of any search
// filter, like the campaigns-overview drawer counts.
func (r *contactRepository) SearchCounts(ctx context.Context, orgID string) (*models.ContactsCounts, *errx.Error) {
	counts := &models.ContactsCounts{Categories: []models.ContactCategoryCount{}}

	scalarQuery := `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE c.subscribed),
			COUNT(*) FILTER (WHERE NOT c.subscribed),
			COUNT(*) FILTER (WHERE COALESCE(cl.campaign_count, 0) > 0),
			COUNT(*) FILTER (WHERE COALESCE(cl.campaign_count, 0) = 0)
		FROM contacts c
		LEFT JOIN (
			SELECT contact_id, COUNT(campaign_id) AS campaign_count
			FROM campaign_leads
			GROUP BY contact_id
		) cl ON c.id = cl.contact_id
		WHERE c.organization_id = $1
	`
	if err := r.DB.QueryRow(ctx, scalarQuery, orgID).Scan(
		&counts.Total, &counts.Subscribed, &counts.Unsubscribed,
		&counts.InCampaign, &counts.NotContacted,
	); err != nil {
		db.CaptureError(err, scalarQuery, []any{orgID}, "queryrow")
		return nil, errx.InternalError()
	}

	categoryQuery := `
		SELECT cc.category_id, COUNT(*)
		FROM contact_categories cc
		JOIN contacts c ON c.id = cc.contact_id
		WHERE c.organization_id = $1
		GROUP BY cc.category_id
	`
	rows, err := r.DB.Query(ctx, categoryQuery, orgID)
	if err != nil {
		db.CaptureError(err, categoryQuery, []any{orgID}, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()
	for rows.Next() {
		var cat models.ContactCategoryCount
		if err := rows.Scan(&cat.CategoryID, &cat.Count); err != nil {
			db.CaptureError(err, categoryQuery, nil, "scan")
			return nil, errx.InternalError()
		}
		counts.Categories = append(counts.Categories, cat)
	}

	return counts, nil
}

// leadStatusClause builds the WHERE predicate for a derived lead status inside
// ONE campaign, matching pg_contact Search's read-time derivation exactly:
// unsubscribed > bounced > replied > processing(active) > queued(pending). `cp`
// is the already-bound placeholder for that campaign id (e.g. "$5"). Returns ""
// for an unknown status (the caller then applies no lead filter).
func leadStatusClause(status, cp string) string {
	// EXISTS a progress row for (this campaign, this contact) with `col` set.
	has := func(col string) string {
		return fmt.Sprintf(
			"EXISTS (SELECT 1 FROM campaign_contact_progress p WHERE p.campaign_id = %s AND p.contact_id = c.id AND p.%s IS NOT NULL)",
			cp, col,
		)
	}
	sent, replied, bounced := has("sent_at"), has("replied_at"), has("bounced_at")
	switch status {
	case models.LeadStatusUnsubscribed:
		return "NOT c.subscribed"
	case models.LeadStatusBounced:
		return fmt.Sprintf("(c.subscribed AND %s)", bounced)
	case models.LeadStatusReplied:
		return fmt.Sprintf("(c.subscribed AND NOT %s AND %s)", bounced, replied)
	case models.LeadStatusActive:
		return fmt.Sprintf("(c.subscribed AND NOT %s AND NOT %s AND %s)", bounced, replied, sent)
	case models.LeadStatusPending:
		return fmt.Sprintf("(c.subscribed AND NOT %s AND NOT %s AND NOT %s)", bounced, replied, sent)
	default:
		return ""
	}
}

// CampaignLeadCounts returns per-status lead totals for one campaign (the
// campaign Leads view scope chips). A single aggregate over the campaign's
// leads joined to their contact and a rolled-up view of their progress, so the
// buckets follow the same unsubscribed > bounced > replied > processing >
// queued priority as the row-level derived status. Scoped to the org through
// the contacts join.
func (r *contactRepository) CampaignLeadCounts(ctx context.Context, orgID, campaignID string) (*models.CampaignLeadCounts, *errx.Error) {
	query := `
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE NOT c.subscribed) AS unsubscribed,
			COUNT(*) FILTER (WHERE c.subscribed AND COALESCE(pr.has_bounced, false)) AS bounced,
			COUNT(*) FILTER (WHERE c.subscribed AND NOT COALESCE(pr.has_bounced, false) AND COALESCE(pr.has_replied, false)) AS replied,
			COUNT(*) FILTER (WHERE c.subscribed AND NOT COALESCE(pr.has_bounced, false) AND NOT COALESCE(pr.has_replied, false) AND COALESCE(pr.has_sent, false)) AS processing,
			COUNT(*) FILTER (WHERE c.subscribed AND NOT COALESCE(pr.has_bounced, false) AND NOT COALESCE(pr.has_replied, false) AND NOT COALESCE(pr.has_sent, false)) AS queued
		FROM campaign_leads cl
		JOIN contacts c ON c.id = cl.contact_id AND c.organization_id = $2
		LEFT JOIN LATERAL (
			SELECT
				bool_or(p.sent_at IS NOT NULL)    AS has_sent,
				bool_or(p.replied_at IS NOT NULL) AS has_replied,
				bool_or(p.bounced_at IS NOT NULL) AS has_bounced
			FROM campaign_contact_progress p
			WHERE p.campaign_id = cl.campaign_id AND p.contact_id = cl.contact_id
		) pr ON true
		WHERE cl.campaign_id = $1
	`
	out := &models.CampaignLeadCounts{}
	if err := r.DB.QueryRow(ctx, query, campaignID, orgID).Scan(
		&out.Total, &out.Unsubscribed, &out.Bounced, &out.Replied, &out.Processing, &out.Queued,
	); err != nil {
		if err == pgx.ErrNoRows {
			return out, nil
		}
		db.CaptureError(err, query, []any{campaignID, orgID}, "queryrow")
		return nil, errx.InternalError()
	}
	return out, nil
}

func (r *contactRepository) Update(ctx context.Context, userID, contactID string, orgID uuid.UUID, data *models.UpdateContact) (*models.Contact, *errx.Error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)

	// Validate contact existence and fetch current data
	var c models.Contact
	var campaignsJSON []byte

	query := `
		SELECT 
			c.id, c.first_name, c.last_name, c.email, c.company, c.phone,
			c.custom_fields, c.subscribed, c.updated_at, c.created_at,
			COALESCE(
				(
					SELECT json_agg(json_build_object('id', cam.id, 'name', cam.name))
					FROM campaign_leads cl2
					JOIN campaigns cam ON cl2.campaign_id = cam.id
					WHERE cl2.contact_id = c.id AND cam.user_id = $2
				),
				'[]'::json
			) AS campaigns
		FROM contacts c
		WHERE c.id = $1 AND c.organization_id = $3
		`

	params := []any{
		contactID,
		userID,
		orgID,
	}

	err = tx.QueryRow(
		ctx,
		query,
		params...,
	).Scan(
		&c.ID, &c.FirstName, &c.LastName, &c.Email,
		&c.Company, &c.Phone, &c.CustomFields, &c.Subscribed,
		&c.UpdatedAt, &c.CreatedAt, &campaignsJSON,
	)
	if err == pgx.ErrNoRows {
		return nil, errx.ErrNotFound
	}
	if err != nil {
		db.CaptureError(err, query, params, "queryrow")
		return nil, errx.InternalError()
	}

	// Unmarshal current campaigns
	if len(campaignsJSON) > 0 {
		var campaigns []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(campaignsJSON, &campaigns); err != nil {
			sentry.CaptureException(err)
			return nil, errx.InternalError()
		}
		c.Campaigns = make([]models.MiniCampaign, len(campaigns))
		for i, camp := range campaigns {
			c.Campaigns[i] = models.MiniCampaign{
				ID:   camp.ID,
				Name: camp.Name,
			}
		}
	} else {
		c.Campaigns = make([]models.MiniCampaign, 0)
	}

	// Build update query for contacts table
	var setClauses []string
	var args []interface{}
	argIndex := 1

	// Update fields if provided
	if data.FirstName != nil {
		setClauses = append(setClauses, fmt.Sprintf("first_name = $%d", argIndex))
		args = append(args, *data.FirstName)
		argIndex++
	}
	if data.LastName != nil {
		setClauses = append(setClauses, fmt.Sprintf("last_name = $%d", argIndex))
		args = append(args, *data.LastName)
		argIndex++
	}
	if data.Company != nil {
		setClauses = append(setClauses, fmt.Sprintf("company = $%d", argIndex))
		args = append(args, *data.Company)
		argIndex++
	}
	if data.Phone != nil {
		setClauses = append(setClauses, fmt.Sprintf("phone = $%d", argIndex))
		args = append(args, *data.Phone)
		argIndex++
	}
	if data.Subscribed != nil {
		setClauses = append(setClauses, fmt.Sprintf("subscribed = $%d", argIndex))
		args = append(args, *data.Subscribed)
		argIndex++
	}
	if data.CustomFields != nil {
		for key := range *data.CustomFields {
			if !utils.IsValidJSONKey(key) {
				return nil, errx.ErrJSONKey
			}
		}
		// Merge existing custom_fields with updates
		mergedFields := make(map[string]string)
		for k, v := range c.CustomFields {
			mergedFields[k] = v
		}
		for k, v := range *data.CustomFields {
			if v == "" {
				delete(mergedFields, k) // Remove key if value is empty
			} else {
				mergedFields[k] = v // Update or add key
			}
		}
		setClauses = append(setClauses, fmt.Sprintf("custom_fields = $%d", argIndex))
		args = append(args, mergedFields)
		argIndex++
	}

	// Always update updated_at
	setClauses = append(setClauses, "updated_at = NOW()")

	// If no fields to update, skip contacts table update
	var updatedContact models.Contact
	if len(setClauses) > 1 { // >1 because updated_at is always included
		args = append(args, contactID, orgID)
		query := fmt.Sprintf(`
			UPDATE contacts
			SET %s
			WHERE id = $%d AND organization_id = $%d
			RETURNING id, first_name, last_name, email, company, phone, custom_fields, subscribed, updated_at, created_at`,
			strings.Join(setClauses, ", "), argIndex, argIndex+1)
		err = tx.QueryRow(ctx, query, args...).Scan(
			&updatedContact.ID, &updatedContact.FirstName, &updatedContact.LastName, &updatedContact.Email,
			&updatedContact.Company, &updatedContact.Phone, &updatedContact.CustomFields, &updatedContact.Subscribed,
			&updatedContact.UpdatedAt, &updatedContact.CreatedAt,
		)
		if err != nil {
			if err == pgx.ErrNoRows {
				return nil, errx.ErrNotFound
			}
			db.CaptureError(err, query, args, "queryrow")
			return nil, errx.InternalError()
		}
	} else {
		updatedContact = c // No fields updated, use existing contact
	}

	// Update campaigns if provided
	if data.Campaigns != nil {
		// Get current campaign IDs
		currentCampaignIDs := make([]string, len(updatedContact.Campaigns))
		for i, c := range updatedContact.Campaigns {
			currentCampaignIDs[i] = c.ID
		}

		// Compute campaigns to insert and delete
		toInsert := utils.Difference(data.Campaigns, currentCampaignIDs)
		toDelete := utils.Difference(currentCampaignIDs, data.Campaigns)

		// Delete removed campaigns
		query = `
			DELETE FROM campaign_leads
			WHERE contact_id = $1 AND campaign_id = $2
		`
		for _, campaignID := range toDelete {
			params := []any{
				contactID,
				campaignID,
			}
			_, err = tx.Exec(
				ctx,
				query,
				params...,
			)
			if err != nil {
				db.CaptureError(err, query, params, "exec")
				return nil, errx.InternalError()
			}
		}

		// Insert new campaigns
		query = `
			INSERT INTO campaign_leads (contact_id, campaign_id)
			SELECT $1, id
			FROM campaigns
			WHERE id = $2 AND user_id = $3
			ON CONFLICT (campaign_id, contact_id) DO NOTHING
		`
		for _, campaignID := range toInsert {
			params := []any{
				contactID,
				campaignID,
				userID,
			}
			_, err = tx.Exec(
				ctx,
				query,
				params...,
			)
			if err != nil {
				db.CaptureError(err, query, params, "exec")
				return nil, errx.InternalError()
			}
		}

		// Fetch updated campaigns
		var newCampaignsJSON []byte

		query = `
			SELECT COALESCE(
				(
					SELECT json_agg(json_build_object('id', cam.id, 'name', cam.name))
					FROM campaign_leads cl
					JOIN campaigns cam ON cl.campaign_id = cam.id
					WHERE cl.contact_id =$1 AND cam.user_id = $2
				),
				'[]'::json
			)
		`

		params := []any{
			contactID,
			userID,
		}

		err = tx.QueryRow(
			ctx,
			query,
			params...,
		).Scan(&newCampaignsJSON)
		if err != nil {
			db.CaptureError(err, query, params, "queryrow")
			return nil, errx.InternalError()
		}
		if len(newCampaignsJSON) > 0 {
			var campaigns []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if err := json.Unmarshal(newCampaignsJSON, &campaigns); err != nil {
				sentry.CaptureException(err)
				return nil, errx.InternalError()
			}
			updatedContact.Campaigns = make([]models.MiniCampaign, len(campaigns))
			for i, c := range campaigns {
				updatedContact.Campaigns[i] = models.MiniCampaign{
					ID:   c.ID,
					Name: c.Name,
				}
			}
		} else {
			updatedContact.Campaigns = make([]models.MiniCampaign, 0)
		}
	}

	// Categories. Two modes supported on the request:
	//   - `categories: [..]` → set absolute (full replace).
	//   - `add_categories / remove_categories` → diff style.
	// When the absolute form is non-nil it wins; the diff is ignored.
	categoriesChanged := false
	if data.Categories != nil {
		ids, perr := parseCategoryIDs(data.Categories)
		if perr != nil {
			return nil, perr
		}
		// Wipe then insert; scoped to user-owned categories.
		if _, err := tx.Exec(ctx, `DELETE FROM contact_categories WHERE contact_id = $1`, contactID); err != nil {
			db.CaptureError(err, "", nil, "categories wipe")
			return nil, errx.InternalError()
		}
		if len(ids) > 0 {
			if _, err := tx.Exec(ctx, `
				INSERT INTO contact_categories (contact_id, category_id)
				SELECT $1, cat.id
				FROM   categories cat
				WHERE  cat.id = ANY($2) AND cat.user_id = $3
				ON CONFLICT (contact_id, category_id) DO NOTHING
			`, contactID, ids, userID); err != nil {
				db.CaptureError(err, "", nil, "categories insert")
				return nil, errx.InternalError()
			}
		}
		categoriesChanged = true
	} else {
		if len(data.AddCategories) > 0 {
			ids, perr := parseCategoryIDs(data.AddCategories)
			if perr != nil {
				return nil, perr
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO contact_categories (contact_id, category_id)
				SELECT $1, cat.id
				FROM   categories cat
				WHERE  cat.id = ANY($2) AND cat.user_id = $3
				ON CONFLICT (contact_id, category_id) DO NOTHING
			`, contactID, ids, userID); err != nil {
				db.CaptureError(err, "", nil, "categories add")
				return nil, errx.InternalError()
			}
			categoriesChanged = true
		}
		if len(data.RemoveCategories) > 0 {
			ids, perr := parseCategoryIDs(data.RemoveCategories)
			if perr != nil {
				return nil, perr
			}
			if _, err := tx.Exec(ctx, `
				DELETE FROM contact_categories
				WHERE contact_id = $1 AND category_id = ANY($2)
			`, contactID, ids); err != nil {
				db.CaptureError(err, "", nil, "categories remove")
				return nil, errx.InternalError()
			}
			categoriesChanged = true
		}
	}

	// Always re-read categories so the response reflects current state
	// (cheap, indexed lookup).
	if categoriesChanged || updatedContact.Categories == nil {
		var catJSON []byte
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(
				(
					SELECT json_agg(json_build_object('id', cat.id, 'title', cat.title, 'color', cat.color) ORDER BY cat.position ASC, cat.title ASC)
					FROM contact_categories cc
					JOIN categories cat ON cc.category_id = cat.id
					WHERE cc.contact_id = $1 AND cat.user_id = $2
				),
				'[]'::json
			)
		`, contactID, userID).Scan(&catJSON); err != nil {
			db.CaptureError(err, "", nil, "categories reload")
			return nil, errx.InternalError()
		}
		updatedContact.Categories = make([]models.MiniCategory, 0)
		if len(catJSON) > 0 {
			if err := json.Unmarshal(catJSON, &updatedContact.Categories); err != nil {
				sentry.CaptureException(err)
				return nil, errx.InternalError()
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, errx.InternalError()
	}

	return &updatedContact, nil
}

func (r *contactRepository) BulkUpdate(ctx context.Context, userID string, orgID uuid.UUID, data *models.BulkEditContactsData) ([]models.Contact, *errx.Error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)

	b := &pgx.Batch{}

	if data.Subscribe != nil {
		b.Queue(`UPDATE contacts
		         SET subscribed = $1, updated_at = NOW()
		         WHERE organization_id = $2 AND id = ANY($3)`,
			*data.Subscribe, orgID, data.Contacts)
	}

	if len(data.RemoveCampaigns) > 0 {
		b.Queue(`DELETE FROM campaign_leads cl
		         USING contacts c, campaigns cam
		         WHERE cl.contact_id = c.id
		           AND cl.campaign_id = cam.id
		           AND c.organization_id = $1
		           AND cam.user_id = $4
		           AND cl.contact_id = ANY($2)
		           AND cl.campaign_id = ANY($3)`,
			orgID, data.Contacts, data.RemoveCampaigns, userID)
	}

	if len(data.AddCampaigns) > 0 {
		b.Queue(`INSERT INTO campaign_leads (contact_id, campaign_id)
		         SELECT c.id, cam.id
		         FROM contacts c
		         CROSS JOIN campaigns cam
		         WHERE c.organization_id = $1
		           AND c.id = ANY($2)
		           AND cam.id = ANY($3::uuid[])
		           AND cam.user_id = $4
		         ON CONFLICT DO NOTHING`,
			orgID, data.Contacts, data.AddCampaigns, userID)
	}

	if len(data.RemoveCategories) > 0 {
		b.Queue(`DELETE FROM contact_categories cc
		         USING contacts c
		         WHERE cc.contact_id = c.id
		           AND c.organization_id = $1
		           AND cc.contact_id = ANY($2)
		           AND cc.category_id = ANY($3::uuid[])`,
			orgID, data.Contacts, data.RemoveCategories)
	}

	if len(data.AddCategories) > 0 {
		b.Queue(`INSERT INTO contact_categories (contact_id, category_id)
		         SELECT c.id, cat.id
		         FROM contacts c
		         CROSS JOIN categories cat
		         WHERE c.organization_id = $1
		           AND c.id = ANY($2)
		           AND cat.id = ANY($3::uuid[])
		           AND cat.user_id = $4
		         ON CONFLICT DO NOTHING`,
			orgID, data.Contacts, data.AddCategories, userID)
	}

	for _, p := range data.Fields {
		switch p.Type {
		case models.BulkAddField:
			b.Queue(`UPDATE contacts
			         SET custom_fields = custom_fields || jsonb_build_object($1,$2),
			             updated_at = NOW()
			         WHERE organization_id = $3 AND id = ANY($4)`,
				p.Key, p.Value, orgID, data.Contacts)
		case models.BulkEditField:
			b.Queue(`UPDATE contacts
			         SET custom_fields = jsonb_set(custom_fields, ARRAY[$1], to_jsonb($2::text)),
			             updated_at = NOW()
			         WHERE organization_id = $3 AND id = ANY($4)`,
				p.Key, p.Value, orgID, data.Contacts)
		case models.BulkDeleteField:
			b.Queue(`UPDATE contacts
			         SET custom_fields = custom_fields - $1,
			             updated_at = NOW()
			         WHERE organization_id = $2 AND id = ANY($3)`,
				p.Key, orgID, data.Contacts)
		case models.BulkRenameField:
			b.Queue(`UPDATE contacts
			         SET custom_fields = (custom_fields - $1) || jsonb_build_object($2, custom_fields->$1),
			             updated_at = NOW()
			         WHERE organization_id = $3 AND id = ANY($4)
			           AND custom_fields ? $1`,
				p.Key, p.Value, orgID, data.Contacts)
		}
	}

	br := tx.SendBatch(ctx, b)

	for i := 0; i < b.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			br.Close()
			db.CaptureError(err, "", nil, "batch exec")
			return nil, errx.InternalError()
		}
	}

	br.Close()

	query := `
		SELECT
			c.id, c.first_name, c.last_name, c.email, c.company, c.phone,
			c.custom_fields, c.subscribed, c.updated_at, c.created_at,
			COALESCE(
				(
					SELECT json_agg(json_build_object('id', cam.id, 'name', cam.name))
					FROM campaign_leads cl
					JOIN campaigns cam ON cl.campaign_id = cam.id
					WHERE cl.contact_id =c.id AND cam.user_id = $2
				),
				'[]'::json
			) AS campaigns,
			COALESCE(
				(
					SELECT json_agg(json_build_object('id', cat.id, 'title', cat.title, 'color', cat.color) ORDER BY cat.position ASC, cat.title ASC)
					FROM contact_categories cc
					JOIN categories cat ON cc.category_id = cat.id
					WHERE cc.contact_id = c.id AND cat.user_id = $2
				),
				'[]'::json
			) AS categories
		FROM contacts c
		WHERE c.organization_id = $3 AND c.id = ANY($1)
	`

	params := []any{
		data.Contacts,
		userID,
		orgID,
	}
	rows, err := tx.Query(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, "", nil, "fetch updated contacts")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	var updatedContacts []models.Contact

	for rows.Next() {
		var c models.Contact
		var campaignsJSON []byte
		var categoriesJSON []byte

		err := rows.Scan(
			&c.ID, &c.FirstName, &c.LastName, &c.Email,
			&c.Company, &c.Phone, &c.CustomFields, &c.Subscribed,
			&c.UpdatedAt, &c.CreatedAt, &campaignsJSON, &categoriesJSON,
		)
		if err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}

		// Unmarshal campaigns
		if len(campaignsJSON) > 0 {
			var campaigns []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if err := json.Unmarshal(campaignsJSON, &campaigns); err != nil {
				sentry.CaptureException(err)
				return nil, errx.InternalError()
			}
			c.Campaigns = make([]models.MiniCampaign, len(campaigns))
			for i, camp := range campaigns {
				c.Campaigns[i] = models.MiniCampaign{
					ID:   camp.ID,
					Name: camp.Name,
				}
			}
		} else {
			c.Campaigns = make([]models.MiniCampaign, 0)
		}

		c.Categories = make([]models.MiniCategory, 0)
		if len(categoriesJSON) > 0 {
			if err := json.Unmarshal(categoriesJSON, &c.Categories); err != nil {
				sentry.CaptureException(err)
				return nil, errx.InternalError()
			}
		}

		updatedContacts = append(updatedContacts, c)
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, errx.InternalError()
	}

	return updatedContacts, nil
}

func (r *contactRepository) BulkDelete(ctx context.Context, userID string, orgID uuid.UUID, IDs []string) *errx.Error {
	query := `
		DELETE FROM contacts
		WHERE id = ANY($1) AND organization_id = $2
	`
	params := []any{
		IDs,
		orgID,
	}
	_, err := r.DB.Exec(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}
	return nil
}

func (r *contactRepository) Delete(ctx context.Context, userID string, orgID uuid.UUID, ID string) *errx.Error {
	query := `
		DELETE FROM contacts
		WHERE id = $1 AND organization_id = $2
	`
	params := []any{
		ID,
		orgID,
	}
	cmd, err := r.DB.Exec(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}
	if cmd.RowsAffected() == 0 {
		return errx.ErrNotFound
	}
	return nil
}

// GetByEmailsAndUser returns the contacts whose lowercased email is in
// the given list, scoped to a single user. Used by the import path to
// detect collisions before doing the bulk upsert. The map is keyed by
// lowercased email so the caller doesn't have to normalize again.
func (r *contactRepository) GetByEmailsAndUser(ctx context.Context, userID uuid.UUID, emails []string) (map[string]models.Contact, *errx.Error) {
	out := make(map[string]models.Contact, len(emails))
	if len(emails) == 0 {
		return out, nil
	}
	norm := make([]string, 0, len(emails))
	for _, e := range emails {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		norm = append(norm, e)
	}
	if len(norm) == 0 {
		return out, nil
	}

	rows, err := r.DB.Query(ctx, `
		SELECT id, first_name, last_name, email, company, phone, custom_fields, subscribed, updated_at, created_at
		FROM contacts
		WHERE user_id = $1 AND LOWER(email) = ANY($2)
	`, userID, norm)
	if err != nil {
		db.CaptureError(err, "", nil, "GetByEmailsAndUser query")
		return nil, errx.InternalError()
	}
	defer rows.Close()
	for rows.Next() {
		var c models.Contact
		if err := rows.Scan(
			&c.ID, &c.FirstName, &c.LastName, &c.Email,
			&c.Company, &c.Phone, &c.CustomFields, &c.Subscribed,
			&c.UpdatedAt, &c.CreatedAt,
		); err != nil {
			db.CaptureError(err, "", nil, "GetByEmailsAndUser scan")
			return nil, errx.InternalError()
		}
		c.Campaigns = []models.MiniCampaign{}
		c.Categories = []models.MiniCategory{}
		out[strings.ToLower(c.Email)] = c
	}
	return out, nil
}

// ExportAll fetches every contact matching the given selection so it
// can be streamed out as CSV/XLSX/JSON. There is no pagination — the
// caller is expected to enforce max upstream (handler does).
//
// Three selection modes overlap with the search filter machinery:
//   - filters != nil  → reuse the SearchContacts WHERE-builder.
//   - contactIDs > 0  → constrain to just those rows.
//   - both nil/empty  → "every contact this user owns".
func (r *contactRepository) ExportAll(ctx context.Context, userID string, filters *models.SearchContacts, contactIDs []string, max int) ([]models.Contact, *errx.Error) {
	if _, perr := uuid.Parse(userID); perr != nil {
		return nil, errx.ErrUuid
	}
	if max <= 0 {
		max = models.MaxContactExportRows
	}

	// Fall back to "everyone" by walking Search in pages. Reuse Search
	// to keep WHERE-builder logic in one place: a divergent copy here
	// would drift away from production semantics as filters evolve.
	var search models.SearchContacts
	if filters != nil {
		search = *filters
	}
	// If a specific id list is provided, narrow further by pulling
	// directly. We still pass through Search so we get the same joined
	// categories/campaigns shape.
	idSet := make(map[uuid.UUID]struct{}, len(contactIDs))
	useIDFilter := false
	for _, raw := range contactIDs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		id, perr := uuid.Parse(raw)
		if perr != nil {
			return nil, errx.ErrUuid
		}
		idSet[id] = struct{}{}
		useIDFilter = true
	}

	out := make([]models.Contact, 0, 256)
	var cursor *string
	pageSize := int32(500)
	for {
		page, xerr := r.Search(ctx, userID, nil, cursor, search, pageSize)
		if xerr != nil {
			return nil, xerr
		}
		for _, c := range page.Data {
			if useIDFilter {
				if _, ok := idSet[c.ID]; !ok {
					continue
				}
			}
			out = append(out, c)
			if len(out) >= max {
				return out, nil
			}
		}
		if !page.Pagination.HasMore || page.Pagination.NextCursor == nil {
			break
		}
		// NextCursor is now an opaque token; decode it back to the id the next
		// Search call keys on.
		id, derr := paging.DecodeUUID(*page.Pagination.NextCursor)
		if derr != nil {
			break
		}
		s := id.String()
		cursor = &s
	}
	return out, nil
}

func (r *contactRepository) GetContactCount(ctx context.Context, userID string) (int, *errx.Error) {
	query := `SELECT COUNT(*) FROM contacts WHERE user_id = $1`
	var count int
	err := r.DB.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		db.CaptureError(err, query, []any{userID}, "queryrow")
		return 0, errx.InternalError()
	}
	return count, nil
}

// GetDetail loads the contact 360 payload: core fields + categories +
// campaigns + engagement counts + suppression. Single round-trip via a
// few separate queries (one main select + a couple of small aggregates)
// so the query plans stay simple and cheap to reason about.
//
// orgID is optional because not every caller has an org context (e.g.
// an API key scoped to a user without a selected org). When nil we
// skip the org-scoped joins (suppression, deliverability) and return
// zeros for those fields.
func (r *contactRepository) GetDetail(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, contactID uuid.UUID) (*models.ContactDetail, *errx.Error) {
	// 1. Core contact + categories + campaigns. Same shape as Search
	//    so the UI gets identical fields back.
	var detail models.ContactDetail
	var campaignsJSON, categoriesJSON []byte
	// Scope the contact row to the org so teammates can open each other's
	// contacts. Without an org (e.g. an API key with no selected org) fall
	// back to the legacy user scope. The campaign/category badge subselects
	// stay user-scoped (categories has no organization_id column).
	rowScope := "c.user_id = $1"
	if orgID != nil {
		rowScope = "c.organization_id = $3"
	}
	mainQuery := fmt.Sprintf(`
		SELECT
			c.id, c.first_name, c.last_name, c.email, c.company, c.phone,
			c.custom_fields, c.subscribed, c.updated_at, c.created_at,
			COALESCE(
				(
					SELECT json_agg(json_build_object('id', cam.id, 'name', cam.name))
					FROM   campaign_leads cl
					JOIN   campaigns cam ON cam.id = cl.campaign_id
					WHERE  cl.contact_id = c.id AND cam.user_id = $1
				), '[]'::json
			) AS campaigns,
			COALESCE(
				(
					SELECT json_agg(json_build_object('id', cat.id, 'title', cat.title, 'color', cat.color) ORDER BY cat.position ASC, cat.title ASC)
					FROM   contact_categories cc
					JOIN   categories cat ON cat.id = cc.category_id
					WHERE  cc.contact_id = c.id AND cat.user_id = $1
				), '[]'::json
			) AS categories
		FROM contacts c
		WHERE c.id = $2 AND %s
	`, rowScope)
	mainArgs := []any{userID, contactID}
	if orgID != nil {
		mainArgs = append(mainArgs, *orgID)
	}
	err := r.DB.QueryRow(ctx, mainQuery, mainArgs...).Scan(
		&detail.ID, &detail.FirstName, &detail.LastName, &detail.Email,
		&detail.Company, &detail.Phone, &detail.CustomFields, &detail.Subscribed,
		&detail.UpdatedAt, &detail.CreatedAt, &campaignsJSON, &categoriesJSON,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, mainQuery, mainArgs, "GetDetail main")
		return nil, errx.InternalError()
	}
	if detail.CustomFields == nil {
		detail.CustomFields = map[string]string{}
	}
	detail.Campaigns = []models.MiniCampaign{}
	if len(campaignsJSON) > 0 {
		var raw []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(campaignsJSON, &raw); err != nil {
			sentry.CaptureException(err)
			return nil, errx.InternalError()
		}
		detail.Campaigns = make([]models.MiniCampaign, len(raw))
		for i, m := range raw {
			detail.Campaigns[i] = models.MiniCampaign{ID: m.ID, Name: m.Name}
		}
	}
	detail.Categories = []models.MiniCategory{}
	if len(categoriesJSON) > 0 {
		if err := json.Unmarshal(categoriesJSON, &detail.Categories); err != nil {
			sentry.CaptureException(err)
			return nil, errx.InternalError()
		}
	}

	// 2. Engagement aggregates. campaign_contact_progress is the canonical
	//    sent/opened/clicked/replied/bounced ledger keyed by (campaign,
	//    contact, sequence). Counts come from non-null timestamp columns,
	//    "last X" comes from MAX() of each.
	engQuery := `
		SELECT
			COUNT(*) FILTER (WHERE sent_at    IS NOT NULL) AS sent,
			COUNT(*) FILTER (WHERE opened_at  IS NOT NULL) AS opened,
			COUNT(*) FILTER (WHERE clicked_at IS NOT NULL) AS clicked,
			COUNT(*) FILTER (WHERE replied_at IS NOT NULL) AS replied,
			COUNT(*) FILTER (WHERE bounced_at IS NOT NULL) AS bounced,
			MAX(sent_at), MAX(opened_at), MAX(clicked_at), MAX(replied_at), MAX(bounced_at)
		FROM campaign_contact_progress
		WHERE contact_id = $1
	`
	if err := r.DB.QueryRow(ctx, engQuery, contactID).Scan(
		&detail.Engagement.TotalSent, &detail.Engagement.TotalOpened,
		&detail.Engagement.TotalClicked, &detail.Engagement.TotalReplied,
		&detail.Engagement.TotalBounced,
		&detail.Engagement.LastSentAt, &detail.Engagement.LastOpenedAt,
		&detail.Engagement.LastClickedAt, &detail.Engagement.LastRepliedAt,
		&detail.Engagement.LastBouncedAt,
	); err != nil {
		db.CaptureError(err, engQuery, []any{contactID}, "GetDetail engagement")
		return nil, errx.InternalError()
	}

	// 3. Org-scoped extras. Only run when we have an org id.
	if orgID != nil {
		// Complaints don't live in campaign_contact_progress — they
		// arrive via deliverability_events. Count rows of type
		// "complaint" pointing at this contact (either by contact_id
		// or by recipient_email fallback for older rows).
		complaintQuery := `
			SELECT COUNT(*)
			FROM deliverability_events
			WHERE organization_id = $1
			  AND event_type = 'complaint'
			  AND (contact_id = $2 OR LOWER(recipient_email) = LOWER($3))
		`
		if err := r.DB.QueryRow(ctx, complaintQuery, *orgID, contactID, detail.Email).Scan(
			&detail.Engagement.TotalComplained,
		); err != nil {
			db.CaptureError(err, complaintQuery, []any{*orgID, contactID, detail.Email}, "GetDetail complaints")
			return nil, errx.InternalError()
		}

		// Suppression — there's at most one row per (org, email)
		// thanks to the unique constraint.
		suppQuery := `
			SELECT reason, source, expires_at, created_at
			FROM suppressed_recipients
			WHERE organization_id = $1 AND LOWER(email) = LOWER($2)
		`
		var s models.ContactSuppression
		err := r.DB.QueryRow(ctx, suppQuery, *orgID, detail.Email).Scan(
			&s.Reason, &s.Source, &s.ExpiresAt, &s.CreatedAt,
		)
		switch {
		case err == nil:
			detail.Suppression = &s
		case err == pgx.ErrNoRows:
			// not suppressed; leave nil
		default:
			db.CaptureError(err, suppQuery, []any{*orgID, detail.Email}, "GetDetail suppression")
			return nil, errx.InternalError()
		}
	}

	return &detail, nil
}

// ListSentEmails returns one row per task we sent (or attempted to
// send) to the contact, ordered by sent time DESC. Uses keyset
// pagination on (created_at, task_id) so we can scroll through the
// full history without blowing up offset.
//
// We deliberately scope by the contact's owning user via the
// campaign join — this keeps multi-tenant safety even though the
// tasks table itself has no user_id column.
func (r *contactRepository) ListSentEmails(ctx context.Context, userID, contactID uuid.UUID, limit int, beforeSentAt *time.Time, beforeTaskID *uuid.UUID) (*models.ContactSentEmailsResult, *errx.Error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	args := []any{userID, contactID}
	cursorClause := ""
	if beforeSentAt != nil && beforeTaskID != nil {
		cursorClause = "AND (t.created_at, t.id) < ($3, $4)"
		args = append(args, *beforeSentAt, *beforeTaskID)
	}
	args = append(args, limit+1)

	query := fmt.Sprintf(`
		SELECT
			t.id, t.status::text, t.message_id, t.created_at,
			ea.id, ea.email, ea.name,
			cam.id, cam.name,
			seq.id, seq.name,
			COALESCE(et.subject, seq.subject, '') AS subject,
			ccp.opened_at, ccp.clicked_at, ccp.replied_at, ccp.bounced_at
		FROM tasks t
		JOIN campaign_tasks ct ON ct.task_id = t.id
		LEFT JOIN email_accounts ea ON ea.id = t.email_account_id
		LEFT JOIN campaigns cam     ON cam.id = ct.campaign_id
		LEFT JOIN sequences seq     ON seq.id = ct.sequence_id
		LEFT JOIN email_tasks et    ON et.task_id = t.id
		LEFT JOIN campaign_contact_progress ccp
			   ON ccp.campaign_id = ct.campaign_id
			  AND ccp.contact_id  = ct.contact_id
			  AND ccp.sequence_id = ct.sequence_id
		WHERE ct.contact_id = $2
		  AND cam.user_id   = $1
		  %s
		ORDER BY t.created_at DESC, t.id DESC
		LIMIT $%d
	`, cursorClause, len(args))

	rows, err := r.DB.Query(ctx, query, args...)
	if err != nil {
		db.CaptureError(err, query, args, "ListSentEmails")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	out := make([]models.ContactSentEmail, 0, limit)
	for rows.Next() {
		var e models.ContactSentEmail
		if err := rows.Scan(
			&e.TaskID, &e.Status, &e.MessageID, &e.SentAt,
			&e.EmailAccountID, &e.EmailAccountEmail, &e.EmailAccountName,
			&e.CampaignID, &e.CampaignName,
			&e.SequenceID, &e.SequenceName,
			&e.Subject,
			&e.OpenedAt, &e.ClickedAt, &e.RepliedAt, &e.BouncedAt,
		); err != nil {
			db.CaptureError(err, "", nil, "ListSentEmails scan")
			return nil, errx.InternalError()
		}
		out = append(out, e)
	}

	hasMore := false
	var nextCursor *string
	if len(out) > limit {
		hasMore = true
		nextCursor = paging.EncodeUUID(out[limit].TaskID)
		out = out[:limit]
	}

	return &models.ContactSentEmailsResult{
		Data: out,
		Pagination: models.Pagination{
			NextCursor: nextCursor,
			HasMore:    hasMore,
		},
	}, nil
}

// ListTimeline merges per-contact events from several source tables
// into a single, reverse-chronological feed.
//
// Sources:
//   - campaign_contact_progress       → sent / opened / clicked / replied / bounced
//   - reply_intents                   → received replies (with intent classification)
//   - deliverability_events           → bounce / complaint
//   - suppressed_recipients           → suppression added
//   - contact_notes                   → CRM notes
//
// We pull up to (limit) candidates from each source ordered by time
// DESC, then merge-sort in Go. This avoids a 5-way UNION with
// matching column lists (each source has a different shape), and the
// per-source limit caps the read at roughly 5*limit rows.
//
// The `before` cursor is a wall-clock time; everything strictly older
// than it is eligible. The caller paginates by setting `before` to
// the oldest returned event's `At` on the next call.
func (r *contactRepository) ListTimeline(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, contactID uuid.UUID, limit int, before *time.Time) (*models.ContactTimelineResult, *errx.Error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	// We resolve the contact's email up front because some org-scoped
	// joins (suppression, deliverability fallback, reply_intents) key
	// off email rather than contact_id.
	var contactEmail string
	if err := r.DB.QueryRow(ctx,
		`SELECT email FROM contacts WHERE id = $1 AND user_id = $2`,
		contactID, userID,
	).Scan(&contactEmail); err != nil {
		if err == pgx.ErrNoRows {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, "", nil, "ListTimeline contact email")
		return nil, errx.InternalError()
	}

	// "before" defaults to "now + 1 minute" so the first page picks
	// up everything. Using a future bound keeps the SQL uniform — every
	// query passes the same predicate.
	bound := time.Now().Add(time.Minute)
	if before != nil {
		bound = *before
	}

	events := make([]models.ContactTimelineEvent, 0, limit*2)

	// 1. Engagement events from campaign_contact_progress. One progress
	//    row can emit up to 5 events (sent/opened/clicked/replied/bounced).
	progressQuery := `
		SELECT
			ccp.sent_at, ccp.opened_at, ccp.clicked_at, ccp.replied_at, ccp.bounced_at,
			cam.id, cam.name,
			seq.id, seq.name, seq.subject,
			ea.id, ea.email, ea.name
		FROM campaign_contact_progress ccp
		JOIN campaigns cam ON cam.id = ccp.campaign_id
		JOIN sequences seq ON seq.id = ccp.sequence_id
		LEFT JOIN LATERAL (
			SELECT ea.id, ea.email, ea.name
			FROM   tasks t
			JOIN   campaign_tasks ct ON ct.task_id = t.id
			JOIN   email_accounts ea ON ea.id = t.email_account_id
			WHERE  ct.campaign_id = ccp.campaign_id
			  AND  ct.contact_id  = ccp.contact_id
			  AND  ct.sequence_id = ccp.sequence_id
			ORDER  BY t.created_at DESC
			LIMIT  1
		) ea ON TRUE
		WHERE ccp.contact_id = $1
		  AND cam.user_id    = $2
		  AND COALESCE(ccp.sent_at, ccp.opened_at, ccp.clicked_at, ccp.replied_at, ccp.bounced_at) < $3
		ORDER BY GREATEST(
			COALESCE(ccp.sent_at,    'epoch'),
			COALESCE(ccp.opened_at,  'epoch'),
			COALESCE(ccp.clicked_at, 'epoch'),
			COALESCE(ccp.replied_at, 'epoch'),
			COALESCE(ccp.bounced_at, 'epoch')
		) DESC
		LIMIT $4
	`
	prows, err := r.DB.Query(ctx, progressQuery, contactID, userID, bound, limit)
	if err != nil {
		db.CaptureError(err, progressQuery, []any{contactID, userID, bound, limit}, "ListTimeline progress")
		return nil, errx.InternalError()
	}
	for prows.Next() {
		var sentAt, openedAt, clickedAt, repliedAt, bouncedAt *time.Time
		var campID, seqID, eaID *uuid.UUID
		var campName, seqName, seqSubject, eaEmail, eaName *string
		if err := prows.Scan(
			&sentAt, &openedAt, &clickedAt, &repliedAt, &bouncedAt,
			&campID, &campName,
			&seqID, &seqName, &seqSubject,
			&eaID, &eaEmail, &eaName,
		); err != nil {
			prows.Close()
			db.CaptureError(err, "", nil, "ListTimeline progress scan")
			return nil, errx.InternalError()
		}
		baseSubject := seqSubject
		makeEvent := func(t *time.Time, ty models.ContactTimelineEventType) {
			if t == nil || !t.Before(bound) {
				return
			}
			ev := models.ContactTimelineEvent{
				Type:              ty,
				At:                *t,
				EmailAccountID:    eaID,
				EmailAccountEmail: eaEmail,
				EmailAccountName:  eaName,
				CampaignID:        campID,
				CampaignName:      campName,
				SequenceID:        seqID,
				SequenceName:      seqName,
			}
			if baseSubject != nil && *baseSubject != "" {
				ev.Subject = baseSubject
			}
			events = append(events, ev)
		}
		makeEvent(sentAt, models.TimelineEmailSent)
		makeEvent(openedAt, models.TimelineEmailOpened)
		makeEvent(clickedAt, models.TimelineEmailClicked)
		makeEvent(repliedAt, models.TimelineEmailReplied)
		makeEvent(bouncedAt, models.TimelineEmailBounced)
	}
	prows.Close()

	if orgID != nil {
		// 2. Reply intents (inbound replies with classification).
		replyQuery := `
			SELECT ri.created_at, ri.intent, ri.campaign_id, cam.name, ri.task_id
			FROM reply_intents ri
			LEFT JOIN campaigns cam ON cam.id = ri.campaign_id
			WHERE ri.organization_id = $1
			  AND LOWER(ri.contact_email) = LOWER($2)
			  AND ri.created_at < $3
			ORDER BY ri.created_at DESC
			LIMIT $4
		`
		rrows, err := r.DB.Query(ctx, replyQuery, *orgID, contactEmail, bound, limit)
		if err != nil {
			db.CaptureError(err, replyQuery, nil, "ListTimeline replies")
			return nil, errx.InternalError()
		}
		for rrows.Next() {
			var ev models.ContactTimelineEvent
			var intent string
			if err := rrows.Scan(&ev.At, &intent, &ev.CampaignID, &ev.CampaignName, &ev.TaskID); err != nil {
				rrows.Close()
				db.CaptureError(err, "", nil, "ListTimeline replies scan")
				return nil, errx.InternalError()
			}
			ev.Type = models.TimelineReplyReceived
			ev.Intent = &intent
			events = append(events, ev)
		}
		rrows.Close()

		// 3. Deliverability events (bounce / complaint / unsubscribe).
		delivQuery := `
			SELECT de.created_at, de.event_type, de.provider, de.reason,
			       de.campaign_id, cam.name, de.task_id
			FROM deliverability_events de
			LEFT JOIN campaigns cam ON cam.id = de.campaign_id
			WHERE de.organization_id = $1
			  AND (de.contact_id = $2 OR LOWER(de.recipient_email) = LOWER($3))
			  AND de.created_at < $4
			ORDER BY de.created_at DESC
			LIMIT $5
		`
		drows, err := r.DB.Query(ctx, delivQuery, *orgID, contactID, contactEmail, bound, limit)
		if err != nil {
			db.CaptureError(err, delivQuery, nil, "ListTimeline deliv")
			return nil, errx.InternalError()
		}
		for drows.Next() {
			var ev models.ContactTimelineEvent
			var eventType, provider, reason string
			if err := drows.Scan(&ev.At, &eventType, &provider, &reason, &ev.CampaignID, &ev.CampaignName, &ev.TaskID); err != nil {
				drows.Close()
				db.CaptureError(err, "", nil, "ListTimeline deliv scan")
				return nil, errx.InternalError()
			}
			ev.Type = models.TimelineDeliverability
			ev.Source = &eventType
			ev.Provider = &provider
			if reason != "" {
				ev.Reason = &reason
			}
			events = append(events, ev)
		}
		drows.Close()

		// 4. Suppression — emit one event at create time. We treat
		//    later updates as the same event for now.
		suppQuery := `
			SELECT created_at, reason, source
			FROM suppressed_recipients
			WHERE organization_id = $1
			  AND LOWER(email) = LOWER($2)
			  AND created_at < $3
			ORDER BY created_at DESC
			LIMIT 1
		`
		var sAt time.Time
		var sReason, sSource string
		if err := r.DB.QueryRow(ctx, suppQuery, *orgID, contactEmail, bound).Scan(&sAt, &sReason, &sSource); err == nil {
			ev := models.ContactTimelineEvent{
				Type:   models.TimelineSuppressed,
				At:     sAt,
				Source: &sSource,
			}
			if sReason != "" {
				ev.Reason = &sReason
			}
			events = append(events, ev)
		} else if err != pgx.ErrNoRows {
			db.CaptureError(err, suppQuery, nil, "ListTimeline suppression")
			return nil, errx.InternalError()
		}

		// 5. Notes.
		notesQuery := `
			SELECT created_at, user_id, content
			FROM contact_notes
			WHERE contact_id = $1
			  AND organization_id = $2
			  AND created_at < $3
			ORDER BY created_at DESC
			LIMIT $4
		`
		nrows, err := r.DB.Query(ctx, notesQuery, contactID, *orgID, bound, limit)
		if err != nil {
			db.CaptureError(err, notesQuery, nil, "ListTimeline notes")
			return nil, errx.InternalError()
		}
		for nrows.Next() {
			var ev models.ContactTimelineEvent
			var uid uuid.UUID
			var content string
			if err := nrows.Scan(&ev.At, &uid, &content); err != nil {
				nrows.Close()
				db.CaptureError(err, "", nil, "ListTimeline notes scan")
				return nil, errx.InternalError()
			}
			ev.Type = models.TimelineNote
			ev.UserID = &uid
			ev.Content = &content
			events = append(events, ev)
		}
		nrows.Close()

		// 6. Meetings booked through a connected scheduling provider. The event
		//    time is when the booking arrived; scheduled_for carries the call
		//    window so the UI can render "Meeting on <date>".
		meetingQuery := `
			SELECT created_at, status, source, event_name, scheduled_for, join_url, canceled_reason
			FROM meeting_bookings
			WHERE contact_id = $1
			  AND organization_id = $2
			  AND created_at < $3
			ORDER BY created_at DESC
			LIMIT $4
		`
		mrows, err := r.DB.Query(ctx, meetingQuery, contactID, *orgID, bound, limit)
		if err != nil {
			db.CaptureError(err, meetingQuery, nil, "ListTimeline meetings")
			return nil, errx.InternalError()
		}
		for mrows.Next() {
			var ev models.ContactTimelineEvent
			var status, source, eventName, joinURL, canceledReason string
			var scheduledFor *time.Time
			if err := mrows.Scan(&ev.At, &status, &source, &eventName, &scheduledFor, &joinURL, &canceledReason); err != nil {
				mrows.Close()
				db.CaptureError(err, "", nil, "ListTimeline meetings scan")
				return nil, errx.InternalError()
			}
			switch status {
			case "rescheduled":
				ev.Type = models.TimelineMeetingRescheduled
			case "canceled":
				ev.Type = models.TimelineMeetingCanceled
			default:
				ev.Type = models.TimelineMeetingBooked
			}
			if eventName != "" {
				ev.Subject = &eventName
			}
			if source != "" {
				ev.Source = &source
			}
			if joinURL != "" {
				ev.JoinURL = &joinURL
			}
			if canceledReason != "" {
				ev.Reason = &canceledReason
			}
			ev.ScheduledFor = scheduledFor
			st := status
			ev.MeetingState = &st
			events = append(events, ev)
		}
		mrows.Close()
	}

	// Merge sort: newest first.
	sort.Slice(events, func(i, j int) bool { return events[i].At.After(events[j].At) })

	hasMore := false
	if len(events) > limit {
		hasMore = true
		events = events[:limit]
	}

	return &models.ContactTimelineResult{
		Data:    events,
		HasMore: hasMore,
	}, nil
}

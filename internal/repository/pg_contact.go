package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/email"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
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
	// ListVerificationCandidates returns contacts due for a check: never
	// checked, or checked long enough ago that the verdict has aged out.
	// Manual verdicts are never candidates. Oldest first.
	ListVerificationCandidates(ctx context.Context, limit int) ([]VerificationCandidate, *errx.Error)
	// SetContactsVerification stores one verdict on many of the org's contacts
	// (a manual "mark deliverable"). Returns how many rows changed.
	SetContactsVerification(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID, w models.ContactVerificationWrite) (int, *errx.Error)
	// ResetContactsVerification clears the verdict so the scheduler checks the
	// contacts again on its next pass. Returns how many rows changed.
	ResetContactsVerification(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) (int, *errx.Error)
	// UndeliverableLeadIDs lists the campaign's leads verification refused.
	UndeliverableLeadIDs(ctx context.Context, orgID, campaignID uuid.UUID) ([]uuid.UUID, *errx.Error)
	// VerificationCounts is the org's contacts by verdict.
	VerificationCounts(ctx context.Context, orgID uuid.UUID) (models.ContactVerificationCounts, *errx.Error)
	// SetContactESP caches the recipient ESP/provider resolved from the contact's
	// domain (control-plane only, no MX dial). Best-effort: a failure should not
	// block sending.
	SetContactESP(ctx context.Context, contactID uuid.UUID, provider string) error
	GetByEmailsAndUser(ctx context.Context, userID uuid.UUID, emails []string) (map[string]models.Contact, *errx.Error)
	// ResolveCategoryNames maps category titles (as typed in an imported file)
	// to the workspace's category IDs, creating the ones that don't exist yet.
	// Keys of the returned map are the lowercased titles.
	ResolveCategoryNames(ctx context.Context, orgID, userID uuid.UUID, names []string) (map[string]uuid.UUID, *errx.Error)
	Search(ctx context.Context, userID string, category *string, cursor *paging.SortCursor, filters models.SearchContacts, limit int32) (*models.ContactsResult, *errx.Error)
	// SearchIDs returns the ids of every contact matching the same request
	// Search runs, capped at max+1 rows so the caller can tell "exactly max"
	// from "more than max". Backs the dashboard's "select all matching" bulk
	// actions, which name a filter instead of listing ids.
	SearchIDs(ctx context.Context, orgID string, filters models.SearchContacts, max int) ([]uuid.UUID, *errx.Error)
	// SearchCounts returns org-wide contact facet totals for the browse
	// sidebar (independent of any search filters), mirroring campaigns-overview.
	SearchCounts(ctx context.Context, orgID string) (*models.ContactsCounts, *errx.Error)
	// CampaignLeadCounts returns per-status lead totals for one campaign (the
	// Leads-view scope chips), independent of the request's lead_status filter.
	CampaignLeadCounts(ctx context.Context, orgID, campaignID string) (*models.CampaignLeadCounts, *errx.Error)
	ExportAll(ctx context.Context, orgID string, filters *models.SearchContacts, contactIDs []string, max int) ([]models.Contact, *errx.Error)
	BulkUpdate(ctx context.Context, userID string, orgID uuid.UUID, data *models.BulkEditContactsData) ([]models.Contact, *errx.Error)
	Update(ctx context.Context, userID, contactID string, orgID uuid.UUID, data *models.UpdateContact) (*models.Contact, *errx.Error)
	// SetSubscribedByEmail flips the subscription flag on every contact in the
	// organization with that address; a recipient's own opt-out or
	// resubscribe is recorded on the contact as well as the suppression list.
	SetSubscribedByEmail(ctx context.Context, orgID uuid.UUID, email string, subscribed bool) error
	BulkDelete(ctx context.Context, userID string, orgID uuid.UUID, contactIDs []string) *errx.Error
	Delete(ctx context.Context, userID string, orgID uuid.UUID, contactID string) *errx.Error
	GetContactCount(ctx context.Context, userID string) (int, *errx.Error)

	// DistinctCustomFieldKeys returns the org's distinct contact custom-field
	// keys, frequency-ranked (most common first) then alphabetical, capped at
	// 200. Powers the dashboard variable picker's real-field suggestions.
	DistinctCustomFieldKeys(ctx context.Context, orgID uuid.UUID) ([]string, error)

	// 360 view read paths. orgID is optional — when nil, the suppression
	// + deliverability + reply joins are skipped (they're org-scoped).
	GetDetail(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, contactID uuid.UUID) (*models.ContactDetail, *errx.Error)
	ListSentEmails(ctx context.Context, userID, contactID uuid.UUID, limit int, beforeSentAt *time.Time, beforeTaskID *uuid.UUID) (*models.ContactSentEmailsResult, *errx.Error)
	ListTimeline(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, contactID uuid.UUID, limit int, cursor *models.ContactTimelineKey) (*models.ContactTimelineResult, *errx.Error)
	// ListCampaignStates returns the contact's campaigns with their flow,
	// this contact's progress on every step, and the derived lead status.
	ListCampaignStates(ctx context.Context, orgID, contactID uuid.UUID) ([]models.ContactCampaignState, *errx.Error)
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

// parseUUIDList accepts string IDs from a JSON body and returns a
// deduped slice of uuid.UUIDs. Empty strings are skipped silently —
// they're a normal artifact of clients sending [""] to "clear" a list.
// A malformed (non-UUID) entry is a client bug worth surfacing as 400.
func parseUUIDList(raw []string) ([]uuid.UUID, *errx.Error) {
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

// normalizeCustomFields trims and collapses whitespace in every custom-field
// key, then rejects the map if any key is still unusable. Keys are user data
// (CSV headers, API payloads) so " Company Mobile" and "Company Mobile" must
// land on the same JSONB key instead of splitting the field in two.
func normalizeCustomFields(in map[string]string) (map[string]string, *errx.Error) {
	if len(in) == 0 {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		key := utils.NormalizeJSONKey(k)
		if !utils.IsValidJSONKey(key) {
			return nil, errx.New(errx.BadRequest,
				"invalid custom field name "+strconv.Quote(k)+": "+utils.JSONKeyRules)
		}
		out[key] = v
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
	segmentIDs := make([][]uuid.UUID, 0, len(contacts))
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
		fields, xerr := normalizeCustomFields(lead.CustomFields)
		if xerr != nil {
			return nil, xerr
		}
		lead.CustomFields = fields

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

		// Parse + dedupe segment IDs. Same rules again; these become manual
		// include overrides written in this transaction, so a contact created
		// inside a segment cannot come back without it.
		segSet := make(map[uuid.UUID]struct{}, len(lead.Segments))
		segs := make([]uuid.UUID, 0, len(lead.Segments))
		for _, raw := range lead.Segments {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			sid, serr := uuid.Parse(raw)
			if serr != nil {
				return nil, errx.ErrUuid
			}
			if _, dup := segSet[sid]; dup {
				continue
			}
			segSet[sid] = struct{}{}
			segs = append(segs, sid)
		}

		// First-touch attribution. Every creation path stamps one; an empty
		// value is recorded honestly rather than guessed.
		if lead.Source == "" {
			lead.Source = models.ContactSourceUnknown
		}
		if !lead.Source.Valid() {
			return nil, errx.New(errx.BadRequest, "invalid contact source")
		}
		lead.SourceDetail = strings.TrimSpace(lead.SourceDetail)

		// A verdict the caller brought along, in any vocabulary we can read.
		if v, xerr := verificationFromRequest(lead.VerificationStatus, lead.VerificationProvider); xerr != nil {
			return nil, xerr
		} else if v != nil {
			lead.Verification = v
		}

		normalized = append(normalized, lead)
		campaignIDs = append(campaignIDs, cids)
		categoryIDs = append(categoryIDs, cats)
		segmentIDs = append(segmentIDs, segs)
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
		var vStatus, vSub, vReason, vProvider string
		if lead.Verification != nil {
			vStatus, vSub, vReason, vProvider = lead.Verification.Status, lead.Verification.SubStatus, lead.Verification.Reason, lead.Verification.Provider
		}
		insertBatch.Queue(
			// $9 and $10 are the same value: a parameter used both as an
			// INSERT value and inside the DO UPDATE set gives Postgres two
			// inference sites for one placeholder, so it is passed twice with
			// explicit casts. NULL means "leave the flag alone".
			`INSERT INTO contacts (
			 id, user_id, organization_id, first_name, last_name, email, company, phone, custom_fields, subscribed,
			 source, source_detail, first_seen_at,
			 verification_status, verification_sub_status, verification_reason, verification_provider,
			 verification_source, is_catch_all, verification_checked_at
			 ) VALUES (
			  gen_random_uuid(), $1, $2, $3, $4, LOWER($5), $6, $7, $8, COALESCE($9::boolean, TRUE),
			  $11, $12, NOW(),
			  COALESCE(NULLIF($13::text, ''), 'unknown'), $14, $15, $16,
			  CASE WHEN $13::text <> '' THEN 'imported' ELSE '' END,
			  ($14::text = 'catch_all'),
			  CASE WHEN $13::text <> '' THEN NOW() ELSE NULL END
			 )
			 ON CONFLICT (user_id, (LOWER(email))) DO UPDATE SET
			  organization_id = COALESCE(contacts.organization_id, EXCLUDED.organization_id),
			  -- Enrich, never erase: a blank cell in a re-imported file must
			  -- not wipe a name we already have.
			  first_name = COALESCE(NULLIF(EXCLUDED.first_name, ''), contacts.first_name),
			  last_name = COALESCE(NULLIF(EXCLUDED.last_name, ''), contacts.last_name),
			  company = COALESCE(NULLIF(EXCLUDED.company, ''), contacts.company),
			  phone = COALESCE(NULLIF(EXCLUDED.phone, ''), contacts.phone),
			  custom_fields = contacts.custom_fields || EXCLUDED.custom_fields,
			  subscribed = COALESCE($10::boolean, contacts.subscribed),
			  -- A verdict in the file replaces whatever was recorded; a file
			  -- without one leaves the existing verdict alone.
			  verification_status = CASE WHEN $13::text <> '' THEN $13 ELSE contacts.verification_status END,
			  verification_sub_status = CASE WHEN $13::text <> '' THEN $14 ELSE contacts.verification_sub_status END,
			  verification_reason = CASE WHEN $13::text <> '' THEN $15 ELSE contacts.verification_reason END,
			  verification_provider = CASE WHEN $13::text <> '' THEN $16 ELSE contacts.verification_provider END,
			  verification_source = CASE WHEN $13::text <> '' THEN 'imported' ELSE contacts.verification_source END,
			  is_catch_all = CASE WHEN $13::text <> '' THEN ($14::text = 'catch_all') ELSE contacts.is_catch_all END,
			  verification_checked_at = CASE WHEN $13::text <> '' THEN NOW() ELSE contacts.verification_checked_at END,
			  updated_at = NOW()
			 -- xmax = 0 only on a fresh row: the source is first-touch, so an
			 -- upsert that hit an existing contact is not a creation.
			 RETURNING id, first_name, last_name, email, company, phone, custom_fields, subscribed, updated_at, created_at, (xmax = 0)`,
			userID, orgID, lead.FirstName, lead.LastName, lead.Email, lead.Company, lead.Phone, lead.CustomFields,
			lead.Subscribed, lead.Subscribed, string(lead.Source), lead.SourceDetail,
			vStatus, vSub, vReason, vProvider,
		)
	}

	br := tx.SendBatch(ctx, &insertBatch)

	ncontacts := make([]models.Contact, 0, len(normalized))
	var createdIDs []uuid.UUID
	created := make([]bool, 0, len(normalized))
	for range normalized {
		ncon := models.Contact{
			Campaigns:  []models.MiniCampaign{},
			Categories: []models.MiniCategory{},
			Subscribed: true,
		}
		var inserted bool
		if err := br.QueryRow().Scan(
			&ncon.ID, &ncon.FirstName, &ncon.LastName, &ncon.Email, &ncon.Company,
			&ncon.Phone, &ncon.CustomFields, &ncon.Subscribed, &ncon.UpdatedAt, &ncon.CreatedAt, &inserted,
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
		ncon.IsNew = inserted
		ncontacts = append(ncontacts, ncon)
		created = append(created, inserted)
		if inserted {
			createdIDs = append(createdIDs, ncon.ID)
		}
	}
	if err := br.Close(); err != nil {
		db.CaptureError(err, "", nil, "batch close")
		return nil, errx.InternalError()
	}

	// Lifecycle events for the activity timeline, written in this transaction.
	actor := actorID(userID)
	var campaignLinks, categoryLinks []contactLink

	// Link campaigns. Original code's RETURNING clause referenced a
	// non-inserted table, which is invalid SQL; resolve by inserting
	// first, then SELECTing the name back from `campaigns` in a
	// separate statement. Scoped to the organization's campaigns, not the
	// caller's own: campaigns are org assets (issue #187).
	for i, cids := range campaignIDs {
		if len(cids) == 0 {
			continue
		}
		lrows, err := tx.Query(ctx, `
			INSERT INTO campaign_leads (contact_id, campaign_id)
			SELECT $1, c.id
			FROM   campaigns c
			WHERE  c.id = ANY($2) AND c.organization_id = $3
			ON CONFLICT (campaign_id, contact_id) DO NOTHING
			RETURNING campaign_id
		`, ncontacts[i].ID, cids, orgID)
		if err != nil {
			db.CaptureError(err, "", nil, "campaign_leads insert")
			return nil, errx.InternalError()
		}
		added, err := collectLinks(lrows, ncontacts[i].ID)
		if err != nil {
			db.CaptureError(err, "", nil, "campaign_leads returning")
			return nil, errx.InternalError()
		}
		campaignLinks = append(campaignLinks, added...)
		// A hand-picked add ends a manual removal, as the bulk add does.
		if _, err := tx.Exec(ctx, `DELETE FROM campaign_lead_removals WHERE contact_id = $1 AND campaign_id = ANY($2)`, ncontacts[i].ID, cids); err != nil {
			db.CaptureError(err, "", nil, "campaign_lead_removals clear")
			return nil, errx.InternalError()
		}
		// A contact created from a campaign's Leads tab is attributed to that
		// campaign by name, resolved here rather than trusted from the client.
		if created[i] && normalized[i].Source == models.ContactSourceCampaign && normalized[i].SourceDetail == "" && len(added) > 0 {
			if _, err := tx.Exec(ctx, `
				UPDATE contacts SET source_detail = cam.name
				FROM   campaigns cam
				WHERE  contacts.id = $1 AND cam.id = $2
			`, ncontacts[i].ID, added[0].LinkID); err != nil {
				db.CaptureError(err, "", nil, "source_detail from campaign")
				return nil, errx.InternalError()
			}
		}

		rows, err := tx.Query(ctx, `
			SELECT c.id, c.name
			FROM   campaigns c
			JOIN   campaign_leads cl ON cl.campaign_id = c.id
			WHERE  cl.contact_id = $1 AND c.organization_id = $2
		`, ncontacts[i].ID, orgID)
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

	// Link categories. Scoped to the workspace's own categories so a
	// malicious or stale ID can't attach foreign data.
	for i, cats := range categoryIDs {
		if len(cats) == 0 {
			continue
		}
		crows, err := tx.Query(ctx, `
			INSERT INTO contact_categories (contact_id, category_id)
			SELECT $1, cat.id
			FROM   categories cat
			WHERE  cat.id = ANY($2) AND cat.organization_id = $3
			ON CONFLICT (contact_id, category_id) DO NOTHING
			RETURNING category_id
		`, ncontacts[i].ID, cats, orgID)
		if err != nil {
			db.CaptureError(err, "", nil, "contact_categories insert")
			return nil, errx.InternalError()
		}
		added, err := collectLinks(crows, ncontacts[i].ID)
		if err != nil {
			db.CaptureError(err, "", nil, "contact_categories returning")
			return nil, errx.InternalError()
		}
		categoryLinks = append(categoryLinks, added...)

		rows, err := tx.Query(ctx, `
			SELECT cat.id, cat.title, cat.color
			FROM   categories cat
			JOIN   contact_categories cc ON cc.category_id = cat.id
			WHERE  cc.contact_id = $1 AND cat.organization_id = $2
			ORDER BY cat.position ASC, cat.title ASC
		`, ncontacts[i].ID, orgID)
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

	// Pin into segments, in the same transaction as the contact itself: a
	// failed override write must not answer with a contact that quietly never
	// joined the segment it was created in (issue #285). Scoped to the org, so
	// a foreign segment id links nothing.
	for i, segs := range segmentIDs {
		if len(segs) == 0 {
			continue
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO segment_members (segment_id, contact_id, mode)
			SELECT s.id, $1, 'include'
			FROM   segments s
			WHERE  s.id = ANY($2) AND s.organization_id = $3
			ON CONFLICT (segment_id, contact_id) DO UPDATE SET mode = EXCLUDED.mode, created_at = now()
		`, ncontacts[i].ID, segs, orgID)
		if err != nil {
			db.CaptureError(err, "", nil, "segment_members insert")
			return nil, errx.InternalError()
		}
		// One row per requested segment, since `segs` is deduplicated and a
		// conflicting row still counts as affected. Fewer means a segment was
		// deleted between the service's check and this statement, so the
		// create is rolled back rather than answered without the membership.
		if int(tag.RowsAffected()) != len(segs) {
			return nil, errx.New(errx.BadRequest, "a selected segment does not exist")
		}
	}

	if err := logContactCreated(ctx, tx, orgID, actor, createdIDs); err != nil {
		db.CaptureError(err, "", nil, "contact_created activity")
		return nil, errx.InternalError()
	}
	if err := logCampaignLinks(ctx, tx, orgID, actor, models.ActivityCampaignAdded, campaignLinks); err != nil {
		db.CaptureError(err, "", nil, "campaign_added activity")
		return nil, errx.InternalError()
	}
	if err := logCategoryLinks(ctx, tx, orgID, actor, models.ActivityCategoryAdded, categoryLinks); err != nil {
		db.CaptureError(err, "", nil, "category_added activity")
		return nil, errx.InternalError()
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
			c.verification_source, c.verification_provider, c.verification_sub_status, c.verification_confidence,
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
		&contact.VerificationSource, &contact.VerificationProvider, &contact.VerificationSubStatus, &contact.VerificationConfidence,
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
func (r *contactRepository) SetSubscribedByEmail(ctx context.Context, orgID uuid.UUID, email string, subscribed bool) error {
	_, err := r.DB.Exec(ctx,
		`UPDATE contacts SET subscribed = $3, updated_at = NOW()
		 WHERE organization_id = $1 AND LOWER(email) = LOWER($2) AND subscribed IS DISTINCT FROM $3`,
		orgID, email, subscribed)
	return err
}

func (r *contactRepository) UpdateContactVerification(ctx context.Context, contactID uuid.UUID, res emailverify.Result) *errx.Error {
	status := string(res.Status)
	if status == "" {
		status = string(emailverify.StatusUnknown)
	}
	checkedAt := res.CheckedAt
	if checkedAt.IsZero() {
		checkedAt = time.Now().UTC()
	}
	provider := res.Provider
	if provider == "" {
		provider = emailverify.ProviderBuiltin
	}
	source := models.VerificationSourceProvider
	if provider == emailverify.ProviderBuiltin {
		source = models.VerificationSourceProbe
	}

	query := `
		UPDATE contacts
		SET verification_status = $2,
		    verification_reason = $3,
		    is_catch_all = $4,
		    verification_checked_at = $5,
		    verification_source = $6,
		    verification_provider = $7,
		    verification_sub_status = $8,
		    verification_confidence = $9,
		    updated_at = NOW()
		WHERE id = $1
	`
	params := []any{contactID, status, res.Reason, res.IsCatchAll, checkedAt, source, provider, string(res.SubStatus), res.Confidence}
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

// VerificationCandidate is one contact due for a verification check.
type VerificationCandidate struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	Email          string
	// Requested is true when a member asked for this check (the verdict was
	// reset), so it is worth spending a paid credit on even when the
	// organization has none to spare.
	Requested bool
}

// ListVerificationCandidates returns up to `limit` contacts due for a check.
// Never-checked contacts come first (a reset counts as never checked), then
// verdicts older than their shelf life: an unknown verdict is retried after
// config.VerificationUnknownRecheckDays, everything else after
// config.VerificationRecheckDays. Manual verdicts are never re-checked.
//
// A built-in verdict that predates a connected verifier is reopened once, so
// connecting one actually reaches the addresses it was connected for.
func (r *contactRepository) ListVerificationCandidates(ctx context.Context, limit int) ([]VerificationCandidate, *errx.Error) {
	if limit <= 0 {
		limit = 100
	}
	query := `
		SELECT c.id, c.organization_id, c.email, c.verification_checked_at IS NULL
		FROM contacts c
		WHERE c.organization_id IS NOT NULL
		  AND c.verification_source <> 'manual'
		  -- Real mail seen recently excuses the address from a check.
		  AND (c.verification_evidence_at IS NULL OR c.verification_evidence_at < NOW() - make_interval(days => $4))
		  AND (
		    c.verification_checked_at IS NULL
		    OR (c.verification_status = 'unknown' AND c.verification_checked_at < NOW() - make_interval(days => $2))
		    OR c.verification_checked_at < NOW() - make_interval(days => $3)
		    -- A built-in verdict reached before the workspace connected a
		    -- verifier was reached without it; re-check once rather than after
		    -- the shelf life. Only the built-in probe's verdicts: one a paid
		    -- verifier already produced is a real answer, and re-checking it
		    -- because another verifier was connected spends a credit for
		    -- nothing.
		    OR (
		      c.verification_provider = $6
		      AND EXISTS (
		        SELECT 1 FROM integration_connections ic
		        WHERE ic.organization_id = c.organization_id
		          AND ic.provider = ANY($5)
		          AND ic.status <> 'disconnected'
		          AND c.verification_checked_at < ic.created_at
		      )
		    )
		  )
		ORDER BY c.verification_checked_at ASC NULLS FIRST, c.created_at ASC
		LIMIT $1
	`
	providers := make([]string, 0, len(models.VerificationProviders))
	for _, p := range models.VerificationProviders {
		providers = append(providers, string(p))
	}
	params := []any{
		limit, config.VerificationUnknownRecheckDays, config.VerificationRecheckDays,
		config.VerificationEvidenceFreshDays, providers, emailverify.ProviderBuiltin,
	}
	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	out := make([]VerificationCandidate, 0, limit)
	for rows.Next() {
		var c VerificationCandidate
		if err := rows.Scan(&c.ID, &c.OrganizationID, &c.Email, &c.Requested); err != nil {
			db.CaptureError(err, "", nil, "ListVerificationCandidates scan")
			return nil, errx.InternalError()
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, "", nil, "ListVerificationCandidates rows")
		return nil, errx.InternalError()
	}
	return out, nil
}

// SetContactsVerification writes one verdict onto the org's listed contacts.
func (r *contactRepository) SetContactsVerification(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID, w models.ContactVerificationWrite) (int, *errx.Error) {
	if len(ids) == 0 {
		return 0, nil
	}
	query := `
		UPDATE contacts
		SET verification_status = $3,
		    verification_sub_status = $4,
		    verification_reason = $5,
		    verification_provider = $6,
		    verification_source = $7,
		    is_catch_all = ($4 = 'catch_all'),
		    verification_checked_at = NOW(),
		    updated_at = NOW()
		WHERE organization_id = $1 AND id = ANY($2)
	`
	params := []any{orgID, ids, w.Status, w.SubStatus, w.Reason, w.Provider, w.Source}
	cmd, err := r.DB.Exec(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return 0, errx.InternalError()
	}
	return int(cmd.RowsAffected()), nil
}

// ResetContactsVerification returns the org's listed contacts to "never
// checked" so the next scheduler pass picks them up first.
func (r *contactRepository) ResetContactsVerification(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) (int, *errx.Error) {
	if len(ids) == 0 {
		return 0, nil
	}
	query := `
		UPDATE contacts
		SET verification_status = 'unknown',
		    verification_sub_status = '',
		    verification_reason = 'verification requested',
		    verification_provider = '',
		    verification_source = '',
		    is_catch_all = false,
		    verification_checked_at = NULL,
		    updated_at = NOW()
		WHERE organization_id = $1 AND id = ANY($2)
	`
	params := []any{orgID, ids}
	cmd, err := r.DB.Exec(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return 0, errx.InternalError()
	}
	return int(cmd.RowsAffected()), nil
}

// UndeliverableLeadIDs lists the campaign's leads the routing predicate skips
// for verification reasons (invalid, or risky with the risky toggle off).
func (r *contactRepository) UndeliverableLeadIDs(ctx context.Context, orgID, campaignID uuid.UUID) ([]uuid.UUID, *errx.Error) {
	query := `
		SELECT c.id
		FROM campaign_leads cl
		JOIN contacts c ON c.id = cl.contact_id
		JOIN campaigns cp ON cp.id = cl.campaign_id
		WHERE cl.campaign_id = $1 AND cp.organization_id = $2
		  AND (c.verification_status = 'invalid' OR (c.verification_status = 'risky' AND NOT cp.risky_emails))
	`
	params := []any{campaignID, orgID}
	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			db.CaptureError(err, "", nil, "UndeliverableLeadIDs scan")
			return nil, errx.InternalError()
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, "", nil, "UndeliverableLeadIDs rows")
		return nil, errx.InternalError()
	}
	return out, nil
}

// VerificationCounts is the org's contacts by verdict.
func (r *contactRepository) VerificationCounts(ctx context.Context, orgID uuid.UUID) (models.ContactVerificationCounts, *errx.Error) {
	var c models.ContactVerificationCounts
	query := `
		SELECT
			COUNT(*) FILTER (WHERE verification_status = 'valid'),
			COUNT(*) FILTER (WHERE verification_status = 'risky'),
			COUNT(*) FILTER (WHERE verification_status = 'invalid'),
			COUNT(*) FILTER (WHERE verification_status NOT IN ('valid','risky','invalid')),
			COUNT(*) FILTER (WHERE verification_checked_at IS NULL)
		FROM contacts
		WHERE organization_id = $1
	`
	if err := r.DB.QueryRow(ctx, query, orgID).Scan(&c.Valid, &c.Risky, &c.Invalid, &c.Unknown, &c.Pending); err != nil {
		db.CaptureError(err, query, []any{orgID}, "queryrow")
		return c, errx.InternalError()
	}
	return c, nil
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

// contactSortKind decides three things that have to agree: how a row's sort
// value is written into the cursor, how the cursor's text is cast back for the
// comparison, and what counts as a well-formed boundary.
type contactSortKind int

const (
	sortText contactSortKind = iota
	sortTimestamp
	sortNumber
)

// contactSortTimeLayout mirrors the to_char pattern below, so Go validates
// exactly the boundaries Postgres will accept.
const contactSortTimeLayout = "2006-01-02 15:04:05.000000"

// contactSort describes one sortable column of the contacts list: the SQL to
// order and compare on, its kind, and whether the column admits NULL (which
// decides where the NULL block sits in the order). Every column here is
// currently NOT NULL; flipping `nullable` turns on the NULL-aware keyset
// branches so a nullable sort cannot silently truncate the list.
type contactSort struct {
	expr     string
	kind     contactSortKind
	nullable bool
}

// render is the SELECT expression that puts a row's sort value in the cursor.
// Timestamps get an explicit pattern rather than ::text so a token does not
// depend on the server's DateStyle.
func (s contactSort) render() string {
	if s.kind == sortTimestamp {
		return fmt.Sprintf("to_char(%s, 'YYYY-MM-DD HH24:MI:SS.US')", s.expr)
	}
	return "(" + s.expr + ")::text"
}

// bound casts a cursor's text boundary back to what expr compares in.
func (s contactSort) bound(placeholder string) string {
	switch s.kind {
	case sortTimestamp:
		return placeholder + "::text::timestamp"
	case sortNumber:
		return placeholder + "::text::bigint"
	default:
		return placeholder + "::text"
	}
}

// wellFormed rejects a boundary the cast would choke on, so a hand-made cursor
// is a 400 instead of a database error surfacing as a 500.
func (s contactSort) wellFormed(v string) bool {
	switch s.kind {
	case sortTimestamp:
		_, err := time.Parse(contactSortTimeLayout, v)
		return err == nil
	case sortNumber:
		_, err := strconv.ParseInt(v, 10, 64)
		return err == nil
	default:
		return true
	}
}

// campaignCountLateral counts one contact's campaign memberships, joined only
// when a filter or the sort actually needs it.
const campaignCountLateral = `LEFT JOIN LATERAL (
			SELECT COUNT(*) AS campaign_count
			FROM campaign_leads cl1
			WHERE cl1.contact_id = c.id
		) cl ON TRUE`

var contactSorts = map[string]contactSort{
	"first_name":     {expr: "c.first_name", kind: sortText},
	"last_name":      {expr: "c.last_name", kind: sortText},
	"email":          {expr: "c.email", kind: sortText},
	"created_at":     {expr: "c.created_at", kind: sortTimestamp},
	"updated_at":     {expr: "c.updated_at", kind: sortTimestamp},
	"campaign_count": {expr: "COALESCE(cl.campaign_count,0)", kind: sortNumber},
}

// contactFilter is a compiled contact search: the WHERE terms, the args they
// bind, the next free placeholder, and (single-campaign Leads view only) the
// placeholder the lead-progress subquery reuses.
type contactFilter struct {
	clauses        []string
	args           []any
	nextArg        int
	singleCampaign string
}

// contactSearchMaxTerms bounds how many words one search box turns into ILIKE
// terms; past a handful the extra scans cost more than they narrow.
const contactSearchMaxTerms = 6

// contactSearchTerms splits a contact search into the words that must each
// match some field. An empty or whitespace-only query yields no terms, which
// leaves the search unfiltered exactly as before.
func contactSearchTerms(query string) []string {
	terms := strings.Fields(query)
	if len(terms) > contactSearchMaxTerms {
		terms = terms[:contactSearchMaxTerms]
	}
	return terms
}

// buildContactFilter compiles a search request into WHERE terms. Search and
// SearchIDs share it so a "select all" bulk action resolves exactly the rows
// the list was showing, filter for filter.
func (r *contactRepository) buildContactFilter(ctx context.Context, orgID string, filters models.SearchContacts) (*contactFilter, *errx.Error) {
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
	// Every word of the query has to match one of the fields, rather than the
	// query as a whole matching one of them: no single column holds both
	// halves of a person's name, so "Test Demo" found nothing while "Test"
	// found the contact (issue #413).
	for _, term := range contactSearchTerms(filters.Query) {
		whereClauses = append(whereClauses, fmt.Sprintf(`
			(c.first_name ILIKE $%d OR
			 c.last_name ILIKE $%d OR
			 c.email ILIKE $%d OR
			 c.company ILIKE $%d OR
			 c.phone ILIKE $%d)
		`, argIndex, argIndex, argIndex, argIndex, argIndex))
		args = append(args, "%"+term+"%")
		argIndex++
	}

	// -----------------------------
	// Custom field filters (JSONB)
	// -----------------------------
	for _, f := range filters.CustomFieldFilters {
		name := utils.NormalizeJSONKey(f.Name)
		if name == "" || f.Value == "" || !utils.IsValidJSONKey(name) {
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
		// The key is a bound parameter, never interpolated: custom-field
		// names are user data and now legitimately contain spaces.
		whereClauses = append(whereClauses, fmt.Sprintf(`c.custom_fields ->> $%d::text %s $%d`, argIndex, op, argIndex+1))
		args = append(args, name, val)
		argIndex += 2
	}

	// -----------------------------
	// Subscription filter
	// -----------------------------
	if filters.Subscribed != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("c.subscribed = $%d", argIndex))
		args = append(args, *filters.Subscribed)
		argIndex++
	}
	if filters.VerificationStatus != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("c.verification_status = $%d", argIndex))
		args = append(args, filters.VerificationStatus)
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
		// One EXISTS per campaign ("in ALL of them"), each a primary-key probe
		// the planner can run either way round: driving from the ordered
		// contacts index for a broad campaign, or from the leads for a narrow
		// one. A GROUP BY/HAVING over campaign_leads forces the second.
		for _, ph := range placeholders {
			whereClauses = append(whereClauses, fmt.Sprintf(
				"EXISTS (SELECT 1 FROM campaign_leads cle WHERE cle.campaign_id = %s AND cle.contact_id = c.id)", ph))
		}
	}

	// -----------------------------
	// Lead status filter (single-campaign Leads view only)
	// -----------------------------
	// The derived status has no stored column, so reproduce the same priority
	// chain used on read (unsubscribed > bounced > replied > failed >
	// processing > queued) as a boolean predicate over the campaign's progress
	// rows. Only
	// meaningful with exactly one campaign bound; ignored otherwise.
	if filters.LeadStatus != "" && singleCampaignPlaceholder != "" {
		if clause := leadStatusClause(filters.LeadStatus, singleCampaignPlaceholder); clause != "" {
			whereClauses = append(whereClauses, clause)
		}
	}
	// Engagement composes with lead_status as AND, so "replied AND not_opened"
	// is a valid (if odd) combination rather than one overriding the other.
	if filters.Engagement != "" && singleCampaignPlaceholder != "" {
		if clause := leadEngagementClause(filters.Engagement, singleCampaignPlaceholder); clause != "" {
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
		for _, ph := range placeholders {
			whereClauses = append(whereClauses, fmt.Sprintf(
				"EXISTS (SELECT 1 FROM contact_categories cce WHERE cce.category_id = %s AND cce.contact_id = c.id)", ph))
		}
	}

	// -----------------------------
	// Segment membership (must be in ALL specified segments)
	// -----------------------------
	// Each segment compiles to its own predicate; args grow in lockstep with
	// argIndex, which the builder relies on.
	for _, raw := range filters.SegmentIDs {
		sid, err := uuid.Parse(raw)
		if err != nil {
			return nil, errx.New(errx.BadRequest, "invalid segment id")
		}
		orgUUID, err := uuid.Parse(orgID)
		if err != nil {
			return nil, errx.New(errx.BadRequest, "invalid organization id")
		}
		clause, nextArgs, cerr := compileSavedSegment(ctx, r.DB, orgUUID, sid, args)
		if cerr != nil {
			db.CaptureError(cerr, "segment compile", nil, "query")
			return nil, errx.InternalError()
		}
		args = nextArgs
		argIndex = len(args) + 1
		whereClauses = append(whereClauses, "("+clause+")")
	}

	// -----------------------------
	// Campaign count filters (min/max)
	// -----------------------------
	if filters.MinCampaigns != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("COALESCE(cl.campaign_count,0) >= $%d", argIndex))
		args = append(args, *filters.MinCampaigns)
		argIndex++
	}
	if filters.MaxCampaigns != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("COALESCE(cl.campaign_count,0) <= $%d", argIndex))
		args = append(args, *filters.MaxCampaigns)
		argIndex++
	}

	return &contactFilter{
		clauses:        whereClauses,
		args:           args,
		nextArg:        argIndex,
		singleCampaign: singleCampaignPlaceholder,
	}, nil
}

func (r *contactRepository) Search(
	ctx context.Context,
	orgID string,
	category *string,
	cursor *paging.SortCursor,
	filters models.SearchContacts,
	limit int32,
) (*models.ContactsResult, *errx.Error) {
	fq, ferr := r.buildContactFilter(ctx, orgID, filters)
	if ferr != nil {
		return nil, ferr
	}
	whereClauses := fq.clauses
	args := fq.args
	argIndex := fq.nextArg
	singleCampaignPlaceholder := fq.singleCampaign

	// -----------------------------
	// Sort logic
	// -----------------------------
	// campaign_count is a computed column, so the cursor compares against the
	// expression rather than the SELECT alias, which WHERE cannot see.
	sortName := "created_at"
	if _, ok := contactSorts[filters.SortBy]; ok {
		sortName = filters.SortBy
	}
	spec := contactSorts[sortName]
	direction := "DESC"
	nulls := "NULLS FIRST"
	if filters.Reverse {
		direction = "ASC"
		nulls = "NULLS LAST"
	}
	sortBy := spec.expr
	// The ordering the cursor is taken under. A token minted under a different
	// one points at a position that does not exist here.
	sortKey := sortName + ":" + strings.ToLower(direction)

	// -----------------------------
	// Cursor pagination
	// -----------------------------
	// Keyset, not offset: the token carries the boundary row's own sort value
	// alongside its id, so a row deleted or re-sorted between pages cannot move
	// the boundary.
	if cursor != nil {
		if cursor.Sort != sortKey {
			return nil, errx.New(errx.BadRequest, "invalid cursor")
		}
		if cursor.Value == nil && !spec.nullable {
			return nil, errx.New(errx.BadRequest, "invalid cursor")
		}
		if cursor.Value != nil && !spec.wellFormed(*cursor.Value) {
			return nil, errx.New(errx.BadRequest, "invalid cursor")
		}
		bound := spec.bound(fmt.Sprintf("$%d", argIndex))
		args = append(args, cursor.Value)
		argIndex++
		idArg := fmt.Sprintf("$%d", argIndex)
		args = append(args, cursor.ID)
		argIndex++

		// The tiebreak follows the sort direction, so one index serves both ways
		// round; the boundary row itself is included because it is this page's
		// first row.
		cmp, tie := "<", "<="
		if direction == "ASC" {
			cmp, tie = ">", ">="
		}
		after := fmt.Sprintf("(%[1]s %[2]s %[3]s OR (%[1]s = %[3]s AND c.id %[5]s %[4]s))", sortBy, cmp, bound, idArg, tie)
		if spec.nullable {
			switch {
			case cursor.Value == nil:
				// The boundary sits in the NULL block: the rest of that block by
				// id, plus every non-NULL row when NULLs come first.
				after = fmt.Sprintf("(%s IS NULL AND c.id %s %s)", sortBy, tie, idArg)
				if nulls == "NULLS FIRST" {
					after += fmt.Sprintf(" OR %s IS NOT NULL", sortBy)
				}
			case nulls == "NULLS LAST":
				// Past the non-NULL rows, the NULL block still follows.
				after += fmt.Sprintf(" OR %s IS NULL", sortBy)
			}
		}
		whereClauses = append(whereClauses, "("+after+")")
	}

	// -----------------------------
	// Build WHERE SQL
	// -----------------------------
	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
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
				-- Human opens only; automated fetches (Apple MPP prefetch, UA-less
				-- clients) are counted apart so they never read as engagement.
				'opened',  COUNT(*) FILTER (WHERE p.opened_at IS NOT NULL AND NOT p.opened_machine),
				'machine_opened', COUNT(*) FILTER (WHERE p.opened_at IS NOT NULL AND p.opened_machine),
				'clicked', COUNT(*) FILTER (WHERE p.clicked_at IS NOT NULL),
				'replied', COUNT(*) FILTER (WHERE p.replied_at IS NOT NULL),
				'bounced', COUNT(*) FILTER (WHERE p.bounced_at IS NOT NULL),
				-- Steps the mailbox could not send after every retry.
				'failed',  COUNT(*) FILTER (WHERE p.sent_at IS NULL AND p.failed_at IS NOT NULL AND p.send_attempts >= %[2]d),
				'failure_reason', (
					SELECT fp.failure_reason FROM campaign_contact_progress fp
					WHERE fp.campaign_id = %[1]s AND fp.contact_id = c.id AND fp.failed_at IS NOT NULL
					ORDER BY fp.failed_at DESC LIMIT 1
				),
				'last_at', MAX(GREATEST(p.sent_at, p.opened_at, p.clicked_at, p.replied_at, p.bounced_at, p.failed_at)),
				-- Routing will never send to this lead: its address failed
				-- verification. Surfaced so it reads "undeliverable" instead of
				-- sitting at "pending" forever with no explanation.
				'undeliverable', %[3]s,
				-- Total email steps in the sequence, to tell "still sending" (active)
				-- apart from "every step sent" (completed/done).
				'total_steps', (SELECT COUNT(*) FROM sequences st WHERE st.campaign_id = %[1]s AND st.kind = 'email'),
				-- The mailbox this lead's whole sequence sends from, fixed when
				-- its first email went out. Null until then.
				'sender', (
					SELECT ea.email FROM campaign_leads cls
					JOIN email_accounts ea ON ea.id = cls.email_account_id
					WHERE cls.campaign_id = %[1]s AND cls.contact_id = c.id
				),
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
							WHEN 'add_to_segment' THEN 'Add to segment'
							WHEN 'remove_from_segment' THEN 'Remove from segment'
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
		)`, singleCampaignPlaceholder, config.CampaignSendMaxAttempts, undeliverableClause(singleCampaignPlaceholder))
	}

	// campaign_count is only ever read by the min/max filters and the
	// campaign_count sort; the response carries the campaign list itself. So the
	// count is a lateral computed for the rows that survive, and it is left out
	// entirely when nothing asks for it. Aggregating the whole campaign_leads
	// table on every search was the list's dominant cost.
	campaignCountJoin := ""
	if filters.MinCampaigns != nil || filters.MaxCampaigns != nil || sortName == "campaign_count" {
		campaignCountJoin = campaignCountLateral
	}

	// Main query.
	//
	// Both the `campaigns` and `categories` agg subqueries scope on the
	// organization so they can't leak rows from another workspace that happens
	// to share a contact id (theoretically impossible thanks to the outer
	// WHERE, but cheap defence-in-depth). They reuse the same $%d placeholder
	// so the org id is appended once. The categories one compared that id
	// against categories.user_id until #436, which matched nothing, so every
	// row in the contact list came back with no categories at all.
	query := fmt.Sprintf(`
		SELECT
			c.id, c.first_name, c.last_name, c.email, c.company, c.phone,
			c.custom_fields, c.subscribed, c.updated_at, c.created_at,
			c.verification_status, c.verification_reason, c.is_catch_all, c.verification_checked_at,
			c.verification_source, c.verification_provider, c.verification_sub_status, c.verification_confidence,
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
					AND cat.organization_id = $%d
				), '[]'::json
			) AS categories,
			%s AS lead_progress,
			%s AS sort_value
		FROM contacts c
		%s
		%s
		ORDER BY %s %s %s, c.id %s
		LIMIT $%d
	`, argIndex, argIndex, leadProgressSelect, spec.render(), campaignCountJoin, whereSQL, sortBy, direction, nulls, direction, argIndex+1)

	args = append(args, orgID, limit+1)

	// Skip total count if cursor exists
	var totalCount *int64
	if cursor == nil {
		countJoin := ""
		if filters.MinCampaigns != nil || filters.MaxCampaigns != nil {
			countJoin = campaignCountLateral
		}
		countQuery := fmt.Sprintf(`
			SELECT COUNT(*)
			FROM contacts c
			%s
			%s
		`, countJoin, whereSQL)
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
	// The sort value of each row, kept alongside so the next cursor carries the
	// boundary instead of re-reading it from a row that may be gone by then.
	sortValues := make([]*string, 0, limit+1)
	for rows.Next() {
		var c models.Contact
		var campaignsJSON []byte
		var categoriesJSON []byte
		var leadProgressJSON []byte
		var sortValue *string

		if err := rows.Scan(
			&c.ID, &c.FirstName, &c.LastName, &c.Email,
			&c.Company, &c.Phone, &c.CustomFields, &c.Subscribed,
			&c.UpdatedAt, &c.CreatedAt,
			&c.VerificationStatus, &c.VerificationReason, &c.IsCatchAll, &c.VerificationCheckedAt,
			&c.VerificationSource, &c.VerificationProvider, &c.VerificationSubStatus, &c.VerificationConfidence,
			&campaignsJSON, &categoriesJSON, &leadProgressJSON,
			&sortValue,
		); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}

		// Per-campaign lead progress (single-campaign Leads view only). Derive
		// the single display status from the counts + subscription flag.
		if len(leadProgressJSON) > 0 {
			var lp struct {
				Sent       int        `json:"sent"`
				Opened     int        `json:"opened"`
				MachineOpn int        `json:"machine_opened"`
				Clicked    int        `json:"clicked"`
				Replied    int        `json:"replied"`
				Bounced    int        `json:"bounced"`
				Failed     int        `json:"failed"`
				FailReason *string    `json:"failure_reason"`
				TotalSteps int        `json:"total_steps"`
				LastAt     *time.Time `json:"last_at"`
				Step       *string    `json:"step"`
				Sender     *string    `json:"sender"`

				Undeliverable bool `json:"undeliverable"`
			}
			if err := json.Unmarshal(leadProgressJSON, &lp); err != nil {
				errs.CaptureException(err)
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
			case lp.Failed > 0:
				status = models.LeadStatusFailed
			case lp.Sent > 0 && lp.TotalSteps > 0 && lp.Sent >= lp.TotalSteps:
				// Every email step has been sent and the contact hasn't replied
				// or bounced: the sequence is exhausted, so the lead is done.
				status = models.LeadStatusCompleted
			case lp.Sent > 0:
				status = models.LeadStatusActive
			case lp.Undeliverable:
				// Nothing sent and nothing ever will be: verification refused
				// the address, so routing skips it.
				status = models.LeadStatusUndeliverable
			}
			currentStep := ""
			if lp.Step != nil {
				currentStep = *lp.Step
			}
			failureReason := ""
			if status == models.LeadStatusFailed && lp.FailReason != nil {
				failureReason = *lp.FailReason
			}
			sender := ""
			if lp.Sender != nil {
				sender = *lp.Sender
			}
			c.CampaignLead = &models.ContactCampaignProgress{
				Status:         status,
				Sender:         sender,
				Sent:           lp.Sent,
				Opened:         lp.Opened,
				MachineOpened:  lp.MachineOpn,
				Clicked:        lp.Clicked,
				Replied:        lp.Replied,
				Bounced:        lp.Bounced,
				LastActivityAt: lp.LastAt,
				CurrentStep:    currentStep,
				FailureReason:  failureReason,
			}
		}

		if len(campaignsJSON) > 0 {
			var campaigns []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if err := json.Unmarshal(campaignsJSON, &campaigns); err != nil {
				errs.CaptureException(err)
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
				errs.CaptureException(err)
				return nil, errx.InternalError()
			}
		}
		if c.Categories == nil {
			c.Categories = []models.MiniCategory{}
		}

		contacts = append(contacts, c)
		sortValues = append(sortValues, sortValue)
	}

	// Next cursor. The (limit+1)-th row is the first row of the NEXT page, so
	// its position is the boundary and the id comparison is inclusive.
	var nextCursor *string
	var hasMore bool
	if len(contacts) > int(limit) {
		hasMore = true
		nextCursor = paging.EncodeSort(sortKey, sortValues[limit], contacts[limit].ID)
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
// SearchIDs resolves a search request to the matching contact ids. It shares
// buildContactFilter with Search, so the set is exactly the one the list
// showed; ordering follows the same sort so a capped result is the first max
// rows the user was looking at rather than an arbitrary slice.
func (r *contactRepository) SearchIDs(ctx context.Context, orgID string, filters models.SearchContacts, max int) ([]uuid.UUID, *errx.Error) {
	if _, err := uuid.Parse(orgID); err != nil {
		return nil, errx.ErrUuid
	}
	if max <= 0 {
		max = models.MaxContactBulkSelection
	}

	fq, ferr := r.buildContactFilter(ctx, orgID, filters)
	if ferr != nil {
		return nil, ferr
	}
	args := fq.args

	sortName := "created_at"
	if _, ok := contactSorts[filters.SortBy]; ok {
		sortName = filters.SortBy
	}
	spec := contactSorts[sortName]
	direction, nulls := "DESC", "NULLS FIRST"
	if filters.Reverse {
		direction, nulls = "ASC", "NULLS LAST"
	}
	// Same rule as Search: the count lateral costs a scan, so it is joined only
	// when a filter or the sort actually reads it.
	campaignCountJoin := ""
	if filters.MinCampaigns != nil || filters.MaxCampaigns != nil || sortName == "campaign_count" {
		campaignCountJoin = campaignCountLateral
	}

	whereSQL := ""
	if len(fq.clauses) > 0 {
		whereSQL = "WHERE " + strings.Join(fq.clauses, " AND ")
	}

	// max+1 so the caller can report "more than max match" instead of silently
	// acting on a truncated set.
	query := fmt.Sprintf(`
		SELECT c.id
		FROM contacts c
		%s
		%s
		ORDER BY %s %s %s, c.id %s
		LIMIT $%d
	`, campaignCountJoin, whereSQL, spec.expr, direction, nulls, direction, fq.nextArg)
	args = append(args, max+1)

	rows, err := r.DB.Query(ctx, query, args...)
	if err != nil {
		db.CaptureError(err, query, args, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	ids := make([]uuid.UUID, 0, 256)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, query, args, "rows")
		return nil, errx.InternalError()
	}
	return ids, nil
}

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

// DistinctCustomFieldKeys returns the org's distinct contact custom-field keys,
// frequency-ranked (most common first) then alphabetical, capped at 200. Used
// by the dashboard variable picker to suggest fields contacts actually have.
func (r *contactRepository) DistinctCustomFieldKeys(ctx context.Context, orgID uuid.UUID) ([]string, error) {
	const query = `
		SELECT key
		FROM contacts, jsonb_object_keys(custom_fields) AS key
		WHERE organization_id = $1 AND custom_fields IS NOT NULL AND custom_fields <> '{}'::jsonb
		GROUP BY key
		ORDER BY count(*) DESC, key ASC
		LIMIT 200
	`
	rows, err := r.DB.Query(ctx, query, orgID)
	if err != nil {
		db.CaptureError(err, query, []any{orgID}, "query")
		return nil, err
	}
	defer rows.Close()

	keys := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			db.CaptureError(err, query, nil, "scan")
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, query, nil, "rows")
		return nil, err
	}
	return keys, nil
}

// leadStatusClause builds the WHERE predicate for a derived lead status inside
// ONE campaign, matching pg_contact Search's read-time derivation exactly:
// unsubscribed > bounced > replied > failed > completed > processing(active) >
// queued(pending). `cp` is the already-bound placeholder for that campaign id
// (e.g. "$5"). Returns "" for an unknown status (the caller then applies no lead
// filter).
func leadStatusClause(status, cp string) string {
	// EXISTS a progress row for (this campaign, this contact) with `col` set.
	has := func(col string) string {
		return fmt.Sprintf(
			"EXISTS (SELECT 1 FROM campaign_contact_progress p WHERE p.campaign_id = %s AND p.contact_id = c.id AND p.%s IS NOT NULL)",
			cp, col,
		)
	}
	sent, replied, bounced := has("sent_at"), has("replied_at"), has("bounced_at")
	// failed: a step the mailbox could not send after every retry (sent_at was
	// walked back and the attempt cap is spent).
	failed := fmt.Sprintf(
		"EXISTS (SELECT 1 FROM campaign_contact_progress p WHERE p.campaign_id = %s AND p.contact_id = c.id "+
			"AND p.sent_at IS NULL AND p.failed_at IS NOT NULL AND p.send_attempts >= %d)",
		cp, config.CampaignSendMaxAttempts,
	)
	// allSent: every email step of the campaign has been sent to this contact.
	allSent := fmt.Sprintf(
		"((SELECT COUNT(*) FROM sequences st WHERE st.campaign_id = %[1]s AND st.kind = 'email') > 0 "+
			"AND (SELECT COUNT(*) FROM campaign_contact_progress p WHERE p.campaign_id = %[1]s AND p.contact_id = c.id AND p.sent_at IS NOT NULL) "+
			">= (SELECT COUNT(*) FROM sequences st WHERE st.campaign_id = %[1]s AND st.kind = 'email'))",
		cp,
	)
	undeliverable := undeliverableClause(cp)
	switch status {
	case models.LeadStatusUnsubscribed:
		return "NOT c.subscribed"
	case models.LeadStatusBounced:
		return fmt.Sprintf("(c.subscribed AND %s)", bounced)
	case models.LeadStatusReplied:
		return fmt.Sprintf("(c.subscribed AND NOT %s AND %s)", bounced, replied)
	case models.LeadStatusFailed:
		return fmt.Sprintf("(c.subscribed AND NOT %s AND NOT %s AND %s)", bounced, replied, failed)
	case models.LeadStatusCompleted:
		return fmt.Sprintf("(c.subscribed AND NOT %s AND NOT %s AND NOT %s AND %s AND %s)", bounced, replied, failed, sent, allSent)
	case models.LeadStatusActive:
		return fmt.Sprintf("(c.subscribed AND NOT %s AND NOT %s AND NOT %s AND %s AND NOT %s)", bounced, replied, failed, sent, allSent)
	case models.LeadStatusUndeliverable:
		return fmt.Sprintf("(c.subscribed AND NOT %s AND NOT %s AND NOT %s AND NOT %s AND %s)", bounced, replied, failed, sent, undeliverable)
	case models.LeadStatusPending:
		return fmt.Sprintf("(c.subscribed AND NOT %s AND NOT %s AND NOT %s AND NOT %s AND NOT %s)", bounced, replied, failed, sent, undeliverable)
	default:
		return ""
	}
}

// CampaignLeadCounts returns per-status lead totals for one campaign (the
// campaign Leads view scope chips). A single aggregate over the campaign's
// leads joined to their contact and a rolled-up view of their progress, so the
// buckets follow the same unsubscribed > bounced > replied > failed >
// completed > processing > undeliverable > queued priority as the row-level
// derived status.
// Scoped to the org through the contacts join.
// leadEngagementClause builds the WHERE predicate for one engagement filter
// value inside ONE campaign (`cp` is that campaign's bound placeholder). An
// open counts only when it is human: opened_machine marks automated fetches,
// the same split the analytics summary reports as machine opens. The negative
// forms require at least one sent step, so a lead never emailed matches
// neither side. Returns "" for an unknown value.
func leadEngagementClause(engagement, cp string) string {
	has := func(cond string) string {
		return fmt.Sprintf(
			"EXISTS (SELECT 1 FROM campaign_contact_progress p WHERE p.campaign_id = %s AND p.contact_id = c.id AND %s)",
			cp, cond,
		)
	}
	sent := has("p.sent_at IS NOT NULL")
	opened := has("p.opened_at IS NOT NULL AND NOT p.opened_machine")
	clicked := has("p.clicked_at IS NOT NULL")
	replied := has("p.replied_at IS NOT NULL")
	bounced := has("p.bounced_at IS NOT NULL")
	switch engagement {
	case models.LeadEngagementOpened:
		return opened
	case models.LeadEngagementNotOpened:
		return fmt.Sprintf("(%s AND NOT %s)", sent, opened)
	case models.LeadEngagementClicked:
		return clicked
	case models.LeadEngagementNotClicked:
		return fmt.Sprintf("(%s AND NOT %s)", sent, clicked)
	case models.LeadEngagementReplied:
		return replied
	case models.LeadEngagementNotReplied:
		return fmt.Sprintf("(%s AND NOT %s)", sent, replied)
	case models.LeadEngagementBounced:
		return bounced
	default:
		return ""
	}
}

func (r *contactRepository) CampaignLeadCounts(ctx context.Context, orgID, campaignID string) (*models.CampaignLeadCounts, *errx.Error) {
	// A lead is "done" (completed) when every email step has been sent and it
	// hasn't replied or bounced; "processing" when some but not all steps sent.
	const done = "ts.total_steps > 0 AND COALESCE(pr.sent_steps, 0) >= ts.total_steps"
	const live = "c.subscribed AND NOT COALESCE(pr.has_bounced, false) AND NOT COALESCE(pr.has_replied, false) AND NOT COALESCE(pr.has_failed, false)"
	query := fmt.Sprintf(`
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE NOT c.subscribed) AS unsubscribed,
			COUNT(*) FILTER (WHERE c.subscribed AND COALESCE(pr.has_bounced, false)) AS bounced,
			COUNT(*) FILTER (WHERE c.subscribed AND NOT COALESCE(pr.has_bounced, false) AND COALESCE(pr.has_replied, false)) AS replied,
			COUNT(*) FILTER (WHERE c.subscribed AND NOT COALESCE(pr.has_bounced, false) AND NOT COALESCE(pr.has_replied, false) AND COALESCE(pr.has_failed, false)) AS failed,
			COUNT(*) FILTER (WHERE %[2]s AND COALESCE(pr.has_sent, false) AND (%[1]s)) AS completed,
			COUNT(*) FILTER (WHERE %[2]s AND COALESCE(pr.has_sent, false) AND NOT (%[1]s)) AS processing,
			COUNT(*) FILTER (WHERE %[2]s AND NOT COALESCE(pr.has_sent, false) AND %[3]s) AS undeliverable,
			COUNT(*) FILTER (WHERE %[2]s AND NOT COALESCE(pr.has_sent, false) AND NOT %[3]s) AS queued,
			COUNT(*) FILTER (WHERE COALESCE(pr.has_sent, false)) AS contacted,
			COUNT(*) FILTER (WHERE COALESCE(pr.has_opened, false)) AS opened,
			COUNT(*) FILTER (WHERE COALESCE(pr.has_clicked, false)) AS clicked,
			COUNT(*) FILTER (WHERE COALESCE(pr.has_replied, false)) AS replied_any
		FROM campaign_leads cl
		JOIN contacts c ON c.id = cl.contact_id AND c.organization_id = $2
		CROSS JOIN (SELECT COUNT(*) AS total_steps FROM sequences st WHERE st.campaign_id = $1 AND st.kind = 'email') ts
		LEFT JOIN LATERAL (
			SELECT
				bool_or(p.sent_at IS NOT NULL)    AS has_sent,
				bool_or(p.replied_at IS NOT NULL) AS has_replied,
				bool_or(p.bounced_at IS NOT NULL) AS has_bounced,
				bool_or(p.opened_at IS NOT NULL AND NOT p.opened_machine) AS has_opened,
				bool_or(p.clicked_at IS NOT NULL) AS has_clicked,
				bool_or(p.sent_at IS NULL AND p.failed_at IS NOT NULL AND p.send_attempts >= $3) AS has_failed,
				COUNT(*) FILTER (WHERE p.sent_at IS NOT NULL) AS sent_steps
			FROM campaign_contact_progress p
			WHERE p.campaign_id = cl.campaign_id AND p.contact_id = cl.contact_id
		) pr ON true
		WHERE cl.campaign_id = $1
	`, done, live, undeliverableClause("$1"))
	out := &models.CampaignLeadCounts{}
	if err := r.DB.QueryRow(ctx, query, campaignID, orgID, config.CampaignSendMaxAttempts).Scan(
		&out.Total, &out.Unsubscribed, &out.Bounced, &out.Replied, &out.Failed, &out.Completed, &out.Processing, &out.Undeliverable, &out.Queued,
		&out.Contacted, &out.Opened, &out.Clicked, &out.RepliedAny,
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
					WHERE cl2.contact_id = c.id AND cam.organization_id = $2
				),
				'[]'::json
			) AS campaigns
		FROM contacts c
		WHERE c.id = $1 AND c.organization_id = $2
		`

	params := []any{
		contactID,
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

	// Membership changes, written to the activity timeline before commit.
	var campaignsAdded, campaignsRemoved, categoriesAdded, categoriesRemoved []contactLink

	// Unmarshal current campaigns
	if len(campaignsJSON) > 0 {
		var campaigns []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(campaignsJSON, &campaigns); err != nil {
			errs.CaptureException(err)
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
		incoming, xerr := normalizeCustomFields(*data.CustomFields)
		if xerr != nil {
			return nil, xerr
		}
		// Merge existing custom_fields with updates
		mergedFields := make(map[string]string)
		for k, v := range c.CustomFields {
			mergedFields[k] = v
		}
		for k, v := range incoming {
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

	// Campaigns are organization assets: scoping membership by the caller made a
	// teammate's add/remove a silent no-op on campaigns they did not create
	// (issue #187).
	if data.Campaigns != nil {
		want, perr := parseUUIDList(data.Campaigns)
		if perr != nil {
			return nil, perr
		}
		wantIDs := make([]string, len(want))
		for i, id := range want {
			wantIDs[i] = id.String()
		}

		currentCampaignIDs := make([]string, len(updatedContact.Campaigns))
		for i, c := range updatedContact.Campaigns {
			currentCampaignIDs[i] = c.ID
		}

		toInsert := utils.Difference(wantIDs, currentCampaignIDs)
		toDelete := utils.Difference(currentCampaignIDs, wantIDs)

		if len(toDelete) > 0 {
			// Record the removal so linked-segment enrolment does not re-add
			// the pair; a later manual add clears it again.
			query = `
				WITH gone AS (
					DELETE FROM campaign_leads cl
					USING campaigns cam
					WHERE cl.contact_id = $1
					  AND cl.campaign_id = cam.id
					  AND cam.id = ANY($2::uuid[])
					  AND cam.organization_id = $3
					RETURNING cl.contact_id, cl.campaign_id
				), mark AS (
					INSERT INTO campaign_lead_removals (campaign_id, contact_id)
					SELECT campaign_id, contact_id FROM gone
					ON CONFLICT (campaign_id, contact_id) DO UPDATE SET created_at = now()
				)
				SELECT campaign_id FROM gone
			`
			params := []any{contactID, toDelete, orgID}
			rows, err := tx.Query(ctx, query, params...)
			if err != nil {
				db.CaptureError(err, query, params, "exec")
				return nil, errx.InternalError()
			}
			removed, err := collectLinks(rows, c.ID)
			if err != nil {
				db.CaptureError(err, query, params, "returning")
				return nil, errx.InternalError()
			}
			campaignsRemoved = append(campaignsRemoved, removed...)
		}

		if len(toInsert) > 0 {
			query = `
				WITH cleared AS (
					DELETE FROM campaign_lead_removals r
					USING campaigns cam
					WHERE r.contact_id = $1
					  AND r.campaign_id = cam.id
					  AND cam.id = ANY($2::uuid[])
					  AND cam.organization_id = $3
				)
				INSERT INTO campaign_leads (contact_id, campaign_id)
				SELECT $1, cam.id
				FROM campaigns cam
				WHERE cam.id = ANY($2::uuid[]) AND cam.organization_id = $3
				ON CONFLICT (campaign_id, contact_id) DO NOTHING
				RETURNING campaign_id
			`
			params := []any{contactID, toInsert, orgID}
			rows, err := tx.Query(ctx, query, params...)
			if err != nil {
				db.CaptureError(err, query, params, "exec")
				return nil, errx.InternalError()
			}
			added, err := collectLinks(rows, c.ID)
			if err != nil {
				db.CaptureError(err, query, params, "returning")
				return nil, errx.InternalError()
			}
			campaignsAdded = append(campaignsAdded, added...)
		}
	}

	// Always re-read campaigns so the response carries current membership even
	// when the request touched other fields (a nil slice marshals to `null`,
	// which the dashboard then caches and reads `.campaigns.map` off).
	var newCampaignsJSON []byte
	query = `
		SELECT COALESCE(
			(
				SELECT json_agg(json_build_object('id', cam.id, 'name', cam.name))
				FROM campaign_leads cl
				JOIN campaigns cam ON cl.campaign_id = cam.id
				WHERE cl.contact_id = $1 AND cam.organization_id = $2
			),
			'[]'::json
		)
	`

	params = []any{
		contactID,
		orgID,
	}

	if err = tx.QueryRow(ctx, query, params...).Scan(&newCampaignsJSON); err != nil {
		db.CaptureError(err, query, params, "queryrow")
		return nil, errx.InternalError()
	}
	updatedContact.Campaigns = make([]models.MiniCampaign, 0)
	if len(newCampaignsJSON) > 0 {
		var campaigns []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(newCampaignsJSON, &campaigns); err != nil {
			errs.CaptureException(err)
			return nil, errx.InternalError()
		}
		for _, c := range campaigns {
			updatedContact.Campaigns = append(updatedContact.Campaigns, models.MiniCampaign{
				ID:   c.ID,
				Name: c.Name,
			})
		}
	}

	// Categories. Two modes supported on the request:
	//   - `categories: [..]` → set absolute (full replace).
	//   - `add_categories / remove_categories` → diff style.
	// When the absolute form is non-nil it wins; the diff is ignored.
	categoriesChanged := false
	if data.Categories != nil {
		ids, perr := parseUUIDList(data.Categories)
		if perr != nil {
			return nil, perr
		}
		// Drop everything not in the (workspace-owned) wanted set, then insert
		// the rest; RETURNING on both sides is what feeds the timeline.
		drows, err := tx.Query(ctx, `
			DELETE FROM contact_categories
			WHERE contact_id = $1
			  AND category_id NOT IN (SELECT id FROM categories WHERE id = ANY($2) AND organization_id = $3)
			RETURNING category_id
		`, contactID, ids, orgID)
		if err != nil {
			db.CaptureError(err, "", nil, "categories wipe")
			return nil, errx.InternalError()
		}
		removed, err := collectLinks(drows, c.ID)
		if err != nil {
			db.CaptureError(err, "", nil, "categories wipe returning")
			return nil, errx.InternalError()
		}
		categoriesRemoved = append(categoriesRemoved, removed...)
		if len(ids) > 0 {
			irows, err := tx.Query(ctx, `
				INSERT INTO contact_categories (contact_id, category_id)
				SELECT $1, cat.id
				FROM   categories cat
				WHERE  cat.id = ANY($2) AND cat.organization_id = $3
				ON CONFLICT (contact_id, category_id) DO NOTHING
				RETURNING category_id
			`, contactID, ids, orgID)
			if err != nil {
				db.CaptureError(err, "", nil, "categories insert")
				return nil, errx.InternalError()
			}
			added, err := collectLinks(irows, c.ID)
			if err != nil {
				db.CaptureError(err, "", nil, "categories insert returning")
				return nil, errx.InternalError()
			}
			categoriesAdded = append(categoriesAdded, added...)
		}
		categoriesChanged = true
	} else {
		if len(data.AddCategories) > 0 {
			ids, perr := parseUUIDList(data.AddCategories)
			if perr != nil {
				return nil, perr
			}
			irows, err := tx.Query(ctx, `
				INSERT INTO contact_categories (contact_id, category_id)
				SELECT $1, cat.id
				FROM   categories cat
				WHERE  cat.id = ANY($2) AND cat.organization_id = $3
				ON CONFLICT (contact_id, category_id) DO NOTHING
				RETURNING category_id
			`, contactID, ids, orgID)
			if err != nil {
				db.CaptureError(err, "", nil, "categories add")
				return nil, errx.InternalError()
			}
			added, err := collectLinks(irows, c.ID)
			if err != nil {
				db.CaptureError(err, "", nil, "categories add returning")
				return nil, errx.InternalError()
			}
			categoriesAdded = append(categoriesAdded, added...)
			categoriesChanged = true
		}
		if len(data.RemoveCategories) > 0 {
			ids, perr := parseUUIDList(data.RemoveCategories)
			if perr != nil {
				return nil, perr
			}
			drows, err := tx.Query(ctx, `
				DELETE FROM contact_categories
				WHERE contact_id = $1 AND category_id = ANY($2)
				RETURNING category_id
			`, contactID, ids)
			if err != nil {
				db.CaptureError(err, "", nil, "categories remove")
				return nil, errx.InternalError()
			}
			removed, err := collectLinks(drows, c.ID)
			if err != nil {
				db.CaptureError(err, "", nil, "categories remove returning")
				return nil, errx.InternalError()
			}
			categoriesRemoved = append(categoriesRemoved, removed...)
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
					WHERE cc.contact_id = $1 AND cat.organization_id = $2
				),
				'[]'::json
			)
		`, contactID, orgID).Scan(&catJSON); err != nil {
			db.CaptureError(err, "", nil, "categories reload")
			return nil, errx.InternalError()
		}
		updatedContact.Categories = make([]models.MiniCategory, 0)
		if len(catJSON) > 0 {
			if err := json.Unmarshal(catJSON, &updatedContact.Categories); err != nil {
				errs.CaptureException(err)
				return nil, errx.InternalError()
			}
		}
	}

	actor := actorID(userID)
	for _, w := range []struct {
		typ   models.ActivityType
		links []contactLink
		log   func(context.Context, activityWriter, uuid.UUID, *uuid.UUID, models.ActivityType, []contactLink) error
	}{
		{models.ActivityCampaignAdded, campaignsAdded, logCampaignLinks},
		{models.ActivityCampaignRemoved, campaignsRemoved, logCampaignLinks},
		{models.ActivityCategoryAdded, categoriesAdded, logCategoryLinks},
		{models.ActivityCategoryRemoved, categoriesRemoved, logCategoryLinks},
	} {
		if err := w.log(ctx, tx, orgID, actor, w.typ, w.links); err != nil {
			db.CaptureError(err, "", nil, string(w.typ)+" activity")
			return nil, errx.InternalError()
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

	// Membership changes run outside the batch: their RETURNING rows are what
	// the activity timeline records. Campaigns are organization assets:
	// scoping them by the caller made a teammate's "add to campaign" a silent
	// no-op on campaigns they did not create.
	actor := actorID(userID)
	link := func(sql string, typ models.ActivityType, log func(context.Context, activityWriter, uuid.UUID, *uuid.UUID, models.ActivityType, []contactLink) error, args ...any) *errx.Error {
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			db.CaptureError(err, sql, args, "bulk link")
			return errx.InternalError()
		}
		links, err := collectLinkPairs(rows)
		if err != nil {
			db.CaptureError(err, sql, args, "bulk link returning")
			return errx.InternalError()
		}
		if err := log(ctx, tx, orgID, actor, typ, links); err != nil {
			db.CaptureError(err, "", nil, string(typ)+" activity")
			return errx.InternalError()
		}
		return nil
	}
	if len(data.RemoveCampaigns) > 0 {
		// Only pairs that actually held a lead are recorded as removals, so
		// the automatic segment enrolment never re-adds a hand-removed lead
		// while contacts merely listed in the request stay eligible.
		if xerr := link(`WITH gone AS (
		         DELETE FROM campaign_leads cl
		         USING contacts c, campaigns cam
		         WHERE cl.contact_id = c.id
		           AND cl.campaign_id = cam.id
		           AND c.organization_id = $1
		           AND cam.organization_id = $1
		           AND cl.contact_id = ANY($2)
		           AND cl.campaign_id = ANY($3)
		         RETURNING cl.contact_id, cl.campaign_id
		         ), mark AS (
		         INSERT INTO campaign_lead_removals (campaign_id, contact_id)
		         SELECT campaign_id, contact_id FROM gone
		         ON CONFLICT (campaign_id, contact_id) DO UPDATE SET created_at = now()
		         )
		         SELECT contact_id, campaign_id FROM gone`,
			models.ActivityCampaignRemoved, logCampaignLinks, orgID, data.Contacts, data.RemoveCampaigns); xerr != nil {
			return nil, xerr
		}
	}

	if len(data.AddCampaigns) > 0 {
		// A manual add clears the removal record: the user changed their mind,
		// so linked segments may manage this pair again.
		if xerr := link(`WITH cleared AS (
		         DELETE FROM campaign_lead_removals r
		         USING contacts c, campaigns cam
		         WHERE r.contact_id = c.id
		           AND r.campaign_id = cam.id
		           AND c.organization_id = $1
		           AND cam.organization_id = $1
		           AND r.contact_id = ANY($2)
		           AND r.campaign_id = ANY($3::uuid[])
		         )
		         INSERT INTO campaign_leads (contact_id, campaign_id)
		         SELECT c.id, cam.id
		         FROM contacts c
		         CROSS JOIN campaigns cam
		         WHERE c.organization_id = $1
		           AND c.id = ANY($2)
		           AND cam.id = ANY($3::uuid[])
		           AND cam.organization_id = $1
		         ON CONFLICT DO NOTHING
		         RETURNING contact_id, campaign_id`,
			models.ActivityCampaignAdded, logCampaignLinks, orgID, data.Contacts, data.AddCampaigns); xerr != nil {
			return nil, xerr
		}
	}

	if len(data.RemoveCategories) > 0 {
		if xerr := link(`DELETE FROM contact_categories cc
		         USING contacts c
		         WHERE cc.contact_id = c.id
		           AND c.organization_id = $1
		           AND cc.contact_id = ANY($2)
		           AND cc.category_id = ANY($3::uuid[])
		         RETURNING cc.contact_id, cc.category_id`,
			models.ActivityCategoryRemoved, logCategoryLinks, orgID, data.Contacts, data.RemoveCategories); xerr != nil {
			return nil, xerr
		}
	}

	if len(data.AddCategories) > 0 {
		if xerr := link(`INSERT INTO contact_categories (contact_id, category_id)
		         SELECT c.id, cat.id
		         FROM contacts c
		         CROSS JOIN categories cat
		         WHERE c.organization_id = $1
		           AND c.id = ANY($2)
		           AND cat.id = ANY($3::uuid[])
		           AND cat.organization_id = $1
		         ON CONFLICT DO NOTHING
		         RETURNING contact_id, category_id`,
			models.ActivityCategoryAdded, logCategoryLinks, orgID, data.Contacts, data.AddCategories); xerr != nil {
			return nil, xerr
		}
	}

	for _, p := range data.Fields {
		// Keys arrive from the UI's bulk field editor; normalize + reject the
		// same way the single-contact writes do so a bulk edit cannot mint a
		// field name nothing else in the product can address.
		p.Key = utils.NormalizeJSONKey(p.Key)
		if !utils.IsValidJSONKey(p.Key) {
			return nil, errx.New(errx.BadRequest,
				"invalid custom field name "+strconv.Quote(p.Key)+": "+utils.JSONKeyRules)
		}
		if p.Type == models.BulkRenameField {
			p.Value = utils.NormalizeJSONKey(p.Value)
			if !utils.IsValidJSONKey(p.Value) {
				return nil, errx.New(errx.BadRequest,
					"invalid custom field name "+strconv.Quote(p.Value)+": "+utils.JSONKeyRules)
			}
		}
		switch p.Type {
		case models.BulkAddField:
			// jsonb_build_object is variadic "any", so it gives Postgres nothing
			// to infer $1/$2 from and the statement is refused before it runs.
			// to_jsonb($2::text) also matches BulkEditField, which stores the
			// value as a JSON string rather than a bare literal.
			b.Queue(`UPDATE contacts
			         SET custom_fields = custom_fields || jsonb_build_object($1::text, to_jsonb($2::text)),
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
			         SET custom_fields = (custom_fields - $1::text) || jsonb_build_object($2::text, custom_fields -> ($1::text)),
			             updated_at = NOW()
			         WHERE organization_id = $3 AND id = ANY($4)
			           AND custom_fields ? ($1::text)`,
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
					WHERE cl.contact_id =c.id AND cam.organization_id = $2
				),
				'[]'::json
			) AS campaigns,
			COALESCE(
				(
					SELECT json_agg(json_build_object('id', cat.id, 'title', cat.title, 'color', cat.color) ORDER BY cat.position ASC, cat.title ASC)
					FROM contact_categories cc
					JOIN categories cat ON cc.category_id = cat.id
					WHERE cc.contact_id = c.id AND cat.organization_id = $2
				),
				'[]'::json
			) AS categories
		FROM contacts c
		WHERE c.organization_id = $2 AND c.id = ANY($1)
	`

	params := []any{
		data.Contacts,
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
				errs.CaptureException(err)
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
				errs.CaptureException(err)
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
// MaxImportCategoryNames bounds how many distinct category titles one import
// may introduce. A column of free text mapped to Categories by mistake would
// otherwise mint a category per row.
const MaxImportCategoryNames = 100

func (r *contactRepository) ResolveCategoryNames(ctx context.Context, orgID, userID uuid.UUID, names []string) (map[string]uuid.UUID, *errx.Error) {
	out := make(map[string]uuid.UUID, len(names))
	wanted := make([]string, 0, len(names))
	seen := make(map[string]string, len(names)) // lowered -> original casing
	for _, raw := range names {
		title := strings.TrimSpace(raw)
		if title == "" {
			continue
		}
		if len(title) > 50 {
			return nil, errx.New(errx.BadRequest,
				"category name "+strconv.Quote(title)+" is longer than 50 characters")
		}
		lower := strings.ToLower(title)
		if _, dup := seen[lower]; dup {
			continue
		}
		seen[lower] = title
		wanted = append(wanted, lower)
	}
	if len(wanted) == 0 {
		return out, nil
	}
	if len(wanted) > MaxImportCategoryNames {
		return nil, errx.New(errx.BadRequest, fmt.Sprintf(
			"the categories column has %d distinct values; at most %d can be created in one import",
			len(wanted), MaxImportCategoryNames))
	}

	// Ordered, and the first match wins: the migration that made this registry
	// workspace-wide deliberately did not merge two members' identically named
	// categories, so a title can resolve to more than one row. Without an order
	// an import would file the same name under a different category run to run.
	rows, err := r.DB.Query(ctx, `
		SELECT id, LOWER(title) FROM categories
		WHERE organization_id = $1 AND LOWER(title) = ANY($2::text[])
		ORDER BY "position" ASC, created_at ASC, id ASC
	`, orgID, wanted)
	if err != nil {
		db.CaptureError(err, "", nil, "ResolveCategoryNames query")
		return nil, errx.InternalError()
	}
	for rows.Next() {
		var id uuid.UUID
		var lower string
		if err := rows.Scan(&id, &lower); err != nil {
			rows.Close()
			db.CaptureError(err, "", nil, "ResolveCategoryNames scan")
			return nil, errx.InternalError()
		}
		if _, taken := out[lower]; !taken {
			out[lower] = id
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		db.CaptureError(err, "", nil, "ResolveCategoryNames rows")
		return nil, errx.InternalError()
	}

	missing := make([]string, 0, len(wanted))
	for _, lower := range wanted {
		if _, ok := out[lower]; !ok {
			missing = append(missing, lower)
		}
	}
	if len(missing) == 0 {
		return out, nil
	}

	// Positions continue after whatever the workspace already has, so the new
	// categories land at the end of the list instead of colliding.
	var nextPos int32
	if err := r.DB.QueryRow(ctx,
		`SELECT COALESCE(MAX(position), -1) + 1 FROM categories WHERE organization_id = $1`,
		orgID).Scan(&nextPos); err != nil {
		db.CaptureError(err, "", nil, "ResolveCategoryNames position")
		return nil, errx.InternalError()
	}
	for _, lower := range missing {
		id := uuid.New()
		if _, err := r.DB.Exec(ctx, `
			INSERT INTO categories (id, organization_id, user_id, title, color, position)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, id, orgID, userID, seen[lower], defaultGroupColor(nextPos), nextPos); err != nil {
			db.CaptureError(err, "", nil, "ResolveCategoryNames insert")
			return nil, errx.InternalError()
		}
		out[lower] = id
		nextPos++
	}
	return out, nil
}

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
//   - both nil/empty  → "every contact in the organization".
func (r *contactRepository) ExportAll(ctx context.Context, orgID string, filters *models.SearchContacts, contactIDs []string, max int) ([]models.Contact, *errx.Error) {
	if _, perr := uuid.Parse(orgID); perr != nil {
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
	var cursor *paging.SortCursor
	pageSize := int32(500)
	for {
		page, xerr := r.Search(ctx, orgID, nil, cursor, search, pageSize)
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
		// NextCursor is an opaque token; decode it back to the keyset boundary
		// the next Search call resumes from.
		next, derr := paging.DecodeSortCursor(*page.Pagination.NextCursor)
		if derr != nil || next == nil {
			break
		}
		cursor = next
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
	// back to the legacy user scope. Campaigns and categories are org assets,
	// so their badge subselects follow the same scope (issues #187, #436).
	// $1 is the contact and $2 is whichever of the two scopes applies, so the
	// statement never carries a parameter nothing references (Postgres cannot
	// infer a type for one of those and rejects the whole query).
	rowScope, campScope, catScope := "c.user_id = $2", "cam.user_id = $2", "cat.user_id = $2"
	scopeArg := any(userID)
	if orgID != nil {
		rowScope, campScope, catScope = "c.organization_id = $2", "cam.organization_id = $2", "cat.organization_id = $2"
		scopeArg = *orgID
	}
	mainQuery := fmt.Sprintf(`
		SELECT
			c.id, c.first_name, c.last_name, c.email, c.company, c.phone,
			c.custom_fields, c.subscribed, c.updated_at, c.created_at,
			c.source, c.source_detail, c.first_seen_at,
			COALESCE(
				(
					SELECT json_agg(json_build_object('id', cam.id, 'name', cam.name))
					FROM   campaign_leads cl
					JOIN   campaigns cam ON cam.id = cl.campaign_id
					WHERE  cl.contact_id = c.id AND %s
				), '[]'::json
			) AS campaigns,
			COALESCE(
				(
					SELECT json_agg(json_build_object('id', cat.id, 'title', cat.title, 'color', cat.color) ORDER BY cat.position ASC, cat.title ASC)
					FROM   contact_categories cc
					JOIN   categories cat ON cat.id = cc.category_id
					WHERE  cc.contact_id = c.id AND %s
				), '[]'::json
			) AS categories
		FROM contacts c
		WHERE c.id = $1 AND %s
	`, campScope, catScope, rowScope)
	mainArgs := []any{contactID, scopeArg}
	err := r.DB.QueryRow(ctx, mainQuery, mainArgs...).Scan(
		&detail.ID, &detail.FirstName, &detail.LastName, &detail.Email,
		&detail.Company, &detail.Phone, &detail.CustomFields, &detail.Subscribed,
		&detail.UpdatedAt, &detail.CreatedAt,
		&detail.Source, &detail.SourceDetail, &detail.FirstSeenAt,
		&campaignsJSON, &categoriesJSON,
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
			errs.CaptureException(err)
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
			errs.CaptureException(err)
			return nil, errx.InternalError()
		}
	}

	// 2. Engagement aggregates. campaign_contact_progress is the canonical
	//    sent/opened/clicked/replied/bounced ledger keyed by (campaign,
	//    contact, sequence). Counts come from non-null timestamp columns,
	//    "last X" comes from MAX() of each. Opens count people only: a machine
	//    open is a delivery signal, not engagement, here as in analytics.
	engQuery := `
		SELECT
			COUNT(*) FILTER (WHERE sent_at    IS NOT NULL) AS sent,
			COUNT(*) FILTER (WHERE opened_at  IS NOT NULL AND NOT opened_machine) AS opened,
			COUNT(*) FILTER (WHERE clicked_at IS NOT NULL) AS clicked,
			COUNT(*) FILTER (WHERE replied_at IS NOT NULL) AS replied,
			COUNT(*) FILTER (WHERE bounced_at IS NOT NULL) AS bounced,
			MAX(sent_at), MAX(opened_at) FILTER (WHERE NOT opened_machine), MAX(clicked_at), MAX(replied_at), MAX(bounced_at)
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

		// Suppression: the contact's own address row wins over a row for its
		// whole domain, so the card names the entry that actually blocks mail.
		suppQuery := `
			SELECT id, kind, email, reason, source, expires_at, created_at
			FROM suppressed_recipients
			WHERE organization_id = $1
			  AND (expires_at IS NULL OR expires_at > NOW())
			  AND ((kind = 'email' AND email = LOWER($2))
			    OR (kind = 'domain' AND email = split_part(LOWER($2), '@', 2)))
			ORDER BY (kind = 'email') DESC
			LIMIT 1
		`
		var s models.ContactSuppression
		err := r.DB.QueryRow(ctx, suppQuery, *orgID, detail.Email).Scan(
			&s.ID, &s.Kind, &s.Value, &s.Reason, &s.Source, &s.ExpiresAt, &s.CreatedAt,
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
//
// opened_at is a person's open, as it is in campaign analytics and the
// contact's engagement summary: a fetch by a mail client's prefetch or a
// security gateway is reported separately as machine_opened_at, so this list
// never claims a recipient read mail they never opened (issue #392).
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
			CASE WHEN ccp.opened_machine THEN NULL ELSE ccp.opened_at END AS opened_at,
			CASE WHEN ccp.opened_machine THEN ccp.opened_at END AS machine_opened_at,
			ccp.clicked_at, ccp.replied_at, ccp.bounced_at
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
			&e.OpenedAt, &e.MachineOpenedAt, &e.ClickedAt, &e.RepliedAt, &e.BouncedAt,
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

// timelineKeyset is the predicate that pages one source of the contact
// timeline: rows whose (time, source rank, id) tuple sorts strictly before
// the cursor, which the query receives as three consecutive parameters
// starting at $first. Every source uses it so SQL and the merged sort agree.
func timelineKeyset(atCol string, source models.ContactTimelineSource, idCol string, first int) string {
	return fmt.Sprintf("(%s, %d, %s) < ($%d::timestamptz, $%d::int, $%d::uuid)",
		atCol, source, idCol, first, first+1, first+2)
}

// ListTimeline merges per-contact events from several source tables
// into a single, reverse-chronological feed.
//
// Sources:
//   - campaign_contact_progress       → sent / opened / clicked / replied / bounced
//   - email_link_clicks, email_opens  → per-event clicks and opens
//   - reply_intents                   → received replies (with intent classification)
//   - deliverability_events           → bounce / complaint
//   - suppressed_recipients           → suppression added
//   - contact_notes                   → CRM notes
//   - meeting_bookings                → meetings
//   - contact_activities              → creation and campaign / category membership
//   - website_page_hits               → page views from the tracking snippet
//
// We pull one row past the page from each source, newest first, then
// merge-sort in Go. This avoids a 10-way UNION with matching column lists
// (each source has a different shape), and the per-source limit caps the
// read at roughly 10*limit rows. The lookahead row is what makes has_more
// right when a single source fills the page on its own.
//
// The feed is ordered by (at, source, id) and a page resumes strictly after
// the cursor on that tuple, so two events at the same instant, from the
// same table or different ones, land on one side of a page boundary or the
// other and are never skipped or repeated. A nil cursor is the first page.
func (r *contactRepository) ListTimeline(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, contactID uuid.UUID, limit int, cursor *models.ContactTimelineKey) (*models.ContactTimelineResult, *errx.Error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	fetch := limit + 1

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

	// The position the page resumes after. The first page starts a minute
	// in the future at the lowest rank, which admits every event the same
	// way a real cursor would, so every query passes the same predicate.
	after := models.ContactTimelineKey{At: time.Now().Add(time.Minute)}
	if cursor != nil {
		after = *cursor
	}
	afterSource := int(after.Source)

	events := make([]models.ContactTimelineEvent, 0, limit*2)

	// 1. Engagement stamps from campaign_contact_progress, unnested to one
	//    row per stamp so the limit and the cursor apply to events, not to
	//    leads: a lead whose newest stamp is past the cursor must not push
	//    an older lead's eligible stamp off the page. The coarse opened and
	//    clicked stamps are emitted only when no logged open or click stands
	//    for them (a stamp written before per-event logging); otherwise
	//    sources 9 and 10 carry the event with its origin. A logged event
	//    represents the stamp when it landed within a minute of it, the
	//    stamp being written as the event is logged.
	progressQuery := fmt.Sprintf(`
		SELECT
			ev.source, ev.at, ccp.sequence_id, ccp.opened_machine,
			cam.id, cam.name,
			seq.id, seq.name, seq.subject,
			ea.id, ea.email, ea.name
		FROM campaign_contact_progress ccp
		JOIN campaigns cam ON cam.id = ccp.campaign_id
		JOIN sequences seq ON seq.id = ccp.sequence_id
		CROSS JOIN LATERAL (VALUES
			(%[1]d, ccp.sent_at),
			(%[2]d, ccp.opened_at),
			(%[3]d, ccp.clicked_at),
			(%[4]d, ccp.replied_at),
			(%[5]d, ccp.bounced_at)
		) AS ev(source, at)
		LEFT JOIN LATERAL (
			SELECT ea.id, ea.email, ea.name
			FROM   tasks t
			JOIN   campaign_tasks ct ON ct.task_id = t.id
			JOIN   email_accounts ea ON ea.id = t.email_account_id
			WHERE  ct.campaign_id = ccp.campaign_id
			  AND  ct.contact_id  = ccp.contact_id
			  AND  ct.sequence_id = ccp.sequence_id
			-- The task holding the step's reservation is the one that put the
			-- email on the wire; a step can carry several task rows (a retry, a
			-- tick that skipped as a duplicate) and only that one names the
			-- mailbox the recipient saw.
			ORDER  BY COALESCE(t.id = ccp.dispatch_task_id, false) DESC, t.created_at DESC
			LIMIT  1
		) ea ON TRUE
		WHERE ccp.contact_id = $1
		  AND cam.user_id    = $2
		  AND ev.at IS NOT NULL
		  AND (ev.at, ev.source, ccp.sequence_id) < ($3::timestamptz, $4::int, $5::uuid)
		  AND NOT (ev.source = %[2]d AND EXISTS (
				SELECT 1 FROM email_opens o
				WHERE o.campaign_id = ccp.campaign_id AND o.contact_id = ccp.contact_id AND o.sequence_id = ccp.sequence_id
				  AND o.opened_at BETWEEN ccp.opened_at - INTERVAL '1 minute' AND ccp.opened_at + INTERVAL '1 minute'
		  ))
		  AND NOT (ev.source = %[3]d AND EXISTS (
				SELECT 1 FROM email_link_clicks lc
				WHERE lc.campaign_id = ccp.campaign_id AND lc.contact_id = ccp.contact_id AND lc.sequence_id = ccp.sequence_id
				  AND lc.clicked_at BETWEEN ccp.clicked_at - INTERVAL '1 minute' AND ccp.clicked_at + INTERVAL '1 minute'
		  ))
		ORDER BY ev.at DESC, ev.source DESC, ccp.sequence_id DESC
		LIMIT $6
	`,
		models.TimelineSourceProgressSent,
		models.TimelineSourceProgressOpened,
		models.TimelineSourceProgressClicked,
		models.TimelineSourceProgressReplied,
		models.TimelineSourceProgressBounced,
	)
	prows, err := r.DB.Query(ctx, progressQuery, contactID, userID, after.At, afterSource, after.ID, fetch)
	if err != nil {
		db.CaptureError(err, progressQuery, []any{contactID, userID, after.At, afterSource, after.ID, fetch}, "ListTimeline progress")
		return nil, errx.InternalError()
	}
	for prows.Next() {
		var source int
		var at time.Time
		var seqKey uuid.UUID
		var openedMachine bool
		var campID, seqID, eaID *uuid.UUID
		var campName, seqName, seqSubject, eaEmail, eaName *string
		if err := prows.Scan(
			&source, &at, &seqKey, &openedMachine,
			&campID, &campName,
			&seqID, &seqName, &seqSubject,
			&eaID, &eaEmail, &eaName,
		); err != nil {
			prows.Close()
			db.CaptureError(err, "", nil, "ListTimeline progress scan")
			return nil, errx.InternalError()
		}
		ev := models.ContactTimelineEvent{
			At:                at,
			Key:               models.ContactTimelineKey{At: at, Source: models.ContactTimelineSource(source), ID: seqKey},
			EmailAccountID:    eaID,
			EmailAccountEmail: eaEmail,
			EmailAccountName:  eaName,
			CampaignID:        campID,
			CampaignName:      campName,
			SequenceID:        seqID,
			SequenceName:      seqName,
		}
		if seqSubject != nil && *seqSubject != "" {
			ev.Subject = seqSubject
		}
		switch ev.Key.Source {
		case models.TimelineSourceProgressSent:
			ev.Type = models.TimelineEmailSent
		case models.TimelineSourceProgressOpened:
			ev.Type = models.TimelineEmailOpened
			machine := openedMachine
			ev.Machine = &machine
		case models.TimelineSourceProgressClicked:
			ev.Type = models.TimelineEmailClicked
		case models.TimelineSourceProgressReplied:
			ev.Type = models.TimelineEmailReplied
		case models.TimelineSourceProgressBounced:
			ev.Type = models.TimelineEmailBounced
		default:
			continue
		}
		events = append(events, ev)
	}
	prows.Close()
	if err := prows.Err(); err != nil {
		db.CaptureError(err, progressQuery, nil, "ListTimeline progress rows")
		return nil, errx.InternalError()
	}

	// 9. Per-link clicks: which link, where it went, and whether a person or
	//    a scanner clicked it. Same campaign scope as the progress feed.
	clickQuery := `
		SELECT lc.id, lc.task_id, lc.clicked_at, lc.destination, lc.label, lc.user_agent, lc.machine, lc.machine_reason,
		       lc.client, lc.device_type, lc.os, lc.browser, lc.browser_version, lc.country_code, lc.region, lc.city,
		       cam.id, cam.name,
		       seq.id, seq.name, seq.subject,
		       ea.id, ea.email, ea.name
		FROM email_link_clicks lc
		JOIN campaigns cam ON cam.id = lc.campaign_id
		JOIN sequences seq ON seq.id = lc.sequence_id
		LEFT JOIN LATERAL (
			SELECT ea.id, ea.email, ea.name
			FROM   tasks t
			JOIN   email_accounts ea ON ea.id = t.email_account_id
			WHERE  t.id = lc.task_id
		) ea ON TRUE
		WHERE lc.contact_id = $1
		  AND cam.user_id   = $2
		  AND ` + timelineKeyset("lc.clicked_at", models.TimelineSourceLinkClick, "lc.id", 3) + `
		ORDER BY lc.clicked_at DESC, lc.id DESC
		LIMIT $6
	`
	crows, err := r.DB.Query(ctx, clickQuery, contactID, userID, after.At, afterSource, after.ID, fetch)
	if err != nil {
		db.CaptureError(err, clickQuery, []any{contactID, userID, after.At, afterSource, after.ID, fetch}, "ListTimeline link clicks")
		return nil, errx.InternalError()
	}
	for crows.Next() {
		var link models.ContactLinkClick
		var origin models.EngagementOrigin
		var taskID uuid.UUID
		var at time.Time
		var machine bool
		var reason string
		var campID, seqID, eaID *uuid.UUID
		var campName, seqName, seqSubject, eaEmail, eaName *string
		if err := crows.Scan(
			&link.ID, &taskID, &at, &link.URL, &link.Label, &link.UserAgent, &machine, &reason,
			&origin.Client, &origin.DeviceType, &origin.OS, &origin.Browser, &origin.BrowserVersion,
			&origin.CountryCode, &origin.Region, &origin.City,
			&campID, &campName,
			&seqID, &seqName, &seqSubject,
			&eaID, &eaEmail, &eaName,
		); err != nil {
			crows.Close()
			db.CaptureError(err, "", nil, "ListTimeline link clicks scan")
			return nil, errx.InternalError()
		}
		fillUTM(&link)
		ev := models.ContactTimelineEvent{
			Type:              models.TimelineEmailClicked,
			At:                at,
			Key:               models.ContactTimelineKey{At: at, Source: models.TimelineSourceLinkClick, ID: link.ID},
			EmailAccountID:    eaID,
			EmailAccountEmail: eaEmail,
			EmailAccountName:  eaName,
			CampaignID:        campID,
			CampaignName:      campName,
			SequenceID:        seqID,
			SequenceName:      seqName,
			Machine:           &machine,
			Link:              &link,
			TaskID:            &taskID,
		}
		if !origin.Empty() {
			o := origin
			ev.Origin = &o
		}
		if seqSubject != nil && *seqSubject != "" {
			ev.Subject = seqSubject
		}
		if reason != "" {
			r := reason
			ev.MachineReason = &r
		}
		events = append(events, ev)
	}
	crows.Close()
	if err := crows.Err(); err != nil {
		db.CaptureError(err, clickQuery, nil, "ListTimeline link clicks rows")
		return nil, errx.InternalError()
	}

	// 10. Per-event opens: each one with what it came from, machine ones
	//     labelled. Same campaign scope as the progress feed.
	openQuery := `
		SELECT o.id, o.task_id, o.opened_at, o.user_agent, o.machine, o.machine_reason,
		       o.client, o.device_type, o.os, o.browser, o.browser_version, o.country_code, o.region, o.city,
		       cam.id, cam.name,
		       seq.id, seq.name, seq.subject,
		       ea.id, ea.email, ea.name
		FROM email_opens o
		JOIN campaigns cam ON cam.id = o.campaign_id
		JOIN sequences seq ON seq.id = o.sequence_id
		LEFT JOIN LATERAL (
			SELECT ea.id, ea.email, ea.name
			FROM   tasks t
			JOIN   email_accounts ea ON ea.id = t.email_account_id
			WHERE  t.id = o.task_id
		) ea ON TRUE
		WHERE o.contact_id = $1
		  AND cam.user_id  = $2
		  AND ` + timelineKeyset("o.opened_at", models.TimelineSourceOpen, "o.id", 3) + `
		ORDER BY o.opened_at DESC, o.id DESC
		LIMIT $6
	`
	orows, err := r.DB.Query(ctx, openQuery, contactID, userID, after.At, afterSource, after.ID, fetch)
	if err != nil {
		db.CaptureError(err, openQuery, []any{contactID, userID, after.At, afterSource, after.ID, fetch}, "ListTimeline opens")
		return nil, errx.InternalError()
	}
	for orows.Next() {
		var id, taskID uuid.UUID
		var origin models.EngagementOrigin
		var at time.Time
		var machine bool
		var reason, userAgent string
		var campID, seqID, eaID *uuid.UUID
		var campName, seqName, seqSubject, eaEmail, eaName *string
		if err := orows.Scan(
			&id, &taskID, &at, &userAgent, &machine, &reason,
			&origin.Client, &origin.DeviceType, &origin.OS, &origin.Browser, &origin.BrowserVersion,
			&origin.CountryCode, &origin.Region, &origin.City,
			&campID, &campName,
			&seqID, &seqName, &seqSubject,
			&eaID, &eaEmail, &eaName,
		); err != nil {
			orows.Close()
			db.CaptureError(err, "", nil, "ListTimeline opens scan")
			return nil, errx.InternalError()
		}
		ev := models.ContactTimelineEvent{
			Type:              models.TimelineEmailOpened,
			At:                at,
			Key:               models.ContactTimelineKey{At: at, Source: models.TimelineSourceOpen, ID: id},
			EmailAccountID:    eaID,
			EmailAccountEmail: eaEmail,
			EmailAccountName:  eaName,
			CampaignID:        campID,
			CampaignName:      campName,
			SequenceID:        seqID,
			SequenceName:      seqName,
			Machine:           &machine,
			TaskID:            &taskID,
		}
		if !origin.Empty() {
			o := origin
			ev.Origin = &o
		}
		if seqSubject != nil && *seqSubject != "" {
			ev.Subject = seqSubject
		}
		if reason != "" {
			r := reason
			ev.MachineReason = &r
		}
		events = append(events, ev)
	}
	orows.Close()
	if err := orows.Err(); err != nil {
		db.CaptureError(err, openQuery, nil, "ListTimeline opens rows")
		return nil, errx.InternalError()
	}

	if orgID != nil {
		// 2. Reply intents (inbound replies with classification).
		replyQuery := `
			SELECT ri.id, ri.created_at, ri.intent, ri.campaign_id, cam.name, ri.task_id
			FROM reply_intents ri
			LEFT JOIN campaigns cam ON cam.id = ri.campaign_id
			WHERE ri.organization_id = $1
			  AND LOWER(ri.contact_email) = LOWER($2)
			  AND ` + timelineKeyset("ri.created_at", models.TimelineSourceReplyIntent, "ri.id", 3) + `
			ORDER BY ri.created_at DESC, ri.id DESC
			LIMIT $6
		`
		rrows, err := r.DB.Query(ctx, replyQuery, *orgID, contactEmail, after.At, afterSource, after.ID, fetch)
		if err != nil {
			db.CaptureError(err, replyQuery, nil, "ListTimeline replies")
			return nil, errx.InternalError()
		}
		for rrows.Next() {
			var ev models.ContactTimelineEvent
			var id uuid.UUID
			var intent string
			if err := rrows.Scan(&id, &ev.At, &intent, &ev.CampaignID, &ev.CampaignName, &ev.TaskID); err != nil {
				rrows.Close()
				db.CaptureError(err, "", nil, "ListTimeline replies scan")
				return nil, errx.InternalError()
			}
			ev.Type = models.TimelineReplyReceived
			ev.Key = models.ContactTimelineKey{At: ev.At, Source: models.TimelineSourceReplyIntent, ID: id}
			ev.Intent = &intent
			events = append(events, ev)
		}
		rrows.Close()
		if err := rrows.Err(); err != nil {
			db.CaptureError(err, replyQuery, nil, "ListTimeline replies rows")
			return nil, errx.InternalError()
		}

		// 3. Deliverability events (bounce / complaint / unsubscribe).
		delivQuery := `
			SELECT de.id, de.created_at, de.event_type, de.provider, de.reason,
			       de.campaign_id, cam.name, de.task_id
			FROM deliverability_events de
			LEFT JOIN campaigns cam ON cam.id = de.campaign_id
			WHERE de.organization_id = $1
			  AND (de.contact_id = $2 OR LOWER(de.recipient_email) = LOWER($3))
			  AND ` + timelineKeyset("de.created_at", models.TimelineSourceDeliverability, "de.id", 4) + `
			ORDER BY de.created_at DESC, de.id DESC
			LIMIT $7
		`
		drows, err := r.DB.Query(ctx, delivQuery, *orgID, contactID, contactEmail, after.At, afterSource, after.ID, fetch)
		if err != nil {
			db.CaptureError(err, delivQuery, nil, "ListTimeline deliv")
			return nil, errx.InternalError()
		}
		for drows.Next() {
			var ev models.ContactTimelineEvent
			var id uuid.UUID
			var eventType, provider, reason string
			if err := drows.Scan(&id, &ev.At, &eventType, &provider, &reason, &ev.CampaignID, &ev.CampaignName, &ev.TaskID); err != nil {
				drows.Close()
				db.CaptureError(err, "", nil, "ListTimeline deliv scan")
				return nil, errx.InternalError()
			}
			ev.Type = models.TimelineDeliverability
			ev.Key = models.ContactTimelineKey{At: ev.At, Source: models.TimelineSourceDeliverability, ID: id}
			ev.Source = &eventType
			ev.Provider = &provider
			if reason != "" {
				ev.Reason = &reason
			}
			events = append(events, ev)
		}
		drows.Close()
		if err := drows.Err(); err != nil {
			db.CaptureError(err, delivQuery, nil, "ListTimeline deliv rows")
			return nil, errx.InternalError()
		}

		// 4. Suppression: one event per matching entry (the address itself
		//    and its domain), at create time. Later updates are the same event.
		suppQuery := `
			SELECT id, created_at, reason, source
			FROM suppressed_recipients
			WHERE organization_id = $1
			  AND ((kind = 'email' AND email = LOWER($2))
			    OR (kind = 'domain' AND email = split_part(LOWER($2), '@', 2)))
			  AND ` + timelineKeyset("created_at", models.TimelineSourceSuppression, "id", 3) + `
			ORDER BY created_at DESC, id DESC
			LIMIT $6
		`
		srows, err := r.DB.Query(ctx, suppQuery, *orgID, contactEmail, after.At, afterSource, after.ID, fetch)
		if err != nil {
			db.CaptureError(err, suppQuery, nil, "ListTimeline suppression")
			return nil, errx.InternalError()
		}
		for srows.Next() {
			var id uuid.UUID
			var sAt time.Time
			var sReason, sSource string
			if err := srows.Scan(&id, &sAt, &sReason, &sSource); err != nil {
				srows.Close()
				db.CaptureError(err, "", nil, "ListTimeline suppression scan")
				return nil, errx.InternalError()
			}
			ev := models.ContactTimelineEvent{
				Type:   models.TimelineSuppressed,
				At:     sAt,
				Key:    models.ContactTimelineKey{At: sAt, Source: models.TimelineSourceSuppression, ID: id},
				Source: &sSource,
			}
			if sReason != "" {
				ev.Reason = &sReason
			}
			events = append(events, ev)
		}
		srows.Close()
		if err := srows.Err(); err != nil {
			db.CaptureError(err, suppQuery, nil, "ListTimeline suppression rows")
			return nil, errx.InternalError()
		}

		// 5. Notes.
		notesQuery := `
			SELECT id, created_at, user_id, content
			FROM contact_notes
			WHERE contact_id = $1
			  AND organization_id = $2
			  AND ` + timelineKeyset("created_at", models.TimelineSourceNote, "id", 3) + `
			ORDER BY created_at DESC, id DESC
			LIMIT $6
		`
		nrows, err := r.DB.Query(ctx, notesQuery, contactID, *orgID, after.At, afterSource, after.ID, fetch)
		if err != nil {
			db.CaptureError(err, notesQuery, nil, "ListTimeline notes")
			return nil, errx.InternalError()
		}
		for nrows.Next() {
			var ev models.ContactTimelineEvent
			var id, uid uuid.UUID
			var content string
			if err := nrows.Scan(&id, &ev.At, &uid, &content); err != nil {
				nrows.Close()
				db.CaptureError(err, "", nil, "ListTimeline notes scan")
				return nil, errx.InternalError()
			}
			ev.Type = models.TimelineNote
			ev.Key = models.ContactTimelineKey{At: ev.At, Source: models.TimelineSourceNote, ID: id}
			ev.UserID = &uid
			ev.Content = &content
			events = append(events, ev)
		}
		nrows.Close()
		if err := nrows.Err(); err != nil {
			db.CaptureError(err, notesQuery, nil, "ListTimeline notes rows")
			return nil, errx.InternalError()
		}

		// 6. Meetings booked through a connected scheduling provider. The event
		//    time is when the booking arrived; scheduled_for carries the call
		//    window so the UI can render "Meeting on <date>".
		meetingQuery := `
			SELECT id, created_at, status, source, event_name, scheduled_for, join_url, canceled_reason
			FROM meeting_bookings
			WHERE contact_id = $1
			  AND organization_id = $2
			  AND ` + timelineKeyset("created_at", models.TimelineSourceMeeting, "id", 3) + `
			ORDER BY created_at DESC, id DESC
			LIMIT $6
		`
		mrows, err := r.DB.Query(ctx, meetingQuery, contactID, *orgID, after.At, afterSource, after.ID, fetch)
		if err != nil {
			db.CaptureError(err, meetingQuery, nil, "ListTimeline meetings")
			return nil, errx.InternalError()
		}
		for mrows.Next() {
			var ev models.ContactTimelineEvent
			var id uuid.UUID
			var status, source, eventName, joinURL, canceledReason string
			var scheduledFor *time.Time
			if err := mrows.Scan(&id, &ev.At, &status, &source, &eventName, &scheduledFor, &joinURL, &canceledReason); err != nil {
				mrows.Close()
				db.CaptureError(err, "", nil, "ListTimeline meetings scan")
				return nil, errx.InternalError()
			}
			ev.Key = models.ContactTimelineKey{At: ev.At, Source: models.TimelineSourceMeeting, ID: id}
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
		if err := mrows.Err(); err != nil {
			db.CaptureError(err, meetingQuery, nil, "ListTimeline meetings rows")
			return nil, errx.InternalError()
		}

		// 7. Lifecycle: creation (with its first-touch source) and campaign /
		//    category membership changes, from contact_activities. Names were
		//    resolved when the row was written, so a renamed or deleted
		//    campaign still reads correctly.
		lifeQuery := `
			SELECT id, created_at, user_id, activity_type, metadata
			FROM contact_activities
			WHERE contact_id = $1
			  AND organization_id = $2
			  AND activity_type IN ('contact_created', 'campaign_added', 'campaign_removed', 'category_added', 'category_removed', 'form_submitted')
			  AND ` + timelineKeyset("created_at", models.TimelineSourceActivity, "id", 3) + `
			ORDER BY created_at DESC, id DESC
			LIMIT $6
		`
		lrows, err := r.DB.Query(ctx, lifeQuery, contactID, *orgID, after.At, afterSource, after.ID, fetch)
		if err != nil {
			db.CaptureError(err, lifeQuery, nil, "ListTimeline lifecycle")
			return nil, errx.InternalError()
		}
		for lrows.Next() {
			var ev models.ContactTimelineEvent
			var rowID uuid.UUID
			var typ string
			var meta map[string]any
			if err := lrows.Scan(&rowID, &ev.At, &ev.UserID, &typ, &meta); err != nil {
				lrows.Close()
				db.CaptureError(err, "", nil, "ListTimeline lifecycle scan")
				return nil, errx.InternalError()
			}
			ev.Type = models.ContactTimelineEventType(typ)
			ev.Key = models.ContactTimelineKey{At: ev.At, Source: models.TimelineSourceActivity, ID: rowID}
			str := func(k string) *string {
				if v, ok := meta[k].(string); ok && v != "" {
					return &v
				}
				return nil
			}
			id := func(k string) *uuid.UUID {
				if v, ok := meta[k].(string); ok {
					if u, perr := uuid.Parse(v); perr == nil {
						return &u
					}
				}
				return nil
			}
			switch ev.Type {
			case models.TimelineContactCreated:
				ev.Source = str("source")
				ev.SourceDetail = str("source_detail")
			case models.TimelineCampaignAdded, models.TimelineCampaignRemoved:
				ev.CampaignID = id("campaign_id")
				ev.CampaignName = str("campaign_name")
			case models.TimelineCategoryAdded, models.TimelineCategoryRemoved:
				ev.CategoryID = id("category_id")
				ev.CategoryTitle = str("category_title")
			case models.TimelineFormSubmitted:
				ev.FormID = id("form_id")
				ev.FormName = str("form_name")
			}
			events = append(events, ev)
		}
		lrows.Close()
		if err := lrows.Err(); err != nil {
			db.CaptureError(err, lifeQuery, nil, "ListTimeline lifecycle rows")
			return nil, errx.InternalError()
		}

		// 8. Website page views from any browser tied to the contact through
		//    an email-link ticket.
		hitQuery := `
			SELECT h.id, h.visitor_id, h.session_key, h.occurred_at,
			       h.url, h.path, h.title, h.referrer, h.referrer_domain, h.landing,
			       h.utm_source, h.utm_medium, h.utm_campaign, h.utm_term, h.utm_content,
			       h.device_type, h.os, h.browser, h.browser_version, h.device_brand,
			       h.language, h.timezone, h.screen_width, h.screen_height,
			       h.country_code, h.region, h.city
			FROM website_page_hits h
			WHERE h.organization_id = $1
			  AND h.visitor_id IN (SELECT id FROM website_visitors WHERE contact_id = $2)
			  AND ` + timelineKeyset("h.occurred_at", models.TimelineSourcePageHit, "h.id", 3) + `
			ORDER BY h.occurred_at DESC, h.id DESC
			LIMIT $6
		`
		hrows, err := r.DB.Query(ctx, hitQuery, *orgID, contactID, after.At, afterSource, after.ID, fetch)
		if err != nil {
			db.CaptureError(err, hitQuery, nil, "ListTimeline page hits")
			return nil, errx.InternalError()
		}
		for hrows.Next() {
			var h models.WebsitePageHit
			if err := hrows.Scan(
				&h.ID, &h.VisitorID, &h.SessionKey, &h.OccurredAt,
				&h.URL, &h.Path, &h.Title, &h.Referrer, &h.ReferrerDomain, &h.Landing,
				&h.UTMSource, &h.UTMMedium, &h.UTMCampaign, &h.UTMTerm, &h.UTMContent,
				&h.DeviceType, &h.OS, &h.Browser, &h.BrowserVersion, &h.DeviceBrand,
				&h.Language, &h.Timezone, &h.ScreenWidth, &h.ScreenHeight,
				&h.CountryCode, &h.Region, &h.City,
			); err != nil {
				hrows.Close()
				db.CaptureError(err, "", nil, "ListTimeline page hits scan")
				return nil, errx.InternalError()
			}
			hit := h
			ev := models.ContactTimelineEvent{
				Type:    models.TimelinePageHit,
				At:      h.OccurredAt,
				Key:     models.ContactTimelineKey{At: h.OccurredAt, Source: models.TimelineSourcePageHit, ID: h.ID},
				PageHit: &hit,
			}
			subject := h.Title
			if subject == "" {
				subject = h.Path
			}
			ev.Subject = &subject
			events = append(events, ev)
		}
		hrows.Close()
		if err := hrows.Err(); err != nil {
			db.CaptureError(err, hitQuery, nil, "ListTimeline page hits rows")
			return nil, errx.InternalError()
		}
	}

	// Merge sort: newest first, ties broken exactly as each source query
	// broke them, so the page boundary is the same position everywhere.
	sort.Slice(events, func(i, j int) bool { return events[j].Key.Before(events[i].Key) })

	res := &models.ContactTimelineResult{Data: events}
	if len(events) > limit {
		res.Data = events[:limit]
		last := res.Data[limit-1].Key
		res.HasMore = true
		res.Pagination = models.Pagination{
			NextCursor: paging.EncodeMerged(last.At, int(last.Source), last.ID),
			HasMore:    true,
		}
	}
	return res, nil
}

// fillUTM reads the UTM parameters off a clicked link's destination, whether
// the send path stamped them or the author wrote them by hand.
func fillUTM(link *models.ContactLinkClick) {
	u, err := url.Parse(link.URL)
	if err != nil {
		return
	}
	q := u.Query()
	link.UTMSource = q.Get("utm_source")
	link.UTMMedium = q.Get("utm_medium")
	link.UTMCampaign = q.Get("utm_campaign")
	link.UTMTerm = q.Get("utm_term")
	link.UTMContent = q.Get("utm_content")
}

// verificationFromRequest normalises a verdict a caller supplied with a
// contact. Empty means none; an unrecognised value is the caller's error.
func verificationFromRequest(status, provider string) (*models.ContactVerificationWrite, *errx.Error) {
	status = strings.TrimSpace(status)
	if status == "" {
		return nil, nil
	}
	provider = strings.TrimSpace(provider)
	if provider != "" {
		if _, ok := emailverify.KnownVocabulary(provider); !ok {
			return nil, errx.NewWithIdentifier(errx.BadRequest, "unknown_verification_provider",
				"unknown verification_provider "+strconv.Quote(provider))
		}
	}
	v, ok := emailverify.NormalizeExternal(provider, status)
	if !ok {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "unknown_verification_status",
			"verification_status "+strconv.Quote(status)+" is not a value any known verification service writes")
	}
	name := provider
	if name == "" {
		name = "imported"
	} else if k, ok := emailverify.KnownVocabulary(provider); ok {
		name = k
	}
	return &models.ContactVerificationWrite{
		Status:    string(v.Status),
		SubStatus: string(v.SubStatus),
		Reason:    "imported verdict: " + strings.ToLower(status),
		Provider:  name,
		Source:    models.VerificationSourceImported,
	}, nil
}

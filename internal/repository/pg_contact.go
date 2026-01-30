package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/email"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/utils"
)

type ContactRepository interface {
	Add(ctx context.Context, userID string, contacts []models.AddContact) ([]models.Contact, *errx.Error)
	GetByID(ctx context.Context, contactID uuid.UUID) (*models.Contact, *errx.Error)
	Search(ctx context.Context, userID string, category, cursor *string, filters models.SearchContacts, limit int32) (*models.ContactsResult, *errx.Error)
	BulkUpdate(ctx context.Context, userID string, data *models.BulkEditContactsData) ([]models.Contact, *errx.Error)
	Update(ctx context.Context, userID, contactID string, data *models.UpdateContact) (*models.Contact, *errx.Error)
	BulkDelete(ctx context.Context, userID string, contactIDs []string) *errx.Error
	Delete(ctx context.Context, userID string, contactID string) *errx.Error
	GetContactCount(ctx context.Context, userID string) (int, *errx.Error)
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

func (r *contactRepository) Add(ctx context.Context, userID string, contacts []models.AddContact) ([]models.Contact, *errx.Error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)

	var ncontacts []models.Contact = make([]models.Contact, len(contacts))

	b := pgx.Batch{}

	for _, lead := range contacts {
		if !email.IsValid(lead.Email) {
			return nil, errx.ErrEmail
		}

		data, err := json.Marshal(lead)
		if err != nil {
			return nil, errx.ErrContactSerialize
		}
		if len(data) > config.MaxContactSize {
			return nil, errx.ErrContactSize
		}

		for key := range lead.CustomFields {
			if !utils.IsValidJSONKey(key) {
				return nil, errx.ErrJSONKey
			}
		}

		b.Queue(
			`INSERT INTO contacts (
			 id, user_id, first_name, last_name, email, company, phone, custom_fields
			 ) VALUES (
			  gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7
			 ) RETURNING id, first_name, last_name, email, company, phone, custom_fields, subscribed, updated_at, created_at`,
			userID, lead.FirstName, lead.LastName, lead.Email, lead.Company, lead.Phone, lead.CustomFields,
		)
	}

	br := tx.SendBatch(ctx, &b)
	defer br.Close()

	bc := pgx.Batch{}

	for _, lead := range contacts {
		var ncon models.Contact = models.Contact{
			Campaigns:  make([]models.MiniCampaign, 0),
			Subscribed: true,
		}
		err := br.QueryRow().Scan(&ncon.ID, &ncon.FirstName, &ncon.LastName, &ncon.Email, &ncon.Company, &ncon.Phone, &ncon.CustomFields, &ncon.Subscribed, &ncon.UpdatedAt, &ncon.CreatedAt)
		if err != nil {
			br.Close()
			db.CaptureError(err, "", nil, "batch queryrow")
			return nil, errx.InternalError()
		}

		if len(lead.Campaigns) > 0 {
			const stmt = `
			INSERT INTO campaign_leads (contact, campaign)
			SELECT $1, campaigns.id
			FROM   campaigns
			WHERE  campaigns.id = ANY($2)
			AND  campaigns.user_id = $3
			ON CONFLICT (campaign, contact) DO NOTHING
			RETURNING campaigns.id, campaigns.name`

			bc.Queue(
				stmt,
				ncon.ID,
				lead.Campaigns,
				userID,
			)
		}
		ncontacts = append(ncontacts, ncon)
	}

	br.Close()

	brc := tx.SendBatch(ctx, &bc)

	for i := range ncontacts {
		if len(ncontacts[i].Campaigns) == 0 {
			continue
		}
		rows, err := brc.Query()
		if err != nil {
			brc.Close()
			db.CaptureError(err, "", nil, "batch query")
			return nil, errx.InternalError()
		}
		var idx int
		for rows.Next() {
			var id, name string
			err := rows.Scan(&id, &name)
			if err != nil {
				rows.Close()
				db.CaptureError(err, "", nil, "batch scan")
				return nil, errx.InternalError()
			}
			ncontacts[i].Campaigns[idx] = models.MiniCampaign{
				ID:   id,
				Name: name,
			}
			idx++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			brc.Close()
			db.CaptureError(err, "", nil, "rows")
			return nil, errx.InternalError()
		}
	}

	brc.Close()

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
			c.custom_fields, c.subscribed, c.updated_at, c.created_at
		FROM contacts c
		WHERE c.id = $1
	`

	var contact models.Contact
	err := r.DB.QueryRow(ctx, query, contactID).Scan(
		&contact.ID, &contact.FirstName, &contact.LastName, &contact.Email,
		&contact.Company, &contact.Phone, &contact.CustomFields, &contact.Subscribed,
		&contact.UpdatedAt, &contact.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, query, []any{contactID}, "queryrow")
		return nil, errx.InternalError()
	}

	contact.Campaigns = []models.MiniCampaign{}
	return &contact, nil
}

func (r *contactRepository) Search(
	ctx context.Context,
	userID string,
	category,
	cursor *string,
	filters models.SearchContacts,
	limit int32,
) (*models.ContactsResult, *errx.Error) {
	var whereClauses []string
	var args []any
	argIndex := 1

	if filters.Offset < 0 {
		filters.Offset = 0
	}

	// -----------------------------
	// Base filter: user_id
	// -----------------------------
	whereClauses = append(whereClauses, fmt.Sprintf("c.user_id = $%d", argIndex))
	args = append(args, userID)
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
	if len(filters.CampaignIDs) > 0 {
		placeholders := make([]string, len(filters.CampaignIDs))
		for i, id := range filters.CampaignIDs {
			placeholders[i] = fmt.Sprintf("$%d", argIndex)
			args = append(args, id)
			argIndex++
		}
		campaignClause := fmt.Sprintf(`
			c.id IN (
				SELECT contact
				FROM campaign_leads
				WHERE campaign IN (%s)
				GROUP BY contact
				HAVING COUNT(DISTINCT campaign) = %d
			)
		`, strings.Join(placeholders, ","), len(filters.CampaignIDs))
		whereClauses = append(whereClauses, campaignClause)
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

	// Main query
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
					AND cam.user_id = $%d
				), '[]'::json
			) AS campaigns
		FROM contacts c
		LEFT JOIN (
			SELECT contact_id, COUNT(campaign) AS campaign_count
			FROM campaign_leads
			GROUP BY contact
		) cl ON c.id = cl.contact_id
		%s
		ORDER BY %s %s, c.id ASC
		LIMIT $%d
	`, argIndex, whereSQL, sortBy, direction, argIndex+1)

	args = append(args, userID, limit+1)

	// Skip total count if cursor exists
	var totalCount *int64
	if cursor == nil || *cursor == "" {
		countQuery := fmt.Sprintf(`
			SELECT COUNT(*)
			FROM contacts c
			LEFT JOIN (
				SELECT contact, COUNT(campaign) AS campaign_count
				FROM campaign_leads
				GROUP BY contact
			) cl ON c.id = cl.contact
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

	var contacts []models.Contact
	for rows.Next() {
		var c models.Contact
		var campaignCount int
		var campaignsJSON []byte

		if err := rows.Scan(
			&c.ID, &c.FirstName, &c.LastName, &c.Email,
			&c.Company, &c.Phone, &c.CustomFields, &c.Subscribed,
			&c.UpdatedAt, &c.CreatedAt, &campaignCount, &campaignsJSON,
		); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
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

		contacts = append(contacts, c)
	}

	// Next cursor
	var nextCursor *uuid.UUID
	var hasMore bool
	if len(contacts) > int(limit) {
		hasMore = true
		nextID := contacts[limit].ID
		nextCursor = &nextID
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

func (r *contactRepository) Update(ctx context.Context, userID, contactID string, data *models.UpdateContact) (*models.Contact, *errx.Error) {
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
					JOIN campaigns cam ON cl2.campaign = cam.id
					WHERE cl2.contact = c.id AND cam.user_id = $2
				),
				'[]'::json
			) AS campaigns
		FROM contacts c
		WHERE c.id = $1 AND c.user_id = $2
		`

	params := []any{
		contactID,
		userID,
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
		args = append(args, contactID, userID)
		query := fmt.Sprintf(`
			UPDATE contacts
			SET %s
			WHERE id = $%d AND user_id = $%d
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
			WHERE contact = $1 AND campaign = $2
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
			INSERT INTO campaign_leads (contact, campaign)
			SELECT $1, id
			FROM campaigns
			WHERE id = $2 AND user_id = $3
			ON CONFLICT (campaign, contact) DO NOTHING
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
					JOIN campaigns cam ON cl.campaign = cam.id
					WHERE cl.contact = $1 AND cam.user_id = $2
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

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, errx.InternalError()
	}

	return &updatedContact, nil
}

func (r *contactRepository) BulkUpdate(ctx context.Context, userID string, data *models.BulkEditContactsData) ([]models.Contact, *errx.Error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}

	b := &pgx.Batch{}

	if data.Subscribe != nil {
		b.Queue(`UPDATE contacts
		         SET subscribed = $1, updated_at = NOW()
		         WHERE user_id = $2 AND id = ANY($3)`,
			*data.Subscribe, userID, data.Contacts)
	}

	if len(data.RemoveCampaigns) > 0 {
		b.Queue(`DELETE FROM campaign_leads cl
		         USING contacts c
		         WHERE cl.contact = c.id
		           AND c.user_id = $1
		           AND cl.contact = ANY($2)
		           AND cl.campaign = ANY($3)`,
			userID, data.Contacts, data.RemoveCampaigns)
	}

	if len(data.AddCampaigns) > 0 {
		b.Queue(`INSERT INTO campaign_leads (contact, campaign)
		         SELECT c.id, UNNEST($3::uuid[])
		         FROM contacts c
		         WHERE c.user_id = $1 AND c.id = ANY($2)
		         ON CONFLICT DO NOTHING`,
			userID, data.Contacts, data.AddCampaigns)
	}

	for _, p := range data.Fields {
		switch p.Type {
		case models.BulkAddField:
			b.Queue(`UPDATE contacts
			         SET custom_fields = custom_fields || jsonb_build_object($1,$2),
			             updated_at = NOW()
			         WHERE user_id = $3 AND id = ANY($4)`,
				p.Key, p.Value, userID, data.Contacts)
		case models.BulkEditField:
			b.Queue(`UPDATE contacts
			         SET custom_fields = jsonb_set(custom_fields, ARRAY[$1], to_jsonb($2::text)),
			             updated_at = NOW()
			         WHERE user_id = $3 AND id = ANY($4)`,
				p.Key, p.Value, userID, data.Contacts)
		case models.BulkDeleteField:
			b.Queue(`UPDATE contacts
			         SET custom_fields = custom_fields - $1,
			             updated_at = NOW()
			         WHERE user_id = $2 AND id = ANY($3)`,
				p.Key, userID, data.Contacts)
		case models.BulkRenameField:
			b.Queue(`UPDATE contacts
			         SET custom_fields = (custom_fields - $1) || jsonb_build_object($2, custom_fields->$1),
			             updated_at = NOW()
			         WHERE user_id = $3 AND id = ANY($4)
			           AND custom_fields ? $1`,
				p.Key, p.Value, userID, data.Contacts)
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
					JOIN campaigns cam ON cl.campaign = cam.id
					WHERE cl.contact = c.id AND cam.user_id = $2
				),
				'[]'::json
			) AS campaigns
		FROM contacts c
		WHERE c.user_id = $2 AND c.id = ANY($1)
	`

	params := []any{
		data.Contacts,
		userID,
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

		err := rows.Scan(
			&c.ID, &c.FirstName, &c.LastName, &c.Email,
			&c.Company, &c.Phone, &c.CustomFields, &c.Subscribed,
			&c.UpdatedAt, &c.CreatedAt, &campaignsJSON,
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

		updatedContacts = append(updatedContacts, c)
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, errx.InternalError()
	}

	return updatedContacts, nil
}

func (r *contactRepository) BulkDelete(ctx context.Context, userID string, IDs []string) *errx.Error {
	query := `
		DELETE FROM contacts
		WHERE id = ANY($1) AND user_id = $2
	`
	params := []any{
		IDs,
		userID,
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

func (r *contactRepository) Delete(ctx context.Context, userID, ID string) *errx.Error {
	query := `
		DELETE FROM contacts
		WHERE id = $1 AND user_id = $2
	`
	params := []any{
		ID,
		userID,
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

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

type AdvancedOutreachRepository interface {
	GetOutreachSettings(ctx context.Context, organizationID uuid.UUID) (*models.AdvancedOutreachSettings, error)
	UpsertOutreachSettings(ctx context.Context, organizationID, updatedBy uuid.UUID, settings *models.AdvancedOutreachSettings) error

	GetCampaignAdvancedSettings(ctx context.Context, campaignID uuid.UUID) (*models.CampaignAdvancedSettings, error)
	UpsertCampaignAdvancedSettings(ctx context.Context, campaignID uuid.UUID, settings *models.AdvancedOutreachSettings) error

	ListABVariants(ctx context.Context, campaignID uuid.UUID) ([]models.CampaignABVariant, error)
	CreateABVariant(ctx context.Context, campaignID uuid.UUID, req *models.CreateCampaignABVariantRequest) (*models.CampaignABVariant, error)
	UpdateABVariant(ctx context.Context, campaignID, variantID uuid.UUID, req *models.UpdateCampaignABVariantRequest) (*models.CampaignABVariant, error)
	DeleteABVariant(ctx context.Context, campaignID, variantID uuid.UUID) error
	GetAssignedVariant(ctx context.Context, campaignID, contactID uuid.UUID) (*models.CampaignABVariant, error)
	AssignVariant(ctx context.Context, campaignID, contactID, variantID uuid.UUID) error
	MarkVariantEvent(ctx context.Context, campaignID, contactID uuid.UUID, eventType string) error

	IsRecipientSuppressed(ctx context.Context, organizationID uuid.UUID, email string) (*models.SuppressedRecipient, error)
	UpsertSuppressedRecipient(ctx context.Context, entry *models.SuppressedRecipient) error

	CreateDeliverabilityEvent(ctx context.Context, event *models.DeliverabilityEvent) error
	GetDeliverabilityDashboard(ctx context.Context, organizationID uuid.UUID, from, to time.Time) (*models.DeliverabilityDashboard, error)

	StartTaskExecution(ctx context.Context, taskID uuid.UUID, executionKey string, metadata map[string]interface{}) (bool, error)
	CompleteTaskExecution(ctx context.Context, taskID uuid.UUID, executionKey, status string, metadata map[string]interface{}) error

	CreateTaskDeadLetter(ctx context.Context, item *models.TaskDeadLetter) error
	ListTaskDeadLetters(ctx context.Context, organizationID uuid.UUID, status string, limit int) ([]models.TaskDeadLetter, error)
	GetTaskDeadLetter(ctx context.Context, id uuid.UUID, organizationID uuid.UUID) (*models.TaskDeadLetter, error)
	MarkTaskDeadLetterReplayed(ctx context.Context, id uuid.UUID) error

	CreateReplyIntent(ctx context.Context, record *models.ReplyIntentRecord) error
	CreatePreflightReport(ctx context.Context, report *models.PreflightReport) error

	GetABVariantStats(ctx context.Context, campaignID uuid.UUID) ([]models.ABVariantStats, error)

	// DLQ auto-retry
	ListRetryableDeadLetters(ctx context.Context, limit int) ([]models.TaskDeadLetter, error)
	IncrementDeadLetterAttempt(ctx context.Context, id uuid.UUID, nextRetryAt *time.Time) error
}

type advancedOutreachRepository struct {
	db *pgxpool.Pool
}

func NewAdvancedOutreachRepository(db *pgxpool.Pool) AdvancedOutreachRepository {
	return &advancedOutreachRepository{db: db}
}

func marshalJSON(v interface{}) ([]byte, error) {
	if v == nil {
		return []byte(`{}`), nil
	}
	return json.Marshal(v)
}

func unmarshalJSON[T any](b []byte, out *T) error {
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, out)
}

func (r *advancedOutreachRepository) GetOutreachSettings(ctx context.Context, organizationID uuid.UUID) (*models.AdvancedOutreachSettings, error) {
	query := `SELECT settings FROM outreach_settings WHERE organization_id = $1`
	var raw []byte
	if err := r.db.QueryRow(ctx, query, organizationID).Scan(&raw); err != nil {
		if err == pgx.ErrNoRows {
			def := models.DefaultAdvancedOutreachSettings()
			return &def, nil
		}
		return nil, err
	}

	settings := models.DefaultAdvancedOutreachSettings()
	if err := unmarshalJSON(raw, &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}

func (r *advancedOutreachRepository) UpsertOutreachSettings(ctx context.Context, organizationID, updatedBy uuid.UUID, settings *models.AdvancedOutreachSettings) error {
	raw, err := marshalJSON(settings)
	if err != nil {
		return err
	}
	query := `
		INSERT INTO outreach_settings (organization_id, settings, updated_by, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (organization_id)
		DO UPDATE SET settings = EXCLUDED.settings, updated_by = EXCLUDED.updated_by, updated_at = NOW()
	`
	_, err = r.db.Exec(ctx, query, organizationID, raw, updatedBy)
	return err
}

func (r *advancedOutreachRepository) GetCampaignAdvancedSettings(ctx context.Context, campaignID uuid.UUID) (*models.CampaignAdvancedSettings, error) {
	query := `SELECT settings, updated_at FROM campaign_advanced_settings WHERE campaign_id = $1`
	var raw []byte
	var updatedAt time.Time
	if err := r.db.QueryRow(ctx, query, campaignID).Scan(&raw, &updatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	settings := models.DefaultAdvancedOutreachSettings()
	if err := unmarshalJSON(raw, &settings); err != nil {
		return nil, err
	}
	return &models.CampaignAdvancedSettings{
		CampaignID: campaignID,
		Overrides:  settings,
		UpdatedAt:  updatedAt,
	}, nil
}

func (r *advancedOutreachRepository) UpsertCampaignAdvancedSettings(ctx context.Context, campaignID uuid.UUID, settings *models.AdvancedOutreachSettings) error {
	raw, err := marshalJSON(settings)
	if err != nil {
		return err
	}
	query := `
		INSERT INTO campaign_advanced_settings (campaign_id, settings, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (campaign_id)
		DO UPDATE SET settings = EXCLUDED.settings, updated_at = NOW()
	`
	_, err = r.db.Exec(ctx, query, campaignID, raw)
	return err
}

func scanABVariant(rows pgx.Row, v *models.CampaignABVariant) error {
	var metadata []byte
	if err := rows.Scan(
		&v.ID,
		&v.CampaignID,
		&v.SequenceID,
		&v.Name,
		&v.Weight,
		&v.Subject,
		&v.BodyHTML,
		&v.BodyPlain,
		&v.IsControl,
		&v.IsActive,
		&metadata,
		&v.CreatedAt,
		&v.UpdatedAt,
	); err != nil {
		return err
	}
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &v.Metadata); err != nil {
			return err
		}
	}
	if v.Metadata == nil {
		v.Metadata = map[string]interface{}{}
	}
	return nil
}

func (r *advancedOutreachRepository) ListABVariants(ctx context.Context, campaignID uuid.UUID) ([]models.CampaignABVariant, error) {
	query := `
		SELECT id, campaign_id, sequence_id, name, weight, subject, body_html, body_plain, is_control, is_active, metadata, created_at, updated_at
		FROM campaign_ab_variants
		WHERE campaign_id = $1
		ORDER BY is_control DESC, created_at ASC
	`
	rows, err := r.db.Query(ctx, query, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.CampaignABVariant
	for rows.Next() {
		var v models.CampaignABVariant
		if err := scanABVariant(rows, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *advancedOutreachRepository) CreateABVariant(ctx context.Context, campaignID uuid.UUID, req *models.CreateCampaignABVariantRequest) (*models.CampaignABVariant, error) {
	metadata, err := marshalJSON(req.Metadata)
	if err != nil {
		return nil, err
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	weight := req.Weight
	if weight <= 0 {
		weight = 100
	}
	query := `
		INSERT INTO campaign_ab_variants (
			campaign_id, sequence_id, name, weight, subject, body_html, body_plain, is_control, is_active, metadata, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
		RETURNING id, campaign_id, sequence_id, name, weight, subject, body_html, body_plain, is_control, is_active, metadata, created_at, updated_at
	`
	var out models.CampaignABVariant
	if err := scanABVariant(r.db.QueryRow(ctx, query,
		campaignID,
		req.SequenceID,
		req.Name,
		weight,
		req.Subject,
		req.BodyHTML,
		req.BodyPlain,
		req.IsControl,
		isActive,
		metadata,
	), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *advancedOutreachRepository) UpdateABVariant(ctx context.Context, campaignID, variantID uuid.UUID, req *models.UpdateCampaignABVariantRequest) (*models.CampaignABVariant, error) {
	sets := make([]string, 0, 8)
	args := []interface{}{campaignID, variantID}
	argPos := 3

	if req.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argPos))
		args = append(args, *req.Name)
		argPos++
	}
	if req.Weight != nil {
		sets = append(sets, fmt.Sprintf("weight = $%d", argPos))
		args = append(args, *req.Weight)
		argPos++
	}
	if req.Subject != nil {
		sets = append(sets, fmt.Sprintf("subject = $%d", argPos))
		args = append(args, *req.Subject)
		argPos++
	}
	if req.BodyHTML != nil {
		sets = append(sets, fmt.Sprintf("body_html = $%d", argPos))
		args = append(args, *req.BodyHTML)
		argPos++
	}
	if req.BodyPlain != nil {
		sets = append(sets, fmt.Sprintf("body_plain = $%d", argPos))
		args = append(args, *req.BodyPlain)
		argPos++
	}
	if req.IsControl != nil {
		sets = append(sets, fmt.Sprintf("is_control = $%d", argPos))
		args = append(args, *req.IsControl)
		argPos++
	}
	if req.IsActive != nil {
		sets = append(sets, fmt.Sprintf("is_active = $%d", argPos))
		args = append(args, *req.IsActive)
		argPos++
	}
	if req.Metadata != nil {
		metadata, err := marshalJSON(req.Metadata)
		if err != nil {
			return nil, err
		}
		sets = append(sets, fmt.Sprintf("metadata = $%d", argPos))
		args = append(args, metadata)
		argPos++
	}

	if len(sets) == 0 {
		return nil, fmt.Errorf("no fields to update")
	}
	sets = append(sets, "updated_at = NOW()")

	query := fmt.Sprintf(`
		UPDATE campaign_ab_variants
		SET %s
		WHERE campaign_id = $1 AND id = $2
		RETURNING id, campaign_id, sequence_id, name, weight, subject, body_html, body_plain, is_control, is_active, metadata, created_at, updated_at
	`, strings.Join(sets, ", "))

	var out models.CampaignABVariant
	if err := scanABVariant(r.db.QueryRow(ctx, query, args...), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *advancedOutreachRepository) DeleteABVariant(ctx context.Context, campaignID, variantID uuid.UUID) error {
	query := `DELETE FROM campaign_ab_variants WHERE campaign_id = $1 AND id = $2`
	cmd, err := r.db.Exec(ctx, query, campaignID, variantID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *advancedOutreachRepository) GetAssignedVariant(ctx context.Context, campaignID, contactID uuid.UUID) (*models.CampaignABVariant, error) {
	query := `
		SELECT v.id, v.campaign_id, v.sequence_id, v.name, v.weight, v.subject, v.body_html, v.body_plain, v.is_control, v.is_active, v.metadata, v.created_at, v.updated_at
		FROM campaign_ab_assignments a
		JOIN campaign_ab_variants v ON v.id = a.variant_id
		WHERE a.campaign_id = $1 AND a.contact_id = $2
	`
	var out models.CampaignABVariant
	if err := scanABVariant(r.db.QueryRow(ctx, query, campaignID, contactID), &out); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func (r *advancedOutreachRepository) AssignVariant(ctx context.Context, campaignID, contactID, variantID uuid.UUID) error {
	query := `
		INSERT INTO campaign_ab_assignments (campaign_id, contact_id, variant_id, assigned_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (campaign_id, contact_id)
		DO UPDATE SET variant_id = EXCLUDED.variant_id, assigned_at = NOW()
	`
	_, err := r.db.Exec(ctx, query, campaignID, contactID, variantID)
	return err
}

func (r *advancedOutreachRepository) MarkVariantEvent(ctx context.Context, campaignID, contactID uuid.UUID, eventType string) error {
	var setClause string
	switch eventType {
	case string(models.DeliverabilityEventOpen):
		setClause = "opened_at = COALESCE(opened_at, NOW())"
	case string(models.DeliverabilityEventClick):
		setClause = "clicked_at = COALESCE(clicked_at, NOW())"
	case string(models.DeliverabilityEventReply):
		setClause = "replied_at = COALESCE(replied_at, NOW())"
	case string(models.DeliverabilityEventBounce):
		setClause = "bounced_at = COALESCE(bounced_at, NOW())"
	default:
		return nil
	}
	query := fmt.Sprintf(`UPDATE campaign_ab_assignments SET %s WHERE campaign_id = $1 AND contact_id = $2`, setClause)
	_, err := r.db.Exec(ctx, query, campaignID, contactID)
	return err
}

func (r *advancedOutreachRepository) IsRecipientSuppressed(ctx context.Context, organizationID uuid.UUID, email string) (*models.SuppressedRecipient, error) {
	query := `
		SELECT id, organization_id, email, reason, source, campaign_id, expires_at, metadata, created_at, updated_at
		FROM suppressed_recipients
		WHERE organization_id = $1
		  AND LOWER(email) = LOWER($2)
		  AND (expires_at IS NULL OR expires_at > NOW())
	`
	var out models.SuppressedRecipient
	var metadata []byte
	if err := r.db.QueryRow(ctx, query, organizationID, email).Scan(
		&out.ID,
		&out.OrganizationID,
		&out.Email,
		&out.Reason,
		&out.Source,
		&out.CampaignID,
		&out.ExpiresAt,
		&metadata,
		&out.CreatedAt,
		&out.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &out.Metadata)
	}
	return &out, nil
}

func (r *advancedOutreachRepository) UpsertSuppressedRecipient(ctx context.Context, entry *models.SuppressedRecipient) error {
	metadata, err := marshalJSON(entry.Metadata)
	if err != nil {
		return err
	}
	query := `
		INSERT INTO suppressed_recipients (organization_id, email, reason, source, campaign_id, expires_at, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
		ON CONFLICT (organization_id, email)
		DO UPDATE SET
			reason = EXCLUDED.reason,
			source = EXCLUDED.source,
			campaign_id = EXCLUDED.campaign_id,
			expires_at = EXCLUDED.expires_at,
			metadata = EXCLUDED.metadata,
			updated_at = NOW()
	`
	_, err = r.db.Exec(ctx, query, entry.OrganizationID, strings.ToLower(strings.TrimSpace(entry.Email)), entry.Reason, entry.Source, entry.CampaignID, entry.ExpiresAt, metadata)
	return err
}

func (r *advancedOutreachRepository) CreateDeliverabilityEvent(ctx context.Context, event *models.DeliverabilityEvent) error {
	metadata, err := marshalJSON(event.Metadata)
	if err != nil {
		return err
	}
	query := `
		INSERT INTO deliverability_events (
			organization_id, campaign_id, task_id, contact_id, event_type, provider,
			recipient_email, reason, idempotency_key, metadata, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
		ON CONFLICT (idempotency_key) DO NOTHING
	`
	_, err = r.db.Exec(ctx, query,
		event.OrganizationID,
		event.CampaignID,
		event.TaskID,
		event.ContactID,
		event.EventType,
		event.Provider,
		strings.ToLower(strings.TrimSpace(event.RecipientEmail)),
		event.Reason,
		event.IdempotencyKey,
		metadata,
	)
	return err
}

func (r *advancedOutreachRepository) GetDeliverabilityDashboard(ctx context.Context, organizationID uuid.UUID, from, to time.Time) (*models.DeliverabilityDashboard, error) {
	out := &models.DeliverabilityDashboard{
		From: from,
		To:   to,
	}

	queryEvents := `
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE event_type = 'bounce') AS bounces,
			COUNT(*) FILTER (WHERE event_type = 'complaint') AS complaints,
			COUNT(*) FILTER (WHERE event_type = 'unsubscribe') AS unsubscribes,
			COUNT(*) FILTER (WHERE event_type = 'reply') AS replies,
			COUNT(*) FILTER (WHERE event_type = 'open') AS opens,
			COUNT(*) FILTER (WHERE event_type = 'click') AS clicks
		FROM deliverability_events
		WHERE organization_id = $1
		  AND created_at >= $2
		  AND created_at <= $3
	`
	if err := r.db.QueryRow(ctx, queryEvents, organizationID, from, to).Scan(
		&out.EventsTotal,
		&out.BounceCount,
		&out.ComplaintCount,
		&out.UnsubscribeCount,
		&out.ReplyCount,
		&out.OpenCount,
		&out.ClickCount,
	); err != nil {
		return nil, err
	}

	querySuppressed := `SELECT COUNT(*) FROM suppressed_recipients WHERE organization_id = $1 AND (expires_at IS NULL OR expires_at > NOW())`
	if err := r.db.QueryRow(ctx, querySuppressed, organizationID).Scan(&out.SuppressedRecipients); err != nil {
		return nil, err
	}

	queryDLQ := `
		SELECT COUNT(*)
		FROM task_dead_letters d
		JOIN tasks t ON t.id = d.task_id
		JOIN email_accounts ea ON ea.id = t.email_account_id
		WHERE ea.organization_id = $1
		  AND d.status = 'pending'
	`
	if err := r.db.QueryRow(ctx, queryDLQ, organizationID).Scan(&out.DLQPending); err != nil {
		return nil, err
	}

	queryIntents := `
		SELECT
			COUNT(*) FILTER (WHERE intent = 'positive') AS positive,
			COUNT(*) FILTER (WHERE intent = 'negative') AS negative,
			COUNT(*) FILTER (WHERE intent = 'out_of_office') AS ooo,
			COUNT(*) FILTER (WHERE intent = 'question') AS question,
			COUNT(*) FILTER (WHERE intent = 'neutral') AS neutral
		FROM reply_intents
		WHERE organization_id = $1
		  AND created_at >= $2
		  AND created_at <= $3
	`
	if err := r.db.QueryRow(ctx, queryIntents, organizationID, from, to).Scan(
		&out.IntentPositive,
		&out.IntentNegative,
		&out.IntentOOO,
		&out.IntentQuestion,
		&out.IntentNeutral,
	); err != nil {
		return nil, err
	}

	// Rate denominator: completed campaign sends in the window. Best-effort — a
	// rate/breakdown query failing must not break the (already-built) core rollup.
	sentQuery := `
		SELECT COUNT(*) FROM tasks t
		JOIN email_accounts ea ON ea.id = t.email_account_id
		WHERE ea.organization_id = $1 AND t.task_type = 'campaign' AND t.status = 'completed'
		  AND t.completed_at >= $2 AND t.completed_at <= $3`
	_ = r.db.QueryRow(ctx, sentQuery, organizationID, from, to).Scan(&out.EmailsSent)
	out.BounceRate = models.Rate(out.BounceCount, out.EmailsSent)
	out.ComplaintRate = models.Rate(out.ComplaintCount, out.EmailsSent)
	out.OpenRate = models.Rate(out.OpenCount, out.EmailsSent)
	out.ClickRate = models.Rate(out.ClickCount, out.EmailsSent)
	out.ReplyRate = models.Rate(out.ReplyCount, out.EmailsSent)

	out.Timeseries = r.deliverabilityTimeseries(ctx, organizationID, from, to)
	out.ByCampaign = r.deliverabilityByCampaign(ctx, organizationID, from, to)
	out.ByMailbox = r.deliverabilityByMailbox(ctx, organizationID, from, to)

	// Seed inbox-placement (optional — nil rates when there are no seed samples).
	var spam, inbox, placementTotal int
	placementQuery := `
		SELECT pr.folder, COUNT(*)
		FROM placement_results pr JOIN placement_tests pt ON pt.id = pr.test_id
		WHERE pt.organization_id = $1 AND pr.detected_at >= $2 AND pr.detected_at <= $3 AND pr.folder <> 'pending'
		GROUP BY pr.folder`
	if rows, perr := r.db.Query(ctx, placementQuery, organizationID, from, to); perr == nil {
		for rows.Next() {
			var folder string
			var n int
			if rows.Scan(&folder, &n) == nil {
				placementTotal += n
				switch folder {
				case "spam":
					spam = n
				case "inbox":
					inbox = n
				}
			}
		}
		rows.Close()
	}
	out.PlacementSamples = placementTotal
	spamRate := 0.0
	if placementTotal > 0 {
		sr := models.Rate(spam, placementTotal)
		ir := models.Rate(inbox, placementTotal)
		out.SpamPlacementRate = &sr
		out.InboxPlacementRate = &ir
		spamRate = sr
	}

	out.Band = models.DeliverabilityBand(out.BounceRate, out.ComplaintRate, spamRate)
	return out, nil
}

// deliverabilityTimeseries returns gap-filled UTC daily points for the window.
func (r *advancedOutreachRepository) deliverabilityTimeseries(ctx context.Context, orgID uuid.UUID, from, to time.Time) []models.DeliverabilityDailyPoint {
	byDay := map[string]*models.DeliverabilityDailyPoint{}
	// Bucket by UTC calendar day explicitly (created_at::date alone uses the DB
	// session timezone) so the SQL days line up with the Go gap-fill loop below.
	evQ := `
		SELECT (created_at AT TIME ZONE 'UTC')::date,
			COUNT(*) FILTER (WHERE event_type='bounce'),
			COUNT(*) FILTER (WHERE event_type='complaint'),
			COUNT(*) FILTER (WHERE event_type='open'),
			COUNT(*) FILTER (WHERE event_type='click'),
			COUNT(*) FILTER (WHERE event_type='reply'),
			COUNT(*) FILTER (WHERE event_type='unsubscribe')
		FROM deliverability_events
		WHERE organization_id=$1 AND created_at >= $2 AND created_at <= $3
		GROUP BY 1`
	if rows, err := r.db.Query(ctx, evQ, orgID, from, to); err == nil {
		for rows.Next() {
			var d time.Time
			var b, c, o, cl, rep, u int
			if rows.Scan(&d, &b, &c, &o, &cl, &rep, &u) == nil {
				key := d.Format("2006-01-02")
				byDay[key] = &models.DeliverabilityDailyPoint{Date: key, Bounces: b, Complaints: c, Opens: o, Clicks: cl, Replies: rep, Unsubscribes: u}
			}
		}
		rows.Close()
	}
	sentQ := `
		SELECT (t.completed_at AT TIME ZONE 'UTC')::date, COUNT(*)
		FROM tasks t JOIN email_accounts ea ON ea.id = t.email_account_id
		WHERE ea.organization_id=$1 AND t.task_type='campaign' AND t.status='completed'
		  AND t.completed_at >= $2 AND t.completed_at <= $3
		GROUP BY 1`
	if rows, err := r.db.Query(ctx, sentQ, orgID, from, to); err == nil {
		for rows.Next() {
			var d time.Time
			var s int
			if rows.Scan(&d, &s) == nil {
				key := d.Format("2006-01-02")
				if p := byDay[key]; p != nil {
					p.Sent = s
				} else {
					byDay[key] = &models.DeliverabilityDailyPoint{Date: key, Sent: s}
				}
			}
		}
		rows.Close()
	}
	out := []models.DeliverabilityDailyPoint{}
	for d := from.UTC().Truncate(24 * time.Hour); !d.After(to.UTC()); d = d.Add(24 * time.Hour) {
		key := d.Format("2006-01-02")
		if p := byDay[key]; p != nil {
			out = append(out, *p)
		} else {
			out = append(out, models.DeliverabilityDailyPoint{Date: key})
		}
	}
	return out
}

// deliverabilityByCampaign ranks campaigns by bounce+complaint in the window,
// with the per-campaign sent denominator from campaign_contact_progress.
func (r *advancedOutreachRepository) deliverabilityByCampaign(ctx context.Context, orgID uuid.UUID, from, to time.Time) []models.CampaignDeliverability {
	out := []models.CampaignDeliverability{}
	q := `
		SELECT de.campaign_id, c.name,
			COUNT(*) FILTER (WHERE de.event_type='bounce'),
			COUNT(*) FILTER (WHERE de.event_type='complaint')
		FROM deliverability_events de JOIN campaigns c ON c.id = de.campaign_id
		WHERE de.organization_id=$1 AND de.created_at >= $2 AND de.created_at <= $3 AND de.campaign_id IS NOT NULL
		GROUP BY de.campaign_id, c.name
		ORDER BY (COUNT(*) FILTER (WHERE de.event_type='bounce') + COUNT(*) FILTER (WHERE de.event_type='complaint')) DESC
		LIMIT 20`
	rows, err := r.db.Query(ctx, q, orgID, from, to)
	if err != nil {
		return out
	}
	type rec struct {
		id   uuid.UUID
		name string
		b, c int
	}
	items := []rec{}
	for rows.Next() {
		var x rec
		if rows.Scan(&x.id, &x.name, &x.b, &x.c) == nil {
			items = append(items, x)
		}
	}
	rows.Close()
	if len(items) == 0 {
		return out
	}
	sent := map[uuid.UUID]int{}
	sq := `
		SELECT ccp.campaign_id, COUNT(*)
		FROM campaign_contact_progress ccp JOIN campaigns c ON c.id = ccp.campaign_id
		WHERE c.organization_id=$1 AND ccp.sent_at IS NOT NULL AND ccp.sent_at >= $2 AND ccp.sent_at <= $3
		GROUP BY ccp.campaign_id`
	if srows, serr := r.db.Query(ctx, sq, orgID, from, to); serr == nil {
		for srows.Next() {
			var id uuid.UUID
			var n int
			if srows.Scan(&id, &n) == nil {
				sent[id] = n
			}
		}
		srows.Close()
	}
	for _, x := range items {
		s := sent[x.id]
		br := models.Rate(x.b, s)
		cr := models.Rate(x.c, s)
		out = append(out, models.CampaignDeliverability{CampaignID: x.id, Name: x.name, Sent: s, Bounces: x.b, Complaints: x.c, BounceRate: br, ComplaintRate: cr, Band: models.DeliverabilityBand(br, cr, 0)})
	}
	return out
}

// deliverabilityByMailbox ranks mailboxes by bounce+complaint in the window,
// with the per-mailbox sent denominator from completed campaign tasks.
func (r *advancedOutreachRepository) deliverabilityByMailbox(ctx context.Context, orgID uuid.UUID, from, to time.Time) []models.MailboxDeliverability {
	out := []models.MailboxDeliverability{}
	q := `
		SELECT ea.id, ea.email,
			COUNT(*) FILTER (WHERE de.event_type='bounce'),
			COUNT(*) FILTER (WHERE de.event_type='complaint')
		FROM deliverability_events de
		JOIN tasks t ON t.id = de.task_id
		JOIN email_accounts ea ON ea.id = t.email_account_id
		WHERE de.organization_id=$1 AND de.created_at >= $2 AND de.created_at <= $3 AND de.task_id IS NOT NULL
		GROUP BY ea.id, ea.email
		ORDER BY (COUNT(*) FILTER (WHERE de.event_type='bounce') + COUNT(*) FILTER (WHERE de.event_type='complaint')) DESC
		LIMIT 50`
	rows, err := r.db.Query(ctx, q, orgID, from, to)
	if err != nil {
		return out
	}
	type rec struct {
		id    uuid.UUID
		email string
		b, c  int
	}
	items := []rec{}
	for rows.Next() {
		var x rec
		if rows.Scan(&x.id, &x.email, &x.b, &x.c) == nil {
			items = append(items, x)
		}
	}
	rows.Close()
	if len(items) == 0 {
		return out
	}
	sent := map[uuid.UUID]int{}
	sq := `
		SELECT t.email_account_id, COUNT(*)
		FROM tasks t JOIN email_accounts ea ON ea.id = t.email_account_id
		WHERE ea.organization_id=$1 AND t.task_type='campaign' AND t.status='completed'
		  AND t.completed_at >= $2 AND t.completed_at <= $3
		GROUP BY t.email_account_id`
	if srows, serr := r.db.Query(ctx, sq, orgID, from, to); serr == nil {
		for srows.Next() {
			var id uuid.UUID
			var n int
			if srows.Scan(&id, &n) == nil {
				sent[id] = n
			}
		}
		srows.Close()
	}
	for _, x := range items {
		s := sent[x.id]
		br := models.Rate(x.b, s)
		cr := models.Rate(x.c, s)
		out = append(out, models.MailboxDeliverability{EmailAccountID: x.id, Email: x.email, Sent: s, Bounces: x.b, Complaints: x.c, BounceRate: br, ComplaintRate: cr, Band: models.DeliverabilityBand(br, cr, 0)})
	}
	return out
}

// executionLockTTL is the maximum time an in_progress execution lock is considered valid.
// If a lock has been in_progress longer than this, it is treated as expired (worker crash).
const executionLockTTL = 5 * time.Minute

func (r *advancedOutreachRepository) StartTaskExecution(ctx context.Context, taskID uuid.UUID, executionKey string, metadata map[string]interface{}) (bool, error) {
	var existingStatus string
	var lastSeenAt time.Time
	err := r.db.QueryRow(ctx,
		`SELECT status, last_seen_at FROM task_execution_keys WHERE task_id = $1 AND execution_key = $2`,
		taskID, executionKey,
	).Scan(&existingStatus, &lastSeenAt)
	if err == nil {
		switch existingStatus {
		case "completed":
			_, _ = r.db.Exec(ctx, `UPDATE task_execution_keys SET attempts = attempts + 1, last_seen_at = NOW() WHERE task_id = $1 AND execution_key = $2`, taskID, executionKey)
			return true, nil
		case "in_progress":
			// Check if the lock has expired (worker likely crashed)
			if time.Since(lastSeenAt) > executionLockTTL {
				// Expired lock - reclaim it
				meta, _ := marshalJSON(metadata)
				_, err := r.db.Exec(ctx, `
					UPDATE task_execution_keys
					SET attempts = attempts + 1, last_seen_at = NOW(), status = 'in_progress', metadata = $3
					WHERE task_id = $1 AND execution_key = $2
				`, taskID, executionKey, meta)
				return false, err
			}
			_, _ = r.db.Exec(ctx, `UPDATE task_execution_keys SET attempts = attempts + 1, last_seen_at = NOW() WHERE task_id = $1 AND execution_key = $2`, taskID, executionKey)
			return true, nil
		default:
			meta, _ := marshalJSON(metadata)
			_, err := r.db.Exec(ctx, `
				UPDATE task_execution_keys
				SET attempts = attempts + 1, last_seen_at = NOW(), status = 'in_progress', metadata = $3
				WHERE task_id = $1 AND execution_key = $2
			`, taskID, executionKey, meta)
			return false, err
		}
	}
	if err != pgx.ErrNoRows {
		return false, err
	}

	meta, _ := marshalJSON(metadata)
	_, err = r.db.Exec(ctx, `
		INSERT INTO task_execution_keys (task_id, execution_key, status, metadata, first_seen_at, last_seen_at, attempts)
		VALUES ($1, $2, 'in_progress', $3, NOW(), NOW(), 1)
	`, taskID, executionKey, meta)
	return false, err
}

func (r *advancedOutreachRepository) CompleteTaskExecution(ctx context.Context, taskID uuid.UUID, executionKey, status string, metadata map[string]interface{}) error {
	meta, _ := marshalJSON(metadata)
	_, err := r.db.Exec(ctx, `
		UPDATE task_execution_keys
		SET status = $3, metadata = $4, last_seen_at = NOW()
		WHERE task_id = $1 AND execution_key = $2
	`, taskID, executionKey, status, meta)
	return err
}

func (r *advancedOutreachRepository) CreateTaskDeadLetter(ctx context.Context, item *models.TaskDeadLetter) error {
	payload, err := marshalJSON(item.Payload)
	if err != nil {
		return err
	}
	query := `
		INSERT INTO task_dead_letters (task_id, task_type, payload, last_error, attempts, max_attempts, status, next_retry_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
	`
	if item.MaxAttempts <= 0 {
		item.MaxAttempts = 5
	}
	if item.Status == "" {
		item.Status = "pending"
	}
	_, err = r.db.Exec(ctx, query, item.TaskID, item.TaskType, payload, item.LastError, item.Attempts, item.MaxAttempts, item.Status, item.NextRetryAt)
	return err
}

func (r *advancedOutreachRepository) ListTaskDeadLetters(ctx context.Context, organizationID uuid.UUID, status string, limit int) ([]models.TaskDeadLetter, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `
		SELECT d.id, d.task_id, d.task_type, d.payload, d.last_error, d.attempts, d.max_attempts, d.status, d.next_retry_at, d.replayed_at, d.created_at, d.updated_at
		FROM task_dead_letters d
		JOIN tasks t ON t.id = d.task_id
		JOIN email_accounts ea ON ea.id = t.email_account_id
		WHERE ea.organization_id = $1
		  AND ($2 = '' OR d.status = $2)
		ORDER BY d.updated_at DESC
		LIMIT $3
	`
	rows, err := r.db.Query(ctx, query, organizationID, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.TaskDeadLetter, 0, limit)
	for rows.Next() {
		var d models.TaskDeadLetter
		var payload []byte
		if err := rows.Scan(
			&d.ID,
			&d.TaskID,
			&d.TaskType,
			&payload,
			&d.LastError,
			&d.Attempts,
			&d.MaxAttempts,
			&d.Status,
			&d.NextRetryAt,
			&d.ReplayedAt,
			&d.CreatedAt,
			&d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &d.Payload)
		}
		if d.Payload == nil {
			d.Payload = map[string]interface{}{}
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *advancedOutreachRepository) GetTaskDeadLetter(ctx context.Context, id uuid.UUID, organizationID uuid.UUID) (*models.TaskDeadLetter, error) {
	query := `
		SELECT d.id, d.task_id, d.task_type, d.payload, d.last_error, d.attempts, d.max_attempts, d.status, d.next_retry_at, d.replayed_at, d.created_at, d.updated_at
		FROM task_dead_letters d
		JOIN tasks t ON t.id = d.task_id
		JOIN email_accounts ea ON ea.id = t.email_account_id
		WHERE d.id = $1 AND ea.organization_id = $2
	`
	var d models.TaskDeadLetter
	var payload []byte
	if err := r.db.QueryRow(ctx, query, id, organizationID).Scan(
		&d.ID,
		&d.TaskID,
		&d.TaskType,
		&payload,
		&d.LastError,
		&d.Attempts,
		&d.MaxAttempts,
		&d.Status,
		&d.NextRetryAt,
		&d.ReplayedAt,
		&d.CreatedAt,
		&d.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &d.Payload)
	}
	if d.Payload == nil {
		d.Payload = map[string]interface{}{}
	}
	return &d, nil
}

func (r *advancedOutreachRepository) MarkTaskDeadLetterReplayed(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE task_dead_letters
		SET status = 'replayed', replayed_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, id)
	return err
}

func (r *advancedOutreachRepository) CreateReplyIntent(ctx context.Context, record *models.ReplyIntentRecord) error {
	metadata, err := marshalJSON(record.Metadata)
	if err != nil {
		return err
	}
	query := `
		INSERT INTO reply_intents (
			organization_id, contact_email, campaign_id, task_id, intent, confidence, action_taken, metadata, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
	`
	_, err = r.db.Exec(ctx, query,
		record.OrganizationID,
		strings.ToLower(strings.TrimSpace(record.ContactEmail)),
		record.CampaignID,
		record.TaskID,
		record.Intent,
		record.Confidence,
		record.ActionTaken,
		metadata,
	)
	return err
}

func (r *advancedOutreachRepository) CreatePreflightReport(ctx context.Context, report *models.PreflightReport) error {
	checks, err := marshalJSON(report.Checks)
	if err != nil {
		return err
	}
	recommendations, err := marshalJSON(report.Recommendations)
	if err != nil {
		return err
	}
	query := `
		INSERT INTO preflight_reports (organization_id, campaign_id, passed, score, checks, recommendations, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
	`
	_, err = r.db.Exec(ctx, query, report.OrganizationID, report.CampaignID, report.Passed, report.Score, checks, recommendations)
	return err
}

func (r *advancedOutreachRepository) GetABVariantStats(ctx context.Context, campaignID uuid.UUID) ([]models.ABVariantStats, error) {
	query := `
		SELECT
			v.id, v.name,
			COUNT(a.contact_id) AS total_sent,
			COUNT(a.opened_at) AS opened,
			COUNT(a.clicked_at) AS clicked,
			COUNT(a.replied_at) AS replied,
			COUNT(a.bounced_at) AS bounced
		FROM campaign_ab_variants v
		LEFT JOIN campaign_ab_assignments a ON a.variant_id = v.id AND a.campaign_id = v.campaign_id
		WHERE v.campaign_id = $1 AND v.is_control = false
		GROUP BY v.id, v.name
		ORDER BY v.created_at
	`
	rows, err := r.db.Query(ctx, query, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []models.ABVariantStats
	for rows.Next() {
		var s models.ABVariantStats
		if err := rows.Scan(&s.VariantID, &s.VariantName, &s.TotalSent, &s.Opened, &s.Clicked, &s.Replied, &s.Bounced); err != nil {
			return nil, err
		}
		if s.TotalSent > 0 {
			s.OpenRate = float64(s.Opened) / float64(s.TotalSent) * 100
			s.ClickRate = float64(s.Clicked) / float64(s.TotalSent) * 100
			s.ReplyRate = float64(s.Replied) / float64(s.TotalSent) * 100
			s.BounceRate = float64(s.Bounced) / float64(s.TotalSent) * 100
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

func (r *advancedOutreachRepository) ListRetryableDeadLetters(ctx context.Context, limit int) ([]models.TaskDeadLetter, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	query := `
		SELECT id, task_id, task_type, payload, last_error, attempts, max_attempts, status, next_retry_at, replayed_at, created_at, updated_at
		FROM task_dead_letters
		WHERE status = 'pending'
		  AND next_retry_at IS NOT NULL
		  AND next_retry_at <= NOW()
		  AND attempts < max_attempts
		ORDER BY next_retry_at ASC
		LIMIT $1
	`
	rows, err := r.db.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.TaskDeadLetter
	for rows.Next() {
		var d models.TaskDeadLetter
		var payload []byte
		if err := rows.Scan(&d.ID, &d.TaskID, &d.TaskType, &payload, &d.LastError, &d.Attempts, &d.MaxAttempts, &d.Status, &d.NextRetryAt, &d.ReplayedAt, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &d.Payload)
		}
		items = append(items, d)
	}
	return items, rows.Err()
}

func (r *advancedOutreachRepository) IncrementDeadLetterAttempt(ctx context.Context, id uuid.UUID, nextRetryAt *time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE task_dead_letters
		SET attempts = attempts + 1, next_retry_at = $2, updated_at = NOW()
		WHERE id = $1
	`, id, nextRetryAt)
	return err
}

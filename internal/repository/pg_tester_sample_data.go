package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/models"
)

var ErrTesterWorkspaceInactive = errors.New("sample data requires an active dedicated Test workspace")

// testerWorkspaceActiveSQL is the seeding authority over workspace o and owner u: an active tester
// and an unexpired managed Test grant, whatever state the owner's initial password is in.
func testerWorkspaceActiveSQL(planParam string) string {
	return `o.category = 'test' AND u.login_code_exempt AND EXISTS (
		SELECT 1 FROM subscriptions s WHERE s.organization_id = o.id
		  AND s.managed_plan_id = ` + planParam + ` AND s.managed_at IS NOT NULL AND s.managed_until > now()
	)`
}

func (r *organizationRepository) SeedTesterWorkspace(ctx context.Context, orgID, adminID uuid.UUID, ip, userAgent string) (*models.TesterSampleData, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var ownerID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT o.owner_user_id FROM organizations o JOIN users u ON u.id = o.owner_user_id
		WHERE o.id = $1 AND `+testerWorkspaceActiveSQL("$2")+`
		FOR UPDATE OF o, u`, orgID, models.TestPlanID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTesterWorkspaceInactive
	}
	if err != nil {
		return nil, err
	}

	result := &models.TesterSampleData{OrganizationID: orgID}
	// The workspace lock serializes retries; the audit marker preserves edits and deletions.
	err = tx.QueryRow(ctx, `SELECT created_at FROM admin_audit_logs
		WHERE target_type = 'organization' AND target_id = $1 AND action = 'seed_tester_workspace'
		ORDER BY created_at LIMIT 1`, orgID).Scan(&result.SeededAt)
	if err == nil {
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if err := seedTesterSampleData(ctx, tx, orgID, ownerID); err != nil {
		return nil, err
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO admin_audit_logs (admin_user_id, action, target_type, target_id, details, ip_address, user_agent)
		VALUES ($1, 'seed_tester_workspace', 'organization', $2,
			'{"dataset_version":1,"contacts":6,"contact_lists":1,"campaigns":1,"sequence_steps":2,"templates":2,"pipelines":1,"deals":2,"tasks":1,"notes":1}', $3, $4)
		RETURNING created_at`, adminID, orgID, ip, userAgent).Scan(&result.SeededAt)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	result.Created = true
	return result, nil
}

func seedTesterSampleData(ctx context.Context, tx pgx.Tx, orgID, ownerID uuid.UUID) error {
	batch := &pgx.Batch{}
	listID, campaignID, pipelineID := uuid.New(), uuid.New(), uuid.New()
	batch.Queue(`INSERT INTO categories (id, organization_id, user_id, title, color, position)
		VALUES ($1, $2, $3, 'Sample: reviewer contacts', '#38bdf8',
			(SELECT COALESCE(MAX(position), -1) + 1 FROM categories WHERE organization_id = $2))`, listID, orgID, ownerID)

	contacts := []struct{ first, last, company, email string }{
		{"Avery", "Sample", "Sample North Studio", "avery@review.invalid"},
		{"Jordan", "Sample", "Sample Meadow Labs", "jordan@review.invalid"},
		{"Morgan", "Sample", "Sample Harbor Works", "morgan@review.invalid"},
		{"Casey", "Sample", "Sample Cedar Team", "casey@review.invalid"},
		{"Riley", "Sample", "Sample Valley Studio", "riley@review.invalid"},
		{"Taylor", "Sample", "Sample Brook Labs", "taylor@review.invalid"},
	}
	contactIDs := make([]uuid.UUID, len(contacts))
	for i, contact := range contacts {
		id := uuid.New()
		contactIDs[i] = id
		batch.Queue(`INSERT INTO contacts (id, organization_id, user_id, first_name, last_name, email, company, phone, custom_fields, subscribed)
			VALUES ($1, $2, $3, $4, $5, $6, $7, '', '{"sample_data":"true"}', false)`,
			id, orgID, ownerID, contact.first, contact.last, contact.email, contact.company)
		batch.Queue(`INSERT INTO contact_categories (contact_id, category_id) VALUES ($1, $2)`, id, listID)
	}

	batch.Queue(`INSERT INTO campaigns (id, organization_id, user_id, name, description, status,
		stop_on_reply, open_tracking, link_tracking, risky_emails, days, timezone, updated_at, created_at)
		VALUES ($1, $2, $3, 'Sample: reviewer outreach',
			'Fictional sample data. Contacts are unsubscribed and use non-deliverable .invalid addresses. No mailbox is assigned.',
			'draft', true, false, false, false, 31, 'UTC', now(), now())`, campaignID, orgID, ownerID)
	for i, id := range contactIDs {
		batch.Queue(`INSERT INTO campaign_leads (campaign_id, contact_id, position) VALUES ($1, $2, $3)`, campaignID, id, i)
	}
	steps := []struct{ name, subject, plain, html string }{
		{"Sample introduction", "Sample introduction for {{.Company}}", "Hi {{.FirstName}},\n\nThis is fictional content for reviewing the editor. No email has been sent.", "<p>Hi {{.FirstName}},</p><p>This is fictional content for reviewing the editor. No email has been sent.</p>"},
		{"Sample follow-up", "Sample follow-up", "Hi {{.FirstName}},\n\nThis is a second draft step for reviewing the sequence.", "<p>Hi {{.FirstName}},</p><p>This is a second draft step for reviewing the sequence.</p>"},
	}
	stepIDs := []uuid.UUID{uuid.New(), uuid.New()}
	for i, step := range steps {
		batch.Queue(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_plain, body_html, wait_after, position, x, y)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 0, $10)`, stepIDs[i], campaignID, orgID,
			step.name, step.subject, step.plain, step.html, i*10, i, i*240)
		batch.Queue(`INSERT INTO reply_templates (organization_id, user_id, name, subject, body_plain, body_html, position)
			VALUES ($1, $2, $3, $4, $5, $6,
				(SELECT COALESCE(MAX(position), -1) + 1 FROM reply_templates WHERE organization_id = $1))`,
			orgID, ownerID, "Sample: "+step.name, step.subject, step.plain, step.html)
	}

	batch.Queue(`INSERT INTO pipelines (id, organization_id, name, position)
		VALUES ($1, $2, 'Sample: review pipeline',
			(SELECT COALESCE(MAX(position), -1) + 1 FROM pipelines WHERE organization_id = $2))`, pipelineID, orgID)
	stageIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	for i, stage := range []struct{ name, color string }{{"New", "#38bdf8"}, {"Qualified", "#a855f7"}, {"Won", "#10b981"}} {
		batch.Queue(`INSERT INTO pipeline_stages (id, pipeline_id, name, color, position) VALUES ($1, $2, $3, $4, $5)`,
			stageIDs[i], pipelineID, stage.name, stage.color, i)
	}
	dealIDs := []uuid.UUID{uuid.New(), uuid.New()}
	for i, id := range dealIDs {
		batch.Queue(`INSERT INTO deals (id, organization_id, pipeline_id, stage_id, contact_id, name, value, currency, assigned_to)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'USD', $8)`, id, orgID, pipelineID, stageIDs[i],
			contactIDs[i], "Sample: "+contacts[i].company+" review", (i+1)*1000, ownerID)
	}
	batch.Queue(`INSERT INTO crm_tasks (organization_id, contact_id, deal_id, assigned_to, created_by, title, description)
		VALUES ($1, $2, $3, $4, $4, 'Sample: inspect the deal', 'Fictional review task. No email or external action is scheduled.')`,
		orgID, contactIDs[0], dealIDs[0], ownerID)
	batch.Queue(`INSERT INTO contact_notes (contact_id, organization_id, user_id, content)
		VALUES ($1, $2, $3, 'Fictional sample contact for product review. Do not replace this with customer data.')`,
		contactIDs[0], orgID, ownerID)

	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}
	return connectLinearSequenceTx(ctx, tx, stepIDs)
}

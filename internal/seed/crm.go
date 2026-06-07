package seed

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedCRMPipelines(ctx context.Context, pool *pgxpool.Pool, _ *Result) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO pipelines (id, organization_id, name, position, created_at, updated_at)
		VALUES ($1, $2, 'Sales pipeline', 0, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, updated_at = NOW()
	`, PipelineSalesID, OrgAcmeID)
	if err != nil {
		return err
	}

	type stage struct {
		id    uuid.UUID
		name  string
		color string
		pos   int
	}
	stages := []stage{
		{StageNewID, "New", "#3b82f6", 0},
		{StageQualID, "Qualified", "#a855f7", 1},
		{StageDemoID, "Demo booked", "#f59e0b", 2},
		{StageWonID, "Won", "#10b981", 3},
		{StageLostID, "Lost", "#ef4444", 4},
	}
	for _, s := range stages {
		_, err := pool.Exec(ctx, `
			INSERT INTO pipeline_stages (id, pipeline_id, name, color, position, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, color = EXCLUDED.color, position = EXCLUDED.position, updated_at = NOW()
		`, s.id, PipelineSalesID, s.name, s.color, s.pos)
		if err != nil {
			return err
		}
	}
	return nil
}

// seedCRMTaskTypes gives the demo orgs the same starter task types a real org
// gets at creation, so the tasks UI has Call / Email / Meeting out of the box.
func seedCRMTaskTypes(ctx context.Context, pool *pgxpool.Pool, _ *Result) error {
	defaults := []struct {
		name  string
		color string
	}{
		{"Call", "#8b5cf6"},
		{"Email", "#0ea5e9"},
		{"Meeting", "#f59e0b"},
	}
	for _, orgID := range []uuid.UUID{OrgAcmeID, OrgGlobexID} {
		for i, d := range defaults {
			if _, err := pool.Exec(ctx, `
				INSERT INTO crm_task_types (organization_id, name, color, position)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (organization_id, name) DO NOTHING
			`, orgID, d.name, d.color, i); err != nil {
				return err
			}
		}
	}
	return nil
}

func seedCRMDeals(ctx context.Context, pool *pgxpool.Pool, _ *Result) error {
	type deal struct {
		id      uuid.UUID
		stage   uuid.UUID
		name    string
		value   float64
		status  string
		won     bool
		contact uuid.UUID
	}
	deals := []deal{
		{DealAcmeBigID, StageDemoID, "Northwind - team-wide rollout", 24_000, "open", false, contactID(0x01, 1)},
		{DealAcmeWonID, StageWonID, "Initech - pilot extension", 6_000, "won", true, contactID(0x01, 1)},
		// Deals tied to other contacts that also appear in the unibox, so opening
		// their threads shows live pipeline in the CRM panel — not just Aiden's.
		{uuid.MustParse("0000000d-0000-0000-0000-000000000003"), StageDemoID, "Vandelay - list hygiene retainer", 9_000, "open", false, contactID(0x01, 7)},
		{uuid.MustParse("0000000d-0000-0000-0000-000000000004"), StageDemoID, "Hooli - deliverability pilot", 15_000, "open", false, contactID(0x01, 4)},
	}
	for _, d := range deals {
		wonAt := "NULL"
		if d.won {
			wonAt = "NOW() - INTERVAL '7 days'"
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO deals (id, organization_id, pipeline_id, stage_id, contact_id, name, value, currency, status, won_at, assigned_to, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'USD', $8, `+wonAt+`, $9, NOW(), NOW())
			ON CONFLICT (id) DO UPDATE SET stage_id = EXCLUDED.stage_id, status = EXCLUDED.status, updated_at = NOW()
		`, d.id, OrgAcmeID, PipelineSalesID, d.stage, d.contact, d.name, d.value, d.status, UserOwnerID)
		if err != nil {
			return err
		}
	}
	return nil
}

func seedCRMTasks(ctx context.Context, pool *pgxpool.Pool, _ *Result) error {
	type task struct {
		id          uuid.UUID
		assignedTo  uuid.UUID
		title       string
		description string
		priority    string
		status      string
		dealID      uuid.UUID
	}
	tasks := []task{
		{CRMTask1ID, UserOwnerID, "Send proposal to Northwind", "Use the v2 template.", "high", "in_progress", DealAcmeBigID},
		{CRMTask2ID, UserManagerID, "Schedule QBR with Initech", "Q2 quarterly business review.", "medium", "pending", DealAcmeWonID},
	}
	for _, t := range tasks {
		_, err := pool.Exec(ctx, `
			INSERT INTO crm_tasks (id, organization_id, contact_id, deal_id, assigned_to, created_by, title, description, due_date, priority, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW() + INTERVAL '3 days', $9, $10, NOW(), NOW())
			ON CONFLICT (id) DO UPDATE SET title = EXCLUDED.title, status = EXCLUDED.status, updated_at = NOW()
		`, t.id, OrgAcmeID, contactID(0x01, 1), t.dealID, t.assignedTo, UserOwnerID, t.title, t.description, t.priority, t.status)
		if err != nil {
			return err
		}
	}

	// One note attached to a Northwind contact.
	_, err := pool.Exec(ctx, `
		INSERT INTO contact_notes (id, contact_id, organization_id, user_id, content, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'Prefers async email over calls. Asked us to revisit in Q2.', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING
	`, uuid.MustParse("00000000-0000-0000-0000-000000000b10"), contactID(0x01, 1), OrgAcmeID, UserOwnerID)
	return err
}

func seedContactActivity(ctx context.Context, pool *pgxpool.Pool, _ *Result) error {
	rows := []struct {
		contact  uuid.UUID
		actType  string
		metadata string
	}{
		{contactID(0x01, 1), "email_sent", `{"campaign":"Q1 Outreach","step":1}`},
		{contactID(0x01, 1), "email_opened", `{"campaign":"Q1 Outreach","step":1}`},
		{contactID(0x01, 1), "email_replied", `{"campaign":"Q1 Outreach","step":1}`},
		{contactID(0x01, 2), "email_sent", `{"campaign":"Q1 Outreach","step":1}`},
		{contactID(0x01, 2), "email_opened", `{"campaign":"Q1 Outreach","step":1}`},
		{contactID(0x01, 3), "email_sent", `{"campaign":"Q1 Outreach","step":1}`},
		{contactID(0x01, 1), "deal_created", `{"deal":"Northwind - team-wide rollout"}`},
		{contactID(0x01, 1), "deal_stage_changed", `{"from":"Qualified","to":"Demo booked"}`},
	}
	for _, r := range rows {
		_, err := pool.Exec(ctx, `
			INSERT INTO contact_activities (id, contact_id, organization_id, user_id, activity_type, metadata, created_at)
			VALUES (gen_random_uuid(), $1, $2, $3, $4::activity_type, $5::jsonb, NOW())
		`, r.contact, OrgAcmeID, UserOwnerID, r.actType, r.metadata)
		if err != nil {
			return err
		}
	}
	return nil
}

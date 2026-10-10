package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveOrganizationScopedCountsIntersectGrantsAndOwnership(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := context.Background()
	user, org, foreignOrg := uuid.New(), uuid.New(), uuid.New()
	active, paused, outside, foreign := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	mailbox, deniedMailbox, foreignMailbox := uuid.New(), uuid.New(), uuid.New()
	sharedContact, scopedContact, outsideContact, foreignContact := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	orgs := []uuid.UUID{org, foreignOrg}
	mailboxes := []uuid.UUID{mailbox, deniedMailbox, foreignMailbox}

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	t.Cleanup(func() {
		for _, step := range []struct {
			query string
			arg   any
		}{
			{`DELETE FROM campaigns WHERE organization_id = ANY($1)`, orgs},
			{`DELETE FROM tasks WHERE email_account_id = ANY($1)`, mailboxes},
			{`DELETE FROM contacts WHERE organization_id = ANY($1)`, orgs},
			{`DELETE FROM email_accounts WHERE id = ANY($1)`, mailboxes},
			{`DELETE FROM organizations WHERE id = ANY($1)`, orgs},
			{`DELETE FROM users WHERE id = $1`, user},
		} {
			if _, err := pool.Exec(context.Background(), step.query, step.arg); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})
	exec(`INSERT INTO users(id, first_name, last_name, email) VALUES($1, 'Counts', 'Scope', $2)`, user, user.String()+"@example.test")
	exec(`INSERT INTO organizations(id, name, owner_user_id) VALUES($1, 'Scope', $3), ($2, 'Foreign', $3)`, org, foreignOrg, user)
	for _, c := range []struct {
		id, org uuid.UUID
		status  string
	}{
		{active, org, "active"}, {paused, org, "paused"}, {outside, org, "active"}, {foreign, foreignOrg, "active"},
	} {
		exec(`INSERT INTO campaigns(id, user_id, organization_id, name, description, days, status, created_at, updated_at) VALUES($1, $2, $3, 'Counts', '', 62, $4, NOW(), NOW())`, c.id, user, c.org, c.status)
	}
	for _, m := range []struct{ id, org uuid.UUID }{{mailbox, org}, {deniedMailbox, org}, {foreignMailbox, foreignOrg}} {
		exec(`INSERT INTO email_accounts(id, user_id, organization_id, email, name, signature_plain, signature_html, provider) VALUES($1, $2, $3, $4, 'Scope', '', '', 'smtp_imap')`, m.id, user, m.org, m.id.String()+"@example.test")
	}
	for _, c := range []struct{ id, org uuid.UUID }{{sharedContact, org}, {scopedContact, org}, {outsideContact, org}, {foreignContact, foreignOrg}} {
		exec(`INSERT INTO contacts(id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, created_at, updated_at) VALUES($1, $2, $3, $4, 'Lead', '', '', '', '{}', NOW(), NOW())`, c.id, user, c.org, c.id.String()+"@example.test")
	}
	exec(`INSERT INTO campaign_leads(campaign_id, contact_id, position) VALUES($1, $5, 0), ($2, $5, 0), ($1, $6, 1), ($3, $7, 0), ($4, $8, 0)`, active, paused, outside, foreign, sharedContact, scopedContact, outsideContact, foreignContact)
	for _, send := range []struct {
		campaign, mailbox     uuid.UUID
		status, message, kind string
		days                  int
	}{
		{active, mailbox, "completed", "<active@example.test>", "campaign", 0},
		{paused, mailbox, "completed", "<paused@example.test>", "campaign", 0},
		{active, deniedMailbox, "completed", "<denied-mailbox@example.test>", "campaign", 0},
		{outside, mailbox, "completed", "<denied-campaign@example.test>", "campaign", 0},
		{foreign, foreignMailbox, "completed", "<foreign@example.test>", "campaign", 0},
		{active, mailbox, "completed", "<yesterday@example.test>", "campaign", -1},
		{active, mailbox, "pending", "<pending@example.test>", "campaign", 0},
		{active, mailbox, "completed", "", "campaign", 0},
		{active, mailbox, "completed", "<warmup@example.test>", "warmup", 0},
	} {
		id := uuid.New()
		exec(`INSERT INTO tasks(id, task_type, email_account_id, status, message_id, completed_at) VALUES($1, $2, $3, $4, $5, NOW()+$6::int*INTERVAL '1 day')`, id, send.kind, send.mailbox, send.status, send.message, send.days)
		exec(`INSERT INTO campaign_tasks(task_id, campaign_id) VALUES($1, $2)`, id, send.campaign)
	}
	step, dispatch := uuid.New(), uuid.New()
	exec(`INSERT INTO sequences(id, campaign_id, organization_id, name, subject, body_plain, body_html, wait_after, position, kind) VALUES($1, $2, $3, 'Email', '', '', '', 0, 1, 'email')`, step, active, org)
	exec(`INSERT INTO tasks(id, task_type, email_account_id, status, message_id, completed_at) VALUES($1, 'campaign', $2, 'completed', '', NOW())`, dispatch, mailbox)
	exec(`INSERT INTO campaign_tasks(task_id, campaign_id) VALUES($1, $2)`, dispatch, active)
	exec(`INSERT INTO campaign_contact_progress(campaign_id, contact_id, sequence_id, dispatch_task_id, dispatched_at) VALUES($1, $2, $3, $4, NOW())`, active, scopedContact, step, dispatch)

	r := NewOrganizationRepository(pool)
	for _, tc := range []struct {
		name  string
		scope *models.ResourceScope
		want  models.OrganizationCounts
	}{
		{"direct and foreign IDs", &models.ResourceScope{Campaigns: []uuid.UUID{active, paused, active, foreign}, Mailboxes: []uuid.UUID{mailbox, foreignMailbox}}, models.OrganizationCounts{TotalCampaigns: 2, ActiveCampaigns: 1, TotalContacts: 2, EmailAccounts: 1, EmailsSentToday: 3}},
		{"nil grant lists", &models.ResourceScope{}, models.OrganizationCounts{}},
		{"empty grant lists", &models.ResourceScope{Campaigns: []uuid.UUID{}, Mailboxes: []uuid.UUID{}}, models.OrganizationCounts{}},
		{"campaigns only", &models.ResourceScope{Campaigns: []uuid.UUID{active}}, models.OrganizationCounts{TotalCampaigns: 1, ActiveCampaigns: 1, TotalContacts: 2}},
		{"mailboxes only", &models.ResourceScope{Mailboxes: []uuid.UUID{mailbox}}, models.OrganizationCounts{EmailAccounts: 1}},
		{"foreign grants", &models.ResourceScope{Campaigns: []uuid.UUID{foreign}, Mailboxes: []uuid.UUID{foreignMailbox}}, models.OrganizationCounts{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			counts, err := r.GetScopedOrganizationCounts(ctx, org, tc.scope)
			if err != nil || counts == nil || *counts != tc.want {
				t.Fatalf("counts=%+v err=%v; want %+v", counts, err, tc.want)
			}
		})
	}
	if counts, err := r.GetScopedOrganizationCounts(ctx, org, nil); err == nil || counts != nil {
		t.Fatalf("unresolved scope returned %+v err=%v", counts, err)
	}
}

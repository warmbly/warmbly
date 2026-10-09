package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveUsageOverviewCountsTheSelectedOrganization(t *testing.T) {
	handle, pool := liveContactDB(t)
	ctx := context.Background()
	userID, orgID := uuid.New(), uuid.New()
	accountID := uuid.New()
	campaignID, contactID, pendingContactID := uuid.New(), uuid.New(), uuid.New()
	emailStepID, waitStepID := uuid.New(), uuid.New()

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email)
	      VALUES ($1, 'Usage', 'Scope', $2)`, userID, "usage-scope-"+uuid.NewString()+"@example.test")
	exec(`INSERT INTO organizations (id, name, owner_user_id)
	      VALUES ($1, 'Usage scope', $2)`, orgID, userID)
	exec(`INSERT INTO email_accounts
	        (id, user_id, organization_id, email, name, signature_plain, signature_html, provider)
	      VALUES ($1, $2, $3, $4, 'Usage scope', '', '', 'smtp_imap')`,
		accountID, userID, orgID, "usage-scope-mailbox-"+uuid.NewString()+"@example.test")
	exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, days, status, created_at, updated_at)
	      VALUES ($1, $2, $3, 'Usage scope', '', 62, 'active', NOW(), NOW())`, campaignID, userID, orgID)
	exec(`INSERT INTO contacts
	        (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, subscribed, created_at, updated_at)
	      VALUES ($1, $3, $4, $5, 'Usage', 'Scope', '', '', '{}'::jsonb, true, NOW(), NOW()),
	             ($2, $3, $4, $6, 'Pending', 'Scope', '', '', '{}'::jsonb, true, NOW(), NOW())`,
		contactID, pendingContactID, userID, orgID,
		"usage-scope-contact-"+uuid.NewString()+"@example.test",
		"usage-scope-pending-"+uuid.NewString()+"@example.test")
	exec(`INSERT INTO campaign_leads (campaign_id, contact_id, position)
	      VALUES ($1, $2, 0), ($1, $3, 1)`, campaignID, contactID, pendingContactID)
	for i, step := range []struct {
		id   uuid.UUID
		kind string
	}{{emailStepID, "email"}, {waitStepID, "wait"}} {
		exec(`INSERT INTO sequences
		        (id, campaign_id, organization_id, name, subject, body_plain, body_html, wait_after, position, kind, created_at)
		      VALUES ($1, $2, $3, $4, '', '', '', 0, $5, $6, NOW())`,
			step.id, campaignID, orgID, "Step "+step.kind, i+1, step.kind)
	}
	exec(`INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at)
	      VALUES ($1, $2, $3, NOW()), ($1, $2, $4, NOW())`, campaignID, contactID, emailStepID, waitStepID)

	t.Cleanup(func() {
		for _, step := range []struct {
			query string
			arg   uuid.UUID
		}{
			{`DELETE FROM campaigns WHERE organization_id = $1`, orgID},
			{`DELETE FROM contacts WHERE organization_id = $1`, orgID},
			{`DELETE FROM email_accounts WHERE organization_id = $1`, orgID},
			{`DELETE FROM organizations WHERE id = $1`, orgID},
			{`DELETE FROM users WHERE id = $1`, userID},
		} {
			if _, err := pool.Exec(context.Background(), step.query, step.arg); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})

	repo := &analyticsRepository{DB: handle}
	accounts, xerr := repo.GetEmailAccountCounts(ctx, orgID)
	if xerr != nil {
		t.Fatalf("GetEmailAccountCounts: %v", xerr)
	}
	campaigns, xerr := repo.GetCampaignCounts(ctx, orgID, time.Now().Add(-7*24*time.Hour), time.Now())
	if xerr != nil {
		t.Fatalf("GetCampaignCounts: %v", xerr)
	}
	contacts, xerr := repo.GetContactCounts(ctx, orgID)
	if xerr != nil {
		t.Fatalf("GetContactCounts: %v", xerr)
	}
	if accounts.Total != 1 || campaigns.Total != 1 || contacts.Total != 2 {
		t.Fatalf("usage totals = accounts %d, campaigns %d, contacts %d; want 1/1/2 for the selected organization",
			accounts.Total, campaigns.Total, contacts.Total)
	}
	if campaigns.EmailsSent != 1 {
		t.Fatalf("period emails_sent = %d, want the one email step (the wait step sends nothing)", campaigns.EmailsSent)
	}

	from, to := time.Now().Add(-24*time.Hour), time.Now().Add(24*time.Hour)
	overall, xerr := repo.GetDashboardOverallStats(ctx, orgID, from, to, nil, nil)
	if xerr != nil {
		t.Fatalf("GetDashboardOverallStats: %v", xerr)
	}
	if overall.TotalEmailsSent != 1 {
		t.Fatalf("dashboard total sent = %d, want the one email step", overall.TotalEmailsSent)
	}
	daily, xerr := repo.GetDashboardDailyTrend(ctx, orgID, from, to, nil)
	if xerr != nil {
		t.Fatalf("GetDashboardDailyTrend: %v", xerr)
	}
	if len(daily) != 1 || daily[0].Sent != 1 {
		t.Fatalf("dashboard daily trend = %+v, want one email sent", daily)
	}
	top, xerr := repo.GetTopCampaigns(ctx, orgID, from, to, 10, "emails_sent", nil)
	if xerr != nil {
		t.Fatalf("GetTopCampaigns: %v", xerr)
	}
	if len(top) != 1 || top[0].EmailsSent != 1 {
		t.Fatalf("top campaigns = %+v, want one email sent", top)
	}
	summary, xerr := repo.GetCampaignSummary(ctx, orgID, campaignID, nil)
	if xerr != nil {
		t.Fatalf("GetCampaignSummary: %v", xerr)
	}
	if summary.TotalContacts != 2 || summary.EmailsSent != 1 || summary.EmailsPending != 1 {
		t.Fatalf("campaign summary = %+v, want two enrolled contacts with one send and one planned send pending", summary)
	}
	campaignDaily, xerr := repo.GetCampaignDailyStats(ctx, campaignID, from, to)
	if xerr != nil {
		t.Fatalf("GetCampaignDailyStats: %v", xerr)
	}
	if len(campaignDaily) != 1 || campaignDaily[0].Sent != 1 {
		t.Fatalf("campaign daily stats = %+v, want one email sent", campaignDaily)
	}
	hourly, xerr := repo.GetCampaignHourlyStats(ctx, campaignID, time.Now().UTC())
	if xerr != nil {
		t.Fatalf("GetCampaignHourlyStats: %v", xerr)
	}
	if len(hourly) != 1 || hourly[0].Sent != 1 {
		t.Fatalf("campaign hourly stats = %+v, want one email sent", hourly)
	}
	comparison, xerr := repo.CompareCampaigns(ctx, orgID, []uuid.UUID{campaignID}, from, to)
	if xerr != nil {
		t.Fatalf("CompareCampaigns: %v", xerr)
	}
	if len(comparison.Campaigns) != 1 || comparison.Campaigns[0].EmailsSent != 1 {
		t.Fatalf("campaign comparison = %+v, want one email sent", comparison.Campaigns)
	}
	steps, xerr := repo.GetSequenceStats(ctx, campaignID, nil)
	if xerr != nil {
		t.Fatalf("GetSequenceStats: %v", xerr)
	}
	if len(steps) != 1 || steps[0].SequenceID != emailStepID || steps[0].EmailsSent != 1 {
		t.Fatalf("sequence stats = %+v, want only the email step", steps)
	}

	exec(`INSERT INTO warmup_pool_participants (pool_id, email_account_id, health_state)
	      VALUES ($1, $2, 'watch')`, models.WarmupPoolPremiumID, accountID)
	health, xerr := repo.GetAccountHealthSummary(ctx, orgID, nil)
	if xerr != nil {
		t.Fatalf("GetAccountHealthSummary watch: %v", xerr)
	}
	if health.WarningAccounts != 1 || health.ErrorAccounts != 0 {
		t.Fatalf("account health for watch state = %+v, want one warning", health)
	}
	exec(`UPDATE warmup_pool_participants SET health_state = 'throttled' WHERE email_account_id = $1`, accountID)
	health, xerr = repo.GetAccountHealthSummary(ctx, orgID, nil)
	if xerr != nil {
		t.Fatalf("GetAccountHealthSummary throttled: %v", xerr)
	}
	if health.WarningAccounts != 0 || health.ErrorAccounts != 1 {
		t.Fatalf("account health for throttled state = %+v, want one error", health)
	}
	exec(`DELETE FROM warmup_pool_participants WHERE email_account_id = $1`, accountID)

	exec(`INSERT INTO email_account_errors
	        (email_account_id, user_id, error_code, severity, title, message)
	      VALUES ($1, $2, 'usage-warning', 'WARNING', 'Warning', 'Warning'),
	             ($1, $2, 'usage-critical', 'CRITICAL', 'Critical', 'Critical')`, accountID, userID)
	health, xerr = repo.GetAccountHealthSummary(ctx, orgID, nil)
	if xerr != nil {
		t.Fatalf("GetAccountHealthSummary: %v", xerr)
	}
	if health.TotalAccounts != 1 || health.HealthyAccounts != 0 || health.WarningAccounts != 0 || health.ErrorAccounts != 1 {
		t.Fatalf("account health = %+v, want one account in the most severe bucket only", health)
	}
}

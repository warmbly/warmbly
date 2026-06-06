package seed

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func intPtr(v int) *int { return &v }

// seedDurations inserts the duration rows our seeded plans reference. The
// migration that adds the durations table doesn't seed it, so we do it here
// to keep the seed self-contained.
func seedDurations(ctx context.Context, pool *pgxpool.Pool, _ *Result) error {
	rows := []struct {
		id    string
		title string
	}{
		{DurationMonthID.String(), "month"},
		{DurationYearID.String(), "year"},
		{DurationLifetimeID.String(), "lifetime"},
	}
	for _, r := range rows {
		_, err := pool.Exec(ctx, `
			INSERT INTO durations (id, title) VALUES ($1, $2)
			ON CONFLICT (id) DO NOTHING
		`, r.id, r.title)
		if err != nil {
			return err
		}
	}
	return nil
}

func seedPlans(ctx context.Context, pool *pgxpool.Pool, r *Result) error {
	type plan struct {
		id                 uuid.UUID
		name               string
		maxContacts        int64
		dailyEmails        int
		ai                 bool
		accountLimit       int
		price              float64
		discounted         float64
		duration           uuid.UUID
		savings            int
		public             bool
		dedicatedWorkers   int
		dailyCampaignLimit *int
		maxCampaigns       *int
		maxActiveCampaigns *int
		maxTeamMembers     *int
		maxEmailAccounts   *int
		monthlyCredits     int
	}

	plans := []plan{
		{
			id: PlanFreeTrialID, name: "Free Trial", maxContacts: 100, dailyEmails: 20,
			ai: false, accountLimit: 2, price: 0, discounted: 0,
			duration: DurationMonthID, savings: 0, public: false,
			dedicatedWorkers: 0, dailyCampaignLimit: intPtr(20),
			maxCampaigns: intPtr(2), maxActiveCampaigns: intPtr(1),
			maxTeamMembers: intPtr(1), maxEmailAccounts: intPtr(2),
			monthlyCredits: 50,
		},
		{
			id: PlanStarterID, name: "Starter", maxContacts: 1_000, dailyEmails: 100,
			ai: false, accountLimit: 3, price: 29, discounted: 29,
			duration: DurationMonthID, savings: 0, public: true,
			dedicatedWorkers: 0, dailyCampaignLimit: intPtr(100),
			maxCampaigns: intPtr(5), maxActiveCampaigns: intPtr(2),
			maxTeamMembers: intPtr(2), maxEmailAccounts: intPtr(3),
			monthlyCredits: 250,
		},
		{
			id: PlanProMonthlyID, name: "Pro", maxContacts: 25_000, dailyEmails: 1_000,
			ai: true, accountLimit: 20, price: 99, discounted: 99,
			duration: DurationMonthID, savings: 0, public: true,
			dedicatedWorkers: 1, dailyCampaignLimit: intPtr(1_000),
			maxCampaigns: intPtr(50), maxActiveCampaigns: intPtr(20),
			maxTeamMembers: intPtr(10), maxEmailAccounts: intPtr(20),
			monthlyCredits: 2_000,
		},
		{
			id: PlanProYearlyID, name: "Pro (Annual)", maxContacts: 25_000, dailyEmails: 1_000,
			ai: true, accountLimit: 20, price: 1188, discounted: 990,
			duration: DurationYearID, savings: 17, public: true,
			dedicatedWorkers: 1, dailyCampaignLimit: intPtr(1_000),
			maxCampaigns: intPtr(50), maxActiveCampaigns: intPtr(20),
			maxTeamMembers: intPtr(10), maxEmailAccounts: intPtr(20),
			monthlyCredits: 2_000,
		},
		{
			id: PlanEnterpriseID, name: "Enterprise", maxContacts: 1_000_000, dailyEmails: 10_000,
			ai: true, accountLimit: 500, price: 0, discounted: 0,
			duration: DurationMonthID, savings: 0, public: false,
			dedicatedWorkers: 3, dailyCampaignLimit: intPtr(10_000),
			maxCampaigns: nil, maxActiveCampaigns: nil,
			maxTeamMembers: nil, maxEmailAccounts: nil,
			monthlyCredits: 25_000,
		},
	}

	for _, p := range plans {
		_, err := pool.Exec(ctx, `
			INSERT INTO plans (
				id, name, max_contacts, daily_emails, ai_generation, account_limit,
				price, discounted_price, duration_id, savings, public,
				dedicated_workers, daily_campaign_limit,
				max_campaigns, max_active_campaigns, max_team_members, max_email_accounts,
				monthly_credits
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				max_contacts = EXCLUDED.max_contacts,
				daily_emails = EXCLUDED.daily_emails,
				ai_generation = EXCLUDED.ai_generation,
				account_limit = EXCLUDED.account_limit,
				price = EXCLUDED.price,
				discounted_price = EXCLUDED.discounted_price,
				duration_id = EXCLUDED.duration_id,
				savings = EXCLUDED.savings,
				public = EXCLUDED.public,
				dedicated_workers = EXCLUDED.dedicated_workers,
				daily_campaign_limit = EXCLUDED.daily_campaign_limit,
				max_campaigns = EXCLUDED.max_campaigns,
				max_active_campaigns = EXCLUDED.max_active_campaigns,
				max_team_members = EXCLUDED.max_team_members,
				max_email_accounts = EXCLUDED.max_email_accounts,
				monthly_credits = EXCLUDED.monthly_credits,
				updated_at = NOW()
		`,
			p.id, p.name, p.maxContacts, p.dailyEmails, p.ai, p.accountLimit,
			p.price, p.discounted, p.duration, p.savings, p.public,
			p.dedicatedWorkers, p.dailyCampaignLimit,
			p.maxCampaigns, p.maxActiveCampaigns, p.maxTeamMembers, p.maxEmailAccounts,
			p.monthlyCredits,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

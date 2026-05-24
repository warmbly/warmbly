package seed

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MonFri is the days bitmask backend's bitmask.DefaultDays() produces (Mon..Fri).
// Duplicated here to avoid pulling in the runtime package — the bits are
// stable: 1<<1 | ... | 1<<5 = 62.
const monFri int16 = 62

func seedCampaigns(ctx context.Context, pool *pgxpool.Pool, r *Result) error {
	type camp struct {
		id     uuid.UUID
		orgID  uuid.UUID
		userID uuid.UUID
		name   string
		desc   string
		status string
	}
	campaigns := []camp{
		{CampaignAcmeActiveID, OrgAcmeID, UserOwnerID, "Q1 Outreach", "Cold outreach to mid-market SaaS in Q1.", "active"},
		{CampaignAcmeDraftID, OrgAcmeID, UserOwnerID, "Welcome series", "Onboarding drip for new signups.", "draft"},
		{CampaignGlobexID, OrgGlobexID, UserFounderID, "Friends and family", "Pilot outreach to founder network.", "draft"},
	}

	counts := map[string]int{}
	for _, c := range campaigns {
		_, err := pool.Exec(ctx, `
			INSERT INTO campaigns (
				id, user_id, organization_id, name, description, status,
				stop_on_reply, open_tracking, link_tracking, text_only,
				daily_limit, unsubscribe_header, risky_emails,
				cc_addr, bcc_addr, timezone, days, start_time, end_time,
				updated_at, created_at
			) VALUES (
				$1,$2,$3,$4,$5,$6,
				TRUE,TRUE,TRUE,FALSE,
				50,TRUE,FALSE,
				'{}','{}','Europe/London',$7,'08:00','18:00',
				NOW(), NOW()
			)
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				description = EXCLUDED.description,
				status = EXCLUDED.status,
				organization_id = EXCLUDED.organization_id,
				updated_at = NOW()
		`, c.id, c.userID, c.orgID, c.name, c.desc, c.status, monFri)
		if err != nil {
			return err
		}
		counts[c.orgID.String()]++
	}

	for i := range r.Organizations {
		if n, ok := counts[r.Organizations[i].ID]; ok {
			r.Organizations[i].Campaigns = n
		}
	}

	// Attach the active Acme campaign to the Inbox folder so the folder view
	// has something to show.
	_, err := pool.Exec(ctx, `
		INSERT INTO campaign_folders (campaign_id, folder_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, CampaignAcmeActiveID, FolderInboxID)
	return err
}

func seedSequences(ctx context.Context, pool *pgxpool.Pool, _ *Result) error {
	type step struct {
		id        uuid.UUID
		campaign  uuid.UUID
		orgID     uuid.UUID
		name      string
		subject   string
		bodyPlain string
		bodyHTML  string
		waitAfter int
	}
	steps := []step{
		{SequenceAcmeStep1ID, CampaignAcmeActiveID, OrgAcmeID,
			"Step 1 - intro", "Quick intro from {{firstName}}",
			"Hey {{firstName}},\n\nNoticed your team is hiring SDRs - quick idea.",
			"<p>Hey {{firstName}},</p><p>Noticed your team is hiring SDRs - quick idea.</p>", 0},
		{SequenceAcmeStep2ID, CampaignAcmeActiveID, OrgAcmeID,
			"Step 2 - bump", "Re: Quick intro from {{firstName}}",
			"Bumping this in case it got buried.",
			"<p>Bumping this in case it got buried.</p>", 3},
		{SequenceAcmeStep3ID, CampaignAcmeActiveID, OrgAcmeID,
			"Step 3 - break-up", "Closing the loop",
			"Closing the loop on my side - happy to circle back later.",
			"<p>Closing the loop on my side - happy to circle back later.</p>", 5},
		{SequenceDraftID, CampaignAcmeDraftID, OrgAcmeID,
			"Welcome", "Welcome to Acme",
			"Thanks for signing up.", "<p>Thanks for signing up.</p>", 0},
		{SequenceGlobexID, CampaignGlobexID, OrgGlobexID,
			"Pilot intro", "Trying something new",
			"Hey - testing a tool, mind a 5-min chat?",
			"<p>Hey - testing a tool, mind a 5-min chat?</p>", 0},
	}
	for _, s := range steps {
		_, err := pool.Exec(ctx, `
			INSERT INTO sequences (
				id, campaign_id, organization_id, name, subject,
				body_plain, body_html, body_sync, body_code, wait_after,
				updated_at, created_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,TRUE,FALSE,$8,NOW(),NOW())
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				subject = EXCLUDED.subject,
				body_plain = EXCLUDED.body_plain,
				body_html = EXCLUDED.body_html,
				wait_after = EXCLUDED.wait_after,
				updated_at = NOW()
		`, s.id, s.campaign, s.orgID, s.name, s.subject, s.bodyPlain, s.bodyHTML, s.waitAfter)
		if err != nil {
			return err
		}
	}
	return nil
}

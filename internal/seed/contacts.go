package seed

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type seededContact struct {
	id         uuid.UUID
	userID     uuid.UUID
	orgID      uuid.UUID
	firstName  string
	lastName   string
	email      string
	company    string
	phone      string
	campaignID uuid.UUID
}

func acmeContacts() []seededContact {
	rows := []struct {
		first, last, company string
	}{
		{"Aiden", "Park", "Northwind"},
		{"Beth", "Chen", "Initech"},
		{"Carlos", "Diaz", "Pied Piper"},
		{"Diana", "Patel", "Hooli"},
		{"Eli", "Brown", "Wonka"},
		{"Fiona", "Walsh", "Sterling Cooper"},
		{"Greg", "Mori", "Vandelay"},
		{"Hana", "Nakamura", "Massive Dynamic"},
	}
	out := make([]seededContact, 0, len(rows))
	for i, c := range rows {
		out = append(out, seededContact{
			id:         contactID(0x01, i+1),
			userID:     UserOwnerID,
			orgID:      OrgAcmeID,
			firstName:  c.first,
			lastName:   c.last,
			email:      fmt.Sprintf("%s.%s@%s.test", lower(c.first), lower(c.last), lower(c.company)),
			company:    c.company,
			phone:      "",
			campaignID: CampaignAcmeActiveID,
		})
	}
	return out
}

func globexContacts() []seededContact {
	rows := []struct {
		first, last, company string
	}{
		{"Ivan", "Petrov", "OldFriend Co"},
		{"Jules", "Renoir", "Pilot Studio"},
		{"Kim", "Tanaka", "AlphaLab"},
	}
	out := make([]seededContact, 0, len(rows))
	for i, c := range rows {
		out = append(out, seededContact{
			id:         contactID(0x02, i+1),
			userID:     UserFounderID,
			orgID:      OrgGlobexID,
			firstName:  c.first,
			lastName:   c.last,
			email:      fmt.Sprintf("%s.%s@%s.test", lower(c.first), lower(c.last), lower(c.company)),
			company:    c.company,
			phone:      "",
			campaignID: CampaignGlobexID,
		})
	}
	return out
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
		if c == ' ' {
			b[i] = '-'
		}
	}
	return string(b)
}

func seedContacts(ctx context.Context, pool *pgxpool.Pool, _ *Result) error {
	all := append([]seededContact{}, acmeContacts()...)
	all = append(all, globexContacts()...)

	for _, c := range all {
		_, err := pool.Exec(ctx, `
			INSERT INTO contacts (id, user_id, organization_id, first_name, last_name, email, company, phone, custom_fields, subscribed, updated_at, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'{}'::jsonb,TRUE,NOW(),NOW())
			ON CONFLICT (id) DO UPDATE SET
				first_name = EXCLUDED.first_name,
				last_name = EXCLUDED.last_name,
				email = EXCLUDED.email,
				company = EXCLUDED.company,
				updated_at = NOW()
		`, c.id, c.userID, c.orgID, c.firstName, c.lastName, c.email, c.company, c.phone)
		if err != nil {
			return err
		}
		_, err = pool.Exec(ctx, `
			INSERT INTO campaign_leads (campaign_id, contact_id)
			VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, c.campaignID, c.id)
		if err != nil {
			return err
		}
	}

	// Tag one Acme contact as VIP and put another in the Lead category so the
	// tag/category views aren't empty.
	_, err := pool.Exec(ctx, `
		INSERT INTO contact_categories (contact_id, category_id)
		VALUES ($1, $2) ON CONFLICT DO NOTHING
	`, contactID(0x01, 1), CategoryLeadID)
	return err
}

func seedCampaignProgress(ctx context.Context, pool *pgxpool.Pool, _ *Result) error {
	// Give the active Acme campaign a realistic-looking funnel: every contact
	// has step 1 sent, most opened, a couple clicked, one replied.
	contacts := acmeContacts()
	for i, c := range contacts {
		_, err := pool.Exec(ctx, `
			INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at, opened_at, clicked_at, replied_at)
			VALUES ($1, $2, $3,
				NOW() - INTERVAL '3 days',
				CASE WHEN $4 < 6 THEN NOW() - INTERVAL '3 days' + INTERVAL '15 minutes' ELSE NULL END,
				CASE WHEN $4 < 3 THEN NOW() - INTERVAL '3 days' + INTERVAL '20 minutes' ELSE NULL END,
				CASE WHEN $4 = 0 THEN NOW() - INTERVAL '2 days' ELSE NULL END
			)
			ON CONFLICT (campaign_id, contact_id, sequence_id) DO UPDATE SET
				sent_at = EXCLUDED.sent_at,
				opened_at = EXCLUDED.opened_at,
				clicked_at = EXCLUDED.clicked_at,
				replied_at = EXCLUDED.replied_at
		`, CampaignAcmeActiveID, c.id, SequenceAcmeStep1ID, i)
		if err != nil {
			return err
		}
	}
	return nil
}

func seedCampaignLogs(ctx context.Context, pool *pgxpool.Pool, _ *Result) error {
	type logRow struct {
		campaign uuid.UUID
		event    string
		message  string
	}
	rows := []logRow{
		{CampaignAcmeActiveID, "started", "Campaign moved to active"},
		{CampaignAcmeActiveID, "sequence_added", "Step 3 - break-up added"},
		{CampaignAcmeActiveID, "contact_replied", "aiden.park@northwind.test replied"},
		{CampaignAcmeDraftID, "created", "Campaign created"},
	}
	for _, r := range rows {
		_, err := pool.Exec(ctx, `
			INSERT INTO campaign_logs (id, campaign_id, event_type, message, metadata, created_at)
			VALUES (gen_random_uuid(), $1, $2, $3, '{}'::jsonb, NOW())
		`, r.campaign, r.event, r.message)
		if err != nil {
			return err
		}
	}
	return nil
}

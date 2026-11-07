package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// What SetLeadCC refuses. The service turns each into its own error code.
var (
	// ErrLeadCCContactNotFound is a copy that is not a contact of the workspace.
	ErrLeadCCContactNotFound = errors.New("a contact to copy was not found")
	// ErrLeadCCSelf is the lead copied on their own emails.
	ErrLeadCCSelf = errors.New("a lead cannot be copied on their own emails")
	// ErrLeadCCLeadIsCopied is a lead already copied on another lead's thread
	// in the campaign; their own emails are held, so copies would reach nobody.
	ErrLeadCCLeadIsCopied = errors.New("the lead is copied on another lead in this campaign")
	// ErrLeadCCHasCopies is a contact whose own lead copies others: holding it
	// would silently strand the people it copies.
	ErrLeadCCHasCopies = errors.New("a contact to copy has copies of their own in this campaign")
)

// CopiedLeadRef names the lead, and the step, a copied contact's reply answers.
type CopiedLeadRef struct {
	CampaignID uuid.UUID
	ContactID  uuid.UUID
	SequenceID uuid.UUID
}

// personalMailDomainsSQL never identify a company, so a shared one is not a
// reason to suggest two contacts are colleagues.
const personalMailDomainsSQL = `'gmail.com','googlemail.com','yahoo.com','yahoo.de','hotmail.com','hotmail.de','outlook.com','outlook.de','live.com','live.de','msn.com','aol.com','icloud.com','me.com','gmx.com','gmx.de','gmx.net','web.de','t-online.de','freenet.de','posteo.de','mailbox.org','proton.me','protonmail.com','mail.com','yandex.com'`

// leadCCStatusSQL derives models.LeadCCStatus* for a copied contact aliased c
// on the campaign_lead_cc row aliased x. cp is the bound campaign id.
func leadCCStatusSQL(cp string) string {
	return `CASE
		WHEN c.subscribed IS FALSE
		  OR recipient_suppressed((SELECT organization_id FROM campaigns WHERE id = ` + cp + `), c.email)
		THEN '` + models.LeadCCStatusUnsubscribed + `'
		WHEN x.bounced_at IS NOT NULL
		  OR EXISTS (SELECT 1 FROM campaign_contact_progress b WHERE b.contact_id = c.id AND b.bounced_at IS NOT NULL)
		THEN '` + models.LeadCCStatusBounced + `'
		WHEN ` + undeliverableClause(cp) + ` THEN '` + models.LeadCCStatusUndeliverable + `'
		ELSE '` + models.LeadCCStatusActive + `'
	END`
}

// leadCCSelectSQL lists one lead's copies in the order they were chosen. $1 is
// the campaign, $2 the lead.
func leadCCSelectSQL() string {
	return `
		SELECT c.id, c.email, c.first_name, c.last_name, c.company, ` + leadCCStatusSQL("$1") + `, x.bounced_at
		FROM campaign_lead_cc x
		JOIN contacts c ON c.id = x.cc_contact_id
		WHERE x.campaign_id = $1 AND x.contact_id = $2
		ORDER BY x.position, x.created_at`
}

// listLeadCC is shared by the campaign and contact repositories, so the send
// path and the drawer read one definition.
func listLeadCC(ctx context.Context, pool *pgxpool.Pool, campaignID, contactID uuid.UUID) ([]models.CampaignLeadCC, error) {
	rows, err := pool.Query(ctx, leadCCSelectSQL(), campaignID, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.CampaignLeadCC{}
	for rows.Next() {
		var cc models.CampaignLeadCC
		if err := rows.Scan(&cc.ContactID, &cc.Email, &cc.FirstName, &cc.LastName, &cc.Company, &cc.Status, &cc.BouncedAt); err != nil {
			return nil, err
		}
		out = append(out, cc)
	}
	return out, rows.Err()
}

func (r *campaignProgressRepository) ListLeadCC(ctx context.Context, campaignID, contactID uuid.UUID) ([]models.CampaignLeadCC, error) {
	return listLeadCC(ctx, r.db, campaignID, contactID)
}

func (r *campaignProgressRepository) SetLeadCC(ctx context.Context, orgID, campaignID, contactID uuid.UUID, ccIDs []uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// One writer per campaign, so two edits cannot build a chain of copies
	// that each checked against the other's snapshot.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('campaign_lead_cc:' || $1::text, 0))`, campaignID); err != nil {
		return err
	}

	var isLead bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM campaign_leads cl
			JOIN campaigns cam ON cam.id = cl.campaign_id AND cam.organization_id = $1
			WHERE cl.campaign_id = $2 AND cl.contact_id = $3
		)`, orgID, campaignID, contactID).Scan(&isLead); err != nil {
		return err
	}
	if !isLead {
		return ErrLeadNotInCampaign
	}

	if len(ccIDs) > 0 {
		for _, id := range ccIDs {
			if id == contactID {
				return ErrLeadCCSelf
			}
		}
		var found int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM contacts WHERE organization_id = $1 AND id = ANY($2::uuid[])`,
			orgID, ccIDs).Scan(&found); err != nil {
			return err
		}
		if found != len(ccIDs) {
			return ErrLeadCCContactNotFound
		}
		var leadIsCopied, ccHasCopies bool
		if err := tx.QueryRow(ctx, `
			SELECT
				EXISTS (SELECT 1 FROM campaign_lead_cc WHERE campaign_id = $1 AND cc_contact_id = $2),
				EXISTS (SELECT 1 FROM campaign_lead_cc WHERE campaign_id = $1 AND contact_id = ANY($3::uuid[]))`,
			campaignID, contactID, ccIDs).Scan(&leadIsCopied, &ccHasCopies); err != nil {
			return err
		}
		if leadIsCopied {
			return ErrLeadCCLeadIsCopied
		}
		if ccHasCopies {
			return ErrLeadCCHasCopies
		}
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM campaign_lead_cc
		WHERE campaign_id = $1 AND contact_id = $2
		  AND NOT (cc_contact_id = ANY(COALESCE($3::uuid[], '{}')))`,
		campaignID, contactID, ccIDs); err != nil {
		return err
	}
	if len(ccIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO campaign_lead_cc (campaign_id, contact_id, cc_contact_id, position)
			SELECT $1, $2, u.id, u.ord - 1
			FROM unnest($3::uuid[]) WITH ORDINALITY AS u(id, ord)
			ON CONFLICT (campaign_id, contact_id, cc_contact_id) DO UPDATE SET position = EXCLUDED.position`,
			campaignID, contactID, ccIDs); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *campaignProgressRepository) MarkLeadCCBounced(ctx context.Context, campaignID, contactID uuid.UUID, address string) (*uuid.UUID, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, nil
	}
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `
		UPDATE campaign_lead_cc x
		SET bounced_at = COALESCE(x.bounced_at, NOW())
		FROM contacts c
		WHERE c.id = x.cc_contact_id
		  AND x.campaign_id = $1 AND x.contact_id = $2
		  AND lower(c.email) = lower($3)
		RETURNING x.cc_contact_id`, campaignID, contactID, address).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (r *campaignProgressRepository) LeadForCopiedReply(ctx context.Context, ccContactID, emailAccountID uuid.UUID) (*CopiedLeadRef, error) {
	var ref CopiedLeadRef
	err := r.db.QueryRow(ctx, `
		SELECT p.campaign_id, p.contact_id, p.sequence_id
		FROM campaign_lead_cc x
		JOIN campaign_leads cl ON cl.campaign_id = x.campaign_id AND cl.contact_id = x.contact_id
		JOIN campaign_contact_progress p ON p.campaign_id = x.campaign_id AND p.contact_id = x.contact_id
		WHERE x.cc_contact_id = $1
		  AND (
		    cl.email_account_id = $2
		    OR EXISTS (
		      SELECT 1
		      FROM campaign_tasks ct
		      JOIN tasks t ON t.id = ct.task_id
		      WHERE ct.campaign_id = x.campaign_id AND ct.contact_id = x.contact_id
		        AND t.task_type = 'campaign' AND `+completedSendTaskSQL("t")+`
		        AND t.email_account_id = $2
		    )
		    OR EXISTS (
		      SELECT 1
		      FROM campaign_tasks ct
		      JOIN tasks t ON t.id = ct.task_id
		      JOIN email_accounts here ON here.id = $2
		      WHERE ct.campaign_id = x.campaign_id AND ct.contact_id = x.contact_id
		        AND t.reply_to <> ''
		        AND lower(t.reply_to) IN (lower(here.email), lower(COALESCE(NULLIF(here.send_as_email, ''), here.email)))
		    )
		  )
		  AND p.sent_at IS NOT NULL
		  AND `+progressIsEmailStep("p")+`
		ORDER BY p.sent_at DESC
		LIMIT 1`, ccContactID, emailAccountID).Scan(&ref.CampaignID, &ref.ContactID, &ref.SequenceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ref, nil
}

func (r *campaignProgressRepository) SuggestLeadCC(ctx context.Context, orgID, campaignID, contactID uuid.UUID, limit int) ([]models.CampaignLeadCCSuggestion, error) {
	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		WITH lead AS (
			SELECT lower(btrim(company)) AS co, lower(split_part(email, '@', 2)) AS dom
			FROM contacts WHERE id = $2 AND organization_id = $1
		)
		SELECT c.id, c.email, c.first_name, c.last_name, c.company,
		       CASE WHEN lead.co <> '' AND lower(btrim(c.company)) = lead.co THEN 'company' ELSE 'domain' END AS reason
		FROM contacts c, lead
		WHERE c.organization_id = $1
		  AND c.id <> $2
		  AND c.subscribed IS NOT FALSE
		  AND (
		    (lead.co <> '' AND lower(btrim(c.company)) = lead.co)
		    OR (lead.dom <> '' AND lead.dom NOT IN (%s) AND lower(split_part(c.email, '@', 2)) = lead.dom)
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM campaign_lead_cc x
		    WHERE x.campaign_id = $3 AND x.contact_id = $2 AND x.cc_contact_id = c.id
		  )
		ORDER BY (lead.co <> '' AND lower(btrim(c.company)) = lead.co) DESC, c.first_name, c.last_name, c.email
		LIMIT $4`, personalMailDomainsSQL), orgID, contactID, campaignID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.CampaignLeadCCSuggestion{}
	for rows.Next() {
		var s models.CampaignLeadCCSuggestion
		if err := rows.Scan(&s.ContactID, &s.Email, &s.FirstName, &s.LastName, &s.Company, &s.Reason); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *campaignProgressRepository) BouncedCopyAddresses(ctx context.Context, campaignID uuid.UUID, addresses []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(addresses) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT lower(recipient_email)
		FROM deliverability_events
		WHERE campaign_id = $1 AND event_type = 'bounce'
		  AND lower(recipient_email) = ANY($2::text[])`, campaignID, addresses)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out[a] = true
	}
	return out, rows.Err()
}

// leadCCJSONSQL is one lead's copies as a JSON array for the Leads list, with
// the lead row aliased hl and the campaign bound at cp.
func leadCCJSONSQL(cp string) string {
	return `(
		SELECT COALESCE(json_agg(json_build_object(
			'contact_id', c.id, 'email', c.email, 'first_name', c.first_name,
			'last_name', c.last_name, 'company', c.company,
			'status', ` + leadCCStatusSQL(cp) + `, 'bounced_at', x.bounced_at
		) ORDER BY x.position, x.created_at), '[]'::json)
		FROM campaign_lead_cc x
		JOIN contacts c ON c.id = x.cc_contact_id
		WHERE x.campaign_id = hl.campaign_id AND x.contact_id = hl.contact_id
	)`
}

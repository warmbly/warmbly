package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// SocialStateChange is what one GraphKit signal says about an account and a contact; nil fields are left as they are.
type SocialStateChange struct {
	InviteSentAt  *time.Time
	InvitePending *bool
	ConnectedAt   *time.Time
	MessageAt     *time.Time
	RepliedAt     *time.Time
	// Bind keeps the contact with the account for the rest of the campaign.
	Bind bool
}

// SocialActionUpdate is GraphKit's latest word on an action.
type SocialActionUpdate struct {
	GraphKitID  string
	Reference   string
	Status      string
	SkipReason  *string
	ErrorCode   *string
	CompletedAt *time.Time
}

// SocialActionStop is a queued campaign action whose contact should no longer get it, and why.
type SocialActionStop struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	GraphKitID     string
	ContactID      uuid.UUID
	CampaignID     *uuid.UUID
	Channel        string
	Type           string
	Reason         string
}

// OpenProfileCheck is a campaign contact whose LinkedIn Open Profile a branch is waiting to read.
type OpenProfileCheck struct {
	OrganizationID uuid.UUID
	CampaignID     uuid.UUID
	ContactID      uuid.UUID
	// Recipient is the contact's normalized LinkedIn public id, "" when they have none.
	Recipient string
}

// ---- contact identities ---------------------------------------------------

func (r *socialRepository) ContactIdentity(ctx context.Context, orgID, contactID uuid.UUID, channel string) (*models.SocialIdentityRow, error) {
	var row models.SocialIdentityRow
	err := r.db.QueryRow(ctx, `SELECT contact_id, channel, handle, normalized, consent_basis FROM contact_social_identities
		WHERE organization_id = $1 AND contact_id = $2 AND channel = $3`, orgID, contactID, channel).
		Scan(&row.ContactID, &row.Channel, &row.Handle, &row.Normalized, &row.ConsentBasis)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *socialRepository) SetContactIdentity(ctx context.Context, orgID, contactID uuid.UUID, row models.SocialIdentityRow) error {
	return writeContactSocial(ctx, r.db, orgID, contactID, contactSocialPatch{set: []models.SocialIdentityRow{row}}, true)
}

// ---- campaign accounts ----------------------------------------------------

func (r *socialRepository) LeadsWithoutHandle(ctx context.Context, orgID, campaignID uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `WITH ch AS (SELECT DISTINCT s.action->'social'->>'channel' AS channel FROM sequences s
			WHERE s.campaign_id = $2 AND s.organization_id = $1 AND s.kind = 'action' AND s.action->>'type' = 'social'
			AND s.action->'social'->>'channel' = ANY($3::text[]))
		SELECT count(*) FROM campaign_leads cl
		JOIN campaigns ca ON ca.id = cl.campaign_id AND ca.organization_id = $1
		WHERE cl.campaign_id = $2 AND EXISTS (SELECT 1 FROM ch
			WHERE NOT EXISTS (SELECT 1 FROM contact_social_identities si
				WHERE si.contact_id = cl.contact_id AND si.organization_id = $1 AND si.channel = ch.channel))`,
		orgID, campaignID, models.SocialChannels).Scan(&n)
	return n, err
}

func (r *socialRepository) CampaignAccountIDs(ctx context.Context, orgID, campaignID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `SELECT cla.social_account_id FROM campaign_social_accounts cla
		JOIN campaigns ca ON ca.id = cla.campaign_id AND ca.organization_id = $1
		JOIN social_accounts a ON a.id = cla.social_account_id AND a.organization_id = $1
		WHERE cla.campaign_id = $2 AND a.removed_at IS NULL
		ORDER BY cla.created_at, cla.social_account_id`, orgID, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *socialRepository) SetCampaignAccounts(ctx context.Context, orgID, campaignID uuid.UUID, ids []uuid.UUID) (bool, error) {
	if ids == nil {
		ids = []uuid.UUID{}
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var owned bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM campaigns WHERE id = $2 AND organization_id = $1)`,
		orgID, campaignID).Scan(&owned); err != nil {
		return false, err
	}
	if !owned {
		return false, nil
	}
	var mine int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM social_accounts WHERE organization_id = $1 AND id = ANY($2) AND removed_at IS NULL`,
		orgID, ids).Scan(&mine); err != nil {
		return false, err
	}
	if mine != len(ids) {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `DELETE FROM campaign_social_accounts WHERE campaign_id = $1 AND social_account_id <> ALL($2)`, campaignID, ids); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO campaign_social_accounts (campaign_id, social_account_id)
		SELECT $1, unnest($2::uuid[]) ON CONFLICT DO NOTHING`, campaignID, ids); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (r *socialRepository) CampaignAccounts(ctx context.Context, orgID, campaignID uuid.UUID) ([]models.SocialAccount, error) {
	return r.accounts(ctx, `SELECT `+socialAccountColumns+` FROM social_accounts
		WHERE organization_id = $1 AND removed_at IS NULL AND id IN (
			SELECT cla.social_account_id FROM campaign_social_accounts cla
			JOIN campaigns ca ON ca.id = cla.campaign_id AND ca.organization_id = $1
			WHERE cla.campaign_id = $2)
		ORDER BY created_at, id`, orgID, campaignID)
}

func (r *socialRepository) CampaignWakeAccount(ctx context.Context, campaignID uuid.UUID) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT e.id FROM campaigns ca
		JOIN email_accounts e ON e.organization_id = ca.organization_id
		WHERE ca.id = $1 AND EXISTS (
			SELECT 1 FROM campaign_social_accounts cla JOIN social_accounts a ON a.id = cla.social_account_id
			WHERE cla.campaign_id = ca.id AND a.organization_id = ca.organization_id AND a.removed_at IS NULL)
		ORDER BY (e.status = 'active') DESC, e.created_at, e.id LIMIT 1`, campaignID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return id, err == nil, err
}

// ---- actions --------------------------------------------------------------

const socialActionColumns = `a.id, a.organization_id, a.graphkit_id, a.social_account_id, a.campaign_id, COALESCE(ca.name, ''), a.contact_id,
	a.sequence_id, a.channel, a.type, a.status, a.skip_reason, a.error_code, a.reference, a.attempts, a.queued_at, a.completed_at, a.created_at, a.updated_at`

func scanSocialAction(row pgx.Row) (*models.SocialActionRow, error) {
	var a models.SocialActionRow
	err := row.Scan(&a.ID, &a.OrganizationID, &a.GraphKitID, &a.AccountID, &a.CampaignID, &a.CampaignName, &a.ContactID, &a.SequenceID,
		&a.Channel, &a.Type, &a.Status, &a.SkipReason, &a.ErrorCode, &a.Reference, &a.Attempts, &a.QueuedAt, &a.CompletedAt, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *socialRepository) actionWhere(ctx context.Context, where string, args ...any) (*models.SocialActionRow, error) {
	return scanSocialAction(r.db.QueryRow(ctx, `SELECT `+socialActionColumns+` FROM social_actions a
		LEFT JOIN campaigns ca ON ca.id = a.campaign_id WHERE `+where, args...))
}

func (r *socialRepository) ReserveAction(ctx context.Context, a *models.SocialActionRow) (*models.SocialActionRow, error) {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if err := r.exec(ctx, `INSERT INTO social_actions (id, organization_id, social_account_id, campaign_id, contact_id, sequence_id,
			type, status, skip_reason, error_code, reference, completed_at, channel)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (campaign_id, contact_id, sequence_id) WHERE campaign_id IS NOT NULL AND sequence_id IS NOT NULL DO NOTHING`,
		a.ID, a.OrganizationID, a.AccountID, a.CampaignID, a.ContactID, a.SequenceID, a.Type, a.Status, a.SkipReason, a.ErrorCode,
		a.Reference, a.CompletedAt, a.Channel); err != nil {
		return nil, err
	}
	return r.actionWhere(ctx, `a.organization_id = $1 AND a.campaign_id = $2 AND a.contact_id = $3 AND a.sequence_id = $4`,
		a.OrganizationID, a.CampaignID, a.ContactID, a.SequenceID)
}

func (r *socialRepository) SetActionQueued(ctx context.Context, orgID, id, accountID uuid.UUID, graphkitID, status string) error {
	return r.exec(ctx, `UPDATE social_actions SET graphkit_id = $3, social_account_id = $4,
			status = CASE WHEN status = 'pending' THEN $5 ELSE status END, queued_at = COALESCE(queued_at, now()), updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, graphkitID, accountID, status)
}

func (r *socialRepository) CountActionAttempt(ctx context.Context, orgID, id uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `UPDATE social_actions SET attempts = attempts + 1, updated_at = now()
		WHERE organization_id = $1 AND id = $2 RETURNING attempts`, orgID, id).Scan(&n)
	return n, err
}

func (r *socialRepository) FinishAction(ctx context.Context, orgID, id uuid.UUID, status string, skipReason, errorCode *string) error {
	return r.exec(ctx, `UPDATE social_actions SET status = $3, skip_reason = $4, error_code = $5, completed_at = now(), updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, status, skipReason, errorCode)
}

func (r *socialRepository) ApplyActionUpdate(ctx context.Context, orgID uuid.UUID, u SocialActionUpdate) (*models.SocialActionRow, bool, error) {
	var id uuid.UUID
	var prev string
	err := r.db.QueryRow(ctx, `SELECT id, status FROM social_actions
		WHERE organization_id = $1 AND (graphkit_id = $2 OR (graphkit_id IS NULL AND $3 <> '' AND reference = $3))
		ORDER BY (graphkit_id = $2) DESC NULLS LAST LIMIT 1`, orgID, u.GraphKitID, u.Reference).Scan(&id, &prev)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	// A finished action never moves back to a live status on a late event.
	if err := r.exec(ctx, `UPDATE social_actions SET graphkit_id = COALESCE(graphkit_id, $3), status = $4, skip_reason = $5, error_code = $6,
			queued_at = COALESCE(queued_at, now()),
			completed_at = CASE WHEN $4 IN ('succeeded', 'skipped', 'failed', 'cancelled', 'expired') THEN COALESCE($7, now()) ELSE completed_at END,
			updated_at = now()
		WHERE organization_id = $1 AND id = $2
		  AND (status IN ('pending', 'queued', 'running') OR status = $4)`,
		orgID, id, u.GraphKitID, u.Status, u.SkipReason, u.ErrorCode, u.CompletedAt); err != nil {
		return nil, false, err
	}
	row, err := r.actionWhere(ctx, `a.organization_id = $1 AND a.id = $2`, orgID, id)
	if err != nil || row == nil {
		return row, false, err
	}
	return row, row.Status != prev, nil
}

func (r *socialRepository) ActionsToStop(ctx context.Context, contactID *uuid.UUID, limit int) ([]SocialActionStop, error) {
	query := `SELECT a.id, a.organization_id, a.graphkit_id, a.contact_id, a.campaign_id, a.channel, a.type, x.reason
		FROM social_actions a
		JOIN contacts c ON c.id = a.contact_id
		LEFT JOIN campaigns ca ON ca.id = a.campaign_id AND ca.organization_id = a.organization_id
		LEFT JOIN campaign_leads cl ON cl.campaign_id = ca.id AND cl.contact_id = a.contact_id
		CROSS JOIN LATERAL (SELECT CASE
			WHEN ca.id IS NULL THEN 'campaign_deleted'
			WHEN ca.status::text NOT IN ('active', 'completed') THEN 'campaign_paused'
			WHEN cl.contact_id IS NULL THEN 'removed_from_campaign'
			WHEN c.subscribed IS FALSE THEN 'unsubscribed'
			WHEN COALESCE(c.email, '') <> '' AND recipient_suppressed(a.organization_id, c.email) THEN 'suppressed'
			WHEN ca.stop_on_reply AND (
				EXISTS (SELECT 1 FROM campaign_contact_progress p
					WHERE p.campaign_id = ca.id AND p.contact_id = a.contact_id AND p.replied_at > a.created_at)
				OR EXISTS (SELECT 1 FROM social_contact_state s
					WHERE s.organization_id = a.organization_id AND s.contact_id = a.contact_id AND s.replied_at > a.created_at))
				THEN 'replied'
			END AS reason) x
		WHERE a.status = 'queued' AND a.graphkit_id IS NOT NULL AND a.reference LIKE 'wb:%' AND x.reason IS NOT NULL
		  AND ($1::uuid IS NULL OR a.contact_id = $1)
		ORDER BY a.created_at LIMIT $2`
	rows, err := r.db.Query(ctx, query, contactID, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	var out []SocialActionStop
	for rows.Next() {
		var s SocialActionStop
		if err := rows.Scan(&s.ID, &s.OrganizationID, &s.GraphKitID, &s.ContactID, &s.CampaignID, &s.Channel, &s.Type, &s.Reason); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *socialRepository) StickyAccount(ctx context.Context, orgID, campaignID, contactID uuid.UUID, channel string, candidates []uuid.UUID) (*uuid.UUID, error) {
	var id uuid.UUID
	// The account the contact is bound to in this campaign, then one used for them in it, then one connected to or
	// inviting them in any campaign.
	err := r.db.QueryRow(ctx, `SELECT account_id FROM (
			SELECT s.social_account_id AS account_id, 0 AS rank, s.bound_at AS at FROM social_contact_state s
			WHERE s.organization_id = $1 AND s.campaign_id = $2 AND s.contact_id = $3 AND s.channel = $5
			  AND s.bound_at IS NOT NULL AND s.social_account_id = ANY($4)
			UNION ALL
			SELECT a.social_account_id, 1, a.created_at FROM social_actions a
			WHERE a.organization_id = $1 AND a.campaign_id = $2 AND a.contact_id = $3 AND a.channel = $5 AND a.social_account_id = ANY($4)
			UNION ALL
			SELECT s.social_account_id, CASE WHEN s.connected_at IS NOT NULL THEN 2 ELSE 3 END, COALESCE(s.connected_at, s.invite_sent_at, s.updated_at)
			FROM social_contact_state s
			WHERE s.organization_id = $1 AND s.contact_id = $3 AND s.channel = $5 AND s.social_account_id = ANY($4)
			  AND (s.connected_at IS NOT NULL OR s.invite_pending)
		) c ORDER BY rank, at LIMIT 1`, orgID, campaignID, contactID, candidates, channel).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (r *socialRepository) OpenActionsToday(ctx context.Context, orgID uuid.UUID, accountIDs []uuid.UUID, since time.Time) (map[uuid.UUID]map[string]int, error) {
	out := map[uuid.UUID]map[string]int{}
	rows, err := r.db.Query(ctx, `SELECT social_account_id, type, count(*) FROM social_actions
		WHERE organization_id = $1 AND social_account_id = ANY($2) AND created_at >= $3 AND status IN ('pending', 'queued', 'running')
		GROUP BY 1, 2`, orgID, accountIDs, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var t string
		var n int
		if err := rows.Scan(&id, &t, &n); err != nil {
			return nil, err
		}
		if out[id] == nil {
			out[id] = map[string]int{}
		}
		out[id][t] = n
	}
	return out, rows.Err()
}

func (r *socialRepository) ContactActions(ctx context.Context, orgID, contactID uuid.UUID, limit int) ([]models.SocialActionRow, error) {
	query := `SELECT ` + socialActionColumns + ` FROM social_actions a LEFT JOIN campaigns ca ON ca.id = a.campaign_id
		WHERE a.organization_id = $1 AND a.contact_id = $2 ORDER BY a.created_at DESC, a.id DESC LIMIT $3`
	rows, err := r.db.Query(ctx, query, orgID, contactID, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.SocialActionRow{}
	for rows.Next() {
		a, err := scanSocialAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (r *socialRepository) CampaignStats(ctx context.Context, orgID uuid.UUID, campaignIDs []uuid.UUID) (map[uuid.UUID]*models.SocialCampaignStats, error) {
	out := map[uuid.UUID]*models.SocialCampaignStats{}
	if len(campaignIDs) == 0 {
		return out, nil
	}
	// One row per campaign and channel; a reply counts once per contact after the campaign's first step to them there.
	query := `SELECT ca.id, a.channel,
			count(*) FILTER (WHERE a.status = 'succeeded'),
			count(*) FILTER (WHERE a.type = 'invite' AND a.status = 'succeeded'),
			count(*) FILTER (WHERE a.type = 'invite' AND a.status = 'succeeded' AND EXISTS (
				SELECT 1 FROM social_contact_state s WHERE s.campaign_id = ca.id AND s.contact_id = a.contact_id AND s.channel = a.channel
				  AND s.organization_id = $1 AND s.connected_at IS NOT NULL AND s.connected_at >= a.created_at)),
			count(*) FILTER (WHERE a.type = 'message' AND a.status = 'succeeded'),
			count(*) FILTER (WHERE a.type = 'inmail' AND a.status = 'succeeded'),
			count(*) FILTER (WHERE a.type = 'voice_note' AND a.status = 'succeeded'),
			count(DISTINCT a.contact_id) FILTER (WHERE EXISTS (
				SELECT 1 FROM social_contact_state s WHERE s.campaign_id = ca.id AND s.contact_id = a.contact_id AND s.channel = a.channel
				  AND s.organization_id = $1 AND s.replied_at IS NOT NULL AND s.replied_at >= a.created_at))
		FROM campaigns ca JOIN social_actions a ON a.campaign_id = ca.id AND a.organization_id = $1
		WHERE ca.organization_id = $1 AND ca.id = ANY($2) GROUP BY ca.id, a.channel`
	rows, err := r.db.Query(ctx, query, orgID, campaignIDs)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var ch string
		var sent, invites, accepted, messages, inmails, voice, replies int
		if err := rows.Scan(&id, &ch, &sent, &invites, &accepted, &messages, &inmails, &voice, &replies); err != nil {
			return nil, err
		}
		st := out[id]
		if st == nil {
			st = &models.SocialCampaignStats{ByChannel: map[string]models.SocialChannelStats{}}
			out[id] = st
		}
		st.InvitesSent += invites
		st.InvitesAccepted += accepted
		st.MessagesSent += messages
		st.InmailsSent += inmails
		st.VoiceNotesSent += voice
		st.Replies += replies
		st.ByChannel[ch] = models.SocialChannelStats{ActionsSent: sent, Replies: replies}
	}
	for _, st := range out {
		if st.InvitesSent > 0 {
			st.AcceptanceRate = float64(st.InvitesAccepted) / float64(st.InvitesSent) * 100
		}
	}
	return out, rows.Err()
}

// ---- contact state --------------------------------------------------------

// sameAccount is a state row the signal's account may speak for: unbound, or bound to that account.
const sameAccount = `(EXCLUDED.social_account_id IS NOT NULL
	AND COALESCE(social_contact_state.social_account_id, EXCLUDED.social_account_id) = EXCLUDED.social_account_id)`

func (r *socialRepository) NoteContactState(ctx context.Context, orgID uuid.UUID, campaignID *uuid.UUID, contactIDs []uuid.UUID, channel string, accountID uuid.UUID, c SocialStateChange) error {
	if len(contactIDs) == 0 {
		return nil
	}
	// Each campaign the contact is a lead of takes the signal. What the account did reaches only the campaigns that
	// send from it (or the one named); a reply reaches every one, since a reply on any channel stops the contact.
	return r.exec(ctx, `WITH t AS (
			SELECT cl.campaign_id, cl.contact_id,
				CASE WHEN $5::uuid IS NOT NULL OR EXISTS (SELECT 1 FROM campaign_social_accounts csa
					WHERE csa.campaign_id = cl.campaign_id AND csa.social_account_id = $4) THEN $4::uuid END AS acct
			FROM campaign_leads cl
			JOIN campaigns ca ON ca.id = cl.campaign_id AND ca.organization_id = $1
			JOIN contacts c ON c.id = cl.contact_id AND c.organization_id = $1
			WHERE cl.contact_id = ANY($2) AND ($5::uuid IS NULL OR cl.campaign_id = $5)
			  AND EXISTS (SELECT 1 FROM social_accounts a WHERE a.id = $4 AND a.organization_id = $1)
		)
		INSERT INTO social_contact_state (campaign_id, contact_id, channel, organization_id, social_account_id, bound_at,
			invite_sent_at, invite_pending, connected_at, last_message_at, replied_at)
		SELECT t.campaign_id, t.contact_id, $3, $1, t.acct,
			CASE WHEN t.acct IS NOT NULL AND $11 THEN now() END,
			CASE WHEN t.acct IS NOT NULL THEN $6::timestamptz END,
			t.acct IS NOT NULL AND COALESCE($7::bool, false) AND $8::timestamptz IS NULL,
			CASE WHEN t.acct IS NOT NULL THEN $8::timestamptz END,
			CASE WHEN t.acct IS NOT NULL THEN $9::timestamptz END,
			$10::timestamptz
		FROM t WHERE t.acct IS NOT NULL OR $10::timestamptz IS NOT NULL
		ON CONFLICT (campaign_id, contact_id, channel) DO UPDATE SET
			social_account_id = COALESCE(social_contact_state.social_account_id, EXCLUDED.social_account_id),
			bound_at = CASE WHEN `+sameAccount+` THEN COALESCE(social_contact_state.bound_at, EXCLUDED.bound_at)
				ELSE social_contact_state.bound_at END,
			invite_sent_at = CASE WHEN `+sameAccount+` THEN GREATEST(social_contact_state.invite_sent_at, EXCLUDED.invite_sent_at)
				ELSE social_contact_state.invite_sent_at END,
			connected_at = CASE WHEN `+sameAccount+` THEN COALESCE(social_contact_state.connected_at, EXCLUDED.connected_at)
				ELSE social_contact_state.connected_at END,
			invite_pending = CASE WHEN NOT `+sameAccount+` THEN social_contact_state.invite_pending
				WHEN COALESCE(social_contact_state.connected_at, EXCLUDED.connected_at) IS NOT NULL THEN false
				ELSE COALESCE($7::bool, social_contact_state.invite_pending) END,
			last_message_at = CASE WHEN `+sameAccount+` THEN GREATEST(social_contact_state.last_message_at, EXCLUDED.last_message_at)
				ELSE social_contact_state.last_message_at END,
			replied_at = GREATEST(social_contact_state.replied_at, EXCLUDED.replied_at),
			updated_at = now()
		WHERE social_contact_state.organization_id = EXCLUDED.organization_id`,
		orgID, contactIDs, channel, accountID, campaignID, c.InviteSentAt, c.InvitePending, c.ConnectedAt, c.MessageAt, c.RepliedAt, c.Bind)
}

func (r *socialRepository) SetOpenProfile(ctx context.Context, orgID, campaignID, contactID uuid.UUID, open bool) error {
	return r.exec(ctx, `INSERT INTO social_contact_state (campaign_id, contact_id, channel, organization_id, open_profile, open_profile_checked_at)
		SELECT ca.id, $3, 'linkedin', $1, $4, now() FROM campaigns ca
		WHERE ca.id = $2 AND ca.organization_id = $1
		  AND EXISTS (SELECT 1 FROM contacts c WHERE c.id = $3 AND c.organization_id = $1)
		ON CONFLICT (campaign_id, contact_id, channel) DO UPDATE SET open_profile = EXCLUDED.open_profile,
			open_profile_checked_at = now(), updated_at = now()
		WHERE social_contact_state.organization_id = EXCLUDED.organization_id`, orgID, campaignID, contactID, open)
}

func (r *socialRepository) OpenProfileChecks(ctx context.Context, limit int) ([]OpenProfileCheck, error) {
	// Leads of active campaigns that branch on Open Profile, once a step reached them and before it is read.
	query := `SELECT ca.organization_id, cl.campaign_id, cl.contact_id, COALESCE(si.normalized, '')
		FROM campaign_leads cl
		JOIN campaigns ca ON ca.id = cl.campaign_id AND ca.status = 'active'
		LEFT JOIN contact_social_identities si ON si.contact_id = cl.contact_id AND si.organization_id = ca.organization_id
			AND si.channel = 'linkedin'
		WHERE EXISTS (SELECT 1 FROM sequences sq WHERE sq.campaign_id = ca.id AND sq.conditions::text LIKE '%social_open_profile%')
		  AND EXISTS (SELECT 1 FROM campaign_contact_progress p WHERE p.campaign_id = ca.id AND p.contact_id = cl.contact_id
			AND p.sent_at IS NOT NULL)
		  AND NOT EXISTS (SELECT 1 FROM social_contact_state s WHERE s.campaign_id = ca.id AND s.contact_id = cl.contact_id
			AND s.channel = 'linkedin' AND s.open_profile_checked_at IS NOT NULL)
		LIMIT $1`
	rows, err := r.db.Query(ctx, query, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	var out []OpenProfileCheck
	for rows.Next() {
		var c OpenProfileCheck
		if err := rows.Scan(&c.OrganizationID, &c.CampaignID, &c.ContactID, &c.Recipient); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *socialRepository) ContactIDsByHandle(ctx context.Context, orgID uuid.UUID, channel, normalized string) ([]uuid.UUID, error) {
	out := []uuid.UUID{}
	if normalized == "" {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT si.contact_id FROM contact_social_identities si
		JOIN contacts c ON c.id = si.contact_id AND c.organization_id = $1
		WHERE si.organization_id = $1 AND si.channel = $2 AND si.normalized = $3 LIMIT 50`, orgID, channel, normalized)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *socialRepository) InviteStates(ctx context.Context, orgID uuid.UUID, publicIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(publicIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT si.normalized, bool_or(s.connected_at IS NOT NULL)
		FROM contact_social_identities si
		JOIN social_contact_state s ON s.contact_id = si.contact_id AND s.organization_id = $1 AND s.channel = 'linkedin'
		WHERE si.organization_id = $1 AND si.channel = 'linkedin' AND si.normalized = ANY($2)
		  AND (s.invite_pending OR s.connected_at IS NOT NULL)
		GROUP BY 1`, orgID, publicIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var connected bool
		if err := rows.Scan(&id, &connected); err != nil {
			return nil, err
		}
		out[id] = connected
	}
	return out, rows.Err()
}

func (r *socialRepository) ActiveCampaignsFor(ctx context.Context, orgID uuid.UUID, contactIDs []uuid.UUID) ([]string, error) {
	out := []string{}
	if len(contactIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT DISTINCT ca.id::text FROM campaign_leads cl
		JOIN campaigns ca ON ca.id = cl.campaign_id AND ca.organization_id = $1
		WHERE cl.contact_id = ANY($2) AND ca.status = 'active'`, orgID, contactIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *socialRepository) ReplyCampaign(ctx context.Context, orgID, contactID uuid.UUID, at time.Time) (*uuid.UUID, *uuid.UUID, error) {
	var campaignID, sequenceID uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT p.campaign_id, p.sequence_id FROM campaign_contact_progress p
		JOIN campaigns ca ON ca.id = p.campaign_id AND ca.organization_id = $1
		WHERE p.contact_id = $2 AND p.sent_at IS NOT NULL AND p.sent_at <= $3
		  AND EXISTS (SELECT 1 FROM social_actions a WHERE a.organization_id = $1 AND a.campaign_id = p.campaign_id
			AND a.contact_id = p.contact_id AND a.created_at <= $3 AND a.status NOT IN ('pending', 'skipped', 'failed', 'cancelled', 'expired'))
		ORDER BY p.sent_at DESC LIMIT 1`, orgID, contactID, at).Scan(&campaignID, &sequenceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return &campaignID, &sequenceID, nil
}

func (r *socialRepository) ClaimInboundReply(ctx context.Context, orgID, messageID uuid.UUID) (bool, error) {
	n, err := r.affected(ctx, `UPDATE social_messages SET reply_handled_at = now()
		WHERE organization_id = $1 AND id = $2 AND direction = 'inbound' AND reply_handled_at IS NULL`, orgID, messageID)
	return n > 0, err
}

func (r *socialRepository) MessageID(ctx context.Context, orgID uuid.UUID, graphkitID string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT id FROM social_messages WHERE organization_id = $1 AND graphkit_id = $2`, orgID, graphkitID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return id, err
}

func (r *socialRepository) UnreadConversations(ctx context.Context, orgID uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM social_conversations c
		WHERE c.organization_id = $1 AND c.unread_count > 0
		  AND EXISTS (SELECT 1 FROM social_accounts a WHERE a.id = c.social_account_id AND a.organization_id = $1 AND a.removed_at IS NULL)`,
		orgID).Scan(&n)
	return n, err
}

// ---- imports --------------------------------------------------------------

const linkedInImportColumns = `id, organization_id, user_id, status, total, processed, created, updated, skipped, no_email, emails_found,
	contact_ids, request, error, created_at, updated_at`

func scanLinkedInImport(row pgx.Row) (*models.LinkedInImport, error) {
	var im models.LinkedInImport
	err := row.Scan(&im.ID, &im.OrganizationID, &im.UserID, &im.Status, &im.Total, &im.Processed, &im.Created, &im.Updated, &im.Skipped,
		&im.NoEmail, &im.EmailsFound, &im.ContactIDs, &im.Request, &im.Error, &im.CreatedAt, &im.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if im.ContactIDs == nil {
		im.ContactIDs = []uuid.UUID{}
	}
	return &im, nil
}

func (r *socialRepository) CreateImport(ctx context.Context, im *models.LinkedInImport) error {
	if im.ID == uuid.Nil {
		im.ID = uuid.New()
	}
	return r.exec(ctx, `INSERT INTO linkedin_imports (id, organization_id, user_id, status, request) VALUES ($1, $2, $3, $4, $5)`,
		im.ID, im.OrganizationID, im.UserID, im.Status, im.Request)
}

func (r *socialRepository) GetImport(ctx context.Context, orgID, id uuid.UUID) (*models.LinkedInImport, error) {
	return scanLinkedInImport(r.db.QueryRow(ctx, `SELECT `+linkedInImportColumns+` FROM linkedin_imports
		WHERE organization_id = $1 AND id = $2`, orgID, id))
}

func (r *socialRepository) ClaimImport(ctx context.Context, id *uuid.UUID, staleBefore time.Time) (*models.LinkedInImport, error) {
	return scanLinkedInImport(r.db.QueryRow(ctx, `UPDATE linkedin_imports SET status = 'running', updated_at = now()
		WHERE id = (SELECT id FROM linkedin_imports
			WHERE ($1::uuid IS NULL OR id = $1)
			  AND (status = 'queued' OR (status = 'running' AND updated_at < $2))
			ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING `+linkedInImportColumns, id, staleBefore))
}

func (r *socialRepository) SaveImport(ctx context.Context, im *models.LinkedInImport) error {
	return r.exec(ctx, `UPDATE linkedin_imports SET status = $3, total = $4, processed = $5, created = $6, updated = $7, skipped = $8,
			no_email = $9, emails_found = $10, contact_ids = $11, error = $12, updated_at = now()
		WHERE organization_id = $1 AND id = $2`,
		im.OrganizationID, im.ID, im.Status, im.Total, im.Processed, im.Created, im.Updated, im.Skipped, im.NoEmail, im.EmailsFound,
		im.ContactIDs, im.Error)
}

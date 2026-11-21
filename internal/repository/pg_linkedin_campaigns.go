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

// LinkedInStateChange is what one GraphKit signal says about an account and a contact; nil fields are left as they are.
type LinkedInStateChange struct {
	InviteSentAt  *time.Time
	InvitePending *bool
	ConnectedAt   *time.Time
	MessageAt     *time.Time
	RepliedAt     *time.Time
}

// LinkedInActionUpdate is GraphKit's latest word on an action.
type LinkedInActionUpdate struct {
	GraphKitID  string
	Reference   string
	Status      string
	SkipReason  *string
	ErrorCode   *string
	CompletedAt *time.Time
}

// LinkedInActionStop is a queued campaign action whose contact should no longer get it, and why.
type LinkedInActionStop struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	GraphKitID     string
	ContactID      uuid.UUID
	CampaignID     *uuid.UUID
	Type           string
	Reason         string
}

// ---- campaign accounts ----------------------------------------------------

func (r *linkedInRepository) LeadsWithoutProfile(ctx context.Context, orgID, campaignID uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM campaign_leads cl
		JOIN campaigns ca ON ca.id = cl.campaign_id AND ca.organization_id = $1
		JOIN contacts c ON c.id = cl.contact_id
		WHERE cl.campaign_id = $2 AND COALESCE(c.linkedin_url, '') = ''`, orgID, campaignID).Scan(&n)
	return n, err
}

func (r *linkedInRepository) CampaignAccountIDs(ctx context.Context, orgID, campaignID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `SELECT cla.linkedin_account_id FROM campaign_linkedin_accounts cla
		JOIN campaigns ca ON ca.id = cla.campaign_id AND ca.organization_id = $1
		JOIN linkedin_accounts a ON a.id = cla.linkedin_account_id AND a.organization_id = $1
		WHERE cla.campaign_id = $2 AND a.removed_at IS NULL
		ORDER BY cla.created_at, cla.linkedin_account_id`, orgID, campaignID)
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

func (r *linkedInRepository) SetCampaignAccounts(ctx context.Context, orgID, campaignID uuid.UUID, ids []uuid.UUID) (bool, error) {
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
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM linkedin_accounts WHERE organization_id = $1 AND id = ANY($2) AND removed_at IS NULL`,
		orgID, ids).Scan(&mine); err != nil {
		return false, err
	}
	if mine != len(ids) {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `DELETE FROM campaign_linkedin_accounts WHERE campaign_id = $1 AND linkedin_account_id <> ALL($2)`, campaignID, ids); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO campaign_linkedin_accounts (campaign_id, linkedin_account_id)
		SELECT $1, unnest($2::uuid[]) ON CONFLICT DO NOTHING`, campaignID, ids); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (r *linkedInRepository) CampaignAccounts(ctx context.Context, orgID, campaignID uuid.UUID) ([]models.LinkedInAccount, error) {
	return r.accounts(ctx, `SELECT `+linkedInAccountColumns+` FROM linkedin_accounts
		WHERE organization_id = $1 AND removed_at IS NULL AND id IN (
			SELECT cla.linkedin_account_id FROM campaign_linkedin_accounts cla
			JOIN campaigns ca ON ca.id = cla.campaign_id AND ca.organization_id = $1
			WHERE cla.campaign_id = $2)
		ORDER BY created_at, id`, orgID, campaignID)
}

func (r *linkedInRepository) CampaignWakeAccount(ctx context.Context, campaignID uuid.UUID) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT e.id FROM campaigns ca
		JOIN email_accounts e ON e.organization_id = ca.organization_id
		WHERE ca.id = $1 AND EXISTS (
			SELECT 1 FROM campaign_linkedin_accounts cla JOIN linkedin_accounts a ON a.id = cla.linkedin_account_id
			WHERE cla.campaign_id = ca.id AND a.organization_id = ca.organization_id AND a.removed_at IS NULL)
		ORDER BY (e.status = 'active') DESC, e.created_at, e.id LIMIT 1`, campaignID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return id, err == nil, err
}

// ---- actions --------------------------------------------------------------

const linkedInActionColumns = `a.id, a.organization_id, a.graphkit_id, a.linkedin_account_id, a.campaign_id, COALESCE(ca.name, ''), a.contact_id,
	a.sequence_id, a.type, a.status, a.skip_reason, a.error_code, a.reference, a.attempts, a.queued_at, a.completed_at, a.created_at, a.updated_at`

func scanLinkedInAction(row pgx.Row) (*models.LinkedInAction, error) {
	var a models.LinkedInAction
	err := row.Scan(&a.ID, &a.OrganizationID, &a.GraphKitID, &a.AccountID, &a.CampaignID, &a.CampaignName, &a.ContactID, &a.SequenceID,
		&a.Type, &a.Status, &a.SkipReason, &a.ErrorCode, &a.Reference, &a.Attempts, &a.QueuedAt, &a.CompletedAt, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *linkedInRepository) actionWhere(ctx context.Context, where string, args ...any) (*models.LinkedInAction, error) {
	return scanLinkedInAction(r.db.QueryRow(ctx, `SELECT `+linkedInActionColumns+` FROM linkedin_actions a
		LEFT JOIN campaigns ca ON ca.id = a.campaign_id WHERE `+where, args...))
}

func (r *linkedInRepository) ReserveAction(ctx context.Context, a *models.LinkedInAction) (*models.LinkedInAction, error) {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if err := r.exec(ctx, `INSERT INTO linkedin_actions (id, organization_id, linkedin_account_id, campaign_id, contact_id, sequence_id,
			type, status, skip_reason, error_code, reference, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (campaign_id, contact_id, sequence_id) WHERE campaign_id IS NOT NULL AND sequence_id IS NOT NULL DO NOTHING`,
		a.ID, a.OrganizationID, a.AccountID, a.CampaignID, a.ContactID, a.SequenceID, a.Type, a.Status, a.SkipReason, a.ErrorCode,
		a.Reference, a.CompletedAt); err != nil {
		return nil, err
	}
	return r.actionWhere(ctx, `a.organization_id = $1 AND a.campaign_id = $2 AND a.contact_id = $3 AND a.sequence_id = $4`,
		a.OrganizationID, a.CampaignID, a.ContactID, a.SequenceID)
}

func (r *linkedInRepository) SetActionQueued(ctx context.Context, orgID, id, accountID uuid.UUID, graphkitID, status string) error {
	return r.exec(ctx, `UPDATE linkedin_actions SET graphkit_id = $3, linkedin_account_id = $4,
			status = CASE WHEN status = 'pending' THEN $5 ELSE status END, queued_at = COALESCE(queued_at, now()), updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, graphkitID, accountID, status)
}

func (r *linkedInRepository) CountActionAttempt(ctx context.Context, orgID, id uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `UPDATE linkedin_actions SET attempts = attempts + 1, updated_at = now()
		WHERE organization_id = $1 AND id = $2 RETURNING attempts`, orgID, id).Scan(&n)
	return n, err
}

func (r *linkedInRepository) FinishAction(ctx context.Context, orgID, id uuid.UUID, status string, skipReason, errorCode *string) error {
	return r.exec(ctx, `UPDATE linkedin_actions SET status = $3, skip_reason = $4, error_code = $5, completed_at = now(), updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, status, skipReason, errorCode)
}

func (r *linkedInRepository) ApplyActionUpdate(ctx context.Context, orgID uuid.UUID, u LinkedInActionUpdate) (*models.LinkedInAction, bool, error) {
	var id uuid.UUID
	var prev string
	err := r.db.QueryRow(ctx, `SELECT id, status FROM linkedin_actions
		WHERE organization_id = $1 AND (graphkit_id = $2 OR (graphkit_id IS NULL AND $3 <> '' AND reference = $3))
		ORDER BY (graphkit_id = $2) DESC NULLS LAST LIMIT 1`, orgID, u.GraphKitID, u.Reference).Scan(&id, &prev)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	// A finished action never moves back to a live status on a late event.
	if err := r.exec(ctx, `UPDATE linkedin_actions SET graphkit_id = COALESCE(graphkit_id, $3), status = $4, skip_reason = $5, error_code = $6,
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

func (r *linkedInRepository) ActionsToStop(ctx context.Context, contactID *uuid.UUID, limit int) ([]LinkedInActionStop, error) {
	query := `SELECT a.id, a.organization_id, a.graphkit_id, a.contact_id, a.campaign_id, a.type, x.reason
		FROM linkedin_actions a
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
				OR EXISTS (SELECT 1 FROM linkedin_contact_state s
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
	var out []LinkedInActionStop
	for rows.Next() {
		var s LinkedInActionStop
		if err := rows.Scan(&s.ID, &s.OrganizationID, &s.GraphKitID, &s.ContactID, &s.CampaignID, &s.Type, &s.Reason); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *linkedInRepository) StickyAccount(ctx context.Context, orgID, campaignID, contactID uuid.UUID, candidates []uuid.UUID) (*uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT account_id FROM (
			SELECT a.linkedin_account_id AS account_id, 0 AS rank, a.created_at AS at FROM linkedin_actions a
			WHERE a.organization_id = $1 AND a.campaign_id = $2 AND a.contact_id = $3 AND a.linkedin_account_id = ANY($4)
			UNION ALL
			SELECT s.linkedin_account_id, CASE WHEN s.connected_at IS NOT NULL THEN 1 ELSE 2 END, COALESCE(s.connected_at, s.invite_sent_at, s.updated_at)
			FROM linkedin_contact_state s
			WHERE s.organization_id = $1 AND s.contact_id = $3 AND s.linkedin_account_id = ANY($4)
			  AND (s.connected_at IS NOT NULL OR s.invite_pending)
		) c ORDER BY rank, at LIMIT 1`, orgID, campaignID, contactID, candidates).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (r *linkedInRepository) OpenActionsToday(ctx context.Context, orgID uuid.UUID, accountIDs []uuid.UUID, since time.Time) (map[uuid.UUID]map[string]int, error) {
	out := map[uuid.UUID]map[string]int{}
	rows, err := r.db.Query(ctx, `SELECT linkedin_account_id, type, count(*) FROM linkedin_actions
		WHERE organization_id = $1 AND linkedin_account_id = ANY($2) AND created_at >= $3 AND status IN ('pending', 'queued', 'running')
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

func (r *linkedInRepository) ContactActions(ctx context.Context, orgID, contactID uuid.UUID, limit int) ([]models.LinkedInAction, error) {
	query := `SELECT ` + linkedInActionColumns + ` FROM linkedin_actions a LEFT JOIN campaigns ca ON ca.id = a.campaign_id
		WHERE a.organization_id = $1 AND a.contact_id = $2 ORDER BY a.created_at DESC, a.id DESC LIMIT $3`
	rows, err := r.db.Query(ctx, query, orgID, contactID, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.LinkedInAction{}
	for rows.Next() {
		a, err := scanLinkedInAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (r *linkedInRepository) CampaignStats(ctx context.Context, orgID uuid.UUID, campaignIDs []uuid.UUID) (map[uuid.UUID]*models.LinkedInCampaignStats, error) {
	out := map[uuid.UUID]*models.LinkedInCampaignStats{}
	if len(campaignIDs) == 0 {
		return out, nil
	}
	query := `SELECT ca.id,
			count(*) FILTER (WHERE a.type = 'invite' AND a.status = 'succeeded'),
			count(*) FILTER (WHERE a.type = 'invite' AND a.status = 'succeeded' AND EXISTS (
				SELECT 1 FROM linkedin_contact_state s WHERE s.contact_id = a.contact_id AND s.linkedin_account_id = a.linkedin_account_id
				  AND s.organization_id = $1 AND s.connected_at IS NOT NULL AND s.connected_at >= a.created_at)),
			count(*) FILTER (WHERE a.type = 'message' AND a.status = 'succeeded'),
			count(*) FILTER (WHERE a.type = 'inmail' AND a.status = 'succeeded'),
			count(DISTINCT a.contact_id) FILTER (WHERE EXISTS (
				SELECT 1 FROM linkedin_contact_state s WHERE s.contact_id = a.contact_id AND s.linkedin_account_id = a.linkedin_account_id
				  AND s.organization_id = $1 AND s.replied_at IS NOT NULL AND s.replied_at >= a.created_at))
		FROM campaigns ca JOIN linkedin_actions a ON a.campaign_id = ca.id AND a.organization_id = $1
		WHERE ca.organization_id = $1 AND ca.id = ANY($2) GROUP BY ca.id`
	rows, err := r.db.Query(ctx, query, orgID, campaignIDs)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var st models.LinkedInCampaignStats
		if err := rows.Scan(&id, &st.InvitesSent, &st.InvitesAccepted, &st.MessagesSent, &st.InmailsSent, &st.Replies); err != nil {
			return nil, err
		}
		if st.InvitesSent > 0 {
			st.AcceptanceRate = float64(st.InvitesAccepted) / float64(st.InvitesSent) * 100
		}
		out[id] = &st
	}
	return out, rows.Err()
}

// ---- contact state --------------------------------------------------------

func (r *linkedInRepository) NoteContactState(ctx context.Context, orgID uuid.UUID, contactIDs []uuid.UUID, accountID uuid.UUID, c LinkedInStateChange) error {
	if len(contactIDs) == 0 {
		return nil
	}
	// Only the workspace's own contacts take a row.
	return r.exec(ctx, `INSERT INTO linkedin_contact_state (organization_id, contact_id, linkedin_account_id, invite_sent_at, invite_pending,
			connected_at, last_message_at, replied_at)
		SELECT $1, c.id, $3, $4, COALESCE($5, false) AND $6::timestamptz IS NULL, $6, $7, $8
		FROM contacts c WHERE c.organization_id = $1 AND c.id = ANY($2)
		  AND EXISTS (SELECT 1 FROM linkedin_accounts a WHERE a.id = $3 AND a.organization_id = $1)
		ON CONFLICT (contact_id, linkedin_account_id) DO UPDATE SET
			invite_sent_at = GREATEST(linkedin_contact_state.invite_sent_at, EXCLUDED.invite_sent_at),
			connected_at = COALESCE(linkedin_contact_state.connected_at, EXCLUDED.connected_at),
			invite_pending = CASE WHEN COALESCE(linkedin_contact_state.connected_at, EXCLUDED.connected_at) IS NOT NULL THEN false
				ELSE COALESCE($5, linkedin_contact_state.invite_pending) END,
			last_message_at = GREATEST(linkedin_contact_state.last_message_at, EXCLUDED.last_message_at),
			replied_at = GREATEST(linkedin_contact_state.replied_at, EXCLUDED.replied_at),
			updated_at = now()
		WHERE linkedin_contact_state.organization_id = EXCLUDED.organization_id`,
		orgID, contactIDs, accountID, c.InviteSentAt, c.InvitePending, c.ConnectedAt, c.MessageAt, c.RepliedAt)
}

func (r *linkedInRepository) ContactIDsByPublicID(ctx context.Context, orgID uuid.UUID, publicID string) ([]uuid.UUID, error) {
	out := []uuid.UUID{}
	if publicID == "" {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT id FROM contacts WHERE organization_id = $1 AND linkedin_public_id = $2 LIMIT 50`, orgID, publicID)
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

func (r *linkedInRepository) InviteStates(ctx context.Context, orgID uuid.UUID, publicIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(publicIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT c.linkedin_public_id, bool_or(s.connected_at IS NOT NULL)
		FROM contacts c JOIN linkedin_contact_state s ON s.contact_id = c.id AND s.organization_id = $1
		WHERE c.organization_id = $1 AND c.linkedin_public_id = ANY($2) AND (s.invite_pending OR s.connected_at IS NOT NULL)
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

func (r *linkedInRepository) ActiveCampaignsFor(ctx context.Context, orgID uuid.UUID, contactIDs []uuid.UUID) ([]string, error) {
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

func (r *linkedInRepository) ReplyCampaign(ctx context.Context, orgID, contactID uuid.UUID, at time.Time) (*uuid.UUID, *uuid.UUID, error) {
	var campaignID, sequenceID uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT p.campaign_id, p.sequence_id FROM campaign_contact_progress p
		JOIN campaigns ca ON ca.id = p.campaign_id AND ca.organization_id = $1
		WHERE p.contact_id = $2 AND p.sent_at IS NOT NULL AND p.sent_at <= $3
		  AND EXISTS (SELECT 1 FROM linkedin_actions a WHERE a.organization_id = $1 AND a.campaign_id = p.campaign_id
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

func (r *linkedInRepository) ClaimInboundReply(ctx context.Context, orgID, messageID uuid.UUID) (bool, error) {
	n, err := r.affected(ctx, `UPDATE linkedin_messages SET reply_handled_at = now()
		WHERE organization_id = $1 AND id = $2 AND direction = 'inbound' AND reply_handled_at IS NULL`, orgID, messageID)
	return n > 0, err
}

func (r *linkedInRepository) MessageID(ctx context.Context, orgID uuid.UUID, graphkitID string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT id FROM linkedin_messages WHERE organization_id = $1 AND graphkit_id = $2`, orgID, graphkitID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return id, err
}

func (r *linkedInRepository) UnreadConversations(ctx context.Context, orgID uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM linkedin_conversations c
		WHERE c.organization_id = $1 AND c.unread_count > 0
		  AND EXISTS (SELECT 1 FROM linkedin_accounts a WHERE a.id = c.linkedin_account_id AND a.organization_id = $1 AND a.removed_at IS NULL)`,
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

func (r *linkedInRepository) CreateImport(ctx context.Context, im *models.LinkedInImport) error {
	if im.ID == uuid.Nil {
		im.ID = uuid.New()
	}
	return r.exec(ctx, `INSERT INTO linkedin_imports (id, organization_id, user_id, status, request) VALUES ($1, $2, $3, $4, $5)`,
		im.ID, im.OrganizationID, im.UserID, im.Status, im.Request)
}

func (r *linkedInRepository) GetImport(ctx context.Context, orgID, id uuid.UUID) (*models.LinkedInImport, error) {
	return scanLinkedInImport(r.db.QueryRow(ctx, `SELECT `+linkedInImportColumns+` FROM linkedin_imports
		WHERE organization_id = $1 AND id = $2`, orgID, id))
}

func (r *linkedInRepository) ClaimImport(ctx context.Context, id *uuid.UUID, staleBefore time.Time) (*models.LinkedInImport, error) {
	return scanLinkedInImport(r.db.QueryRow(ctx, `UPDATE linkedin_imports SET status = 'running', updated_at = now()
		WHERE id = (SELECT id FROM linkedin_imports
			WHERE ($1::uuid IS NULL OR id = $1)
			  AND (status = 'queued' OR (status = 'running' AND updated_at < $2))
			ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING `+linkedInImportColumns, id, staleBefore))
}

func (r *linkedInRepository) SaveImport(ctx context.Context, im *models.LinkedInImport) error {
	return r.exec(ctx, `UPDATE linkedin_imports SET status = $3, total = $4, processed = $5, created = $6, updated = $7, skipped = $8,
			no_email = $9, emails_found = $10, contact_ids = $11, error = $12, updated_at = now()
		WHERE organization_id = $1 AND id = $2`,
		im.OrganizationID, im.ID, im.Status, im.Total, im.Processed, im.Created, im.Updated, im.Skipped, im.NoEmail, im.EmailsFound,
		im.ContactIDs, im.Error)
}

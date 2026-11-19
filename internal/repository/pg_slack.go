package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// ErrSlackLinkCodeInvalid means the code is unknown, expired or already used.
var ErrSlackLinkCodeInvalid = errors.New("slack link code invalid")

// ErrSlackLinkNotMember means the redeeming user is not an accepted member of
// the code's organization.
var ErrSlackLinkNotMember = errors.New("not a member of the code's organization")

// ErrSlackLinkEmailMismatch means the Slack account's email is not the
// redeeming user's Warmbly email.
var ErrSlackLinkEmailMismatch = errors.New("slack email does not match the user's email")

// SlackRepository persists the Slack app's member links, pending link codes and
// the assistant's thread map. Org-owned reads and writes are scoped by
// organization_id; lookups keyed by a Slack team or user resolve the org.
type SlackRepository interface {
	// GetLinkBySlackUser resolves the link for a Slack member (nil when none).
	GetLinkBySlackUser(ctx context.Context, teamID, slackUserID string) (*models.SlackUserLink, error)
	// GetLinkForUser returns a member's link in the org (nil when none).
	GetLinkForUser(ctx context.Context, orgID, userID uuid.UUID) (*models.SlackUserLink, error)
	ListLinks(ctx context.Context, orgID uuid.UUID) ([]models.SlackUserLink, error)
	SetLinkDMNotifications(ctx context.Context, orgID, userID uuid.UUID, on bool) (*models.SlackUserLink, error)
	DeleteLinkForUser(ctx context.Context, orgID, userID uuid.UUID) (bool, error)
	DeleteLink(ctx context.Context, orgID, linkID uuid.UUID) (*models.SlackUserLink, error)
	DeleteLinkBySlackUser(ctx context.Context, orgID uuid.UUID, teamID, slackUserID string) (bool, error)
	DeleteLinksForConnections(ctx context.Context, connectionIDs []uuid.UUID) error

	// CreateLinkCode stores a hashed code, replacing the member's earlier ones
	// and dropping expired codes.
	CreateLinkCode(ctx context.Context, codeHash []byte, c models.SlackLinkCode) error
	// PreviewLinkCode returns an unexpired code without consuming it.
	PreviewLinkCode(ctx context.Context, codeHash []byte) (*models.SlackLinkCode, error)
	// ConsumeLinkCode redeems a code for userID in one transaction: the code is
	// deleted and the link written only when userID is an accepted member and
	// either verified (Sign in with Slack proved the Slack account) or the
	// member's email is slackEmail, the code's Slack account's email.
	ConsumeLinkCode(ctx context.Context, codeHash []byte, userID uuid.UUID, slackEmail string, verified bool) (*models.SlackUserLink, error)
	PurgeExpiredLinkCodes(ctx context.Context) (int64, error)
	// LinkInstaller links a Warmbly member to a Slack account (the installer,
	// or a member matched by email) when neither is linked yet, the member is
	// an accepted member of the org, and slackEmail is the member's email.
	// Nil when nothing was written.
	LinkInstaller(ctx context.Context, orgID, connectionID uuid.UUID, teamID, slackUserID, slackEmail string, userID uuid.UUID) (*models.SlackUserLink, error)

	GetAgentThread(ctx context.Context, connectionID uuid.UUID, channelID, threadTS string) (*models.SlackAgentThread, error)
	GetAgentThreadByID(ctx context.Context, orgID, id uuid.UUID) (*models.SlackAgentThread, error)
	// CreateAgentThread inserts the mapping, returning the existing row when
	// the thread is already mapped.
	CreateAgentThread(ctx context.Context, t *models.SlackAgentThread) (*models.SlackAgentThread, error)
	SetAgentThreadApproval(ctx context.Context, orgID, id uuid.UUID, ts string) error
	// ReassignAgentThread hands a thread's assistant to another member with a
	// fresh session, clearing any pending approval card.
	ReassignAgentThread(ctx context.Context, orgID, id, userID, sessionID uuid.UUID) (*models.SlackAgentThread, error)

	GetInboxThread(ctx context.Context, orgID uuid.UUID, uniboxThreadID string) (*models.SlackInboxThread, error)
	GetInboxThreadByID(ctx context.Context, id uuid.UUID) (*models.SlackInboxThread, error)
	GetInboxThreadBySlack(ctx context.Context, connectionID uuid.UUID, channelID, threadTS string) (*models.SlackInboxThread, error)
	// UpsertInboxThread points a conversation at a Slack thread, replacing an
	// older mapping for the same conversation.
	UpsertInboxThread(ctx context.Context, t *models.SlackInboxThread) (*models.SlackInboxThread, error)
}

type slackRepository struct {
	DB *db.DB
}

func NewSlackRepository(database *db.DB) SlackRepository {
	return &slackRepository{DB: database}
}

const slackLinkCols = `l.id, l.organization_id, l.connection_id, l.slack_team_id, l.slack_user_id,
	l.user_id, l.dm_notifications, l.created_at, l.updated_at,
	TRIM(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), COALESCE(u.email, ''), COALESCE(u.contact_email, '')`

const slackLinkFrom = ` FROM slack_user_links l LEFT JOIN users u ON u.id = l.user_id `

func scanSlackLink(row scanner, l *models.SlackUserLink) error {
	var contact string
	if err := row.Scan(&l.ID, &l.OrganizationID, &l.ConnectionID, &l.SlackTeamID, &l.SlackUserID,
		&l.UserID, &l.DMNotifications, &l.CreatedAt, &l.UpdatedAt, &l.UserName, &l.UserEmail, &contact); err != nil {
		return err
	}
	l.UserEmail = models.VisibleEmail(l.UserEmail, contact)
	return nil
}

func (r *slackRepository) oneLink(ctx context.Context, where string, args ...any) (*models.SlackUserLink, error) {
	var l models.SlackUserLink
	if err := scanSlackLink(r.DB.QueryRow(ctx, `SELECT `+slackLinkCols+slackLinkFrom+where, args...), &l); err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &l, nil
}

func (r *slackRepository) GetLinkBySlackUser(ctx context.Context, teamID, slackUserID string) (*models.SlackUserLink, error) {
	return r.oneLink(ctx, `WHERE l.slack_team_id = $1 AND l.slack_user_id = $2`, teamID, slackUserID)
}

func (r *slackRepository) GetLinkForUser(ctx context.Context, orgID, userID uuid.UUID) (*models.SlackUserLink, error) {
	return r.oneLink(ctx, `WHERE l.organization_id = $1 AND l.user_id = $2 ORDER BY l.updated_at DESC LIMIT 1`, orgID, userID)
}

func (r *slackRepository) ListLinks(ctx context.Context, orgID uuid.UUID) ([]models.SlackUserLink, error) {
	rows, err := r.DB.Query(ctx, `SELECT `+slackLinkCols+slackLinkFrom+`
		WHERE l.organization_id = $1 ORDER BY l.created_at ASC, l.id ASC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.SlackUserLink{}
	for rows.Next() {
		var l models.SlackUserLink
		if err := scanSlackLink(rows, &l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *slackRepository) SetLinkDMNotifications(ctx context.Context, orgID, userID uuid.UUID, on bool) (*models.SlackUserLink, error) {
	tag, err := r.DB.Exec(ctx, `UPDATE slack_user_links SET dm_notifications = $3, updated_at = now()
		WHERE organization_id = $1 AND user_id = $2`, orgID, userID, on)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, nil
	}
	return r.GetLinkForUser(ctx, orgID, userID)
}

func (r *slackRepository) DeleteLinkForUser(ctx context.Context, orgID, userID uuid.UUID) (bool, error) {
	tag, err := r.DB.Exec(ctx, `DELETE FROM slack_user_links WHERE organization_id = $1 AND user_id = $2`, orgID, userID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *slackRepository) DeleteLink(ctx context.Context, orgID, linkID uuid.UUID) (*models.SlackUserLink, error) {
	l, err := r.oneLink(ctx, `WHERE l.organization_id = $1 AND l.id = $2`, orgID, linkID)
	if err != nil || l == nil {
		return nil, err
	}
	if _, err := r.DB.Exec(ctx, `DELETE FROM slack_user_links WHERE organization_id = $1 AND id = $2`, orgID, linkID); err != nil {
		return nil, err
	}
	return l, nil
}

func (r *slackRepository) DeleteLinkBySlackUser(ctx context.Context, orgID uuid.UUID, teamID, slackUserID string) (bool, error) {
	tag, err := r.DB.Exec(ctx, `DELETE FROM slack_user_links
		WHERE organization_id = $1 AND slack_team_id = $2 AND slack_user_id = $3`, orgID, teamID, slackUserID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *slackRepository) DeleteLinksForConnections(ctx context.Context, connectionIDs []uuid.UUID) error {
	if len(connectionIDs) == 0 {
		return nil
	}
	_, err := r.DB.Exec(ctx, `DELETE FROM slack_user_links WHERE connection_id = ANY($1)`, connectionIDs)
	return err
}

func (r *slackRepository) CreateLinkCode(ctx context.Context, codeHash []byte, c models.SlackLinkCode) error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM slack_link_codes
		WHERE expires_at < now() OR (slack_team_id = $1 AND slack_user_id = $2)`, c.SlackTeamID, c.SlackUserID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO slack_link_codes
		(code_hash, organization_id, connection_id, slack_team_id, slack_user_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		codeHash, c.OrganizationID, c.ConnectionID, c.SlackTeamID, c.SlackUserID, c.ExpiresAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *slackRepository) PreviewLinkCode(ctx context.Context, codeHash []byte) (*models.SlackLinkCode, error) {
	var c models.SlackLinkCode
	err := r.DB.QueryRow(ctx, `SELECT organization_id, connection_id, slack_team_id, slack_user_id, expires_at
		FROM slack_link_codes WHERE code_hash = $1 AND expires_at > now()`, codeHash).
		Scan(&c.OrganizationID, &c.ConnectionID, &c.SlackTeamID, &c.SlackUserID, &c.ExpiresAt)
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *slackRepository) ConsumeLinkCode(ctx context.Context, codeHash []byte, userID uuid.UUID, slackEmail string, verified bool) (*models.SlackUserLink, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var c models.SlackLinkCode
	err = tx.QueryRow(ctx, `DELETE FROM slack_link_codes WHERE code_hash = $1
		RETURNING organization_id, connection_id, slack_team_id, slack_user_id, expires_at`, codeHash).
		Scan(&c.OrganizationID, &c.ConnectionID, &c.SlackTeamID, &c.SlackUserID, &c.ExpiresAt)
	if err != nil {
		if isNoRows(err) {
			return nil, ErrSlackLinkCodeInvalid
		}
		return nil, err
	}
	if !c.ExpiresAt.After(time.Now()) {
		// Commit so the spent code is gone either way.
		_ = tx.Commit(ctx)
		return nil, ErrSlackLinkCodeInvalid
	}

	var member bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM organization_members
		WHERE organization_id = $1 AND user_id = $2 AND accepted_at IS NOT NULL)`, c.OrganizationID, userID).Scan(&member); err != nil {
		return nil, err
	}
	if !member {
		// Rolled back: the code stays redeemable by a real member.
		return nil, ErrSlackLinkNotMember
	}
	if !verified {
		var userEmail string
		if err := tx.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&userEmail); err != nil && !isNoRows(err) {
			return nil, err
		}
		if !models.SlackLinkEmailMatches(slackEmail, userEmail) {
			// Rolled back, like a non-member.
			return nil, ErrSlackLinkEmailMismatch
		}
	}

	// One Slack member per workspace and one link per member per connection.
	if _, err := tx.Exec(ctx, `DELETE FROM slack_user_links
		WHERE (slack_team_id = $1 AND slack_user_id = $2) OR (connection_id = $3 AND user_id = $4)`,
		c.SlackTeamID, c.SlackUserID, c.ConnectionID, userID); err != nil {
		return nil, err
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO slack_user_links
		(organization_id, connection_id, slack_team_id, slack_user_id, user_id)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		c.OrganizationID, c.ConnectionID, c.SlackTeamID, c.SlackUserID, userID).Scan(&id); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.oneLink(ctx, `WHERE l.organization_id = $1 AND l.id = $2`, c.OrganizationID, id)
}

func (r *slackRepository) LinkInstaller(ctx context.Context, orgID, connectionID uuid.UUID, teamID, slackUserID, slackEmail string, userID uuid.UUID) (*models.SlackUserLink, error) {
	var userEmail string
	if err := r.DB.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&userEmail); err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	if !models.SlackLinkEmailMatches(slackEmail, userEmail) {
		return nil, nil
	}
	var id uuid.UUID
	err := r.DB.QueryRow(ctx, `INSERT INTO slack_user_links
		(organization_id, connection_id, slack_team_id, slack_user_id, user_id)
		SELECT $1, $2, $3, $4, $5
		WHERE EXISTS (SELECT 1 FROM organization_members
			WHERE organization_id = $1 AND user_id = $5 AND accepted_at IS NOT NULL)
		AND EXISTS (SELECT 1 FROM integration_connections
			WHERE id = $2 AND organization_id = $1 AND provider = 'slack' AND external_account_id = $3)
		ON CONFLICT DO NOTHING
		RETURNING id`, orgID, connectionID, teamID, slackUserID, userID).Scan(&id)
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return r.oneLink(ctx, `WHERE l.organization_id = $1 AND l.id = $2`, orgID, id)
}

func (r *slackRepository) PurgeExpiredLinkCodes(ctx context.Context) (int64, error) {
	tag, err := r.DB.Exec(ctx, `DELETE FROM slack_link_codes WHERE expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

const slackThreadCols = `id, organization_id, connection_id, channel_id, thread_ts, session_id, user_id,
	COALESCE(approval_message_ts, ''), created_at, updated_at`

func scanSlackThread(row scanner) (*models.SlackAgentThread, error) {
	var t models.SlackAgentThread
	if err := row.Scan(&t.ID, &t.OrganizationID, &t.ConnectionID, &t.ChannelID, &t.ThreadTS, &t.SessionID,
		&t.UserID, &t.ApprovalMessageTS, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

func (r *slackRepository) GetAgentThread(ctx context.Context, connectionID uuid.UUID, channelID, threadTS string) (*models.SlackAgentThread, error) {
	return scanSlackThread(r.DB.QueryRow(ctx, `SELECT `+slackThreadCols+` FROM slack_agent_threads
		WHERE connection_id = $1 AND channel_id = $2 AND thread_ts = $3`, connectionID, channelID, threadTS))
}

func (r *slackRepository) GetAgentThreadByID(ctx context.Context, orgID, id uuid.UUID) (*models.SlackAgentThread, error) {
	return scanSlackThread(r.DB.QueryRow(ctx, `SELECT `+slackThreadCols+` FROM slack_agent_threads
		WHERE organization_id = $1 AND id = $2`, orgID, id))
}

func (r *slackRepository) CreateAgentThread(ctx context.Context, t *models.SlackAgentThread) (*models.SlackAgentThread, error) {
	row, err := scanSlackThread(r.DB.QueryRow(ctx, `INSERT INTO slack_agent_threads
		(organization_id, connection_id, channel_id, thread_ts, session_id, user_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (connection_id, channel_id, thread_ts) DO NOTHING
		RETURNING `+slackThreadCols,
		t.OrganizationID, t.ConnectionID, t.ChannelID, t.ThreadTS, t.SessionID, t.UserID))
	if err != nil || row != nil {
		return row, err
	}
	return r.GetAgentThread(ctx, t.ConnectionID, t.ChannelID, t.ThreadTS)
}

func (r *slackRepository) SetAgentThreadApproval(ctx context.Context, orgID, id uuid.UUID, ts string) error {
	_, err := r.DB.Exec(ctx, `UPDATE slack_agent_threads SET approval_message_ts = NULLIF($3, ''), updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, ts)
	return err
}

func (r *slackRepository) ReassignAgentThread(ctx context.Context, orgID, id, userID, sessionID uuid.UUID) (*models.SlackAgentThread, error) {
	return scanSlackThread(r.DB.QueryRow(ctx, `UPDATE slack_agent_threads
		SET user_id = $3, session_id = $4, approval_message_ts = NULL, updated_at = now()
		WHERE organization_id = $1 AND id = $2
		RETURNING `+slackThreadCols, orgID, id, userID, sessionID))
}

const slackInboxCols = `id, organization_id, connection_id, channel_id, thread_ts, unibox_thread_id, created_at, updated_at`

func scanSlackInbox(row scanner) (*models.SlackInboxThread, error) {
	var t models.SlackInboxThread
	if err := row.Scan(&t.ID, &t.OrganizationID, &t.ConnectionID, &t.ChannelID, &t.ThreadTS, &t.UniboxThreadID,
		&t.CreatedAt, &t.UpdatedAt); err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

func (r *slackRepository) GetInboxThread(ctx context.Context, orgID uuid.UUID, uniboxThreadID string) (*models.SlackInboxThread, error) {
	return scanSlackInbox(r.DB.QueryRow(ctx, `SELECT `+slackInboxCols+` FROM slack_inbox_threads
		WHERE organization_id = $1 AND unibox_thread_id = $2`, orgID, uniboxThreadID))
}

func (r *slackRepository) GetInboxThreadByID(ctx context.Context, id uuid.UUID) (*models.SlackInboxThread, error) {
	return scanSlackInbox(r.DB.QueryRow(ctx, `SELECT `+slackInboxCols+` FROM slack_inbox_threads WHERE id = $1`, id))
}

func (r *slackRepository) GetInboxThreadBySlack(ctx context.Context, connectionID uuid.UUID, channelID, threadTS string) (*models.SlackInboxThread, error) {
	return scanSlackInbox(r.DB.QueryRow(ctx, `SELECT `+slackInboxCols+` FROM slack_inbox_threads
		WHERE connection_id = $1 AND channel_id = $2 AND thread_ts = $3
		ORDER BY updated_at DESC LIMIT 1`, connectionID, channelID, threadTS))
}

func (r *slackRepository) UpsertInboxThread(ctx context.Context, t *models.SlackInboxThread) (*models.SlackInboxThread, error) {
	return scanSlackInbox(r.DB.QueryRow(ctx, `INSERT INTO slack_inbox_threads
		(organization_id, connection_id, channel_id, thread_ts, unibox_thread_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (organization_id, unibox_thread_id) DO UPDATE SET
			connection_id = EXCLUDED.connection_id, channel_id = EXCLUDED.channel_id,
			thread_ts = EXCLUDED.thread_ts, updated_at = now()
		RETURNING `+slackInboxCols,
		t.OrganizationID, t.ConnectionID, t.ChannelID, t.ThreadTS, t.UniboxThreadID))
}

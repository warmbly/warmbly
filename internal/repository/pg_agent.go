package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// AgentRepository persists dashboard-agent sessions, their transcript, and the
// per-org tool approval policies. Sessions are per-user; every read is scoped by
// (org_id, user_id) so one member can never see another's conversations.
type AgentRepository interface {
	CreateSession(ctx context.Context, orgID, userID uuid.UUID, title string, sctx models.AgentSessionContext) (*models.AgentSession, error)
	GetSession(ctx context.Context, orgID, userID, sessionID uuid.UUID) (*models.AgentSession, error)
	ListSessions(ctx context.Context, orgID, userID uuid.UUID, limit int, beforeCreatedAt time.Time, beforeID uuid.UUID) ([]models.AgentSession, error)
	// The transcript/context mutators are scoped by (org_id, user_id) at the
	// SQL layer too, not only via the caller's prior GetSession, so a mis-wired
	// future caller can never touch another member's session by raw id.
	UpdateSessionContext(ctx context.Context, orgID, userID, sessionID uuid.UUID, sctx models.AgentSessionContext) error
	UpdateSessionTitle(ctx context.Context, orgID, userID, sessionID uuid.UUID, title string) error

	AppendMessages(ctx context.Context, orgID, userID, sessionID uuid.UUID, msgs []models.AgentMessageRow) error
	LoadTranscript(ctx context.Context, orgID, userID, sessionID uuid.UUID) ([]models.AgentMessageRow, error)

	GetToolPolicies(ctx context.Context, orgID uuid.UUID) (map[string]string, error)
	SetToolPolicy(ctx context.Context, orgID uuid.UUID, toolName, decision string, createdBy uuid.UUID) error
}

type agentRepository struct {
	DB *db.DB
}

func NewAgentRepository(database *db.DB) AgentRepository {
	return &agentRepository{DB: database}
}

const agentSessionCols = `id, org_id, user_id, title, context, created_at, updated_at`

func scanSession(row pgx.Row, s *models.AgentSession) error {
	var ctxRaw []byte
	if err := row.Scan(&s.ID, &s.OrgID, &s.UserID, &s.Title, &ctxRaw, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return err
	}
	if len(ctxRaw) > 0 {
		if err := json.Unmarshal(ctxRaw, &s.Context); err != nil {
			return err
		}
	}
	return nil
}

func (r *agentRepository) CreateSession(ctx context.Context, orgID, userID uuid.UUID, title string, sctx models.AgentSessionContext) (*models.AgentSession, error) {
	ctxRaw, err := json.Marshal(sctx)
	if err != nil {
		return nil, err
	}
	s := &models.AgentSession{}
	err = scanSession(r.DB.QueryRow(ctx, `
		INSERT INTO agent_sessions (org_id, user_id, title, context)
		VALUES ($1, $2, $3, $4)
		RETURNING `+agentSessionCols, orgID, userID, title, ctxRaw), s)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (r *agentRepository) GetSession(ctx context.Context, orgID, userID, sessionID uuid.UUID) (*models.AgentSession, error) {
	s := &models.AgentSession{}
	err := scanSession(r.DB.QueryRow(ctx,
		`SELECT `+agentSessionCols+` FROM agent_sessions WHERE id = $1 AND org_id = $2 AND user_id = $3`,
		sessionID, orgID, userID), s)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return s, nil
}

func (r *agentRepository) ListSessions(ctx context.Context, orgID, userID uuid.UUID, limit int, beforeCreatedAt time.Time, beforeID uuid.UUID) ([]models.AgentSession, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	query := `
		SELECT ` + agentSessionCols + `
		FROM agent_sessions
		WHERE org_id = $1 AND user_id = $2
		ORDER BY created_at DESC, id DESC
		LIMIT $3`
	args := []any{orgID, userID, limit}
	if !beforeCreatedAt.IsZero() {
		query = `
		SELECT ` + agentSessionCols + `
		FROM agent_sessions
		WHERE org_id = $1 AND user_id = $2 AND (created_at, id) < ($4, $5)
		ORDER BY created_at DESC, id DESC
		LIMIT $3`
		args = append(args, beforeCreatedAt, beforeID)
	}
	rows, err := r.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.AgentSession, 0)
	for rows.Next() {
		var s models.AgentSession
		if err := scanSession(rows, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *agentRepository) UpdateSessionContext(ctx context.Context, orgID, userID, sessionID uuid.UUID, sctx models.AgentSessionContext) error {
	ctxRaw, err := json.Marshal(sctx)
	if err != nil {
		return err
	}
	_, err = r.DB.Exec(ctx, `UPDATE agent_sessions SET context = $2, updated_at = now() WHERE id = $1 AND org_id = $3 AND user_id = $4`, sessionID, ctxRaw, orgID, userID)
	return err
}

func (r *agentRepository) UpdateSessionTitle(ctx context.Context, orgID, userID, sessionID uuid.UUID, title string) error {
	_, err := r.DB.Exec(ctx, `UPDATE agent_sessions SET title = $2, updated_at = now() WHERE id = $1 AND title = '' AND org_id = $3 AND user_id = $4`, sessionID, title, orgID, userID)
	return err
}

func (r *agentRepository) AppendMessages(ctx context.Context, orgID, userID, sessionID uuid.UUID, msgs []models.AgentMessageRow) error {
	if len(msgs) == 0 {
		return nil
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Verify the session belongs to this member before appending, so a raw
	// session id from a mis-wired caller can't write into another org's log.
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agent_sessions WHERE id = $1 AND org_id = $2 AND user_id = $3)`, sessionID, orgID, userID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return errors.New("session not found for this member")
	}
	for _, m := range msgs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO agent_messages (session_id, role, content, tokens)
			VALUES ($1, $2, $3, $4)`, sessionID, m.Role, []byte(m.Content), m.Tokens); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE agent_sessions SET updated_at = now() WHERE id = $1`, sessionID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *agentRepository) LoadTranscript(ctx context.Context, orgID, userID, sessionID uuid.UUID) ([]models.AgentMessageRow, error) {
	rows, err := r.DB.Query(ctx, `
		SELECT id, session_id, role, content, tokens, created_at
		FROM agent_messages
		WHERE session_id = $1
		  AND EXISTS (SELECT 1 FROM agent_sessions WHERE id = $1 AND org_id = $2 AND user_id = $3)
		ORDER BY created_at ASC, id ASC`, sessionID, orgID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.AgentMessageRow, 0)
	for rows.Next() {
		var m models.AgentMessageRow
		var content []byte
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &content, &m.Tokens, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Content = content
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *agentRepository) GetToolPolicies(ctx context.Context, orgID uuid.UUID) (map[string]string, error) {
	rows, err := r.DB.Query(ctx, `SELECT tool_name, decision FROM ai_tool_policies WHERE org_id = $1`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var name, decision string
		if err := rows.Scan(&name, &decision); err != nil {
			return nil, err
		}
		out[name] = decision
	}
	return out, rows.Err()
}

func (r *agentRepository) SetToolPolicy(ctx context.Context, orgID uuid.UUID, toolName, decision string, createdBy uuid.UUID) error {
	_, err := r.DB.Exec(ctx, `
		INSERT INTO ai_tool_policies (org_id, tool_name, decision, created_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (org_id, tool_name) DO UPDATE SET decision = EXCLUDED.decision`,
		orgID, toolName, decision, createdBy)
	return err
}

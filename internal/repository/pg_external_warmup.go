package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// ExternalWarmupRepository reads each workspace's rule for other services' warmup and removes the stored mail it matches.
type ExternalWarmupRepository struct {
	db *pgxpool.Pool
}

func NewExternalWarmupRepository(db *pgxpool.Pool) *ExternalWarmupRepository {
	return &ExternalWarmupRepository{db: db}
}

// ExternalWarmupRule is the organization's rule; the zero rule when it has none.
func (r *ExternalWarmupRepository) ExternalWarmupRule(ctx context.Context, orgID uuid.UUID) (models.ExternalWarmupSettings, error) {
	var raw []byte
	err := r.db.QueryRow(ctx, `SELECT settings->'external_warmup' FROM outreach_settings WHERE organization_id = $1`, orgID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.ExternalWarmupSettings{}, nil
	}
	if err != nil {
		return models.ExternalWarmupSettings{}, err
	}
	return decodeExternalWarmup(raw)
}

// ExternalWarmupRules is every organization with a rule that can hide something.
func (r *ExternalWarmupRepository) ExternalWarmupRules(ctx context.Context) (map[uuid.UUID]models.ExternalWarmupSettings, error) {
	rows, err := r.db.Query(ctx, `
		SELECT organization_id, settings->'external_warmup' FROM outreach_settings
		WHERE settings->'external_warmup'->>'hide_warmbly_marked' = 'true'
		   OR jsonb_typeof(settings->'external_warmup'->'tags') = 'array'
		  AND jsonb_array_length(settings->'external_warmup'->'tags') > 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]models.ExternalWarmupSettings{}
	for rows.Next() {
		var id uuid.UUID
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		rule, err := decodeExternalWarmup(raw)
		if err != nil {
			return nil, err
		}
		if rule.Enabled() {
			out[id] = rule
		}
	}
	return out, rows.Err()
}

func decodeExternalWarmup(raw []byte) (models.ExternalWarmupSettings, error) {
	var rule models.ExternalWarmupSettings
	if len(raw) == 0 || string(raw) == "null" {
		return rule, nil
	}
	return rule, json.Unmarshal(raw, &rule)
}

// OrganizationMailboxIDs lists the mailboxes a sweep walks.
func (r *ExternalWarmupRepository) OrganizationMailboxIDs(ctx context.Context, orgID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `SELECT id FROM email_accounts WHERE organization_id = $1 ORDER BY id`, orgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// externalWarmupMatchSQL mirrors models.ExternalWarmupSettings.Matches: $3 marker switch, $4 lower-cased header names, $5 lower-cased tags.
const externalWarmupMatchSQL = `(
	($3 AND EXISTS (
		SELECT 1 FROM unnest(u.flags) f
		WHERE strpos(f, ':') > 0
		  AND lower(btrim(split_part(f, ':', 1))) = ANY($4::text[])
		  AND btrim(substr(f, strpos(f, ':') + 1)) <> ''))
	OR EXISTS (
		SELECT 1 FROM unnest($5::text[]) t
		WHERE strpos(lower(COALESCE(u.subject, '') || E'\n' || COALESCE(u.snippet, '') || E'\n' || COALESCE(u.body_text, '')), t) > 0)
)`

// DeleteExternalWarmup removes up to limit of one mailbox's stored messages the rule matches, scoped to the organization.
func (r *ExternalWarmupRepository) DeleteExternalWarmup(ctx context.Context, orgID, emailID uuid.UUID, rule models.ExternalWarmupSettings, limit int) ([]models.JobEventNewEmail, error) {
	headers := make([]string, 0, len(models.WarmupMarkerHeaders))
	for _, h := range models.WarmupMarkerHeaders {
		headers = append(headers, strings.ToLower(h))
	}
	rows, err := r.db.Query(ctx, `
		DELETE FROM unibox_emails WHERE id IN (
			SELECT u.id FROM unibox_emails u
			JOIN email_accounts ea ON ea.id = u.email_id AND ea.organization_id = $1
			WHERE u.email_id = $2 AND `+externalWarmupMatchSQL+`
			LIMIT $6)
		RETURNING user_id, id, email_id, COALESCE(thread_id, ''), COALESCE(subject, ''), folder`,
		orgID, emailID, rule.HideWarmblyMarked, headers, rule.LowerTags(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.JobEventNewEmail
	for rows.Next() {
		m := &models.EmailMessageStoreData{}
		e := models.JobEventNewEmail{Message: m}
		if err := rows.Scan(&e.UserID, &m.ID, &m.EmailID, &m.ThreadID, &m.Subject, &m.Folder); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/models"
)

func (r *outreachRepository) EnqueueOutcome(ctx context.Context, ev *models.OutreachOutcome) error {
	if ev.ID == uuid.Nil {
		ev.ID = uuid.New()
	}
	if ev.EventID == uuid.Nil {
		ev.EventID = uuid.New()
	}
	now := time.Now().UTC()
	ev.CreatedAt = now
	ev.UpdatedAt = now
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = now
	}
	if ev.NextAttemptAt.IsZero() {
		ev.NextAttemptAt = now
	}
	payload := ev.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO outreach_outcome_outbox (
			id, organization_id, event_id, idempotency_key, source_lead_id, cnpj14,
			contact_email, event_type, payload, occurred_at, attempts, next_attempt_at,
			created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,0,$11,$12,$12)`,
		ev.ID, ev.OrganizationID, ev.EventID, ev.IdempotencyKey, ev.SourceLeadID, ev.CNPJ14,
		ev.ContactEmail, ev.EventType, payload, ev.OccurredAt, ev.NextAttemptAt, now,
	)
	return err
}

func (r *outreachRepository) ListPendingOutcomes(ctx context.Context, limit int) ([]models.OutreachOutcome, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, organization_id, event_id, idempotency_key, COALESCE(source_lead_id,''), COALESCE(cnpj14,''),
			COALESCE(contact_email,''), event_type, payload, occurred_at, attempts, next_attempt_at,
			delivered_at, COALESCE(last_error,''), dead_letter, created_at, updated_at
		FROM outreach_outcome_outbox
		WHERE delivered_at IS NULL AND dead_letter = false AND next_attempt_at <= now()
		ORDER BY next_attempt_at ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.OutreachOutcome
	for rows.Next() {
		var ev models.OutreachOutcome
		if err := rows.Scan(
			&ev.ID, &ev.OrganizationID, &ev.EventID, &ev.IdempotencyKey, &ev.SourceLeadID, &ev.CNPJ14,
			&ev.ContactEmail, &ev.EventType, &ev.Payload, &ev.OccurredAt, &ev.Attempts, &ev.NextAttemptAt,
			&ev.DeliveredAt, &ev.LastError, &ev.DeadLetter, &ev.CreatedAt, &ev.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (r *outreachRepository) MarkOutcomeDelivered(ctx context.Context, orgID, id uuid.UUID) error {
	ct, err := r.db.Exec(ctx, `
		UPDATE outreach_outcome_outbox SET delivered_at=now(), updated_at=now(), last_error=''
		WHERE organization_id=$1 AND id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *outreachRepository) MarkOutcomeAttempt(ctx context.Context, orgID, id uuid.UUID, attempts int, next time.Time, lastErr string, dead bool) error {
	_, err := r.db.Exec(ctx, `
		UPDATE outreach_outcome_outbox SET
			attempts=$3, next_attempt_at=$4, last_error=$5, dead_letter=$6, updated_at=now()
		WHERE organization_id=$1 AND id=$2`,
		orgID, id, attempts, next, lastErr, dead,
	)
	return err
}

func (r *outreachRepository) GetOutcomeByIdempotency(ctx context.Context, orgID uuid.UUID, key string) (*models.OutreachOutcome, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, organization_id, event_id, idempotency_key, COALESCE(source_lead_id,''), COALESCE(cnpj14,''),
			COALESCE(contact_email,''), event_type, payload, occurred_at, attempts, next_attempt_at,
			delivered_at, COALESCE(last_error,''), dead_letter, created_at, updated_at
		FROM outreach_outcome_outbox WHERE organization_id=$1 AND idempotency_key=$2`, orgID, key)
	var ev models.OutreachOutcome
	err := row.Scan(
		&ev.ID, &ev.OrganizationID, &ev.EventID, &ev.IdempotencyKey, &ev.SourceLeadID, &ev.CNPJ14,
		&ev.ContactEmail, &ev.EventType, &ev.Payload, &ev.OccurredAt, &ev.Attempts, &ev.NextAttemptAt,
		&ev.DeliveredAt, &ev.LastError, &ev.DeadLetter, &ev.CreatedAt, &ev.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ev, nil
}

// FindCandidateByEmail returns the recommended-or-latest candidate for an email in the org,
// plus its account. Used to attribute replies/bounces back to confenge staging.
func (r *outreachRepository) FindCandidateByEmail(ctx context.Context, orgID uuid.UUID, email string) (*models.OutreachContactCandidate, *models.OutreachAccount, error) {
	row := r.db.QueryRow(ctx, outreachCandidateSelect+`
		FROM outreach_contact_candidates
		WHERE organization_id=$1 AND lower(email)=lower($2)
		ORDER BY recommended DESC, updated_at DESC
		LIMIT 1`, orgID, email)
	c, err := scanCandidate(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	acc, err := r.GetAccount(ctx, orgID, c.AccountID)
	if err != nil {
		return c, nil, err
	}
	return c, acc, nil
}

// FindCandidateByEnrollment resolves the exact candidate that created a campaign membership.
func (r *outreachRepository) FindCandidateByEnrollment(ctx context.Context, orgID, campaignID, contactID uuid.UUID) (*models.OutreachContactCandidate, *models.OutreachAccount, error) {
	var candidateID uuid.UUID
	err := r.db.QueryRow(ctx, `
		SELECT contact_candidate_id
		FROM outreach_drafts
		WHERE organization_id=$1 AND campaign_id=$2 AND enrollment_contact_id=$3
		  AND contact_candidate_id IS NOT NULL
		ORDER BY updated_at DESC
		LIMIT 1`, orgID, campaignID, contactID).Scan(&candidateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	cand, err := r.GetCandidate(ctx, orgID, candidateID)
	if err != nil || cand == nil {
		return cand, nil, err
	}
	acc, err := r.GetAccount(ctx, orgID, cand.AccountID)
	return cand, acc, err
}

func (r *outreachRepository) GetTouchpointByEnrollment(ctx context.Context, orgID, campaignID, contactID uuid.UUID) (*models.OutreachTouchpoint, error) {
	row := r.db.QueryRow(ctx, outreachTouchpointSelect+`
		FROM outreach_touchpoints t
		JOIN outreach_drafts d ON d.id=t.draft_id AND d.organization_id=t.organization_id
		WHERE t.organization_id=$1 AND d.campaign_id=$2 AND d.enrollment_contact_id=$3
		ORDER BY t.updated_at DESC
		LIMIT 1`, orgID, campaignID, contactID)
	touchpoint, err := scanTouchpoint(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return touchpoint, err
}

// FindCandidateByPhone matches phone_e164 or raw phone (digits-insensitive).
func (r *outreachRepository) FindCandidateByPhone(ctx context.Context, orgID uuid.UUID, phone string) (*models.OutreachContactCandidate, *models.OutreachAccount, error) {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return nil, nil, nil
	}
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)
	row := r.db.QueryRow(ctx, outreachCandidateSelect+`
		FROM outreach_contact_candidates
		WHERE organization_id=$1 AND (
			phone_e164 = $2 OR phone_e164 = $3 OR phone = $2 OR phone = $3
			OR regexp_replace(COALESCE(phone_e164,''), '[^0-9]', '', 'g') = $4
			OR regexp_replace(COALESCE(phone,''), '[^0-9]', '', 'g') = $4
		)
		ORDER BY recommended DESC, updated_at DESC
		LIMIT 1`, orgID, phone, "+"+digits, digits)
	c, err := scanCandidate(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	acc, err := r.GetAccount(ctx, orgID, c.AccountID)
	if err != nil {
		return c, nil, err
	}
	return c, acc, nil
}

// GetOrgOwnerUserID returns organizations.owner_user_id for system CRM attribution.
func (r *outreachRepository) GetOrgOwnerUserID(ctx context.Context, orgID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT owner_user_id FROM organizations WHERE id=$1`, orgID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return id, err
}

// GetLatestOutcomeForLead returns the newest outcome for cockpit projection.
func (r *outreachRepository) GetLatestOutcomeForLead(ctx context.Context, orgID uuid.UUID, cnpj14, sourceLeadID, contactEmail string) (*models.OutreachOutcome, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, organization_id, event_id, idempotency_key, COALESCE(source_lead_id,''), COALESCE(cnpj14,''),
			COALESCE(contact_email,''), event_type, payload, occurred_at, attempts, next_attempt_at,
			delivered_at, COALESCE(last_error,''), dead_letter, created_at, updated_at
		FROM outreach_outcome_outbox
		WHERE organization_id=$1
		  AND (
			($2 <> '' AND cnpj14=$2) OR
			($3 <> '' AND source_lead_id=$3) OR
			($4 <> '' AND lower(contact_email)=lower($4))
		  )
		  AND event_type IN ('REPLIED','DO_NOT_CONTACT','MEETING','PROPOSAL')
		ORDER BY occurred_at DESC
		LIMIT 1`, orgID, cnpj14, sourceLeadID, contactEmail)
	var ev models.OutreachOutcome
	err := row.Scan(
		&ev.ID, &ev.OrganizationID, &ev.EventID, &ev.IdempotencyKey, &ev.SourceLeadID, &ev.CNPJ14,
		&ev.ContactEmail, &ev.EventType, &ev.Payload, &ev.OccurredAt, &ev.Attempts, &ev.NextAttemptAt,
		&ev.DeliveredAt, &ev.LastError, &ev.DeadLetter, &ev.CreatedAt, &ev.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ev, nil
}

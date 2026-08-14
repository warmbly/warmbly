package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/models"
)

const outreachTouchpointSelect = `
	SELECT id, organization_id, account_id, contact_candidate_id,
		ordinal, COALESCE(cadence_step,''), COALESCE(channel,'EMAIL'), COALESCE(purpose,''),
		due_at, state, draft_id,
		COALESCE(recipient,''), COALESCE(subject,''), COALESCE(body_text,''),
		COALESCE(content_hash,''), COALESCE(approved_content_hash,''), approved_by, approved_at,
		COALESCE(authorization_mode,''),
		campaign_policy_authorization_id, COALESCE(authorization_policy_hash,''), authorization_at,
		COALESCE(signature_version,''),
		queued_at, sent_at, COALESCE(provider_message_id,''), COALESCE(stop_reason,''),
		previous_touchpoint_id, COALESCE(idempotency_key,''),
		COALESCE(policy_version,''), COALESCE(service_code,''), COALESCE(fact_used,''), evidence_ids,
		COALESCE(generated_context_hash,''),
		created_at, updated_at `

func scanTouchpoint(row scannable) (*models.OutreachTouchpoint, error) {
	var t models.OutreachTouchpoint
	var evid []byte
	err := row.Scan(
		&t.ID, &t.OrganizationID, &t.AccountID, &t.ContactCandidateID,
		&t.Ordinal, &t.CadenceStep, &t.Channel, &t.Purpose,
		&t.DueAt, &t.State, &t.DraftID,
		&t.Recipient, &t.Subject, &t.BodyText,
		&t.ContentHash, &t.ApprovedContentHash, &t.ApprovedBy, &t.ApprovedAt,
		&t.AuthorizationMode,
		&t.CampaignPolicyAuthorizationID, &t.AuthorizationPolicyHash, &t.AuthorizationAt,
		&t.SignatureVersion,
		&t.QueuedAt, &t.SentAt, &t.ProviderMessageID, &t.StopReason,
		&t.PreviousTouchpointID, &t.IdempotencyKey,
		&t.PolicyVersion, &t.ServiceCode, &t.FactUsed, &evid,
		&t.GeneratedContextHash,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(evid, &t.EvidenceIDs)
	return &t, nil
}

func (r *outreachRepository) InsertTouchpoint(ctx context.Context, t *models.OutreachTouchpoint) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	if t.DueAt.IsZero() {
		t.DueAt = now
	}
	if t.Channel == "" {
		t.Channel = models.OutreachChannelEmail
	}
	if t.PolicyVersion == "" {
		t.PolicyVersion = models.CadencePolicyVersionV1
	}
	evid, _ := json.Marshal(t.EvidenceIDs)
	if evid == nil {
		evid = []byte("[]")
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO outreach_touchpoints (
			id, organization_id, account_id, contact_candidate_id,
			ordinal, cadence_step, channel, purpose, due_at, state, draft_id,
			recipient, subject, body_text,
			content_hash, approved_content_hash, approved_by, approved_at,
			authorization_mode,
			campaign_policy_authorization_id, authorization_policy_hash, authorization_at, signature_version,
			queued_at, sent_at, provider_message_id, stop_reason,
			previous_touchpoint_id, idempotency_key,
			policy_version, service_code, fact_used, evidence_ids,
			generated_context_hash,
			created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36
		)`,
		t.ID, t.OrganizationID, t.AccountID, t.ContactCandidateID,
		t.Ordinal, t.CadenceStep, t.Channel, t.Purpose, t.DueAt, t.State, t.DraftID,
		t.Recipient, t.Subject, t.BodyText,
		t.ContentHash, t.ApprovedContentHash, t.ApprovedBy, t.ApprovedAt,
		t.AuthorizationMode,
		t.CampaignPolicyAuthorizationID, t.AuthorizationPolicyHash, t.AuthorizationAt, t.SignatureVersion,
		t.QueuedAt, t.SentAt, t.ProviderMessageID, t.StopReason,
		t.PreviousTouchpointID, t.IdempotencyKey,
		t.PolicyVersion, t.ServiceCode, t.FactUsed, evid,
		t.GeneratedContextHash,
		t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (r *outreachRepository) UpdateTouchpoint(ctx context.Context, t *models.OutreachTouchpoint) error {
	t.UpdatedAt = time.Now().UTC()
	evid, _ := json.Marshal(t.EvidenceIDs)
	if evid == nil {
		evid = []byte("[]")
	}
	ct, err := r.db.Exec(ctx, `
		UPDATE outreach_touchpoints SET
			contact_candidate_id=$3, channel=$4, purpose=$5, due_at=$6, state=$7, draft_id=$8,
			recipient=$9, subject=$10, body_text=$11,
			content_hash=$12, approved_content_hash=$13, approved_by=$14, approved_at=$15,
			authorization_mode=$16,
			campaign_policy_authorization_id=$17, authorization_policy_hash=$18, authorization_at=$19, signature_version=$20,
			queued_at=$21, sent_at=$22, provider_message_id=$23, stop_reason=$24,
			service_code=$25, fact_used=$26, evidence_ids=$27, generated_context_hash=$28, updated_at=$29
		WHERE organization_id=$1 AND id=$2`,
		t.OrganizationID, t.ID,
		t.ContactCandidateID, t.Channel, t.Purpose, t.DueAt, t.State, t.DraftID,
		t.Recipient, t.Subject, t.BodyText,
		t.ContentHash, t.ApprovedContentHash, t.ApprovedBy, t.ApprovedAt,
		t.AuthorizationMode,
		t.CampaignPolicyAuthorizationID, t.AuthorizationPolicyHash, t.AuthorizationAt, t.SignatureVersion,
		t.QueuedAt, t.SentAt, t.ProviderMessageID, t.StopReason,
		t.ServiceCode, t.FactUsed, evid, t.GeneratedContextHash, t.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errors.New("touchpoint not found")
	}
	return nil
}

func (r *outreachRepository) GetTouchpoint(ctx context.Context, orgID, id uuid.UUID) (*models.OutreachTouchpoint, error) {
	row := r.db.QueryRow(ctx, outreachTouchpointSelect+` FROM outreach_touchpoints WHERE organization_id=$1 AND id=$2`, orgID, id)
	t, err := scanTouchpoint(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func (r *outreachRepository) GetTouchpointByIdempotency(ctx context.Context, orgID uuid.UUID, key string) (*models.OutreachTouchpoint, error) {
	if key == "" {
		return nil, nil
	}
	row := r.db.QueryRow(ctx, outreachTouchpointSelect+` FROM outreach_touchpoints WHERE organization_id=$1 AND idempotency_key=$2`, orgID, key)
	t, err := scanTouchpoint(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func (r *outreachRepository) GetTouchpointByDraft(ctx context.Context, orgID, draftID uuid.UUID) (*models.OutreachTouchpoint, error) {
	row := r.db.QueryRow(ctx, outreachTouchpointSelect+` FROM outreach_touchpoints WHERE organization_id=$1 AND draft_id=$2 ORDER BY updated_at DESC LIMIT 1`, orgID, draftID)
	t, err := scanTouchpoint(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func (r *outreachRepository) ListTouchpoints(ctx context.Context, orgID, accountID uuid.UUID, state string, limit, offset int) ([]models.OutreachTouchpoint, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var rows pgx.Rows
	var err error
	if state != "" {
		rows, err = r.db.Query(ctx, outreachTouchpointSelect+` FROM outreach_touchpoints WHERE organization_id=$1 AND account_id=$2 AND state=$3 ORDER BY ordinal ASC LIMIT $4 OFFSET $5`, orgID, accountID, state, limit, offset)
	} else {
		rows, err = r.db.Query(ctx, outreachTouchpointSelect+` FROM outreach_touchpoints WHERE organization_id=$1 AND account_id=$2 ORDER BY ordinal ASC LIMIT $3 OFFSET $4`, orgID, accountID, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectTouchpoints(rows)
}

func (r *outreachRepository) ListReviewTouchpoints(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]models.OutreachTouchpoint, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	// Join account so the review queue can show company context without N+1.
	const q = `
		SELECT t.id, t.organization_id, t.account_id, t.contact_candidate_id,
			t.ordinal, COALESCE(t.cadence_step,''), COALESCE(t.channel,'EMAIL'), COALESCE(t.purpose,''),
			t.due_at, t.state, t.draft_id,
			COALESCE(t.recipient,''), COALESCE(t.subject,''), COALESCE(t.body_text,''),
			COALESCE(t.content_hash,''), COALESCE(t.approved_content_hash,''), t.approved_by, t.approved_at,
			t.queued_at, t.sent_at, COALESCE(t.provider_message_id,''), COALESCE(t.stop_reason,''),
			t.previous_touchpoint_id, COALESCE(t.idempotency_key,''),
			COALESCE(t.policy_version,''), COALESCE(t.service_code,''), COALESCE(t.fact_used,''), t.evidence_ids,
			t.created_at, t.updated_at,
			a.id, a.organization_id, COALESCE(a.cnpj14,''), COALESCE(a.razao_social,''), COALESCE(a.nome_fantasia,''),
			COALESCE(a.municipio,''), COALESCE(a.uf,''), COALESCE(a.service_code,''), COALESCE(a.queue_state,''),
			COALESCE(a.fact_to_mention,''), a.blocked, a.do_not_contact
		FROM outreach_touchpoints t
		LEFT JOIN outreach_accounts a ON a.id = t.account_id AND a.organization_id = t.organization_id
		WHERE t.organization_id=$1 AND t.state IN ('DUE','DRAFTED','NEEDS_REVIEW','APPROVED')
		ORDER BY t.due_at ASC, t.ordinal ASC
		LIMIT $2 OFFSET $3`
	rows, err := r.db.Query(ctx, q, orgID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.OutreachTouchpoint
	for rows.Next() {
		var t models.OutreachTouchpoint
		var evid []byte
		var accID *uuid.UUID
		var accOrg *uuid.UUID
		var cnpj, razao, fantasia, mun, uf, svc, qstate, fact string
		var blocked, dnc bool
		err := rows.Scan(
			&t.ID, &t.OrganizationID, &t.AccountID, &t.ContactCandidateID,
			&t.Ordinal, &t.CadenceStep, &t.Channel, &t.Purpose,
			&t.DueAt, &t.State, &t.DraftID,
			&t.Recipient, &t.Subject, &t.BodyText,
			&t.ContentHash, &t.ApprovedContentHash, &t.ApprovedBy, &t.ApprovedAt,
			&t.QueuedAt, &t.SentAt, &t.ProviderMessageID, &t.StopReason,
			&t.PreviousTouchpointID, &t.IdempotencyKey,
			&t.PolicyVersion, &t.ServiceCode, &t.FactUsed, &evid,
			&t.CreatedAt, &t.UpdatedAt,
			&accID, &accOrg, &cnpj, &razao, &fantasia, &mun, &uf, &svc, &qstate, &fact, &blocked, &dnc,
		)
		if err != nil {
			return nil, err
		}
		_ = json.Unmarshal(evid, &t.EvidenceIDs)
		if accID != nil {
			t.Account = &models.OutreachAccount{
				ID: *accID, OrganizationID: orgID, CNPJ14: cnpj,
				RazaoSocial: razao, NomeFantasia: fantasia, Municipio: mun, UF: uf,
				ServiceCode: svc, QueueState: qstate, FactToMention: fact,
				Blocked: blocked, DoNotContact: dnc,
			}
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func collectTouchpoints(rows pgx.Rows) ([]models.OutreachTouchpoint, error) {
	var out []models.OutreachTouchpoint
	for rows.Next() {
		t, err := scanTouchpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (r *outreachRepository) CASQueueTouchpoint(ctx context.Context, orgID, id uuid.UUID, expectedContentHash string) (*models.OutreachTouchpoint, error) {
	now := time.Now().UTC()
	// Must match outreachTouchpointSelect / scanTouchpoint field count.
	const ret = `id, organization_id, account_id, contact_candidate_id, ordinal, COALESCE(cadence_step,''), COALESCE(channel,'EMAIL'), COALESCE(purpose,''), due_at, state, draft_id, COALESCE(recipient,''), COALESCE(subject,''), COALESCE(body_text,''), COALESCE(content_hash,''), COALESCE(approved_content_hash,''), approved_by, approved_at, COALESCE(authorization_mode,''), campaign_policy_authorization_id, COALESCE(authorization_policy_hash,''), authorization_at, COALESCE(signature_version,''), queued_at, sent_at, COALESCE(provider_message_id,''), COALESCE(stop_reason,''), previous_touchpoint_id, COALESCE(idempotency_key,''), COALESCE(policy_version,''), COALESCE(service_code,''), COALESCE(fact_used,''), evidence_ids, COALESCE(generated_context_hash,''), created_at, updated_at`
	// Human path: approved_by set. Policy path: authorization_mode=CAMPAIGN_POLICY and approved_by null.
	row := r.db.QueryRow(ctx, `
		UPDATE outreach_touchpoints
		SET state='QUEUED', queued_at=$4, updated_at=$4
		WHERE organization_id=$1 AND id=$2 AND state='APPROVED'
		  AND content_hash=$3 AND approved_content_hash=content_hash
		  AND (
		    approved_by IS NOT NULL
		    OR (authorization_mode = 'CAMPAIGN_POLICY' AND approved_by IS NULL)
		  )
		RETURNING `+ret, orgID, id, expectedContentHash, now)
	t, err := scanTouchpoint(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func (r *outreachRepository) CancelOpenTouchpoints(ctx context.Context, orgID, accountID uuid.UUID, terminalState, stopReason string) (int, error) {
	if terminalState == "" {
		terminalState = models.TouchpointCancelled
	}
	now := time.Now().UTC()
	ct, err := r.db.Exec(ctx, `UPDATE outreach_touchpoints SET state=$4, stop_reason=$5, approved_by=NULL, approved_at=NULL, approved_content_hash='', authorization_mode='', campaign_policy_authorization_id=NULL, authorization_policy_hash='', authorization_at=NULL, updated_at=$6 WHERE organization_id=$1 AND account_id=$2 AND state=ANY($3::text[])`,
		orgID, accountID, []string{models.TouchpointPlanned, models.TouchpointDue, models.TouchpointDrafted, models.TouchpointNeedsReview, models.TouchpointApproved, models.TouchpointQueued}, terminalState, stopReason, now)
	if err != nil {
		return 0, err
	}
	return int(ct.RowsAffected()), nil
}

func (r *outreachRepository) ListDuePlannedTouchpoints(ctx context.Context, orgID uuid.UUID, now time.Time, limit int) ([]models.OutreachTouchpoint, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := r.db.Query(ctx, outreachTouchpointSelect+`
		FROM outreach_touchpoints
		WHERE organization_id=$1
		  AND state = 'PLANNED'
		  AND due_at <= $2
		ORDER BY due_at ASC, ordinal ASC
		LIMIT $3`, orgID, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectTouchpoints(rows)
}

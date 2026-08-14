package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/warmbly/warmbly/internal/models"
)

// Draft methods for OutreachRepository are in this file (same interface extension).

// OutreachDraftRepository is satisfied by outreachRepository.
type OutreachDraftStore interface {
	UpsertDraft(ctx context.Context, d *models.OutreachDraft) error
	GetDraft(ctx context.Context, orgID, id uuid.UUID) (*models.OutreachDraft, error)
	GetActiveDraftForAccount(ctx context.Context, orgID, accountID uuid.UUID) (*models.OutreachDraft, error)
	ListDrafts(ctx context.Context, orgID uuid.UUID, status string, limit, offset int) ([]models.OutreachDraft, error)
	UpdateDraftStatus(ctx context.Context, d *models.OutreachDraft) error
	GetOrgSettings(ctx context.Context, orgID uuid.UUID) (*models.OutreachOrgSettings, error)
	UpsertOrgSettings(ctx context.Context, s *models.OutreachOrgSettings) error
}

func (r *outreachRepository) UpsertDraft(ctx context.Context, d *models.OutreachDraft) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	now := time.Now().UTC()
	d.UpdatedAt = now
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	evid, _ := json.Marshal(d.EvidenceIDs)
	if evid == nil {
		evid = []byte("[]")
	}
	flags, _ := json.Marshal(d.RiskFlags)
	if flags == nil {
		flags = []byte("[]")
	}
	reasons, _ := json.Marshal(d.RedTeamReasons)
	if reasons == nil {
		reasons = []byte("[]")
	}
	follow := d.FollowupsJSON
	if len(follow) == 0 {
		follow = []byte("[]")
	}
	val := d.ValidationJSON
	if len(val) == 0 {
		val = []byte("{}")
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO outreach_drafts (
			id, organization_id, account_id, contact_candidate_id,
			channel, recipient_name, recipient_role, recipient_email, recipient_phone_e164, verification_status,
			subject, body_text, body_html, followups_json,
			service_code, strategy_code, fact_used, evidence_ids, question, cta,
			provider, model, prompt_version, generation,
			validation_json, risk_class, risk_flags, red_team_result, red_team_reasons,
			status, human_edited, approved_by, approved_at, review_seconds,
			campaign_id, enrollment_contact_id, enrolled_at,
			created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,
			$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,$39
		)
		ON CONFLICT (id) DO UPDATE SET
			contact_candidate_id = EXCLUDED.contact_candidate_id,
			channel = EXCLUDED.channel,
			recipient_name = EXCLUDED.recipient_name,
			recipient_role = EXCLUDED.recipient_role,
			recipient_email = EXCLUDED.recipient_email,
			recipient_phone_e164 = EXCLUDED.recipient_phone_e164,
			verification_status = EXCLUDED.verification_status,
			subject = EXCLUDED.subject,
			body_text = EXCLUDED.body_text,
			body_html = EXCLUDED.body_html,
			followups_json = EXCLUDED.followups_json,
			service_code = EXCLUDED.service_code,
			strategy_code = EXCLUDED.strategy_code,
			fact_used = EXCLUDED.fact_used,
			evidence_ids = EXCLUDED.evidence_ids,
			question = EXCLUDED.question,
			cta = EXCLUDED.cta,
			provider = EXCLUDED.provider,
			model = EXCLUDED.model,
			prompt_version = EXCLUDED.prompt_version,
			generation = EXCLUDED.generation,
			validation_json = EXCLUDED.validation_json,
			risk_class = EXCLUDED.risk_class,
			risk_flags = EXCLUDED.risk_flags,
			red_team_result = EXCLUDED.red_team_result,
			red_team_reasons = EXCLUDED.red_team_reasons,
			status = EXCLUDED.status,
			human_edited = EXCLUDED.human_edited,
			approved_by = EXCLUDED.approved_by,
			approved_at = EXCLUDED.approved_at,
			review_seconds = EXCLUDED.review_seconds,
			campaign_id = EXCLUDED.campaign_id,
			enrollment_contact_id = EXCLUDED.enrollment_contact_id,
			enrolled_at = EXCLUDED.enrolled_at,
			updated_at = EXCLUDED.updated_at
	`,
		d.ID, d.OrganizationID, d.AccountID, d.ContactCandidateID,
		channelOrEmail(d.Channel), d.RecipientName, d.RecipientRole, d.RecipientEmail, d.RecipientPhoneE164, d.VerificationStatus,
		d.Subject, d.BodyText, d.BodyHTML, follow,
		d.ServiceCode, d.StrategyCode, d.FactUsed, evid, d.Question, d.CTA,
		d.Provider, d.Model, d.PromptVersion, d.Generation,
		val, d.RiskClass, flags, d.RedTeamResult, reasons,
		d.Status, d.HumanEdited, d.ApprovedBy, d.ApprovedAt, d.ReviewSeconds,
		d.CampaignID, d.EnrollmentContactID, d.EnrolledAt,
		d.CreatedAt, d.UpdatedAt,
	)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "outreach_drafts_org_account_active_uidx" {
		existing, getErr := r.GetActiveDraftForAccount(ctx, d.OrganizationID, d.AccountID)
		if getErr == nil && existing != nil && existing.ID != d.ID {
			d.ID = existing.ID
			d.CreatedAt = existing.CreatedAt
			return r.UpsertDraft(ctx, d)
		}
	}
	return err
}

const outreachDraftSelect = `
	SELECT id, organization_id, account_id, contact_candidate_id,
		COALESCE(channel,'EMAIL'), COALESCE(recipient_name,''), COALESCE(recipient_role,''), COALESCE(recipient_email,''), COALESCE(recipient_phone_e164,''), COALESCE(verification_status,''),
		COALESCE(subject,''), COALESCE(body_text,''), COALESCE(body_html,''), followups_json,
		COALESCE(service_code,''), COALESCE(strategy_code,''), COALESCE(fact_used,''), evidence_ids,
		COALESCE(question,''), COALESCE(cta,''),
		COALESCE(provider,''), COALESCE(model,''), COALESCE(prompt_version,''), generation,
		validation_json, risk_class, risk_flags, COALESCE(red_team_result,''), red_team_reasons,
		status, human_edited, approved_by, approved_at, review_seconds,
		campaign_id, enrollment_contact_id, enrolled_at,
		created_at, updated_at `

func scanDraft(row scannable) (*models.OutreachDraft, error) {
	var d models.OutreachDraft
	var evid, flags, reasons []byte
	err := row.Scan(
		&d.ID, &d.OrganizationID, &d.AccountID, &d.ContactCandidateID,
		&d.Channel, &d.RecipientName, &d.RecipientRole, &d.RecipientEmail, &d.RecipientPhoneE164, &d.VerificationStatus,
		&d.Subject, &d.BodyText, &d.BodyHTML, &d.FollowupsJSON,
		&d.ServiceCode, &d.StrategyCode, &d.FactUsed, &evid,
		&d.Question, &d.CTA,
		&d.Provider, &d.Model, &d.PromptVersion, &d.Generation,
		&d.ValidationJSON, &d.RiskClass, &flags, &d.RedTeamResult, &reasons,
		&d.Status, &d.HumanEdited, &d.ApprovedBy, &d.ApprovedAt, &d.ReviewSeconds,
		&d.CampaignID, &d.EnrollmentContactID, &d.EnrolledAt,
		&d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(evid, &d.EvidenceIDs)
	_ = json.Unmarshal(flags, &d.RiskFlags)
	_ = json.Unmarshal(reasons, &d.RedTeamReasons)
	var val struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal(d.ValidationJSON, &val) == nil {
		ok := val.OK
		d.ValidationOK = &ok
	}
	return &d, nil
}

func (r *outreachRepository) GetDraft(ctx context.Context, orgID, id uuid.UUID) (*models.OutreachDraft, error) {
	row := r.db.QueryRow(ctx, outreachDraftSelect+`
		FROM outreach_drafts WHERE organization_id=$1 AND id=$2`, orgID, id)
	d, err := scanDraft(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

func (r *outreachRepository) GetActiveDraftForAccount(ctx context.Context, orgID, accountID uuid.UUID) (*models.OutreachDraft, error) {
	row := r.db.QueryRow(ctx, outreachDraftSelect+`
		FROM outreach_drafts
		WHERE organization_id=$1 AND account_id=$2
		  AND status IN ('NOT_GENERATED','GENERATING','NEEDS_REVIEW','APPROVED')
		ORDER BY updated_at DESC LIMIT 1`, orgID, accountID)
	d, err := scanDraft(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

func (r *outreachRepository) ListDrafts(ctx context.Context, orgID uuid.UUID, status string, limit, offset int) ([]models.OutreachDraft, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var rows pgx.Rows
	var err error
	if status != "" {
		rows, err = r.db.Query(ctx, outreachDraftSelect+`
			FROM outreach_drafts WHERE organization_id=$1 AND status=$2
			ORDER BY updated_at DESC LIMIT $3 OFFSET $4`, orgID, status, limit, offset)
	} else {
		rows, err = r.db.Query(ctx, outreachDraftSelect+`
			FROM outreach_drafts WHERE organization_id=$1
			ORDER BY updated_at DESC LIMIT $2 OFFSET $3`, orgID, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.OutreachDraft
	for rows.Next() {
		d, err := scanDraft(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (r *outreachRepository) UpdateDraftStatus(ctx context.Context, d *models.OutreachDraft) error {
	return r.UpsertDraft(ctx, d)
}

func (r *outreachRepository) GetOrgSettings(ctx context.Context, orgID uuid.UUID) (*models.OutreachOrgSettings, error) {
	row := r.db.QueryRow(ctx, `
		SELECT organization_id, campaign_id, COALESCE(campaign_name,''), created_at, updated_at
		FROM outreach_org_settings WHERE organization_id=$1`, orgID)
	var s models.OutreachOrgSettings
	err := row.Scan(&s.OrganizationID, &s.CampaignID, &s.CampaignName, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *outreachRepository) UpsertOrgSettings(ctx context.Context, s *models.OutreachOrgSettings) error {
	now := time.Now().UTC()
	s.UpdatedAt = now
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	if s.CampaignName == "" {
		s.CampaignName = "CONFENGE | Outreach consultivo inicial"
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO outreach_org_settings (organization_id, campaign_id, campaign_name, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (organization_id) DO UPDATE SET
			campaign_id = EXCLUDED.campaign_id,
			campaign_name = EXCLUDED.campaign_name,
			updated_at = EXCLUDED.updated_at`,
		s.OrganizationID, s.CampaignID, s.CampaignName, s.CreatedAt, s.UpdatedAt,
	)
	return err
}

func channelOrEmail(ch string) string {
	if ch == "" {
		return "EMAIL"
	}
	return ch
}

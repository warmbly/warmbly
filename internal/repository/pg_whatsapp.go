package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// WhatsAppRepository persists channel state, messages, instances, templates, webhook idempotency.
type WhatsAppRepository interface {
	GetInstanceByName(ctx context.Context, provider, instance string) (*models.WhatsAppInstance, *errx.Error)
	InsertWebhookEvent(ctx context.Context, orgID uuid.UUID, provider, idemKey, eventType, externalMsgID, payloadHash string) (inserted bool, xerr *errx.Error)
	UpsertContactState(ctx context.Context, st *models.WhatsAppContactState) *errx.Error
	GetContactStateByPhone(ctx context.Context, orgID uuid.UUID, phoneE164 string) (*models.WhatsAppContactState, *errx.Error)
	GetContactStateByContact(ctx context.Context, orgID, contactID uuid.UUID) (*models.WhatsAppContactState, *errx.Error)
	InsertMessage(ctx context.Context, msg *models.WhatsAppMessage) (inserted bool, xerr *errx.Error)
	GetTemplateStatus(ctx context.Context, orgID uuid.UUID, name, language string) (string, *errx.Error)
	ListMessagesByThread(ctx context.Context, orgID uuid.UUID, threadKey string, limit int) ([]models.WhatsAppMessage, *errx.Error)
}

type pgWhatsApp struct {
	db *pgxpool.Pool
}

// NewWhatsAppRepository constructs the Postgres implementation.
func NewWhatsAppRepository(db *pgxpool.Pool) WhatsAppRepository {
	return &pgWhatsApp{db: db}
}

func (r *pgWhatsApp) GetInstanceByName(ctx context.Context, provider, instance string) (*models.WhatsAppInstance, *errx.Error) {
	if r == nil || r.db == nil {
		return nil, errx.New(errx.ServiceUnavailable, "whatsapp repository unavailable")
	}
	row := r.db.QueryRow(ctx, `
		SELECT id, organization_id, provider, instance_name, integration_mode,
		       phone_e164, webhook_secret, status, created_at, updated_at
		FROM whatsapp_instances
		WHERE provider = $1 AND instance_name = $2
	`, provider, instance)
	var m models.WhatsAppInstance
	err := row.Scan(
		&m.ID, &m.OrganizationID, &m.Provider, &m.InstanceName, &m.IntegrationMode,
		&m.PhoneE164, &m.WebhookSecret, &m.Status, &m.CreatedAt, &m.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, errx.InternalError()
	}
	return &m, nil
}

// InsertWebhookEvent returns inserted=false when the idempotency key already exists.
func (r *pgWhatsApp) InsertWebhookEvent(ctx context.Context, orgID uuid.UUID, provider, idemKey, eventType, externalMsgID, payloadHash string) (bool, *errx.Error) {
	if r == nil || r.db == nil {
		return false, errx.New(errx.ServiceUnavailable, "whatsapp repository unavailable")
	}
	tag, err := r.db.Exec(ctx, `
		INSERT INTO whatsapp_webhook_events (
			organization_id, provider, idempotency_key, event_type, external_message_id, payload_hash
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (organization_id, idempotency_key) DO NOTHING
	`, orgID, provider, idemKey, eventType, externalMsgID, payloadHash)
	if err != nil {
		return false, errx.InternalError()
	}
	return tag.RowsAffected() > 0, nil
}

func (r *pgWhatsApp) UpsertContactState(ctx context.Context, st *models.WhatsAppContactState) *errx.Error {
	if r == nil || r.db == nil || st == nil {
		return errx.New(errx.ServiceUnavailable, "whatsapp repository unavailable")
	}
	if st.ID == uuid.Nil {
		st.ID = uuid.New()
	}
	now := time.Now().UTC()
	st.UpdatedAt = now
	if st.CreatedAt.IsZero() {
		st.CreatedAt = now
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO whatsapp_contact_state (
			id, organization_id, contact_id, outreach_candidate_id,
			phone_raw, phone_e164, phone_country, phone_valid, phone_source, phone_source_url, phone_verified_at,
			whatsapp_consent_status, whatsapp_consent_source, whatsapp_consent_at, whatsapp_consent_scope,
			whatsapp_consent_provenance_ok, whatsapp_consent_form_version, whatsapp_consent_recorded_by,
			whatsapp_last_inbound_at, whatsapp_service_window_until, whatsapp_channel_status,
			whatsapp_opt_out_at, do_not_contact,
			last_email_outbound_at, last_whatsapp_outbound_at,
			created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,
			$5,$6,$7,$8,$9,$10,$11,
			$12,$13,$14,$15,
			$16,$17,$18,
			$19,$20,$21,
			$22,$23,
			$24,$25,
			$26,$27
		)
		ON CONFLICT (organization_id, contact_id) WHERE contact_id IS NOT NULL
		DO UPDATE SET
			phone_raw = EXCLUDED.phone_raw,
			phone_e164 = EXCLUDED.phone_e164,
			phone_country = EXCLUDED.phone_country,
			phone_valid = EXCLUDED.phone_valid,
			phone_source = EXCLUDED.phone_source,
			phone_source_url = EXCLUDED.phone_source_url,
			phone_verified_at = EXCLUDED.phone_verified_at,
			-- sticky opt-out / DNC never downgraded by upsert of softer status
			whatsapp_consent_status = CASE
				WHEN whatsapp_contact_state.whatsapp_consent_status IN ('OPTED_OUT', 'DO_NOT_CONTACT')
					THEN whatsapp_contact_state.whatsapp_consent_status
				WHEN whatsapp_contact_state.do_not_contact OR whatsapp_contact_state.whatsapp_opt_out_at IS NOT NULL
					THEN whatsapp_contact_state.whatsapp_consent_status
				ELSE EXCLUDED.whatsapp_consent_status
			END,
			whatsapp_consent_source = CASE
				WHEN whatsapp_contact_state.whatsapp_consent_status IN ('OPTED_OUT', 'DO_NOT_CONTACT')
					THEN whatsapp_contact_state.whatsapp_consent_source
				ELSE EXCLUDED.whatsapp_consent_source
			END,
			whatsapp_consent_at = CASE
				WHEN whatsapp_contact_state.whatsapp_consent_status IN ('OPTED_OUT', 'DO_NOT_CONTACT')
					THEN whatsapp_contact_state.whatsapp_consent_at
				ELSE EXCLUDED.whatsapp_consent_at
			END,
			whatsapp_consent_scope = EXCLUDED.whatsapp_consent_scope,
			whatsapp_consent_provenance_ok = EXCLUDED.whatsapp_consent_provenance_ok,
			whatsapp_last_inbound_at = COALESCE(EXCLUDED.whatsapp_last_inbound_at, whatsapp_contact_state.whatsapp_last_inbound_at),
			whatsapp_service_window_until = COALESCE(EXCLUDED.whatsapp_service_window_until, whatsapp_contact_state.whatsapp_service_window_until),
			whatsapp_channel_status = EXCLUDED.whatsapp_channel_status,
			whatsapp_opt_out_at = COALESCE(whatsapp_contact_state.whatsapp_opt_out_at, EXCLUDED.whatsapp_opt_out_at),
			do_not_contact = whatsapp_contact_state.do_not_contact OR EXCLUDED.do_not_contact,
			last_email_outbound_at = COALESCE(EXCLUDED.last_email_outbound_at, whatsapp_contact_state.last_email_outbound_at),
			last_whatsapp_outbound_at = COALESCE(EXCLUDED.last_whatsapp_outbound_at, whatsapp_contact_state.last_whatsapp_outbound_at),
			updated_at = EXCLUDED.updated_at
	`, st.ID, st.OrganizationID, st.ContactID, st.OutreachCandidateID,
		st.PhoneRaw, st.PhoneE164, st.PhoneCountry, st.PhoneValid, st.PhoneSource, st.PhoneSourceURL, st.PhoneVerifiedAt,
		st.ConsentStatus, st.ConsentSource, st.ConsentAt, st.ConsentScope,
		st.ConsentProvenanceOK, st.ConsentFormVersion, st.ConsentRecordedBy,
		st.LastInboundAt, st.ServiceWindowUntil, st.ChannelStatus,
		st.OptOutAt, st.DoNotContact,
		st.LastEmailOutboundAt, st.LastWhatsAppOutboundAt,
		st.CreatedAt, st.UpdatedAt,
	)
	if err != nil {
		return errx.InternalError()
	}
	return nil
}

func (r *pgWhatsApp) GetContactStateByPhone(ctx context.Context, orgID uuid.UUID, phoneE164 string) (*models.WhatsAppContactState, *errx.Error) {
	return r.scanState(ctx, `
		SELECT id, organization_id, contact_id, outreach_candidate_id,
			phone_raw, phone_e164, phone_country, phone_valid, phone_source, phone_source_url, phone_verified_at,
			whatsapp_consent_status, whatsapp_consent_source, whatsapp_consent_at, whatsapp_consent_scope,
			whatsapp_consent_provenance_ok, whatsapp_consent_form_version, whatsapp_consent_recorded_by,
			whatsapp_last_inbound_at, whatsapp_service_window_until, whatsapp_channel_status,
			whatsapp_opt_out_at, do_not_contact,
			last_email_outbound_at, last_whatsapp_outbound_at,
			created_at, updated_at
		FROM whatsapp_contact_state
		WHERE organization_id = $1 AND phone_e164 = $2
		ORDER BY updated_at DESC
		LIMIT 1
	`, orgID, phoneE164)
}

func (r *pgWhatsApp) GetContactStateByContact(ctx context.Context, orgID, contactID uuid.UUID) (*models.WhatsAppContactState, *errx.Error) {
	return r.scanState(ctx, `
		SELECT id, organization_id, contact_id, outreach_candidate_id,
			phone_raw, phone_e164, phone_country, phone_valid, phone_source, phone_source_url, phone_verified_at,
			whatsapp_consent_status, whatsapp_consent_source, whatsapp_consent_at, whatsapp_consent_scope,
			whatsapp_consent_provenance_ok, whatsapp_consent_form_version, whatsapp_consent_recorded_by,
			whatsapp_last_inbound_at, whatsapp_service_window_until, whatsapp_channel_status,
			whatsapp_opt_out_at, do_not_contact,
			last_email_outbound_at, last_whatsapp_outbound_at,
			created_at, updated_at
		FROM whatsapp_contact_state
		WHERE organization_id = $1 AND contact_id = $2
		LIMIT 1
	`, orgID, contactID)
}

func (r *pgWhatsApp) scanState(ctx context.Context, q string, args ...any) (*models.WhatsAppContactState, *errx.Error) {
	if r == nil || r.db == nil {
		return nil, errx.New(errx.ServiceUnavailable, "whatsapp repository unavailable")
	}
	row := r.db.QueryRow(ctx, q, args...)
	var st models.WhatsAppContactState
	err := row.Scan(
		&st.ID, &st.OrganizationID, &st.ContactID, &st.OutreachCandidateID,
		&st.PhoneRaw, &st.PhoneE164, &st.PhoneCountry, &st.PhoneValid, &st.PhoneSource, &st.PhoneSourceURL, &st.PhoneVerifiedAt,
		&st.ConsentStatus, &st.ConsentSource, &st.ConsentAt, &st.ConsentScope,
		&st.ConsentProvenanceOK, &st.ConsentFormVersion, &st.ConsentRecordedBy,
		&st.LastInboundAt, &st.ServiceWindowUntil, &st.ChannelStatus,
		&st.OptOutAt, &st.DoNotContact,
		&st.LastEmailOutboundAt, &st.LastWhatsAppOutboundAt,
		&st.CreatedAt, &st.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, errx.InternalError()
	}
	return &st, nil
}

func (r *pgWhatsApp) InsertMessage(ctx context.Context, msg *models.WhatsAppMessage) (bool, *errx.Error) {
	if r == nil || r.db == nil || msg == nil {
		return false, errx.New(errx.ServiceUnavailable, "whatsapp repository unavailable")
	}
	if msg.ID == uuid.Nil {
		msg.ID = uuid.New()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	if msg.Channel == "" {
		msg.Channel = "WHATSAPP"
	}
	tag, err := r.db.Exec(ctx, `
		INSERT INTO whatsapp_messages (
			id, organization_id, contact_id, thread_key, direction, channel, provider,
			provider_message_id, idempotency_key, body_text, template_name, template_language,
			status, failure_code, draft_id, campaign_id, reviewed_by, approved_at, sent_at, occurred_at, created_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,
			$8,$9,$10,$11,$12,
			$13,$14,$15,$16,$17,$18,$19,$20,$21
		)
		ON CONFLICT (organization_id, idempotency_key) DO NOTHING
	`, msg.ID, msg.OrganizationID, msg.ContactID, msg.ThreadKey, msg.Direction, msg.Channel, msg.Provider,
		msg.ProviderMessageID, msg.IdempotencyKey, msg.BodyText, msg.TemplateName, msg.TemplateLanguage,
		msg.Status, msg.FailureCode, msg.DraftID, msg.CampaignID, msg.ReviewedBy, msg.ApprovedAt, msg.SentAt, msg.OccurredAt, msg.CreatedAt,
	)
	if err != nil {
		return false, errx.InternalError()
	}
	return tag.RowsAffected() > 0, nil
}

func (r *pgWhatsApp) GetTemplateStatus(ctx context.Context, orgID uuid.UUID, name, language string) (string, *errx.Error) {
	if r == nil || r.db == nil {
		return "MISSING", errx.New(errx.ServiceUnavailable, "whatsapp repository unavailable")
	}
	var status string
	err := r.db.QueryRow(ctx, `
		SELECT status FROM whatsapp_templates
		WHERE organization_id = $1 AND name = $2 AND language = $3
	`, orgID, name, language).Scan(&status)
	if err == pgx.ErrNoRows {
		return "MISSING", nil
	}
	if err != nil {
		return "MISSING", errx.InternalError()
	}
	return status, nil
}

func (r *pgWhatsApp) ListMessagesByThread(ctx context.Context, orgID uuid.UUID, threadKey string, limit int) ([]models.WhatsAppMessage, *errx.Error) {
	if r == nil || r.db == nil {
		return nil, errx.New(errx.ServiceUnavailable, "whatsapp repository unavailable")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, organization_id, contact_id, thread_key, direction, channel, provider,
			provider_message_id, idempotency_key, body_text, template_name, template_language,
			status, failure_code, draft_id, campaign_id, reviewed_by, approved_at, sent_at, occurred_at, created_at
		FROM whatsapp_messages
		WHERE organization_id = $1 AND thread_key = $2
		ORDER BY occurred_at DESC
		LIMIT $3
	`, orgID, threadKey, limit)
	if err != nil {
		return nil, errx.InternalError()
	}
	defer rows.Close()
	var out []models.WhatsAppMessage
	for rows.Next() {
		var m models.WhatsAppMessage
		if err := rows.Scan(
			&m.ID, &m.OrganizationID, &m.ContactID, &m.ThreadKey, &m.Direction, &m.Channel, &m.Provider,
			&m.ProviderMessageID, &m.IdempotencyKey, &m.BodyText, &m.TemplateName, &m.TemplateLanguage,
			&m.Status, &m.FailureCode, &m.DraftID, &m.CampaignID, &m.ReviewedBy, &m.ApprovedAt, &m.SentAt, &m.OccurredAt, &m.CreatedAt,
		); err != nil {
			return nil, errx.InternalError()
		}
		out = append(out, m)
	}
	return out, nil
}

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// IntegrationRepository owns persistence for third-party integrations.
// Connection rows store the encrypted config and inbound secret; meeting
// bookings store Calendly/Cal.com conversion events.
type IntegrationRepository interface {
	// Connections
	UpsertConnection(ctx context.Context, c *models.IntegrationConnection, configEncrypted []byte, inboundSecret string) error
	ListConnections(ctx context.Context, orgID uuid.UUID) ([]models.IntegrationConnection, error)
	GetConnection(ctx context.Context, orgID uuid.UUID, provider models.IntegrationProvider, label string) (*models.IntegrationConnection, error)
	GetConnectionByInboundSecret(ctx context.Context, provider models.IntegrationProvider, secret string) (*models.IntegrationConnection, error)
	DeleteConnection(ctx context.Context, orgID, id uuid.UUID) error
	MarkConnectionSynced(ctx context.Context, id uuid.UUID, status models.IntegrationStatus, displayFields json.RawMessage, errMsg string) error

	// Bookings
	UpsertMeetingBooking(ctx context.Context, b *models.MeetingBooking) error
	ListMeetingBookings(ctx context.Context, orgID uuid.UUID, limit int) ([]models.MeetingBooking, error)
}

type integrationRepository struct {
	db *pgxpool.Pool
}

func NewIntegrationRepository(db *pgxpool.Pool) IntegrationRepository {
	return &integrationRepository{db: db}
}

// UpsertConnection inserts a new connection or updates an existing
// (org, provider, label) tuple. Encrypted config and inbound secret are
// only written when non-nil, so partial updates do not blow away the rest
// of the config.
func (r *integrationRepository) UpsertConnection(ctx context.Context, c *models.IntegrationConnection, configEncrypted []byte, inboundSecret string) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	now := time.Now().UTC()
	c.CreatedAt = now
	c.UpdatedAt = now

	display := c.DisplayFields
	if len(display) == 0 {
		display = json.RawMessage("{}")
	}

	_, err := r.db.Exec(ctx, `
		INSERT INTO integration_connections (
			id, organization_id, provider, label, status,
			inbound_secret, config_encrypted, display_fields,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
		ON CONFLICT (organization_id, provider, label) DO UPDATE SET
			status = EXCLUDED.status,
			inbound_secret = COALESCE(EXCLUDED.inbound_secret, integration_connections.inbound_secret),
			config_encrypted = COALESCE(EXCLUDED.config_encrypted, integration_connections.config_encrypted),
			display_fields = EXCLUDED.display_fields,
			updated_at = EXCLUDED.updated_at
	`,
		c.ID, c.OrganizationID, string(c.Provider), c.Label, string(c.Status),
		nullIfEmptyStr(inboundSecret), nullIfEmptyBytes(configEncrypted), display, now,
	)
	return err
}

func nullIfEmptyStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullIfEmptyBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func (r *integrationRepository) ListConnections(ctx context.Context, orgID uuid.UUID) ([]models.IntegrationConnection, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, organization_id, provider, label, status, display_fields,
		       last_synced_at, last_error, last_error_at, created_at, updated_at
		FROM integration_connections
		WHERE organization_id = $1
		ORDER BY created_at DESC
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.IntegrationConnection{}
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (r *integrationRepository) GetConnection(ctx context.Context, orgID uuid.UUID, provider models.IntegrationProvider, label string) (*models.IntegrationConnection, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, organization_id, provider, label, status, display_fields,
		       last_synced_at, last_error, last_error_at, created_at, updated_at
		FROM integration_connections
		WHERE organization_id = $1 AND provider = $2 AND label = $3
	`, orgID, string(provider), label)
	c, err := scanConnection(row)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

// GetConnectionByInboundSecret resolves the connection an incoming webhook
// belongs to. Callers must validate the secret out-of-band (e.g. Calendly
// signature). This lookup is the org-routing step.
func (r *integrationRepository) GetConnectionByInboundSecret(ctx context.Context, provider models.IntegrationProvider, secret string) (*models.IntegrationConnection, error) {
	if secret == "" {
		return nil, nil
	}
	row := r.db.QueryRow(ctx, `
		SELECT id, organization_id, provider, label, status, display_fields,
		       last_synced_at, last_error, last_error_at, created_at, updated_at
		FROM integration_connections
		WHERE provider = $1 AND inbound_secret = $2
		LIMIT 1
	`, string(provider), secret)
	c, err := scanConnection(row)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

func (r *integrationRepository) DeleteConnection(ctx context.Context, orgID, id uuid.UUID) error {
	_, err := r.db.Exec(ctx,
		`DELETE FROM integration_connections WHERE organization_id = $1 AND id = $2`,
		orgID, id,
	)
	return err
}

func (r *integrationRepository) MarkConnectionSynced(ctx context.Context, id uuid.UUID, status models.IntegrationStatus, displayFields json.RawMessage, errMsg string) error {
	now := time.Now().UTC()
	if len(displayFields) == 0 {
		displayFields = json.RawMessage("{}")
	}
	if errMsg == "" {
		_, err := r.db.Exec(ctx, `
			UPDATE integration_connections
			SET status = $1, display_fields = $2, last_synced_at = $3,
			    last_error = NULL, last_error_at = NULL, updated_at = $3
			WHERE id = $4
		`, string(status), displayFields, now, id)
		return err
	}
	_, err := r.db.Exec(ctx, `
		UPDATE integration_connections
		SET status = $1, display_fields = $2,
		    last_error = $3, last_error_at = $4, updated_at = $4
		WHERE id = $5
	`, string(status), displayFields, errMsg, now, id)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanConnection(row scanner) (*models.IntegrationConnection, error) {
	var c models.IntegrationConnection
	var provider, status string
	if err := row.Scan(
		&c.ID, &c.OrganizationID, &provider, &c.Label, &status, &c.DisplayFields,
		&c.LastSyncedAt, &c.LastError, &c.LastErrorAt, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return nil, err
	}
	c.Provider = models.IntegrationProvider(provider)
	c.Status = models.IntegrationStatus(status)
	if len(c.DisplayFields) == 0 {
		c.DisplayFields = json.RawMessage("{}")
	}
	return &c, nil
}

// Meeting bookings

func (r *integrationRepository) UpsertMeetingBooking(ctx context.Context, b *models.MeetingBooking) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	raw := b.RawPayload
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO meeting_bookings (
			id, organization_id, source, external_event_id,
			invitee_email, invitee_name, event_name, scheduled_for,
			contact_id, campaign_id, raw_payload, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
		ON CONFLICT (organization_id, source, external_event_id) DO UPDATE SET
			invitee_email = EXCLUDED.invitee_email,
			invitee_name = EXCLUDED.invitee_name,
			event_name = EXCLUDED.event_name,
			scheduled_for = EXCLUDED.scheduled_for,
			contact_id = COALESCE(EXCLUDED.contact_id, meeting_bookings.contact_id),
			campaign_id = COALESCE(EXCLUDED.campaign_id, meeting_bookings.campaign_id),
			raw_payload = EXCLUDED.raw_payload
	`,
		b.ID, b.OrganizationID, b.Source, b.ExternalEventID,
		b.InviteeEmail, b.InviteeName, b.EventName, b.ScheduledFor,
		b.ContactID, b.CampaignID, raw,
	)
	return err
}

func (r *integrationRepository) ListMeetingBookings(ctx context.Context, orgID uuid.UUID, limit int) ([]models.MeetingBooking, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, organization_id, source, external_event_id,
		       invitee_email, invitee_name, event_name, scheduled_for,
		       contact_id, campaign_id, created_at
		FROM meeting_bookings
		WHERE organization_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.MeetingBooking{}
	for rows.Next() {
		var b models.MeetingBooking
		if err := rows.Scan(
			&b.ID, &b.OrganizationID, &b.Source, &b.ExternalEventID,
			&b.InviteeEmail, &b.InviteeName, &b.EventName, &b.ScheduledFor,
			&b.ContactID, &b.CampaignID, &b.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

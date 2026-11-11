package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// AgencyVendorLink is one workspace connection made from an agency's vendor account.
type AgencyVendorLink struct {
	OrganizationID uuid.UUID
	ConnectionID   uuid.UUID
}

// AgencyVendorRepository stores the inbox vendor accounts an agency holds for its clients.
type AgencyVendorRepository interface {
	Create(ctx context.Context, a *models.AgencyVendorAccount, createdBy uuid.UUID) error
	// List returns the agency's accounts with the clients each is used in; credentials are left out.
	List(ctx context.Context, agencyID uuid.UUID) ([]models.AgencyVendorAccount, error)
	Get(ctx context.Context, agencyID, id uuid.UUID) (*models.AgencyVendorAccount, error)
	// AgencyOf names the agency an account belongs to; false when there is none.
	AgencyOf(ctx context.Context, id uuid.UUID) (uuid.UUID, bool, error)
	// Update renames the account and, when sealed is set, replaces its key.
	Update(ctx context.Context, agencyID, id uuid.UUID, label, sealed string) (bool, error)
	Delete(ctx context.Context, agencyID, id uuid.UUID) (bool, error)
	// Links lists the workspace connections made from an account.
	Links(ctx context.Context, id uuid.UUID) ([]AgencyVendorLink, error)
	// Linked is the workspace's connection made from an account; nil when it has none.
	Linked(ctx context.Context, orgID, id uuid.UUID) (*models.VendorConnection, error)
	// CreateLinked stores a workspace connection made from an account; false when the workspace already has one.
	CreateLinked(ctx context.Context, c *models.VendorConnection, agencyConnectionID, createdBy uuid.UUID) (bool, error)
}

type agencyVendorRepository struct{ DB *db.DB }

func NewAgencyVendorRepository(database *db.DB) AgencyVendorRepository {
	return &agencyVendorRepository{DB: database}
}

func (r *agencyVendorRepository) Create(ctx context.Context, a *models.AgencyVendorAccount, createdBy uuid.UUID) error {
	query := `INSERT INTO agency_vendor_connections (id, agency_id, vendor, label, credentials, created_by)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING created_at`
	if err := r.DB.QueryRow(ctx, query, a.ID, a.AgencyID, a.Vendor, a.Label, a.Credentials, createdBy).Scan(&a.CreatedAt); err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return err
	}
	a.Status = "active"
	a.Clients = []models.AgencyVendorAccountClient{}
	return nil
}

func (r *agencyVendorRepository) List(ctx context.Context, agencyID uuid.UUID) ([]models.AgencyVendorAccount, error) {
	query := `SELECT id, agency_id, vendor, label, created_at FROM agency_vendor_connections WHERE agency_id = $1 ORDER BY created_at, id`
	rows, err := r.DB.Query(ctx, query, agencyID)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	out := make([]models.AgencyVendorAccount, 0)
	at := map[uuid.UUID]int{}
	for rows.Next() {
		a := models.AgencyVendorAccount{Status: "active", Clients: []models.AgencyVendorAccountClient{}}
		if err := rows.Scan(&a.ID, &a.AgencyID, &a.Vendor, &a.Label, &a.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		at[a.ID] = len(out)
		out = append(out, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(out) == 0 {
		return out, err
	}
	// Only workspaces still in the agency are named; a detached one has no connection left.
	uses := `SELECT c.agency_connection_id, c.organization_id, o.name, c.id, c.status, c.last_error,
			(SELECT count(*) FROM email_accounts ea WHERE ea.vendor_connection_id = c.id)
		FROM mailbox_vendor_connections c
		JOIN agency_vendor_connections a ON a.id = c.agency_connection_id
		JOIN agency_clients ac ON ac.organization_id = c.organization_id AND ac.agency_id = a.agency_id
		JOIN organizations o ON o.id = c.organization_id
		WHERE a.agency_id = $1
		ORDER BY lower(o.name), o.id`
	rows, err = r.DB.Query(ctx, uses, agencyID)
	if err != nil {
		db.CaptureError(err, uses, nil, "query")
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var c models.AgencyVendorAccountClient
		if err := rows.Scan(&id, &c.OrganizationID, &c.Name, &c.ConnectionID, &c.Status, &c.LastError, &c.Mailboxes); err != nil {
			return nil, err
		}
		i, ok := at[id]
		if !ok {
			continue
		}
		a := &out[i]
		a.Clients = append(a.Clients, c)
		a.Mailboxes += c.Mailboxes
		if c.Status == "invalid" && a.Status != "invalid" {
			a.Status, a.LastError = "invalid", c.LastError
		}
	}
	return out, rows.Err()
}

func (r *agencyVendorRepository) Get(ctx context.Context, agencyID, id uuid.UUID) (*models.AgencyVendorAccount, error) {
	query := `SELECT id, agency_id, vendor, label, created_at, credentials FROM agency_vendor_connections WHERE agency_id = $1 AND id = $2`
	a := models.AgencyVendorAccount{Status: "active", Clients: []models.AgencyVendorAccountClient{}}
	if err := r.DB.QueryRow(ctx, query, agencyID, id).Scan(&a.ID, &a.AgencyID, &a.Vendor, &a.Label, &a.CreatedAt, &a.Credentials); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		db.CaptureError(err, query, nil, "queryrow")
		return nil, err
	}
	return &a, nil
}

func (r *agencyVendorRepository) AgencyOf(ctx context.Context, id uuid.UUID) (uuid.UUID, bool, error) {
	var agencyID uuid.UUID
	err := r.DB.QueryRow(ctx, `SELECT agency_id FROM agency_vendor_connections WHERE id = $1`, id).Scan(&agencyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return agencyID, err == nil, err
}

func (r *agencyVendorRepository) Update(ctx context.Context, agencyID, id uuid.UUID, label, sealed string) (bool, error) {
	tag, err := r.DB.Exec(ctx, `UPDATE agency_vendor_connections
		SET label = $3, credentials = CASE WHEN $4 = '' THEN credentials ELSE $4 END, updated_at = now()
		WHERE agency_id = $1 AND id = $2`, agencyID, id, label, sealed)
	if err != nil {
		db.CaptureError(err, "update agency_vendor_connections", nil, "exec")
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *agencyVendorRepository) Delete(ctx context.Context, agencyID, id uuid.UUID) (bool, error) {
	tag, err := r.DB.Exec(ctx, `DELETE FROM agency_vendor_connections WHERE agency_id = $1 AND id = $2`, agencyID, id)
	if err != nil {
		db.CaptureError(err, "delete agency_vendor_connections", nil, "exec")
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *agencyVendorRepository) Links(ctx context.Context, id uuid.UUID) ([]AgencyVendorLink, error) {
	rows, err := r.DB.Query(ctx, `SELECT organization_id, id FROM mailbox_vendor_connections WHERE agency_connection_id = $1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgencyVendorLink
	for rows.Next() {
		var l AgencyVendorLink
		if err := rows.Scan(&l.OrganizationID, &l.ConnectionID); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *agencyVendorRepository) Linked(ctx context.Context, orgID, id uuid.UUID) (*models.VendorConnection, error) {
	query := `SELECT ` + vendorColumns + ` FROM mailbox_vendor_connections c WHERE c.organization_id = $1 AND c.agency_connection_id = $2`
	var c models.VendorConnection
	if err := scanVendor(r.DB.QueryRow(ctx, query, orgID, id), &c); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		db.CaptureError(err, query, nil, "queryrow")
		return nil, err
	}
	return &c, nil
}

func (r *agencyVendorRepository) CreateLinked(ctx context.Context, c *models.VendorConnection, agencyConnectionID, createdBy uuid.UUID) (bool, error) {
	// The workspace must still be the agency's client as the row is written.
	query := `INSERT INTO mailbox_vendor_connections (id, organization_id, vendor, label, credentials, status, created_by, last_used_at, agency_connection_id)
		SELECT $1, ac.organization_id, $3, $4, $5, 'active', $6, now(), a.id
		FROM agency_vendor_connections a
		JOIN agency_clients ac ON ac.agency_id = a.agency_id AND ac.organization_id = $2
		WHERE a.id = $7
		ON CONFLICT DO NOTHING
		RETURNING created_at`
	err := r.DB.QueryRow(ctx, query, c.ID, c.OrganizationID, c.Vendor, c.Label, c.Credentials, createdBy, agencyConnectionID).Scan(&c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return false, err
	}
	c.Status, c.AgencyConnectionID = "active", &agencyConnectionID
	return true, nil
}

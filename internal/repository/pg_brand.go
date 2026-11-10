package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// ErrPortalDomainTaken: another agency holds the portal domain verified.
var ErrPortalDomainTaken = errors.New("portal domain verified by another agency")

const portalDomainConstraint = "agency_brands_portal_domain_one_agency"

// BrandRepository stores workspace and agency brands.
type BrandRepository interface {
	// GetWorkspaceBrand is nil, nil when the workspace stored none.
	GetWorkspaceBrand(ctx context.Context, orgID uuid.UUID) (*models.BrandSettings, error)
	UpsertWorkspaceBrand(ctx context.Context, orgID uuid.UUID, in models.BrandInput, by uuid.UUID) (*models.BrandSettings, error)
	// SetWorkspaceLogo stores url and returns the one it replaced.
	SetWorkspaceLogo(ctx context.Context, orgID uuid.UUID, url string, by uuid.UUID) (previous string, err error)
	// DeleteWorkspaceBrand returns the logo the removed row named.
	DeleteWorkspaceBrand(ctx context.Context, orgID uuid.UUID) (previousLogo string, err error)

	// GetAgencyBrand is nil, nil when the agency stored none.
	GetAgencyBrand(ctx context.Context, agencyID uuid.UUID) (*models.AgencyBrand, error)
	// AgencyBrandForOrganization is the brand of the agency orgID is a client of; nil, nil when none.
	AgencyBrandForOrganization(ctx context.Context, orgID uuid.UUID) (*models.AgencyBrand, error)
	// UpsertAgencyBrand keeps the stored sign-in choice and theme when in carries none.
	UpsertAgencyBrand(ctx context.Context, agencyID uuid.UUID, in models.BrandInput, by uuid.UUID) (*models.AgencyBrand, error)
	// SetAgencyImage stores url as the agency's logo, tab icon or sign-in image and returns the one it replaced.
	SetAgencyImage(ctx context.Context, agencyID uuid.UUID, image models.AgencyBrandImage, url string, by uuid.UUID) (previous string, err error)
	// SetAgencyPortalDomain stores a host and clears the verdict when the name changed.
	SetAgencyPortalDomain(ctx context.Context, agencyID uuid.UUID, domain string, by uuid.UUID) error
	// SetAgencyPortalDomainVerified records a verdict on domain, only while it is still the agency's host; ErrPortalDomainTaken when another agency holds it.
	SetAgencyPortalDomainVerified(ctx context.Context, agencyID uuid.UUID, domain string, verified bool, at *time.Time) error
	// PortalHostHeldElsewhere reports whether anyone but agencyID's clients holds host as a custom domain of any kind.
	PortalHostHeldElsewhere(ctx context.Context, agencyID uuid.UUID, host string) (bool, error)
	// ListPortalDomains is every agency with a portal domain set, for the re-verification sweep.
	ListPortalDomains(ctx context.Context) ([]models.AgencyBrand, error)
	// VerifiedPortal is the agency holding host verified as its portal domain, with the agency's name; nil when none.
	VerifiedPortal(ctx context.Context, host string) (*models.AgencyBrand, string, error)

	// AgencyRole is userID's staff role in the agency; empty when not staff.
	AgencyRole(ctx context.Context, agencyID, userID uuid.UUID) (models.AgencyRole, error)
	// AgencyPlan is the agency's plan and subscription status.
	AgencyPlan(ctx context.Context, agencyID uuid.UUID) (*uuid.UUID, models.SubscriptionStatus, error)
}

type brandRepository struct {
	db *pgxpool.Pool
}

func NewBrandRepository(db *pgxpool.Pool) BrandRepository {
	return &brandRepository{db: db}
}

const brandColumns = `display_name, logo_url, accent_color, website_url, hide_powered_by, updated_at, updated_by`

const agencyBrandColumns = `agency_id, ` + brandColumns + `, portal_domain, portal_domain_verified, portal_domain_verified_at, portal_sign_in,
	icon_url, sign_in_image_url, theme`

// agencyBrandColumnsAB is agencyBrandColumns qualified for a join, where agency_id and updated_at are ambiguous.
const agencyBrandColumnsAB = `ab.agency_id, ab.display_name, ab.logo_url, ab.accent_color, ab.website_url, ab.hide_powered_by,
	ab.updated_at, ab.updated_by, ab.portal_domain, ab.portal_domain_verified, ab.portal_domain_verified_at, ab.portal_sign_in,
	ab.icon_url, ab.sign_in_image_url, ab.theme`

func scanBrand(row pgx.Row) (*models.BrandSettings, error) {
	var b models.BrandSettings
	err := row.Scan(&b.DisplayName, &b.LogoURL, &b.AccentColor, &b.WebsiteURL, &b.HidePoweredBy, &b.UpdatedAt, &b.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// scanAgencyBrand reads agencyBrandColumns, then any columns selected after them into extra.
func scanAgencyBrand(row pgx.Row, extra ...any) (*models.AgencyBrand, error) {
	var b models.AgencyBrand
	var theme []byte
	dest := append([]any{&b.AgencyID, &b.DisplayName, &b.LogoURL, &b.AccentColor, &b.WebsiteURL, &b.HidePoweredBy,
		&b.UpdatedAt, &b.UpdatedBy, &b.PortalDomain, &b.PortalDomainVerified, &b.PortalDomainVerifiedAt, &b.PortalSignIn,
		&b.IconURL, &b.SignInImageURL, &theme}, extra...)
	err := row.Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.Theme = decodePortalTheme(theme)
	return &b, nil
}

// decodePortalTheme reads a stored theme; one that does not fit the type is no theme, never a failed brand read.
func decodePortalTheme(raw []byte) models.PortalTheme {
	var t models.PortalTheme
	if len(raw) == 0 || json.Unmarshal(raw, &t) != nil {
		return models.PortalTheme{}
	}
	return t
}

// portalThemeParam is NULL when the request kept the stored theme.
func portalThemeParam(t *models.PortalTheme) ([]byte, error) {
	if t == nil {
		return nil, nil
	}
	return json.Marshal(t)
}

func (r *brandRepository) GetWorkspaceBrand(ctx context.Context, orgID uuid.UUID) (*models.BrandSettings, error) {
	return scanBrand(r.db.QueryRow(ctx, `SELECT `+brandColumns+` FROM workspace_brands WHERE organization_id = $1`, orgID))
}

func (r *brandRepository) UpsertWorkspaceBrand(ctx context.Context, orgID uuid.UUID, in models.BrandInput, by uuid.UUID) (*models.BrandSettings, error) {
	return scanBrand(r.db.QueryRow(ctx, `
		INSERT INTO workspace_brands (organization_id, display_name, accent_color, website_url, hide_powered_by, updated_at, updated_by)
		VALUES ($1, $2, $3, $4, $5, now(), $6)
		ON CONFLICT (organization_id) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			accent_color = EXCLUDED.accent_color,
			website_url = EXCLUDED.website_url,
			hide_powered_by = EXCLUDED.hide_powered_by,
			updated_at = now(),
			updated_by = EXCLUDED.updated_by
		RETURNING `+brandColumns,
		orgID, in.DisplayName, in.AccentColor, in.WebsiteURL, in.HidePoweredBy, by))
}

func (r *brandRepository) SetWorkspaceLogo(ctx context.Context, orgID uuid.UUID, url string, by uuid.UUID) (string, error) {
	var previous string
	err := r.db.QueryRow(ctx, `
		WITH prior AS (SELECT logo_url FROM workspace_brands WHERE organization_id = $1)
		INSERT INTO workspace_brands (organization_id, logo_url, updated_at, updated_by)
		VALUES ($1, $2, now(), $3)
		ON CONFLICT (organization_id) DO UPDATE SET logo_url = EXCLUDED.logo_url, updated_at = now(), updated_by = EXCLUDED.updated_by
		RETURNING COALESCE((SELECT logo_url FROM prior), '')
	`, orgID, url, by).Scan(&previous)
	return previous, err
}

func (r *brandRepository) DeleteWorkspaceBrand(ctx context.Context, orgID uuid.UUID) (string, error) {
	var previous string
	err := r.db.QueryRow(ctx, `DELETE FROM workspace_brands WHERE organization_id = $1 RETURNING logo_url`, orgID).Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return previous, err
}

func (r *brandRepository) GetAgencyBrand(ctx context.Context, agencyID uuid.UUID) (*models.AgencyBrand, error) {
	return scanAgencyBrand(r.db.QueryRow(ctx, `SELECT `+agencyBrandColumns+` FROM agency_brands WHERE agency_id = $1`, agencyID))
}

func (r *brandRepository) AgencyBrandForOrganization(ctx context.Context, orgID uuid.UUID) (*models.AgencyBrand, error) {
	return scanAgencyBrand(r.db.QueryRow(ctx, `
		SELECT `+agencyBrandColumnsAB+`
		FROM agency_clients ac
		JOIN agency_brands ab ON ab.agency_id = ac.agency_id
		WHERE ac.organization_id = $1
	`, orgID))
}

func (r *brandRepository) VerifiedPortal(ctx context.Context, host string) (*models.AgencyBrand, string, error) {
	if host == "" {
		return nil, "", nil
	}
	var name string
	b, err := scanAgencyBrand(r.db.QueryRow(ctx, `
		SELECT `+agencyBrandColumnsAB+`, a.name
		FROM agency_brands ab
		JOIN agencies a ON a.id = ab.agency_id
		WHERE ab.portal_domain = $1 AND ab.portal_domain_verified
	`, host), &name)
	if err != nil || b == nil {
		return nil, "", err
	}
	return b, name, nil
}

func (r *brandRepository) UpsertAgencyBrand(ctx context.Context, agencyID uuid.UUID, in models.BrandInput, by uuid.UUID) (*models.AgencyBrand, error) {
	theme, err := portalThemeParam(in.Theme)
	if err != nil {
		return nil, err
	}
	return scanAgencyBrand(r.db.QueryRow(ctx, `
		INSERT INTO agency_brands (agency_id, display_name, accent_color, website_url, hide_powered_by, portal_sign_in, theme, updated_at, updated_by)
		VALUES ($1, $2, $3, $4, $5, COALESCE($7, 'portal'), COALESCE($8::jsonb, '{}'::jsonb), now(), $6)
		ON CONFLICT (agency_id) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			accent_color = EXCLUDED.accent_color,
			website_url = EXCLUDED.website_url,
			hide_powered_by = EXCLUDED.hide_powered_by,
			portal_sign_in = COALESCE($7, agency_brands.portal_sign_in),
			theme = COALESCE($8::jsonb, agency_brands.theme),
			updated_at = now(),
			updated_by = EXCLUDED.updated_by
		RETURNING `+agencyBrandColumns,
		agencyID, in.DisplayName, in.AccentColor, in.WebsiteURL, in.HidePoweredBy, by, portalSignInParam(in.PortalSignIn), theme))
}

// portalSignInParam is NULL when the request kept the stored choice.
func portalSignInParam(p *models.PortalSignIn) *string {
	if p == nil {
		return nil
	}
	v := string(*p)
	return &v
}

// agencyImageColumns is the column behind each uploaded image; the name is spliced into SQL, so it only ever comes from here.
var agencyImageColumns = map[models.AgencyBrandImage]string{
	models.AgencyBrandLogo:        "logo_url",
	models.AgencyBrandIcon:        "icon_url",
	models.AgencyBrandSignInImage: "sign_in_image_url",
}

func (r *brandRepository) SetAgencyImage(ctx context.Context, agencyID uuid.UUID, image models.AgencyBrandImage, url string, by uuid.UUID) (string, error) {
	col, ok := agencyImageColumns[image]
	if !ok {
		return "", fmt.Errorf("unknown agency brand image %q", image)
	}
	var previous string
	err := r.db.QueryRow(ctx, `
		WITH prior AS (SELECT `+col+` AS url FROM agency_brands WHERE agency_id = $1)
		INSERT INTO agency_brands (agency_id, `+col+`, updated_at, updated_by)
		VALUES ($1, $2, now(), $3)
		ON CONFLICT (agency_id) DO UPDATE SET `+col+` = EXCLUDED.`+col+`, updated_at = now(), updated_by = EXCLUDED.updated_by
		RETURNING COALESCE((SELECT url FROM prior), '')
	`, agencyID, url, by).Scan(&previous)
	return previous, err
}

func (r *brandRepository) SetAgencyPortalDomain(ctx context.Context, agencyID uuid.UUID, domain string, by uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO agency_brands (agency_id, portal_domain, updated_at, updated_by)
		VALUES ($1, $2, now(), $3)
		ON CONFLICT (agency_id) DO UPDATE SET
			portal_domain = EXCLUDED.portal_domain,
			portal_domain_verified = agency_brands.portal_domain_verified AND agency_brands.portal_domain = EXCLUDED.portal_domain,
			portal_domain_verified_at = CASE WHEN agency_brands.portal_domain = EXCLUDED.portal_domain THEN agency_brands.portal_domain_verified_at END,
			updated_at = now(),
			updated_by = EXCLUDED.updated_by
	`, agencyID, domain, by)
	return err
}

func (r *brandRepository) SetAgencyPortalDomainVerified(ctx context.Context, agencyID uuid.UUID, domain string, verified bool, at *time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE agency_brands SET portal_domain_verified = $2, portal_domain_verified_at = $3
		WHERE agency_id = $1 AND portal_domain = $4
	`, agencyID, verified, at, domain)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == portalDomainConstraint {
		return ErrPortalDomainTaken
	}
	return err
}

func (r *brandRepository) PortalHostHeldElsewhere(ctx context.Context, agencyID uuid.UUID, host string) (bool, error) {
	if host == "" {
		return false, nil
	}
	var held bool
	err := r.db.QueryRow(ctx, `
		WITH clients AS (SELECT organization_id FROM agency_clients WHERE agency_id = $2)
		SELECT EXISTS (
			SELECT 1 FROM agency_brands
			WHERE portal_domain = $1 AND portal_domain_verified AND agency_id <> $2
			UNION ALL
			SELECT 1 FROM organizations
			WHERE forms_domain = $1 AND forms_domain_verified AND id NOT IN (SELECT organization_id FROM clients)
			UNION ALL
			SELECT 1 FROM email_accounts
			WHERE tracking_domain = $1 AND tracking_domain_verified
			  AND (organization_id IS NULL OR organization_id NOT IN (SELECT organization_id FROM clients))
			UNION ALL
			SELECT 1 FROM campaigns
			WHERE tracking_domain = $1 AND tracking_domain_verified
			  AND (organization_id IS NULL OR organization_id NOT IN (SELECT organization_id FROM clients))
			UNION ALL
			SELECT 1 FROM domain_redirects
			WHERE verified AND served_by = 'instance' AND organization_id NOT IN (SELECT organization_id FROM clients)
			  AND (domain = $1 OR (include_www AND 'www.' || domain = $1))
		)
	`, host, agencyID).Scan(&held)
	return held, err
}

func (r *brandRepository) ListPortalDomains(ctx context.Context) ([]models.AgencyBrand, error) {
	rows, err := r.db.Query(ctx, `SELECT `+agencyBrandColumns+` FROM agency_brands WHERE portal_domain <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.AgencyBrand{}
	for rows.Next() {
		b, err := scanAgencyBrand(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func (r *brandRepository) AgencyRole(ctx context.Context, agencyID, userID uuid.UUID) (models.AgencyRole, error) {
	var role models.AgencyRole
	err := r.db.QueryRow(ctx, `SELECT role FROM agency_members WHERE agency_id = $1 AND user_id = $2`, agencyID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return role, err
}

func (r *brandRepository) AgencyPlan(ctx context.Context, agencyID uuid.UUID) (*uuid.UUID, models.SubscriptionStatus, error) {
	var planID *uuid.UUID
	var status models.SubscriptionStatus
	// An operator's plan in force stands in for the subscription.
	err := r.db.QueryRow(ctx, `SELECT CASE WHEN `+agencyGrantLive+` THEN a.managed_plan_id ELSE a.plan_id END,
			CASE WHEN `+agencyGrantLive+` THEN 'active' ELSE a.status END
		FROM agencies a WHERE a.id = $1`, agencyID).Scan(&planID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil
	}
	return planID, status, err
}

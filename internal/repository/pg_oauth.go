package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// OAuthRepository owns persistence for the OAuth 2.1 authorization server:
// registered apps (clients), single-use authorization codes, and issued
// access+refresh grants. Tokens/secrets are stored hashed; lookups are by hash.
type OAuthRepository interface {
	// Applications
	CreateApplication(ctx context.Context, a *models.OAuthApplication) error
	ListApplications(ctx context.Context, orgID uuid.UUID) ([]models.OAuthApplication, error)
	GetApplication(ctx context.Context, orgID, id uuid.UUID) (*models.OAuthApplication, error)
	GetApplicationByClientID(ctx context.Context, clientID string) (*models.OAuthApplication, error)
	UpdateApplication(ctx context.Context, a *models.OAuthApplication) error
	UpdateApplicationSecret(ctx context.Context, orgID, id uuid.UUID, secretHash string) error
	DeleteApplication(ctx context.Context, orgID, id uuid.UUID) error

	// Authorization codes
	CreateAuthorizationCode(ctx context.Context, c *models.OAuthAuthorizationCode) error
	// TakeAuthorizationCode atomically consumes a valid, unexpired, unused code.
	TakeAuthorizationCode(ctx context.Context, codeHash string) (*models.OAuthAuthorizationCode, error)

	// Access grants
	CreateAccessGrant(ctx context.Context, g *models.OAuthAccessGrant) error
	GetGrantByAccessTokenHash(ctx context.Context, hash string) (*models.OAuthAccessGrant, error)
	GetGrantByRefreshTokenHash(ctx context.Context, hash string) (*models.OAuthAccessGrant, error)
	RotateGrantTokens(ctx context.Context, id uuid.UUID, accessHash, refreshHash string, accessExp time.Time, refreshExp *time.Time) error
	TouchGrantLastUsed(ctx context.Context, id uuid.UUID) error
	RevokeGrant(ctx context.Context, id uuid.UUID) error
	RevokeGrantByTokenHash(ctx context.Context, appID uuid.UUID, hash string) error
	ListAuthorizedApps(ctx context.Context, orgID, userID uuid.UUID) ([]models.OAuthAuthorizedApp, error)
	RevokeAuthorization(ctx context.Context, orgID, userID, appID uuid.UUID) error
}

type oauthRepository struct {
	db *pgxpool.Pool
}

func NewOAuthRepository(db *pgxpool.Pool) OAuthRepository {
	return &oauthRepository{db: db}
}

const oauthAppCols = `id, organization_id, created_by, name, description, logo_url, website_url,
	client_id, client_secret_hash, redirect_uris, scopes, status, created_at, updated_at`

func scanOAuthApp(row pgx.Row, a *models.OAuthApplication) error {
	var scopes int64
	var status string
	if err := row.Scan(&a.ID, &a.OrganizationID, &a.CreatedBy, &a.Name, &a.Description, &a.LogoURL, &a.WebsiteURL,
		&a.ClientID, &a.ClientSecretHash, &a.RedirectURIs, &scopes, &status, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return err
	}
	a.Scopes = uint64(scopes)
	a.Status = models.OAuthAppStatus(status)
	if a.RedirectURIs == nil {
		a.RedirectURIs = []string{}
	}
	return nil
}

func (r *oauthRepository) CreateApplication(ctx context.Context, a *models.OAuthApplication) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	now := time.Now().UTC()
	a.CreatedAt = now
	a.UpdatedAt = now
	if a.Status == "" {
		a.Status = models.OAuthAppActive
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO oauth_applications (id, organization_id, created_by, name, description, logo_url, website_url,
			client_id, client_secret_hash, redirect_uris, scopes, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13)`,
		a.ID, a.OrganizationID, a.CreatedBy, a.Name, a.Description, a.LogoURL, a.WebsiteURL,
		a.ClientID, a.ClientSecretHash, a.RedirectURIs, int64(a.Scopes), string(a.Status), now)
	return err
}

func (r *oauthRepository) ListApplications(ctx context.Context, orgID uuid.UUID) ([]models.OAuthApplication, error) {
	rows, err := r.db.Query(ctx, `SELECT `+oauthAppCols+` FROM oauth_applications WHERE organization_id = $1 ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.OAuthApplication{}
	for rows.Next() {
		var a models.OAuthApplication
		if err := scanOAuthApp(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *oauthRepository) GetApplication(ctx context.Context, orgID, id uuid.UUID) (*models.OAuthApplication, error) {
	var a models.OAuthApplication
	row := r.db.QueryRow(ctx, `SELECT `+oauthAppCols+` FROM oauth_applications WHERE id = $1 AND organization_id = $2`, id, orgID)
	if err := scanOAuthApp(row, &a); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

func (r *oauthRepository) GetApplicationByClientID(ctx context.Context, clientID string) (*models.OAuthApplication, error) {
	var a models.OAuthApplication
	row := r.db.QueryRow(ctx, `SELECT `+oauthAppCols+` FROM oauth_applications WHERE client_id = $1`, clientID)
	if err := scanOAuthApp(row, &a); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

func (r *oauthRepository) UpdateApplication(ctx context.Context, a *models.OAuthApplication) error {
	now := time.Now().UTC()
	a.UpdatedAt = now
	tag, err := r.db.Exec(ctx, `
		UPDATE oauth_applications SET name=$3, description=$4, logo_url=$5, website_url=$6,
			redirect_uris=$7, scopes=$8, status=$9, updated_at=$10
		WHERE id=$1 AND organization_id=$2`,
		a.ID, a.OrganizationID, a.Name, a.Description, a.LogoURL, a.WebsiteURL,
		a.RedirectURIs, int64(a.Scopes), string(a.Status), now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("application not found")
	}
	return nil
}

func (r *oauthRepository) UpdateApplicationSecret(ctx context.Context, orgID, id uuid.UUID, secretHash string) error {
	tag, err := r.db.Exec(ctx, `UPDATE oauth_applications SET client_secret_hash=$3, updated_at=now() WHERE id=$1 AND organization_id=$2`, id, orgID, secretHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("application not found")
	}
	return nil
}

func (r *oauthRepository) DeleteApplication(ctx context.Context, orgID, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM oauth_applications WHERE id=$1 AND organization_id=$2`, id, orgID)
	return err
}

func (r *oauthRepository) CreateAuthorizationCode(ctx context.Context, c *models.OAuthAuthorizationCode) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	c.CreatedAt = time.Now().UTC()
	_, err := r.db.Exec(ctx, `
		INSERT INTO oauth_authorization_codes (id, code_hash, application_id, organization_id, user_id,
			redirect_uri, scopes, code_challenge, code_challenge_method, expires_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		c.ID, c.CodeHash, c.ApplicationID, c.OrganizationID, c.UserID,
		c.RedirectURI, int64(c.Scopes), c.CodeChallenge, c.CodeChallengeMethod, c.ExpiresAt, c.CreatedAt)
	return err
}

func (r *oauthRepository) TakeAuthorizationCode(ctx context.Context, codeHash string) (*models.OAuthAuthorizationCode, error) {
	var c models.OAuthAuthorizationCode
	var scopes int64
	row := r.db.QueryRow(ctx, `
		UPDATE oauth_authorization_codes SET used_at = now()
		WHERE code_hash = $1 AND used_at IS NULL AND expires_at > now()
		RETURNING id, code_hash, application_id, organization_id, user_id, redirect_uri, scopes,
			code_challenge, code_challenge_method, used_at, expires_at, created_at`, codeHash)
	if err := row.Scan(&c.ID, &c.CodeHash, &c.ApplicationID, &c.OrganizationID, &c.UserID, &c.RedirectURI, &scopes,
		&c.CodeChallenge, &c.CodeChallengeMethod, &c.UsedAt, &c.ExpiresAt, &c.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	c.Scopes = uint64(scopes)
	return &c, nil
}

const oauthGrantCols = `id, application_id, organization_id, user_id, scopes, access_token_hash, refresh_token_hash,
	access_expires_at, refresh_expires_at, revoked_at, last_used_at, created_at`

func scanOAuthGrant(row pgx.Row, g *models.OAuthAccessGrant) error {
	var scopes int64
	var refreshHash *string
	if err := row.Scan(&g.ID, &g.ApplicationID, &g.OrganizationID, &g.UserID, &scopes, &g.AccessTokenHash, &refreshHash,
		&g.AccessExpiresAt, &g.RefreshExpiresAt, &g.RevokedAt, &g.LastUsedAt, &g.CreatedAt); err != nil {
		return err
	}
	g.Scopes = uint64(scopes)
	if refreshHash != nil {
		g.RefreshTokenHash = *refreshHash
	}
	return nil
}

func (r *oauthRepository) CreateAccessGrant(ctx context.Context, g *models.OAuthAccessGrant) error {
	if g.ID == uuid.Nil {
		g.ID = uuid.New()
	}
	g.CreatedAt = time.Now().UTC()
	var refreshHash *string
	if g.RefreshTokenHash != "" {
		refreshHash = &g.RefreshTokenHash
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO oauth_access_grants (id, application_id, organization_id, user_id, scopes,
			access_token_hash, refresh_token_hash, access_expires_at, refresh_expires_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		g.ID, g.ApplicationID, g.OrganizationID, g.UserID, int64(g.Scopes),
		g.AccessTokenHash, refreshHash, g.AccessExpiresAt, g.RefreshExpiresAt, g.CreatedAt)
	return err
}

func (r *oauthRepository) GetGrantByAccessTokenHash(ctx context.Context, hash string) (*models.OAuthAccessGrant, error) {
	var g models.OAuthAccessGrant
	row := r.db.QueryRow(ctx, `SELECT `+oauthGrantCols+` FROM oauth_access_grants WHERE access_token_hash = $1`, hash)
	if err := scanOAuthGrant(row, &g); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &g, nil
}

func (r *oauthRepository) GetGrantByRefreshTokenHash(ctx context.Context, hash string) (*models.OAuthAccessGrant, error) {
	var g models.OAuthAccessGrant
	row := r.db.QueryRow(ctx, `SELECT `+oauthGrantCols+` FROM oauth_access_grants WHERE refresh_token_hash = $1`, hash)
	if err := scanOAuthGrant(row, &g); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &g, nil
}

func (r *oauthRepository) RotateGrantTokens(ctx context.Context, id uuid.UUID, accessHash, refreshHash string, accessExp time.Time, refreshExp *time.Time) error {
	var refresh *string
	if refreshHash != "" {
		refresh = &refreshHash
	}
	_, err := r.db.Exec(ctx, `
		UPDATE oauth_access_grants SET access_token_hash=$2, refresh_token_hash=$3, access_expires_at=$4, refresh_expires_at=$5, last_used_at=now()
		WHERE id=$1`, id, accessHash, refresh, accessExp, refreshExp)
	return err
}

func (r *oauthRepository) TouchGrantLastUsed(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE oauth_access_grants SET last_used_at=now() WHERE id=$1`, id)
	return err
}

func (r *oauthRepository) RevokeGrant(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE oauth_access_grants SET revoked_at=now() WHERE id=$1 AND revoked_at IS NULL`, id)
	return err
}

func (r *oauthRepository) RevokeGrantByTokenHash(ctx context.Context, appID uuid.UUID, hash string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE oauth_access_grants SET revoked_at=now()
		WHERE application_id=$1 AND (access_token_hash=$2 OR refresh_token_hash=$2) AND revoked_at IS NULL`, appID, hash)
	return err
}

func (r *oauthRepository) ListAuthorizedApps(ctx context.Context, orgID, userID uuid.UUID) ([]models.OAuthAuthorizedApp, error) {
	rows, err := r.db.Query(ctx, `
		SELECT a.id, a.name, a.logo_url, a.website_url,
			bit_or(g.scopes)::bigint AS scopes, min(g.created_at) AS authorized_at, max(g.last_used_at) AS last_used_at
		FROM oauth_access_grants g
		JOIN oauth_applications a ON a.id = g.application_id
		WHERE g.organization_id = $1 AND g.user_id = $2 AND g.revoked_at IS NULL
		GROUP BY a.id, a.name, a.logo_url, a.website_url
		ORDER BY authorized_at DESC`, orgID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.OAuthAuthorizedApp{}
	for rows.Next() {
		var ap models.OAuthAuthorizedApp
		var scopes int64
		if err := rows.Scan(&ap.ApplicationID, &ap.Name, &ap.LogoURL, &ap.WebsiteURL, &scopes, &ap.AuthorizedAt, &ap.LastUsedAt); err != nil {
			return nil, err
		}
		ap.Scopes = uint64(scopes)
		out = append(out, ap)
	}
	return out, rows.Err()
}

func (r *oauthRepository) RevokeAuthorization(ctx context.Context, orgID, userID, appID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE oauth_access_grants SET revoked_at=now()
		WHERE organization_id=$1 AND user_id=$2 AND application_id=$3 AND revoked_at IS NULL`, orgID, userID, appID)
	return err
}

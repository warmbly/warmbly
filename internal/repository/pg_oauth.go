package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
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
	// UpdateApplicationLogo sets the logo the server stored for the app; "" clears it.
	UpdateApplicationLogo(ctx context.Context, orgID, id uuid.UUID, logoURL string) error
	// GetAllowedWebhookDomains fetches an app's webhook-domain allowlist by id
	// alone (no org), for delivery-time enforcement on app-scoped endpoints.
	GetAllowedWebhookDomains(ctx context.Context, id uuid.UUID) ([]string, error)
	// GetApplicationByID fetches an app by id alone (no org), for app-webhook
	// reconciliation which spans every org that authorized the app.
	GetApplicationByID(ctx context.Context, id uuid.UUID) (*models.OAuthApplication, error)
	// ListActiveGrantOrgs returns every org with an active (non-revoked) grant for
	// the app, with the union of scopes that org granted. Used to materialize the
	// app's per-org webhook endpoints.
	ListActiveGrantOrgs(ctx context.Context, appID uuid.UUID) ([]models.OAuthGrantOrg, error)

	// Authorization codes
	CreateAuthorizationCode(ctx context.Context, c *models.OAuthAuthorizationCode) error
	// TakeAuthorizationCode atomically consumes a valid, unexpired, unused code.
	TakeAuthorizationCode(ctx context.Context, codeHash string) (*models.OAuthAuthorizationCode, error)

	// Access grants
	CreateAccessGrant(ctx context.Context, g *models.OAuthAccessGrant) error
	GetGrantByAccessTokenHash(ctx context.Context, hash string) (*models.OAuthAccessGrant, error)
	GetGrantByRefreshTokenHash(ctx context.Context, hash string) (*models.OAuthAccessGrant, error)
	// RotateGrantTokens swaps the pair only while oldRefreshHash is still current; false means it was not.
	RotateGrantTokens(ctx context.Context, id uuid.UUID, oldRefreshHash, accessHash, refreshHash string, accessExp time.Time, refreshExp *time.Time) (bool, error)
	TouchGrantLastUsed(ctx context.Context, id uuid.UUID) error
	RevokeGrant(ctx context.Context, id uuid.UUID) error
	// RevokeGrantByPreviousRefresh ends the app's grant whose rotated-away refresh token has this hash.
	RevokeGrantByPreviousRefresh(ctx context.Context, appID uuid.UUID, refreshHash string) (bool, error)
	RevokeGrantByTokenHash(ctx context.Context, appID uuid.UUID, hash string) error
	ListAuthorizedApps(ctx context.Context, orgID, userID uuid.UUID) ([]models.OAuthAuthorizedApp, error)
	// ListWorkspaceAuthorizations lists every member's live app authorizations in orgID.
	ListWorkspaceAuthorizations(ctx context.Context, orgID uuid.UUID) ([]models.OAuthMemberAuthorization, error)
	RevokeAuthorization(ctx context.Context, orgID, userID, appID uuid.UUID) error
	RevokeMemberGrants(ctx context.Context, orgID, userID uuid.UUID) ([]uuid.UUID, error)

	// DeveloperBlock returns the operator's block on this workspace or person,
	// or nil when they may register and publish apps.
	DeveloperBlock(ctx context.Context, orgID, userID uuid.UUID) (*models.OAuthDeveloperBlock, error)
	// IsFeatured reports whether the app's directory listing is featured.
	IsFeatured(ctx context.Context, appID uuid.UUID) (bool, error)
}

type oauthRepository struct {
	db *pgxpool.Pool
	// enc seals the app webhook signing secret under the instance key; nil stores it as given.
	enc *encrypt.Encrypter
}

func NewOAuthRepository(db *pgxpool.Pool) OAuthRepository {
	return &oauthRepository{db: db}
}

// NewOAuthRepositorySealed is NewOAuthRepository with webhook-secret sealing on.
func NewOAuthRepositorySealed(db *pgxpool.Pool, enc *encrypt.Encrypter) OAuthRepository {
	return &oauthRepository{db: db, enc: enc}
}

// webhookSecretPrefix marks a plaintext signing secret.
const webhookSecretPrefix = "whsec_"

func (r *oauthRepository) sealWebhookSecret(plain string) (string, error) {
	if plain == "" {
		return plain, nil
	}
	if r.enc == nil {
		return "", errNoCredentialKey
	}
	return r.enc.Encrypt(plain)
}

// openWebhookSecret replaces the stored secret with its plaintext, resealing a row stored before sealing.
func (r *oauthRepository) openWebhookSecret(ctx context.Context, a *models.OAuthApplication) {
	stored := a.WebhookSecret
	if r.enc == nil || stored == "" {
		return
	}
	if plain, err := r.enc.Decrypt(stored); err == nil {
		a.WebhookSecret = plain
		return
	}
	if !strings.HasPrefix(stored, webhookSecretPrefix) {
		// Unreadable under this key: never hand ciphertext out as a signing secret.
		a.WebhookSecret = ""
		return
	}
	if sealed, err := r.enc.Encrypt(stored); err == nil {
		_, _ = r.db.Exec(ctx, `UPDATE oauth_applications SET webhook_secret = $2 WHERE id = $1 AND webhook_secret = $3`, a.ID, sealed, stored)
	}
}

const oauthAppCols = `id, organization_id, created_by, name, description, logo_url, website_url,
	client_id, client_secret_hash, redirect_uris, allowed_webhook_domains,
	webhook_url, webhook_events, webhook_secret, scopes, status, is_public, dynamically_registered,
	suspended_at, suspended_reason, created_at, updated_at`

// nullableUUID renders uuid.Nil as SQL NULL, so a dynamically-registered client
// (which has no owning org/user) writes NULL into the nullable FK columns rather
// than the all-zero uuid, which would violate the foreign key.
func nullableUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func scanOAuthApp(row pgx.Row, a *models.OAuthApplication) error {
	var scopes int64
	var status string
	// organization_id/created_by are nullable for dynamically-registered clients.
	var orgID, createdBy *uuid.UUID
	if err := row.Scan(&a.ID, &orgID, &createdBy, &a.Name, &a.Description, &a.LogoURL, &a.WebsiteURL,
		&a.ClientID, &a.ClientSecretHash, &a.RedirectURIs, &a.AllowedWebhookDomains,
		&a.WebhookURL, &a.WebhookEvents, &a.WebhookSecret, &scopes, &status, &a.IsPublic, &a.DynamicallyRegistered,
		&a.SuspendedAt, &a.SuspendedReason, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return err
	}
	if orgID != nil {
		a.OrganizationID = *orgID
	}
	if createdBy != nil {
		a.CreatedBy = *createdBy
	}
	a.Scopes = uint64(scopes)
	a.Status = models.OAuthAppStatus(status)
	if a.WebhookEvents == nil {
		a.WebhookEvents = []string{}
	}
	if a.RedirectURIs == nil {
		a.RedirectURIs = []string{}
	}
	if a.AllowedWebhookDomains == nil {
		a.AllowedWebhookDomains = []string{}
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
	webhookSecret, err := r.sealWebhookSecret(a.WebhookSecret)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `
		INSERT INTO oauth_applications (id, organization_id, created_by, name, description, logo_url, website_url,
			client_id, client_secret_hash, redirect_uris, allowed_webhook_domains,
			webhook_url, webhook_events, webhook_secret, scopes, status, is_public, dynamically_registered, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$19)`,
		a.ID, nullableUUID(a.OrganizationID), nullableUUID(a.CreatedBy), a.Name, a.Description, a.LogoURL, a.WebsiteURL,
		a.ClientID, a.ClientSecretHash, a.RedirectURIs, a.AllowedWebhookDomains,
		a.WebhookURL, a.WebhookEvents, webhookSecret, int64(a.Scopes), string(a.Status), a.IsPublic, a.DynamicallyRegistered, now)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range out {
		r.openWebhookSecret(ctx, &out[i])
	}
	return out, nil
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
	r.openWebhookSecret(ctx, &a)
	return &a, nil
}

func (r *oauthRepository) GetApplicationByID(ctx context.Context, id uuid.UUID) (*models.OAuthApplication, error) {
	var a models.OAuthApplication
	row := r.db.QueryRow(ctx, `SELECT `+oauthAppCols+` FROM oauth_applications WHERE id = $1`, id)
	if err := scanOAuthApp(row, &a); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	r.openWebhookSecret(ctx, &a)
	return &a, nil
}

func (r *oauthRepository) ListActiveGrantOrgs(ctx context.Context, appID uuid.UUID) ([]models.OAuthGrantOrg, error) {
	rows, err := r.db.Query(ctx, `
		SELECT organization_id, bit_or(scopes)
		FROM oauth_access_grants
		WHERE application_id = $1 AND `+grantIsLive+`
		GROUP BY organization_id
	`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.OAuthGrantOrg{}
	for rows.Next() {
		var g models.OAuthGrantOrg
		var scopes int64
		if err := rows.Scan(&g.OrgID, &scopes); err != nil {
			return nil, err
		}
		g.Scopes = uint64(scopes)
		out = append(out, g)
	}
	return out, rows.Err()
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
	r.openWebhookSecret(ctx, &a)
	return &a, nil
}

func (r *oauthRepository) UpdateApplication(ctx context.Context, a *models.OAuthApplication) error {
	now := time.Now().UTC()
	a.UpdatedAt = now
	webhookSecret, err := r.sealWebhookSecret(a.WebhookSecret)
	if err != nil {
		return err
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE oauth_applications SET name=$3, description=$4, logo_url=$5, website_url=$6,
			redirect_uris=$7, allowed_webhook_domains=$8, webhook_url=$9, webhook_events=$10,
			webhook_secret=$11, scopes=$12, status=$13, updated_at=$14
		WHERE id=$1 AND organization_id=$2`,
		a.ID, a.OrganizationID, a.Name, a.Description, a.LogoURL, a.WebsiteURL,
		a.RedirectURIs, a.AllowedWebhookDomains, a.WebhookURL, a.WebhookEvents,
		webhookSecret, int64(a.Scopes), string(a.Status), now)
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

func (r *oauthRepository) GetAllowedWebhookDomains(ctx context.Context, id uuid.UUID) ([]string, error) {
	var domains []string
	err := r.db.QueryRow(ctx, `SELECT allowed_webhook_domains FROM oauth_applications WHERE id=$1`, id).Scan(&domains)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if domains == nil {
		domains = []string{}
	}
	return domains, nil
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

func scanOAuthGrant(row pgx.Row, g *models.OAuthAccessGrant, extra ...any) error {
	var scopes int64
	var refreshHash *string
	dest := []any{&g.ID, &g.ApplicationID, &g.OrganizationID, &g.UserID, &scopes, &g.AccessTokenHash, &refreshHash,
		&g.AccessExpiresAt, &g.RefreshExpiresAt, &g.RevokedAt, &g.LastUsedAt, &g.CreatedAt}
	if err := row.Scan(append(dest, extra...)...); err != nil {
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

// grantIsLive is a grant neither revoked nor past its refresh lifetime.
const grantIsLive = `revoked_at IS NULL AND (refresh_expires_at IS NULL OR refresh_expires_at > now())`

// grantIsLiveAs is grantIsLive for the grants table under alias g.
const grantIsLiveAs = `g.revoked_at IS NULL AND (g.refresh_expires_at IS NULL OR g.refresh_expires_at > now())`

// grantHolderIsMember limits a token lookup to grants whose user still belongs to the grant's workspace.
const grantHolderIsMember = ` AND EXISTS (SELECT 1 FROM organization_members m
	WHERE m.organization_id = oauth_access_grants.organization_id AND m.user_id = oauth_access_grants.user_id)`

// A token stops working the moment its holder is banned from signing in.
const grantHolderNotBanned = ` AND NOT EXISTS (SELECT 1 FROM users u
	WHERE u.id = oauth_access_grants.user_id AND (u.ban_scope & 1) <> 0)`

// A token works only while its app is enabled by its owner and not suspended.
const grantAppIsUsable = ` AND EXISTS (SELECT 1 FROM oauth_applications a
	WHERE a.id = oauth_access_grants.application_id AND a.status = 'active' AND a.suspended_at IS NULL)`

// grantByTokenHash reads a usable grant by one of its token hashes, with its holder's current membership.
func (r *oauthRepository) grantByTokenHash(ctx context.Context, column, hash string) (*models.OAuthAccessGrant, error) {
	var g models.OAuthAccessGrant
	holder := &models.OrganizationMember{}
	row := r.db.QueryRow(ctx, `SELECT `+oauthGrantCols+`, holder.role, holder.permissions, holder.access_scope
		FROM oauth_access_grants
		CROSS JOIN LATERAL (SELECT m.role, m.permissions, m.access_scope FROM organization_members m
			WHERE m.organization_id = oauth_access_grants.organization_id AND m.user_id = oauth_access_grants.user_id) holder
		WHERE `+column+` = $1`+grantHolderIsMember+grantHolderNotBanned+grantAppIsUsable, hash)
	if err := scanOAuthGrant(row, &g, &holder.Role, &holder.Permissions, &holder.AccessScope); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	holder.OrganizationID = g.OrganizationID
	holder.UserID = g.UserID
	g.Holder = holder
	return &g, nil
}

func (r *oauthRepository) GetGrantByAccessTokenHash(ctx context.Context, hash string) (*models.OAuthAccessGrant, error) {
	return r.grantByTokenHash(ctx, "access_token_hash", hash)
}

func (r *oauthRepository) GetGrantByRefreshTokenHash(ctx context.Context, hash string) (*models.OAuthAccessGrant, error) {
	return r.grantByTokenHash(ctx, "refresh_token_hash", hash)
}

func (r *oauthRepository) RotateGrantTokens(ctx context.Context, id uuid.UUID, oldRefreshHash, accessHash, refreshHash string, accessExp time.Time, refreshExp *time.Time) (bool, error) {
	var refresh *string
	if refreshHash != "" {
		refresh = &refreshHash
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE oauth_access_grants SET previous_refresh_token_hash=refresh_token_hash, access_token_hash=$2, refresh_token_hash=$3,
			access_expires_at=$4, refresh_expires_at=$5, last_used_at=now()
		WHERE id=$1 AND refresh_token_hash=$6 AND revoked_at IS NULL`, id, accessHash, refresh, accessExp, refreshExp, oldRefreshHash)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *oauthRepository) RevokeGrantByPreviousRefresh(ctx context.Context, appID uuid.UUID, refreshHash string) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE oauth_access_grants SET revoked_at = now()
		WHERE application_id = $1 AND previous_refresh_token_hash = $2 AND revoked_at IS NULL`, appID, refreshHash)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
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
		WHERE g.organization_id = $1 AND g.user_id = $2 AND `+grantIsLiveAs+`
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

func (r *oauthRepository) ListWorkspaceAuthorizations(ctx context.Context, orgID uuid.UUID) ([]models.OAuthMemberAuthorization, error) {
	rows, err := r.db.Query(ctx, `
		SELECT a.id, a.name, a.logo_url, a.website_url, g.user_id, COALESCE(u.email, ''),
			trim(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')),
			bit_or(g.scopes)::bigint, min(g.created_at), max(g.last_used_at)
		FROM oauth_access_grants g
		JOIN oauth_applications a ON a.id = g.application_id
		LEFT JOIN users u ON u.id = g.user_id
		WHERE g.organization_id = $1 AND `+grantIsLiveAs+`
		GROUP BY a.id, a.name, a.logo_url, a.website_url, g.user_id, u.email, u.first_name, u.last_name
		ORDER BY min(g.created_at) DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.OAuthMemberAuthorization{}
	for rows.Next() {
		var ap models.OAuthMemberAuthorization
		var scopes int64
		if err := rows.Scan(&ap.ApplicationID, &ap.Name, &ap.LogoURL, &ap.WebsiteURL, &ap.UserID, &ap.UserEmail, &ap.UserName,
			&scopes, &ap.AuthorizedAt, &ap.LastUsedAt); err != nil {
			return nil, err
		}
		ap.Scopes = uint64(scopes)
		out = append(out, ap)
	}
	return out, rows.Err()
}

// RevokeMemberGrants revokes every live grant userID holds in orgID and returns the apps they were for.
func (r *oauthRepository) RevokeMemberGrants(ctx context.Context, orgID, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `
		WITH revoked AS (
			UPDATE oauth_access_grants SET revoked_at=now()
			WHERE organization_id=$1 AND user_id=$2 AND revoked_at IS NULL
			RETURNING application_id
		)
		SELECT DISTINCT application_id FROM revoked`, orgID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var apps []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		apps = append(apps, id)
	}
	return apps, rows.Err()
}

func (r *oauthRepository) RevokeAuthorization(ctx context.Context, orgID, userID, appID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE oauth_access_grants SET revoked_at=now()
		WHERE organization_id=$1 AND user_id=$2 AND application_id=$3 AND revoked_at IS NULL`, orgID, userID, appID)
	return err
}

func (r *oauthRepository) DeveloperBlock(ctx context.Context, orgID, userID uuid.UUID) (*models.OAuthDeveloperBlock, error) {
	var b models.OAuthDeveloperBlock
	err := r.db.QueryRow(ctx, `
		SELECT id, organization_id, user_id, reason, created_at FROM oauth_developer_blocks
		WHERE organization_id = $1 OR (user_id = $2 AND $2 <> '00000000-0000-0000-0000-000000000000'::uuid)
		ORDER BY organization_id NULLS LAST LIMIT 1`, orgID, userID).Scan(&b.ID, &b.OrganizationID, &b.UserID, &b.Reason, &b.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *oauthRepository) UpdateApplicationLogo(ctx context.Context, orgID, id uuid.UUID, logoURL string) error {
	tag, err := r.db.Exec(ctx, `UPDATE oauth_applications SET logo_url = $3, updated_at = now() WHERE organization_id = $1 AND id = $2`, orgID, id, logoURL)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *oauthRepository) IsFeatured(ctx context.Context, appID uuid.UUID) (bool, error) {
	var featured bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM app_directory_listings WHERE application_id = $1 AND status = 'featured')`, appID).Scan(&featured)
	return featured, err
}

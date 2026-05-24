package seed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// SeedAPIKeySecret is the predictable plaintext API key seeded for the Acme
// owner. It uses the same wmbly_ prefix as production keys so the auth flow
// treats it identically. In production keys are randomly generated and only
// shown once; the seed gives up that property in exchange for being
// reproducible across re-runs.
const SeedAPIKeySecret = "wmbly_seed_acme_owner_full_access_0000000000"

func seedAPIKeys(ctx context.Context, pool *pgxpool.Pool, r *Result) error {
	sum := sha256.Sum256([]byte(SeedAPIKeySecret))
	hash := hex.EncodeToString(sum[:])
	prefix := SeedAPIKeySecret[:8]

	_, err := pool.Exec(ctx, `
		INSERT INTO api_keys (id, user_id, organization_id, name, key_prefix, key_hash, permissions, status, created_at, updated_at)
		VALUES ($1, $2, $3, 'Seed - full access', $4, $5, $6, 'active', NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			key_prefix = EXCLUDED.key_prefix,
			key_hash = EXCLUDED.key_hash,
			permissions = EXCLUDED.permissions,
			status = 'active',
			updated_at = NOW()
	`, APIKeyAcmeID, UserOwnerID, OrgAcmeID, prefix, hash, int64(models.APIPermFullAccess))
	if err != nil {
		return err
	}

	r.APIKeySecret = SeedAPIKeySecret
	r.APIKeyPrefix = prefix
	return nil
}

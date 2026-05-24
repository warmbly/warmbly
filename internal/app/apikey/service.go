package apikey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// KeyPrefix is the human-readable brand on every key. Single environment
	// for now — once a sandbox exists we can introduce wmbly_live_ / wmbly_test_
	// without breaking existing keys (they'd just stay in the unprefixed bucket).
	KeyPrefix = "wmbly_"

	// 32 random bytes = 256 bits of entropy. Encodes to a 43-char base64url
	// string, so a full key is "wmbly_" + 43 = 49 chars.
	KeyLength = 32

	// CacheKeyTTL is how long a (key_hash → key_id) lookup is cached.
	// We still hit the DB on every request to pick up revocations, so the
	// cache only saves a single index seek.
	CacheKeyTTL = 300

	// Display prefix shown in lists: "wmbly_" + first 2 random chars.
	displayPrefixLen = 8
	// Display suffix shown in lists: last 4 random chars.
	displaySuffixLen = 4

	// Soft caps on per-key scoping lists. Keep the rows small + cheap to scan.
	maxAllowedIPs           = 64
	maxAllowedEmailAccounts = 128
)

type APIKeyService interface {
	Create(ctx context.Context, orgID, userID uuid.UUID, data *models.CreateAPIKey) (*models.APIKeyWithSecret, *errx.Error)
	Get(ctx context.Context, orgID, keyID uuid.UUID) (*models.APIKey, *errx.Error)
	List(ctx context.Context, orgID uuid.UUID, limit int, cursor *uuid.UUID) (*models.APIKeysResult, *errx.Error)
	Update(ctx context.Context, orgID, keyID uuid.UUID, data *models.UpdateAPIKey) (*models.APIKey, *errx.Error)
	Revoke(ctx context.Context, orgID, keyID uuid.UUID, reason string) *errx.Error

	// Validation
	ValidateKey(ctx context.Context, rawKey string) (*models.APIKey, *errx.Error)
	ValidateKeyIP(key *models.APIKey, ip string) bool
	ValidateKeyPermission(key *models.APIKey, permission uint64) bool

	// Usage tracking
	UpdateLastUsed(ctx context.Context, keyID uuid.UUID)
	LogUsage(ctx context.Context, log *models.APIKeyUsageLog)
}

type apiKeyService struct {
	repo  repository.APIKeyRepository
	cache *cache.Cache
}

func NewService(cache *cache.Cache, repo repository.APIKeyRepository) APIKeyService {
	return &apiKeyService{
		repo:  repo,
		cache: cache,
	}
}

// generateKey returns a freshly-minted API key plus the display prefix,
// display suffix, and SHA-256 hash to persist. SHA-256 (no salt) is
// appropriate here: the input has 256 bits of entropy, so rainbow tables
// and brute force are computationally infeasible.
func generateKey() (rawKey, prefix, suffix, hash string, err error) {
	randomBytes := make([]byte, KeyLength)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", "", "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(randomBytes)
	rawKey = KeyPrefix + encoded

	prefix = rawKey[:displayPrefixLen]
	suffix = rawKey[len(rawKey)-displaySuffixLen:]

	hash = hashKey(rawKey)
	return rawKey, prefix, suffix, hash, nil
}

func hashKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}

// validateAllowedIPs accepts either a bare IP ("1.2.3.4") or a CIDR
// block ("10.0.0.0/8", "2001:db8::/32"). Returns the canonical form
// to store (so equality comparisons are stable) plus an error if any
// entry is unparseable.
func validateAllowedIPs(entries []string) ([]string, *errx.Error) {
	if len(entries) > maxAllowedIPs {
		return nil, errx.New(errx.BadRequest, fmt.Sprintf("at most %d allowed_ips entries", maxAllowedIPs))
	}
	out := make([]string, 0, len(entries))
	for _, raw := range entries {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if strings.Contains(s, "/") {
			_, ipnet, err := net.ParseCIDR(s)
			if err != nil {
				return nil, errx.New(errx.BadRequest, fmt.Sprintf("invalid CIDR: %s", raw))
			}
			out = append(out, ipnet.String())
			continue
		}
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, errx.New(errx.BadRequest, fmt.Sprintf("invalid IP: %s", raw))
		}
		out = append(out, ip.String())
	}
	return out, nil
}

func (s *apiKeyService) Create(ctx context.Context, orgID, userID uuid.UUID, data *models.CreateAPIKey) (*models.APIKeyWithSecret, *errx.Error) {
	if len(data.Name) == 0 || len(data.Name) > 255 {
		return nil, errx.New(errx.BadRequest, "name must be between 1 and 255 characters")
	}
	if data.Permissions == 0 {
		return nil, errx.New(errx.BadRequest, "at least one permission is required")
	}
	if data.Permissions&^models.AllAPIPermissionsMask != 0 {
		return nil, errx.New(errx.BadRequest, "permissions bitmask contains unknown bits")
	}
	if data.ExpiresAt != nil && data.ExpiresAt.Before(time.Now()) {
		return nil, errx.New(errx.BadRequest, "expires_at must be in the future")
	}
	if len(data.AllowedEmailAccounts) > maxAllowedEmailAccounts {
		return nil, errx.New(errx.BadRequest, fmt.Sprintf("at most %d allowed_email_accounts entries", maxAllowedEmailAccounts))
	}

	allowedIPs, xerr := validateAllowedIPs(data.AllowedIPs)
	if xerr != nil {
		return nil, xerr
	}
	data.AllowedIPs = allowedIPs

	rawKey, prefix, suffix, hash, err := generateKey()
	if err != nil {
		return nil, errx.InternalError()
	}

	key, xerr := s.repo.Create(ctx, orgID, userID, data, prefix, suffix, hash)
	if xerr != nil {
		return nil, xerr
	}

	return &models.APIKeyWithSecret{
		APIKey: *key,
		Secret: rawKey,
	}, nil
}

func (s *apiKeyService) Get(ctx context.Context, orgID, keyID uuid.UUID) (*models.APIKey, *errx.Error) {
	return s.repo.GetByID(ctx, orgID, keyID)
}

func (s *apiKeyService) List(ctx context.Context, orgID uuid.UUID, limit int, cursor *uuid.UUID) (*models.APIKeysResult, *errx.Error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.repo.List(ctx, orgID, limit, cursor)
}

func (s *apiKeyService) Update(ctx context.Context, orgID, keyID uuid.UUID, data *models.UpdateAPIKey) (*models.APIKey, *errx.Error) {
	if data.Name != nil && (len(*data.Name) == 0 || len(*data.Name) > 255) {
		return nil, errx.New(errx.BadRequest, "name must be between 1 and 255 characters")
	}
	if data.Permissions != nil {
		if *data.Permissions == 0 {
			return nil, errx.New(errx.BadRequest, "at least one permission is required")
		}
		if *data.Permissions&^models.AllAPIPermissionsMask != 0 {
			return nil, errx.New(errx.BadRequest, "permissions bitmask contains unknown bits")
		}
	}
	if data.AllowedEmailAccounts != nil && len(data.AllowedEmailAccounts) > maxAllowedEmailAccounts {
		return nil, errx.New(errx.BadRequest, fmt.Sprintf("at most %d allowed_email_accounts entries", maxAllowedEmailAccounts))
	}
	if data.AllowedIPs != nil {
		allowedIPs, xerr := validateAllowedIPs(data.AllowedIPs)
		if xerr != nil {
			return nil, xerr
		}
		data.AllowedIPs = allowedIPs
	}

	return s.repo.Update(ctx, orgID, keyID, data)
}

func (s *apiKeyService) Revoke(ctx context.Context, orgID, keyID uuid.UUID, reason string) *errx.Error {
	xerr := s.repo.Revoke(ctx, orgID, keyID, reason)
	if xerr != nil {
		return xerr
	}
	// Best-effort cache invalidation so a revoked key stops authenticating
	// even within the CacheKeyTTL window. The cache key is the hash, which
	// we don't have on hand, so we drop the by-id mapping if we cache it
	// later. For now the GetByHash query filters status='active', so the
	// revoke takes effect on the next request regardless of cache state.
	return nil
}

func (s *apiKeyService) ValidateKey(ctx context.Context, rawKey string) (*models.APIKey, *errx.Error) {
	if !strings.HasPrefix(rawKey, KeyPrefix) {
		return nil, errx.ErrAuth
	}

	hash := hashKey(rawKey)

	cacheKey := fmt.Sprintf("apikey:%s", hash)
	if s.cache != nil {
		if cached, err := s.cache.Get(ctx, cacheKey).Result(); err == nil && cached != "" {
			if _, err := uuid.Parse(cached); err == nil {
				if key, xerr := s.repo.GetByHash(ctx, hash); xerr == nil {
					return key, nil
				}
			}
		}
	}

	key, xerr := s.repo.GetByHash(ctx, hash)
	if xerr != nil {
		return nil, xerr
	}

	if s.cache != nil {
		s.cache.Set(ctx, cacheKey, key.ID.String(), CacheKeyTTL)
	}

	return key, nil
}

// ValidateKeyIP returns true when the request IP is allowed by the key's
// allowlist. An empty allowlist means "any IP". Entries can be bare IPs
// (exact match) or CIDR blocks.
func (s *apiKeyService) ValidateKeyIP(key *models.APIKey, ip string) bool {
	if len(key.AllowedIPs) == 0 {
		return true
	}

	parsed := net.ParseIP(ip)
	if parsed == nil {
		// Caller gave us a malformed IP (or the client IP couldn't be
		// resolved). Fail closed when an allowlist is configured.
		return false
	}

	for _, allowed := range key.AllowedIPs {
		if strings.Contains(allowed, "/") {
			_, ipnet, err := net.ParseCIDR(allowed)
			if err != nil {
				continue
			}
			if ipnet.Contains(parsed) {
				return true
			}
			continue
		}
		if other := net.ParseIP(allowed); other != nil && other.Equal(parsed) {
			return true
		}
	}

	return false
}

func (s *apiKeyService) ValidateKeyPermission(key *models.APIKey, permission uint64) bool {
	return models.HasAPIPermission(key.Permissions, permission)
}

func (s *apiKeyService) UpdateLastUsed(ctx context.Context, keyID uuid.UUID) {
	// Fire-and-forget; the request shouldn't wait on a stats update.
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.repo.UpdateLastUsed(bgCtx, keyID)
	}()
}

func (s *apiKeyService) LogUsage(ctx context.Context, log *models.APIKeyUsageLog) {
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.repo.LogUsage(bgCtx, log)
	}()
}

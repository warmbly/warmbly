package apikey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	KeyPrefix   = "wmbly_"
	KeyLength   = 32  // 32 bytes = 256 bits of randomness
	CacheKeyTTL = 300 // 5 minutes cache for key lookups
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

// generateKey generates a secure random API key with the format: wmbly_<base64_random>
func generateKey() (rawKey, prefix, hash string, err error) {
	// Generate random bytes
	randomBytes := make([]byte, KeyLength)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Encode to base64 (URL-safe, no padding)
	encoded := base64.RawURLEncoding.EncodeToString(randomBytes)

	// Build the full key
	rawKey = KeyPrefix + encoded

	// Get the display prefix (first 8 chars including wmbly_)
	prefix = rawKey[:8]

	// Hash the key for storage
	hasher := sha256.New()
	hasher.Write([]byte(rawKey))
	hash = hex.EncodeToString(hasher.Sum(nil))

	return rawKey, prefix, hash, nil
}

// hashKey hashes a raw key for lookup
func hashKey(rawKey string) string {
	hasher := sha256.New()
	hasher.Write([]byte(rawKey))
	return hex.EncodeToString(hasher.Sum(nil))
}

func (s *apiKeyService) Create(ctx context.Context, orgID, userID uuid.UUID, data *models.CreateAPIKey) (*models.APIKeyWithSecret, *errx.Error) {
	// Validate name
	if len(data.Name) == 0 || len(data.Name) > 255 {
		return nil, errx.New(errx.BadRequest, "Name must be between 1 and 255 characters")
	}

	// Validate permissions
	if data.Permissions == 0 {
		return nil, errx.New(errx.BadRequest, "At least one permission is required")
	}

	// Generate the key
	rawKey, prefix, hash, err := generateKey()
	if err != nil {
		return nil, errx.InternalError()
	}

	// Create in database
	key, xerr := s.repo.Create(ctx, orgID, userID, data, prefix, hash)
	if xerr != nil {
		return nil, xerr
	}

	// Return key with secret (only time it's shown)
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
	// Validate name if provided
	if data.Name != nil && (len(*data.Name) == 0 || len(*data.Name) > 255) {
		return nil, errx.New(errx.BadRequest, "Name must be between 1 and 255 characters")
	}

	return s.repo.Update(ctx, orgID, keyID, data)
}

func (s *apiKeyService) Revoke(ctx context.Context, orgID, keyID uuid.UUID, reason string) *errx.Error {
	return s.repo.Revoke(ctx, orgID, keyID, reason)
}

func (s *apiKeyService) ValidateKey(ctx context.Context, rawKey string) (*models.APIKey, *errx.Error) {
	// Check key format
	if len(rawKey) < len(KeyPrefix) || rawKey[:len(KeyPrefix)] != KeyPrefix {
		return nil, errx.ErrAuth
	}

	// Hash the key
	hash := hashKey(rawKey)

	// Try cache first
	cacheKey := fmt.Sprintf("apikey:%s", hash)
	if s.cache != nil {
		if cached, err := s.cache.Get(ctx, cacheKey).Result(); err == nil && cached != "" {
			// Parse cached key ID and fetch fresh from DB
			if _, err := uuid.Parse(cached); err == nil {
				key, xerr := s.repo.GetByHash(ctx, hash)
				if xerr == nil {
					return key, nil
				}
			}
		}
	}

	// Look up by hash
	key, xerr := s.repo.GetByHash(ctx, hash)
	if xerr != nil {
		return nil, xerr
	}

	// Cache the key ID for future lookups
	if s.cache != nil {
		s.cache.Set(ctx, cacheKey, key.ID.String(), CacheKeyTTL)
	}

	return key, nil
}

func (s *apiKeyService) ValidateKeyIP(key *models.APIKey, ip string) bool {
	// If no IP restrictions, allow all
	if len(key.AllowedIPs) == 0 {
		return true
	}

	// Check if IP is in allowed list
	for _, allowed := range key.AllowedIPs {
		if allowed == ip {
			return true
		}
	}

	return false
}

func (s *apiKeyService) ValidateKeyPermission(key *models.APIKey, permission uint64) bool {
	return models.HasAPIPermission(key.Permissions, permission)
}

func (s *apiKeyService) UpdateLastUsed(ctx context.Context, keyID uuid.UUID) {
	// Fire and forget - don't block the request, but use a proper timeout
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.repo.UpdateLastUsed(bgCtx, keyID)
	}()
}

func (s *apiKeyService) LogUsage(ctx context.Context, log *models.APIKeyUsageLog) {
	// Fire and forget - don't block the request, but use a proper timeout
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.repo.LogUsage(bgCtx, log)
	}()
}

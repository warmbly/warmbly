package middleware

import (
	"github.com/warmbly/warmbly/internal/app/apikey"
	"github.com/warmbly/warmbly/internal/app/idempotency"
	"github.com/warmbly/warmbly/internal/app/oauth"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/ratelimit"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
)

type Handler struct {
	TokenService        token.TokenService
	APIKeyService       apikey.APIKeyService
	IdempotencyService  idempotency.Service
	RateLimitService    ratelimit.RateLimitService
	OrganizationService organization.OrganizationService
	// OAuthService validates OAuth 2.1 bearer access tokens (nil-safe: when
	// unset, only JWT + API-key auth are accepted).
	OAuthService *oauth.Service
	// Cache backs the pre-login per-IP limiter, which cannot use
	// RateLimitService because that one is keyed on a user id that does not
	// exist yet.
	Cache *cache.Cache
}

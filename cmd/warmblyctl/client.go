package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/app/trial"
	"github.com/warmbly/warmbly/internal/app/user"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/repository"
)

// conn is the object graph the backend boots with, minus everything a CLI
// never needs. Redis is attached separately because the commands that matter
// most run while it is down.
type conn struct {
	db     *db.DB
	cache  *cache.Cache
	users  repository.UserRepository
	admins repository.AdminRepository
	auth   repository.AuthRepository
	totp   repository.TOTPRepository
}

func connect(ctx context.Context) (*conn, error) {
	dsn, err := dbEndpoint(ctx)
	if err != nil {
		return nil, err
	}

	handle, err := db.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("PRIMARY_DB is not a usable connection string (%s): %w", redact(dsn), err)
	}
	if err := handle.Ping(ctx); err != nil {
		handle.Close()
		return nil, fmt.Errorf("could not reach postgres at %s: %w\nStart the database, then run this again. Inside the backend container the value is already correct.", redact(dsn), err)
	}

	// The user repository's kms handle is only used by paths this CLI never
	// calls, so nil keeps the binary free of AWS credentials.
	return &conn{
		db:     handle,
		users:  repository.NewUserRepostory(handle, nil),
		admins: repository.NewAdminRepository(handle.Pool),
		auth:   repository.NewAuthRepostory(handle),
		totp:   repository.NewTOTPRepository(handle.Pool),
	}, nil
}

func (c *conn) close() {
	if c.db != nil {
		c.db.Close()
	}
}

// openCache attaches Redis. When required is false an unreachable Redis is a
// warning, not a failure, so an outage never blocks account recovery.
func (c *conn) openCache(ctx context.Context, required bool, consequence string) error {
	endpoint, err := redisEndpoint(ctx)
	if err != nil {
		if required {
			return err
		}
		warn("%v %s", err, consequence)
		return nil
	}

	client, cerr := cache.New(endpoint)
	if cerr != nil {
		if required {
			return fmt.Errorf("could not reach Redis at %s: %w\nStart Redis, then run this again.", redact(endpoint), cerr)
		}
		warn("could not reach Redis at %s. %s", redact(endpoint), consequence)
		return nil
	}

	c.cache = client
	return nil
}

func (c *conn) userService() user.UserService {
	return user.NewService(c.users, c.cache)
}

// orgService passes no daily-creation throttle: the throttle exists to stop a
// script spawning workspaces through the API, and the CLI is already root.
func (c *conn) orgService() organization.OrganizationService {
	return organization.NewService(
		repository.NewOrganizationRepository(c.db.Pool),
		repository.NewSubscriptionRepository(c.db.Pool),
		c.users,
		nil,
	)
}

func (c *conn) trialService() trial.TrialService {
	creditSvc := credits.NewService(
		repository.NewCreditRepository(c.db),
		repository.NewAISettingsRepository(c.db),
		c.cache,
	)
	return trial.NewService(
		repository.NewSubscriptionRepository(c.db.Pool),
		c.users,
		repository.NewPlanRepository(c.db.Pool),
		creditSvc,
	)
}

func (c *conn) tokenService(secret string) token.TokenService {
	return token.NewService(c.db, repository.NewTokenRepostory(c.db), c.cache, nil, secret)
}

func dbEndpoint(ctx context.Context) (string, error) {
	if v := strings.TrimSpace(os.Getenv("PRIMARY_DB")); v != "" {
		return v, nil
	}
	if cfg, err := config.NewConfig(ctx); err == nil {
		if v, verr := cfg.LoadPrimaryDBEndpoint(ctx); verr == nil && v != "" {
			return v, nil
		}
	}
	return "", errors.New("PRIMARY_DB is not set, so there is no database to talk to.\nRun this inside the backend container, where the environment is already correct:\n  docker compose -p warmbly exec backend warmblyctl status\nOr export PRIMARY_DB first.")
}

func redisEndpoint(ctx context.Context) (string, error) {
	if v := strings.TrimSpace(os.Getenv("REDIS")); v != "" {
		return v, nil
	}
	if cfg, err := config.NewConfig(ctx); err == nil {
		if v, verr := cfg.LoadPrimaryRedisEndpoint(ctx); verr == nil && v != "" {
			return v, nil
		}
	}
	return "", errors.New("REDIS is not set, so there is no cache to talk to.")
}

// authSecret is the JWT signing key. It must match the backend's, or the link
// this CLI prints is rejected the moment it is opened.
func authSecret(ctx context.Context) (string, error) {
	if v := strings.TrimSpace(os.Getenv("AUTH_SECRET")); v != "" {
		return v, nil
	}
	cfg, err := config.NewConfig(ctx)
	if err == nil {
		if authCfg, aerr := cfg.LoadAuthConfig(ctx); aerr == nil && authCfg.AuthSecret != "" {
			return authCfg.AuthSecret, nil
		}
	}
	return "", errors.New("AUTH_SECRET is not set, so a reset link cannot be signed with the key the backend verifies.\nRun this inside the backend container, or set the password directly instead:\n  warmblyctl user reset-password --email you@example.com --password-stdin")
}

// redact strips the password out of a connection string before it is printed.
func redact(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.User == nil {
		return endpoint
	}
	if name := parsed.User.Username(); name != "" {
		parsed.User = url.UserPassword(name, "redacted")
	}
	return parsed.String()
}

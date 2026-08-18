package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/orgtransfer"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/app/trial"
	"github.com/warmbly/warmbly/internal/app/user"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/infrastructure/encryptedkeys"
	"github.com/warmbly/warmbly/internal/infrastructure/kms"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
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

// orgTransferService builds the workspace archive engine.
//
// This is the one command family that needs the crypto stack, because moving a
// workspace means opening every sealed value on the way out and re-sealing it
// on the way in. Both key domains are wired here: KMS (behind the per-org DEK)
// and the instance credential key.
//
// Object storage is deliberately not wired. The CLI writes archives to a file
// on the box it runs on, and attachment bytes are the only thing that needs a
// blob store, which the export reports as absent rather than failing over.
func (c *conn) orgTransferService(ctx context.Context) (orgtransfer.Service, error) {
	// Redis only caches decrypted keys, so an outage costs a KMS round trip per
	// organization rather than the command.
	_ = c.openCache(ctx, false, "Sealed values will be unwrapped through KMS on every read, which is slower but correct.")

	masterKey := "alias/master-key"
	if cfg, err := config.NewConfig(ctx); err == nil && cfg.Env != "prod" {
		masterKey += "-dev"
	}

	var awscfg aws.Config
	if loaded, err := awsconfig.LoadDefaultConfig(ctx); err == nil {
		awscfg = loaded
	}

	keyStore, err := kms.FromEnv(ctx, awscfg, masterKey)
	if err != nil {
		return nil, fmt.Errorf("this instance's KMS provider could not be opened, so sealed values cannot be read: %w", err)
	}

	encryptedKeys, err := encryptedkeys.FromEnv(encryptedkeys.Deps{DB: c.db}, "postgres")
	if err != nil {
		return nil, fmt.Errorf("the encrypted-key store could not be opened: %w", err)
	}

	// A missing CREDENTIALS_ENCRYPTION_KEY is not fatal: an export without
	// credentials still works, and the engine reports the gap per mailbox.
	credEncrypter, err := encrypt.FromEnv()
	if err != nil {
		return nil, fmt.Errorf("CREDENTIALS_ENCRYPTION_KEY is set but unusable: %w", err)
	}
	if credEncrypter == nil {
		warn("CREDENTIALS_ENCRYPTION_KEY is not set, so mailbox credentials cannot be read or written by this command.")
	}

	return orgtransfer.NewService(
		repository.NewOrgTransferRepository(c.db),
		cipher.NewService(keyStore, c.cache, encryptedKeys),
		credEncrypter,
		nil,
		orgtransfer.InstanceInfo{
			PublicURL:  strings.TrimSpace(os.Getenv("APP_URL")),
			AppVersion: strings.TrimSpace(os.Getenv("APP_VERSION")),
		},
	), nil
}

// resolveOrg accepts whatever an operator is most likely to have to hand: the
// organization's id, its slug, or the email of the person who owns it.
func (c *conn) resolveOrg(ctx context.Context, ref string) (uuid.UUID, string, error) {
	ref = strings.TrimSpace(ref)

	var id uuid.UUID
	var name string
	err := c.db.QueryRow(ctx, `
		SELECT o.id, o.name
		  FROM organizations o
		  LEFT JOIN users u ON u.id = o.owner_user_id
		 WHERE o.id::text = $1
		    OR lower(o.slug) = lower($1)
		    OR lower(u.email) = lower($1)
		 ORDER BY o.created_at
		 LIMIT 1
	`, ref).Scan(&id, &name)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("no organization matches %q. Run `warmblyctl org list` to see what is on this instance.", ref)
	}
	return id, name, nil
}

// orgOwnerID returns the workspace owner, who inherits any imported row whose
// original owner has no account on this instance.
func (c *conn) orgOwnerID(ctx context.Context, orgID uuid.UUID) (uuid.UUID, error) {
	var owner uuid.UUID
	if err := c.db.QueryRow(ctx, `SELECT owner_user_id FROM organizations WHERE id = $1`, orgID).Scan(&owner); err != nil {
		return uuid.Nil, fmt.Errorf("reading the organization's owner: %w", err)
	}
	return owner, nil
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

package config

import (
	"context"
	"fmt"
	"os"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/warmbly/warmbly/internal/infrastructure/secrets"
	"github.com/warmbly/warmbly/internal/infrastructure/ssm"
)

type Config struct {
	Env              string // "dev" or "prod"
	AWSConfigEnabled bool
	params           *ssm.SSMParameterStore
	secrets          *secrets.SecretsManagerClient
}

// NewConfig creates a new config instance with env-first loading and optional AWS fallback.
// AWS clients are only initialized if AWS_CONFIG_ENABLED=true.
func NewConfig(ctx context.Context) (*Config, error) {
	env := getEnvOrDefault("APP_ENV", "dev")
	awsEnabled := getEnvOrDefault("AWS_CONFIG_ENABLED", "false") == "true"

	cfg := &Config{
		Env:              env,
		AWSConfigEnabled: awsEnabled,
	}

	if awsEnabled {
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load AWS config: %w", err)
		}

		params, err := ssm.NewSSMParameterStore(ctx, awsCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create SSM client: %w", err)
		}
		cfg.params = params

		secretsClient, err := secrets.NewSecretsManagerClient(ctx, awsCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create Secrets Manager client: %w", err)
		}
		cfg.secrets = secretsClient
	}

	return cfg, nil
}

// Load creates a Config with pre-initialized AWS clients (legacy compatibility).
// Deprecated: Use NewConfig for new code.
func Load(params *ssm.SSMParameterStore, secrets *secrets.SecretsManagerClient) *Config {
	env := getEnvOrDefault("APP_ENV", "dev")

	return &Config{
		Env:              env,
		AWSConfigEnabled: true, // Legacy mode always has AWS enabled
		params:           params,
		secrets:          secrets,
	}
}

// GetKeyID returns the full AWS parameter/secret path with environment prefix.
func (c *Config) GetKeyID(s string) string {
	return "/warmbly/" + c.Env + "/" + s
}

// getEnvOrDefault returns the environment variable value or a default if not set.
func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// GetString retrieves a configuration value, checking env var first, then AWS SSM if enabled.
func (c *Config) GetString(ctx context.Context, envKey, awsKey string) (string, error) {
	if val := os.Getenv(envKey); val != "" {
		return val, nil
	}
	if c.AWSConfigEnabled && c.params != nil {
		return c.params.Get(ctx, c.GetKeyID(awsKey))
	}
	return "", fmt.Errorf("config %s not found (env: %s, aws: %s)", envKey, envKey, awsKey)
}

// GetStringRaw retrieves a configuration value from AWS SSM using the raw key (no environment prefix).
func (c *Config) GetStringRaw(ctx context.Context, envKey, awsKey string) (string, error) {
	if val := os.Getenv(envKey); val != "" {
		return val, nil
	}
	if c.AWSConfigEnabled && c.params != nil {
		return c.params.Get(ctx, awsKey)
	}
	return "", fmt.Errorf("config %s not found (env: %s, aws: %s)", envKey, envKey, awsKey)
}

// GetSecret retrieves a secret value, checking env var first, then AWS Secrets Manager if enabled.
func (c *Config) GetSecret(ctx context.Context, envKey, awsKey string) (string, error) {
	if val := os.Getenv(envKey); val != "" {
		return val, nil
	}
	if c.AWSConfigEnabled && c.secrets != nil {
		return c.secrets.Get(ctx, c.GetKeyID(awsKey))
	}
	return "", fmt.Errorf("secret %s not found (env: %s, aws: %s)", envKey, envKey, awsKey)
}

// GetSecretRaw retrieves a secret value from AWS Secrets Manager using the raw key (no environment prefix).
func (c *Config) GetSecretRaw(ctx context.Context, envKey, awsKey string) (string, error) {
	if val := os.Getenv(envKey); val != "" {
		return val, nil
	}
	if c.AWSConfigEnabled && c.secrets != nil {
		return c.secrets.Get(ctx, awsKey)
	}
	return "", fmt.Errorf("secret %s not found (env: %s, aws: %s)", envKey, envKey, awsKey)
}

// GetStringOptional retrieves an optional configuration value with a default fallback.
func (c *Config) GetStringOptional(ctx context.Context, envKey, awsKey, defaultVal string) string {
	if val := os.Getenv(envKey); val != "" {
		return val
	}
	if c.AWSConfigEnabled && c.params != nil {
		if val, err := c.params.Get(ctx, c.GetKeyID(awsKey)); err == nil && val != "" {
			return val
		}
	}
	return defaultVal
}

// GetStringOptionalRaw retrieves an optional configuration value using raw AWS key.
func (c *Config) GetStringOptionalRaw(ctx context.Context, envKey, awsKey, defaultVal string) string {
	if val := os.Getenv(envKey); val != "" {
		return val
	}
	if c.AWSConfigEnabled && c.params != nil {
		if val, err := c.params.Get(ctx, awsKey); err == nil && val != "" {
			return val
		}
	}
	return defaultVal
}

// GetSecretOptional retrieves an optional secret value with a default fallback.
func (c *Config) GetSecretOptional(ctx context.Context, envKey, awsKey, defaultVal string) string {
	if val := os.Getenv(envKey); val != "" {
		return val
	}
	if c.AWSConfigEnabled && c.secrets != nil {
		if val, err := c.secrets.Get(ctx, c.GetKeyID(awsKey)); err == nil && val != "" {
			return val
		}
	}
	return defaultVal
}

// GetSecretOptionalRaw retrieves an optional secret value using raw AWS key.
func (c *Config) GetSecretOptionalRaw(ctx context.Context, envKey, awsKey, defaultVal string) string {
	if val := os.Getenv(envKey); val != "" {
		return val
	}
	if c.AWSConfigEnabled && c.secrets != nil {
		if val, err := c.secrets.Get(ctx, awsKey); err == nil && val != "" {
			return val
		}
	}
	return defaultVal
}

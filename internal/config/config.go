package config

import (
	"os"

	"github.com/warmbly/warmbly/internal/infrastructure/secrets"
	"github.com/warmbly/warmbly/internal/infrastructure/ssm"
)

type Config struct {
	Env     string // "dev" or "prod"
	params  *ssm.SSMParameterStore
	secrets *secrets.SecretsManagerClient
}

func Load(params *ssm.SSMParameterStore, secrets *secrets.SecretsManagerClient) *Config {
	var env string = "dev"
	if os.Getenv("APP_ENV") == "prod" {
		env = "prod"
	}

	return &Config{
		Env:     env,
		params:  params,
		secrets: secrets,
	}
}

func (c *Config) GetKeyID(s string) string {
	return "/warmbly/" + c.Env + "/" + s
}

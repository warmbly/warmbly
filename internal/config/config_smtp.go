package config

import (
	"context"
	"os"
)

type SMTPConfig struct {
	Host     string
	Port     string
	Username string
	Password string
}

func (c *Config) LoadSMTPConfig(ctx context.Context) *SMTPConfig {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		return nil
	}

	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "1025"
	}

	return &SMTPConfig{
		Host:     host,
		Port:     port,
		Username: os.Getenv("SMTP_USERNAME"),
		Password: os.Getenv("SMTP_PASSWORD"),
	}
}

package config

import (
	"context"
	"os"

	"github.com/gin-gonic/gin"
)

type ApiConfig struct {
	Hostname string
	GinMode  string

	WebsocketURI string
}

func (c *Config) LoadApiConfig(ctx context.Context) (*ApiConfig, error) {
	// For API host, check env vars first with sensible defaults
	hostName := c.GetStringOptional(ctx, "API_HOST", "api/host", "0.0.0.0:8080")

	websocketUri, err := c.GetString(ctx, "WEBSOCKET_URL", "api/websocket_uri")
	if err != nil {
		return nil, err
	}

	// GIN_MODE from env, or derive from APP_ENV
	ginMode := os.Getenv("GIN_MODE")
	if ginMode == "" {
		if c.Env == "prod" {
			ginMode = gin.ReleaseMode
		} else {
			ginMode = gin.DebugMode
		}
	}

	return &ApiConfig{
		Hostname:     hostName,
		GinMode:      ginMode,
		WebsocketURI: websocketUri,
	}, nil
}

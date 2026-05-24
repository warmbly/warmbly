package config

import (
	"context"
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

type ApiConfig struct {
	Hostname string
	GinMode  string

	WebsocketURI   string
	AllowedOrigins []string
}

func (c *Config) LoadApiConfig(ctx context.Context) (*ApiConfig, error) {
	// For API host, check env vars first with sensible defaults
	hostName := c.GetStringOptional(ctx, "API_HOST", "api/host", "0.0.0.0:8080")

	websocketUri, err := c.GetString(ctx, "WEBSOCKET_URL", "api/websocket_uri")
	if err != nil {
		return nil, err
	}

	allowedOriginsRaw := os.Getenv("CORS_ALLOW_ORIGINS")
	if allowedOriginsRaw == "" {
		allowedOriginsRaw = os.Getenv("APP_URL")
	}
	allowedOrigins := splitCSV(allowedOriginsRaw)
	if len(allowedOrigins) == 0 {
		if origin := originFromURI(websocketUri); origin != "" {
			allowedOrigins = []string{origin}
		}
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
		Hostname:       hostName,
		GinMode:        ginMode,
		WebsocketURI:   websocketUri,
		AllowedOrigins: allowedOrigins,
	}, nil
}

func splitCSV(value string) []string {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func originFromURI(raw string) string {
	if raw == "" {
		return ""
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}

	scheme := u.Scheme
	switch scheme {
	case "ws":
		scheme = "http"
	case "wss":
		scheme = "https"
	}
	if scheme == "" {
		return ""
	}

	return scheme + "://" + u.Host
}

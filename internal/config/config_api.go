package config

import (
	"context"

	"github.com/gin-gonic/gin"
)

type ApiConfig struct {
	Hostname string
	GinMode  string

	WebsocketURI string
}

func (c *Config) LoadApiConfig(ctx context.Context) (*ApiConfig, error) {
	hostName, err := c.params.Get(ctx, c.GetKeyID("api/host"))
	if err != nil {
		return nil, err
	}

	websocketUri, err := c.params.Get(ctx, c.GetKeyID("api/websocket_uri"))
	if err != nil {
		return nil, err
	}

	var ginMode string = gin.DebugMode
	if c.Env == "prod" {
		ginMode = gin.ReleaseMode
	}

	return &ApiConfig{
		Hostname:     hostName,
		GinMode:      ginMode,
		WebsocketURI: websocketUri,
	}, nil
}

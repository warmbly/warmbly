package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// HealthDeps holds infrastructure dependencies for deep health checks
type HealthDeps struct {
	DB *pgxpool.Pool
}

type componentHealth struct {
	Status  string `json:"status"`
	Latency string `json:"latency,omitempty"`
	Error   string `json:"error,omitempty"`
}

func healthHandler(deps *HealthDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		overall := "ok"
		components := map[string]*componentHealth{}

		// Check PostgreSQL
		if deps.DB != nil {
			ch := checkComponent(func(ctx context.Context) error {
				return deps.DB.Ping(ctx)
			})
			components["postgres"] = ch
			if ch.Status != "ok" {
				overall = "degraded"
			}
		}


		status := http.StatusOK
		if overall != "ok" {
			status = http.StatusServiceUnavailable
		}

		c.JSON(status, gin.H{
			"status":     overall,
			"components": components,
			"timestamp":  time.Now().UTC(),
		})
	}
}

func checkComponent(check func(ctx context.Context) error) *componentHealth {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	start := time.Now()
	err := check(ctx)
	latency := time.Since(start)

	if err != nil {
		return &componentHealth{
			Status:  "error",
			Latency: latency.String(),
			Error:   err.Error(),
		}
	}
	return &componentHealth{
		Status:  "ok",
		Latency: latency.String(),
	}
}

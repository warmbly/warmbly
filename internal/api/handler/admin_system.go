package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/app/sysstatus"
)

// AdminSystemStatus runs the wired infrastructure probes (Postgres, Redis,
// Kafka, schema registry, realtime, ...) and returns per-component health for
// the admin System Status page. Probes run in parallel with a 3s cap each, so
// this is safe to poll.
func (h *Handler) AdminSystemStatus(c *gin.Context) {
	results := []sysstatus.Result{}
	if h.SystemChecker != nil {
		results = h.SystemChecker.Run(c.Request.Context())
	}
	c.JSON(http.StatusOK, gin.H{"data": results, "checked_at": time.Now()})
}

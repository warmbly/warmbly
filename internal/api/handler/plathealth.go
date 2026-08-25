package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/app/plathealth"
)

// Live is process-up. Orchestrators may restart on failure. A 200 here is
// not readiness and is not all-planes healthy.
func (h *Handler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "live", "live": true})
}

// Ready is dependency truth. 200 only when every required plane is observed
// and ok. 503 otherwise. Timeouts and unobserved planes fail closed.
func (h *Handler) Ready(c *gin.Context) {
	report := h.platformReport(c.Request.Context())
	code := http.StatusOK
	if !report.Ready {
		code = http.StatusServiceUnavailable
	}
	c.JSON(code, report)
}

// HealthDeps is the same matrix as Ready, for operators who want the
// dependency document at a stable path.
func (h *Handler) HealthDeps(c *gin.Context) {
	h.Ready(c)
}

func (h *Handler) platformReport(ctx context.Context) plathealth.Report {
	if h.PlatformHealth == nil {
		return plathealth.Evaluate(true, nil, time.Now().UTC(), plathealth.DefaultTimeout)
	}
	return h.PlatformHealth.Report(ctx)
}

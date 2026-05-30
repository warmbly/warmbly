package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (h *Handler) ServeWorkerInstaller(c *gin.Context) {
	if h.WorkerOrchestrator == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "worker installer is not configured"))
		return
	}
	script, err := h.WorkerOrchestrator.InstallerScript()
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to load worker installer"))
		return
	}
	c.Header("Content-Type", "text/x-shellscript; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, "text/x-shellscript; charset=utf-8", script)
}

type workerEnrollmentRequest struct {
	Token    string `json:"token"`
	PublicIP string `json:"public_ip,omitempty"`
}

// EnrollWorker exchanges a one-time enrollment token for a complete worker
// dotenv file. It is intentionally public: the high-entropy one-time token is
// the credential, and it is consumed atomically before secrets are returned.
func (h *Handler) EnrollWorker(c *gin.Context) {
	if h.WorkerRepo == nil || h.WorkerOrchestrator == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "worker enrollment is not configured"))
		return
	}

	var req workerEnrollmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	req.Token = strings.TrimSpace(req.Token)
	if req.Token == "" {
		errx.JSON(c, errx.New(errx.BadRequest, "enrollment token is required"))
		return
	}

	sum := sha256.Sum256([]byte(req.Token))
	worker, err := h.WorkerRepo.ConsumeEnrollmentToken(c.Request.Context(), hex.EncodeToString(sum[:]))
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to consume enrollment token"))
		return
	}
	if worker == nil {
		errx.JSON(c, errx.New(errx.Unauthorized, "enrollment token is invalid or expired"))
		return
	}

	ip := strings.TrimSpace(req.PublicIP)
	if ip == "" {
		ip = c.ClientIP()
	}
	if ip != "" {
		_ = h.WorkerRepo.RecordEnrolledIP(c.Request.Context(), worker.ID, ip)
	}

	envFile, _, err := h.WorkerOrchestrator.RenderEnrollmentEnv(c.Request.Context(), worker.ID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to render worker config"))
		return
	}
	envFile += "WORKER_TIER=" + workerTierLabel(worker) + "\n"
	if ip != "" {
		envFile += "WORKER_PUBLIC_IP=" + ip + "\n"
	}
	if worker.EgressKind != "" {
		envFile += "WORKER_EGRESS_KIND=" + string(worker.EgressKind) + "\n"
	}

	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.String(http.StatusOK, envFile)
}

func workerTierLabel(w *models.Worker) string {
	if w == nil {
		return "shared_premium"
	}
	if w.WorkerType == models.WorkerTypeDedicated {
		return "dedicated"
	}
	if w.FreeTier {
		return "shared_free"
	}
	return "shared_premium"
}

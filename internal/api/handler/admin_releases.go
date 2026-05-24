// Release-handling endpoints.
//
//   POST /webhooks/github/releases   — public, HMAC-validated. GitHub posts here
//                                      when a release event fires.
//   POST /admin/releases/check       — manual trigger from the dashboard.
//   GET  /admin/releases/state       — last known per-channel resolution.
//   PUT  /admin/worker-profiles/:id/release  — set release channel + auto-update.
//
// The webhook endpoint is intentionally not behind admin auth; security comes
// from the X-Hub-Signature-256 HMAC. If RELEASES_WEBHOOK_SECRET isn't set,
// the endpoint refuses all requests.

package handler

import (
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/app/releases"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (h *Handler) GithubReleasesWebhook(c *gin.Context) {
	if h.ReleasesService == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "releases service not configured"})
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body: " + err.Error()})
		return
	}
	signature := c.GetHeader("X-Hub-Signature-256")
	eventType := c.GetHeader("X-GitHub-Event")
	if err := h.ReleasesService.HandleWebhook(c.Request.Context(), body, signature, eventType); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) AdminCheckReleases(c *gin.Context) {
	if h.ReleasesService == nil {
		errx.JSON(c, errx.New(errx.NotFound, "releases service not configured"))
		return
	}
	changed, err := h.ReleasesService.CheckGitHub(c.Request.Context())
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, models.AuditActionCheckReleases, models.AuditEntityRelease, nil, map[string]string{
		"changed_profiles": fmt.Sprintf("%d", len(changed)),
	})
	c.JSON(http.StatusOK, gin.H{
		"state":   h.ReleasesService.GetState(),
		"changed": changed,
	})
}

func (h *Handler) AdminReleasesState(c *gin.Context) {
	if h.ReleasesService == nil {
		c.JSON(http.StatusOK, gin.H{"enabled": false})
		return
	}
	c.JSON(http.StatusOK, h.ReleasesService.GetState())
}

type setReleaseBody struct {
	Channel    string `json:"channel" binding:"required,oneof=pinned stable dev"`
	AutoUpdate bool   `json:"auto_update"`
}

func (h *Handler) AdminSetProfileRelease(c *gin.Context) {
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	var body setReleaseBody
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	if err := h.CredentialsRepo.UpdateProfileRelease(c.Request.Context(), id, models.ReleaseChannel(body.Channel), body.AutoUpdate); err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, models.AuditActionUpdate, models.AuditEntityWorkerProfile, &id, map[string]string{
		"release_channel": body.Channel,
		"auto_update":     fmt.Sprintf("%t", body.AutoUpdate),
	})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Compile-time check that the releases package is referenced (keeps the
// import stable across edits even if every method body changes).
var _ = (*releases.Service)(nil)

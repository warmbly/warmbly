package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

const adminInsightUnavailable = "this insight is not available on this instance"

// queryInt reads an integer query parameter, clamping it to [min, max].
func queryInt(c *gin.Context, key string, def, min, max int) int {
	v := def
	if raw := c.Query(key); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			v = n
		}
	}
	if v < min {
		v = min
	}
	if v > max {
		v = max
	}
	return v
}

// AdminGetAcquisition is GET /admin/analytics/acquisition?days=30.
func (h *Handler) AdminGetAcquisition(c *gin.Context) {
	if h.AdminInsightRepo == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, adminInsightUnavailable))
		return
	}
	days := queryInt(c, "days", 30, 1, 365)
	out, err := h.AdminInsightRepo.Acquisition(c.Request.Context(), days)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to load acquisition"))
		return
	}
	c.JSON(http.StatusOK, out)
}

// AdminWarmupActions is GET /admin/warmup/actions?limit=100.
func (h *Handler) AdminWarmupActions(c *gin.Context) {
	if h.AdminInsightRepo == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, adminInsightUnavailable))
		return
	}
	limit := queryInt(c, "limit", 100, 1, 500)
	rows, err := h.AdminInsightRepo.WarmupActions(c.Request.Context(), limit)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to load warmup actions"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// AdminListOrgAPIKeys is GET /admin/organizations/:id/api-keys.
func (h *Handler) AdminListOrgAPIKeys(c *gin.Context) {
	if h.AdminInsightRepo == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, adminInsightUnavailable))
		return
	}
	orgID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid organization ID"))
		return
	}
	rows, err := h.AdminInsightRepo.OrgAPIKeys(c.Request.Context(), orgID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to load API keys"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// AdminRevokeOrgAPIKey is DELETE /admin/organizations/:id/api-keys/:keyId.
func (h *Handler) AdminRevokeOrgAPIKey(c *gin.Context) {
	if h.APIKeyService == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "API keys are not available on this instance"))
		return
	}
	orgID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid organization ID"))
		return
	}
	keyID, err := uuid.Parse(c.Param("keyId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid API key ID"))
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
			return
		}
	}
	reason := body.Reason
	if reason == "" {
		reason = "revoked by platform admin"
	}
	if xerr := h.APIKeyService.Revoke(c.Request.Context(), orgID, keyID, reason); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.audit(c, models.AuditActionRevoke, models.AuditEntityAPIKey, &keyID, map[string]string{
		"organization_id": orgID.String(),
		"reason":          reason,
	})
	c.JSON(http.StatusOK, gin.H{"id": keyID, "organization_id": orgID, "status": models.APIKeyStatusRevoked})
}

// AdminListTransfers is GET /admin/transfers?limit=100.
func (h *Handler) AdminListTransfers(c *gin.Context) {
	if h.AdminInsightRepo == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, adminInsightUnavailable))
		return
	}
	limit := queryInt(c, "limit", 100, 1, 500)
	rows, err := h.AdminInsightRepo.ListTransfers(c.Request.Context(), limit)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to load transfers"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

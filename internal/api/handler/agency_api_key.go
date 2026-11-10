package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// agencyKeysCaller is agencyCaller for the key routes, which also need the key service.
func (h *Handler) agencyKeysCaller(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	if h.AgencyKeyService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "agency API keys are not available on this instance"))
		return uuid.Nil, uuid.Nil, false
	}
	return h.agencyCaller(c)
}

func agencyKeyParam(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("keyId"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return uuid.Nil, false
	}
	return id, true
}

// ListAgencyAPIKeys is GET /agency/:agencyId/api-keys.
func (h *Handler) ListAgencyAPIKeys(c *gin.Context) {
	userID, agencyID, ok := h.agencyKeysCaller(c)
	if !ok {
		return
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			errx.JSON(c, errx.New(errx.BadRequest, "limit must be between 1 and 100"))
			return
		}
		limit = n
	}
	res, xerr := h.AgencyKeyService.List(c.Request.Context(), agencyID, userID, limit, c.Query("cursor"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, res)
}

// ListAgencyAPIKeyPermissions is GET /agency/:agencyId/api-keys/permissions: the bits an agency key may hold.
func (h *Handler) ListAgencyAPIKeyPermissions(c *gin.Context) {
	workspace := make([]models.APIPermission, 0, len(models.AllAPIPermissions))
	for _, p := range models.AllAPIPermissions {
		if p.Value&models.AgencyKeyWorkspaceScopes != 0 {
			workspace = append(workspace, p)
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"workspace": workspace,
		"agency":    models.AllAgencyAPIPermissions,
		"presets": gin.H{
			"read_only":   models.APIPermReadOnly | models.APIPermAgencyRead | models.APIPermAgencyReadUsage,
			"full_access": models.AgencyKeyGrantable,
		},
		"grantable": models.AgencyKeyGrantable,
	})
}

// CreateAgencyAPIKey is POST /agency/:agencyId/api-keys. The secret is in this response only.
func (h *Handler) CreateAgencyAPIKey(c *gin.Context) {
	userID, agencyID, ok := h.agencyKeysCaller(c)
	if !ok {
		return
	}
	var req models.CreateAgencyAPIKey
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	k, xerr := h.AgencyKeyService.Create(c.Request.Context(), agencyID, userID, &req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditAgency(c, agencyID, models.AuditActionCreate, map[string]string{"action": "api_key_created", "agency_api_key_id": k.ID.String(), "name": k.Name})
	c.JSON(http.StatusCreated, k)
}

// UpdateAgencyAPIKey is PATCH /agency/:agencyId/api-keys/:keyId: rename or set the IP allowlist.
func (h *Handler) UpdateAgencyAPIKey(c *gin.Context) {
	userID, agencyID, ok := h.agencyKeysCaller(c)
	if !ok {
		return
	}
	keyID, ok := agencyKeyParam(c)
	if !ok {
		return
	}
	var req models.UpdateAgencyAPIKey
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	k, xerr := h.AgencyKeyService.Update(c.Request.Context(), agencyID, userID, keyID, &req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditAgency(c, agencyID, models.AuditActionUpdate, map[string]string{"action": "api_key_updated", "agency_api_key_id": k.ID.String()})
	c.JSON(http.StatusOK, k)
}

// RevokeAgencyAPIKey is DELETE /agency/:agencyId/api-keys/:keyId. Revoking twice succeeds, so retries are safe.
func (h *Handler) RevokeAgencyAPIKey(c *gin.Context) {
	userID, agencyID, ok := h.agencyKeysCaller(c)
	if !ok {
		return
	}
	keyID, ok := agencyKeyParam(c)
	if !ok {
		return
	}
	if xerr := h.AgencyKeyService.Revoke(c.Request.Context(), agencyID, userID, keyID); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditAgency(c, agencyID, models.AuditActionRevoke, map[string]string{"action": "api_key_revoked", "agency_api_key_id": keyID.String()})
	c.Status(http.StatusNoContent)
}

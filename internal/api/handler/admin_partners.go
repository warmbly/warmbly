package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/partner"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func (h *Handler) partnersAvailable(c *gin.Context) bool {
	if h.Partners == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "partners are not available on this instance"))
		return false
	}
	return true
}

func (h *Handler) adminPartnerParam(c *gin.Context) (uuid.UUID, *uuid.UUID, bool) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return uuid.Nil, nil, false
	}
	if !h.partnersAvailable(c) {
		return uuid.Nil, nil, false
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid partner id"))
		return uuid.Nil, nil, false
	}
	return id, adminID, true
}

func (h *Handler) logPartnerAction(c *gin.Context, adminID, partnerID uuid.UUID, action string, details map[string]any) {
	if h.AdminService != nil {
		h.AdminService.LogAdminAction(c.Request.Context(), adminID, action, "partner", &partnerID, details, c.ClientIP(), c.Request.UserAgent())
	}
}

// AdminListPartners is GET /admin/partners.
func (h *Handler) AdminListPartners(c *gin.Context) {
	if !h.partnersAvailable(c) {
		return
	}
	list, err := h.Partners.Repo.ListPartners(c.Request.Context())
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

type adminPartnerDetail struct {
	*models.Partner
	Keys []models.PartnerAPIKey `json:"keys"`
}

// AdminGetPartner is GET /admin/partners/:id: the partner and its keys.
func (h *Handler) AdminGetPartner(c *gin.Context) {
	id, _, ok := h.adminPartnerParam(c)
	if !ok {
		return
	}
	h.writePartnerDetail(c, id)
}

func (h *Handler) writePartnerDetail(c *gin.Context, id uuid.UUID) {
	p, err := h.Partners.Repo.GetPartner(c.Request.Context(), id)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	if p == nil {
		errx.JSON(c, errx.ErrNotFound)
		return
	}
	keys, err := h.Partners.Repo.ListPartnerKeys(c.Request.Context(), id)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	c.JSON(http.StatusOK, adminPartnerDetail{Partner: p, Keys: keys})
}

// AdminCreatePartner is POST /admin/partners.
func (h *Handler) AdminCreatePartner(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	if !h.partnersAvailable(c) {
		return
	}
	var req struct {
		Slug         string   `json:"slug"`
		Name         string   `json:"name"`
		DisplayName  string   `json:"display_name"`
		AllowedHosts []string `json:"allowed_hosts"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	p, xerr := h.Partners.CreatePartner(c.Request.Context(), partner.CreatePartnerInput{
		Slug: req.Slug, Name: req.Name, DisplayName: req.DisplayName, AllowedHosts: req.AllowedHosts,
	}, adminID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.logPartnerAction(c, *adminID, p.ID, "create_partner", map[string]any{"slug": p.Slug})
	h.writePartnerDetail(c, p.ID)
}

// AdminUpdatePartner is PATCH /admin/partners/:id.
func (h *Handler) AdminUpdatePartner(c *gin.Context) {
	id, adminID, ok := h.adminPartnerParam(c)
	if !ok {
		return
	}
	var req struct {
		Name                      *string               `json:"name"`
		DisplayName               *string               `json:"display_name"`
		Status                    *models.PartnerStatus `json:"status"`
		AllowedHosts              *[]string             `json:"allowed_hosts"`
		DelegationDomains         *[]string             `json:"delegation_domains"`
		WebhookURL                *string               `json:"webhook_url"`
		CallbackBaseURL           *string               `json:"callback_base_url"`
		WholesaleSendingWarmupDay *int64                `json:"wholesale_sending_warmup_day_micros"`
		WholesaleWarmupOnlyDay    *int64                `json:"wholesale_warmup_only_day_micros"`
		RateLimitPerSecond        *int                  `json:"rate_limit_per_second"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	_, xerr := h.Partners.UpdatePartner(c.Request.Context(), id, partner.UpdatePartnerInput{
		Name: req.Name, DisplayName: req.DisplayName, Status: req.Status, AllowedHosts: req.AllowedHosts, DelegationDomains: req.DelegationDomains,
		WebhookURL: req.WebhookURL, CallbackBaseURL: req.CallbackBaseURL,
		WholesaleSendingWarmupDay: req.WholesaleSendingWarmupDay, WholesaleWarmupOnlyDay: req.WholesaleWarmupOnlyDay,
		RateLimitPerSecond: req.RateLimitPerSecond,
	})
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	details := map[string]any{}
	if req.Status != nil {
		details["status"] = string(*req.Status)
	}
	if req.DelegationDomains != nil {
		details["delegation_domains"] = len(*req.DelegationDomains)
	}
	h.logPartnerAction(c, *adminID, id, "update_partner", details)
	h.writePartnerDetail(c, id)
}

// AdminRotatePartnerWebhookSecret is POST /admin/partners/:id/webhook-secret: the new secret, shown once.
func (h *Handler) AdminRotatePartnerWebhookSecret(c *gin.Context) {
	id, adminID, ok := h.adminPartnerParam(c)
	if !ok {
		return
	}
	secret, xerr := h.Partners.RotateWebhookSecret(c.Request.Context(), id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.logPartnerAction(c, *adminID, id, "rotate_partner_webhook_secret", nil)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"secret": secret})
}

// AdminSetPartnerCallbackKey is PUT /admin/partners/:id/callback-key.
func (h *Handler) AdminSetPartnerCallbackKey(c *gin.Context) {
	id, adminID, ok := h.adminPartnerParam(c)
	if !ok {
		return
	}
	var req struct {
		Key string `json:"key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	if xerr := h.Partners.SetCallbackKey(c.Request.Context(), id, req.Key); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.logPartnerAction(c, *adminID, id, "set_partner_callback_key", map[string]any{"cleared": req.Key == ""})
	h.writePartnerDetail(c, id)
}

// AdminCreatePartnerKey is POST /admin/partners/:id/keys: the key, shown once.
func (h *Handler) AdminCreatePartnerKey(c *gin.Context) {
	id, adminID, ok := h.adminPartnerParam(c)
	if !ok {
		return
	}
	var req struct {
		Mode models.PartnerMode `json:"mode"`
		Name string             `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	k, xerr := h.Partners.CreateKey(c.Request.Context(), id, req.Mode, req.Name, adminID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.logPartnerAction(c, *adminID, id, "create_partner_key", map[string]any{"key_id": k.ID.String(), "mode": string(k.Mode)})
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, k)
}

// AdminRevokePartnerKey is DELETE /admin/partners/:id/keys/:keyId.
func (h *Handler) AdminRevokePartnerKey(c *gin.Context) {
	id, adminID, ok := h.adminPartnerParam(c)
	if !ok {
		return
	}
	keyID, err := uuid.Parse(c.Param("keyId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid key id"))
		return
	}
	if xerr := h.Partners.RevokeKey(c.Request.Context(), id, keyID); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.logPartnerAction(c, *adminID, id, "revoke_partner_key", map[string]any{"key_id": keyID.String()})
	c.Status(http.StatusNoContent)
}

// AdminListPartnerOrganizations is GET /admin/partners/:id/organizations, both modes, newest first.
func (h *Handler) AdminListPartnerOrganizations(c *gin.Context) {
	id, _, ok := h.adminPartnerParam(c)
	if !ok {
		return
	}
	limit, xerr := partner.ParseLimit(c.Query("limit"), 100)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	var after *repository.PartnerOrgCursor
	if cur := c.Query("cursor"); cur != "" {
		var okCur bool
		if after, okCur = partner.DecodeCursor(cur); !okCur {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid cursor"))
			return
		}
	}
	rows, err := h.Partners.Repo.ListPartnerOrgs(c.Request.Context(), id, nil, after, limit+1)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	page := partner.Page[models.PartnerOrganization]{Data: rows}
	if len(rows) > limit {
		page.Data = rows[:limit]
		page.Pagination.HasMore = true
		next := partner.EncodeCursor(rows[limit-1].CreatedAt, rows[limit-1].ID)
		page.Pagination.NextCursor = &next
	}
	c.JSON(http.StatusOK, page)
}

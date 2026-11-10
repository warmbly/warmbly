package handler

import (
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/agency"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// adminAgencyCaller resolves the operator and the agency a write is about.
func (h *Handler) adminAgencyCaller(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return uuid.Nil, uuid.Nil, false
	}
	if h.AgencyService == nil {
		errx.JSON(c, errx.New(errx.NotFound, "agency not found"))
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid agency id"))
		return uuid.Nil, uuid.Nil, false
	}
	return *adminID, id, true
}

// adminAgencyDone records the operator's change on the platform-admin trail and answers with the agency as it now stands.
func (h *Handler) adminAgencyDone(c *gin.Context, adminID, agencyID uuid.UUID, action string, details map[string]any, d *agency.AdminAgencyDetail, xerr *errx.Error) {
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if h.AdminService != nil {
		h.AdminService.LogAdminAction(c.Request.Context(), adminID, action, "agency", &agencyID, details, c.ClientIP(), c.Request.UserAgent())
	}
	c.JSON(http.StatusOK, d)
}

// AdminGetOrganizationAgency is GET /admin/organizations/:id/agency: the agency a workspace is a client of, or null.
func (h *Handler) AdminGetOrganizationAgency(c *gin.Context) {
	orgID, ok := adminOrgParam(c)
	if !ok {
		return
	}
	var ref *models.AgencyRef
	if h.AgencyService != nil {
		ref = h.AgencyService.AgencyOf(c.Request.Context(), orgID)
	}
	c.JSON(http.StatusOK, gin.H{"agency": ref})
}

// AdminCreateAgency is POST /admin/agencies: an agency opened for an account by an operator.
func (h *Handler) AdminCreateAgency(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	if h.AgencyService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "agencies are not available on this instance"))
		return
	}
	var req struct {
		OwnerEmail string `json:"owner_email"`
		Name       string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	a, xerr := h.AgencyService.AdminCreate(c.Request.Context(), req.OwnerEmail, req.Name)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	d, xerr := h.AgencyService.AdminGet(c.Request.Context(), a.ID)
	h.adminAgencyDone(c, *adminID, a.ID, "create_agency", map[string]any{"owner_user_id": a.OwnerUserID.String()}, d, xerr)
}

// AdminGrantAgencyPlan is PUT /admin/agencies/:id/managed-plan: an agency plan held without Stripe.
func (h *Handler) AdminGrantAgencyPlan(c *gin.Context) {
	adminID, agencyID, ok := h.adminAgencyCaller(c)
	if !ok {
		return
	}
	var req adminManagedPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "a plan and a reason are required"))
		return
	}
	planID, err := uuid.Parse(strings.TrimSpace(req.PlanID))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "plan_id must be a plan uuid"))
		return
	}
	d, xerr := h.AgencyService.AdminGrantPlan(c.Request.Context(), agencyID, planID, adminID, req.Reason, req.Until)
	details := map[string]any{"plan_id": planID.String(), "reason": strings.TrimSpace(req.Reason)}
	if req.Until != nil {
		details["until"] = req.Until.UTC().Format(time.RFC3339)
	}
	h.adminAgencyDone(c, adminID, agencyID, "grant_agency_plan", details, d, xerr)
}

// AdminRevokeAgencyPlan is DELETE /admin/agencies/:id/managed-plan.
func (h *Handler) AdminRevokeAgencyPlan(c *gin.Context) {
	adminID, agencyID, ok := h.adminAgencyCaller(c)
	if !ok {
		return
	}
	d, xerr := h.AgencyService.AdminRevokePlan(c.Request.Context(), agencyID)
	h.adminAgencyDone(c, adminID, agencyID, "revoke_agency_plan", nil, d, xerr)
}

// AdminSetAgencyOverrides is PUT /admin/agencies/:id/overrides: the pool and AI allowance for one agency; null is its plan's own.
func (h *Handler) AdminSetAgencyOverrides(c *gin.Context) {
	adminID, agencyID, ok := h.adminAgencyCaller(c)
	if !ok {
		return
	}
	var req struct {
		MonthlySendPool *int     `json:"monthly_send_pool"`
		AIAllowanceUSD  *float64 `json:"ai_allowance_usd"`
		Reason          string   `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	var allowance *models.Micro
	details := map[string]any{"reason": strings.TrimSpace(req.Reason)}
	if req.AIAllowanceUSD != nil {
		m := models.MicroFromCents(int64(math.Round(*req.AIAllowanceUSD * 100)))
		allowance = &m
		details["ai_allowance_usd"] = m.USD()
	}
	if req.MonthlySendPool != nil {
		details["monthly_send_pool"] = *req.MonthlySendPool
	}
	d, xerr := h.AgencyService.AdminSetOverrides(c.Request.Context(), agencyID, adminID, req.MonthlySendPool, allowance, req.Reason)
	h.adminAgencyDone(c, adminID, agencyID, "set_agency_overrides", details, d, xerr)
}

// AdminCreditAgencyPool is POST /admin/agencies/:id/pool-credit: sends added to the current period.
func (h *Handler) AdminCreditAgencyPool(c *gin.Context) {
	adminID, agencyID, ok := h.adminAgencyCaller(c)
	if !ok {
		return
	}
	var req struct {
		Sends  int    `json:"sends"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	d, xerr := h.AgencyService.AdminCreditPool(c.Request.Context(), agencyID, adminID, req.Sends, req.Reason)
	h.adminAgencyDone(c, adminID, agencyID, "credit_agency_pool", map[string]any{"sends": req.Sends, "reason": strings.TrimSpace(req.Reason)}, d, xerr)
}

// AdminCreditAgencyAI is POST /admin/agencies/:id/ai-credit: money added to the shared AI balance.
func (h *Handler) AdminCreditAgencyAI(c *gin.Context) {
	adminID, agencyID, ok := h.adminAgencyCaller(c)
	if !ok {
		return
	}
	var req struct {
		AmountUSD float64 `json:"amount_usd"`
		Reason    string  `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	amount := models.MicroFromCents(int64(math.Round(req.AmountUSD * 100)))
	// A retried request carries its key, so the same credit is never added twice.
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		key = uuid.NewString()
	}
	d, xerr := h.AgencyService.AdminCreditAI(c.Request.Context(), agencyID, adminID, amount, req.Reason, agencyID.String()+":"+key)
	h.adminAgencyDone(c, adminID, agencyID, "credit_agency_ai", map[string]any{"amount_usd": amount.USD(), "reason": strings.TrimSpace(req.Reason)}, d, xerr)
}

// AdminAttachAgencyClient is POST /admin/agencies/:id/clients: a workspace brought under the agency.
func (h *Handler) AdminAttachAgencyClient(c *gin.Context) {
	adminID, agencyID, ok := h.adminAgencyCaller(c)
	if !ok {
		return
	}
	var req struct {
		OrganizationID string `json:"organization_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	orgID, err := uuid.Parse(strings.TrimSpace(req.OrganizationID))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "organization_id must be a workspace uuid"))
		return
	}
	d, xerr := h.AgencyService.AdminAttachClient(c.Request.Context(), agencyID, orgID)
	h.adminAgencyDone(c, adminID, agencyID, "attach_agency_client", map[string]any{"organization_id": orgID.String()}, d, xerr)
}

// AdminDetachAgencyClient is DELETE /admin/agencies/:id/clients/:orgId.
func (h *Handler) AdminDetachAgencyClient(c *gin.Context) {
	adminID, agencyID, ok := h.adminAgencyCaller(c)
	if !ok {
		return
	}
	orgID, err := uuid.Parse(c.Param("orgId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid organization id"))
		return
	}
	d, xerr := h.AgencyService.AdminDetachClient(c.Request.Context(), agencyID, orgID)
	h.adminAgencyDone(c, adminID, agencyID, "detach_agency_client", map[string]any{"organization_id": orgID.String()}, d, xerr)
}

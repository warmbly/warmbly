package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
)

// adminManagedPlanRequest is a grant. A reason is required for the same reason
// the risk override requires one: an unexplained paid workspace is the thing
// this feature exists to avoid creating.
type adminManagedPlanRequest struct {
	PlanID string     `json:"plan_id"`
	Reason string     `json:"reason"`
	Until  *time.Time `json:"until,omitempty"`
}

// AdminListPlans returns every plan, private ones included. The customer
// endpoint returns only public plans, and a grant is most often onto a private
// one (an internal or partner plan), so the picker needs the full list.
func (h *Handler) AdminListPlans(c *gin.Context) {
	if h.SubscriptionService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "plans are not available on this instance"))
		return
	}
	plans, xerr := h.SubscriptionService.ListPlans(c.Request.Context(), false)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"plans": plans})
}

// AdminGetOrgManagedPlan reports whether a workspace is paid because an
// operator said so, and why.
func (h *Handler) AdminGetOrgManagedPlan(c *gin.Context) {
	orgID, ok := adminOrgParam(c)
	if !ok {
		return
	}
	if h.OrganizationService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "organizations are not available on this instance"))
		return
	}
	managed, xerr := h.OrganizationService.GetManagedPlan(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, managed)
}

// AdminGrantOrgManagedPlan puts a workspace on a plan without Stripe.
func (h *Handler) AdminGrantOrgManagedPlan(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	orgID, ok := adminOrgParam(c)
	if !ok {
		return
	}
	if h.OrganizationService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "organizations are not available on this instance"))
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
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		errx.JSON(c, errx.New(errx.BadRequest, "a reason is required, so the grant is answerable later"))
		return
	}

	managed, xerr := h.OrganizationService.GrantManagedPlan(c.Request.Context(), orgID, planID, *adminID, reason, req.Until)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	details := map[string]any{"plan_id": planID.String(), "reason": reason}
	if req.Until != nil {
		details["until"] = req.Until.UTC().Format(time.RFC3339)
	}
	h.logOrgPlanAction(c, *adminID, orgID, "grant_managed_plan", details)
	c.JSON(http.StatusOK, managed)
}

// AdminRevokeOrgManagedPlan ends a grant. The workspace keeps the plan row it
// was on and stops counting as paid.
func (h *Handler) AdminRevokeOrgManagedPlan(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	orgID, ok := adminOrgParam(c)
	if !ok {
		return
	}
	if h.OrganizationService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "organizations are not available on this instance"))
		return
	}

	managed, xerr := h.OrganizationService.RevokeManagedPlan(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.logOrgPlanAction(c, *adminID, orgID, "revoke_managed_plan", nil)
	c.JSON(http.StatusOK, managed)
}

// logOrgPlanAction records the operator action on the platform-admin trail,
// which is who-did-what across tenants rather than within one.
func (h *Handler) logOrgPlanAction(c *gin.Context, adminID, orgID uuid.UUID, action string, details map[string]any) {
	if h.AdminService == nil {
		return
	}
	h.AdminService.LogAdminAction(c.Request.Context(), adminID, action, "organization", &orgID,
		details, c.ClientIP(), c.Request.UserAgent())
}

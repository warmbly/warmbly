package handler

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/agency"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// agencyCaller resolves the session user and the :agencyId parameter.
func (h *Handler) agencyCaller(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return uuid.Nil, uuid.Nil, false
	}
	if h.AgencyService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "agencies are not available on this instance"))
		return uuid.Nil, uuid.Nil, false
	}
	agencyID, err := uuid.Parse(c.Param("agencyId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid agency id"))
		return uuid.Nil, uuid.Nil, false
	}
	return userID, agencyID, true
}

// auditAgency records an agency change in each client workspace, so every
// staff member's dashboard refreshes through the audit spine.
func (h *Handler) auditAgency(c *gin.Context, agencyID uuid.UUID, action models.AuditAction, meta map[string]string) {
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		return
	}
	clients, xerr := h.AgencyService.Clients(c.Request.Context(), agencyID, userID)
	if xerr != nil || len(clients) == 0 {
		h.auditOrg(c, action, models.AuditEntityAgency, &agencyID, nil, meta)
		return
	}
	for _, cl := range clients {
		h.auditInOrg(c, cl.OrganizationID, action, models.AuditEntityAgency, &agencyID, nil, meta)
	}
}

// ListAgencies is GET /agency: the caller's agencies.
func (h *Handler) ListAgencies(c *gin.Context) {
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	if h.AgencyService == nil {
		c.JSON(http.StatusOK, gin.H{"data": []models.Agency{}, "billing_enabled": false})
		return
	}
	if agencyID := middleware.GetAgencyKeyAgencyID(c); agencyID != nil {
		a, xerr := h.AgencyService.Get(c.Request.Context(), *agencyID, userID)
		if xerr != nil {
			errx.JSON(c, xerr)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": []models.Agency{*a}, "billing_enabled": h.AgencyService.BillingEnabled()})
		return
	}
	list, xerr := h.AgencyService.ListForUser(c.Request.Context(), userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	// A portal session sees only the agency whose portal it signed in on.
	if bound := middleware.GetPortalAgencyID(c); bound != nil {
		own := list[:0]
		for _, a := range list {
			if a.ID == *bound {
				own = append(own, a)
			}
		}
		list = own
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "billing_enabled": h.AgencyService.BillingEnabled()})
}

// ListAgencyPlans is GET /agency/plans.
func (h *Handler) ListAgencyPlans(c *gin.Context) {
	if h.AgencyService == nil {
		c.JSON(http.StatusOK, gin.H{"data": []models.Plan{}, "billing_enabled": false})
		return
	}
	plans, xerr := h.AgencyService.Plans(c.Request.Context())
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": plans, "billing_enabled": h.AgencyService.BillingEnabled()})
}

type agencyNameRequest struct {
	Name string `json:"name"`
}

// CreateAgency is POST /agency.
func (h *Handler) CreateAgency(c *gin.Context) {
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	if h.AgencyService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "agencies are not available on this instance"))
		return
	}
	var req agencyNameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	a, xerr := h.AgencyService.Create(c.Request.Context(), userID, req.Name)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityAgency, &a.ID, nil, map[string]string{"name": a.Name})
	c.JSON(http.StatusCreated, a)
}

// GetAgency is GET /agency/:agencyId.
func (h *Handler) GetAgency(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	a, xerr := h.AgencyService.Get(c.Request.Context(), agencyID, userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"agency": a, "billing_enabled": h.AgencyService.BillingEnabled()})
}

// UpdateAgency is PATCH /agency/:agencyId.
func (h *Handler) UpdateAgency(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	var req agencyNameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	a, xerr := h.AgencyService.Rename(c.Request.Context(), agencyID, userID, req.Name)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditAgency(c, agencyID, models.AuditActionUpdate, map[string]string{"name": a.Name})
	c.JSON(http.StatusOK, a)
}

// ListAgencyStaff is GET /agency/:agencyId/members.
func (h *Handler) ListAgencyStaff(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	members, invites, xerr := h.AgencyService.Staff(c.Request.Context(), agencyID, userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": members, "invitations": invites})
}

type agencyStaffRequest struct {
	Email string            `json:"email"`
	Role  models.AgencyRole `json:"role"`
}

// AddAgencyStaff is POST /agency/:agencyId/members.
func (h *Handler) AddAgencyStaff(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	var req agencyStaffRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	member, invite, xerr := h.AgencyService.AddStaff(c.Request.Context(), agencyID, userID, req.Email, req.Role)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditAgency(c, agencyID, models.AuditActionInvite, map[string]string{"role": string(req.Role)})
	c.JSON(http.StatusCreated, gin.H{"member": member, "invitation": invite})
}

type agencyRoleRequest struct {
	Role models.AgencyRole `json:"role"`
}

// UpdateAgencyStaff is PATCH /agency/:agencyId/members/:userId.
func (h *Handler) UpdateAgencyStaff(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	target, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid user id"))
		return
	}
	var req agencyRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	m, xerr := h.AgencyService.UpdateStaffRole(c.Request.Context(), agencyID, userID, target, req.Role)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditAgency(c, agencyID, models.AuditActionUpdate, map[string]string{"member_id": target.String(), "role": string(req.Role)})
	c.JSON(http.StatusOK, m)
}

// RemoveAgencyStaff is DELETE /agency/:agencyId/members/:userId.
func (h *Handler) RemoveAgencyStaff(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	target, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid user id"))
		return
	}
	if xerr := h.AgencyService.RemoveStaff(c.Request.Context(), agencyID, userID, target); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditAgency(c, agencyID, models.AuditActionRemove, map[string]string{"member_id": target.String()})
	c.Status(http.StatusNoContent)
}

// CancelAgencyInvitation is DELETE /agency/:agencyId/invitations/:invitationId.
func (h *Handler) CancelAgencyInvitation(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	invID, err := uuid.Parse(c.Param("invitationId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid invitation id"))
		return
	}
	if xerr := h.AgencyService.CancelInvitation(c.Request.Context(), agencyID, userID, invID); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditAgency(c, agencyID, models.AuditActionRevoke, map[string]string{"invitation_id": invID.String()})
	c.Status(http.StatusNoContent)
}

// ListAgencyClients is GET /agency/:agencyId/clients.
func (h *Handler) ListAgencyClients(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	list, xerr := h.AgencyService.Clients(c.Request.Context(), agencyID, userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

type agencyClientCreateRequest struct {
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
}

// CreateAgencyClient is POST /agency/:agencyId/clients: a new workspace.
func (h *Handler) CreateAgencyClient(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	var req agencyClientCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	client, xerr := h.AgencyService.CreateClient(c.Request.Context(), agencyID, userID, req.Name, req.Timezone)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditInOrg(c, client.OrganizationID, models.AuditActionCreate, models.AuditEntityOrganization, &client.OrganizationID, nil, map[string]string{"name": client.Name, "agency_id": agencyID.String()})
	h.auditAgency(c, agencyID, models.AuditActionCreate, map[string]string{"organization_id": client.OrganizationID.String()})
	c.JSON(http.StatusCreated, client)
}

type agencyAttachRequest struct {
	OrganizationID uuid.UUID `json:"organization_id"`
}

// AttachAgencyClient is POST /agency/:agencyId/clients/attach: a workspace the caller owns.
func (h *Handler) AttachAgencyClient(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	var req agencyAttachRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.OrganizationID == uuid.Nil {
		errx.JSON(c, errx.New(errx.BadRequest, "organization_id is required"))
		return
	}
	client, xerr := h.AgencyService.AttachClient(c.Request.Context(), agencyID, userID, req.OrganizationID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditAgency(c, agencyID, models.AuditActionConnect, map[string]string{"organization_id": client.OrganizationID.String()})
	c.JSON(http.StatusCreated, client)
}

// agencyClientPatch applies only the fields present; a JSON null clears that cap.
type agencyClientPatch struct {
	MonthlySendLimit  json.RawMessage `json:"monthly_send_limit"`
	MonthlyAILimitUSD json.RawMessage `json:"monthly_ai_limit_usd"`
}

// nullable decodes a present field into *T, nil for a JSON null.
func nullable[T any](raw json.RawMessage) (*T, error) {
	if string(raw) == "null" {
		return nil, nil
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// UpdateAgencyClient is PATCH /agency/:agencyId/clients/:orgId: the client's monthly send cap and AI cap.
func (h *Handler) UpdateAgencyClient(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	orgID, err := uuid.Parse(c.Param("orgId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid organization id"))
		return
	}
	var req agencyClientPatch
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	if len(req.MonthlySendLimit) == 0 && len(req.MonthlyAILimitUSD) == 0 {
		errx.JSON(c, errx.New(errx.BadRequest, "send monthly_send_limit, monthly_ai_limit_usd or both"))
		return
	}
	setSend, setAI := len(req.MonthlySendLimit) > 0, len(req.MonthlyAILimitUSD) > 0
	var sendLimit *int
	if setSend {
		if sendLimit, err = nullable[int](req.MonthlySendLimit); err != nil {
			errx.JSON(c, errx.NewWithIdentifier(errx.BadRequest, "invalid_monthly_send_limit", "The monthly limit must be a whole number of sends."))
			return
		}
	}
	var aiLimit *models.Micro
	if setAI {
		dollars, err := nullable[float64](req.MonthlyAILimitUSD)
		if err != nil {
			errx.JSON(c, errx.NewWithIdentifier(errx.BadRequest, "invalid_monthly_ai_limit", "The monthly AI cap must be an amount in dollars."))
			return
		}
		if dollars != nil {
			m := models.MicroFromCents(int64(math.Round(*dollars * 100)))
			aiLimit = &m
		}
	}
	if xerr := h.AgencyService.CheckClientLimits(sendLimit, setSend, aiLimit, setAI); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	ctx := c.Request.Context()
	meta := map[string]string{"organization_id": orgID.String()}
	var client *models.AgencyClient
	var xerr *errx.Error
	if setSend {
		if client, xerr = h.AgencyService.SetClientLimit(ctx, agencyID, userID, orgID, sendLimit); xerr != nil {
			errx.JSON(c, xerr)
			return
		}
		meta["monthly_send_limit"] = "none"
		if sendLimit != nil {
			meta["monthly_send_limit"] = strconv.Itoa(*sendLimit)
		}
	}
	if setAI {
		if client, xerr = h.AgencyService.SetClientAILimit(ctx, agencyID, userID, orgID, aiLimit); xerr != nil {
			errx.JSON(c, xerr)
			return
		}
		meta["monthly_ai_limit_usd"] = "none"
		if aiLimit != nil {
			meta["monthly_ai_limit_usd"] = strconv.FormatFloat(aiLimit.USD(), 'f', 2, 64)
		}
	}
	h.auditAgency(c, agencyID, models.AuditActionUpdate, meta)
	c.JSON(http.StatusOK, client)
}

// DetachAgencyClient is DELETE /agency/:agencyId/clients/:orgId.
func (h *Handler) DetachAgencyClient(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	orgID, err := uuid.Parse(c.Param("orgId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid organization id"))
		return
	}
	if xerr := h.AgencyService.DetachClient(c.Request.Context(), agencyID, userID, orgID); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	meta := map[string]string{"organization_id": orgID.String()}
	h.auditInOrg(c, orgID, models.AuditActionDisconnect, models.AuditEntityAgency, &agencyID, nil, meta)
	h.auditAgency(c, agencyID, models.AuditActionDisconnect, meta)
	c.Status(http.StatusNoContent)
}

// GetAgencyUsage is GET /agency/:agencyId/usage.
func (h *Handler) GetAgencyUsage(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	u, xerr := h.AgencyService.Usage(c.Request.Context(), agencyID, userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, u)
}

type agencyCheckoutRequest struct {
	PlanID     uuid.UUID `json:"plan_id"`
	Interval   string    `json:"interval"`
	SuccessURL string    `json:"success_url"`
	CancelURL  string    `json:"cancel_url"`
}

const agencyBillingPath = "/app/agency/settings/billing"

// AgencyCheckout is POST /agency/:agencyId/billing/checkout.
func (h *Handler) AgencyCheckout(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	var req agencyCheckoutRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanID == uuid.Nil {
		errx.JSON(c, errx.New(errx.BadRequest, "plan_id is required"))
		return
	}
	if req.Interval != "" && req.Interval != "month" && req.Interval != "year" {
		errx.JSON(c, errx.New(errx.BadRequest, "interval must be month or year"))
		return
	}
	ctx := c.Request.Context()
	url, xerr := h.AgencyService.Checkout(ctx, agencyID, userID, req.PlanID, req.Interval == "year",
		billingReturnURL(ctx, req.SuccessURL, agencyBillingPath+"?checkout=done"), billingReturnURL(ctx, req.CancelURL, agencyBillingPath))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditAgency(c, agencyID, models.AuditActionCreate, map[string]string{"action": "checkout", "plan_id": req.PlanID.String()})
	c.JSON(http.StatusOK, gin.H{"checkout_url": url})
}

// agencyPlanChange reads the plan and billing period a switch names.
func agencyPlanChange(c *gin.Context, planID, interval string) (uuid.UUID, bool, bool) {
	id, err := uuid.Parse(strings.TrimSpace(planID))
	if err != nil || id == uuid.Nil {
		errx.JSON(c, errx.New(errx.BadRequest, "plan_id is required"))
		return uuid.Nil, false, false
	}
	if interval != "" && interval != "month" && interval != "year" {
		errx.JSON(c, errx.New(errx.BadRequest, "interval must be month or year"))
		return uuid.Nil, false, false
	}
	return id, interval == "year", true
}

// PreviewAgencyPlanChange is GET /agency/:agencyId/billing/change/preview: what switching plan or billing period would bill.
func (h *Handler) PreviewAgencyPlanChange(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	planID, yearly, ok := agencyPlanChange(c, c.Query("plan_id"), c.Query("interval"))
	if !ok {
		return
	}
	preview, xerr := h.AgencyService.PreviewPlanChange(c.Request.Context(), agencyID, userID, planID, yearly)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, preview)
}

// ChangeAgencyPlan is POST /agency/:agencyId/billing/change: move the agency's subscription to another plan or billing period.
func (h *Handler) ChangeAgencyPlan(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	var req struct {
		PlanID   string `json:"plan_id"`
		Interval string `json:"interval"`
		// ProrationDate and FromPriceID are the preview's, so the switch bills what was shown.
		ProrationDate int64  `json:"proration_date"`
		FromPriceID   string `json:"from_price_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	planID, yearly, ok := agencyPlanChange(c, req.PlanID, req.Interval)
	if !ok {
		return
	}
	if req.ProrationDate < 0 {
		errx.JSON(c, errx.New(errx.BadRequest, "proration_date must be the value a preview returned"))
		return
	}
	a, xerr := h.AgencyService.ChangePlan(c.Request.Context(), agencyID, userID, planID, yearly,
		agency.PlanChangeQuote{ProrationDate: req.ProrationDate, FromPriceID: strings.TrimSpace(req.FromPriceID)})
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	interval := "month"
	if yearly {
		interval = "year"
	}
	h.auditAgency(c, agencyID, models.AuditActionUpdate, map[string]string{"action": "plan_change", "plan_id": planID.String(), "interval": interval})
	c.JSON(http.StatusOK, a)
}

type agencyReturnRequest struct {
	SuccessURL string `json:"success_url"`
	CancelURL  string `json:"cancel_url"`
	ReturnURL  string `json:"return_url"`
}

// AgencyTopup is POST /agency/:agencyId/billing/topup.
func (h *Handler) AgencyTopup(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	var req agencyReturnRequest
	_ = c.ShouldBindJSON(&req)
	ctx := c.Request.Context()
	url, xerr := h.AgencyService.Topup(ctx, agencyID, userID,
		billingReturnURL(ctx, req.SuccessURL, agencyBillingPath+"?topup=done"), billingReturnURL(ctx, req.CancelURL, agencyBillingPath))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditAgency(c, agencyID, models.AuditActionCreate, map[string]string{"action": "topup_checkout"})
	c.JSON(http.StatusOK, gin.H{"checkout_url": url})
}

// AgencyPortal is POST /agency/:agencyId/billing/portal.
func (h *Handler) AgencyPortal(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	var req agencyReturnRequest
	_ = c.ShouldBindJSON(&req)
	ctx := c.Request.Context()
	url, xerr := h.AgencyService.Portal(ctx, agencyID, userID, billingReturnURL(ctx, req.ReturnURL, agencyBillingPath))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": url})
}

// GetAgencyAIBalance is GET /agency/:agencyId/ai-balance.
func (h *Handler) GetAgencyAIBalance(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	b, xerr := h.AgencyService.AIBalance(c.Request.Context(), agencyID, userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, b)
}

type agencyAmountRequest struct {
	AmountUSD  float64 `json:"amount_usd"`
	SuccessURL string  `json:"success_url"`
	CancelURL  string  `json:"cancel_url"`
}

// readAgencyAmount reads amount_usd in whole cents, as a card is charged.
func readAgencyAmount(c *gin.Context) (agencyAmountRequest, models.Micro, bool) {
	var req agencyAmountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return req, 0, false
	}
	return req, models.MicroFromCents(int64(math.Round(req.AmountUSD * 100))), true
}

// TransferAgencyAIBalance is POST /agency/:agencyId/clients/:orgId/ai-balance: move agency balance into the client's own.
func (h *Handler) TransferAgencyAIBalance(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	orgID, err := uuid.Parse(c.Param("orgId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid organization id"))
		return
	}
	_, amount, ok := readAgencyAmount(c)
	if !ok {
		return
	}
	key := strings.TrimSpace(c.GetHeader(middleware.IdempotencyKeyHeader))
	t, xerr := h.AgencyService.TransferAI(c.Request.Context(), agencyID, userID, orgID, amount, key)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if !t.Replayed {
		meta := map[string]string{"action": "ai_transfer", "organization_id": orgID.String(), "amount_usd": strconv.FormatFloat(t.Amount.USD(), 'f', 2, 64)}
		h.auditInOrg(c, orgID, models.AuditActionCreate, models.AuditEntityCreditPurchase, nil, nil, meta)
		h.auditAgency(c, agencyID, models.AuditActionUpdate, meta)
	}
	c.JSON(http.StatusOK, gin.H{
		"amount_usd": usd(t.Amount), "agency_balance_usd": usd(t.AgencyBalance), "workspace_balance_usd": usd(t.WorkspaceBalance),
	})
}

// AgencyAITopup is POST /agency/:agencyId/billing/ai-topup: Checkout for money added to the agency balance.
func (h *Handler) AgencyAITopup(c *gin.Context) {
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return
	}
	req, amount, ok := readAgencyAmount(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	url, xerr := h.AgencyService.AITopup(ctx, agencyID, userID, amount,
		billingReturnURL(ctx, req.SuccessURL, agencyBillingPath+"?ai_topup=done"), billingReturnURL(ctx, req.CancelURL, agencyBillingPath))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditAgency(c, agencyID, models.AuditActionCreate, map[string]string{"action": "ai_topup_checkout", "amount_usd": strconv.FormatFloat(amount.USD(), 'f', 2, 64)})
	c.JSON(http.StatusOK, gin.H{"checkout_url": url})
}

// AdminListAgencies is GET /admin/agencies.
func (h *Handler) AdminListAgencies(c *gin.Context) {
	if h.AgencyService == nil {
		c.JSON(http.StatusOK, gin.H{"data": []models.AdminAgency{}, "total": 0})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	list, total, xerr := h.AgencyService.AdminList(c.Request.Context(), c.Query("search"), limit, offset)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "total": total})
}

// AdminGetAgency is GET /admin/agencies/:id.
func (h *Handler) AdminGetAgency(c *gin.Context) {
	if h.AgencyService == nil {
		errx.JSON(c, errx.New(errx.NotFound, "agency not found"))
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid agency id"))
		return
	}
	d, xerr := h.AgencyService.AdminGet(c.Request.Context(), id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, d)
}

// LeaveAgency is POST /organization/agency/leave: the workspace's owner takes it out of its agency.
func (h *Handler) LeaveAgency(c *gin.Context) {
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	if h.AgencyService == nil {
		c.Status(http.StatusNoContent)
		return
	}
	ref, xerr := h.AgencyService.LeaveAgency(c.Request.Context(), *orgID, userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if ref != nil {
		h.auditOrg(c, models.AuditActionDisconnect, models.AuditEntityAgency, &ref.ID, nil,
			map[string]string{"organization_id": orgID.String(), "left": "true"})
	}
	c.Status(http.StatusNoContent)
}

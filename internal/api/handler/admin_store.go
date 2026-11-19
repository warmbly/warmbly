package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/store"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// The admin panel's Store section: instance-wide purchases, health, unit
// economics, and the operator's repairs. Every action is audited.

const adminStoreDefaultLimit = 50

// adminStoreReady answers 404 store_disabled when the instance sells nothing.
func (h *Handler) adminStoreReady(c *gin.Context) bool {
	if h.StoreService == nil || !h.StoreService.Enabled() {
		errx.JSON(c, errx.NewWithIdentifier(errx.NotFound, store.CodeDisabled, store.NotConfiguredMessage))
		return false
	}
	return true
}

func adminStoreLimit(c *gin.Context) (int, bool) {
	raw := c.Query("limit")
	if raw == "" {
		return adminStoreDefaultLimit, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 100 {
		errx.JSON(c, errx.NewWithIdentifier(errx.BadRequest, "invalid_limit", "limit must be between 1 and 100."))
		return 0, false
	}
	return n, true
}

// queryOrg reads an optional org_id filter.
func queryOrg(c *gin.Context) (*uuid.UUID, bool) {
	raw := strings.TrimSpace(c.Query("org_id"))
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid org_id"))
		return nil, false
	}
	return &id, true
}

func queryTrue(c *gin.Context, name string) bool {
	v, _ := strconv.ParseBool(c.Query(name))
	return v
}

// auditStore records an operator's store action against the row it touched.
func (h *Handler) auditStore(c *gin.Context, action string, entity string, id uuid.UUID, details map[string]any) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil || h.AdminService == nil {
		return
	}
	h.AdminService.LogAdminAction(c.Request.Context(), *adminID, action, entity, &id, details, c.ClientIP(), c.Request.UserAgent())
}

// AdminStoreHealth is GET /admin/store/health.
func (h *Handler) AdminStoreHealth(c *gin.Context) {
	if h.StoreService == nil {
		c.JSON(http.StatusOK, &store.Health{Mode: store.ModeOff, Alerts: []store.Alert{}})
		return
	}
	out, err := h.StoreService.AdminHealth(c.Request.Context())
	if err != nil {
		storeError(c, err)
		return
	}
	if out.Enabled && h.JobRuns != nil {
		if jobs, err := h.JobRuns.List(c.Request.Context()); err == nil {
			for _, j := range jobs {
				st := &store.LoopState{LastStartedAt: j.LastStartedAt, LastFinishedAt: j.LastFinishedAt, LastStatus: j.LastStatus, LastError: j.LastError}
				if j.LastFinishedAt != nil {
					st.LagSeconds = int64(time.Since(*j.LastFinishedAt).Seconds())
				}
				switch j.Name {
				case store.LoopReconcile:
					out.Reconciler = st
				case store.LoopBilling:
					out.Billing = st
				}
			}
		}
	}
	c.JSON(http.StatusOK, out)
}

// AdminStoreEconomics is GET /admin/store/economics.
func (h *Handler) AdminStoreEconomics(c *gin.Context) {
	if !h.adminStoreReady(c) {
		return
	}
	out, err := h.StoreService.AdminEconomics(c.Request.Context())
	if err != nil {
		storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// AdminListStoreOrders is GET /admin/store/orders.
func (h *Handler) AdminListStoreOrders(c *gin.Context) {
	if !h.adminStoreReady(c) {
		return
	}
	limit, ok := adminStoreLimit(c)
	if !ok {
		return
	}
	org, ok := queryOrg(c)
	if !ok {
		return
	}
	f := models.StoreAdminOrderFilter{OrgID: org, Status: c.Query("status"), FailureCode: c.Query("failure_code"),
		Stuck: queryTrue(c, "stuck"), Query: c.Query("q")}
	page, err := h.StoreService.AdminListOrders(c.Request.Context(), f, c.Query("cursor"), limit)
	if err != nil {
		storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// AdminGetStoreOrder is GET /admin/store/orders/:id.
func (h *Handler) AdminGetStoreOrder(c *gin.Context) {
	if !h.adminStoreReady(c) {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	o, err := h.StoreService.AdminGetOrder(c.Request.Context(), id)
	if err != nil {
		storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, o)
}

// AdminListStoreDomains is GET /admin/store/domains.
func (h *Handler) AdminListStoreDomains(c *gin.Context) {
	if !h.adminStoreReady(c) {
		return
	}
	limit, ok := adminStoreLimit(c)
	if !ok {
		return
	}
	org, ok := queryOrg(c)
	if !ok {
		return
	}
	f := models.StoreAdminDomainFilter{OrgID: org, Status: c.Query("status"), RenewalFailed: queryTrue(c, "renewal_failed"), Query: c.Query("q")}
	if raw := c.Query("auto_renew"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "auto_renew must be true or false"))
			return
		}
		f.AutoRenew = &v
	}
	if raw := c.Query("expiring_days"); raw != "" {
		days, err := strconv.Atoi(raw)
		if err != nil || days < 0 || days > 3650 {
			errx.JSON(c, errx.New(errx.BadRequest, "expiring_days must be between 0 and 3650"))
			return
		}
		before := time.Now().Add(time.Duration(days) * 24 * time.Hour)
		f.ExpiresBefore = &before
	}
	page, err := h.StoreService.AdminListDomains(c.Request.Context(), f, c.Query("cursor"), limit)
	if err != nil {
		storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// AdminListStoreMailboxes is GET /admin/store/mailboxes.
func (h *Handler) AdminListStoreMailboxes(c *gin.Context) {
	if !h.adminStoreReady(c) {
		return
	}
	limit, ok := adminStoreLimit(c)
	if !ok {
		return
	}
	org, ok := queryOrg(c)
	if !ok {
		return
	}
	f := models.StoreAdminMailboxFilter{OrgID: org, Status: c.Query("status"), ConnectState: c.Query("connect_state"),
		BillingFailed: queryTrue(c, "billing_failed"), Query: c.Query("q")}
	page, err := h.StoreService.AdminListMailboxes(c.Request.Context(), f, c.Query("cursor"), limit)
	if err != nil {
		storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// AdminGetStoreWorkspace is GET /admin/store/workspaces/:orgId.
func (h *Handler) AdminGetStoreWorkspace(c *gin.Context) {
	if !h.adminStoreReady(c) {
		return
	}
	orgID, err := uuid.Parse(c.Param("orgId"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	out, err := h.StoreService.AdminWorkspace(c.Request.Context(), orgID)
	if err != nil {
		storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// adminStoreAction runs one operator action on the row the path names and audits it.
func adminStoreAction[T any](h *Handler, c *gin.Context, action, entity string, run func(id uuid.UUID) (T, error), details func(T) map[string]any) {
	if !h.adminStoreReady(c) {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	out, err := run(id)
	if err != nil {
		storeError(c, err)
		return
	}
	var d map[string]any
	if details != nil {
		d = details(out)
	}
	h.auditStore(c, action, entity, id, d)
	c.JSON(http.StatusOK, out)
}

func orderDetails(o *store.AdminOrder) map[string]any {
	return map[string]any{"organization_id": o.OrganizationID.String(), "status": o.Status}
}

func mailboxDetails(m *store.AdminMailbox) map[string]any {
	return map[string]any{"organization_id": m.OrganizationID.String(), "email": m.Email, "status": m.Status, "connect_state": m.ConnectState}
}

func domainDetails(d *store.AdminDomain) map[string]any {
	return map[string]any{"organization_id": d.OrganizationID.String(), "domain": d.Name, "status": d.Status, "auto_renew": d.AutoRenew}
}

// AdminRefreshStoreOrder is POST /admin/store/orders/:id/refresh.
func (h *Handler) AdminRefreshStoreOrder(c *gin.Context) {
	adminStoreAction(h, c, "store_order_refresh", "store_order", func(id uuid.UUID) (*store.AdminOrder, error) {
		return h.StoreService.AdminRefreshOrder(c.Request.Context(), id)
	}, orderDetails)
}

// AdminPlaceStoreOrder is POST /admin/store/orders/:id/place.
func (h *Handler) AdminPlaceStoreOrder(c *gin.Context) {
	adminStoreAction(h, c, "store_order_place", "store_order", func(id uuid.UUID) (*store.AdminOrder, error) {
		return h.StoreService.AdminPlaceOrder(c.Request.Context(), id)
	}, orderDetails)
}

// AdminRefundStoreOrder is POST /admin/store/orders/:id/refund: a quote line, or an amount under an Idempotency-Key.
func (h *Handler) AdminRefundStoreOrder(c *gin.Context) {
	var in store.RefundRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	in.Key = strings.TrimSpace(c.GetHeader(middleware.IdempotencyKeyHeader))
	adminStoreAction(h, c, "store_order_refund", "store_order", func(id uuid.UUID) (*store.RefundResult, error) {
		return h.StoreService.AdminRefund(c.Request.Context(), id, in)
	}, func(r *store.RefundResult) map[string]any {
		d := orderDetails(r.Order)
		d["refunded_micros"] = r.RefundedMicros
		if in.Line != nil {
			d["line"] = *in.Line
		} else {
			d["amount_micros"] = in.AmountMicros
			d["reason"] = in.Reason
		}
		return d
	})
}

// AdminRefreshStoreMailbox is POST /admin/store/mailboxes/:id/refresh.
func (h *Handler) AdminRefreshStoreMailbox(c *gin.Context) {
	adminStoreAction(h, c, "store_mailbox_refresh", "store_mailbox", func(id uuid.UUID) (*store.AdminMailbox, error) {
		return h.StoreService.AdminRefreshMailbox(c.Request.Context(), id)
	}, mailboxDetails)
}

// AdminConnectStoreMailbox is POST /admin/store/mailboxes/:id/connect.
func (h *Handler) AdminConnectStoreMailbox(c *gin.Context) {
	adminStoreAction(h, c, "store_mailbox_connect", "store_mailbox", func(id uuid.UUID) (*store.AdminMailbox, error) {
		return h.StoreService.AdminRetryConnect(c.Request.Context(), id)
	}, mailboxDetails)
}

// AdminCancelStoreMailbox is POST /admin/store/mailboxes/:id/cancel.
func (h *Handler) AdminCancelStoreMailbox(c *gin.Context) {
	adminStoreAction(h, c, "store_mailbox_cancel", "store_mailbox", func(id uuid.UUID) (*store.AdminMailbox, error) {
		return h.StoreService.AdminCancelMailbox(c.Request.Context(), id)
	}, mailboxDetails)
}

// AdminResumeStoreMailbox is POST /admin/store/mailboxes/:id/resume.
func (h *Handler) AdminResumeStoreMailbox(c *gin.Context) {
	adminStoreAction(h, c, "store_mailbox_resume", "store_mailbox", func(id uuid.UUID) (*store.AdminMailbox, error) {
		return h.StoreService.AdminResumeMailbox(c.Request.Context(), id)
	}, mailboxDetails)
}

// AdminRefreshStoreDomain is POST /admin/store/domains/:id/refresh.
func (h *Handler) AdminRefreshStoreDomain(c *gin.Context) {
	adminStoreAction(h, c, "store_domain_refresh", "store_domain", func(id uuid.UUID) (*store.AdminDomain, error) {
		return h.StoreService.AdminRefreshDomain(c.Request.Context(), id)
	}, domainDetails)
}

// AdminRenewStoreDomain is POST /admin/store/domains/:id/renew.
func (h *Handler) AdminRenewStoreDomain(c *gin.Context) {
	adminStoreAction(h, c, "store_domain_renew", "store_domain", func(id uuid.UUID) (*store.AdminDomain, error) {
		return h.StoreService.AdminRenewDomain(c.Request.Context(), id)
	}, domainDetails)
}

// AdminPatchStoreDomain is PATCH /admin/store/domains/:id: auto-renew.
func (h *Handler) AdminPatchStoreDomain(c *gin.Context) {
	var in struct {
		AutoRenew *bool `json:"auto_renew"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.AutoRenew == nil {
		errx.JSON(c, errx.NewWithIdentifier(errx.BadRequest, "invalid_body", "Send auto_renew as true or false."))
		return
	}
	adminStoreAction(h, c, "store_domain_auto_renew", "store_domain", func(id uuid.UUID) (*store.AdminDomain, error) {
		return h.StoreService.AdminSetAutoRenew(c.Request.Context(), id, *in.AutoRenew)
	}, domainDetails)
}

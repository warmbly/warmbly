package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/store"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// storeCaller is the caller's workspace and user, or false once it has answered.
func (h *Handler) storeCaller(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return uuid.Nil, uuid.Nil, false
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return uuid.Nil, uuid.Nil, false
	}
	return *orgID, userID, true
}

// storeError answers a store failure in Warmbly's error shape.
func storeError(c *gin.Context, err error) {
	rid := c.GetString("request_id")
	var (
		price  *store.PriceChangedError
		method *store.PaymentMethodError
		card   *store.CardError
		bad    *store.ContactError
	)
	var xerr *errx.Error
	if errors.As(err, &xerr) && xerr.Identifier == store.ErrIDContactInvalid {
		err = store.NewContactError(strings.Split(xerr.Reason, ","))
	}
	switch {
	case errors.As(err, &price):
		c.JSON(http.StatusConflict, gin.H{
			"error": "Conflict", "code": store.CodePriceChanged, "request_id": rid,
			"message": "The price changed since you looked. Review the new total.",
			"details": gin.H{"quote": price.Quote},
		})
	case errors.As(err, &method):
		c.JSON(http.StatusPaymentRequired, gin.H{
			"error": "Payment Required", "code": store.CodePaymentMethodRequired, "request_id": rid,
			"message":   "Add a card to pay for this order.",
			"setup_url": c.GetString("store_setup_url"),
		})
	case errors.As(err, &card):
		c.JSON(http.StatusPaymentRequired, gin.H{
			"error": "Payment Required", "code": card.Code, "request_id": rid,
			"message": "The card was not charged. Try another card or add money to your balance.",
		})
	case errors.As(err, &bad):
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Bad Request", "code": store.ErrIDContactInvalid, "request_id": rid,
			"message": bad.Message(),
			"details": gin.H{"fields": bad.Fields},
		})
	default:
		if !errors.As(err, &xerr) {
			log.Error().Err(err).Str("request_id", rid).Msg("store: request failed")
		}
		errx.Handle(c, err)
	}
}

func storeLimit(c *gin.Context) (int, bool) {
	raw := c.Query("limit")
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 100 {
		errx.JSON(c, errx.NewWithIdentifier(errx.BadRequest, "invalid_limit", "limit must be between 1 and 100."))
		return 0, false
	}
	return n, true
}

func storeID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return uuid.Nil, false
	}
	return id, true
}

// GetStoreStatus is GET /v1/store/status.
func (h *Handler) GetStoreStatus(c *gin.Context) {
	orgID, userID, ok := h.storeCaller(c)
	if !ok {
		return
	}
	st, err := h.StoreService.Status(c.Request.Context(), orgID)
	if err != nil {
		storeError(c, err)
		return
	}
	if st.Metered && h.StripeService != nil {
		st.StripePublishableKey = h.StripeService.PublishableKey()
	}
	if st.Enabled {
		st.DefaultForwardingURL = h.StoreService.DefaultForward(c.Request.Context(), orgID, userID)
		st.Photos = h.StoreService.PhotosAvailable()
	}
	if st.Locked && h.canConfigureStore(c, userID) {
		st.CanConfigure = true
		if admin := config.AdminURL(); admin != "" {
			st.ConfigureURL = admin + storeConfigurePath
		}
	}
	c.JSON(http.StatusOK, st)
}

// storeConfigurePath is the admin panel page that takes the GraphKit API key.
const storeConfigurePath = "/health?tab=integrations"

// canConfigureStore reports a signed-in member who is also an instance admin holding manage_settings; never an API key.
func (h *Handler) canConfigureStore(c *gin.Context, userID uuid.UUID) bool {
	if middleware.GetAPIKeyID(c) != nil || h.OrganizationService == nil {
		return false
	}
	perms, err := h.OrganizationService.GetUserAdminPermissions(c.Request.Context(), userID)
	return err == nil && models.AdminPermission(perms).HasPermission(models.AdminPermManageSettings)
}

// SearchStoreDomains is GET /v1/store/domains/search?q=&tlds=&suggestions=; suggestions=false answers with the exact matches only.
func (h *Handler) SearchStoreDomains(c *gin.Context) {
	if _, _, ok := h.storeCaller(c); !ok {
		return
	}
	res, err := h.StoreService.Search(c.Request.Context(), c.Query("q"), c.QueryArray("tlds"), c.Query("suggestions") == "false")
	if err != nil {
		storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// PutStoreRegistrant is PUT /v1/store/registrant.
func (h *Handler) PutStoreRegistrant(c *gin.Context) {
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	var in store.Registrant
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	r, err := h.StoreService.PutRegistrant(c.Request.Context(), orgID, in)
	if err != nil {
		storeError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityStoreDomain, nil, nil, map[string]string{"field": "registrant"})
	c.JSON(http.StatusOK, r)
}

// CreateStoreQuote is POST /v1/store/quotes.
func (h *Handler) CreateStoreQuote(c *gin.Context) {
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	var in store.QuoteRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	q, err := h.StoreService.CreateQuote(c.Request.Context(), orgID, in)
	if err != nil {
		storeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, q)
}

// CreateStoreOrder is POST /v1/store/orders: charge the workspace and order.
func (h *Handler) CreateStoreOrder(c *gin.Context) {
	orgID, userID, ok := h.storeCaller(c)
	if !ok {
		return
	}
	var in store.OrderRequest
	if err := c.ShouldBindJSON(&in); err != nil || in.QuoteID == uuid.Nil {
		errx.JSON(c, errx.NewWithIdentifier(errx.BadRequest, "invalid_body", "Send the quote to order."))
		return
	}
	o, err := h.StoreService.PlaceOrder(c.Request.Context(), orgID, userID, in)
	if err != nil {
		var method *store.PaymentMethodError
		if errors.As(err, &method) && h.StripeService != nil {
			// The same card setup the billing page uses, returning to the checkout.
			if sess, xerr := h.StripeService.CreateCardSetupCheckout(c.Request.Context(), userID, orgID,
				billingReturnURL(c.Request.Context(), "", "/app/emails/buy?payment_method=success"),
				billingReturnURL(c.Request.Context(), "", "/app/emails/buy?payment_method=canceled")); xerr == nil && sess != nil {
				c.Set("store_setup_url", sess.URL)
			}
		}
		storeError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityStoreOrder, &o.ID, nil, map[string]string{
		"due_now_usd": strconv.FormatFloat(float64(o.DueNowMicros)/1_000_000, 'f', 2, 64),
	})
	c.JSON(http.StatusCreated, o)
}

// ListStoreOrders is GET /v1/store/orders.
func (h *Handler) ListStoreOrders(c *gin.Context) {
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	limit, ok := storeLimit(c)
	if !ok {
		return
	}
	page, err := h.StoreService.ListOrders(c.Request.Context(), orgID, c.Query("cursor"), limit)
	if err != nil {
		storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// GetStoreOrder is GET /v1/store/orders/:id.
func (h *Handler) GetStoreOrder(c *gin.Context) {
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	o, err := h.StoreService.GetOrder(c.Request.Context(), orgID, id)
	if err != nil {
		storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, o)
}

// ListStoreDomains is GET /v1/store/domains.
func (h *Handler) ListStoreDomains(c *gin.Context) {
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	limit, ok := storeLimit(c)
	if !ok {
		return
	}
	page, err := h.StoreService.ListDomains(c.Request.Context(), orgID, c.Query("cursor"), limit)
	if err != nil {
		storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// GetStoreDomain is GET /v1/store/domains/:id, with its DNS records.
func (h *Handler) GetStoreDomain(c *gin.Context) {
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	d, err := h.StoreService.GetDomain(c.Request.Context(), orgID, id)
	if err != nil {
		storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

// credentialCaller is an API key or OAuth token: it presents a scoped credential on every call and has no session to refresh.
func credentialCaller(c *gin.Context) bool {
	t := c.GetString(middleware.AuthTypeKey)
	return t == middleware.AuthTypeAPIKey || t == middleware.AuthTypeOAuth
}

// PatchStoreDomain is PATCH /v1/store/domains/:id. Turning auto-renew on or
// off changes what is charged, so it needs manage_billing and, for a session, a fresh sign-in.
func (h *Handler) PatchStoreDomain(c *gin.Context) {
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	var in store.DomainPatch
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	if in.AutoRenew != nil {
		perms, _ := middleware.GetMemberPermissions(c)
		if !perms.HasPermission(models.PermManageBilling) {
			errx.JSON(c, errx.ErrForbidden)
			return
		}
		if !credentialCaller(c) && !middleware.SessionFresh(c) {
			errx.JSON(c, errx.NewWithIdentifier(errx.Forbidden, "reauth_required", "Confirm it is you before making this change."))
			return
		}
	}
	d, err := h.StoreService.PatchDomain(c.Request.Context(), orgID, id, in)
	if err != nil {
		storeError(c, err)
		return
	}
	changes := map[string]string{}
	if in.AutoRenew != nil {
		changes["auto_renew"] = strconv.FormatBool(*in.AutoRenew)
	}
	if in.ForwardingURL != nil {
		changes["forwarding_url"] = d.ForwardingURL
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityStoreDomain, &d.ID, changes, map[string]string{"domain": d.Name})
	c.JSON(http.StatusOK, d)
}

// RenewStoreDomain is POST /v1/store/domains/:id/renew.
func (h *Handler) RenewStoreDomain(c *gin.Context) {
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	d, err := h.StoreService.RenewNow(c.Request.Context(), orgID, id)
	if err != nil {
		storeError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityStoreDomain, &d.ID, nil, map[string]string{"domain": d.Name, "action": "renew"})
	c.JSON(http.StatusOK, d)
}

// TransferOutStoreDomain is POST /v1/store/domains/:id/transfer-out.
func (h *Handler) TransferOutStoreDomain(c *gin.Context) {
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	code, err := h.StoreService.TransferOut(c.Request.Context(), orgID, id)
	if err != nil {
		storeError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionTransfer, models.AuditEntityStoreDomain, &id, nil, nil)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"auth_code": code})
}

// ListStoreMailboxes is GET /v1/store/mailboxes.
func (h *Handler) ListStoreMailboxes(c *gin.Context) {
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	limit, ok := storeLimit(c)
	if !ok {
		return
	}
	page, err := h.StoreService.ListMailboxes(c.Request.Context(), orgID, c.Query("cursor"), limit)
	if err != nil {
		storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// PatchStoreMailbox is PATCH /v1/store/mailboxes/:id: the sender name.
func (h *Handler) PatchStoreMailbox(c *gin.Context) {
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	var in store.MailboxPatch
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	m, err := h.StoreService.RenameMailbox(c.Request.Context(), orgID, id, in)
	if err != nil {
		storeError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityStoreMailbox, &m.ID, map[string]string{"first_name": m.FirstName, "last_name": m.LastName}, map[string]string{"email": m.Email})
	c.JSON(http.StatusOK, m)
}

// storePhotoLimits are a mailbox photo's: GraphKit takes PNG or JPG up to 2 MB.
var storePhotoLimits = imageLimits{label: "photo", formats: "PNG or JPG", maxBytes: 2 << 20, maxDim: 2048}

// UploadStorePhoto is POST /v1/store/photos: a multipart "file", or {"from": "profile"}
// to copy the caller's own profile photo. Answers the photo's id for an order line or a mailbox.
func (h *Handler) UploadStorePhoto(c *gin.Context) {
	orgID, userID, ok := h.storeCaller(c)
	if !ok {
		return
	}
	var (
		p   *store.Photo
		err error
	)
	if strings.HasPrefix(c.ContentType(), "application/json") {
		var in struct {
			From string `json:"from"`
		}
		if berr := c.ShouldBindJSON(&in); berr != nil || in.From != "profile" {
			errx.JSON(c, errx.NewWithIdentifier(errx.BadRequest, "invalid_body", `Send a photo as the multipart field "file", or {"from": "profile"}.`))
			return
		}
		p, err = h.StoreService.ProfilePhoto(c.Request.Context(), orgID, userID)
	} else {
		body, _, _, xerr := readImageUpload(c, storePhotoLimits)
		if xerr != nil {
			errx.JSON(c, xerr)
			return
		}
		p, err = h.StoreService.UploadPhoto(c.Request.Context(), orgID, userID, body)
	}
	if err != nil {
		storeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, p)
}

// GetStorePhoto is GET /v1/store/photos/:id: one of the workspace's uploaded photos, as a JPEG.
func (h *Handler) GetStorePhoto(c *gin.Context) {
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	data, err := h.StoreService.PhotoBytes(c.Request.Context(), orgID, id)
	if err != nil {
		storeError(c, err)
		return
	}
	c.Header("Cache-Control", "private, max-age=86400")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "image/jpeg", data)
}

// SetStoreMailboxPhoto is PUT /v1/store/mailboxes/:id/photo: the mailbox's profile photo at Google or Microsoft.
func (h *Handler) SetStoreMailboxPhoto(c *gin.Context) {
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	var in store.MailboxPhotoRequest
	if err := c.ShouldBindJSON(&in); err != nil || in.PhotoID == uuid.Nil {
		errx.JSON(c, errx.NewWithIdentifier(errx.BadRequest, "invalid_body", "Send the uploaded photo's photo_id."))
		return
	}
	m, err := h.StoreService.SetMailboxPhoto(c.Request.Context(), orgID, id, in.PhotoID)
	if err != nil {
		storeError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityStoreMailbox, &m.ID, nil, map[string]string{"email": m.Email, "field": "photo"})
	c.JSON(http.StatusOK, m)
}

// CancelStoreMailbox is DELETE /v1/store/mailboxes/:id: cancel at the end of the paid month.
func (h *Handler) CancelStoreMailbox(c *gin.Context) {
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	m, err := h.StoreService.CancelMailbox(c.Request.Context(), orgID, id)
	if err != nil {
		storeError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityStoreMailbox, &m.ID, nil, map[string]string{"email": m.Email})
	c.JSON(http.StatusOK, m)
}

// ResumeStoreMailbox is POST /v1/store/mailboxes/:id/resume.
func (h *Handler) ResumeStoreMailbox(c *gin.Context) {
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	m, err := h.StoreService.ResumeMailbox(c.Request.Context(), orgID, id)
	if err != nil {
		storeError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionResume, models.AuditEntityStoreMailbox, &m.ID, nil, map[string]string{"email": m.Email})
	c.JSON(http.StatusOK, m)
}

func storeRecordID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("record_id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return uuid.Nil, false
	}
	return id, true
}

// storeDomainCall runs a call on one of the workspace's store domains.
func (h *Handler) storeDomainCall(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	id, ok := storeID(c)
	return orgID, id, ok
}

// ConnectStoreDomain is POST /v1/store/domains/connect: add a domain registered elsewhere.
func (h *Handler) ConnectStoreDomain(c *gin.Context) {
	orgID, _, ok := h.storeCaller(c)
	if !ok {
		return
	}
	var in store.ConnectRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	d, err := h.StoreService.ConnectDomain(c.Request.Context(), orgID, in)
	if err != nil {
		storeError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionConnect, models.AuditEntityStoreDomain, &d.ID, nil, map[string]string{"domain": d.Name, "method": in.Method})
	c.JSON(http.StatusCreated, d)
}

// CheckStoreDomainConnection is POST /v1/store/domains/:id/connection/check.
func (h *Handler) CheckStoreDomainConnection(c *gin.Context) {
	orgID, id, ok := h.storeDomainCall(c)
	if !ok {
		return
	}
	d, err := h.StoreService.CheckConnection(c.Request.Context(), orgID, id)
	if err != nil {
		storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

// RegenerateStoreDomainConnection is POST /v1/store/domains/:id/connection/regenerate.
func (h *Handler) RegenerateStoreDomainConnection(c *gin.Context) {
	orgID, id, ok := h.storeDomainCall(c)
	if !ok {
		return
	}
	d, err := h.StoreService.RegenerateConnection(c.Request.Context(), orgID, id)
	if err != nil {
		storeError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityStoreDomain, &d.ID, nil, map[string]string{"domain": d.Name, "action": "regenerate"})
	c.JSON(http.StatusOK, d)
}

// RemoveStoreDomain is DELETE /v1/store/domains/:id: a connected domain with no mailboxes.
func (h *Handler) RemoveStoreDomain(c *gin.Context) {
	orgID, id, ok := h.storeDomainCall(c)
	if !ok {
		return
	}
	if err := h.StoreService.RemoveDomain(c.Request.Context(), orgID, id); err != nil {
		storeError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionRemove, models.AuditEntityStoreDomain, &id, nil, nil)
	c.Status(http.StatusNoContent)
}

// AddStoreDNSRecord is POST /v1/store/domains/:id/dns.
func (h *Handler) AddStoreDNSRecord(c *gin.Context) {
	orgID, id, ok := h.storeDomainCall(c)
	if !ok {
		return
	}
	var in store.DNSRecordInput
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	r, err := h.StoreService.AddDNSRecord(c.Request.Context(), orgID, id, in)
	if err != nil {
		storeError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityStoreDomain, &id, nil, map[string]string{"dns_record": r.Type + " " + r.Name})
	c.JSON(http.StatusCreated, r)
}

// UpdateStoreDNSRecord is PATCH /v1/store/domains/:id/dns/:record_id.
func (h *Handler) UpdateStoreDNSRecord(c *gin.Context) {
	orgID, id, ok := h.storeDomainCall(c)
	if !ok {
		return
	}
	rid, ok := storeRecordID(c)
	if !ok {
		return
	}
	var in store.DNSRecordPatch
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	r, err := h.StoreService.UpdateDNSRecord(c.Request.Context(), orgID, id, rid, in)
	if err != nil {
		storeError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityStoreDomain, &id, nil, map[string]string{"dns_record": r.Type + " " + r.Name})
	c.JSON(http.StatusOK, r)
}

// DeleteStoreDNSRecord is DELETE /v1/store/domains/:id/dns/:record_id.
func (h *Handler) DeleteStoreDNSRecord(c *gin.Context) {
	orgID, id, ok := h.storeDomainCall(c)
	if !ok {
		return
	}
	rid, ok := storeRecordID(c)
	if !ok {
		return
	}
	if err := h.StoreService.DeleteDNSRecord(c.Request.Context(), orgID, id, rid); err != nil {
		storeError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityStoreDomain, &id, nil, map[string]string{"dns_record_id": rid.String()})
	c.Status(http.StatusNoContent)
}

// CheckStoreDNS is POST /v1/store/domains/:id/dns/check: every record looked up in public DNS now.
func (h *Handler) CheckStoreDNS(c *gin.Context) {
	orgID, id, ok := h.storeDomainCall(c)
	if !ok {
		return
	}
	res, err := h.StoreService.CheckDNS(c.Request.Context(), orgID, id)
	if err != nil {
		storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// StoreMailboxSignIn is POST /v1/store/mailboxes/:id/sign-in: the mailbox's
// sign-in, the admin console's for the admin mailbox. Shown once, never cached.
func (h *Handler) StoreMailboxSignIn(c *gin.Context) {
	orgID, id, ok := h.storeDomainCall(c)
	if !ok {
		return
	}
	in, err := h.StoreService.SignIn(c.Request.Context(), orgID, id)
	if err != nil {
		storeError(c, err)
		return
	}
	h.auditOrg(c, models.AuditActionExport, models.AuditEntityStoreMailbox, &id, nil, map[string]string{"email": in.Email, "action": "sign_in"})
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, in)
}

// GraphKitWebhook is POST /v1/webhooks/graphkit: public, and trusted only
// once its signature verifies against the configured secrets.
func (h *Handler) GraphKitWebhook(c *gin.Context) {
	if h.StoreService == nil || !h.StoreService.Enabled() {
		errx.JSON(c, errx.ErrNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "unreadable body"))
		return
	}
	ev, err := h.StoreService.VerifyWebhook(c.GetHeader(graphkit.SignatureHeader), body)
	if err != nil {
		errx.JSON(c, errx.NewWithIdentifier(errx.Unauthorized, "invalid_signature", "The signature is not valid."))
		return
	}
	h.StoreService.NoteWebhook(c.Request.Context())
	if err := h.StoreService.ApplyEvent(c.Request.Context(), *ev); err != nil {
		log.Error().Err(err).Str("event_type", ev.Type).Msg("store: applying a webhook failed; GraphKit retries it")
		errx.JSON(c, errx.InternalError())
		return
	}
	c.Status(http.StatusNoContent)
}

// AdminStorePricing is GET /admin/store/pricing: GraphKit's live price list
// and this instance's retail for each TLD.
func (h *Handler) AdminStorePricing(c *gin.Context) {
	if h.StoreService == nil || !h.StoreService.Enabled() {
		errx.JSON(c, errx.NewWithIdentifier(errx.NotFound, store.CodeDisabled, store.NotConfiguredMessage))
		return
	}
	p, err := h.StoreService.AdminPricing(c.Request.Context())
	if err != nil {
		storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

// GetAgencyStore is GET /agency/:agencyId/store: the purchased domains and
// mailboxes of every client whose mailboxes the caller manages.
func (h *Handler) GetAgencyStore(c *gin.Context) {
	userID, agencyID, ok := h.agencyHomeCaller(c)
	if !ok {
		return
	}
	list, xerr := h.AgencyHomeService.ClientsWith(c.Request.Context(), agencyID, userID, models.PermManageEmails)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if h.StoreService == nil {
		c.JSON(http.StatusOK, &store.AgencyOverview{Clients: []store.AgencyClientStore{}, Renewals: []store.AgencyRenewal{}})
		return
	}
	clients := make([]store.AgencyClient, 0, len(list))
	for _, m := range list {
		clients = append(clients, store.AgencyClient{ID: m.OrganizationID, Name: m.Name, AvatarURL: m.AvatarURL})
	}
	out, err := h.StoreService.AgencyOverview(c.Request.Context(), clients)
	if err != nil {
		storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// StartStoreMicrosoftConsent is POST /v1/store/domains/:id/microsoft-consent: where the domain's Microsoft 365
// administrator approves Warmbly, landing back on this dashboard. It changes nothing until they do.
func (h *Handler) StartStoreMicrosoftConsent(c *gin.Context) {
	orgID, userID, ok := h.storeCaller(c)
	if !ok {
		return
	}
	id, ok := storeID(c)
	if !ok {
		return
	}
	var in store.ConsentRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&in); err != nil {
			errx.JSON(c, errx.InvalidBody(err))
			return
		}
	}
	link, err := h.StoreService.MicrosoftConsent(c.Request.Context(), orgID, userID, id, in)
	if err != nil {
		storeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, link)
}

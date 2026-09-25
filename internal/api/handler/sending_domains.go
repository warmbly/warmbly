package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/app/sendingdomain"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
)

// ListSendingDomains is GET /emails/domains: every domain the workspace sends
// from, with its authentication, tracking hosts and redirect.
func (h *Handler) ListSendingDomains(c *gin.Context) {
	orgID, _, ok := importCaller(c)
	if !ok {
		return
	}
	list, xerr := h.SendingDomainService.Overview(c.Request.Context(), orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// GetTrackingSuggestion is GET /emails/domains/:domain/tracking-suggestion.
func (h *Handler) GetTrackingSuggestion(c *gin.Context) {
	orgID, _, ok := importCaller(c)
	if !ok {
		return
	}
	domains, xerr := h.SendingDomainService.Overview(c.Request.Context(), orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	domain := mailhost.NormalizeDomain(strings.TrimPrefix(strings.ToLower(c.Param("domain")), "www."))
	var inUse []models.TrackingDomainUse
	found := false
	for _, d := range domains {
		if d.Domain == domain {
			inUse, found = d.TrackingDomains, true
		}
	}
	if !found {
		errx.Handle(c, errx.NewWithIdentifier(errx.NotFound, sendingdomain.ErrIDNotYours, "The workspace has no mailbox on this domain."))
		return
	}
	sug := h.SendingDomainService.TrackingSuggestion(c.Request.Context(), domain, inUse)
	if sug == nil {
		errx.Handle(c, errx.NewWithIdentifier(errx.BadRequest, sendingdomain.ErrIDNoTracking, "Open and click tracking is not set up on this instance."))
		return
	}
	sug.VendorDomain = h.SendingDomainService.VendorLink(c.Request.Context(), orgID, domain)
	c.JSON(http.StatusOK, sug)
}

// BulkDomainSetup is POST /emails/domains/bulk: one tracking subdomain and one
// root redirect for up to 100 domains, each through its vendor when the vendor
// can. Every row reports its own outcome. Safe to retry: each setting is set, not added.
func (h *Handler) BulkDomainSetup(c *gin.Context) {
	orgID, userID, ok := importCaller(c)
	if !ok {
		return
	}
	var req sendingdomain.BulkInput
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	rows, xerr := h.SendingDomainService.BulkSetup(c.Request.Context(), orgID, userID, req)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	meta := map[string]string{"domains": strconv.Itoa(len(rows)), "via": "bulk"}
	if req.TrackingLabel != "" {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityEmailAccount, nil, map[string]string{"tracking_label": req.TrackingLabel}, meta)
	}
	if req.RedirectURL != "" {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityDomainRedirect, nil, map[string]string{"target_url": req.RedirectURL}, meta)
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

type domainTrackingRequest struct {
	Host string `json:"host"`
}

// SetDomainTracking is PUT /emails/domains/:domain/tracking: one tracking host
// for every mailbox on the domain; an empty host clears it. Idempotent.
func (h *Handler) SetDomainTracking(c *gin.Context) {
	orgID, _, ok := importCaller(c)
	if !ok {
		return
	}
	var req domainTrackingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	status, n, xerr := h.SendingDomainService.ApplyTracking(c.Request.Context(), orgID, c.Param("domain"), req.Host)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityEmailAccount, nil, map[string]string{"tracking_domain": req.Host}, map[string]string{"domain": c.Param("domain")})
	c.JSON(http.StatusOK, gin.H{"tracking": status, "mailboxes": n})
}

// SetDomainRedirect is PUT /emails/domains/:domain/redirect. Idempotent: the
// same domain keeps its verification token, so the TXT record stays valid.
func (h *Handler) SetDomainRedirect(c *gin.Context) {
	orgID, userID, ok := importCaller(c)
	if !ok {
		return
	}
	var req sendingdomain.RedirectInput
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	r, xerr := h.SendingDomainService.SetRedirect(c.Request.Context(), orgID, userID, c.Param("domain"), req)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityDomainRedirect, &r.ID, nil, map[string]string{"domain": r.Domain})
	c.JSON(http.StatusOK, r)
}

// VerifyDomainRedirect is POST /emails/domains/:domain/redirect/verify: check DNS now.
func (h *Handler) VerifyDomainRedirect(c *gin.Context) {
	orgID, _, ok := importCaller(c)
	if !ok {
		return
	}
	r, xerr := h.SendingDomainService.VerifyRedirect(c.Request.Context(), orgID, c.Param("domain"))
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, r)
}

// DeleteDomainRedirect is DELETE /emails/domains/:domain/redirect.
func (h *Handler) DeleteDomainRedirect(c *gin.Context) {
	orgID, _, ok := importCaller(c)
	if !ok {
		return
	}
	if xerr := h.SendingDomainService.DeleteRedirect(c.Request.Context(), orgID, c.Param("domain")); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityDomainRedirect, nil, nil, map[string]string{"domain": c.Param("domain")})
	c.Status(http.StatusNoContent)
}

// InternalGetDomainRedirect is the tracking service's lookup for a verified bare domain.
//
//	GET /api/v1/internal/domain-redirects/:host -> 200 {"target_url"} | 404
func (h *Handler) InternalGetDomainRedirect(c *gin.Context) {
	if h.SendingDomainService == nil {
		c.Status(http.StatusNotFound)
		return
	}
	target, ok, err := h.SendingDomainService.Lookup(c.Request.Context(), c.Param("host"))
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, gin.H{"target_url": target})
}

type vendorForwardingRequest struct {
	URL string `json:"url"`
}

// SetDomainVendorForwarding is PUT /emails/domains/:domain/vendor-forwarding:
// the inbox vendor holding the domain forwards its root. An empty url removes
// it where the vendor allows. Idempotent; the service audits the change.
func (h *Handler) SetDomainVendorForwarding(c *gin.Context) {
	orgID, userID, ok := importCaller(c)
	if !ok {
		return
	}
	var req vendorForwardingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	link, xerr := h.SendingDomainService.VendorForward(c.Request.Context(), orgID, userID, c.Param("domain"), req.URL)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, link)
}

// SetDomainVendorTracking is POST /emails/domains/:domain/vendor-tracking: the
// inbox vendor writes the tracking CNAME, then every mailbox on the domain uses
// the host. Safe to retry: the record is upserted and the host is set, not added.
func (h *Handler) SetDomainVendorTracking(c *gin.Context) {
	orgID, _, ok := importCaller(c)
	if !ok {
		return
	}
	var req domainTrackingRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Host) == "" {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	status, n, xerr := h.SendingDomainService.VendorTracking(c.Request.Context(), orgID, c.Param("domain"), req.Host)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityEmailAccount, nil, map[string]string{"tracking_domain": req.Host}, map[string]string{"domain": c.Param("domain"), "via": "vendor"})
	c.JSON(http.StatusOK, gin.H{"tracking": status, "mailboxes": n})
}

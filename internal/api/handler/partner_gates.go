package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Writes that are about the signed-in account rather than the selected workspace, allowed while it is read-only.
var partnerReadOnlyExempt = map[string]bool{
	"POST /v1/organization":            true,
	"POST /v1/organization/switch/:id": true,
	"POST /v1/getaway":                 true,
	"POST /v1/invitations/accept":      true,
}

func partnerWriteMethod(m string) bool {
	return m == http.MethodPost || m == http.MethodPut || m == http.MethodPatch || m == http.MethodDelete
}

// PartnerReadOnlyGate keeps a workspace its reseller suspended or cancelled read-only: members and keys may read, not write.
func (h *Handler) PartnerReadOnlyGate() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.Partners == nil || !partnerWriteMethod(c.Request.Method) {
			c.Next()
			return
		}
		route := c.Request.Method + " " + c.FullPath()
		if partnerReadOnlyExempt[route] || strings.HasPrefix(c.FullPath(), "/v1/me/") || c.FullPath() == "/v1/me" {
			c.Next()
			return
		}
		orgID := middleware.GetOrganizationID(c)
		if orgID == nil {
			c.Next()
			return
		}
		if xerr := h.partnerWriteRefusal(c, *orgID); xerr != nil {
			errx.JSON(c, xerr)
			c.Abort()
			return
		}
		c.Next()
	}
}

// partnerWriteRefusal is the refusal for a change to a workspace its partner paused, nil when the change may go ahead.
func (h *Handler) partnerWriteRefusal(c *gin.Context, orgID uuid.UUID) *errx.Error {
	if h.Partners == nil || orgID == uuid.Nil {
		return nil
	}
	ref, xerr := h.Partners.ReadOnly(c.Request.Context(), orgID)
	if xerr != nil {
		return xerr
	}
	if ref != nil {
		return partnerPausedError(ref)
	}
	return nil
}

func partnerPausedError(ref *models.PartnerManagedRef) *errx.Error {
	id := "organization_suspended"
	if ref.State == models.PartnerOrgCancelled {
		id = "organization_cancelled"
	}
	return errx.NewWithIdentifier(errx.Forbidden, id,
		"This workspace is paused by "+ref.DisplayName+", which bills it. You can read everything, but changes and sending wait until it is resumed.")
}

// Billing reads and the discount check stay open in a managed workspace; nothing else in billing applies.
var partnerBillingOpen = map[string]bool{
	"/v1/subscription/discount/validate":  true,
	"/v1/subscription/enterprise-inquiry": true,
}

// PartnerBillingGate refuses self-serve billing in a workspace a reseller bills: no checkout, plan change, cancellation or top-up.
func (h *Handler) PartnerBillingGate() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.Partners == nil || !partnerWriteMethod(c.Request.Method) || partnerBillingOpen[c.FullPath()] {
			c.Next()
			return
		}
		orgID := middleware.GetOrganizationID(c)
		if orgID == nil {
			c.Next()
			return
		}
		if managed, name := h.Partners.IsPartnerManaged(c.Request.Context(), *orgID); managed {
			errx.JSON(c, errx.NewWithIdentifier(errx.Forbidden, "partner_managed",
				"This workspace is billed through "+name+". Plan, payment and balance are managed there."))
			c.Abort()
			return
		}
		c.Next()
	}
}

// RefusePartnerBound stops an account a reseller signs in from adding its own way in (a password or a passkey).
func (h *Handler) RefusePartnerBound() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.Partners == nil {
			c.Next()
			return
		}
		userID, err := uuid.Parse(middleware.GetUserID(c))
		if err != nil {
			c.Next()
			return
		}
		bound, err := h.Partners.IsPartnerBound(c.Request.Context(), userID)
		if err != nil {
			errx.JSON(c, errx.InternalError())
			c.Abort()
			return
		}
		if bound {
			errx.JSON(c, errx.NewWithIdentifier(errx.Forbidden, "partner_account",
				"This account signs in through the service that manages your workspace, so it cannot add a password, passkey or second factor here."))
			c.Abort()
			return
		}
		c.Next()
	}
}

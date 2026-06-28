package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

// referralActor resolves the (orgID, userID) pair every referral handler needs,
// writing the error response itself and returning ok=false when either is
// missing.
func (h *Handler) referralActor(c *gin.Context) (orgID, userID uuid.UUID, ok bool) {
	org := middleware.GetOrganizationID(c)
	if org == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return uuid.Nil, uuid.Nil, false
	}
	uid, err := uuid.Parse(c.GetString("user_id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.Unauthorized, "invalid user"))
		return uuid.Nil, uuid.Nil, false
	}
	return *org, uid, true
}

// referralPaging reads the limit + opaque cursor query params shared by the
// referral list endpoints. An invalid cursor surfaces as a 400.
func referralPaging(c *gin.Context) (limit, offset int, xerr *errx.Error) {
	limit = 20
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 100 {
		limit = 100
	}
	offset, xerr = paging.DecodeOffsetCursor(c.Query("cursor"))
	return limit, offset, xerr
}

func nextCursor(count, limit, offset int) models.CPagination {
	p := models.CPagination{HasMore: count == limit}
	if p.HasMore {
		p.NextCursor = paging.EncodeOffset(offset + limit)
	}
	return p
}

// ListAppliedDiscounts returns the current org's promo-code redemption history
// for the billing page.
func (h *Handler) ListAppliedDiscounts(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	rows, xerr := h.DiscountService.ListOrganizationRedemptions(c.Request.Context(), *orgID, limit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// GetReferralSummary returns the caller's referral code, share link, earnings,
// and conversion counts. Mints the code on first view.
func (h *Handler) GetReferralSummary(c *gin.Context) {
	orgID, userID, ok := h.referralActor(c)
	if !ok {
		return
	}
	summary, xerr := h.ReferralService.Summary(c.Request.Context(), userID, orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, summary)
}

// EnsureReferralCode idempotently mints the caller's referral code.
func (h *Handler) EnsureReferralCode(c *gin.Context) {
	orgID, userID, ok := h.referralActor(c)
	if !ok {
		return
	}
	code, xerr := h.ReferralService.EnsureCode(c.Request.Context(), userID, orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	codeID := code.ID
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityReferral, &codeID, nil, map[string]string{
		"code": code.Code,
	})
	c.JSON(http.StatusOK, code)
}

// ListReferralAttributions returns the caller's referred organizations.
func (h *Handler) ListReferralAttributions(c *gin.Context) {
	orgID, _, ok := h.referralActor(c)
	if !ok {
		return
	}
	limit, offset, xerr := referralPaging(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	rows, xerr := h.ReferralService.ListAttributions(c.Request.Context(), orgID, limit, offset)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, models.ReferralAttributionsResult{
		Data:       rows,
		Pagination: nextCursor(len(rows), limit, offset),
	})
}

// ListReferralEarnings returns the caller's referral earnings ledger trail.
func (h *Handler) ListReferralEarnings(c *gin.Context) {
	orgID, _, ok := h.referralActor(c)
	if !ok {
		return
	}
	limit, offset, xerr := referralPaging(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	rows, xerr := h.ReferralService.ListEarnings(c.Request.Context(), orgID, limit, offset)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, models.ReferralEarningsResult{
		Data:       rows,
		Pagination: nextCursor(len(rows), limit, offset),
	})
}

package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// --- Admin discount-code management ---

// AdminListDiscounts lists discount codes with optional status/search filters.
func (h *Handler) AdminListDiscounts(c *gin.Context) {
	search := &models.AdminDiscountSearch{
		Status: c.Query("status"),
		Search: c.Query("search"),
		Cursor: parseCursor(c.Query("cursor")),
		Limit:  parseLimit(c.Query("limit"), 50),
	}

	result, xerr := h.DiscountService.List(c.Request.Context(), search)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// AdminCreateDiscount creates a new discount code.
func (h *Handler) AdminCreateDiscount(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	var req models.CreateDiscountCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	code, xerr := h.DiscountService.Create(c.Request.Context(), *adminID, &req, c.ClientIP(), c.GetHeader("User-Agent"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusCreated, code)
}

// AdminGetDiscount returns a single discount code.
func (h *Handler) AdminGetDiscount(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid discount code ID"))
		return
	}

	code, xerr := h.DiscountService.Get(c.Request.Context(), id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, code)
}

// AdminUpdateDiscount applies a partial update to a discount code.
func (h *Handler) AdminUpdateDiscount(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid discount code ID"))
		return
	}

	var req models.UpdateDiscountCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	code, xerr := h.DiscountService.Update(c.Request.Context(), *adminID, id, &req, c.ClientIP(), c.GetHeader("User-Agent"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, code)
}

// AdminDeleteDiscount removes a discount code.
func (h *Handler) AdminDeleteDiscount(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid discount code ID"))
		return
	}

	xerr := h.DiscountService.Delete(c.Request.Context(), *adminID, id, c.ClientIP(), c.GetHeader("User-Agent"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "discount code deleted successfully"})
}

// AdminListDiscountRedemptions lists redemptions for a discount code.
func (h *Handler) AdminListDiscountRedemptions(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid discount code ID"))
		return
	}

	cursor := parseCursor(c.Query("cursor"))
	limit := parseLimit(c.Query("limit"), 50)

	result, xerr := h.DiscountService.ListRedemptions(c.Request.Context(), id, cursor, limit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// --- Customer-facing validation ---

// ValidateDiscountCode previews whether a code is valid for the current org and
// (optionally) a target plan, returning the discount details for display.
func (h *Handler) ValidateDiscountCode(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var req models.ValidateDiscountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	preview, xerr := h.DiscountService.Validate(c.Request.Context(), *orgID, req.Code, req.PlanID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, preview)
}

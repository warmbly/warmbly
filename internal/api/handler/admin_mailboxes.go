package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// AdminSearchMailboxes is GET /admin/mailboxes — cross-org mailbox
// triage. Search covers email / owner email / org name; status filter
// defaults to active so the table shows real surface area first.
func (h *Handler) AdminSearchMailboxes(c *gin.Context) {
	var search models.AdminMailboxSearch
	if err := c.ShouldBindQuery(&search); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid query parameters"))
		return
	}
	result, xerr := h.AdminService.SearchMailboxes(c.Request.Context(), &search)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, result)
}

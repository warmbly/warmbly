package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/dnsauth"
)

// GetEmailAuthCheck validates SPF/DKIM/DMARC for a mailbox's sending domain on
// demand. Authentication alignment is a hard bulk-sender requirement and the
// most common silent deliverability failure, so this lets the user confirm
// their domain is set up correctly without leaving the dashboard.
func (h *Handler) GetEmailAuthCheck(c *gin.Context) {
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	account, xerr := h.EmailService.Get(c.Request.Context(), userID.String(), c.Param("id"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	domain := ""
	if at := strings.LastIndex(account.Email, "@"); at >= 0 {
		domain = account.Email[at+1:]
	}

	res := dnsauth.Check(c.Request.Context(), domain, nil)
	c.JSON(http.StatusOK, res)
}

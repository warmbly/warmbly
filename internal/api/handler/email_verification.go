package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
)

// verifyEmailRequest is the optional JSON body for VerifyEmail. The address may
// also be passed as the `email` query param; the body takes precedence.
type verifyEmailRequest struct {
	Email string `json:"email"`
}

// VerifyEmail verifies a single email address on demand (syntax -> MX -> SMTP
// RCPT probe -> catch-all detection) and returns the emailverify.Result. This
// is pre-send verification: it lets the user/admin confirm an address is
// deliverable *before* a worker ever sends to it, instead of learning from a
// hard bounce after the fact.
//
// Control-plane only: the SMTP RCPT probe behind this runs from the backend (a
// non-sending IP). Probing must never run from worker (sending) IPs — see
// internal/pkg/emailverify.
//
// Route registration is intentionally NOT done here; the parent workstream
// wires it in internal/api/routes.go behind the appropriate permission gates.
func (h *Handler) VerifyEmail(c *gin.Context) {
	if _, err := middleware.GetUserUUID(c); err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	if h.EmailVerifyService == nil {
		errx.JSON(c, errx.InternalError())
		return
	}

	var req verifyEmailRequest
	// Body is optional; ignore a bind error and fall back to the query param.
	_ = c.ShouldBindJSON(&req)
	email := strings.TrimSpace(req.Email)
	if email == "" {
		email = strings.TrimSpace(c.Query("email"))
	}
	if email == "" {
		errx.JSON(c, errx.ErrEmail)
		return
	}

	res := h.EmailVerifyService.VerifyAddress(c.Request.Context(), email)
	c.JSON(http.StatusOK, res)
}

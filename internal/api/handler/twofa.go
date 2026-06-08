package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/errx"
)

// TwoFAStatus reports whether the caller has 2FA enabled.
func (h *Handler) TwoFAStatus(c *gin.Context) {
	uid, ok := notifActor(c)
	if !ok {
		return
	}
	enabled, err := h.TwoFAService.IsEnabled(c.Request.Context(), uid)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	c.JSON(http.StatusOK, gin.H{"enabled": enabled})
}

// TwoFAEnrollStart begins enrollment, returning the secret + otpauth URI once.
func (h *Handler) TwoFAEnrollStart(c *gin.Context) {
	uid, ok := notifActor(c)
	if !ok {
		return
	}
	res, xerr := h.TwoFAService.EnrollStart(c.Request.Context(), uid)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, res)
}

// TwoFAEnrollConfirm verifies a code, enables 2FA, and returns recovery codes once.
func (h *Handler) TwoFAEnrollConfirm(c *gin.Context) {
	uid, ok := notifActor(c)
	if !ok {
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	codes, xerr := h.TwoFAService.EnrollConfirm(c.Request.Context(), uid, body.Code)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"recovery_codes": codes})
}

// TwoFADisable turns off 2FA (requires a current TOTP or recovery code).
func (h *Handler) TwoFADisable(c *gin.Context) {
	uid, ok := notifActor(c)
	if !ok {
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	_ = c.ShouldBindJSON(&body)
	if xerr := h.TwoFAService.Disable(c.Request.Context(), uid, body.Code); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// TwoFAVerifyLogin (PUBLIC) exchanges a pending token + code for a real session.
func (h *Handler) TwoFAVerifyLogin(c *gin.Context) {
	var body struct {
		PendingToken string `json:"pending_token"`
		Code         string `json:"code"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	tok, xerr := h.TwoFAService.VerifyLogin(c.Request.Context(), body.PendingToken, body.Code, c.ClientIP(), c.Request.UserAgent())
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, tok)
}

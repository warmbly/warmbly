package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/auth"
	"github.com/warmbly/warmbly/internal/errx"
)

func (h *Handler) LoginStart(c *gin.Context) {
	var data auth.AuthData

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, err := h.AuthService.LoginStart(c.Request.Context(), &data, c.ClientIP())
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) LoginConfirm(c *gin.Context) {
	var data auth.ConfirmData

	sessionToken := c.Query("session")

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, err)
		return
	}

	resp, err := h.AuthService.LoginConfirm(c.Request.Context(), &data, sessionToken, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) RegistrationStart(c *gin.Context) {
	var data auth.AuthData

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, err := h.AuthService.RegistrationStart(c.Request.Context(), &data, c.ClientIP())
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) RegistrationConfirm(c *gin.Context) {
	var data auth.ConfirmData

	sessionToken := c.Query("session")

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, err)
		return
	}

	if err := h.AuthService.RegistrationConfirm(c.Request.Context(), &data, sessionToken, c.ClientIP()); err != nil {
		errx.Handle(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) RefreshToken(c *gin.Context) {
	var data struct {
		RefreshToken string `json:"refresh_token"`
	}

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	token, err := h.TokenService.RefreshToken(c.Request.Context(), data.RefreshToken)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, token)
}

func (h *Handler) Logout(c *gin.Context) {
	accessToken := middleware.GetAccessToken(c)
	userIDStr := middleware.GetUserID(c)

	if err := h.TokenService.RevokeSession(c.Request.Context(), accessToken); err != nil {
		errx.Handle(c, err)
		return
	}

	// Audit log
	if userID, err := uuid.Parse(userIDStr); err == nil {
		audit.LogLogout(h.AuditService, c.Request.Context(), userID, c.ClientIP(), c.Request.UserAgent())
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) LogoutAll(c *gin.Context) {
	accessToken := middleware.GetAccessToken(c)
	userIDStr := middleware.GetUserID(c)

	if err := h.TokenService.RevokeAllSession(c.Request.Context(), accessToken); err != nil {
		errx.Handle(c, err)
		return
	}

	// Audit log
	if userID, err := uuid.Parse(userIDStr); err == nil {
		audit.LogLogout(h.AuditService, c.Request.Context(), userID, c.ClientIP(), c.Request.UserAgent())
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) GetUser(c *gin.Context) {
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	u, xerr := h.UserService.GetUser(c.Request.Context(), uid)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusNoContent, u)
}

func (h *Handler) ResetPasswordStart(c *gin.Context) {
	var data auth.ResetPasswordStart

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	if err := h.AuthService.ResetPasswordStart(c.Request.Context(), &data, c.ClientIP()); err != nil {
		errx.Handle(c, err)
		return
	}

	c.Status(http.StatusOK)
}

func (h *Handler) ResetPasswordConfirm(c *gin.Context) {
	var data auth.ResetPasswordConfirm

	sessionToken := c.Query("session")

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	if err := h.AuthService.ResetPasswordConfirm(c.Request.Context(), &data, sessionToken, c.ClientIP()); err != nil {
		errx.Handle(c, err)
		return
	}

	c.Status(http.StatusOK)
}

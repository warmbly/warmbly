package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/auth"
	"github.com/warmbly/warmbly/internal/errx"
)

const authRequestTimeout = 15 * time.Second

func (h *Handler) LoginStart(c *gin.Context) {
	var data auth.AuthData

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), authRequestTimeout)
	defer cancel()

	resp, err := h.AuthService.LoginStart(ctx, &data, c.ClientIP())
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) LoginConfirm(c *gin.Context) {
	var data auth.ConfirmData

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, err)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), authRequestTimeout)
	defer cancel()

	resp, err := h.AuthService.LoginConfirm(ctx, &data, data.Session, c.ClientIP(), c.Request.UserAgent())
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

	ctx, cancel := context.WithTimeout(c.Request.Context(), authRequestTimeout)
	defer cancel()

	resp, err := h.AuthService.RegistrationStart(ctx, &data, c.ClientIP())
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) RegistrationConfirm(c *gin.Context) {
	var data auth.ConfirmData

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, err)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), authRequestTimeout)
	defer cancel()

	if err := h.AuthService.RegistrationConfirm(ctx, &data, data.Session, c.ClientIP()); err != nil {
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

	if err := h.TokenService.RevokeSession(c.Request.Context(), accessToken); err != nil {
		errx.Handle(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) LogoutAll(c *gin.Context) {
	accessToken := middleware.GetAccessToken(c)

	if err := h.TokenService.RevokeAllSession(c.Request.Context(), accessToken); err != nil {
		errx.Handle(c, err)
		return
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

	ctx := c.Request.Context()

	u, xerr := h.UserService.GetUser(ctx, uid)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	// Populate the per-user label groups so the frontend can render
	// folder/tag pickers on initial page load without three extra
	// round-trips. Without this, anything the user created in a
	// previous session would disappear after a refresh: the cache
	// would optimistic-update from a Create response, but on reload
	// the /auth/me payload had empty folders/tags/categories.
	if folders, ferr := h.FolderService.List(ctx, uid); ferr == nil {
		u.Folders = folders
	}
	if tags, terr := h.TagService.List(ctx, uid); terr == nil {
		u.Tags = tags
	}
	if cats, cerr := h.CategoryService.List(ctx, uid); cerr == nil {
		u.Categories = cats
	}

	c.JSON(http.StatusOK, u)
}

func (h *Handler) ResetPasswordStart(c *gin.Context) {
	var data auth.ResetPasswordStart

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), authRequestTimeout)
	defer cancel()

	if err := h.AuthService.ResetPasswordStart(ctx, &data, c.ClientIP()); err != nil {
		errx.Handle(c, err)
		return
	}

	c.Status(http.StatusOK)
}

func (h *Handler) ResetPasswordConfirm(c *gin.Context) {
	var data auth.ResetPasswordConfirm

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), authRequestTimeout)
	defer cancel()

	if err := h.AuthService.ResetPasswordConfirm(ctx, &data, data.Session, c.ClientIP()); err != nil {
		errx.Handle(c, err)
		return
	}

	c.Status(http.StatusOK)
}

// ChangePassword updates the signed-in user's password (current + new).
func (h *Handler) ChangePassword(c *gin.Context) {
	uid, err := uuid.Parse(middleware.GetUserID(c))
	if err != nil {
		errx.Handle(c, errx.ErrUnauthorized)
		return
	}

	var data auth.ChangePassword
	if berr := c.ShouldBindJSON(&data); berr != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), authRequestTimeout)
	defer cancel()

	if xerr := h.AuthService.ChangePassword(ctx, uid, &data); xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.Status(http.StatusOK)
}

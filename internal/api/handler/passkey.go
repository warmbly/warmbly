package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
)

// --- Registration (authenticated: a signed-in user enrolls a passkey) ---

func (h *Handler) PasskeyRegisterBegin(c *gin.Context) {
	uid, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	options, xerr := h.PasskeyService.BeginRegistration(c.Request.Context(), uid)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, options)
}

type passkeyRegisterFinishRequest struct {
	Name       string          `json:"name"`
	Credential json.RawMessage `json:"credential"`
}

func (h *Handler) PasskeyRegisterFinish(c *gin.Context) {
	uid, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	var req passkeyRegisterFinishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	if len(req.Credential) == 0 {
		errx.Handle(c, errx.ErrPasskey)
		return
	}

	view, xerr := h.PasskeyService.FinishRegistration(c.Request.Context(), uid, req.Name, req.Credential)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, view)
}

// --- Login (public, discoverable/usernameless — single step, no email OTP) ---

func (h *Handler) PasskeyLoginBegin(c *gin.Context) {
	challenge, xerr := h.PasskeyService.BeginLogin(c.Request.Context())
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, challenge)
}

type passkeyLoginFinishRequest struct {
	Session    string          `json:"session"`
	Credential json.RawMessage `json:"credential"`
}

func (h *Handler) PasskeyLoginFinish(c *gin.Context) {
	var req passkeyLoginFinishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	if req.Session == "" || len(req.Credential) == 0 {
		errx.Handle(c, errx.ErrPasskey)
		return
	}

	token, xerr := h.PasskeyService.FinishLogin(c.Request.Context(), req.Session, req.Credential, c.ClientIP(), c.Request.UserAgent())
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, token)
}

// --- Management (authenticated) ---

func (h *Handler) PasskeyListCredentials(c *gin.Context) {
	uid, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	views, xerr := h.PasskeyService.ListCredentials(c.Request.Context(), uid)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, views)
}

type passkeyRenameRequest struct {
	Name string `json:"name"`
}

func (h *Handler) PasskeyRenameCredential(c *gin.Context) {
	uid, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	var req passkeyRenameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	view, xerr := h.PasskeyService.RenameCredential(c.Request.Context(), uid, id, req.Name)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, view)
}

func (h *Handler) PasskeyDeleteCredential(c *gin.Context) {
	uid, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	if xerr := h.PasskeyService.DeleteCredential(c.Request.Context(), uid, id); xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.Status(http.StatusNoContent)
}

package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/admindevice"
	"github.com/warmbly/warmbly/internal/errx"
)

func (h *Handler) AdminDeviceStart(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if xerr := h.AdminDevice.LimitPublic(c.Request.Context(), c.ClientIP(), false); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var req admindevice.Request
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	res, xerr := h.AdminDevice.Start(c.Request.Context(), req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusCreated, res)
}

func (h *Handler) AdminDevicePoll(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if xerr := h.AdminDevice.LimitPublic(c.Request.Context(), c.ClientIP(), true); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var req struct {
		DeviceSecret string `json:"device_secret"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	res, xerr := h.AdminDevice.Poll(c.Request.Context(), req.DeviceSecret)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if res.Status == "approved" && res.UserID != nil && h.AdminService != nil {
		h.AdminService.LogAdminAction(c.Request.Context(), *res.UserID, "admin_device_session_issued", "session", nil, nil, c.ClientIP(), "warmblyctl")
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) AdminDeviceDescribe(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var req struct {
		UserCode string `json:"user_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	res, xerr := h.AdminDevice.Describe(c.Request.Context(), req.UserCode, middleware.GetSession(c))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"request": res, "requested_access": "Your live platform administrator permissions through a separate revocable session", "admin_permissions": middleware.GetAdminPermissions(c)})
}

func (h *Handler) AdminDeviceDecide(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var req struct {
		UserCode     string `json:"user_code"`
		ConsentToken string `json:"consent_token"`
		Decision     string `json:"decision"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	if xerr := h.AdminDevice.Decide(c.Request.Context(), req.UserCode, req.ConsentToken, req.Decision, middleware.GetSession(c)); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if h.AdminService != nil {
		h.AdminService.LogAdminAction(c.Request.Context(), middleware.GetSession(c).UserID, "admin_device_"+req.Decision, "session", nil, nil, c.ClientIP(), c.Request.UserAgent())
	}
	c.JSON(http.StatusOK, gin.H{"status": req.Decision})
}

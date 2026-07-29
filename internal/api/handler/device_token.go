package handler

import (
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// APNs tokens are hex; allow generous length for future formats.
var deviceTokenPattern = regexp.MustCompile(`^[0-9a-fA-F]{16,512}$`)

// RegisterDeviceToken stores an APNs device token for the caller. Upserts, so
// retries and re-registrations on app launch are naturally safe.
func (h *Handler) RegisterDeviceToken(c *gin.Context) {
	uid, ok := notifActor(c)
	if !ok {
		return
	}
	var req models.RegisterDeviceTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	if !deviceTokenPattern.MatchString(req.Token) {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid device token"))
		return
	}
	if req.Platform == "" {
		req.Platform = "ios"
	}
	if req.Platform != "ios" {
		errx.JSON(c, errx.New(errx.BadRequest, "unsupported platform"))
		return
	}
	if req.Environment == "" {
		req.Environment = "production"
	}
	if req.Environment != "production" && req.Environment != "development" {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid environment"))
		return
	}
	if err := h.NotificationService.RegisterDevice(c.Request.Context(), uid, req.Platform, req.Token, req.Environment); err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteDeviceToken removes one of the caller's device tokens (sign-out).
func (h *Handler) DeleteDeviceToken(c *gin.Context) {
	uid, ok := notifActor(c)
	if !ok {
		return
	}
	token := c.Param("token")
	if !deviceTokenPattern.MatchString(token) {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid device token"))
		return
	}
	if err := h.NotificationService.UnregisterDevice(c.Request.Context(), uid, token); err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

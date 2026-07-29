package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/notification"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// emailDeliveryInfo tells clients the email-channel bounds so the window
// control renders the right range without hardcoding it.
func emailDeliveryInfo() gin.H {
	return gin.H{
		"min_minutes": config.NotificationEmailWindowMinMinutes,
		"max_minutes": config.NotificationEmailWindowMaxMinutes,
		"daily_cap":   notification.EmailDailyCap(),
	}
}

func notifActor(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(middleware.GetUserID(c))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid user"))
		return uuid.Nil, false
	}
	return id, true
}

// GetNotificationPreferences returns the caller's notification preferences
// (merged over defaults).
func (h *Handler) GetNotificationPreferences(c *gin.Context) {
	uid, ok := notifActor(c)
	if !ok {
		return
	}
	prefs, xerr := h.NotificationService.GetPreferences(c.Request.Context(), uid)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"preferences": prefs, "email_delivery": emailDeliveryInfo()})
}

// UpdateNotificationPreferences replaces the caller's notification preferences.
func (h *Handler) UpdateNotificationPreferences(c *gin.Context) {
	uid, ok := notifActor(c)
	if !ok {
		return
	}
	var req models.UpdateNotificationPreferencesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	if req.Preferences.EmailDigestMinutes == 0 {
		req.Preferences.EmailDigestMinutes = config.NotificationEmailWindowDefaultMinutes
	}
	if req.Preferences.EmailDigestMinutes < config.NotificationEmailWindowMinMinutes ||
		req.Preferences.EmailDigestMinutes > config.NotificationEmailWindowMaxMinutes {
		errx.JSON(c, errx.New(errx.BadRequest, fmt.Sprintf(
			"email_digest_minutes must be between %d and %d",
			config.NotificationEmailWindowMinMinutes, config.NotificationEmailWindowMaxMinutes)))
		return
	}
	if xerr := h.NotificationService.UpdatePreferences(c.Request.Context(), uid, &req.Preferences); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"preferences": req.Preferences, "email_delivery": emailDeliveryInfo()})
}

// ListNotifications returns the caller's recent feed + the unread count.
func (h *Handler) ListNotifications(c *gin.Context) {
	uid, ok := notifActor(c)
	if !ok {
		return
	}
	limit := 50
	if l := c.Query("limit"); l != "" {
		if n, e := strconv.Atoi(l); e == nil {
			limit = n
		}
	}
	unreadOnly := c.Query("unread") == "1" || c.Query("unread") == "true"
	items, xerr := h.NotificationService.List(c.Request.Context(), uid, limit, unreadOnly)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	unread, _ := h.NotificationService.UnreadCount(c.Request.Context(), uid)
	c.JSON(http.StatusOK, gin.H{"notifications": items, "unread": unread})
}

// MarkNotificationRead marks one notification read.
func (h *Handler) MarkNotificationRead(c *gin.Context) {
	uid, ok := notifActor(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	if xerr := h.NotificationService.MarkRead(c.Request.Context(), uid, id); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// MarkAllNotificationsRead marks the caller's whole feed read (PUT on the
// collection — keeps the route tree clear of a static-vs-:id conflict).
func (h *Handler) MarkAllNotificationsRead(c *gin.Context) {
	uid, ok := notifActor(c)
	if !ok {
		return
	}
	if xerr := h.NotificationService.MarkAllRead(c.Request.Context(), uid); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

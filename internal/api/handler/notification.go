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
		errx.JSON(c, errx.InvalidBody(err))
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
	// A portal session's feed holds only its agency's client workspaces, and the unread count only what it shows.
	if middleware.GetPortalAgencyID(c) != nil {
		shown := items[:0]
		unread = 0
		for _, n := range items {
			if n.OrganizationID == nil {
				continue
			}
			ok, xerr := middleware.PortalAllowsWorkspace(c, *n.OrganizationID)
			if xerr != nil {
				errx.JSON(c, xerr)
				return
			}
			if ok {
				shown = append(shown, n)
				if n.ReadAt == nil {
					unread++
				}
			}
		}
		items = shown
	}
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
	if middleware.GetPortalAgencyID(c) != nil {
		if xerr := h.markPortalRead(c, uid, &id); xerr != nil {
			errx.JSON(c, xerr)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	if xerr := h.NotificationService.MarkRead(c.Request.Context(), uid, id); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// markPortalRead marks read, for a portal session, only notifications from its agency's client workspaces.
func (h *Handler) markPortalRead(c *gin.Context, uid uuid.UUID, notifID *uuid.UUID) *errx.Error {
	members, xerr := h.OrganizationService.GetUserOrganizations(c.Request.Context(), uid)
	if xerr != nil {
		return xerr
	}
	allowed := make([]uuid.UUID, 0, len(members))
	for _, m := range members {
		ok, xerr := middleware.PortalAllowsWorkspace(c, m.OrganizationID)
		if xerr != nil {
			return xerr
		}
		if ok {
			allowed = append(allowed, m.OrganizationID)
		}
	}
	return h.NotificationService.MarkReadIn(c.Request.Context(), uid, notifID, allowed)
}

// MarkAllNotificationsRead marks the caller's whole feed read (PUT on the
// collection — keeps the route tree clear of a static-vs-:id conflict).
func (h *Handler) MarkAllNotificationsRead(c *gin.Context) {
	uid, ok := notifActor(c)
	if !ok {
		return
	}
	if middleware.GetPortalAgencyID(c) != nil {
		if xerr := h.markPortalRead(c, uid, nil); xerr != nil {
			errx.JSON(c, xerr)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	if xerr := h.NotificationService.MarkAllRead(c.Request.Context(), uid); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

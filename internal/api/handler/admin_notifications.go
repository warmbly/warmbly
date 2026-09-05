package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/app/opsnotify"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
)

// Operator notification channels. The channels themselves live in the instance
// settings document and are written through PUT /admin/instance/settings; this
// file adds the two things that document cannot carry: the catalog of
// subscribable events, and a way to prove a channel actually works.

// AdminNotificationEvents returns the subscribable event catalog.
//
// A self-hosted deployment gets the subset that means something without
// billing, so the panel never offers to alert on a commercial event that can
// never fire there.
func (h *Handler) AdminNotificationEvents(c *gin.Context) {
	selfHosted := config.SelfHosted()
	out := make([]opsnotify.EventDef, 0, len(opsnotify.Catalog))
	for _, def := range opsnotify.Catalog {
		if selfHosted && !def.SelfHostRelevant {
			continue
		}
		out = append(out, def)
	}
	c.JSON(http.StatusOK, gin.H{"events": out, "self_hosted": selfHosted})
}

// AdminTestNotificationChannel delivers a test alert.
//
// It accepts either a saved channel id or a full channel body, so an operator
// can prove a webhook URL before committing it to the document. Unlike every
// other delivery this one is synchronous and reports the transport error, and
// it is the only place a channel is exercised on demand.
func (h *Handler) AdminTestNotificationChannel(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	if h.OpsNotifier == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "Operator notifications are not available on this deployment."))
		return
	}

	var req struct {
		// ID selects a saved channel; when set the body's other fields are ignored.
		ID string `json:"id"`
		// The unsaved-channel form.
		Type   string `json:"type"`
		Name   string `json:"name"`
		Target string `json:"target"`
		Secret string `json:"secret"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	var ch instancesettings.NotifyChannel
	if req.ID != "" {
		if h.InstanceSettings == nil {
			errx.JSON(c, errx.New(errx.BadRequest, "Instance settings are not available on this deployment."))
			return
		}
		saved, ok := h.InstanceSettings.Get(c.Request.Context()).Notifications.Find(req.ID)
		if !ok {
			errx.JSON(c, errx.New(errx.NotFound, "no such notification channel"))
			return
		}
		ch = saved
	} else {
		ch = instancesettings.NotifyChannel{
			ID:      "test",
			Name:    req.Name,
			Type:    req.Type,
			Target:  req.Target,
			Secret:  req.Secret,
			Enabled: true,
		}
		// A masked target means the client sent back a redacted read without
		// retyping it, which cannot be delivered to. Ask for the id instead.
		if ch.Target == instancesettings.Masked {
			errx.JSON(c, errx.New(errx.BadRequest, "Save the channel first, then send a test to it by id."))
			return
		}
		if ch.IsWebhookTransport() {
			if err := instancesettings.ValidateChannelURL(ch.Target); err != nil {
				errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
				return
			}
		}
	}

	// A test is always delivered, whatever the channel is subscribed to:
	// Enabled and Events gate real alerts, not a deliberate probe.
	ch.Enabled = true
	ch.Events = nil

	event := opsnotify.NewEvent(
		opsnotify.EventTest,
		"Test alert from Warmbly",
		"If you can read this, this channel is wired up correctly.",
		opsnotify.Field{Label: "Channel", Value: firstNonBlank(ch.Name, ch.Type)},
	)

	if err := h.OpsNotifier.Deliver(c.Request.Context(), ch, event); err != nil {
		// Never echo the raw error: a *url.Error embeds the request URL, and
		// the whole point of redacting targets is that this endpoint does not
		// hand a webhook URL back out.
		errx.JSON(c, errx.New(errx.BadRequest, "Delivery failed: "+sanitizeDeliveryError(err, ch.Target)))
		return
	}

	if h.AdminService != nil {
		h.AdminService.LogAdminAction(
			c.Request.Context(), *adminID,
			"test_notification_channel", "instance", nil,
			map[string]any{"channel_type": ch.Type, "channel_name": ch.Name},
			c.ClientIP(), c.Request.UserAgent(),
		)
	}

	c.JSON(http.StatusOK, gin.H{"delivered": true})
}

// sanitizeDeliveryError reduces a transport error to something safe to show:
// the status or cause, with any occurrence of the target stripped out.
func sanitizeDeliveryError(err error, target string) string {
	msg := err.Error()
	var uerr *url.Error
	if errors.As(err, &uerr) {
		// Keep the cause, drop the URL the stdlib prefixes onto it.
		msg = uerr.Err.Error()
	}
	if target != "" {
		msg = strings.ReplaceAll(msg, target, "the configured target")
	}
	// Belt and braces: never let a bare URL through whatever the shape.
	if i := strings.Index(msg, "http"); i >= 0 {
		msg = strings.TrimSpace(msg[:i]) + " (endpoint redacted)"
	}
	if strings.TrimSpace(msg) == "" {
		return "the endpoint could not be reached"
	}
	return msg
}

func firstNonBlank(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return "channel"
}

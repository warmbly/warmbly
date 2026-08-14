package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/errx"
)

// AdminMailStatus reports the platform mail transport and dials it.
//
// Every mature self-hosted product ships this (Mattermost's Test Connection,
// Gitea's mailer summary, Vaultwarden's /admin/test/smtp, Zulip's
// send_test_email) because a broken relay is not a degraded feature here, it
// is a lockout: login codes, password resets and invitations all go through it.
func (h *Handler) AdminMailStatus(c *gin.Context) {
	transport := h.MailTransportRef
	if transport == nil {
		c.JSON(http.StatusOK, gin.H{
			"transport": "unconfigured",
			"delivers":  false,
			"healthy":   false,
			"detail":    "No platform mail transport is configured.",
		})
		return
	}

	resp := gin.H{
		"transport": transport.Kind,
		"delivers":  transport.Delivers,
		"detail":    transport.Description,
	}

	if err := transport.Preflight(c.Request.Context()); err != nil {
		resp["healthy"] = false
		// The dialogue-level error is the whole value of this endpoint: which
		// step failed, and what the relay said.
		resp["error"] = err.Error()
	} else {
		resp["healthy"] = true
	}

	c.JSON(http.StatusOK, resp)
}

type platformTestEmailRequest struct {
	To string `json:"to" binding:"required"`
}

// AdminSendTestEmail sends a real message through the platform transport.
func (h *Handler) AdminSendTestEmail(c *gin.Context) {
	var req platformTestEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	transport := h.MailTransportRef
	if transport == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "No platform mail transport is configured."))
		return
	}

	const subject = "Warmbly test email"
	body := `<p>This is a test message from your Warmbly deployment.</p>` +
		`<p>If you are reading it in your inbox, login codes, password resets and team invitations will reach your users.</p>` +
		`<p>Transport: ` + transport.Description + `</p>`

	if err := transport.Send(c.Request.Context(), []string{req.To}, nil, nil, subject, body); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"sent":      false,
			"transport": transport.Kind,
			"error":     err.Error(),
		})
		return
	}

	sent := gin.H{"sent": true, "transport": transport.Kind}
	if !transport.Delivers {
		sent["note"] = "MAIL_TRANSPORT=log, so the message was written to the backend logs instead of being delivered."
	}
	c.JSON(http.StatusOK, sent)
}

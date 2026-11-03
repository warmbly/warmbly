package handler

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/mailpush"
)

// graphPostLimit bounds one notification POST; Graph batches but never near this.
const graphPostLimit = 1 << 20

// gmailPushLimit bounds one Pub/Sub push body; a Gmail notification is under a kilobyte.
const gmailPushLimit = 64 << 10

// MailboxPushGmail — POST /api/v1/mailbox-push/gmail
//
// A Pub/Sub push subscription delivers Gmail watch notifications here. A 2xx
// acknowledges the message; anything else makes Pub/Sub retry it with backoff.
func (h *Handler) MailboxPushGmail(c *gin.Context) {
	if h.GmailPush == nil || h.MailPushWaker == nil {
		c.Status(http.StatusNotFound)
		return
	}
	if err := h.GmailPush.Verify(c.Request.Context(), c.GetHeader("Authorization")); err != nil {
		c.Status(http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, gmailPushLimit))
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	address, ok := mailpush.GmailPushAddress(body)
	if !ok {
		// Nothing a retry would fix; acknowledge it.
		c.Status(http.StatusNoContent)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := h.MailPushWaker.WakeGmail(ctx, address); err != nil {
		log.Warn().Err(err).Msg("gmail notification could not wake the mailbox; Pub/Sub will retry")
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Status(http.StatusNoContent)
}

// MailboxPushGraph — POST /api/v1/mailbox-push/graph/:emailId
//
// Microsoft Graph posts a mailbox's change notifications here. Each item is
// authenticated by the client state issued for that mailbox. Graph marks an
// endpoint slow past 3 seconds, so the work is one wake and the answer is 202.
func (h *Handler) MailboxPushGraph(c *gin.Context) {
	// Subscription validation: echo the token as plain text within 10 seconds.
	if token := c.Query("validationToken"); token != "" {
		if len(token) > 1024 {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(token))
		return
	}
	emailID, ok := mailpush.GraphMailboxID(c.Param("emailId"))
	if !ok || !h.MailPush.GraphEnabled() || h.MailPushWaker == nil {
		// Nothing to wake; answering 2xx stops Graph retrying a channel we no longer use.
		c.Status(http.StatusAccepted)
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, graphPostLimit))
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	post, err := h.MailPush.ParseGraph(emailID, body)
	if err != nil || !post.Authentic {
		c.Status(http.StatusAccepted)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := h.MailPushWaker.WakeMailboxes(ctx, []uuid.UUID{emailID}, post.Renew); err != nil {
		log.Warn().Err(err).Str("email_id", emailID.String()).Msg("graph notification could not wake the mailbox; Graph will retry")
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Status(http.StatusAccepted)
}

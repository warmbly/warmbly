package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
)

// Unsubscribe handles the List-Unsubscribe link and the RFC 8058 one-click POST.
// It is PUBLIC + unauthenticated — mailbox providers and recipients hit it
// directly. Both GET (a recipient clicking the link) and POST (the provider's
// one-click, body "List-Unsubscribe=One-Click") suppress the recipient org-wide.
// The link shape is /unsubscribe?cid=<campaign>&rid=<contact>.
func (h *Handler) Unsubscribe(c *gin.Context) {
	isPost := c.Request.Method == http.MethodPost

	cid, err1 := uuid.Parse(c.Query("cid"))
	rid, err2 := uuid.Parse(c.Query("rid"))
	if err1 != nil || err2 != nil {
		if isPost {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Data(http.StatusBadRequest, "text/html; charset=utf-8", unsubPage("This unsubscribe link is invalid."))
		return
	}

	xerr := h.AdvancedService.Unsubscribe(c.Request.Context(), cid, rid)

	if isPost {
		// RFC 8058: acknowledge one-click. Return 5xx only on a genuine
		// server-side failure so the provider can retry; a bad/expired link
		// (BadRequest) is terminal, so 200 to stop pointless retries.
		if xerr != nil && xerr.Code != errx.BadRequest {
			c.Status(http.StatusBadGateway)
			return
		}
		c.Status(http.StatusOK)
		return
	}

	msg := "You've been unsubscribed."
	if xerr != nil {
		msg = "We couldn't process that unsubscribe link."
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", unsubPage(msg))
}

func unsubPage(msg string) []byte {
	return []byte(`<!doctype html><html lang="en"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1"><title>Unsubscribe</title></head>` +
		`<body style="font-family:system-ui,-apple-system,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem;color:#0f172a">` +
		`<h1 style="font-size:1.25rem;margin:0 0 .5rem">` + msg + `</h1>` +
		`<p style="color:#64748b;margin:0">You will no longer receive emails from this sender.</p></body></html>`)
}

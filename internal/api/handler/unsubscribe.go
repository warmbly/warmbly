package handler

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/unsublink"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// The recipient-facing unsubscribe endpoints. PUBLIC and unauthenticated by
// design: the only credential is the opaque token in the path, minted per
// recipient when the email was sent.
//
// Two token generations share the route. A short stored ticket is what is
// minted today, because the opt-out address is the one URL a recipient reads
// in full (issue #498); a self-contained signed token is what links already
// in inboxes carry, and what still ships when a ticket cannot be stored.
// unsublink.IsTicket decides which, on shape, so neither costs the other a
// lookup.
//
//   GET  /unsubscribe/:token              a click on the link: a confirm page
//   POST /unsubscribe/:token              the confirm button, or the mail
//                                         client's RFC 8058 one-click POST
//                                         (body List-Unsubscribe=One-Click),
//                                         which suppresses with no page
//   POST /unsubscribe/:token/resubscribe  the "unsubscribed by mistake" button
//
// A GET never changes anything, because link scanners and preview fetchers
// follow every link in an email; only a POST suppresses.

func (h *Handler) UnsubscribePage(c *gin.Context) {
	claims, ok := h.unsubscribeClaims(c)
	if !ok {
		return
	}
	if claims.ContactID == uuid.Nil {
		renderUnsubPage(c, http.StatusOK, unsubView{Title: "This was a test email", Body: "Test sends carry a link that is not tied to anyone, so there is nothing to unsubscribe."})
		return
	}
	renderUnsubPage(c, http.StatusOK, unsubView{
		Title:   "Unsubscribe from these emails?",
		Body:    "Confirm and you will not receive further emails from this sender.",
		Confirm: c.Request.URL.Path,
	})
}

// unsubscribeBodyLimit caps the public POST bodies. The engine-wide limit is
// registered after these routes, so it does not cover them; a one-click or
// confirm body is a few bytes.
const unsubscribeBodyLimit = 16 << 10

func (h *Handler) UnsubscribeSubmit(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, unsubscribeBodyLimit)
	oneClick := strings.EqualFold(strings.TrimSpace(c.PostForm("List-Unsubscribe")), "One-Click")
	confirmed := c.PostForm("confirm") == "1"

	claims, err := h.verifyUnsubscribeToken(c.Request.Context(), c.Param("token"))
	if err != nil {
		if oneClick {
			// RFC 8058: a bad or expired link is terminal, so 200 stops the
			// provider retrying; only a genuine server failure gets a 5xx,
			// which is what a lookup that could not run is.
			if err == errUnsubUnavailable {
				c.Status(http.StatusBadGateway)
				return
			}
			c.Status(http.StatusOK)
			return
		}
		renderUnsubPage(c, unsubStatus(err), unsubInvalid(err))
		return
	}
	if claims.ContactID == uuid.Nil {
		if oneClick {
			c.Status(http.StatusOK)
			return
		}
		renderUnsubPage(c, http.StatusOK, unsubView{Title: "This was a test email", Body: "Test sends carry a link that is not tied to anyone, so there is nothing to unsubscribe."})
		return
	}

	// A browser POST without the confirm field is not the button: show the
	// confirm page again rather than act on it.
	if !oneClick && !confirmed {
		renderUnsubPage(c, http.StatusOK, unsubView{
			Title:   "Unsubscribe from these emails?",
			Body:    "Confirm and you will not receive further emails from this sender.",
			Confirm: c.Request.URL.Path,
		})
		return
	}

	via := "link"
	if oneClick {
		via = "one_click"
	}
	xerr := h.AdvancedService.UnsubscribeFromLink(c.Request.Context(), claims.OrgID, claims.CampaignID, claims.ContactID, via)

	if oneClick {
		if xerr != nil && xerr.Code != errx.BadRequest {
			c.Status(http.StatusBadGateway)
			return
		}
		c.Status(http.StatusOK)
		return
	}
	if xerr != nil {
		renderUnsubPage(c, http.StatusOK, unsubView{Title: "We couldn't process that link", Body: "The link is no longer valid. Reply to the email instead and the sender will stop."})
		return
	}
	renderUnsubPage(c, http.StatusOK, unsubView{
		Title:       "You've been unsubscribed",
		Body:        "You will not receive further emails from this sender.",
		Resubscribe: c.Request.URL.Path + "/resubscribe",
	})
}

func (h *Handler) UnsubscribeUndo(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, unsubscribeBodyLimit)
	claims, ok := h.unsubscribeClaims(c)
	if !ok {
		return
	}
	if claims.ContactID == uuid.Nil {
		renderUnsubPage(c, http.StatusBadRequest, unsubInvalid(unsublink.ErrInvalid))
		return
	}
	if xerr := h.AdvancedService.Resubscribe(c.Request.Context(), claims.OrgID, claims.ContactID); xerr != nil {
		renderUnsubPage(c, http.StatusOK, unsubView{Title: "We couldn't process that link", Body: "The link is no longer valid. Reply to the email and the sender can add you back."})
		return
	}
	renderUnsubPage(c, http.StatusOK, unsubView{Title: "You're subscribed again", Body: "The sender can email you as before. You can unsubscribe from any later email."})
}

// errUnsubUnavailable is a ticket lookup that failed rather than a link that
// is not real. Told apart because the answers differ in both directions: the
// recipient is asked to try again instead of told their link is invalid, and
// a one-click POST gets a retryable status instead of a terminal one.
var errUnsubUnavailable = errors.New("unsubscribe link store unavailable")

func (h *Handler) verifyUnsubscribeToken(ctx context.Context, token string) (unsublink.Claims, error) {
	if unsublink.IsTicket(token) {
		if h.UnsubscribeTickets == nil {
			return unsublink.Claims{}, unsublink.ErrInvalid
		}
		t, err := h.UnsubscribeTickets.Resolve(ctx, token)
		if err != nil {
			errs.CaptureException(err)
			return unsublink.Claims{}, errUnsubUnavailable
		}
		if t == nil {
			return unsublink.Claims{}, unsublink.ErrInvalid
		}
		claims := unsublink.Claims{
			OrgID:      t.OrganizationID,
			CampaignID: t.CampaignID,
			ContactID:  t.ContactID,
			ExpiresAt:  t.ExpiresAt,
		}
		if !time.Now().Before(t.ExpiresAt) {
			return claims, unsublink.ErrExpired
		}
		return claims, nil
	}
	if h.UnsubscribeLinks == nil {
		return unsublink.Claims{}, unsublink.ErrInvalid
	}
	return h.UnsubscribeLinks.Verify(token, time.Now())
}

func (h *Handler) unsubscribeClaims(c *gin.Context) (unsublink.Claims, bool) {
	claims, err := h.verifyUnsubscribeToken(c.Request.Context(), c.Param("token"))
	if err != nil {
		renderUnsubPage(c, unsubStatus(err), unsubInvalid(err))
		return claims, false
	}
	return claims, true
}

// unsubStatus is 503 for a lookup that failed, so a recipient reloading gets
// the page rather than a cached refusal, and 400 for a link that is not real.
func unsubStatus(err error) int {
	if err == errUnsubUnavailable {
		return http.StatusServiceUnavailable
	}
	return http.StatusBadRequest
}

func unsubInvalid(err error) unsubView {
	switch err {
	case errUnsubUnavailable:
		return unsubView{Title: "Try again shortly", Body: "We could not check this link just now. Open it again in a few minutes, or reply to the email and the sender will stop."}
	case unsublink.ErrExpired:
		return unsubView{Title: "This link has expired", Body: "Reply to the email instead and the sender will stop."}
	}
	return unsubView{Title: "This unsubscribe link is invalid", Body: "Reply to the email instead and the sender will stop."}
}

type unsubView struct {
	Title       string
	Body        string
	Confirm     string // POST target of the confirm button, when shown
	Resubscribe string // POST target of the resubscribe button, when shown
}

// A neutral page: the email came from the customer's mailbox, so the page
// names no brand and carries no scripts or external assets.
var unsubTemplate = template.Must(template.New("unsubscribe").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex"><title>{{.Title}}</title>
<style>body{font-family:system-ui,-apple-system,"Segoe UI",sans-serif;max-width:32rem;margin:4rem auto;padding:0 1.25rem;color:#0f172a;line-height:1.5}
h1{font-size:1.25rem;margin:0 0 .5rem}p{color:#475569;margin:0 0 1.25rem}
button{font:inherit;padding:.55rem 1rem;border-radius:.375rem;border:1px solid #0284c7;background:#0284c7;color:#fff;cursor:pointer}
button.secondary{background:#fff;color:#0f172a;border-color:#cbd5e1}</style></head>
<body><h1>{{.Title}}</h1><p>{{.Body}}</p>
{{if .Confirm}}<form method="post" action="{{.Confirm}}"><input type="hidden" name="confirm" value="1"><button type="submit">Unsubscribe</button></form>{{end}}
{{if .Resubscribe}}<form method="post" action="{{.Resubscribe}}"><button type="submit" class="secondary">Unsubscribed by mistake? Resubscribe</button></form>{{end}}
</body></html>`))

func renderUnsubPage(c *gin.Context, status int, v unsubView) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Robots-Tag", "noindex")
	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = unsubTemplate.Execute(c.Writer, v)
}

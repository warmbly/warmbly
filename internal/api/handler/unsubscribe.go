package handler

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/brandkit"
	"github.com/warmbly/warmbly/internal/app/unsublink"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
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
	t := unsubCopyFor(c.GetHeader("Accept-Language"))
	if claims.ContactID == uuid.Nil {
		renderUnsubPage(c, http.StatusOK, unsubView{Title: t.TestTitle, Body: t.TestBody})
		return
	}
	renderUnsubPage(c, http.StatusOK, h.withUnsubBrand(c, claims.OrgID, unsubView{Title: t.ConfirmTitle, Body: t.ConfirmBody, Confirm: c.Request.URL.Path}))
}

// unsubscribeBodyLimit caps the public POST bodies. The engine-wide limit is
// registered after these routes, so it does not cover them; a one-click or
// confirm body is a few bytes.
const unsubscribeBodyLimit = 16 << 10

func (h *Handler) UnsubscribeSubmit(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, unsubscribeBodyLimit)
	oneClick := strings.EqualFold(strings.TrimSpace(c.PostForm("List-Unsubscribe")), "One-Click")
	confirmed := c.PostForm("confirm") == "1"

	t := unsubCopyFor(c.GetHeader("Accept-Language"))
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
		renderUnsubPage(c, unsubStatus(err), unsubInvalid(t, err))
		return
	}
	if claims.ContactID == uuid.Nil {
		if oneClick {
			c.Status(http.StatusOK)
			return
		}
		renderUnsubPage(c, http.StatusOK, unsubView{Title: t.TestTitle, Body: t.TestBody})
		return
	}

	// A browser POST without the confirm field is not the button: show the
	// confirm page again rather than act on it.
	if !oneClick && !confirmed {
		renderUnsubPage(c, http.StatusOK, h.withUnsubBrand(c, claims.OrgID, unsubView{Title: t.ConfirmTitle, Body: t.ConfirmBody, Confirm: c.Request.URL.Path}))
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
		renderUnsubPage(c, http.StatusOK, h.withUnsubBrand(c, claims.OrgID, unsubView{Title: t.FailedTitle, Body: t.FailedBody}))
		return
	}
	renderUnsubPage(c, http.StatusOK, h.withUnsubBrand(c, claims.OrgID, unsubView{Title: t.DoneTitle, Body: t.DoneBody, Resubscribe: c.Request.URL.Path + "/resubscribe"}))
}

func (h *Handler) UnsubscribeUndo(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, unsubscribeBodyLimit)
	claims, ok := h.unsubscribeClaims(c)
	if !ok {
		return
	}
	t := unsubCopyFor(c.GetHeader("Accept-Language"))
	if claims.ContactID == uuid.Nil {
		renderUnsubPage(c, http.StatusBadRequest, unsubInvalid(t, unsublink.ErrInvalid))
		return
	}
	if xerr := h.AdvancedService.Resubscribe(c.Request.Context(), claims.OrgID, claims.ContactID); xerr != nil {
		renderUnsubPage(c, http.StatusOK, h.withUnsubBrand(c, claims.OrgID, unsubView{Title: t.FailedTitle, Body: t.FailedResubscribeBody}))
		return
	}
	renderUnsubPage(c, http.StatusOK, h.withUnsubBrand(c, claims.OrgID, unsubView{Title: t.ResubscribedTitle, Body: t.ResubscribedBody}))
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
		renderUnsubPage(c, unsubStatus(err), unsubInvalid(unsubCopyFor(c.GetHeader("Accept-Language")), err))
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

func unsubInvalid(t unsubCopy, err error) unsubView {
	switch err {
	case errUnsubUnavailable:
		return unsubView{Title: t.RetryTitle, Body: t.RetryBody}
	case unsublink.ErrExpired:
		return unsubView{Title: t.ExpiredTitle, Body: t.ReplyBody}
	}
	return unsubView{Title: t.InvalidTitle, Body: t.ReplyBody}
}

type unsubView struct {
	Title       string
	Body        string
	Confirm     string // POST target of the confirm button, when shown
	Resubscribe string // POST target of the resubscribe button, when shown

	// Set by renderUnsubPage from the request's language.
	Lang, ConfirmButton, ResubscribeButton string
	RTL                                    bool

	// BrandName and Accent are the workspace's own brand, set only when it chose one.
	BrandName, Accent string
}

// withUnsubBrand names the sending workspace's own brand on the page; the deployment's brand never appears here.
func (h *Handler) withUnsubBrand(c *gin.Context, orgID uuid.UUID, v unsubView) unsubView {
	if orgID == uuid.Nil {
		return v
	}
	res := h.publicBrand(c.Request.Context(), orgID)
	if !res.Custom() {
		return v
	}
	v.BrandName = displayname.DisplayableOr(res.Brand.Name, "")
	if accent, ok := brandkit.NormalizeAccent(res.Brand.AccentColor); ok {
		v.Accent = accent
	}
	return v
}

// unsubLanguage picks the page's language from the browser's
// Accept-Language: the most preferred one the page is written in, English
// when none is.
func unsubLanguage(acceptLanguage string) string {
	type pref struct {
		code string
		q    float64
	}
	var prefs []pref
	for _, part := range strings.Split(acceptLanguage, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		q := 1.0
		if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			f, err := strconv.ParseFloat(v, 64)
			// A qvalue is 0 to 1; NaN fails both comparisons.
			if err != nil || !(f >= 0 && f <= 1) {
				continue
			}
			q = f
		}
		if code := unsubLanguageCode(tag); code != "" && q > 0 {
			prefs = append(prefs, pref{code, q})
		}
	}
	sort.SliceStable(prefs, func(i, j int) bool { return prefs[i].q > prefs[j].q })
	if len(prefs) > 0 {
		return prefs[0].code
	}
	return "en"
}

// unsubLanguageCode is the page language a tag asks for, or "" for one it
// has none of. Norwegian and Tagalog tags name the language written as nb and
// fil, and Taiwan, Hong Kong and Macau read Traditional Chinese.
func unsubLanguageCode(tag string) string {
	parts := strings.Split(strings.ToLower(strings.ReplaceAll(tag, "_", "-")), "-")
	code := parts[0]
	switch code {
	case "no", "nn":
		code = "nb"
	case "tl":
		code = "fil"
	case "zh":
		for _, p := range parts[1:] {
			if p == "hant" || p == "tw" || p == "hk" || p == "mo" {
				code = "zh-Hant"
			}
		}
	}
	if _, ok := unsubCopies[code]; !ok {
		return ""
	}
	return code
}

func unsubCopyFor(acceptLanguage string) unsubCopy {
	return unsubCopies[unsubLanguage(acceptLanguage)]
}

// A neutral page: the email came from the customer's mailbox, so the page
// names no brand but the workspace's own, and carries no scripts or external assets.
var unsubTemplate = template.Must(template.New("unsubscribe").Parse(`<!doctype html><html lang="{{.Lang}}"{{if .RTL}} dir="rtl"{{end}}><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex"><title>{{.Title}}</title>
<style>body{font-family:system-ui,-apple-system,"Segoe UI",sans-serif;max-width:32rem;margin:4rem auto;padding:0 1.25rem;color:#0f172a;line-height:1.5}
h1{font-size:1.25rem;margin:0 0 .5rem}p{color:#475569;margin:0 0 1.25rem}
button{font:inherit;padding:.55rem 1rem;border-radius:.375rem;border:1px solid #0284c7;background:#0284c7;color:#fff;cursor:pointer}
button.secondary{background:#fff;color:#0f172a;border-color:#cbd5e1}.brand{font-size:.8rem;font-weight:600;color:#334155;margin:0 0 1.5rem}
{{if .Accent}}button:not(.secondary){background:{{.Accent}};border-color:{{.Accent}}}{{end}}</style></head>
<body>{{if .BrandName}}<p class="brand">{{.BrandName}}</p>{{end}}<h1>{{.Title}}</h1><p>{{.Body}}</p>
{{if .Confirm}}<form method="post" action="{{.Confirm}}"><input type="hidden" name="confirm" value="1"><button type="submit">{{.ConfirmButton}}</button></form>{{end}}
{{if .Resubscribe}}<form method="post" action="{{.Resubscribe}}"><button type="submit" class="secondary">{{.ResubscribeButton}}</button></form>{{end}}
</body></html>`))

func renderUnsubPage(c *gin.Context, status int, v unsubView) {
	v.Lang = unsubLanguage(c.GetHeader("Accept-Language"))
	t := unsubCopies[v.Lang]
	v.ConfirmButton, v.ResubscribeButton, v.RTL = t.ConfirmButton, t.ResubscribeButton, t.RTL
	c.Header("Vary", "Accept-Language")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Robots-Tag", "noindex")
	c.Status(status)
	// A static page with one form that posts back to this origin. No script,
	// no images, nothing embedded, and it must not be framed: the whole page
	// is a one-click state change.
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = unsubTemplate.Execute(c.Writer, v)
}

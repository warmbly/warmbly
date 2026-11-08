package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/brandkit"
	"github.com/warmbly/warmbly/internal/app/customhost"
	"github.com/warmbly/warmbly/internal/app/portalhandoff"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
)

// The branded dashboard on an agency's verified portal domain: what it shows, and the challenge bridge its sign-in embeds.

// captchaFrameAvailable reports whether the API can render a challenge for a portal's sign-in.
func captchaFrameAvailable() bool {
	return config.CaptchaProvider() != "none" && config.TurnstileSiteKey() != ""
}

// PortalInfo serves GET /v1/auth/portal?host=: the agency brand a dashboard on a verified portal domain shows.
func (h *Handler) PortalInfo(c *gin.Context) {
	host := customhost.Normalize(c.Query("host"))
	view, err := h.BrandService.PortalView(c.Request.Context(), host, captchaFrameAvailable())
	if err != nil {
		// A failed lookup is not an answer: nothing may cache it as "not a portal".
		log.Error().Err(err).Msg("portal lookup failed")
		errx.JSON(c, errx.InternalError())
		return
	}
	if view == nil {
		view = &brandkit.PortalView{}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, view)
}

// InternalGetPortal answers the portal service: 200 when host is a verified agency portal domain, else 404.
func (h *Handler) InternalGetPortal(c *gin.Context) {
	portals := h.BrandService.Portals()
	if portals == nil {
		c.Status(http.StatusNotFound)
		return
	}
	p, err := portals.Lookup(c.Request.Context(), c.Param("host"))
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if p == nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, gin.H{"host": p.Host, "agency_id": p.AgencyID})
}

// CaptchaFrame serves GET /v1/auth/captcha-frame?origin=: the challenge, rendered on the API's own host, framed only by a verified portal origin.
func (h *Handler) CaptchaFrame(c *gin.Context) {
	origin := config.PortalOrigin(c.Query("origin"))
	if origin == "" || !captchaFrameAvailable() {
		c.Data(http.StatusBadRequest, "text/plain; charset=utf-8", []byte("This page can only be opened from a verified portal domain."))
		return
	}
	nonceRaw := make([]byte, 18)
	if _, err := rand.Read(nonceRaw); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	nonce := base64.StdEncoding.EncodeToString(nonceRaw)
	h2 := c.Writer.Header()
	// The one framable page on the API: framed by exactly the portal it was asked for.
	h2.Del("X-Frame-Options")
	h2.Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"' https://challenges.cloudflare.com; "+
		"style-src 'nonce-"+nonce+"'; frame-src https://challenges.cloudflare.com; connect-src https://challenges.cloudflare.com; "+
		"frame-ancestors "+origin+"; base-uri 'none'; form-action 'none'")
	h2.Set("Cache-Control", "no-store")
	originJS, _ := json.Marshal(origin)
	keyJS, _ := json.Marshal(config.TurnstileSiteKey())
	page := `<!doctype html><html><head><meta charset="utf-8"><meta name="robots" content="noindex"><title>Security check</title>
<style nonce="` + nonce + `">html,body{margin:0;padding:0;background:transparent;overflow:hidden}</style></head>
<body><div id="w"></div>
<script nonce="` + nonce + `">
(function(){
var O=` + string(originJS) + `,K=` + string(keyJS) + `,id=null;
function post(t,e){var m={type:"warmbly:captcha",token:t||""};if(e)m.error=true;window.parent.postMessage(m,O);}
window.warmblyCaptchaReady=function(){id=window.turnstile.render("#w",{sitekey:K,callback:function(t){post(t);},"expired-callback":function(){post("");},"error-callback":function(){post("",true);}});};
window.addEventListener("message",function(e){if(e.origin===O&&e.source===window.parent&&e.data&&e.data.type==="warmbly:captcha-reset"&&id!==null){window.turnstile.reset(id);}});
})();
</script>
<script nonce="` + nonce + `" src="https://challenges.cloudflare.com/turnstile/v0/api.js?onload=warmblyCaptchaReady&render=explicit" async defer></script>
</body></html>`
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(page))
}

// emailLinkOrigin is the request's dashboard origin for a link mailed about orgID: a portal origin only when it serves that workspace.
func (h *Handler) emailLinkOrigin(ctx context.Context, orgID uuid.UUID) string {
	origin := config.DashboardOriginFromContext(ctx)
	if config.PortalOrigin(origin) == "" {
		return origin
	}
	portals := h.BrandService.Portals()
	if portals == nil {
		return ""
	}
	p, err := portals.Lookup(ctx, origin[len("https://"):])
	if err != nil || p == nil || h.CustomHosts == nil {
		return ""
	}
	if ok, err := h.CustomHosts.Serves(ctx, p.Host, orgID); err != nil || !ok {
		return ""
	}
	return origin
}

type portalHandoffRequest struct {
	Host string `json:"host"`
	// EndSession ends this session once the code exists, for a sign-in made only to reach the portal.
	EndSession bool `json:"end_session"`
}

// PortalHandoff serves POST /v1/auth/portal/handoff: a one-time code that signs this account in to one verified portal.
func (h *Handler) PortalHandoff(c *gin.Context) {
	if h.PortalHandoffs == nil {
		errx.JSON(c, portalhandoff.ErrPortalNotFound)
		return
	}
	var req portalHandoffRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	out, xerr := h.PortalHandoffs.Issue(c.Request.Context(), portalhandoff.Request{
		Session:     middleware.GetSession(c),
		AccessToken: middleware.GetAccessToken(c),
		Host:        req.Host,
		EndSession:  req.EndSession,
		Origin:      c.GetHeader("Origin"),
	})
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, out)
}

type portalExchangeRequest struct {
	Code string `json:"code"`
}

// PortalExchange serves POST /v1/auth/portal/exchange: the portal trades its code, once, for a portal session.
func (h *Handler) PortalExchange(c *gin.Context) {
	if h.PortalHandoffs == nil {
		errx.JSON(c, portalhandoff.ErrHandoffInvalid)
		return
	}
	var req portalExchangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	tok, xerr := h.PortalHandoffs.Exchange(c.Request.Context(), req.Code, c.GetHeader("Origin"), c.ClientIP(), c.Request.UserAgent())
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, tok)
}

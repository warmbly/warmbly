package handler

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
)

// OIDCBegin starts a generic OpenID Connect authorization.
//
// It returns the URL rather than issuing a 302 so the dashboard, which is a
// single-page app on a different origin from the API, can navigate to it
// itself. The state, nonce and PKCE verifier are stored server-side and never
// leave the backend.
func (h *Handler) OIDCBegin(c *gin.Context) {
	redirect, err := h.AuthService.OIDCBegin(c.Request.Context())
	if err != nil {
		errx.Handle(c, err)
		return
	}
	c.JSON(http.StatusOK, redirect)
}

// OIDCCallback is where the provider sends the browser back.
//
// It answers with a redirect, not JSON: a person is looking at this response.
// The session itself is held server-side behind a single-use handoff code that
// the dashboard immediately exchanges, so no token ever appears in a URL,
// browser history, Referer header or proxy log.
func (h *Handler) OIDCCallback(c *gin.Context) {
	base := config.AppBaseURL()

	// A provider-side refusal arrives as ?error=, not as a failed exchange.
	if e := c.Query("error"); e != "" {
		c.Redirect(http.StatusFound, base+"/auth/login?sso_error="+url.QueryEscape(e))
		return
	}

	handoff, err := h.AuthService.OIDCCallback(
		c.Request.Context(),
		c.Query("code"),
		c.Query("state"),
		c.ClientIP(),
		c.Request.UserAgent(),
	)
	if err != nil {
		c.Redirect(http.StatusFound, base+"/auth/login?sso_error="+url.QueryEscape(err.Message))
		return
	}

	c.Redirect(http.StatusFound, base+"/auth/sso?code="+url.QueryEscape(handoff))
}

type oidcExchangeRequest struct {
	Code string `json:"code"`
}

// OIDCExchange swaps the handoff code for the session. Single use.
func (h *Handler) OIDCExchange(c *gin.Context) {
	var req oidcExchangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	result, err := h.AuthService.OIDCExchange(c.Request.Context(), req.Code)
	if err != nil {
		errx.Handle(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

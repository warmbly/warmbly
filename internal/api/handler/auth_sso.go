package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/app/auth"
	"github.com/warmbly/warmbly/internal/app/delegation"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Browser sign-in for generic OIDC, Google and Apple: begin returns the
// authorization URL (the dashboard is on a different origin, so it navigates
// itself), the callback answers with a redirect carrying a single-use handoff
// code, and the code is exchanged for the session over POST so no token appears
// in a URL, history, Referer or proxy log.

// OIDCBegin starts a generic OpenID Connect authorization.
func (h *Handler) OIDCBegin(c *gin.Context) {
	h.ssoBegin(c, models.IdentityProviderOIDC)
}

// GoogleBegin starts a Sign in with Google authorization.
func (h *Handler) GoogleBegin(c *gin.Context) {
	h.ssoBegin(c, models.IdentityProviderGoogle)
}

// AppleBegin starts a Sign in with Apple authorization.
func (h *Handler) AppleBegin(c *gin.Context) {
	h.ssoBegin(c, models.IdentityProviderApple)
}

func (h *Handler) ssoBegin(c *gin.Context, provider string) {
	redirect, err := h.AuthService.SSOBegin(c.Request.Context(), provider)
	if err != nil {
		errx.Handle(c, err)
		return
	}
	c.JSON(http.StatusOK, redirect)
}

// OIDCCallback is where an OpenID Connect provider sends the browser back.
func (h *Handler) OIDCCallback(c *gin.Context) {
	h.ssoCallback(c, auth.SSOCallback{
		Provider: models.IdentityProviderOIDC,
		Code:     c.Query("code"),
		State:    c.Query("state"),
	})
}

// GoogleCallback is where Google sends the browser back. An administrator's
// sign-in proving a Workspace domain shares this client and redirect, and is
// handed to the dashboard window that opened it rather than signing anyone in.
func (h *Handler) GoogleCallback(c *gin.Context) {
	if strings.HasPrefix(c.Query("state"), delegation.GoogleStatePrefix) {
		h.renderOAuthCallback(c, "google")
		return
	}
	h.ssoCallback(c, auth.SSOCallback{
		Provider: models.IdentityProviderGoogle,
		Code:     c.Query("code"),
		State:    c.Query("state"),
	})
}

// appleCallbackUser is the name payload Apple posts alongside the code. Apple
// sends it once, on first authorization, and never inside the ID token.
type appleCallbackUser struct {
	Name struct {
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
	} `json:"name"`
}

// AppleCallback is where Apple sends the browser back. Requesting any scope
// forces response_mode=form_post, so it arrives as a cross-site POST; nothing
// here reads a cookie, so that hop carries no state of its own.
func (h *Handler) AppleCallback(c *gin.Context) {
	in := auth.SSOCallback{
		Provider: models.IdentityProviderApple,
		Code:     formOrQuery(c, "code"),
		State:    formOrQuery(c, "state"),
	}
	if raw := formOrQuery(c, "user"); raw != "" {
		var u appleCallbackUser
		if err := json.Unmarshal([]byte(raw), &u); err == nil {
			in.FirstName, in.LastName = u.Name.FirstName, u.Name.LastName
		}
	}
	h.ssoCallback(c, in)
}

func (h *Handler) ssoCallback(c *gin.Context, in auth.SSOCallback) {
	// Read before the state is consumed; only a verified portal origin replaces APP_URL.
	base := config.AppBaseURL()
	if origin := h.AuthService.SSOReturnOrigin(c.Request.Context(), in.State); origin != "" {
		base = origin
	}

	// A provider-side refusal arrives as error=, not as a failed exchange.
	// A closed consent screen is not a failure, so it returns quietly.
	if e := formOrQuery(c, "error"); e != "" {
		cancelled := e == "access_denied" || e == "user_cancelled_authorize"
		if auth.IsReauthState(in.State) {
			if cancelled {
				c.Redirect(http.StatusFound, base+"/reauth/complete?cancelled=1")
				return
			}
			c.Redirect(http.StatusFound, base+"/reauth/complete?error="+url.QueryEscape(e))
			return
		}
		if cancelled {
			c.Redirect(http.StatusFound, base+"/auth/login")
			return
		}
		c.Redirect(http.StatusFound, base+"/auth/login?sso_error="+url.QueryEscape(e))
		return
	}

	in.IPAddress = c.ClientIP()
	in.UserAgent = c.Request.UserAgent()

	res, err := h.AuthService.SSOCallbackComplete(c.Request.Context(), in)
	// A re-authentication runs in a window the dashboard opened, so it lands
	// on the page that reports back to that window, never on sign-in.
	if res != nil && res.Reauth {
		if err != nil {
			c.Redirect(http.StatusFound, base+"/reauth/complete?error="+url.QueryEscape(err.Message))
			return
		}
		c.Redirect(http.StatusFound, base+"/reauth/complete?code="+url.QueryEscape(res.Code))
		return
	}
	if err != nil {
		c.Redirect(http.StatusFound, base+"/auth/login?sso_error="+url.QueryEscape(err.Message))
		return
	}

	c.Redirect(http.StatusFound, base+"/auth/sso?code="+url.QueryEscape(res.Code))
}

// formOrQuery reads a parameter from either the posted form or the query
// string, because the same callback serves both response modes.
func formOrQuery(c *gin.Context, key string) string {
	if v := c.PostForm(key); v != "" {
		return v
	}
	return c.Query(key)
}

type ssoExchangeRequest struct {
	Code string `json:"code"`
	// Binding is the secret /begin handed this browser. It is what stops a
	// forwarded handoff link from signing someone into another person's
	// workspace.
	Binding string `json:"binding"`
}

// SSOExchange swaps the handoff code for the session. Single use.
func (h *Handler) SSOExchange(c *gin.Context) {
	var req ssoExchangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}

	result, err := h.AuthService.SSOExchange(c.Request.Context(), req.Code, req.Binding)
	if err != nil {
		errx.Handle(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// SSOLink takes the password for a federated sign-in that resolved to an
// existing password account, attaches the identity and issues the session.
// Public like /2fa/verify: the pending token is the proof of the completed
// provider flow, and the password budget is enforced in the service.
func (h *Handler) SSOLink(c *gin.Context) {
	var data auth.SSOLinkData
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), authRequestTimeout)
	defer cancel()

	result, err := h.AuthService.SSOLinkConfirm(ctx, &data, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		errx.Handle(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

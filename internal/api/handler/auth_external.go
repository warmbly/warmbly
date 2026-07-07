package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/errx"
)

type appleTokenLoginRequest struct {
	IdentityToken string `json:"identity_token" binding:"required"`
	// Apple shares the user's name only with the app (never inside the
	// identity token), so the client forwards it for first-sign-in prefill.
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// AppleTokenLogin exchanges a native Sign in with Apple identity token for a
// session. Public like the passkey routes: the Apple-signed token is the
// protection, and first sign-in provisions the account.
func (h *Handler) AppleTokenLogin(c *gin.Context) {
	var data appleTokenLoginRequest
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), authRequestTimeout)
	defer cancel()

	session, err := h.AuthService.AppleIDTokenAuth(ctx, data.IdentityToken, data.FirstName, data.LastName, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, session)
}

type googleTokenLoginRequest struct {
	IDToken string `json:"id_token" binding:"required"`
}

// GoogleTokenLogin exchanges a native Google Sign-In ID token for a session.
func (h *Handler) GoogleTokenLogin(c *gin.Context) {
	var data googleTokenLoginRequest
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), authRequestTimeout)
	defer cancel()

	session, err := h.AuthService.GoogleIDTokenAuth(ctx, data.IDToken, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, session)
}

// AuthProviders advertises which social sign-in options this deployment
// supports, so the one shipped app binary adapts to hosted and self-hosted
// backends without a rebuild. Client IDs are public identifiers.
func (h *Handler) AuthProviders(c *gin.Context) {
	type provider struct {
		Enabled  bool   `json:"enabled"`
		ClientID string `json:"client_id,omitempty"`
	}
	c.JSON(http.StatusOK, gin.H{
		"apple": provider{
			Enabled:  h.ExternalAuthProviders.AppleBundleID != "",
			ClientID: h.ExternalAuthProviders.AppleBundleID,
		},
		"google": provider{
			Enabled:  h.ExternalAuthProviders.GoogleIOSClientID != "",
			ClientID: h.ExternalAuthProviders.GoogleIOSClientID,
		},
	})
}

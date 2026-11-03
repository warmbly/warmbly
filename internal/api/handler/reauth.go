package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/app/twofa"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
)

type reauthRequest struct {
	// Password is the account password. Optional for an account that has none
	// (passkey or SSO only), which confirms with Code instead.
	Password string `json:"password"`
	// Code is a current TOTP or recovery code.
	Code string `json:"code"`
}

type reauthResponse struct {
	// ValidForSeconds is how long the confirmation lasts, so the client can
	// decide whether to ask again rather than letting the next call fail.
	ValidForSeconds int `json:"valid_for_seconds"`
}

// Reauth re-proves the account holder behind a live session.
//
// Sensitive changes (minting an API key, registering a passkey, transferring a
// workspace, scheduling a deletion) require a recent confirmation; this is
// where it is given. Either factor is accepted: an account with no password
// confirms with its authenticator, and an account with no 2FA confirms with its
// password.
func (h *Handler) Reauth(c *gin.Context) {
	session := middleware.GetSession(c)
	if session == nil {
		errx.Handle(c, errx.ErrUnauthorized)
		return
	}
	uid, err := uuid.Parse(middleware.GetUserID(c))
	if err != nil {
		errx.Handle(c, errx.ErrUnauthorized)
		return
	}

	var req reauthRequest
	if berr := c.ShouldBindJSON(&req); berr != nil {
		errx.Handle(c, errx.InvalidBody(berr))
		return
	}

	// An account created through Google, Apple or SSO has no password, and
	// until it enrols 2FA it has no code either. Accepting the live session
	// instead would turn a stolen token into a permanent API key, so point it
	// at what it does have rather than refusing a credential it lacks.
	if m := h.reauthMethods(c, uid); !m.Password && !m.TOTP {
		// Confirm password absence before reporting that no factor exists.
		hash, xerr := h.AuthService.PasswordHashFor(c.Request.Context(), uid)
		if xerr != nil {
			errx.Handle(c, xerr)
			return
		}
		if hash == "" {
			errx.Handle(c, errReauthNoFactor(m.any()))
			return
		}
	}

	if req.Password == "" && req.Code == "" {
		errx.Handle(c, errx.New(errx.BadRequest, "provide your password or a two-factor code"))
		return
	}

	// This endpoint checks a password, so it is a second place to guess one.
	// Refused before the hash comparison, so a caller past the budget cannot
	// measure Argon2's timing either.
	ctx := c.Request.Context()
	ok, xerr := h.AuthService.ReserveReauthAttempt(ctx, uid)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	if !ok {
		errx.Handle(c, errx.NewWithIdentifier(errx.BadRequest, "reauth_limited",
			"Too many confirmation attempts. Wait an hour, or sign out and sign in again."))
		return
	}

	// The reserved attempt stays charged on a miss.
	valid, xerr := h.reauthProofValid(c, uid, req)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	if !valid {
		// One message for both factors: which one matched is not something an
		// attacker holding a token should learn here.
		errx.Handle(c, errx.ErrCredentials)
		return
	}
	h.AuthService.ClearReauthFailures(ctx, uid)

	h.stampReauth(c, session)
}

// stampReauth records the confirmation on the session and answers the window.
func (h *Handler) stampReauth(c *gin.Context, session *models.Session) {
	if xerr := h.TokenService.StampReauth(c.Request.Context(), session.ID); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, reauthResponse{ValidForSeconds: int(token.ReauthWindow.Seconds())})
}

// errReauthNoFactor answers a password or code for an account that has
// neither. hasOther means it can confirm with a passkey or a linked identity.
func errReauthNoFactor(hasOther bool) *errx.Error {
	if hasOther {
		return errx.NewWithIdentifier(errx.BadRequest, "reauth_no_factor",
			"This account has no password or authenticator code. Confirm with your passkey or the account you sign in with.")
	}
	return errx.NewWithIdentifier(errx.BadRequest, "reauth_no_factor",
		"This account has no password and no two-factor authentication, so there is nothing to confirm with. "+
			"Sign out and sign in again, then repeat the action within five minutes of signing in. Turning on two-factor authentication under Settings > Security gives the account a way to confirm without signing in again.")
}

// allowEnrollment gates adding an authenticator, which /auth/reauth then
// accepts. See enrollmentGate.
func (h *Handler) allowEnrollment(c *gin.Context, uid uuid.UUID) bool {
	session := middleware.GetSession(c)
	if session == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return false
	}
	if xerr := enrollmentGate(session, time.Now(), func() bool { return h.reauthMethods(c, uid).any() }); xerr != nil {
		errx.JSON(c, xerr)
		return false
	}
	return true
}

// enrollmentGate passes a recent reauth; otherwise an account with a factor
// must confirm, and one with none passes only on a session signed in within
// the reauth window, since a fresh sign-in is the only proof it can give.
func enrollmentGate(session *models.Session, now time.Time, hasFactor func() bool) *errx.Error {
	if session.ReauthAt != nil && now.Sub(*session.ReauthAt) <= token.ReauthWindow {
		return nil
	}
	if hasFactor() {
		return errx.NewWithIdentifier(errx.Forbidden, "reauth_required",
			"Confirm it is you before making this change.")
	}
	if now.Sub(session.CreatedAt) <= token.ReauthWindow {
		return nil
	}
	return errReauthNoFactor(false)
}

// reauthProvider is one linked identity the browser can re-verify with.
type reauthProvider struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// reauthMethodsResponse is every way this account can confirm it is the
// holder, so a client offers only those.
type reauthMethodsResponse struct {
	Password bool `json:"password"`
	TOTP     bool `json:"totp"`
	Passkey  bool `json:"passkey"`
	// Providers run in a browser window; NativeProviders take an ID token
	// from the native app.
	Providers       []reauthProvider `json:"providers"`
	NativeProviders []string         `json:"native_providers"`
	ValidForSeconds int              `json:"valid_for_seconds"`
}

func (m reauthMethodsResponse) any() bool {
	return m.Password || m.TOTP || m.Passkey || len(m.Providers) > 0 || len(m.NativeProviders) > 0
}

// reauthMethods resolves what the account can confirm with. A lookup that
// fails leaves that method out rather than failing the whole answer.
func (h *Handler) reauthMethods(c *gin.Context, uid uuid.UUID) reauthMethodsResponse {
	ctx := c.Request.Context()
	m := reauthMethodsResponse{
		Providers:       []reauthProvider{},
		NativeProviders: []string{},
		ValidForSeconds: int(token.ReauthWindow.Seconds()),
	}

	if h.TwoFAService != nil {
		if enabled, err := h.TwoFAService.IsEnabled(ctx, uid); err == nil && enabled {
			m.TOTP = true
		}
	}
	if hash, xerr := h.AuthService.PasswordHashFor(ctx, uid); xerr == nil && hash != "" {
		m.Password = true
	}
	if h.PasskeyService != nil && h.PasskeysUsable {
		if has, xerr := h.PasskeyService.HasCredentials(ctx, uid); xerr == nil && has {
			m.Passkey = true
		}
	}
	if browser, native, xerr := h.AuthService.ReauthIdentities(ctx, uid); xerr == nil {
		labels := h.AuthService.FederatedProviderLabels()
		for _, id := range browser {
			m.Providers = append(m.Providers, reauthProvider{ID: id, Label: labels[id]})
		}
		m.NativeProviders = append(m.NativeProviders, native...)
	}
	return m
}

// sessionUser is the session and user id every re-authentication step acts on.
func sessionUser(c *gin.Context) (*models.Session, uuid.UUID, bool) {
	session := middleware.GetSession(c)
	if session == nil {
		errx.Handle(c, errx.ErrUnauthorized)
		return nil, uuid.Nil, false
	}
	uid, err := uuid.Parse(middleware.GetUserID(c))
	if err != nil || uid != session.UserID {
		errx.Handle(c, errx.ErrUnauthorized)
		return nil, uuid.Nil, false
	}
	return session, uid, true
}

// ReauthMethods lists the ways the signed-in account can confirm it is you.
func (h *Handler) ReauthMethods(c *gin.Context) {
	_, uid, ok := sessionUser(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, h.reauthMethods(c, uid))
}

// ReauthPasskeyBegin asks for one of the account's own passkeys.
func (h *Handler) ReauthPasskeyBegin(c *gin.Context) {
	session, uid, ok := sessionUser(c)
	if !ok {
		return
	}
	if h.PasskeyService == nil || !h.PasskeysUsable {
		errx.Handle(c, errx.ErrPasskeyNone)
		return
	}
	options, xerr := h.PasskeyService.BeginReauth(c.Request.Context(), uid, session.ID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"options": options})
}

type reauthPasskeyFinishRequest struct {
	Credential json.RawMessage `json:"credential"`
}

// ReauthPasskeyFinish verifies the assertion and stamps the session.
func (h *Handler) ReauthPasskeyFinish(c *gin.Context) {
	session, uid, ok := sessionUser(c)
	if !ok {
		return
	}
	if h.PasskeyService == nil {
		errx.Handle(c, errx.ErrPasskeyNone)
		return
	}
	var req reauthPasskeyFinishRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Credential) == 0 {
		errx.Handle(c, errx.ErrPasskey)
		return
	}
	if xerr := h.PasskeyService.FinishReauth(c.Request.Context(), uid, session.ID, req.Credential); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.stampReauth(c, session)
}

type reauthSSOBeginRequest struct {
	Provider string `json:"provider" binding:"required"`
}

// ReauthSSOBegin starts a provider authorization for the account's linked
// identity. The client opens the URL in a window and keeps the binding.
func (h *Handler) ReauthSSOBegin(c *gin.Context) {
	session, uid, ok := sessionUser(c)
	if !ok {
		return
	}
	var req reauthSSOBeginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	redirect, xerr := h.AuthService.ReauthSSOBegin(c.Request.Context(), req.Provider, uid, session.ID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, redirect)
}

type reauthSSOFinishRequest struct {
	Code    string `json:"code"`
	Binding string `json:"binding"`
}

// ReauthSSOFinish redeems the callback's code with the binding from begin.
func (h *Handler) ReauthSSOFinish(c *gin.Context) {
	session, uid, ok := sessionUser(c)
	if !ok {
		return
	}
	var req reauthSSOFinishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	if xerr := h.AuthService.ReauthSSOFinish(c.Request.Context(), req.Code, req.Binding, uid, session.ID); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.stampReauth(c, session)
}

// ReauthNativeBegin returns the nonce the app passes to Apple or Google.
func (h *Handler) ReauthNativeBegin(c *gin.Context) {
	session, _, ok := sessionUser(c)
	if !ok {
		return
	}
	nonce, xerr := h.AuthService.ReauthNativeBegin(c.Request.Context(), session.ID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"nonce": nonce})
}

type reauthNativeRequest struct {
	Provider string `json:"provider" binding:"required"`
	IDToken  string `json:"id_token" binding:"required"`
}

// ReauthNative accepts a native Apple or Google ID token carrying the nonce.
func (h *Handler) ReauthNative(c *gin.Context) {
	session, uid, ok := sessionUser(c)
	if !ok {
		return
	}
	var req reauthNativeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), authRequestTimeout)
	defer cancel()
	if xerr := h.AuthService.ReauthNativeVerify(ctx, req.Provider, req.IDToken, uid, session.ID); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.stampReauth(c, session)
}

// reauthProofValid checks whichever factor the caller offered.
func (h *Handler) reauthProofValid(c *gin.Context, uid uuid.UUID, req reauthRequest) (bool, *errx.Error) {
	ctx := c.Request.Context()
	var codeErr *errx.Error

	if req.Code != "" && h.TwoFAService != nil {
		valid, xerr := h.TwoFAService.VerifyCurrentCode(ctx, uid, req.Code)
		if valid || xerr != nil && xerr.Identifier != twofa.UsedCodeID {
			return valid, xerr
		}
		codeErr = xerr
	}

	if req.Password != "" {
		hash, xerr := h.AuthService.PasswordHashFor(ctx, uid)
		if xerr != nil {
			return false, xerr
		}
		if hash == "" {
			return false, codeErr
		}
		ok, verr := argon2.Verify(req.Password, hash)
		if verr == nil && ok {
			return true, nil
		}
	}

	return false, codeErr
}

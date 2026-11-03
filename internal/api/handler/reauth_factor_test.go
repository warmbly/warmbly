package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/auth"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/app/twofa"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
)

type reauthFactorStub struct {
	twofa.Service
	valid bool
	xerr  *errx.Error
}

func (s reauthFactorStub) VerifyCurrentCode(context.Context, uuid.UUID, string) (bool, *errx.Error) {
	return s.valid, s.xerr
}

func TestReauthPreservesAuthenticatorVerificationOutcome(t *testing.T) {
	for _, tc := range []struct {
		name  string
		valid bool
		xerr  *errx.Error
	}{
		{name: "accepted", valid: true},
		{name: "mismatch"},
		{name: "unavailable", xerr: errx.InternalError()},
		{name: "used", xerr: errx.NewWithIdentifier(errx.BadRequest, twofa.UsedCodeID, "Wait for the next code.")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{TwoFAService: reauthFactorStub{valid: tc.valid, xerr: tc.xerr}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/auth/reauth", nil)
			valid, xerr := h.reauthProofValid(c, uuid.New(), reauthRequest{Code: "123456"})
			if valid != tc.valid || xerr != tc.xerr {
				t.Fatalf("accepted=%t error=%v; want accepted=%t error=%v", valid, xerr, tc.valid, tc.xerr)
			}
		})
	}
}

type reauthHTTPAuth struct {
	auth.AuthService
	cleared bool
	hash    string
	hashErr *errx.Error
}

func (a *reauthHTTPAuth) PasswordHashFor(context.Context, uuid.UUID) (string, *errx.Error) {
	return a.hash, a.hashErr
}
func (*reauthHTTPAuth) ReauthIdentities(context.Context, uuid.UUID) ([]string, []string, *errx.Error) {
	return nil, nil, nil
}
func (*reauthHTTPAuth) FederatedProviderLabels() map[string]string { return nil }
func (*reauthHTTPAuth) ReserveReauthAttempt(context.Context, uuid.UUID) (bool, *errx.Error) {
	return true, nil
}
func (a *reauthHTTPAuth) ClearReauthFailures(context.Context, uuid.UUID) { a.cleared = true }

func (reauthFactorStub) IsEnabled(context.Context, uuid.UUID) (bool, error) { return true, nil }

type reauthHTTPToken struct {
	token.TokenService
	stamped bool
}

func (s *reauthHTTPToken) StampReauth(context.Context, uuid.UUID) *errx.Error {
	s.stamped = true
	return nil
}

func TestReauthHTTPPreservesVerificationFailureWithoutStampingSession(t *testing.T) {
	for _, tc := range []struct {
		name   string
		xerr   *errx.Error
		status int
		code   string
	}{
		{name: "used", xerr: errx.NewWithIdentifier(errx.BadRequest, twofa.UsedCodeID, "Wait for a new authenticator code, then try again."), status: http.StatusBadRequest, code: twofa.UsedCodeID},
		{name: "unavailable", xerr: errx.InternalError(), status: http.StatusInternalServerError, code: "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &reauthHTTPAuth{}
			tokens := &reauthHTTPToken{}
			h := &Handler{AuthService: a, TokenService: tokens, TwoFAService: reauthFactorStub{xerr: tc.xerr}}
			r := gin.New()
			uid := uuid.New()
			r.Use(func(c *gin.Context) {
				c.Set(middleware.UserIDKey, uid.String())
				c.Set(middleware.SessionKey, &models.Session{ID: uuid.New(), UserID: uid})
			})
			r.POST("/v1/auth/reauth", h.Reauth)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/auth/reauth", strings.NewReader(`{"code":"123456"}`))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			var body struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.status || body.Code != tc.code || tokens.stamped || a.cleared {
				t.Fatalf("status=%d body=%s stamped=%t cleared=%t", w.Code, w.Body, tokens.stamped, a.cleared)
			}
		})
	}
}

func TestReauthUsedCodeRetainsPasswordFallback(t *testing.T) {
	hash, err := argon2.Hash("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	used := errx.NewWithIdentifier(errx.BadRequest, twofa.UsedCodeID, "Wait for a new authenticator code.")
	for _, password := range []string{"", "incorrect-password", "correct-password"} {
		t.Run(password, func(t *testing.T) {
			h := &Handler{AuthService: &reauthHTTPAuth{hash: hash}, TwoFAService: reauthFactorStub{xerr: used}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/auth/reauth", nil)
			valid, xerr := h.reauthProofValid(c, uuid.New(), reauthRequest{Code: "123456", Password: password})
			if password == "correct-password" {
				if !valid || xerr != nil {
					t.Fatalf("password fallback: accepted=%t err=%v", valid, xerr)
				}
			} else if valid || xerr != used {
				t.Fatalf("used code: accepted=%t err=%v", valid, xerr)
			}
		})
	}
}

func TestReauthPropagatesPasswordHashLookupFailure(t *testing.T) {
	lookup := errx.InternalError()
	h := &Handler{AuthService: &reauthHTTPAuth{hashErr: lookup}, TwoFAService: reauthFactorStub{}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/auth/reauth", nil)
	valid, xerr := h.reauthProofValid(c, uuid.New(), reauthRequest{Password: "correct-password"})
	if valid || xerr != lookup {
		t.Fatalf("accepted=%t error=%v; want accepted=false error=%v", valid, xerr, lookup)
	}
}

func TestReauthHTTPStampsSessionOnValidProof(t *testing.T) {
	a := &reauthHTTPAuth{}
	tokens := &reauthHTTPToken{}
	h := &Handler{AuthService: a, TokenService: tokens, TwoFAService: reauthFactorStub{valid: true}}
	r := gin.New()
	uid := uuid.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.UserIDKey, uid.String())
		c.Set(middleware.SessionKey, &models.Session{ID: uuid.New(), UserID: uid})
	})
	r.POST("/v1/auth/reauth", h.Reauth)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/reauth", strings.NewReader(`{"code":"123456"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !tokens.stamped || !a.cleared {
		t.Fatalf("status=%d stamped=%t cleared=%t", w.Code, tokens.stamped, a.cleared)
	}
}

func TestReauthHTTPFallsBackToPasswordAfterUsedCode(t *testing.T) {
	hash, err := argon2.Hash("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	a := &reauthHTTPAuth{hash: hash}
	tokens := &reauthHTTPToken{}
	used := errx.NewWithIdentifier(errx.BadRequest, twofa.UsedCodeID, "Wait for a new authenticator code, then try again.")
	h := &Handler{AuthService: a, TokenService: tokens, TwoFAService: reauthFactorStub{xerr: used}}
	r := gin.New()
	uid := uuid.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.UserIDKey, uid.String())
		c.Set(middleware.SessionKey, &models.Session{ID: uuid.New(), UserID: uid})
	})
	r.POST("/v1/auth/reauth", h.Reauth)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/reauth", strings.NewReader(`{"code":"123456","password":"correct-password"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !tokens.stamped || !a.cleared {
		t.Fatalf("status=%d stamped=%t cleared=%t", w.Code, tokens.stamped, a.cleared)
	}
}

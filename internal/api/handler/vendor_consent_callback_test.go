package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type vendorConsentService struct {
	email.EmailService
	code, state, providerErr string
	res                      *email.VendorConsentResult
	xerr                     *errx.Error
}

func (s *vendorConsentService) FinishVendorConsent(_ context.Context, code, state, providerErr string, authorizeFor func(string) email.FinishAuthorizer) (*email.VendorConsentResult, *errx.Error) {
	s.code, s.state, s.providerErr = code, state, providerErr
	if authorizeFor == nil {
		return nil, errx.ErrForbidden
	}
	return s.res, s.xerr
}

func vendorCallback(t *testing.T, svc email.EmailService, query string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/addresses/google/callback?"+query, nil)
	c.Set("request_id", "req-42")
	(&Handler{EmailService: svc}).renderOAuthCallback(c, "gmail")
	return w
}

func TestVendorConsentCallbackFinishesServerSide(t *testing.T) {
	svc := &vendorConsentService{res: &email.VendorConsentResult{Account: &models.Email{ID: uuid.New(), Email: "ava@acme.io"}, ActorID: uuid.New()}}
	w := vendorCallback(t, svc, "code=the-code&state=vc.nonce")
	body := w.Body.String()
	if w.Code != http.StatusOK || svc.code != "the-code" || svc.state != "vc.nonce" || !strings.Contains(body, "Connected") {
		t.Fatalf("callback = %d %s", w.Code, body)
	}
	// The code is redeemed here, never handed to a page script or another origin.
	if strings.Contains(body, "the-code") || strings.Contains(body, "<script") || strings.Contains(body, "postMessage") {
		t.Fatalf("the vendor callback page carries the code or a script: %s", body)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("the vendor callback must not leak through referrers or caches")
	}
}

func TestVendorConsentCallbackRefusesWithAFixedReason(t *testing.T) {
	w := vendorCallback(t, &vendorConsentService{xerr: email.ErrVendorConsentWrongMailbox}, "code=c&state=vc.n")
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "not the mailbox this link was made for") {
		t.Fatalf("wrong mailbox = %d %s", w.Code, w.Body.String())
	}
	w = vendorCallback(t, &vendorConsentService{xerr: errx.New(errx.Internal, "pq: relation email_accounts does not exist")}, "code=c&state=vc.n")
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "pq:") || !strings.Contains(w.Body.String(), "req-42") {
		t.Fatalf("internal error = %d %s", w.Code, w.Body.String())
	}
	svc := &vendorConsentService{xerr: email.ErrVendorConsentDeclined}
	if w := vendorCallback(t, svc, "error=access_denied&state=vc.n"); w.Code != http.StatusBadRequest || svc.providerErr != "access_denied" {
		t.Fatalf("declined = %d, provider error %q", w.Code, svc.providerErr)
	}
}

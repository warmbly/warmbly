package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/confenge"
)

func TestConfengeOperatorSessionHiddenWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/auth/confenge-operator/session", nil)

	(&Handler{}).ConfengeOperatorSession(ctx)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestConfengeOperatorSessionRateLimit(t *testing.T) {
	ip := "test-" + uuid.NewString()
	now := time.Now()
	for i := 0; i < confengeOperatorLimit; i++ {
		if !allowConfengeOperatorSession(ip, now) {
			t.Fatalf("attempt %d unexpectedly rejected", i+1)
		}
	}
	if allowConfengeOperatorSession(ip, now) {
		t.Fatal("expected attempt above the window limit to be rejected")
	}
	if !allowConfengeOperatorSession(ip, now.Add(confengeOperatorWindow)) {
		t.Fatal("expected a new window to accept the request")
	}
}

func TestConfengeOperatorSessionUnavailableIsStructured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("request_id", "req-confenge")
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/auth/confenge-operator/session", nil)
	ctx.Request.RemoteAddr = "127.0.0.1:43210"
	ctx.Request.Host = "127.0.0.1:8080"

	h := &Handler{ConfengeConfig: confenge.Config{
		Enabled: true, OperatorMode: true, OperatorUserID: uuid.New(), OperatorOrgID: uuid.New(),
	}}
	h.ConfengeOperatorSession(ctx)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var body struct {
		Code      string `json:"code"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "CONFENGE_OPERATOR_SESSION_UNAVAILABLE" || body.RequestID != "req-confenge" {
		t.Fatalf("unexpected response: %+v", body)
	}
}

func TestConfengeOperatorSessionHiddenFromNonLoopbackPeer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/auth/confenge-operator/session", nil)
	ctx.Request.RemoteAddr = "203.0.113.10:43210"
	h := &Handler{ConfengeConfig: confenge.Config{OperatorMode: true}}
	h.ConfengeOperatorSession(ctx)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestConfengeOperatorLoopbackCheckIgnoresForwardedHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.RemoteAddr = "203.0.113.10:43210"
	request.Header.Set("X-Forwarded-For", "127.0.0.1")
	if confengeOperatorRequestIsLocal(request) {
		t.Fatal("forwarded loopback header must not make a remote peer trusted")
	}
	for _, peer := range []string{"[::1]:43210", "127.0.0.1:43210", "172.20.0.1:43210"} {
		request.RemoteAddr = peer
		request.Host = "127.0.0.1:8080"
		if !confengeOperatorRequestIsLocal(request) {
			t.Fatalf("local tunnel peer %s must be accepted", peer)
		}
	}
}

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/api/handler"
	"github.com/warmbly/warmbly/internal/app/plathealth"
)

func TestProcessUpHealthIsNotReadiness(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &handler.Handler{} // nil collector: fail closed
	r := gin.New()
	registerPlatformHealth(r, h)

	health := httptest.NewRecorder()
	r.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("/health status=%d", health.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(health.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Fatalf("/health body=%s", health.Body.Bytes())
	}

	live := httptest.NewRecorder()
	r.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/live", nil))
	if live.Code != http.StatusOK {
		t.Fatalf("/live status=%d", live.Code)
	}

	ready := httptest.NewRecorder()
	r.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if ready.Code != http.StatusServiceUnavailable {
		t.Fatalf("/ready status=%d body=%s (HTTP 200 on /health must not make ready)", ready.Code, ready.Body.Bytes())
	}
	var report plathealth.Report
	if err := json.Unmarshal(ready.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Ready {
		t.Fatal("ready true after process-up only")
	}
	if !report.Live {
		t.Fatal("live should be true")
	}
	if hits := plathealth.PIIFindings(ready.Body.Bytes()); len(hits) > 0 {
		t.Fatalf("pii: %v", hits)
	}
}

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/app/plathealth"
)

func TestLiveStaysUpWhenReadyIsNot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{PlatformHealth: failingDBCollector()}

	live := httptest.NewRecorder()
	cLive, _ := gin.CreateTestContext(live)
	cLive.Request = httptest.NewRequest(http.MethodGet, "/live", nil)
	h.Live(cLive)
	if live.Code != http.StatusOK {
		t.Fatalf("/live status=%d", live.Code)
	}
	var liveBody map[string]any
	if err := json.Unmarshal(live.Body.Bytes(), &liveBody); err != nil {
		t.Fatal(err)
	}
	if liveBody["status"] != "live" {
		t.Fatalf("/live body=%s", live.Body.Bytes())
	}

	ready := httptest.NewRecorder()
	cReady, _ := gin.CreateTestContext(ready)
	cReady.Request = httptest.NewRequest(http.MethodGet, "/ready", nil)
	h.Ready(cReady)
	if ready.Code != http.StatusServiceUnavailable {
		t.Fatalf("/ready status=%d body=%s", ready.Code, ready.Body.Bytes())
	}
	var report plathealth.Report
	if err := json.Unmarshal(ready.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Ready {
		t.Fatal("/ready reported ready with db down")
	}
	if !report.Live {
		t.Fatal("/ready should still say live")
	}
}

func TestHealthDepsNotGreenOnProcessUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{} // nil collector: every required plane unobserved
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/health/deps", nil)
	h.HealthDeps(c)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil collector must not return 200 on /health/deps, got %d %s", w.Code, w.Body.Bytes())
	}
	if hits := plathealth.PIIFindings(w.Body.Bytes()); len(hits) > 0 {
		t.Fatalf("pii in deps: %v %s", hits, w.Body.Bytes())
	}
}

func TestReadyAllPlanesOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{PlatformHealth: healthyCollector()}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/ready", nil)
	h.Ready(c)
	if w.Code != http.StatusOK {
		t.Fatalf("/ready status=%d body=%s", w.Code, w.Body.Bytes())
	}
	var report plathealth.Report
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Ready {
		t.Fatalf("want ready: %+v", report)
	}
}

func failingDBCollector() *plathealth.Collector {
	return plathealth.NewCollector(plathealth.Options{
		DB:    func(context.Context) error { return plathealth.ErrUnobserved },
		Cache: func(context.Context) error { return nil },
		Heartbeat: func(context.Context) (plathealth.HeartbeatSnapshot, error) {
			return plathealth.HeartbeatSnapshot{Observed: true, Fresh: 1}, nil
		},
		Provider: func(context.Context) error { return nil },
		Bus:      nil,
	})
}

func healthyCollector() *plathealth.Collector {
	return plathealth.NewCollector(plathealth.Options{
		DB:    func(context.Context) error { return nil },
		Cache: func(context.Context) error { return nil },
		Heartbeat: func(context.Context) (plathealth.HeartbeatSnapshot, error) {
			return plathealth.HeartbeatSnapshot{Observed: true, Fresh: 1}, nil
		},
		Provider: func(context.Context) error { return nil },
		Bus:      plathealth.NewMemoryBus(),
	})
}

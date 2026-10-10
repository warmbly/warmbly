package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type nodeBrokerRegistry struct {
	repository.FleetNodeRepository
	id uuid.UUID
}

func (r *nodeBrokerRegistry) Get(_ context.Context, id uuid.UUID) (*models.FleetNode, error) {
	return &models.FleetNode{ID: id, Role: models.NodeRoleWorker}, nil
}

type nodeBrokerProbe struct {
	eventbus.EventBus
	mu     sync.Mutex
	scopes []eventbus.DiagnosticScope
}

func (p *nodeBrokerProbe) Diagnose(_ context.Context, at time.Time, scopes []eventbus.DiagnosticScope) models.MonitoringSource {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scopes = append(p.scopes, scopes...)
	return models.MonitoringSource{Availability: models.MonitoringUnavailable, Coverage: "unavailable", ObservedAt: &at, CheckedAt: at, Reason: "resource_absent", Metrics: []models.MonitoringMetric{}}
}

func TestNodeBrokerKeepsWorkerCommandsSeparateFromSharedResults(t *testing.T) {
	id := uuid.New()
	probe := &nodeBrokerProbe{}
	h := &Handler{FleetNodeRepo: &nodeBrokerRegistry{id: id}, BrokerDiagnostics: probe}
	r := gin.New()
	r.GET("/nodes/:id/broker", h.AdminNodeBroker)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/nodes/"+id.String()+"/broker", nil))
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	sort.Slice(probe.scopes, func(i, j int) bool { return probe.scopes[i].Group > probe.scopes[j].Group })
	if len(probe.scopes) != 2 || probe.scopes[0].Group != "worker-"+id.String() || len(probe.scopes[0].Topics) != 1 || probe.scopes[0].Topics[0] != "w."+id.String() || probe.scopes[1].Group != "consumer-group" || len(probe.scopes[1].Topics) != 1 || probe.scopes[1].Topics[0] != "jobs.worker-events" {
		t.Fatal("scope broadened or conflated", probe.scopes)
	}
	var out struct{ Commands, Results models.MonitoringSource }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Commands.Coverage != "unavailable" || out.Results.Coverage != "unavailable" || !probe.scopes[0].IncludePartitions || !probe.scopes[1].IncludePartitions {
		t.Fatal("missing evidence became healthy", out)
	}
}

type independentBrokerProbe struct{ eventbus.EventBus }

func (p *independentBrokerProbe) Diagnose(ctx context.Context, at time.Time, scopes []eventbus.DiagnosticScope) models.MonitoringSource {
	if scopes[0].Group != "consumer-group" {
		<-ctx.Done()
	}
	availability, reason := models.MonitoringFresh, "observed"
	if ctx.Err() != nil {
		availability, reason = models.MonitoringUnavailable, "timeout"
	}
	return models.MonitoringSource{Availability: availability, Reason: reason, CheckedAt: at}
}

func TestNodeBrokerTimeoutDoesNotHideIndependentResults(t *testing.T) {
	id := uuid.New()
	h := &Handler{FleetNodeRepo: &nodeBrokerRegistry{id: id}, BrokerDiagnostics: &independentBrokerProbe{}}
	r := gin.New()
	r.GET("/nodes/:id/broker", h.AdminNodeBroker)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/nodes/"+id.String()+"/broker", nil))
	var out struct{ Commands, Results models.MonitoringSource }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || out.Commands.Reason != "timeout" || out.Results.Availability != models.MonitoringFresh {
		t.Fatal("one scope consumed the other scope's deadline", w.Code, out)
	}
}

func TestNodeEvidenceMissingDependencyReturnsUnavailable(t *testing.T) {
	h := &Handler{}
	for _, read := range []bool{false, true} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/nodes/"+uuid.NewString()+"/logs", nil)
		if read {
			h.AdminNodeLogs(c)
		} else {
			h.InternalNodeLogs(c)
		}
		if w.Code != http.StatusServiceUnavailable || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("unavailable dependency became an authentication failure", w.Code)
		}
	}
}

package handler

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/models"
)

func brokerObservation(ctx context.Context, bus eventbus.EventBus, at time.Time, scope eventbus.DiagnosticScope) models.MonitoringSource {
	out := models.MonitoringSource{Availability: models.MonitoringUnavailable, Coverage: "unavailable", Reason: "unsupported", CheckedAt: at, ObservedAt: &at, Metrics: []models.MonitoringMetric{}}
	if d, ok := bus.(eventbus.Diagnostics); ok {
		out = d.Diagnose(ctx, at, []eventbus.DiagnosticScope{scope})
	}
	out.ID = scope.ID
	return out
}

func (h *Handler) AdminNodeBroker(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	if h.FleetNodeRepo == nil {
		errx.JSON(c, errx.ErrServiceDown)
		return
	}
	node, err := h.FleetNodeRepo.Get(c.Request.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		errx.JSON(c, errx.ErrNotFound)
		return
	}
	if err != nil {
		errx.JSON(c, errx.ErrServiceDown)
		return
	}
	if node.Role != models.NodeRoleWorker {
		errx.JSON(c, errx.New(errx.BadRequest, "command-group observations require a worker node"))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	at := time.Now().UTC()
	var commands, results models.MonitoringSource
	var scopes sync.WaitGroup
	scopes.Add(2)
	go func() {
		defer scopes.Done()
		commands = brokerObservation(ctx, h.BrokerDiagnostics, at, eventbus.DiagnosticScope{ID: id.String(), Group: "worker-" + id.String(), Topics: []string{kafka.GetWorkerTopic(id.String())}, IncludePartitions: true})
	}()
	go func() {
		defer scopes.Done()
		results = brokerObservation(ctx, h.BrokerDiagnostics, at, eventbus.DiagnosticScope{ID: "worker_events", Group: "consumer-group", Topics: []string{kafka.TopicWorkerEvents}, IncludePartitions: true})
	}()
	scopes.Wait()
	c.JSON(http.StatusOK, gin.H{"node_id": id, "observed_at": at, "commands": commands, "results": results, "note": "Results are the shared result-consumer scope, not this worker's delivery or processing count. Committed lag is not evidence of delivery or outage."})
}

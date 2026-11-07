package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/fleetnode"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/nodeauth"
)

// A node admitted by its own credential acts only as itself, and the broker log names it.
func TestNodeAuthenticatedByCredentialActsOnlyAsItself(t *testing.T) {
	setInstanceEnv(t)
	t.Setenv("NODE_BROKER_TOKEN", "fleet")
	t.Setenv("NODE_REQUIRE_NODE_CREDENTIAL", "true")
	var logs bytes.Buffer
	prev := zlog.Logger
	zlog.Logger = zerolog.New(&logs)
	defer func() { zlog.Logger = prev }()

	worker, other := uuid.New(), uuid.New()
	fleet := &fleetOf{
		roles:  map[uuid.UUID]models.NodeRole{worker: models.NodeRoleWorker, other: models.NodeRoleWorker},
		hashes: map[uuid.UUID]string{},
	}
	svc := fleetnode.New(fleet, nil, joinSettings{})
	secret, err := svc.IssueNodeCredential(t.Context(), worker)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{FleetNodes: svc}
	m := &middleware.Handler{NodeCredentials: svc}
	r := gin.New()
	r.GET("/api/v1/internal/fleet/node-env", m.NodeBrokerAuthMiddleware(), h.InternalFleetNodeEnv)
	r.POST("/api/v1/internal/fleet/heartbeat", m.NodeHeartbeatAuthMiddleware(), h.FleetHeartbeat)

	send := func(req *http.Request, withID bool) int {
		if withID {
			req.Header.Set(nodeauth.IDHeader, worker.String())
		}
		req.Header.Set(nodeauth.CredentialHeader, secret)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	if code := send(httptest.NewRequest(http.MethodGet, "/api/v1/internal/fleet/node-env?node_id="+worker.String()+"&role=worker", nil), true); code != http.StatusOK {
		t.Fatalf("own env: %d", code)
	}
	// The updater names its node only in the query, as it always has.
	if code := send(httptest.NewRequest(http.MethodGet, "/api/v1/internal/fleet/node-env?node_id="+worker.String()+"&role=worker", nil), false); code != http.StatusOK {
		t.Fatalf("own env named in the query: %d", code)
	}
	if code := send(httptest.NewRequest(http.MethodGet, "/api/v1/internal/fleet/node-env?node_id="+other.String()+"&role=worker", nil), true); code != http.StatusForbidden {
		t.Fatalf("another node's env: %d, want 403", code)
	}
	beat := httptest.NewRequest(http.MethodPost, "/api/v1/internal/fleet/heartbeat",
		strings.NewReader(`{"node_id":"`+other.String()+`","role":"worker"}`))
	beat.Header.Set("Content-Type", "application/json")
	if code := send(beat, true); code != http.StatusForbidden {
		t.Fatalf("a heartbeat for another node: %d, want 403", code)
	}

	out := logs.String()
	if !strings.Contains(out, `"node_id":"`+worker.String()+`"`) || strings.Contains(out, secret) {
		t.Fatalf("the broker log must name the node and never its credential: %s", out)
	}
}

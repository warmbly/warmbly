package nodeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/pkg/nodeevidence"
)

func TestLogCaptureNeverBlocksAndReportsLoss(t *testing.T) {
	l := &logCapture{queue: make(chan nodeevidence.Event, 1)}
	l.record(nodeevidence.Event{Name: nodeevidence.ControlPlaneHeld})
	done := make(chan struct{})
	go func() { l.record(nodeevidence.Event{Name: nodeevidence.ProviderUnreachable}); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("capture blocked worker")
	}
	if l.dropped.Load() != 1 {
		t.Fatal("capture loss missing")
	}
	l.record(nodeevidence.Event{Name: "raw password"})
	if l.dropped.Load() != 2 {
		t.Fatal("unknown event not dropped")
	}
}

func TestNodeLogTransportUsesHeaderAndRefusesRedirects(t *testing.T) {
	var calls int
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusNoContent) }))
	defer redirect.Close()
	id := uuid.New()
	var safeBody bytes.Buffer
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer test-node-credential" || !strings.HasSuffix(r.URL.Path, id.String()+"/logs") {
			t.Error("unsafe node credential transport")
		}
		var batch nodeevidence.Batch
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(&safeBody).Encode(batch)
		http.Redirect(w, r, redirect.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	a := &Agent{cfg: Config{NodeID: id, BaseURL: server.URL, LogToken: "test-node-credential"}}
	if a.sendLogs(context.Background(), []byte(`{"protocol":1,"events":[{"event":"sync_control_plane_held","http_status":503}]}`)) {
		t.Fatal("redirect counted as ingestion")
	}
	if calls != 0 || strings.Contains(safeBody.String(), "test-node-credential") {
		t.Fatal("credential forwarded across redirect or in body")
	}
}

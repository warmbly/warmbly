package repository

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// countingServer is an httptest server that counts the connections opened to it.
func countingServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var opened atomic.Int32
	srv := httptest.NewUnstartedServer(h)
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			opened.Add(1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, &opened
}

func TestControlPlaneClientReusesConnectionsUnderConcurrency(t *testing.T) {
	srv, opened := countingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		// Hold each call open so every round has all of its calls in flight at once.
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusNotFound)
	})
	repo, err := NewHTTPEmailMessageMapRepository(srv.URL, "fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	const concurrent, rounds = 32, 3
	for range rounds {
		var wg sync.WaitGroup
		for range concurrent {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := repo.Get(context.Background(), uuid.New(), uuid.New(), "<m@example.com>"); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
	}
	if got := opened.Load(); got > concurrent {
		t.Fatalf("%d rounds of %d concurrent calls opened %d connections, want at most %d", rounds, concurrent, got, concurrent)
	}
}

func TestControlPlaneClientDrainsUnreadBodies(t *testing.T) {
	srv, opened := countingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"temporarily unavailable"}`))
	})
	repo, err := NewHTTPEmailMessageMapRepository(srv.URL, "fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		if _, err := repo.Get(context.Background(), uuid.New(), uuid.New(), "<m@example.com>"); err == nil {
			t.Fatal("want an error for a 500")
		}
	}
	if got := opened.Load(); got != 1 {
		t.Fatalf("10 sequential calls with unread error bodies opened %d connections, want 1", got)
	}
}

func TestControlPlaneClientBoundsStalledBodyDrain(t *testing.T) {
	srv, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	repo, err := NewHTTPEmailMessageMapRepository(srv.URL, "fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	repo.(*httpEmailMessageMapRepository).client.Timeout = time.Second
	started := time.Now()
	if _, err := repo.Get(context.Background(), uuid.New(), uuid.New(), "<m@example.com>"); err == nil {
		t.Fatal("want an error for a 500")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("stalled error body delayed return for %s, want less than 500ms", elapsed)
	}
}

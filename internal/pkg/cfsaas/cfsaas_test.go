package cfsaas

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeZone is a zone's custom-hostname API holding hostnames in memory.
type fakeZone struct {
	mu      sync.Mutex
	hosts   map[string]string // hostname -> id
	creates int
	deletes int
	// raceOnCreate answers the next create as a duplicate after adding the name, like a replica that won.
	raceOnCreate bool
	origin       string
}

func (z *fakeZone) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	z.mu.Lock()
	defer z.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer tok" {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`))
		return
	}
	reply := func(result any) {
		b, _ := json.Marshal(map[string]any{"success": true, "errors": []any{}, "result": result})
		_, _ = w.Write(b)
	}
	view := func(name, id string) map[string]any {
		return map[string]any{
			"id": id, "hostname": name, "status": "pending",
			"ssl":                    map[string]any{"status": "pending_validation"},
			"ownership_verification": map[string]any{"type": "txt", "name": "_cf-custom-hostname." + name, "value": "v-" + id},
			"verification_errors":    []string{"custom hostname does not CNAME to this zone."},
		}
	}
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/custom_hostnames"):
		name := r.URL.Query().Get("hostname")
		out := []any{}
		if id, ok := z.hosts[name]; ok {
			out = append(out, view(name, id))
		}
		reply(out)
	case r.Method == http.MethodPost:
		var body struct {
			Hostname string `json:"hostname"`
			Origin   string `json:"custom_origin_server"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		z.creates++
		z.origin = body.Origin
		if _, ok := z.hosts[body.Hostname]; ok || z.raceOnCreate {
			z.raceOnCreate = false
			z.hosts[body.Hostname] = "id-" + body.Hostname
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1406,"message":"Duplicate custom hostname found."}]}`))
			return
		}
		z.hosts[body.Hostname] = "id-" + body.Hostname
		reply(view(body.Hostname, z.hosts[body.Hostname]))
	case r.Method == http.MethodDelete:
		z.deletes++
		for name, id := range z.hosts {
			if strings.HasSuffix(r.URL.Path, "/"+id) {
				delete(z.hosts, name)
			}
		}
		reply(map[string]any{"id": "x"})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newTest(t *testing.T, origin string) (*Client, *fakeZone) {
	z := &fakeZone{hosts: map[string]string{}}
	srv := httptest.NewServer(z)
	t.Cleanup(srv.Close)
	return New(Config{ZoneID: "zone", APIToken: "tok", Origin: origin, BaseURL: srv.URL}), z
}

func TestNewIsOffWithoutZoneAndToken(t *testing.T) {
	if New(Config{ZoneID: "zone"}) != nil || New(Config{APIToken: "tok"}) != nil {
		t.Fatal("a half-configured edge must register nothing")
	}
}

func TestEnsureCreatesOnceAndReportsOwnership(t *testing.T) {
	c, z := newTest(t, "origin.example.net")
	h, err := c.Ensure(context.Background(), "Acme.IO.")
	if err != nil {
		t.Fatal(err)
	}
	if h.Hostname != "acme.io" || h.Active() || h.Ownership == nil || h.Ownership.Name != "_cf-custom-hostname.acme.io" || len(h.Errors) != 1 {
		t.Fatalf("got %+v", h)
	}
	if z.origin != "origin.example.net" {
		t.Fatalf("origin = %q", z.origin)
	}
	if _, err := c.Ensure(context.Background(), "acme.io"); err != nil {
		t.Fatal(err)
	}
	if z.creates != 1 {
		t.Fatalf("creates = %d, a registered name was created again", z.creates)
	}
}

func TestEnsureAdoptsANameAnotherReplicaCreated(t *testing.T) {
	c, z := newTest(t, "")
	z.raceOnCreate = true
	h, err := c.Ensure(context.Background(), "acme.io")
	if err != nil || h == nil || h.ID != "id-acme.io" {
		t.Fatalf("got %+v, %v", h, err)
	}
}

func TestRemove(t *testing.T) {
	c, z := newTest(t, "")
	if err := c.Remove(context.Background(), "acme.io"); err != nil || z.deletes != 0 {
		t.Fatalf("removing an absent name: %v, deletes = %d", err, z.deletes)
	}
	_, _ = c.Ensure(context.Background(), "acme.io")
	if err := c.Remove(context.Background(), "acme.io"); err != nil || len(z.hosts) != 0 {
		t.Fatalf("remove: %v, left %v", err, z.hosts)
	}
}

func TestRefusalCarriesNoToken(t *testing.T) {
	c, _ := newTest(t, "")
	c.cfg.APIToken = "wrong"
	_, err := c.Ensure(context.Background(), "acme.io")
	if err == nil || strings.Contains(err.Error(), "wrong") {
		t.Fatalf("err = %v", err)
	}
}

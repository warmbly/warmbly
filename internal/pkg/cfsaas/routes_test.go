package cfsaas

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// routedZone is fakeZone plus the zone's Worker routes.
type routedZone struct {
	*fakeZone
	routes map[string]string // id -> pattern
	script map[string]string // id -> script
	posts  int
	refuse bool
}

func (z *routedZone) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(r.URL.Path, "/workers/routes") {
		z.fakeZone.ServeHTTP(w, r)
		return
	}
	reply := func(result any) {
		b, _ := json.Marshal(map[string]any{"success": true, "errors": []any{}, "result": result})
		_, _ = w.Write(b)
	}
	switch r.Method {
	case http.MethodPost:
		z.posts++
		if z.refuse {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`))
			return
		}
		var body struct{ Pattern, Script string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, p := range z.routes {
			if p == body.Pattern {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":10020,"message":"A route with the same pattern already exists."}]}`))
				return
			}
		}
		id := "r" + strconv.Itoa(z.posts)
		z.routes[id], z.script[id] = body.Pattern, body.Script
		reply(map[string]any{"id": id, "pattern": body.Pattern, "script": body.Script})
	case http.MethodGet:
		out := []any{}
		for id, p := range z.routes {
			out = append(out, map[string]any{"id": id, "pattern": p, "script": z.script[id]})
		}
		reply(out)
	case http.MethodDelete:
		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		delete(z.routes, id)
		delete(z.script, id)
		reply(map[string]any{"id": id})
	}
}

func newRouted(t *testing.T, worker string) (*Client, *routedZone) {
	z := &routedZone{fakeZone: &fakeZone{hosts: map[string]string{}}, routes: map[string]string{}, script: map[string]string{}}
	srv := httptest.NewServer(z)
	t.Cleanup(srv.Close)
	return New(Config{ZoneID: "zone", APIToken: "tok", Worker: worker, BaseURL: srv.URL}), z
}

func TestEnsureRoutesOnlyItsOwnHostnameToTheWorker(t *testing.T) {
	c, z := newRouted(t, "portal")
	// Another customer's name on the same zone, answered by something else.
	z.routes["other"], z.script["other"] = "links.other.example/*", "tracking"
	for range 2 {
		if _, err := c.Ensure(context.Background(), "Portal.Agency.Example."); err != nil {
			t.Fatal(err)
		}
	}
	if len(z.routes) != 2 || z.routes["r1"] != "portal.agency.example/*" || z.script["r1"] != "portal" {
		t.Fatalf("routes = %v, scripts = %v", z.routes, z.script)
	}
	if z.routes["other"] != "links.other.example/*" || z.script["other"] != "tracking" {
		t.Fatal("another name's route was changed")
	}
}

func TestEnsureFailsWhenTheRouteIsRefused(t *testing.T) {
	c, z := newRouted(t, "portal")
	z.refuse = true
	if h, err := c.Ensure(context.Background(), "portal.agency.example"); err == nil || h != nil {
		t.Fatalf("a hostname without its route must not read as registered: %+v, %v", h, err)
	}
	// The hostname is there, so the next attempt only has the route left to add.
	z.refuse = false
	if _, err := c.Ensure(context.Background(), "portal.agency.example"); err != nil || len(z.routes) != 1 || z.creates != 1 {
		t.Fatalf("retry: %v, routes %v, creates %d", err, z.routes, z.creates)
	}
}

func TestEnsureRefusesAHostnameRoutedToAnotherWorker(t *testing.T) {
	c, z := newRouted(t, "portal")
	z.routes["manual"], z.script["manual"] = "portal.agency.example/*", "something-else"
	if h, err := c.Ensure(context.Background(), "portal.agency.example"); err == nil || h != nil {
		t.Fatalf("a hostname another Worker answers must not read as registered: %+v, %v", h, err)
	}
	if len(z.routes) != 1 || z.script["manual"] != "something-else" {
		t.Fatalf("the other Worker's route was changed: %v, %v", z.routes, z.script)
	}
}

func TestRemoveLeavesARouteForANameTheZoneNeverHad(t *testing.T) {
	c, z := newRouted(t, "portal")
	// Added by hand for a name this client never registered.
	z.routes["manual"], z.script["manual"] = "portal.agency.example/*", "portal"
	if err := c.Remove(context.Background(), "portal.agency.example"); err != nil {
		t.Fatal(err)
	}
	if z.routes["manual"] == "" {
		t.Fatal("a route for a hostname the zone does not have was removed")
	}
}

func TestEnsureAddsNoRouteWithoutAWorker(t *testing.T) {
	c, z := newRouted(t, "")
	if _, err := c.Ensure(context.Background(), "portal.agency.example"); err != nil || z.posts != 0 {
		t.Fatalf("got %v, route creates = %d", err, z.posts)
	}
}

func TestRemoveTakesOnlyItsOwnRoute(t *testing.T) {
	c, z := newRouted(t, "portal")
	z.routes["other"], z.script["other"] = "links.other.example/*", "tracking"
	// The same pattern sent somewhere else by hand is not this client's to delete.
	z.routes["manual"], z.script["manual"] = "manual.agency.example/*", "something-else"
	_, _ = c.Ensure(context.Background(), "portal.agency.example")
	if err := c.Remove(context.Background(), "portal.agency.example"); err != nil {
		t.Fatal(err)
	}
	if len(z.hosts) != 0 || len(z.routes) != 2 {
		t.Fatalf("left hostnames %v, routes %v", z.hosts, z.routes)
	}
	if err := c.Remove(context.Background(), "manual.agency.example"); err != nil || z.routes["manual"] == "" {
		t.Fatalf("a route to another Worker was removed: %v, %v", err, z.routes)
	}
}

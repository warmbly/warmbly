package hetzner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/infrastructure/cloudprovider"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := New("test-token", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	return c, srv
}

func TestNew_RejectsEmptyToken(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("expected error on empty token")
	}
}

func TestDo_SendsBearerToken(t *testing.T) {
	gotAuth := ""
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"datacenters":[]}`))
	})
	if err := c.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("auth header: got %q", gotAuth)
	}
}

func TestDo_SurfaceAPIError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"invalid token"}}`))
	})
	err := c.Verify(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid token") {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
}

func TestLocations_Parsing(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/locations" {
			t.Fatalf("expected /locations, got %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"locations": [
				{"id":1,"name":"fsn1","description":"Falkenstein DC Park 1","country":"DE","city":"Falkenstein","network_zone":"eu-central"},
				{"id":2,"name":"hil","description":"Hillsboro DC1","country":"US","city":"Hillsboro","network_zone":"us-west"}
			]
		}`))
	})
	locs, err := c.Locations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(locs) != 2 || locs[0].Name != "fsn1" || locs[1].Country != "US" {
		t.Fatalf("parse mismatch: %#v", locs)
	}
}

func TestServerTypes_PicksCheapestPrice(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"server_types": [
				{
					"id": 1, "name": "cx22", "description": "CX22",
					"cores": 2, "memory": 4, "disk": 40,
					"storage_type": "local", "cpu_type": "shared", "architecture": "x86",
					"prices": [
						{"location": "fsn1", "price_monthly": {"gross": "5.83", "net": "4.90"}},
						{"location": "hil",  "price_monthly": {"gross": "7.05", "net": "5.92"}}
					]
				}
			]
		}`))
	})
	types, err := c.ServerTypes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 1 {
		t.Fatalf("want 1 type, got %d", len(types))
	}
	if types[0].PriceMonthlyEUR != 5.83 {
		t.Fatalf("want cheapest price 5.83, got %v", types[0].PriceMonthlyEUR)
	}
	if types[0].Cores != 2 || types[0].Memory != 4 || types[0].Disk != 40 {
		t.Fatalf("specs mismatch: %#v", types[0])
	}
}

func TestCreateServer_PostsAndParsesResponse(t *testing.T) {
	var receivedBody createServerReq
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/servers" {
			t.Fatalf("expected POST /servers, got %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{
			"server": {
				"id": 42, "name": "warmbly-fsn1-001", "status": "initializing",
				"public_net": {
					"ipv4": {"ip": "1.2.3.4"},
					"ipv6": {"ip": "2a01::1"}
				}
			}
		}`))
	})

	req := cloudprovider.CreateServerRequest{
		Name:             "warmbly-fsn1-001",
		ServerType:       "cx22",
		Image:            "ubuntu-22.04",
		Location:         "fsn1",
		SSHKeyIDs:        []string{"key-1"},
		UserData:         "#cloud-config\nrunmd: []",
		Labels:           map[string]string{"warmbly": "true"},
		StartAfterCreate: true,
	}
	s, err := c.CreateServer(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "42" {
		t.Fatalf("server id: got %q want %q", s.ID, "42")
	}
	if s.PublicIPv4 != "1.2.3.4" {
		t.Fatalf("server ipv4: got %q", s.PublicIPv4)
	}
	if receivedBody.ServerType != "cx22" {
		t.Fatalf("posted body mismatch: %#v", receivedBody)
	}
	if !receivedBody.StartAfterCreate {
		t.Fatal("StartAfterCreate not propagated")
	}
}

func TestCreatePrimaryIP_DefaultsAssigneeServer(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body createPrimaryIPReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.AssigneeType != "server" {
			t.Fatalf("want assignee_type=server, got %q", body.AssigneeType)
		}
		_, _ = w.Write([]byte(`{"primary_ip":{"id":7,"type":"ipv4","ip":"5.6.7.8"}}`))
	})
	ip, err := c.CreatePrimaryIP(context.Background(), cloudprovider.CreatePrimaryIPRequest{
		Type: "ipv4", Name: "warmbly-ip-1", Datacenter: "fsn1-dc14",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ip.ID != "7" || ip.IP != "5.6.7.8" {
		t.Fatalf("parse mismatch: %#v", ip)
	}
}

func TestSetReverseDNS_PostsToAction(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("want POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/actions/change_dns_ptr") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"action":{"id":1,"status":"running"}}`))
	})
	if err := c.SetReverseDNS(context.Background(), "7", "w.example.com"); err != nil {
		t.Fatal(err)
	}
}

func TestProviderInterfaceConformance(t *testing.T) {
	var _ cloudprovider.Provider = (*Client)(nil)
}

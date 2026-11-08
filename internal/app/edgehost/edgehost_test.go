package edgehost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/cfsaas"
	"github.com/warmbly/warmbly/internal/repository"
)

type memRepo struct {
	kind   repository.EdgeKind
	wanted []string
	rows   map[string]repository.EdgeHostname
}

func (m *memRepo) Kind() repository.EdgeKind {
	if m.kind == "" {
		return repository.EdgeTracking
	}
	return m.kind
}

func (m *memRepo) Wanted(context.Context) ([]string, error) { return m.wanted, nil }
func (m *memRepo) List(context.Context) ([]repository.EdgeHostname, error) {
	out := make([]repository.EdgeHostname, 0, len(m.rows))
	for _, r := range m.rows {
		out = append(out, r)
	}
	return out, nil
}
func (m *memRepo) Get(_ context.Context, hosts []string) ([]repository.EdgeHostname, error) {
	var out []repository.EdgeHostname
	for _, h := range hosts {
		if r, ok := m.rows[h]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *memRepo) Save(_ context.Context, h repository.EdgeHostname) error {
	now := time.Now()
	h.CheckedAt, h.UnwantedSince = &now, nil
	m.rows[h.Hostname] = h
	return nil
}
func (m *memRepo) Failed(_ context.Context, host, zone, reason string) error {
	now := time.Now()
	r := m.rows[host]
	if r.Zone != zone {
		r.Status, r.OwnershipName, r.OwnershipValue = "", "", ""
	}
	r.Hostname, r.Zone, r.LastError, r.CheckedAt, r.UnwantedSince = host, zone, reason, &now, nil
	m.rows[host] = r
	return nil
}
func (m *memRepo) MarkUnwanted(_ context.Context, hosts []string) error {
	now := time.Now()
	for _, h := range hosts {
		r := m.rows[h]
		if r.UnwantedSince == nil {
			r.UnwantedSince = &now
		}
		m.rows[h] = r
	}
	return nil
}
func (m *memRepo) DeleteUnwanted(_ context.Context, host string) (bool, error) {
	if r, ok := m.rows[host]; !ok || r.UnwantedSince == nil {
		return false, nil
	}
	delete(m.rows, host)
	return true, nil
}

type fakeEdge struct {
	ensured  []string
	removed  []string
	attempts int
	status   string
	fail     bool
	zone     string
	// onRemove runs inside Remove, standing in for a replica registering the name at that moment.
	onRemove func(name string)
}

func (f *fakeEdge) Zone() string { return f.zone }

func (f *fakeEdge) Ensure(_ context.Context, name string) (*cfsaas.Hostname, error) {
	f.attempts++
	if f.fail {
		return nil, errors.New("down")
	}
	f.ensured = append(f.ensured, name)
	h := &cfsaas.Hostname{Hostname: name, Status: f.status}
	if f.status != "active" {
		h.Ownership = &cfsaas.TXTRecord{Name: "_cf-custom-hostname." + name, Value: "v"}
	}
	return h, nil
}
func (f *fakeEdge) Remove(_ context.Context, name string) error {
	if f.onRemove != nil {
		f.onRemove(name)
	}
	f.removed = append(f.removed, name)
	return nil
}

func newTest(wanted ...string) (*Service, *memRepo, *fakeEdge) {
	repo := &memRepo{wanted: wanted, rows: map[string]repository.EdgeHostname{}}
	edge := &fakeEdge{status: "pending", zone: "zone-a"}
	return New(repo, edge, func() []string { return []string{"track.warmbly.test"} }), repo, edge
}

func ago(d time.Duration) *time.Time {
	t := time.Now().Add(-d)
	return &t
}

func TestNewIsNilWithoutAnEdge(t *testing.T) {
	if New(&memRepo{}, nil, nil).Enabled() {
		t.Fatal("no edge must mean no registration")
	}
	var s *Service
	s.Register(context.Background(), "acme.io")
	if s.Pending(context.Background(), "acme.io") != nil {
		t.Fatal("a nil service reports nothing pending")
	}
}

func TestSweepRegistersServedNamesButNotItsOwnHost(t *testing.T) {
	s, repo, edge := newTest("ACME.io", "www.acme.io", "track.warmbly.test")
	if err := s.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(edge.ensured) != 2 || repo.rows["acme.io"].OwnershipName != "_cf-custom-hostname.acme.io" {
		t.Fatalf("ensured %v, rows %v", edge.ensured, repo.rows)
	}
	if got := s.Pending(context.Background(), "acme.io", "www.acme.io"); len(got) != 2 {
		t.Fatalf("pending = %v", got)
	}

	// A pending name is not asked about again before its recheck; an active one waits a day.
	_ = s.Sweep(context.Background())
	if len(edge.ensured) != 2 {
		t.Fatalf("rechecked too soon: %v", edge.ensured)
	}
	r := repo.rows["acme.io"]
	r.CheckedAt = ago(pendingRecheck)
	repo.rows["acme.io"] = r
	edge.status = "active"
	_ = s.Sweep(context.Background())
	if len(edge.ensured) != 3 || repo.rows["acme.io"].Status != "active" {
		t.Fatalf("pending name was not rechecked: %v", edge.ensured)
	}
	if got := s.Pending(context.Background(), "acme.io"); len(got) != 0 {
		t.Fatalf("an active name still asks for its TXT: %v", got)
	}
}

func TestSweepRetiresANameOnlyAfterItStaysUnserved(t *testing.T) {
	s, repo, edge := newTest("acme.io", "beta.io")
	_ = s.Sweep(context.Background())
	repo.wanted = []string{"acme.io"}
	_ = s.Sweep(context.Background())
	if len(edge.removed) != 0 || repo.rows["beta.io"].UnwantedSince == nil {
		t.Fatalf("removed at once: %v", edge.removed)
	}
	r := repo.rows["beta.io"]
	r.UnwantedSince = ago(removeAfter)
	repo.rows["beta.io"] = r
	_ = s.Sweep(context.Background())
	if len(edge.removed) != 1 || edge.removed[0] != "beta.io" {
		t.Fatalf("removed %v", edge.removed)
	}
	if _, ok := repo.rows["beta.io"]; ok {
		t.Fatal("a removed name kept its row")
	}
}

func TestSweepKeepsEverythingWhenNothingReadsAsServed(t *testing.T) {
	s, repo, edge := newTest("acme.io")
	_ = s.Sweep(context.Background())
	repo.wanted = nil
	_ = s.Sweep(context.Background())
	if repo.rows["acme.io"].UnwantedSince != nil || len(edge.removed) != 0 {
		t.Fatal("an empty read started retiring every name")
	}
}

func TestANameServedAgainIsKept(t *testing.T) {
	s, repo, edge := newTest("acme.io")
	_ = s.Sweep(context.Background())
	r := repo.rows["acme.io"]
	r.UnwantedSince = ago(time.Hour)
	repo.rows["acme.io"] = r
	_ = s.Sweep(context.Background())
	if repo.rows["acme.io"].UnwantedSince != nil || len(edge.removed) != 0 {
		t.Fatalf("a served name stayed on its removal clock")
	}
}

func TestRegisterSkipsWhatIsLiveAndSurvivesAnOutage(t *testing.T) {
	s, _, edge := newTest()
	edge.status = "active"
	edge.fail = true
	s.Register(context.Background(), "acme.io")
	edge.fail = false
	s.Register(context.Background(), "acme.io", "acme.io")
	s.Register(context.Background(), "acme.io")
	if len(edge.ensured) != 1 {
		t.Fatalf("ensured %v", edge.ensured)
	}
}

func TestHostsThatKeepFailingDoNotHoldTheRestBack(t *testing.T) {
	var hosts []string
	for i := 0; i <= callsPerSweep; i++ {
		hosts = append(hosts, fmt.Sprintf("d%03d.io", i))
	}
	s, repo, edge := newTest(hosts...)
	edge.fail = true
	_ = s.Sweep(context.Background())
	if edge.attempts != callsPerSweep {
		t.Fatalf("attempts = %d", edge.attempts)
	}
	edge.fail = false
	_ = s.Sweep(context.Background())
	if len(edge.ensured) != 1 || edge.ensured[0] != hosts[callsPerSweep] {
		t.Fatalf("the host past the budget was not tried next: %v", edge.ensured)
	}
	if repo.rows[hosts[0]].LastError == "" {
		t.Fatal("a failed attempt left no trace")
	}
}

func TestAVerdictFromAnotherZoneCountsAsNone(t *testing.T) {
	s, repo, edge := newTest("acme.io")
	_ = s.Sweep(context.Background())
	edge.zone, edge.fail = "zone-b", true
	if got := s.Pending(context.Background(), "acme.io"); len(got) != 0 {
		t.Fatalf("pending = %v", got)
	}
	// A failed first ask at the new zone must not pass the old zone's record off as the new one's.
	_ = s.Sweep(context.Background())
	if got := s.Pending(context.Background(), "acme.io"); len(got) != 0 {
		t.Fatalf("the old zone's activation record is shown for the new zone: %v", got)
	}
	r := repo.rows["acme.io"]
	r.CheckedAt = ago(pendingRecheck)
	repo.rows["acme.io"] = r
	edge.fail = false
	_ = s.Sweep(context.Background())
	if len(edge.ensured) != 2 || repo.rows["acme.io"].Zone != "zone-b" {
		t.Fatalf("the new zone was not asked: %v %+v", edge.ensured, repo.rows["acme.io"])
	}
	if got := s.Pending(context.Background(), "acme.io"); len(got) != 1 {
		t.Fatalf("the new zone's activation record is hidden: %v", got)
	}
}

func TestANameRegisteredWhileItIsRemovedIsPutBack(t *testing.T) {
	s, repo, edge := newTest("acme.io", "beta.io")
	_ = s.Sweep(context.Background())
	repo.wanted = []string{"acme.io"}
	_ = s.Sweep(context.Background())
	r := repo.rows["beta.io"]
	r.UnwantedSince = ago(removeAfter)
	repo.rows["beta.io"] = r
	edge.onRemove = func(name string) {
		_ = repo.Save(context.Background(), repository.EdgeHostname{Hostname: name, Zone: edge.zone})
	}
	_ = s.Sweep(context.Background())
	if _, ok := repo.rows["beta.io"]; !ok || edge.ensured[len(edge.ensured)-1] != "beta.io" {
		t.Fatalf("a name registered during its removal was left off the edge: %v", edge.ensured)
	}
}

func TestRegisterAsksAgainAboutAPendingName(t *testing.T) {
	s, _, edge := newTest()
	s.Register(context.Background(), "acme.io")
	s.Register(context.Background(), "acme.io")
	if len(edge.ensured) != 2 {
		t.Fatalf("a check on a pending name did not reach the edge: %v", edge.ensured)
	}
}

// cloudflare stands in for the custom hostname API of one zone, recording each name created on it.
func cloudflare(t *testing.T, zone string) (*httptest.Server, *[]string) {
	t.Helper()
	var created []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zones/"+zone+"/custom_hostnames" {
			t.Errorf("call outside the zone: %s %s", r.Method, r.URL.Path)
		}
		result := any([]any{})
		if r.Method == http.MethodPost {
			var body struct {
				Hostname string `json:"hostname"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			created = append(created, body.Hostname)
			result = map[string]any{"id": "h1", "hostname": body.Hostname, "status": "pending",
				"ownership_verification": map[string]string{"type": "txt", "name": "_cf-custom-hostname." + body.Hostname, "value": "proof"}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	t.Cleanup(srv.Close)
	return srv, &created
}

func TestOpenRegistersNothingWithoutAZone(t *testing.T) {
	repo := &memRepo{kind: repository.EdgePortal, rows: map[string]repository.EdgeHostname{}}
	for _, cfg := range []cfsaas.Config{{}, {ZoneID: "zone-p"}, {APIToken: "token"}} {
		s := Open(repo, cfg, nil)
		if s.Enabled() {
			t.Fatalf("%+v opened a zone", cfg)
		}
		s.Register(context.Background(), "portal.agency.test")
		s.Start(context.Background())
		if recs := s.Records(context.Background(), "portal.agency.test"); len(recs) != 0 || len(repo.rows) != 0 {
			t.Fatalf("an unset zone registered %v and shows %v", repo.rows, recs)
		}
	}

	srv, created := cloudflare(t, "zone-p")
	s := Open(repo, cfsaas.Config{ZoneID: "zone-p", APIToken: "token", BaseURL: srv.URL}, nil)
	s.Register(context.Background(), "Portal.Agency.test")
	if len(*created) != 1 || (*created)[0] != "portal.agency.test" || repo.rows["portal.agency.test"].Zone != "zone-p" {
		t.Fatalf("created %v, rows %v", *created, repo.rows)
	}
	want := []models.DNSRecord{{Purpose: "edge", Type: "TXT", Name: "_cf-custom-hostname.portal.agency.test", Value: "proof"}}
	if got := s.Records(context.Background(), "portal.agency.test"); len(got) != 1 || got[0] != want[0] {
		t.Fatalf("records = %+v", got)
	}
}

func TestPortalZoneLeavesTheTrackingZoneAlone(t *testing.T) {
	tracking, trackingRepo, trackingEdge := newTest("link.acme.test")
	_ = tracking.Sweep(context.Background())
	before := trackingRepo.rows["link.acme.test"]

	portalRepo := &memRepo{kind: repository.EdgePortal, wanted: []string{"portal.agency.test", "portal.other.test", "portal.warmbly.test"}, rows: map[string]repository.EdgeHostname{}}
	portalEdge := &fakeEdge{status: "pending", zone: "zone-p"}
	portal := New(portalRepo, portalEdge, func() []string { return []string{"portal.warmbly.test"} })
	if portal.job() == tracking.job() || tracking.job() != "edge_hostname_sweep" {
		t.Fatalf("the two sweeps share a job: %q %q", portal.job(), tracking.job())
	}
	if err := portal.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(portalEdge.ensured) != 2 || portalRepo.rows["portal.other.test"].Zone != "zone-p" {
		t.Fatalf("portal zone ensured %v", portalEdge.ensured)
	}
	if len(trackingEdge.ensured) != 1 || len(trackingRepo.rows) != 1 || trackingRepo.rows["link.acme.test"] != before {
		t.Fatalf("the portal sweep touched the tracking zone: %v %v", trackingEdge.ensured, trackingRepo.rows)
	}
	if got := tracking.Records(context.Background(), "portal.other.test"); len(got) != 0 {
		t.Fatalf("the tracking zone shows a portal record: %v", got)
	}

	// A portal domain that stops verifying keeps its hostname for a day, then is removed from the portal zone only.
	portalRepo.wanted = []string{"portal.other.test"}
	_ = portal.Sweep(context.Background())
	if len(portalEdge.removed) != 0 {
		t.Fatalf("removed at once: %v", portalEdge.removed)
	}
	r := portalRepo.rows["portal.agency.test"]
	r.UnwantedSince = ago(removeAfter)
	portalRepo.rows["portal.agency.test"] = r
	_ = portal.Sweep(context.Background())
	if len(portalEdge.removed) != 1 || portalEdge.removed[0] != "portal.agency.test" || len(trackingEdge.removed) != 0 {
		t.Fatalf("removed portal %v, tracking %v", portalEdge.removed, trackingEdge.removed)
	}
	if _, ok := portalRepo.rows["portal.agency.test"]; ok {
		t.Fatal("a removed portal name kept its row")
	}
}

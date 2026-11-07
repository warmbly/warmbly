package edgehost

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/cfsaas"
	"github.com/warmbly/warmbly/internal/repository"
)

type memRepo struct {
	wanted []string
	rows   map[string]repository.EdgeHostname
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
func (m *memRepo) Delete(_ context.Context, host string) error {
	delete(m.rows, host)
	return nil
}

type fakeEdge struct {
	ensured []string
	removed []string
	status  string
	fail    bool
}

func (f *fakeEdge) Ensure(_ context.Context, name string) (*cfsaas.Hostname, error) {
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
	f.removed = append(f.removed, name)
	return nil
}

func newTest(wanted ...string) (*Service, *memRepo, *fakeEdge) {
	repo := &memRepo{wanted: wanted, rows: map[string]repository.EdgeHostname{}}
	edge := &fakeEdge{status: "pending"}
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

func TestRegisterSkipsWhatIsCurrentAndSurvivesAnOutage(t *testing.T) {
	s, _, edge := newTest()
	edge.fail = true
	s.Register(context.Background(), "acme.io")
	edge.fail = false
	s.Register(context.Background(), "acme.io", "acme.io")
	s.Register(context.Background(), "acme.io")
	if len(edge.ensured) != 1 {
		t.Fatalf("ensured %v", edge.ensured)
	}
}

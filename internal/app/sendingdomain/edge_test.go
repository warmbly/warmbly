package sendingdomain

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/edgehost"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/cfsaas"
	"github.com/warmbly/warmbly/internal/repository"
)

type memEdgeRows map[string]repository.EdgeHostname

func (m memEdgeRows) Kind() repository.EdgeKind                { return repository.EdgeTracking }
func (m memEdgeRows) Wanted(context.Context) ([]string, error) { return nil, nil }
func (m memEdgeRows) List(context.Context) ([]repository.EdgeHostname, error) {
	return nil, nil
}
func (m memEdgeRows) Get(_ context.Context, hosts []string) ([]repository.EdgeHostname, error) {
	var out []repository.EdgeHostname
	for _, h := range hosts {
		if r, ok := m[h]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m memEdgeRows) Save(_ context.Context, h repository.EdgeHostname) error {
	now := time.Now()
	h.CheckedAt = &now
	m[h.Hostname] = h
	return nil
}
func (m memEdgeRows) Failed(context.Context, string, string, string) error { return nil }
func (m memEdgeRows) MarkUnwanted(context.Context, []string) error         { return nil }
func (m memEdgeRows) DeleteUnwanted(context.Context, string) (bool, error) { return false, nil }

// pendingEdge registers every name as waiting for its TXT, the answer for a root that does not CNAME to the zone.
type pendingEdge struct{ ensured []string }

func (p *pendingEdge) Ensure(_ context.Context, name string) (*cfsaas.Hostname, error) {
	p.ensured = append(p.ensured, name)
	return &cfsaas.Hostname{Hostname: name, Status: "pending", Ownership: &cfsaas.TXTRecord{Name: "_cf-custom-hostname." + name, Value: "cf-" + name}}, nil
}
func (p *pendingEdge) Remove(context.Context, string) error { return nil }
func (p *pendingEdge) Zone() string                         { return "" }

func byPurpose(records []models.DNSRecord) map[string][]models.DNSRecord {
	out := map[string][]models.DNSRecord{}
	for _, r := range records {
		out[r.Purpose] = append(out[r.Purpose], r)
	}
	return out
}

func TestBehindAnEdgeTheRootFollowsTheTrackingHostAndWWWFollowsTheRoot(t *testing.T) {
	dns := baseDNS()
	s, _ := newTest(dns)
	edge := &pendingEdge{}
	s.WireEdge(edgehost.New(memEdgeRows{}, edge, nil))
	org := uuid.New()
	ctx := context.Background()

	r, xerr := s.SetRedirect(ctx, org, uuid.New(), "acme.io", RedirectInput{TargetURL: "https://acme.com"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	recs := byPurpose(r.Records)
	if len(recs["root"]) != 1 || recs["root"][0].Type != "CNAME" || recs["root"][0].Value != "track.warmbly.test" {
		t.Fatalf("root = %+v; the edge's shared addresses must not be handed out", recs["root"])
	}
	if len(recs["www"]) != 1 || recs["www"][0].Type != "CNAME" || recs["www"][0].Value != "acme.io" {
		t.Fatalf("www = %+v", recs["www"])
	}
	if len(recs["edge"]) != 0 || len(edge.ensured) != 0 {
		t.Fatal("an unverified domain was registered at the edge")
	}

	// A flattened root resolves to the tracking host's addresses; www is a CNAME to the root.
	dns.txt["_warmbly.acme.io"] = []string{recs["ownership"][0].Value}
	dns.ips["acme.io"] = []string{"203.0.113.10"}
	dns.cname["www.acme.io"] = "acme.io"
	r, xerr = s.VerifyRedirect(ctx, org, "acme.io")
	if xerr != nil || !r.Verified {
		t.Fatalf("not verified: %v %s", xerr, r.LastError)
	}
	recs = byPurpose(r.Records)
	if !recs["www"][0].OK {
		t.Fatal("www as a CNAME to the root was not accepted")
	}
	if len(edge.ensured) != 2 {
		t.Fatalf("verifying did not register the root and www: %v", edge.ensured)
	}
	if len(recs["edge"]) != 2 || recs["edge"][0].Name != "_cf-custom-hostname.acme.io" || recs["edge"][0].OK {
		t.Fatalf("edge records = %+v", recs["edge"])
	}
}

func TestWithoutAnEdgeTheRootRecordsAreUnchanged(t *testing.T) {
	s, _ := newTest(baseDNS())
	r, xerr := s.SetRedirect(context.Background(), uuid.New(), uuid.New(), "acme.io", RedirectInput{TargetURL: "https://acme.com"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	recs := byPurpose(r.Records)
	if len(recs["root"]) != 1 || recs["root"][0].Type != "A" || recs["root"][0].Value != "203.0.113.10" || len(recs["edge"]) != 0 {
		t.Fatalf("records = %+v", r.Records)
	}
}

func TestWWWPointingAtAnotherRootIsNotAccepted(t *testing.T) {
	dns := baseDNS()
	s, _ := newTest(dns)
	dns.ips["acme.io"] = []string{"203.0.113.10"}
	dns.ips["other.io"] = []string{"203.0.113.99"}
	dns.cname["www.acme.io"] = "other.io"
	if s.wwwPoints(context.Background(), "acme.io", "track.warmbly.test", true) {
		t.Fatal("www CNAMEd to a different domain was accepted")
	}
	dns.cname["www.acme.io"] = "acme.io"
	dns.ips["acme.io"] = dns.ips["other.io"]
	if s.wwwPoints(context.Background(), "acme.io", "track.warmbly.test", false) {
		t.Fatal("www CNAMEd to a root that does not point here was accepted")
	}
}

package dnsauth

import (
	"encoding/json"
	"testing"
)

func TestReadinessSeparatesDNSFromMessageEvidence(t *testing.T) {
	r := Result{Domain: "example.test", SPFFound: true, DMARCFound: true, DMARCPolicy: "none"}
	components := r.Readiness()
	if r.State() != "passing" || components[0].State != "record-present" || components[1].State != "unknown" || components[3].State != "unknown" || components[4].State != "unknown" || components[5].State != "not-evaluated" {
		t.Fatal(components)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var old Result
	if err := json.Unmarshal(b, &old); err != nil || old.State() != r.State() {
		t.Fatalf("legacy JSON decode: %v %+v", err, old)
	}
	var additive struct {
		EvidenceScope string `json:"evidence_scope"`
		Observed      string `json:"observed_authentication"`
	}
	if err := json.Unmarshal(b, &additive); err != nil || additive.EvidenceScope != "dns_discovery" || additive.Observed != "unknown" {
		t.Fatal(additive)
	}
	r.LookupError = true
	for _, c := range r.Readiness()[:3] {
		if c.State != "unknown" {
			t.Fatal("DNS outage became missing", c)
		}
	}
}

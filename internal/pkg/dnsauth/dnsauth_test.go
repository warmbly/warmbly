package dnsauth

import "testing"

func TestResultState(t *testing.T) {
	tests := []struct {
		name string
		res  Result
		want string
	}{
		{"empty domain is unknown", Result{Domain: ""}, "unknown"},
		{"transient lookup error is unknown even with records", Result{Domain: "acme.com", SPFFound: true, DMARCFound: true, LookupError: true}, "unknown"},
		{"spf and dmarc present is passing", Result{Domain: "acme.com", SPFFound: true, DMARCFound: true}, "passing"},
		{"dkim absent does not fail an otherwise-passing domain", Result{Domain: "acme.com", SPFFound: true, DMARCFound: true, DKIMFound: false}, "passing"},
		{"missing spf is failing", Result{Domain: "acme.com", SPFFound: false, DMARCFound: true}, "failing"},
		{"missing dmarc is failing", Result{Domain: "acme.com", SPFFound: true, DMARCFound: false}, "failing"},
		{"nothing found is failing", Result{Domain: "acme.com"}, "failing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.res.State(); got != tt.want {
				t.Errorf("State() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseDMARCPolicy(t *testing.T) {
	tests := []struct {
		record string
		want   string
	}{
		{"v=DMARC1; p=reject; rua=mailto:x@acme.com", "reject"},
		{"v=DMARC1; p=quarantine", "quarantine"},
		{"v=DMARC1;p=none", "none"},
		{"v=DMARC1; sp=reject", ""},
		{"v=DMARC1", ""},
	}
	for _, tt := range tests {
		t.Run(tt.record, func(t *testing.T) {
			if got := parseDMARCPolicy(tt.record); got != tt.want {
				t.Errorf("parseDMARCPolicy(%q) = %q, want %q", tt.record, got, tt.want)
			}
		})
	}
}

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
		{"inherited dmarc is passing", Result{Domain: "mail.acme.com", SPFFound: true, DMARCFound: true, DMARCInherited: true}, "passing"},
		{"p=none is compliant, not failing", Result{Domain: "acme.com", SPFFound: true, DMARCFound: true, DMARCPolicy: "none"}, "passing"},
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

func TestDMARCTag(t *testing.T) {
	tests := []struct {
		record string
		tag    string
		want   string
	}{
		{"v=DMARC1; p=reject; rua=mailto:x@acme.com", "p", "reject"},
		{"v=DMARC1; p=quarantine", "p", "quarantine"},
		{"v=DMARC1;p=none", "p", "none"},
		{"v=DMARC1; sp=reject", "p", ""},
		{"v=DMARC1", "p", ""},
		{"v=DMARC1; p=none; sp=reject", "sp", "reject"},
		{"v=DMARC1; p=reject", "sp", ""},
	}
	for _, tt := range tests {
		t.Run(tt.record+"/"+tt.tag, func(t *testing.T) {
			if got := dmarcTag(tt.record, tt.tag); got != tt.want {
				t.Errorf("dmarcTag(%q, %q) = %q, want %q", tt.record, tt.tag, got, tt.want)
			}
		})
	}
}

func TestOrganizationalDomain(t *testing.T) {
	tests := []struct {
		domain string
		want   string
	}{
		{"mail.acme.com", "acme.com"},
		{"go.outreach.acme.com", "acme.com"},
		{"acme.com", "acme.com"},
		{"acme.co.uk", "acme.co.uk"},
		{"mail.acme.co.uk", "acme.co.uk"},
		// A bare public suffix has no registrable domain above it.
		{"com", ""},
	}
	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			if got := organizationalDomain(tt.domain); got != tt.want {
				t.Errorf("organizationalDomain(%q) = %q, want %q", tt.domain, got, tt.want)
			}
		})
	}
}

// stubResolver serves TXT records from a map; any name absent from the map is
// an authoritative "not found". Names listed in transient always fail
// uncertainly, standing in for a timeout or SERVFAIL.
func stubResolver(records map[string][]string, transient ...string) lookupFunc {
	bad := map[string]bool{}
	for _, n := range transient {
		bad[n] = true
	}
	return func(name string) ([]string, bool) {
		if bad[name] {
			return nil, true
		}
		return records[name], false
	}
}

func TestCheckDMARCOrganizationalFallback(t *testing.T) {
	// The standard cold-outreach setup: a dedicated sending subdomain with its
	// own SPF, covered by the parent domain's DMARC record.
	res := checkWith("mail.acme.com", []string{"k1"}, stubResolver(map[string][]string{
		"mail.acme.com":        {"v=spf1 include:_spf.acme.com ~all"},
		"_dmarc.acme.com":      {"v=DMARC1; p=reject; rua=mailto:dmarc@acme.com"},
		"k1._domainkey.mail.a": nil,
	}))

	if !res.DMARCFound {
		t.Fatal("DMARCFound = false, want true (inherited from the organizational domain)")
	}
	if !res.DMARCInherited {
		t.Error("DMARCInherited = false, want true")
	}
	if res.DMARCDomain != "acme.com" {
		t.Errorf("DMARCDomain = %q, want %q", res.DMARCDomain, "acme.com")
	}
	if res.DMARCPolicy != "reject" {
		t.Errorf("DMARCPolicy = %q, want %q", res.DMARCPolicy, "reject")
	}
	if got := res.State(); got != "passing" {
		t.Errorf("State() = %q, want %q", got, "passing")
	}
}

func TestCheckDMARCInheritedPrefersSubdomainPolicy(t *testing.T) {
	// sp= is what the organizational domain publishes for its subdomains, so an
	// inherited policy must report sp= rather than p=.
	res := checkWith("mail.acme.com", nil, stubResolver(map[string][]string{
		"mail.acme.com":   {"v=spf1 -all"},
		"_dmarc.acme.com": {"v=DMARC1; p=none; sp=quarantine"},
	}))

	if res.DMARCPolicy != "quarantine" {
		t.Errorf("DMARCPolicy = %q, want %q (sp= wins for an inherited policy)", res.DMARCPolicy, "quarantine")
	}
}

func TestCheckDMARCOwnRecordWins(t *testing.T) {
	// A subdomain that publishes its own record is not inherited, and its p=
	// applies even when the parent says something else.
	res := checkWith("mail.acme.com", nil, stubResolver(map[string][]string{
		"mail.acme.com":        {"v=spf1 -all"},
		"_dmarc.mail.acme.com": {"v=DMARC1; p=quarantine"},
		"_dmarc.acme.com":      {"v=DMARC1; p=reject"},
	}))

	if res.DMARCInherited {
		t.Error("DMARCInherited = true, want false")
	}
	if res.DMARCDomain != "mail.acme.com" {
		t.Errorf("DMARCDomain = %q, want %q", res.DMARCDomain, "mail.acme.com")
	}
	if res.DMARCPolicy != "quarantine" {
		t.Errorf("DMARCPolicy = %q, want %q", res.DMARCPolicy, "quarantine")
	}
}

func TestCheckNoFallbackForOrganizationalDomain(t *testing.T) {
	// acme.com IS the organizational domain: a missing record there is a real
	// missing record, with nothing above it to inherit from.
	res := checkWith("acme.com", nil, stubResolver(map[string][]string{
		"acme.com": {"v=spf1 -all"},
	}))

	if res.DMARCFound {
		t.Error("DMARCFound = true, want false")
	}
	if got := res.State(); got != "failing" {
		t.Errorf("State() = %q, want %q", got, "failing")
	}
}

func TestCheckTransientDMARCLookupIsUnknown(t *testing.T) {
	// A resolver hiccup on the subdomain must not fall through to the parent:
	// we do not know whether the subdomain has its own record, so the verdict
	// is unknown rather than an inherited pass or a failing.
	res := checkWith("mail.acme.com", nil, stubResolver(map[string][]string{
		"mail.acme.com":   {"v=spf1 -all"},
		"_dmarc.acme.com": {"v=DMARC1; p=reject"},
	}, "_dmarc.mail.acme.com"))

	if res.DMARCFound {
		t.Error("DMARCFound = true, want false on a transient lookup")
	}
	if !res.LookupError {
		t.Error("LookupError = false, want true")
	}
	if got := res.State(); got != "unknown" {
		t.Errorf("State() = %q, want %q", got, "unknown")
	}
}

func TestCheckTransientFallbackLookupIsUnknown(t *testing.T) {
	res := checkWith("mail.acme.com", nil, stubResolver(map[string][]string{
		"mail.acme.com": {"v=spf1 -all"},
	}, "_dmarc.acme.com"))

	if !res.LookupError {
		t.Error("LookupError = false, want true")
	}
	if got := res.State(); got != "unknown" {
		t.Errorf("State() = %q, want %q", got, "unknown")
	}
}

func TestCheckSPFDoesNotInherit(t *testing.T) {
	// SPF is published per exact domain and never inherits, so a subdomain
	// without its own record fails even when the parent has one.
	res := checkWith("mail.acme.com", nil, stubResolver(map[string][]string{
		"acme.com":        {"v=spf1 -all"},
		"_dmarc.acme.com": {"v=DMARC1; p=reject"},
	}))

	if res.SPFFound {
		t.Error("SPFFound = true, want false (SPF does not inherit)")
	}
	if got := res.State(); got != "failing" {
		t.Errorf("State() = %q, want %q", got, "failing")
	}
}

func TestCheckTrailingDotAndCaseNormalized(t *testing.T) {
	res := checkWith("  MAIL.Acme.COM.  ", nil, stubResolver(map[string][]string{
		"mail.acme.com":   {"v=spf1 -all"},
		"_dmarc.acme.com": {"v=DMARC1; p=reject"},
	}))

	if res.Domain != "mail.acme.com" {
		t.Errorf("Domain = %q, want %q", res.Domain, "mail.acme.com")
	}
	if got := res.State(); got != "passing" {
		t.Errorf("State() = %q, want %q", got, "passing")
	}
}

func TestCheckReservedDomains(t *testing.T) {
	// Special-use domains never resolve, so every lookup returns an
	// authoritative "not found". Without the short circuit they would all read
	// as "failing" and a dev or demo instance would gate its own mailboxes.
	reserved := []string{
		"sunrise.test",
		"acme.invalid",
		"localhost",
		"mail.localhost",
		"printer.local",
		"foo.example",
		"router.home.arpa",
	}
	for _, d := range reserved {
		t.Run(d, func(t *testing.T) {
			res := checkWith(d, nil, stubResolver(nil))
			if !res.Reserved {
				t.Error("Reserved = false, want true")
			}
			if got := res.State(); got != "unknown" {
				t.Errorf("State() = %q, want %q", got, "unknown")
			}
		})
	}
}

func TestCheckReservedLookalikesAreStillChecked(t *testing.T) {
	// Only the suffix is special-use. A real domain that merely contains one of
	// those labels must be evaluated normally.
	for _, d := range []string{"test.com", "localhost.com", "example.com", "mytest.io", "local.dev"} {
		t.Run(d, func(t *testing.T) {
			if reservedDomain(d) {
				t.Errorf("reservedDomain(%q) = true, want false", d)
			}
		})
	}
}

func TestCheckEmptyDomain(t *testing.T) {
	res := checkWith("   ", nil, stubResolver(nil))
	if got := res.State(); got != "unknown" {
		t.Errorf("State() = %q, want %q", got, "unknown")
	}
	if res.Summary != "no domain to check" {
		t.Errorf("Summary = %q", res.Summary)
	}
}

func TestCheckDKIMSelectorsProbed(t *testing.T) {
	res := checkWith("acme.com", []string{"missing", "s1"}, stubResolver(map[string][]string{
		"acme.com":                 {"v=spf1 -all"},
		"_dmarc.acme.com":          {"v=DMARC1; p=reject"},
		"s1._domainkey.acme.com":   {"v=DKIM1; k=rsa; p=MIGf"},
		"none._domainkey.acme.com": {"v=DKIM1"},
	}))

	if !res.DKIMFound {
		t.Fatal("DKIMFound = false, want true")
	}
	if len(res.DKIMSelectors) != 1 || res.DKIMSelectors[0] != "s1" {
		t.Errorf("DKIMSelectors = %v, want [s1]", res.DKIMSelectors)
	}
	if !res.AllAligned {
		t.Error("AllAligned = false, want true")
	}
}

func TestSummaryNamesInheritedSource(t *testing.T) {
	res := checkWith("mail.acme.com", []string{"s1"}, stubResolver(map[string][]string{
		"mail.acme.com":               {"v=spf1 -all"},
		"_dmarc.acme.com":             {"v=DMARC1; p=reject"},
		"s1._domainkey.mail.acme.com": {"v=DKIM1; k=rsa"},
	}))

	want := "SPF, DKIM and DMARC all present (DMARC policy: reject), inherited from acme.com"
	if res.Summary != want {
		t.Errorf("Summary = %q, want %q", res.Summary, want)
	}
}

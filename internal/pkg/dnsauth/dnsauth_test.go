package dnsauth

import (
	"strings"
	"testing"
)

func TestResultState(t *testing.T) {
	tests := []struct {
		name string
		res  Result
		want string
	}{
		{"empty domain is unknown", Result{Domain: ""}, "unknown"},
		{"transient lookup error is unknown even with records", Result{Domain: "acme.com", SPFFound: true, DMARCFound: true, LookupError: true}, "unknown"},
		{"spf and dmarc present is passing", Result{Domain: "acme.com", SPFFound: true, DMARCFound: true}, "passing"},
		{"dkim unverified does not fail an otherwise-passing domain", Result{Domain: "acme.com", SPFFound: true, DMARCFound: true, DKIMFound: false}, "passing"},
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

// stubResolver serves TXT records from a map and answers no MX; any name absent
// from the map is an authoritative "not found". Names listed in transient always
// fail uncertainly, standing in for a timeout or SERVFAIL.
func stubResolver(records map[string][]string, transient ...string) lookups {
	bad := map[string]bool{}
	for _, n := range transient {
		bad[n] = true
	}
	return lookups{txt: func(name string) ([]string, bool) {
		if bad[name] {
			return nil, true
		}
		return records[name], false
	}}
}

// stubResolverMX is stubResolver with MX records, for the selector hints the
// check derives from whoever handles the domain's mail.
func stubResolverMX(records map[string][]string, mx []string) lookups {
	l := stubResolver(records)
	l.mx = func(string) ([]string, bool) { return mx, false }
	return l
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
		"s1._domainkey.mail.acme.com": {"v=DKIM1; k=rsa; p=MIGf"},
	}))

	want := "SPF, DKIM and DMARC all present (DMARC policy: reject), inherited from acme.com"
	if res.Summary != want {
		t.Errorf("Summary = %q, want %q", res.Summary, want)
	}
}

func TestDKIMKeyRecord(t *testing.T) {
	tests := []struct {
		name   string
		record string
		want   bool
	}{
		{"full record", "v=DKIM1; k=rsa; p=MIGfMA0GCSq", true},
		{"no version, k and p present", "k=rsa; p=MIGfMA0GCSq", true},
		{"ed25519 key", "v=DKIM1; k=ed25519; p=11qYAYKxCrf", true},
		{"base64 padding survives the tag split", "v=DKIM1; p=MIGfMA0GCSq==", true},
		// An empty p= is a revoked key: the selector exists and signs nothing.
		{"revoked key", "v=DKIM1; k=rsa; p=", false},
		{"policy record, no key", "v=DKIM1; t=y", false},
		{"someone else's TXT record", "v=spf1 include:_spf.google.com ~all", false},
		{"wrong version", "v=DKIM2; p=MIGf", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dkimKey(tt.record); got != tt.want {
				t.Errorf("dkimKey(%q) = %v, want %v", tt.record, got, tt.want)
			}
		})
	}
}

func TestCheckDKIMUndeterminedIsNotMissing(t *testing.T) {
	// The whole point: a domain whose DKIM sits at a selector we did not guess
	// must read as unverified, never as missing, and must not drag the summary
	// or the verdict down with it.
	res := checkWith("acme.com", nil, stubResolver(map[string][]string{
		"acme.com":                     {"v=spf1 include:_spf.example-esp.net -all"},
		"_dmarc.acme.com":              {"v=DMARC1; p=reject"},
		"aq6y2b4c._domainkey.acme.com": {"v=DKIM1; k=rsa; p=MIGf"},
	}))

	if res.DKIMFound {
		t.Error("DKIMFound = true, want false")
	}
	if res.DKIMStatus != DKIMStatusUndetermined {
		t.Errorf("DKIMStatus = %q, want %q", res.DKIMStatus, DKIMStatusUndetermined)
	}
	if got := res.State(); got != "passing" {
		t.Errorf("State() = %q, want %q", got, "passing")
	}
	if strings.Contains(res.Summary, "missing") {
		t.Errorf("Summary = %q, must not call an unverified DKIM missing", res.Summary)
	}
	if !strings.Contains(res.Summary, "DKIM not verified") {
		t.Errorf("Summary = %q, want it to say DKIM was not verified", res.Summary)
	}
}

func TestCheckRevokedDKIMKeyIsNotFound(t *testing.T) {
	res := checkWith("acme.com", nil, stubResolver(map[string][]string{
		"acme.com":                    {"v=spf1 -all"},
		"_dmarc.acme.com":             {"v=DMARC1; p=none"},
		"default._domainkey.acme.com": {"v=DKIM1; k=rsa; p="},
	}))

	if res.DKIMFound {
		t.Error("DKIMFound = true, want false: a p= with no key is revoked and signs nothing")
	}
}

func TestCheckSelectorHintsFromSPF(t *testing.T) {
	// Google Workspace publishes at "google", which is in the default set
	// anyway; Zoho's "zmail" is not, and the SPF record is what names it.
	res := checkWith("acme.com", nil, stubResolver(map[string][]string{
		"acme.com":                  {"v=spf1 include:zoho.eu ~all"},
		"_dmarc.acme.com":           {"v=DMARC1; p=none"},
		"zmail._domainkey.acme.com": {"v=DKIM1; k=rsa; p=MIGf"},
	}))

	if !res.DKIMFound {
		t.Fatal("DKIMFound = false, want true (zmail derived from the SPF include)")
	}
	if res.DKIMStatus != DKIMStatusFound {
		t.Errorf("DKIMStatus = %q, want %q", res.DKIMStatus, DKIMStatusFound)
	}
}

func TestCheckSelectorHintsFromMX(t *testing.T) {
	// An SMTP/IMAP mailbox on a provider whose selector nobody would guess.
	// The MX record names the provider, and the provider fixes the selector.
	res := checkWith("acme.com", nil, stubResolverMX(map[string][]string{
		"acme.com":                           {"v=spf1 -all"},
		"_dmarc.acme.com":                    {"v=DMARC1; p=none"},
		"hostingermail1._domainkey.acme.com": {"v=DKIM1; k=rsa; p=MIGf"},
	}, []string{"mx1.hostinger.com", "mx2.hostinger.com"}))

	if !res.DKIMFound {
		t.Fatal("DKIMFound = false, want true (selector derived from the MX host)")
	}
	if len(res.DKIMSelectors) != 1 || res.DKIMSelectors[0] != "hostingermail1" {
		t.Errorf("DKIMSelectors = %v, want [hostingermail1]", res.DKIMSelectors)
	}
}

func TestSelectorHints(t *testing.T) {
	got := selectorHints("v=spf1 include:_spf.google.com include:spf.protection.outlook.com -all", []string{"mx.zoho.com"})
	want := map[string]bool{"google": true, "selector1": true, "selector2": true, "zoho": true, "zmail": true}
	if len(got) != len(want) {
		t.Fatalf("selectorHints() = %v, want %d entries", got, len(want))
	}
	for _, s := range got {
		if !want[s] {
			t.Errorf("selectorHints() returned unexpected selector %q", s)
		}
	}
}

func TestDedupeKeepsFirstOccurrence(t *testing.T) {
	got := dedupe([]string{"Google", " google ", "", "selector1", "google"})
	if len(got) != 2 || got[0] != "google" || got[1] != "selector1" {
		t.Errorf("dedupe() = %v, want [google selector1]", got)
	}
}

func TestSummaryDoesNotAccuseOnATransientLookup(t *testing.T) {
	// The verdict is already "unknown" here. The summary is persisted as
	// auth_reason and shown next to it, so it has to agree: a resolver that
	// never answered has not found anything missing.
	res := checkWith("acme.com", nil, stubResolver(nil, "acme.com", "_dmarc.acme.com"))

	if got := res.State(); got != "unknown" {
		t.Fatalf("State() = %q, want %q", got, "unknown")
	}
	if strings.Contains(res.Summary, "missing") {
		t.Errorf("Summary = %q, must not report records missing when DNS did not answer", res.Summary)
	}
}

func TestSummaryMissingNamesOnlyDiscoverableRecords(t *testing.T) {
	res := checkWith("acme.com", nil, stubResolver(nil))
	if res.Summary != "missing: SPF and DMARC" {
		t.Errorf("Summary = %q, want %q", res.Summary, "missing: SPF and DMARC")
	}
}

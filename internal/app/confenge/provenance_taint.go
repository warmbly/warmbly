package confenge

import (
	"regexp"
	"strings"
)

// Provenance taint defense-in-depth at Warmbly import/authorization.
// Primary trust is recomputed by extra-cli; Warmbly never trusts sticky
// VERIFIED / COMPANY_OWNED alone when channel looks demo/fixture/synthetic.

var (
	demoDomainRes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^demo\d*obra\.com\.br$`),
		regexp.MustCompile(`(?i)^demo\d+\.`),
		regexp.MustCompile(`(?i)\.demo\.`),
		regexp.MustCompile(`(?i)(^|\.)example\.(com|org|net)$`),
		regexp.MustCompile(`(?i)^test\.`),
		regexp.MustCompile(`(?i)\.test$`),
		regexp.MustCompile(`(?i)\.local$`),
		regexp.MustCompile(`(?i)\.localhost$`),
		regexp.MustCompile(`(?i)^localhost$`),
		regexp.MustCompile(`(?i)warmbly\.local$`),
		regexp.MustCompile(`(?i)^fixture[.\-]`),
		regexp.MustCompile(`(?i)^synthetic[.\-]`),
		regexp.MustCompile(`(?i)^fake[.\-]`),
		regexp.MustCompile(`(?i)^sample[.\-]`),
		regexp.MustCompile(`(?i)^mock[.\-]`),
	}
	demoEmailLocalRes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^test@`),
		regexp.MustCompile(`(?i)^demo@`),
		regexp.MustCompile(`(?i)^fixture@`),
		regexp.MustCompile(`(?i)^fake@`),
		regexp.MustCompile(`(?i)^synthetic@`),
		regexp.MustCompile(`(?i)^example@`),
	}
	// URL markers for clear synthetic hosts/paths. Do NOT include bare
	// "example.com" as a substring — unit fixtures use acme.example.com etc.
	fixtureURLMarkers = []string{
		"fixture",
		"/fixtures/",
		"://example.com",
		"://example.org",
		"://example.net",
		"@example.com",
		"@example.org",
		"demo000obra",
		"demo001obra",
		"demo002obra",
		"demo003obra",
		"demo004obra",
		"demo005obra",
		"demo006obra",
		"demo007obra",
		"demo008obra",
		"demo009obra",
		"warmbly.local",
		"localhost",
		"127.0.0.1",
		"synthetic",
		"fake-contact",
	}
	taintedRootTypes = map[string]struct{}{
		"TEST_FIXTURE":          {},
		"DEMO":                  {},
		"SYNTHETIC":             {},
		"DERIVED_UNTRUSTED":     {},
		"UNKNOWN":               {},
		"FIXTURE":               {},
		"PROVENANCE_TAINT":      {},
		"PROVENANCE_TAINT_DEMO": {},
	}
)

// IsDemoOrFixtureDomain reports whether a host looks like test/demo/fixture.
func IsDemoOrFixtureDomain(domain string) bool {
	d := strings.ToLower(strings.TrimSpace(domain))
	d = strings.TrimPrefix(d, "www.")
	if d == "" {
		return false
	}
	for _, rx := range demoDomainRes {
		if rx.MatchString(d) {
			return true
		}
	}
	return false
}

// IsDemoOrFixtureEmail reports demo/test/fixture emails.
func IsDemoOrFixtureEmail(email string) bool {
	e := strings.ToLower(strings.TrimSpace(email))
	if e == "" {
		return false
	}
	for _, rx := range demoEmailLocalRes {
		if rx.MatchString(e) {
			return true
		}
	}
	if i := strings.LastIndex(e, "@"); i >= 0 && i+1 < len(e) {
		return IsDemoOrFixtureDomain(e[i+1:])
	}
	return false
}

func looksFixtureURL(url string) bool {
	u := strings.ToLower(strings.TrimSpace(url))
	if u == "" {
		return false
	}
	for _, m := range fixtureURLMarkers {
		if strings.Contains(u, m) {
			return true
		}
	}
	return false
}

// ContactProvenanceTainted is fail-closed import/authorization gate for tainted channels.
// Returns (tainted, reason).
func ContactProvenanceTainted(email, sourceURL, rootSourceType, verificationReason string, derivedFromFixture bool) (bool, string) {
	if derivedFromFixture {
		return true, "derived_from_fixture"
	}
	if IsDemoOrFixtureEmail(email) {
		return true, "demo_or_fixture_email"
	}
	if looksFixtureURL(sourceURL) {
		return true, "fixture_source_url"
	}
	rst := strings.ToUpper(strings.TrimSpace(rootSourceType))
	if _, ok := taintedRootTypes[rst]; ok {
		return true, "root_source_type:" + rst
	}
	vr := strings.ToUpper(strings.TrimSpace(verificationReason))
	if strings.Contains(vr, "PROVENANCE_TAINT") || strings.Contains(vr, "FIXTURE") || strings.Contains(vr, "DEMO") {
		return true, "verification_reason:" + vr
	}
	// Explicit suitability from extra-cli
	return false, ""
}

// FeedContactProvenanceTainted applies taint rules to an imported feed contact.
func FeedContactProvenanceTainted(fc FeedContact) (bool, string) {
	root := ""
	derived := false
	// Optional additive fields (ignored by older feeds).
	if fc.RecipientCommercialSuitability == "UNSUITABLE_PROVENANCE" {
		return true, "unsuitable_provenance"
	}
	// Source URL / email
	if t, reason := ContactProvenanceTainted(fc.Email, fc.SourceURL, root, fc.VerificationStatus, derived); t {
		return true, reason
	}
	// Sticky VERIFIED on demo domain still blocked above via email domain.
	return false, ""
}

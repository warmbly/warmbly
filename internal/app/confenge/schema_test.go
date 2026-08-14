package confenge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestParseAndValidateNativeFeed(t *testing.T) {
	raw := mustReadFixture(t, "native_feed_v1.json")
	feed, err := ParseFeed(raw)
	if err != nil {
		t.Fatalf("ParseFeed: %v", err)
	}
	if err := ValidateFeed(feed); err != nil {
		t.Fatalf("ValidateFeed: %v", err)
	}
	if feed.SchemaVersion != models.OutreachSchemaV1 {
		t.Fatalf("schema %q", feed.SchemaVersion)
	}
	if len(feed.Leads) < 5 {
		t.Fatalf("want >=5 leads, got %d", len(feed.Leads))
	}
	// Company without email must validate (NEEDS_CONTACT later).
	var noEmail bool
	for i, lead := range feed.Leads {
		if lv := ValidateLead(i, lead); lv != nil {
			t.Fatalf("lead %d invalid: %s", i, lv.Message)
		}
		if !hasEnrollableContact(lead) {
			noEmail = true
			if DefaultQueueState(lead, nil) != models.OutreachQueueNeedsContact {
				t.Fatalf("lead without email should be NEEDS_CONTACT")
			}
		}
	}
	if !noEmail {
		t.Fatal("fixture should include a lead without enrollable contact")
	}
}

func TestRejectInvalidSchemaVersion(t *testing.T) {
	raw := []byte(`{"schema_version":"nope","source":{"system":"x"},"leads":[]}`)
	feed, err := ParseFeed(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateFeed(feed); err == nil {
		t.Fatal("expected schema version error")
	}
}

func TestRejectBadCNPJ(t *testing.T) {
	lead := FeedLead{
		SourceLeadID: "x",
		Company:      FeedCompany{CNPJ14: "123", RazaoSocial: "ACME"},
	}
	if lv := ValidateLead(0, lead); lv == nil {
		t.Fatal("expected cnpj error")
	}
}

func TestCanonicalPayloadHashStable(t *testing.T) {
	raw := mustReadFixture(t, "native_feed_v1.json")
	a := CanonicalPayloadHash(raw)
	b := CanonicalPayloadHash(raw)
	if a != b || len(a) != 64 {
		t.Fatalf("hash unstable or wrong length: %s", a)
	}
	other := append([]byte{}, raw...)
	other[len(other)-2] = 'x'
	if CanonicalPayloadHash(other) == a {
		t.Fatal("different payload should hash differently")
	}
}

func TestLeadContentHashChangesWithScore(t *testing.T) {
	lead := FeedLead{
		SourceLeadID: "1",
		Company:      FeedCompany{CNPJ14: "11222333000181", RazaoSocial: "ACME"},
		Priority:     FeedPriority{Score: 10},
	}
	h1 := LeadContentHash(lead)
	lead.Priority.Score = 20
	h2 := LeadContentHash(lead)
	if h1 == h2 {
		t.Fatal("score change must change content hash")
	}
}

func TestSanitizeTextStripsTagsAndControl(t *testing.T) {
	in := "<script>alert(1)</script>Hello\x00World"
	out := SanitizeText(in, 100)
	if out != "alert(1)HelloWorld" && out != "HelloWorld" {
		// stripTags leaves inner text of script; control stripped
		if containsAngle(out) {
			t.Fatalf("tags remain: %q", out)
		}
	}
	if containsAngle(out) {
		t.Fatalf("angle brackets remain: %q", out)
	}
}

func containsAngle(s string) bool {
	for _, r := range s {
		if r == '<' || r == '>' {
			return true
		}
	}
	return false
}

func TestUnenrollableVerification(t *testing.T) {
	cases := []struct {
		vs   string
		want bool
	}{
		{models.OutreachVerifyOfficialSource, true},
		{models.OutreachVerifyInstitutionalGeneric, true},
		{models.OutreachVerifyHumanConfirmed, true},
		{models.OutreachVerifyVerified, true},
		{models.OutreachVerifyCandidateUnverified, false},
		{models.OutreachVerifyNotFound, false},
		{models.OutreachVerifyInvalid, false},
		{models.OutreachVerifyBounced, false},
		{models.OutreachVerifyDoNotContact, false},
	}
	for _, tc := range cases {
		c := &models.OutreachContactCandidate{
			Email:              "a@example.com",
			VerificationStatus: tc.vs,
		}
		if c.CanEnroll() != tc.want {
			t.Errorf("%s CanEnroll=%v want %v", tc.vs, c.CanEnroll(), tc.want)
		}
	}
}

func TestDefaultQueueStatePreservesDNC(t *testing.T) {
	lead := FeedLead{
		Company:  FeedCompany{CNPJ14: "11222333000181", RazaoSocial: "X"},
		Contacts: []FeedContact{{Email: "a@example.com", VerificationStatus: models.OutreachVerifyOfficialSource}},
	}
	existing := &models.OutreachAccount{
		DoNotContact: true,
		QueueState:   models.OutreachQueueDoNotContact,
	}
	if DefaultQueueState(lead, existing) != models.OutreachQueueDoNotContact {
		t.Fatal("DNC must be preserved on reimport")
	}
}

func TestDefaultQueueStatePreservesSent(t *testing.T) {
	lead := FeedLead{
		Company:  FeedCompany{CNPJ14: "11222333000181", RazaoSocial: "X"},
		Contacts: []FeedContact{{Email: "a@example.com", VerificationStatus: models.OutreachVerifyOfficialSource}},
	}
	existing := &models.OutreachAccount{QueueState: models.OutreachQueueSent}
	if DefaultQueueState(lead, existing) != models.OutreachQueueSent {
		t.Fatal("SENT must not restart on reimport")
	}
}

func TestLegacyLeadsArrayNormalizes(t *testing.T) {
	raw := mustReadFixture(t, "legacy_leads_array.json")
	feed, err := DetectAndNormalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	if feed.SchemaVersion != models.OutreachSchemaV1 {
		t.Fatalf("normalized schema %q", feed.SchemaVersion)
	}
	if len(feed.Leads) != 3 {
		t.Fatalf("want 3 leads, got %d", len(feed.Leads))
	}
	// No invented email on contactless company
	var missing bool
	for _, l := range feed.Leads {
		if !hasEnrollableContact(l) {
			missing = true
		}
		if NormalizeCNPJ14(l.Company.CNPJ14) == "" {
			t.Fatalf("cnpj not normalized: %q", l.Company.CNPJ14)
		}
	}
	if !missing {
		t.Fatal("legacy fixture should include company without contact")
	}
}

func TestLegacyDoesNotInventMissingFields(t *testing.T) {
	raw := []byte(`[{"cnpj14":"11222333000181","razao_social":"Only Name"}]`)
	feed, err := DetectAndNormalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	l := feed.Leads[0]
	if l.Offer.ServiceCode != "" || l.MessagingContext.FactToMention != "" {
		t.Fatalf("must not invent offer/fact: %+v", l)
	}
	if len(l.Contacts) != 0 {
		t.Fatalf("must not invent contacts: %+v", l.Contacts)
	}
	if feed.GeneratedAt != "" || !feed.Legacy {
		t.Fatalf("legacy feed must not invent authoritative freshness: %+v", feed)
	}
}

func TestHostAllowlist(t *testing.T) {
	if !hostAllowed("feed.example.com", []string{"example.com"}) {
		t.Fatal("subdomain should match parent allow")
	}
	if hostAllowed("evil.com", []string{"example.com"}) {
		t.Fatal("unrelated host must not match")
	}
	if !hostAllowed("example.com", []string{"example.com"}) {
		t.Fatal("exact host should match")
	}
}

func TestConfigDefaultsOff(t *testing.T) {
	t.Setenv(EnvEnabled, "")
	t.Setenv(EnvAutoSend, "")
	cfg := LoadConfig()
	if cfg.Enabled || cfg.AutoSendEnabled {
		t.Fatal("feature must default off")
	}
	if !cfg.RequireHumanApproval {
		t.Fatal("human approval must default true")
	}
}

func TestConfigValidateProdHTTPS(t *testing.T) {
	cfg := Config{
		Enabled:      true,
		FeedURL:      "http://insecure.example.com/feed",
		AllowedHosts: []string{"insecure.example.com"},
	}
	if err := cfg.ValidateStartup("prod"); err == nil {
		t.Fatal("prod must require https feed")
	}
	cfg.FeedURL = "https://feed.example.com/x"
	cfg.AllowedHosts = nil
	if err := cfg.ValidateStartup("prod"); err == nil {
		t.Fatal("prod must require allowlist when feed set")
	}
	cfg.FeedURL = ""
	cfg.ManifestURL = "http://manifest.example.com/manifest.json"
	cfg.AllowedHosts = []string{"manifest.example.com"}
	if err := cfg.ValidateStartup("prod"); err == nil {
		t.Fatal("prod must require https manifest")
	}
	cfg.ManifestURL = "https://other.example.com/manifest.json"
	if err := cfg.ValidateStartup("prod"); err == nil {
		t.Fatal("prod must require manifest host allowlist")
	}
}

func TestConfigValidateStartupRejectsUnsafeOperatorAutomation(t *testing.T) {
	base := Config{
		Enabled: true, OperatorMode: true, OperatorUserID: uuid.New(), OperatorOrgID: uuid.New(),
		RequireHumanApproval: true, DefaultDailyLimit: 200, MaxInitialEmailWords: 120,
	}
	t.Setenv("APP_URL", "http://127.0.0.1:5173")
	auto := base
	auto.AutoSendEnabled = true
	if err := auto.ValidateStartup("production"); err == nil {
		t.Fatal("operator startup must reject auto-send")
	}
	noHuman := base
	noHuman.RequireHumanApproval = false
	if err := noHuman.ValidateStartup("production"); err == nil {
		t.Fatal("operator startup must require human approval")
	}
	green := base
	green.GreenAutorunEnabled = true
	if err := green.ValidateStartup("production"); err == nil {
		t.Fatal("operator startup must reject green autorun")
	}
}

func TestForbiddenWordsNotInFixtureSubjects(t *testing.T) {
	// Structural: native fixture messaging must not use prohibited outreach phrases.
	raw := mustReadFixture(t, "native_feed_v1.json")
	var feed Feed
	if err := json.Unmarshal(raw, &feed); err != nil {
		t.Fatal(err)
	}
	banned := []string{"dinheiro a receber", "crédito identificado", "lead quente"}
	for _, l := range feed.Leads {
		blob := l.MessagingContext.FactToMention + l.MessagingContext.QuestionToAsk + l.MessagingContext.CTA
		for _, b := range banned {
			if containsFold(blob, b) {
				t.Fatalf("fixture contains banned phrase %q", b)
			}
		}
	}
}

func containsFold(s, sub string) bool {
	return len(sub) > 0 && (s == sub || len(s) >= len(sub) &&
		(json.Valid([]byte(`"`+s+`"`)) && // keep compiler happy path
			false || stringContainsFold(s, sub)))
}

func stringContainsFold(s, sub string) bool {
	// simple ASCII fold
	ls, lsub := []rune(s), []rune(sub)
	for i := 0; i+len(lsub) <= len(ls); i++ {
		match := true
		for j := range lsub {
			a, b := ls[i+j], lsub[j]
			if a >= 'A' && a <= 'Z' {
				a += 32
			}
			if b >= 'A' && b <= 'Z' {
				b += 32
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join("testdata", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

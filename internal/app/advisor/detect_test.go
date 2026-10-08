package advisor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/copyjudge"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/typesafe"
	"github.com/warmbly/warmbly/internal/repository"
)

// The detectors ARE the product here, so what is worth pinning is not that the
// code runs but that the thresholds hold: silence below the sample floor,
// escalation at the documented bands, and no advice invented from thin data.

func defaults() *models.AdvisorSettings {
	return models.DefaultAdvisorSettings(uuid.New())
}

// healthyMailbox is a mailbox nothing should fire on: proven volume, clean
// rates, authenticated, warming, at the default cap and gap.
func healthyMailbox() repository.AdvisorMailbox {
	return repository.AdvisorMailbox{
		ID:                     uuid.New(),
		Email:                  "sender@acme.com",
		Status:                 "active",
		Provider:               "google",
		AgeDays:                120,
		CampaignLimit:          defaultColdCap,
		MinWaitTime:            defaultMinGap,
		TrackingDomain:         "track.acme.com",
		TrackingDomainVerified: true,
		AuthState:              "passing",
		AuthSPF:                true,
		AuthDKIM:               true,
		AuthDMARC:              true,
		WarmupActive:           true,
		WarmupBase:             defaultWarmupBase,
		WarmupMax:              defaultWarmupMax,
		WarmupReplyRate:        30,
		WarmupPoolType:         "premium",
		PoolHealth:             "healthy",
		ColdSent30d:            1000,
		ColdSent7d:             230,
		WarmupSent7d:           200,
		InActiveCampaign:       true,
	}
}

func snapshotOf(mailboxes ...repository.AdvisorMailbox) *repository.AdvisorSnapshot {
	return &repository.AdvisorSnapshot{
		OrganizationID: uuid.New(),
		Now:            time.Now(),
		Mailboxes:      mailboxes,
		Lists:          map[uuid.UUID]repository.AdvisorListStats{},
	}
}

// findingsByKey indexes a detection pass for assertions.
func findingsByKey(findings []Finding) map[string]Finding {
	out := map[string]Finding{}
	for _, f := range findings {
		out[f.Key] = f
	}
	return out
}

func TestHealthyMailboxProducesNoFindings(t *testing.T) {
	got := Detect(snapshotOf(healthyMailbox()), defaults())
	if len(got) != 0 {
		for _, f := range got {
			t.Errorf("unexpected finding on a healthy mailbox: %s (%s) — %s", f.Key, f.Severity, f.Title)
		}
	}
}

func TestComplaintRateRespectsSampleFloor(t *testing.T) {
	// Two complaints out of 40 sends is a 5% complaint rate, which would be
	// catastrophic if it were real. It is not real: it is 40 sends. Advice on
	// that sample would be wrong more often than right.
	m := healthyMailbox()
	m.ColdSent30d = minSendsForComplaintRate - 1
	m.Complaints30d = 2

	if _, fired := findingsByKey(Detect(snapshotOf(m), defaults()))["mailbox_complaint_rate"]; fired {
		t.Fatalf("complaint detector fired below its %d-send sample floor", minSendsForComplaintRate)
	}

	// Same rate, enough volume to mean something.
	m.ColdSent30d = 2000
	m.Complaints30d = 4 // 0.2%: above Google's 0.1% ceiling.
	f, fired := findingsByKey(Detect(snapshotOf(m), defaults()))["mailbox_complaint_rate"]
	if !fired {
		t.Fatal("complaint detector did not fire at 0.2% over 2000 sends")
	}
	if f.Severity != models.AdvisorCritical {
		t.Errorf("complaint rate past the 0.10%% band should be critical, got %s", f.Severity)
	}
	if f.Action == nil {
		t.Error("a complaint-rate finding should offer a one-click volume cut")
	}
}

func TestComplaintRateWarnBandIsHighNotCritical(t *testing.T) {
	m := healthyMailbox()
	m.ColdSent30d = 2000
	m.Complaints30d = 1 // 0.05%: over the 0.03% watch line, under 0.10%.

	f, fired := findingsByKey(Detect(snapshotOf(m), defaults()))["mailbox_complaint_rate"]
	if !fired {
		t.Fatal("complaint detector did not fire in the warning band")
	}
	if f.Severity != models.AdvisorHigh {
		t.Errorf("warning band should be high, got %s", f.Severity)
	}
}

func TestBounceRateEscalatesAtTheSESBand(t *testing.T) {
	m := healthyMailbox()
	m.ColdSent30d = 1000

	m.Bounces30d = 35 // 3.5%: over the watch line, under SES's 5% review band.
	if f := findingsByKey(Detect(snapshotOf(m), defaults()))["mailbox_bounce_rate"]; f.Severity != models.AdvisorHigh {
		t.Errorf("3.5%% bounce should be high, got %q", f.Severity)
	}

	m.Bounces30d = 80 // 8%: past the review band, heading for the pause band.
	if f := findingsByKey(Detect(snapshotOf(m), defaults()))["mailbox_bounce_rate"]; f.Severity != models.AdvisorCritical {
		t.Errorf("8%% bounce should be critical, got %q", f.Severity)
	}
}

func TestUnknownAuthStateIsNotReportedAsFailing(t *testing.T) {
	// A freshly connected mailbox has not been swept yet. Telling someone their
	// DNS is broken when we simply have not looked is the fastest way to make
	// the whole feature untrustworthy.
	m := healthyMailbox()
	m.AuthState = "unknown"
	m.AuthSPF, m.AuthDKIM, m.AuthDMARC = false, false, false

	if _, fired := findingsByKey(Detect(snapshotOf(m), defaults()))["mailbox_domain_auth"]; fired {
		t.Fatal("auth detector fired on an unchecked domain")
	}

	m.AuthState = "failing"
	f, fired := findingsByKey(Detect(snapshotOf(m), defaults()))["mailbox_domain_auth"]
	if !fired {
		t.Fatal("auth detector did not fire on a domain known to be failing")
	}
	if f.Severity != models.AdvisorCritical {
		t.Errorf("unauthenticated mail on a sending mailbox should be critical, got %s", f.Severity)
	}
}

func TestUnverifiedDKIMAloneIsNotAFinding(t *testing.T) {
	// A DKIM selector is not discoverable from DNS, so auth_dkim=false means
	// "no key answered at the selectors we probed", not "this domain has none".
	// A domain with SPF and DMARC in place is passing, and telling its owner
	// they are missing DKIM is a false alarm they cannot act on.
	m := healthyMailbox()
	m.AuthState = "passing"
	m.AuthSPF, m.AuthDMARC = true, true
	m.AuthDKIM = false

	if f, fired := findingsByKey(Detect(snapshotOf(m), defaults()))["mailbox_domain_auth"]; fired {
		t.Fatalf("auth detector fired on unverified DKIM alone: %q", f.Title)
	}
}

func TestUnverifiedDKIMIsNotNamedAsMissing(t *testing.T) {
	// It still gets a step and the host to publish at, because the owner is
	// already in their DNS panel for the record that IS missing. It just must
	// not be counted among the missing records.
	m := healthyMailbox()
	m.AuthState = "failing"
	m.AuthSPF, m.AuthDMARC, m.AuthDKIM = true, false, false

	f, fired := findingsByKey(Detect(snapshotOf(m), defaults()))["mailbox_domain_auth"]
	if !fired {
		t.Fatal("auth detector did not fire on a domain with no DMARC record")
	}
	if strings.Contains(f.Title, "DKIM") {
		t.Errorf("Title = %q, must not name DKIM as missing", f.Title)
	}
	if !strings.Contains(f.Detail, "DMARC") || strings.Contains(f.Detail, "DKIM record") {
		t.Errorf("Detail = %q", f.Detail)
	}
	var hasDKIMHost bool
	for _, s := range f.Snippets {
		if s.Label == "DKIM host" {
			hasDKIMHost = true
		}
	}
	if !hasDKIMHost {
		t.Error("an unverified DKIM should still offer the host to publish at")
	}
}

func TestNewMailboxAtFullVolumeIsFlagged(t *testing.T) {
	m := healthyMailbox()
	m.AgeDays = 3
	m.ColdSent30d = 0
	m.ColdSent7d = 0

	f, fired := findingsByKey(Detect(snapshotOf(m), defaults()))["mailbox_new_ramping_fast"]
	if !fired {
		t.Fatal("a three-day-old mailbox at the full default cap should be flagged")
	}
	if f.Action == nil || f.Action.Undo == nil {
		t.Fatal("the ramp fix should be applyable and undoable")
	}
}

func TestWarmupOffWhileSendingIsCriticalOnANewMailbox(t *testing.T) {
	m := healthyMailbox()
	m.WarmupActive = false
	m.AgeDays = 10
	m.CampaignLimit = newMailboxSafeCap // isolate from the ramp detector

	got := findingsByKey(Detect(snapshotOf(m), defaults()))
	f, fired := got["warmup_off_while_sending"]
	if !fired {
		t.Fatal("cold sending with warmup off should be flagged")
	}
	if f.Severity != models.AdvisorCritical {
		t.Errorf("no warmup on a new sending mailbox should be critical, got %s", f.Severity)
	}

	// An idle mailbox with warmup off is a setup choice, not a problem.
	m.InActiveCampaign = false
	if _, fired := findingsByKey(Detect(snapshotOf(m), defaults()))["warmup_off_while_sending"]; fired {
		t.Error("warmup-off fired on a mailbox that is not sending cold mail")
	}
}

func TestSpamPlacementRespectsItsSampleFloorAndBands(t *testing.T) {
	m := healthyMailbox()
	m.WarmupPlacement = models.WarmupPlacementEvidence{MajorDelivered: minWarmupDeliveriesForPlacement - 1, MajorSpam: 5}

	if _, fired := findingsByKey(Detect(snapshotOf(m), defaults()))["mailbox_spam_placement"]; fired {
		t.Fatal("placement detector fired below its delivery floor")
	}

	m.WarmupPlacement = models.WarmupPlacementEvidence{MajorDelivered: 100, MajorSpam: 55} // past the quarantine line
	f := findingsByKey(Detect(snapshotOf(m), defaults()))["mailbox_spam_placement"]
	if f.Severity != models.AdvisorCritical {
		t.Errorf("55%% spam placement should be critical, got %q", f.Severity)
	}
	if f.Action == nil {
		t.Fatal("a mailbox this deep in spam while sending cold should offer to stop")
	}
	// Stopping cold sending must leave warmup running, so the fix is the hold, never status.
	if f.Action.Tool != "set_mailbox_send_hold" || f.Action.Undo == nil || f.Action.Undo.Tool != "set_mailbox_send_hold" {
		t.Errorf("spam placement fix should hold the mailbox and undo by releasing it, got %q", f.Action.Tool)
	}
	if strings.Contains(string(f.Action.Args), "status") {
		t.Errorf("spam placement fix must not switch the mailbox off: %s", f.Action.Args)
	}
}

// A mailbox already out of rotation gets no hold, so Undo can never release a hold someone else set.
func TestHoldFixIsOfferedOnlyWhileSendingCold(t *testing.T) {
	for _, tc := range []struct {
		name      string
		lifecycle string
		status    string
	}{
		{"held by its owner", "reserve", "active"},
		{"resting", "resting", "active"},
		{"switched off", "active", "inactive"},
	} {
		m := healthyMailbox()
		m.SendLifecycle = tc.lifecycle
		m.Status = tc.status
		m.WarmupPlacement = models.WarmupPlacementEvidence{MajorDelivered: 100, MajorSpam: 55}
		m.PoolHealth = "quarantined"
		found := findingsByKey(Detect(snapshotOf(m), defaults()))
		for _, key := range []string{"mailbox_spam_placement", "warmup_pool_blocked"} {
			if f, ok := found[key]; ok && f.Action != nil {
				t.Errorf("%s: %s offered %q on a mailbox that is not sending cold", tc.name, key, f.Action.Label)
			}
		}
	}
}

func TestMinSeveritySettingFiltersFindings(t *testing.T) {
	m := healthyMailbox()
	m.CampaignLimit = 80 // low-severity on a proven mailbox
	m.MinWaitTime = 60   // medium-severity burst gap

	settings := defaults()
	settings.MinSeverity = models.AdvisorHigh
	for _, f := range Detect(snapshotOf(m), settings) {
		if !f.Severity.AtLeast(models.AdvisorHigh) {
			t.Errorf("finding %s (%s) survived a min_severity of high", f.Key, f.Severity)
		}
	}
}

func TestMutedCategoryIsSkipped(t *testing.T) {
	m := healthyMailbox()
	m.WarmupActive = false

	settings := defaults()
	settings.MutedCategories = []string{string(models.AdvisorCategoryWarmup)}
	for _, f := range Detect(snapshotOf(m), settings) {
		if f.Category == models.AdvisorCategoryWarmup {
			t.Errorf("warmup finding %s survived a muted warmup category", f.Key)
		}
	}
}

func TestFingerprintIsStablePerDetectorAndEntity(t *testing.T) {
	// The fingerprint is what makes a re-run update the same row instead of
	// duplicating advice, and what makes a dismissal stick.
	id := uuid.New()
	a := Finding{Key: "mailbox_cap_too_high", EntityType: "email_account", EntityID: &id}
	b := Finding{Key: "mailbox_cap_too_high", EntityType: "email_account", EntityID: &id, Severity: models.AdvisorCritical}
	if a.Fingerprint() != b.Fingerprint() {
		t.Error("fingerprint changed when only the severity did")
	}

	other := uuid.New()
	c := Finding{Key: "mailbox_cap_too_high", EntityType: "email_account", EntityID: &other}
	if a.Fingerprint() == c.Fingerprint() {
		t.Error("two mailboxes with the same problem share a fingerprint")
	}

	orgWide := Finding{Key: "mailbox_concentration"}
	if orgWide.Fingerprint() != "mailbox_concentration|org" {
		t.Errorf("org-scoped fingerprint is %q", orgWide.Fingerprint())
	}
}

func TestEveryDetectorHasAKeyAndDescription(t *testing.T) {
	// The description is what the narrator is grounded in. A detector without
	// one produces advice the model has to guess the reasoning for.
	seen := map[string]bool{}
	for _, d := range AllDetectors() {
		if d.Key == "" {
			t.Fatal("detector with an empty key")
		}
		if seen[d.Key] {
			t.Errorf("duplicate detector key %q", d.Key)
		}
		seen[d.Key] = true
		if len(d.About) < 40 {
			t.Errorf("detector %q has no usable description for the narrator", d.Key)
		}
		if d.Category == "" {
			t.Errorf("detector %q has no category, so it cannot be muted", d.Key)
		}
		if d.Run == nil {
			t.Errorf("detector %q has no implementation", d.Key)
		}
	}
}

func TestDetectorsTolerateAnEmptySnapshot(t *testing.T) {
	// A partial snapshot load must not panic a whole org's evaluation.
	empty := &repository.AdvisorSnapshot{
		OrganizationID: uuid.New(),
		Now:            time.Now(),
		Lists:          map[uuid.UUID]repository.AdvisorListStats{},
	}
	if got := Detect(empty, defaults()); len(got) != 0 {
		t.Errorf("an empty workspace produced %d findings", len(got))
	}
}

func TestFindingsAreOrderedMostUrgentFirst(t *testing.T) {
	broken := healthyMailbox()
	broken.AuthSPF, broken.AuthDKIM, broken.AuthDMARC = false, false, false
	broken.AuthState = "failing"
	broken.MinWaitTime = 60
	broken.CampaignLimit = 90

	got := Detect(snapshotOf(broken), defaults())
	if len(got) < 2 {
		t.Fatalf("expected several findings, got %d", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Severity.Rank() < got[i].Severity.Rank() {
			t.Fatalf("findings out of order: %s(%s) before %s(%s)",
				got[i-1].Key, got[i-1].Severity, got[i].Key, got[i].Severity)
		}
	}
}

func TestCopyDetectorsIgnoreDraftCampaigns(t *testing.T) {
	campaignID := uuid.New()
	snap := snapshotOf()
	snap.Campaigns = []repository.AdvisorCampaign{{ID: campaignID, Name: "Draft", Status: "draft"}}
	snap.Steps = []repository.AdvisorStep{{
		ID: uuid.New(), CampaignID: campaignID, Kind: "email",
		Subject:   "ACT NOW!! LIMITED TIME!!",
		BodyPlain: "click here to buy now, this is not spam, 100% free",
	}}

	for _, f := range Detect(snap, defaults()) {
		if f.Category == models.AdvisorCategoryCopy {
			t.Errorf("copy detector %q fired on an unfinished draft", f.Key)
		}
	}
}

func TestCopyFindingsAttachToTheirCampaign(t *testing.T) {
	// A copy problem lives on a step, but someone looking for it is on the
	// campaign page, so the finding has to carry its campaign as a parent.
	campaignID := uuid.New()
	snap := snapshotOf()
	snap.Campaigns = []repository.AdvisorCampaign{{ID: campaignID, Name: "Q3 outbound", Status: "active"}}
	snap.Steps = []repository.AdvisorStep{{
		ID: uuid.New(), CampaignID: campaignID, Kind: "email", Position: 0,
		Name:      "Opener",
		Subject:   "quick question",
		BodyPlain: "Act now, this is a limited time offer with no obligation.",
	}}

	found := false
	for _, f := range Detect(snap, defaults()) {
		if f.Category != models.AdvisorCategoryCopy {
			continue
		}
		found = true
		if f.ParentType != "campaign" || f.ParentID == nil || *f.ParentID != campaignID {
			t.Errorf("copy finding %q is not attached to its campaign", f.Key)
		}
	}
	if !found {
		t.Error("expected a copy finding on obvious bulk-mail phrasing")
	}
}

func TestSingleSpamPhraseIsNotAFinding(t *testing.T) {
	// One borderline word in otherwise fine copy is not evidence of anything,
	// and firing on it is how a linter loses its audience.
	campaignID := uuid.New()
	snap := snapshotOf()
	snap.Campaigns = []repository.AdvisorCampaign{{ID: campaignID, Name: "Outbound", Status: "active"}}
	snap.Steps = []repository.AdvisorStep{{
		ID: uuid.New(), CampaignID: campaignID, Kind: "email", Position: 0,
		Subject:   "quick question about your onboarding",
		BodyPlain: "We guarantee delivery within a day. Worth a chat?",
	}}

	for _, f := range Detect(snap, defaults()) {
		if f.Key == "copy_spam_phrases" {
			t.Error("spam-phrase detector fired on a single ordinary word")
		}
	}
}

func TestActionsCarryARenderedPreview(t *testing.T) {
	// Nothing is applied sight-unseen: an action with no preview would put a
	// confirm dialog on screen with nothing to confirm.
	m := healthyMailbox()
	m.CampaignLimit = 90
	m.MinWaitTime = 60
	m.WarmupMax = 5

	for _, f := range Detect(snapshotOf(m), defaults()) {
		if f.Action == nil {
			continue
		}
		if f.Action.Tool == "" {
			t.Errorf("finding %q has an action with no tool", f.Key)
		}
		if len(f.Action.Args) == 0 {
			t.Errorf("finding %q has an action with no arguments", f.Key)
		}
		if len(f.Action.Preview) == 0 {
			t.Errorf("finding %q has an action the user cannot preview", f.Key)
		}
		if f.Action.Label == "" {
			t.Errorf("finding %q has an unlabelled action button", f.Key)
		}
	}
}

func TestEveryFindingIsSelfContained(t *testing.T) {
	// The narrator only ever sees the evidence map. A finding that leans on
	// context outside it would produce copy that quietly drops the specifics.
	m := healthyMailbox()
	m.CampaignLimit = 120
	m.MinWaitTime = 30
	m.AuthDMARC = false
	m.AuthState = "failing"
	m.Complaints30d = 5
	m.WarmupPlacement = models.WarmupPlacementEvidence{MajorDelivered: 60, MajorSpam: 20}

	for _, f := range Detect(snapshotOf(m), defaults()) {
		if f.Title == "" || f.Detail == "" || f.Remedy == "" {
			t.Errorf("finding %q ships incomplete fallback copy", f.Key)
		}
		if len(f.Evidence) == 0 {
			t.Errorf("finding %q carries no evidence", f.Key)
		}
		if f.Surface == "" {
			t.Errorf("finding %q has no surface, so it cannot be shown anywhere", f.Key)
		}
	}
}

func TestScoreFallsWithSeverity(t *testing.T) {
	if got := models.AdvisorScore(0, 0, 0, 0); got != 100 {
		t.Errorf("a clean workspace should score 100, got %d", got)
	}
	if clean, one := models.AdvisorScore(0, 0, 0, 0), models.AdvisorScore(1, 0, 0, 0); one >= clean {
		t.Error("a critical finding should lower the score")
	}
	// One critical has to outweigh a pile of suggestions, or the score stops
	// meaning anything.
	if models.AdvisorScore(1, 0, 0, 0) >= models.AdvisorScore(0, 0, 0, 10) {
		t.Error("one critical finding should cost more than ten low-severity ones")
	}
	// The penalty saturates rather than accumulating linearly, so a badly
	// broken workspace still gets a number that moves when it fixes something.
	// A linear score pins at 0 after about four high-severity findings, after
	// which fixing three of them changes nothing on screen.
	bad, worse := models.AdvisorScore(6, 0, 0, 0), models.AdvisorScore(20, 0, 0, 0)
	if worse >= bad {
		t.Error("more critical findings should still score worse")
	}
	if worse < 1 {
		t.Errorf("the score should never reach 0, got %d", worse)
	}
	// Across the range a workspace actually lives in, every fix moves the
	// number. (Far out in the tail the curve flattens into the floor, which is
	// the correct behaviour: at twenty critical findings the score has already
	// said everything it can.)
	if before, after := models.AdvisorScore(0, 6, 9, 8), models.AdvisorScore(0, 5, 9, 8); after <= before {
		t.Error("fixing one high-severity finding should raise the score")
	}
}

func TestValidGoTemplatesAreNotFlagged(t *testing.T) {
	// The copy in this product is a Go template. Conditionals, pipelines, and
	// the index form for spaced custom fields are all correct, and a detector
	// that flags them would fire on most well-written campaigns in the product.
	campaignID := uuid.New()
	snap := snapshotOf()
	snap.Campaigns = []repository.AdvisorCampaign{{ID: campaignID, Name: "Outbound", Status: "active"}}
	snap.Steps = []repository.AdvisorStep{{
		ID: uuid.New(), CampaignID: campaignID, Kind: "email", Position: 0,
		Subject: "quick question about {{.Company}}",
		BodyPlain: "Hi {{.FirstName}},\n\n" +
			`{{if .Company}}Saw {{.Company}} is hiring.{{else}}Saw your team is hiring.{{end}}` +
			"\n{{index . \"city\"}} came up too.\n\nWorth a chat?",
	}}

	for _, f := range Detect(snap, defaults()) {
		if f.Key == "copy_broken_template" {
			t.Errorf("valid Go template flagged as broken: %s", f.Detail)
		}
	}
}

func TestUnparseableTemplateIsFlagged(t *testing.T) {
	campaignID := uuid.New()
	snap := snapshotOf()
	snap.Campaigns = []repository.AdvisorCampaign{{ID: campaignID, Name: "Outbound", Status: "active"}}
	snap.Steps = []repository.AdvisorStep{{
		ID: uuid.New(), CampaignID: campaignID, Kind: "email", Position: 0,
		Name:      "Opener",
		Subject:   "quick question",
		BodyPlain: "Hi {{.FirstName}},\n\n{{if .Company}}Saw you are hiring.\n\nWorth a chat?",
	}}

	f, fired := findingsByKey(Detect(snap, defaults()))["copy_broken_template"]
	if !fired {
		t.Fatal("an {{if}} with no {{end}} should be flagged: it ships as literal text")
	}
	if f.Severity != models.AdvisorHigh {
		t.Errorf("broken copy in a sending campaign should be high, got %s", f.Severity)
	}
}

func TestMissingFirstNameOnlyFiresWhenTheCopyUsesIt(t *testing.T) {
	campaignID := uuid.New()
	build := func(body string) *repository.AdvisorSnapshot {
		snap := snapshotOf()
		snap.Campaigns = []repository.AdvisorCampaign{{ID: campaignID, Name: "Outbound", Status: "active"}}
		snap.Steps = []repository.AdvisorStep{{
			ID: uuid.New(), CampaignID: campaignID, Kind: "email", Subject: "hello", BodyPlain: body,
		}}
		snap.Lists = map[uuid.UUID]repository.AdvisorListStats{
			campaignID: {CampaignID: campaignID, Total: 200, MissingFirstName: 60},
		}
		return snap
	}

	if _, fired := findingsByKey(Detect(build("Hi there, worth a chat?"), defaults()))["list_missing_personalization_data"]; fired {
		t.Error("missing-first-name fired on copy that never greets by name")
	}

	// The real merge syntax in this product is {{.FirstName}}, not
	// {{first_name}} — this is the case a literal-spelling check would miss.
	if _, fired := findingsByKey(Detect(build("Hi {{.FirstName}}, worth a chat?"), defaults()))["list_missing_personalization_data"]; !fired {
		t.Error("missing-first-name did not fire on copy that does greet by name")
	}
}

func TestAutopilotOnlyGetsReversibleSafeFixes(t *testing.T) {
	// Autopilot applies changes with nobody watching, so the set of fixes it is
	// allowed to touch is a safety boundary, not a convenience. This pins it:
	// an auto fix must be undoable, and the checks that stop a customer's
	// sending or edit their copy must never drift into the set.
	m := healthyMailbox()
	m.CampaignLimit = 120
	m.MinWaitTime = 30
	m.Complaints30d = 5
	m.ColdSent30d = 4000
	m.WarmupPlacement = models.WarmupPlacementEvidence{MajorDelivered: 60, MajorSpam: 30}
	m.InActiveCampaign = true

	// Detectors whose one-click fix is deliberately hand-only: each either
	// halts sending or generates new outbound mail the member did not ask for.
	handOnly := map[string]bool{
		"mailbox_spam_placement": true,
		"warmup_pool_blocked":    true,
		"warmup_off":             true,
		"warmup_paused":          true,
		"warmup_ceiling_low":     true,
		"warmup_reply_rate_low":  true,
	}

	autos := 0
	for _, f := range Detect(snapshotOf(m), defaults()) {
		if f.Action == nil || !f.Action.Auto {
			continue
		}
		autos++
		if f.Action.Undo == nil {
			t.Errorf("finding %q is auto-applied but cannot be undone", f.Key)
		}
		if handOnly[f.Key] {
			t.Errorf("finding %q must not be auto-applied: it changes sending without asking", f.Key)
		}
	}
	if autos == 0 {
		t.Fatal("no auto-safe fix fired, so this test is not checking anything")
	}
}

func TestEveryFindingOffersAWayForward(t *testing.T) {
	// The gap this closes: a finding with no one-click fix, no agent, and no
	// steps leaves the reader a paragraph of prose and no instruction. That is
	// how a missing DMARC record ended up as a dead end, and it was not the
	// only one.
	//
	// Every finding must offer at least one of: a fix to apply, an agent that
	// can do it, or ordered steps to do it by hand.
	m := healthyMailbox()
	m.CampaignLimit = 120
	m.MinWaitTime = 30
	m.AuthDMARC = false
	m.AuthSPF = false
	m.AuthState = "failing"
	m.Complaints30d = 5
	m.ColdSent30d = 4000
	m.Bounces30d = 300
	m.WarmupPlacement = models.WarmupPlacementEvidence{MajorDelivered: 60, MajorSpam: 30}
	m.UnresolvedErrs = 5
	m.TrackingDomain = ""
	m.InActiveCampaign = true
	m.PoolBlocked = true
	m.PoolHealthScore = 80
	m.PoolHealthReason = "warmup spam placement 50.0% exceeded block threshold"

	findings := Detect(snapshotOf(m), defaults())
	if len(findings) < 5 {
		t.Fatalf("this mailbox should trip most of the detectors, got %d findings", len(findings))
	}
	for _, f := range findings {
		hasFix := f.Action != nil
		hasAgent := agentFixable[f.Key]
		hasSteps := len(f.Steps) > 0
		if !hasFix && !hasAgent && !hasSteps {
			t.Errorf("finding %q offers no way forward: no fix, no agent, no steps", f.Key)
		}
	}
}

func TestDNSFindingsShipThePasteableRecord(t *testing.T) {
	// Telling somebody to "add an SPF record" is where most people stop. The
	// record itself is the fix, so a finding whose remedy lives in DNS has to
	// carry the value to paste.
	m := healthyMailbox()
	m.Email = "sender@acme.test"
	m.Provider = "gmail"
	m.AuthSPF = false
	m.AuthDMARC = false
	m.AuthState = "failing"

	byKey := findingsByKey(Detect(snapshotOf(m), defaults()))
	auth, ok := byKey["mailbox_domain_auth"]
	if !ok {
		t.Fatal("domain auth did not fire on a mailbox missing SPF and DMARC")
	}
	if len(auth.Snippets) == 0 {
		t.Fatal("domain auth carries no records to paste")
	}

	joined := ""
	for _, s := range auth.Snippets {
		if s.Label == "" {
			t.Error("a snippet has no label, so it is unclear what field it goes in")
		}
		joined += s.Label + "=" + s.Value + "\n"
	}

	// The provider-specific include and the domain-specific DMARC host are the
	// two values a generic template gets wrong.
	if !strings.Contains(joined, "include:_spf.google.com") {
		t.Errorf("SPF record does not carry the Google include for a gmail mailbox:\n%s", joined)
	}
	if !strings.Contains(joined, "_dmarc.acme.test") {
		t.Errorf("DMARC host is not scoped to the sending domain:\n%s", joined)
	}
	if !strings.Contains(joined, "p=none") {
		t.Errorf("DMARC record should start in monitor-only mode:\n%s", joined)
	}
}

// The finding used to print a spam score that had not caused the state it
// described: nothing read that number, and it grew with volume rather than
// with misbehaviour (#491). It now prints the band's own reason.
func TestWarmupPoolFindingExplainsItselfWithTheBandsReason(t *testing.T) {
	m := healthyMailbox()
	m.PoolHealth = "blocked"
	m.PoolBlocked = true
	m.PoolHealthScore = 62
	m.PoolHealthReason = "warmup spam placement 44.0% exceeded block threshold"

	var found *Finding
	findings := Detect(snapshotOf(m), defaults())
	for i := range findings {
		if findings[i].Key == "warmup_pool_blocked" {
			found = &findings[i]
			break
		}
	}
	if found == nil {
		t.Fatal("a blocked mailbox produced no warmup_pool_blocked finding")
	}
	if !strings.Contains(found.Detail, m.PoolHealthReason) {
		t.Fatalf("detail does not say why the band acted: %q", found.Detail)
	}
	if strings.Contains(strings.ToLower(found.Detail), "spam score") {
		t.Fatalf("detail still quotes a spam score: %q", found.Detail)
	}
	if got := found.Evidence["pool_health_reason"]; got != m.PoolHealthReason {
		t.Fatalf("evidence pool_health_reason = %v, want the band's reason", got)
	}
	if _, ok := found.Evidence["spam_score"]; ok {
		t.Fatal("evidence still carries a spam score")
	}
}

// judgedSnapshot is one active campaign with one email step, plus the verdict
// the copy judge would have attached to it.
func judgedSnapshot(v *copyjudge.Verdict) (*repository.AdvisorSnapshot, uuid.UUID) {
	campaignID, stepID := uuid.New(), uuid.New()
	snap := snapshotOf()
	snap.Campaigns = []repository.AdvisorCampaign{{ID: campaignID, Name: "Outbound", Status: "active"}}
	snap.Steps = []repository.AdvisorStep{{
		ID: stepID, CampaignID: campaignID, Kind: "email", Position: 0,
		Name:      "Opener",
		Subject:   "quick question",
		BodyPlain: "Saw your talk on onboarding. Would a short note on how we cut it to a day be useful?",
	}}
	if v != nil {
		snap.CopyJudgments = map[uuid.UUID]copyjudge.Verdict{stepID: *v}
	}
	return snap, stepID
}

func TestJudgmentDetectorsAreSilentWithoutAVerdict(t *testing.T) {
	// An install without TypeSafe, or a run where the judge was down, has no
	// verdict for the step. That is "not judged", never "judged fine" or
	// "judged bad".
	snap, _ := judgedSnapshot(nil)
	got := findingsByKey(Detect(snap, defaults()))
	for _, key := range []string{"copy_reads_as_bulk", "copy_no_clear_ask"} {
		if _, fired := got[key]; fired {
			t.Errorf("%s fired on a step with no judgment", key)
		}
	}
}

func TestPersonalCopyWithOneAskIsNotAFinding(t *testing.T) {
	snap, _ := judgedSnapshot(&copyjudge.Verdict{
		ReadsAs: 0, Personalization: 0, Ask: copyjudge.AskOneClear, SpamClaim: 0.05, Confidence: 0.95,
	})
	got := findingsByKey(Detect(snap, defaults()))
	for _, key := range []string{"copy_reads_as_bulk", "copy_no_clear_ask"} {
		if _, fired := got[key]; fired {
			t.Errorf("%s fired on a personal note with one clear ask", key)
		}
	}
}

func TestReadsAsBulkFiresOnAConfidentBulkVerdict(t *testing.T) {
	snap, stepID := judgedSnapshot(&copyjudge.Verdict{
		ReadsAs: 1, Personalization: 1, Ask: copyjudge.AskOneClear, SpamClaim: 0.1, Confidence: 0.9,
	})
	f, fired := findingsByKey(Detect(snap, defaults()))["copy_reads_as_bulk"]
	if !fired {
		t.Fatal("copy_reads_as_bulk did not fire on a confident bulk verdict")
	}
	if f.Severity != models.AdvisorMedium {
		t.Errorf("severity should be medium, got %s", f.Severity)
	}
	if f.EntityType != "step" || f.EntityID == nil || *f.EntityID != stepID || f.ParentType != "campaign" {
		t.Error("finding is not attached to its step and campaign")
	}
	if f.Evidence["reads_as"] != 1.0 || f.Evidence["confidence"] != 0.9 {
		t.Errorf("evidence does not carry the numbers it fired on: %v", f.Evidence)
	}
	if !strings.Contains(f.Title, "Opener") {
		t.Errorf("title does not name the step: %q", f.Title)
	}
}

func TestReadsAsBulkNeedsConfidenceUnlessAClaimIsMade(t *testing.T) {
	// The model put it at the bulk end but was not sure. An unsure verdict is
	// not reproducible, and a finding that flickers between runs is worse
	// than none.
	snap, _ := judgedSnapshot(&copyjudge.Verdict{
		ReadsAs: 1, Ask: copyjudge.AskOneClear, SpamClaim: 0.1, Confidence: 0.5,
	})
	if _, fired := findingsByKey(Detect(snap, defaults()))["copy_reads_as_bulk"]; fired {
		t.Error("copy_reads_as_bulk fired below the confidence floor")
	}

	// A filter-baiting claim is its own probability and needs no floor.
	snap, _ = judgedSnapshot(&copyjudge.Verdict{
		ReadsAs: 0.2, Ask: copyjudge.AskOneClear, SpamClaim: 0.85, Confidence: 0.5,
	})
	f, fired := findingsByKey(Detect(snap, defaults()))["copy_reads_as_bulk"]
	if !fired {
		t.Fatal("copy_reads_as_bulk did not fire on a spam claim")
	}
	if !strings.Contains(f.Detail, "spam filter") {
		t.Errorf("detail should say the claim is the problem: %q", f.Detail)
	}
}

func TestNoClearAskSaysWhichWay(t *testing.T) {
	snap, _ := judgedSnapshot(&copyjudge.Verdict{ReadsAs: 0, Ask: copyjudge.AskNone, Confidence: 0.9})
	f, fired := findingsByKey(Detect(snap, defaults()))["copy_no_clear_ask"]
	if !fired {
		t.Fatal("copy_no_clear_ask did not fire on no_ask")
	}
	if f.Severity != models.AdvisorLow {
		t.Errorf("severity should be low, got %s", f.Severity)
	}
	if !strings.Contains(f.Title, "nothing") {
		t.Errorf("title should say it asks for nothing: %q", f.Title)
	}

	snap, _ = judgedSnapshot(&copyjudge.Verdict{ReadsAs: 0, Ask: copyjudge.AskSeveral, Confidence: 0.9})
	f, fired = findingsByKey(Detect(snap, defaults()))["copy_no_clear_ask"]
	if !fired {
		t.Fatal("copy_no_clear_ask did not fire on several_asks")
	}
	if !strings.Contains(f.Title, "several") {
		t.Errorf("title should say it asks for several things: %q", f.Title)
	}
	if f.Evidence["ask"] != copyjudge.AskSeveral {
		t.Errorf("evidence should carry the ask: %v", f.Evidence)
	}

	snap, _ = judgedSnapshot(&copyjudge.Verdict{ReadsAs: 0, Ask: copyjudge.AskNone, Confidence: 0.3})
	if _, fired := findingsByKey(Detect(snap, defaults()))["copy_no_clear_ask"]; fired {
		t.Error("copy_no_clear_ask fired below the confidence floor")
	}
}

// recordingJudge answers every step with one verdict and counts the calls.
type recordingJudge struct {
	calls int
	err   error
}

func (r *recordingJudge) Ask(_ context.Context, _ any, _ map[string]typesafe.Question) (*typesafe.Response, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	resp := &typesafe.Response{Model: typesafe.Model, Answers: map[string]typesafe.Answer{
		"reads_as":        {Type: typesafe.QuestionScore, Score: 2, Confidence: 0.9},
		"personalization": {Type: typesafe.QuestionScore, Score: 1, Confidence: 0.9},
		"ask":             {Type: typesafe.QuestionChoice, Choice: copyjudge.AskOneClear, Confidence: 0.9},
		"spam_claim":      {Type: typesafe.QuestionNoul, Noul: 0.1},
	}}
	return resp, nil
}

type memoryJudgeCache struct {
	rows map[string]copyjudge.Verdict
	puts int
}

func (m *memoryJudgeCache) Get(_ context.Context, orgID uuid.UUID, hash string) (*copyjudge.Verdict, error) {
	v, ok := m.rows[orgID.String()+hash]
	if !ok {
		return nil, nil
	}
	return &v, nil
}

func (m *memoryJudgeCache) Put(_ context.Context, orgID uuid.UUID, hash string, v *copyjudge.Verdict) error {
	if m.rows == nil {
		m.rows = map[string]copyjudge.Verdict{}
	}
	m.rows[orgID.String()+hash] = *v
	m.puts++
	return nil
}

func TestJudgeCopyReadsTheCacheBeforeAsking(t *testing.T) {
	judge := &recordingJudge{}
	cache := &memoryJudgeCache{}
	s := NewService(nil, nil, nil, nil, nil, nil, WithCopyJudge(judge, cache)).(*service)

	snap, stepID := judgedSnapshot(nil)
	s.judgeCopy(context.Background(), snap)
	if judge.calls != 1 || cache.puts != 1 {
		t.Fatalf("first run: %d calls, %d cache writes", judge.calls, cache.puts)
	}
	v, ok := snap.CopyJudgments[stepID]
	if !ok || v.ReadsAs != 1 {
		t.Fatalf("verdict not attached to the step: %v %v", ok, v)
	}

	// Same copy again: the cache answers, the model is not asked.
	snap2, _ := judgedSnapshot(nil)
	snap2.OrganizationID = snap.OrganizationID
	snap2.Steps[0].ID = stepID
	s.judgeCopy(context.Background(), snap2)
	if judge.calls != 1 {
		t.Errorf("unchanged copy was judged again (%d calls)", judge.calls)
	}
	if _, ok := snap2.CopyJudgments[stepID]; !ok {
		t.Error("cached verdict was not attached")
	}

	// Drafts are never judged, so they never cost anything.
	snap3, _ := judgedSnapshot(nil)
	snap3.Campaigns[0].Status = "draft"
	s.judgeCopy(context.Background(), snap3)
	if judge.calls != 1 {
		t.Error("a draft campaign's step was sent to the judge")
	}
}

type unavailableJudgeCache struct{ memoryJudgeCache }

func (m *unavailableJudgeCache) Put(context.Context, uuid.UUID, string, *copyjudge.Verdict) error {
	return errors.New("cache unavailable")
}

func TestJudgeCopyReusesIdenticalCopyWhenCacheWriteFails(t *testing.T) {
	judge := &recordingJudge{}
	s := NewService(nil, nil, nil, nil, nil, nil, WithCopyJudge(judge, &unavailableJudgeCache{})).(*service)
	snap, _ := judgedSnapshot(nil)
	for i := 0; i < 50; i++ {
		step := snap.Steps[0]
		step.ID = uuid.New()
		snap.Steps = append(snap.Steps, step)
	}
	s.judgeCopy(context.Background(), snap)
	if judge.calls != 1 || len(snap.CopyJudgments) != 51 {
		t.Fatalf("identical copy cost %d calls and produced %d verdicts", judge.calls, len(snap.CopyJudgments))
	}
}

func TestJudgeCopyCapsFreshJudgmentsAndSurvivesErrors(t *testing.T) {
	judge := &recordingJudge{}
	cache := &memoryJudgeCache{}
	s := NewService(nil, nil, nil, nil, nil, nil, WithCopyJudge(judge, cache)).(*service)

	snap, _ := judgedSnapshot(nil)
	campaignID := snap.Campaigns[0].ID
	for i := 0; i < maxCopyJudgmentsPerRun+10; i++ {
		snap.Steps = append(snap.Steps, repository.AdvisorStep{
			ID: uuid.New(), CampaignID: campaignID, Kind: "email", Position: i + 1,
			Subject: fmt.Sprintf("step %d", i), BodyPlain: fmt.Sprintf("body %d", i),
		})
	}
	s.judgeCopy(context.Background(), snap)
	if judge.calls != maxCopyJudgmentsPerRun {
		t.Errorf("expected the cap of %d fresh judgments, got %d", maxCopyJudgmentsPerRun, judge.calls)
	}
	if len(snap.CopyJudgments) != maxCopyJudgmentsPerRun {
		t.Errorf("expected %d verdicts attached, got %d", maxCopyJudgmentsPerRun, len(snap.CopyJudgments))
	}

	// The judge being down leaves steps unjudged and the run intact.
	down := NewService(nil, nil, nil, nil, nil, nil, WithCopyJudge(&recordingJudge{err: errors.New("529")}, &memoryJudgeCache{})).(*service)
	snap, _ = judgedSnapshot(nil)
	down.judgeCopy(context.Background(), snap)
	if len(snap.CopyJudgments) != 0 {
		t.Error("a failed judgment produced a verdict")
	}
	got := findingsByKey(Detect(snap, defaults()))
	for _, key := range []string{"copy_reads_as_bulk", "copy_no_clear_ask"} {
		if _, fired := got[key]; fired {
			t.Errorf("%s fired on a step the judge never answered for", key)
		}
	}
}

// The no-senders finding names the way the campaign actually picks mailboxes,
// so a campaign on "every active mailbox" is never sent to check its tags (#661).
func TestNoSendersNamesHowTheCampaignPicksMailboxes(t *testing.T) {
	cases := []struct {
		name       string
		camp       repository.AdvisorCampaign
		selection  string
		mentionTag bool
	}{
		{"all", repository.AdvisorCampaign{SenderStrategy: "tags"}, "every active mailbox", false},
		{"explicit empty", repository.AdvisorCampaign{SenderStrategy: "explicit"}, "picked by hand", false},
		{"picked", repository.AdvisorCampaign{SenderStrategy: "tags", PickedSenders: 3}, "picked by hand", false},
		{"tags", repository.AdvisorCampaign{SenderStrategy: "tags", SenderTags: 1}, "by tag", true},
		{"both", repository.AdvisorCampaign{SenderStrategy: "tags", PickedSenders: 2, SenderTags: 1}, "picked by hand and by tag", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			camp := tc.camp
			camp.ID, camp.Name, camp.Status, camp.LeadsRemaining = uuid.New(), "Q4", "active", 274
			found := detectNoSenders(&repository.AdvisorSnapshot{Campaigns: []repository.AdvisorCampaign{camp}})
			if len(found) != 1 {
				t.Fatalf("got %d findings, want 1", len(found))
			}
			f := found[0]
			if got := f.Evidence["sender_selection"]; got != tc.selection {
				t.Fatalf("sender_selection = %v, want %q", got, tc.selection)
			}
			if _, ok := f.Evidence["sender_strategy"]; ok {
				t.Fatal("evidence still carries sender_strategy, which reads 'tags' for a campaign that picks nothing")
			}
			copy := strings.ToLower(f.Detail + " " + f.Remedy + " " + strings.Join(f.Steps, " "))
			if got := strings.Contains(copy, "tag"); got != tc.mentionTag {
				t.Fatalf("copy mentions tags = %v, want %v: %s", got, tc.mentionTag, copy)
			}
		})
	}
}

// Everything junked at small hosts while Google and Microsoft inbox all of it
// is not a mailbox landing in spam.
func TestSpamPlacementAtSmallHostsAloneDoesNotAlarm(t *testing.T) {
	m := healthyMailbox()
	m.InActiveCampaign = true
	m.WarmupPlacement = models.WarmupPlacementEvidence{MajorDelivered: 40, OtherDelivered: 40, OtherSpam: 40}
	if f, fired := findingsByKey(Detect(snapshotOf(m), defaults()))["mailbox_spam_placement"]; fired {
		t.Fatalf("small-host spam alone raised %q: %s", f.Severity, f.Title)
	}
}

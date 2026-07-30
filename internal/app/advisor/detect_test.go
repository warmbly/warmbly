package advisor

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
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
	m.WarmupSent7d = minWarmupDeliveriesForPlacement - 1
	m.WarmupSpam7d = 5

	if _, fired := findingsByKey(Detect(snapshotOf(m), defaults()))["mailbox_spam_placement"]; fired {
		t.Fatal("placement detector fired below its delivery floor")
	}

	m.WarmupSent7d = 100
	m.WarmupSpam7d = 45 // 45%: past the hard-block band.
	f := findingsByKey(Detect(snapshotOf(m), defaults()))["mailbox_spam_placement"]
	if f.Severity != models.AdvisorCritical {
		t.Errorf("45%% spam placement should be critical, got %q", f.Severity)
	}
	if f.Action == nil {
		t.Error("a mailbox this deep in spam while sending cold should offer to stop")
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
	m.WarmupSent7d = 60
	m.WarmupSpam7d = 20

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
	m.WarmupSent7d = 60
	m.WarmupSpam7d = 30
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

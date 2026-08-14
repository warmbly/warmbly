package confenge

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func fullGreenInput() GreenAutorunInput {
	return GreenAutorunInput{
		Channel:                   "EMAIL",
		EmailSendReady:            true,
		TargetFitSendTier:         "A_AUTOMATIC",
		OwnershipAllowed:          true,
		MailboxPurposeAllowed:     true,
		VerificationAllowed:       true,
		DNC:                       false,
		Bounce:                    false,
		Replied:                   false,
		Blocked:                   false,
		ContactFresh:              true,
		ContextFresh:              true,
		ServiceCode:               "REAJUSTE_14133",
		SingleService:             true,
		FactualHookAnchored:       true,
		NoUnknownEvidenceIDs:      true,
		NoHypothesisAsFact:        true,
		NoClaimsToAvoidViolated:   true,
		ValidationOK:              true,
		RiskClass:                 "GREEN",
		MessageContextHashCurrent: true,
		NoEditAfterAuthorization:  true,
		CopyWithinLimits:          true,
		GovernorHealthy:           true,
		HasContactCandidate:       true,
	}
}

func testPolicyAuth(now time.Time) *CampaignPolicyAuthorization {
	return &CampaignPolicyAuthorization{
		ID: uuid.New(), CampaignID: uuid.New(), Channel: "EMAIL", AllowedRiskClass: "GREEN",
		EffectiveAt: now.Add(-time.Hour), AuthorizedBy: uuid.New(),
		PromptPolicyVersion: PromptVersion, ValidatorVersion: ValidatorVersionV1,
		ContactPolicyVersion: ContactPolicyVersionV1, AllowPolicyTemplateGREEN: true,
		TemplatePolicyVersion: TemplatePolicyVersionV1, SenderMailbox: "tiago.sasaki@confenge.com.br",
		MaxRatePerHour: 10,
	}
}

func TestGreenAutorunFailClosedDefault(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	d := EvaluateGreenAutorun(false, testPolicyAuth(now), fullGreenInput(), now)
	if d.Allow {
		t.Fatal("disabled flag must fail closed")
	}
}

func TestGreenAutorunRequiresPolicy(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	d := EvaluateGreenAutorun(true, nil, fullGreenInput(), now)
	if d.Allow {
		t.Fatal("nil auth must fail")
	}
}

func TestGreenAutorunAllPredicatesPass(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	d := EvaluateGreenAutorun(true, testPolicyAuth(now), fullGreenInput(), now)
	if !d.Allow {
		t.Fatalf("want allow, reasons=%v", d.Reasons)
	}
	if d.AuthorizationMode != AuthorizationModeCampaignPolicy {
		t.Fatalf("mode=%s", d.AuthorizationMode)
	}
}

func TestGreenAutorunBlocksYELLOWAndGenericTemplate(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	auth := testPolicyAuth(now)
	in := fullGreenInput()
	in.RiskClass = "YELLOW"
	d := EvaluateGreenAutorun(true, auth, in, now)
	if d.Allow {
		t.Fatal("YELLOW must not autorun")
	}
	in = fullGreenInput()
	in.GenericUnauditedTemplate = true
	d = EvaluateGreenAutorun(true, auth, in, now)
	if d.Allow {
		t.Fatal("generic template must not autorun")
	}
}

func TestGreenAutorunBlocksMailboxAndTargetFit(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	auth := testPolicyAuth(now)
	in := fullGreenInput()
	in.MailboxPurposeAllowed = false
	if EvaluateGreenAutorun(true, auth, in, now).Allow {
		t.Fatal("blocked mailbox purpose")
	}
	in = fullGreenInput()
	in.TargetFitSendTier = "RESEARCH_ONLY"
	if EvaluateGreenAutorun(true, auth, in, now).Allow {
		t.Fatal("RESEARCH_ONLY must not autorun")
	}
	in = fullGreenInput()
	in.TargetFitSendTier = ""
	if EvaluateGreenAutorun(true, auth, in, now).Allow {
		t.Fatal("absent target_fit must not autorun")
	}
	in = fullGreenInput()
	in.EmailSendReady = false
	if EvaluateGreenAutorun(true, auth, in, now).Allow {
		t.Fatal("EMAIL_SEND_READY false")
	}
	in = fullGreenInput()
	in.HasContactCandidate = false
	if EvaluateGreenAutorun(true, auth, in, now).Allow {
		t.Fatal("missing contact candidate must not autorun")
	}
}

func TestGreenAutorunVersionMismatchCloses(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	auth := testPolicyAuth(now)
	in := fullGreenInput()
	in.RuntimeValidatorVersion = "confenge.validators.v0"
	in.UsedPolicyApprovedTemplate = true
	in.RuntimeTemplateVersion = TemplatePolicyVersionV1
	if EvaluateGreenAutorun(true, auth, in, now).Allow {
		t.Fatal("validator mismatch must close")
	}
	in = fullGreenInput()
	in.RuntimeSenderMailbox = "other@confenge.com.br"
	auth.SenderMailbox = "tiago.sasaki@confenge.com.br"
	if EvaluateGreenAutorun(true, auth, in, now).Allow {
		t.Fatal("sender mailbox mismatch must close")
	}
}

func TestEffectiveHourlyCapRespectsPolicy(t *testing.T) {
	if got := EffectiveHourlyCap(20, 10); got != 10 {
		t.Fatalf("want 10 got %d", got)
	}
	if got := EffectiveHourlyCap(10, 20); got != 10 {
		t.Fatalf("want 10 got %d", got)
	}
	if got := EffectiveHourlyCap(20, 15, 8); got != 8 {
		t.Fatalf("want 8 got %d", got)
	}
}

func TestPolicyAuthorizationHashStable(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	a := testPolicyAuth(now)
	h1 := PolicyAuthorizationHash(a)
	h2 := PolicyAuthorizationHash(a)
	if h1 == "" || h1 != h2 {
		t.Fatalf("hash unstable %q %q", h1, h2)
	}
	a.MaxRatePerHour = 5
	if PolicyAuthorizationHash(a) == h1 {
		t.Fatal("rate change must change hash")
	}
}

package confenge

import (
	"strings"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/app/replyclassify"
)

func TestDetectTextualDNCStickyKeywords(t *testing.T) {
	cases := []string{
		"Please unsubscribe me",
		"pare de me contatar",
		"nao me contate mais",
		"remove me from this list",
		"DO NOT CONTACT",
	}
	for _, c := range cases {
		if !DetectTextualDNC("", c) {
			t.Fatalf("expected DNC for %q", c)
		}
	}
	if DetectTextualDNC("", "obrigado, temos interesse") {
		t.Fatal("false positive DNC")
	}
}

func TestClassifyCommercialIntentPositive(t *testing.T) {
	r := ClassifyCommercialIntent("", "Tenho interesse, vamos agendar uma call", "", nil)
	if r.Intent != IntentPositiveInterest {
		t.Fatalf("intent=%s want POSITIVE_INTEREST", r.Intent)
	}
	if r.SuggestedAction == "" {
		t.Fatal("missing suggested action")
	}
}

func TestClassifyCommercialIntentReferral(t *testing.T) {
	r := ClassifyCommercialIntent("", "Nao sou a pessoa, fale com Maria Silva", "", nil)
	if r.Intent != IntentReferral {
		t.Fatalf("intent=%s", r.Intent)
	}
	if r.ReferralHint == "" {
		t.Fatal("expected referral hint")
	}
}

func TestExtractOOODateNeverInvent(t *testing.T) {
	if d := ExtractOOODate("estou de ferias, volto em breve"); d != nil {
		t.Fatalf("must not invent date, got %v", d)
	}
	if d := ExtractOOODate("back on 2026-09-15"); d == nil || d.Format("2006-01-02") != "2026-09-15" {
		t.Fatalf("expected ISO date, got %v", d)
	}
	if d := ExtractOOODate("retorno em 03/10/2026"); d == nil || d.Format("2006-01-02") != "2026-10-03" {
		t.Fatalf("expected BR date, got %v", d)
	}
}

func TestClassifyCommercialIntentAIOffUnknown(t *testing.T) {
	// No lexicon match, no headers, empty preClass → UNKNOWN (AI-off path).
	r := ClassifyCommercialIntent("", "xyzzy bla 123", "", nil)
	if r.Intent != IntentUnknown {
		t.Fatalf("intent=%s source=%s", r.Intent, r.Source)
	}
	if r.Confidence != 0 {
		t.Fatalf("confidence=%v", r.Confidence)
	}
	r2 := ClassifyCommercialIntent("", "neutral-ish", replyclassify.ClassUnknown, nil)
	if r2.Intent != IntentUnknown {
		t.Fatalf("mapped unknown class → %s", r2.Intent)
	}
}

func TestMapCockpitFilterToQueueState(t *testing.T) {
	cases := map[string]string{
		FilterNeedsAttention:   "REPLIED",
		FilterAwaitingApproval: "NEEDS_REVIEW",
		FilterScheduled:        "ENROLLED",
		FilterSent:             "SENT",
		FilterReplied:          "REPLIED",
		FilterDNC:              "DO_NOT_CONTACT",
		"needs-attention":      "REPLIED",
	}
	for in, want := range cases {
		if got := MapCockpitFilterToQueueState(in); got != want {
			t.Fatalf("%s → %s want %s", in, got, want)
		}
	}
}

func TestObjectionReplyGuardrails(t *testing.T) {
	g := ObjectionReplyGuardrails()
	if len(g) < 3 {
		t.Fatal("expected guardrails")
	}
	joined := strings.Join(g, " ")
	if !strings.Contains(joined, "juridic") && !strings.Contains(joined, "inventar") {
		// Portuguese text; just ensure non-empty policy strings.
		t.Fatalf("guardrails look empty: %v", g)
	}
	body := sanitizeObjectionReply("Voce esta errado. Isso e ilegal. Vamos processar. Obrigado.")
	lower := strings.ToLower(body)
	for _, bad := range []string{"voce esta errado", "isso e ilegal", "vamos processar"} {
		if strings.Contains(lower, bad) {
			t.Fatalf("banned phrase remained: %s in %q", bad, body)
		}
	}
	if !strings.Contains(lower, "nao discuto") {
		t.Fatalf("missing guardrail sentence: %q", body)
	}
}

func TestSuggestNextActionOOOWithAndWithoutDate(t *testing.T) {
	d := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	with := SuggestNextAction(IntentOutOfOffice, &d, "")
	if !strings.Contains(with, "2026-09-01") {
		t.Fatalf("want date in action: %s", with)
	}
	without := SuggestNextAction(IntentOutOfOffice, nil, "")
	if strings.Contains(without, "2026") {
		t.Fatalf("must not invent date: %s", without)
	}
}

func TestDNCTakesPrecedenceOverPositiveLexicon(t *testing.T) {
	r := ClassifyCommercialIntent("", "tenho interesse mas unsubscribe me please", "", nil)
	if r.Intent != IntentDoNotContact {
		t.Fatalf("DNC must win, got %s", r.Intent)
	}
}

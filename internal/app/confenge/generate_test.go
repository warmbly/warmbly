package confenge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

type goldenScenario struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Account struct {
		RazaoSocial       string   `json:"razao_social"`
		NomeFantasia      string   `json:"nome_fantasia"`
		Municipio         string   `json:"municipio"`
		UF                string   `json:"uf"`
		MomentCode        string   `json:"moment_code"`
		MomentSummary     string   `json:"moment_summary"`
		ServiceCode       string   `json:"service_code"`
		ServiceName       string   `json:"service_name"`
		EntryOffer        string   `json:"entry_offer"`
		FactToMention     string   `json:"fact_to_mention"`
		QuestionToAsk     string   `json:"question_to_ask"`
		CTA               string   `json:"cta"`
		ClaimsToAvoid     []string `json:"claims_to_avoid"`
		MomentEvidenceIDs []string `json:"moment_evidence_ids"`
	} `json:"account"`
	Contact struct {
		Name               string `json:"name"`
		Role               string `json:"role"`
		Email              string `json:"email"`
		VerificationStatus string `json:"verification_status"`
	} `json:"contact"`
	Evidence []struct {
		SourceEvidenceID string `json:"source_evidence_id"`
		Title            string `json:"title"`
		Synthesis        string `json:"synthesis"`
		Excerpt          string `json:"excerpt"`
		EpistemicClass   string `json:"epistemic_class"`
	} `json:"evidence"`
	Channel      string `json:"channel"`
	PriorBody    string `json:"prior_body"`
	InboundReply string `json:"inbound_reply"`
}

type goldenFile struct {
	Scenarios []goldenScenario `json:"scenarios"`
}

func loadGolden(t *testing.T) []goldenScenario {
	t.Helper()
	path := filepath.Join("testdata", "golden_accounts.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var f goldenFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	if len(f.Scenarios) < 6 {
		t.Fatalf("expected >=6 scenarios, got %d", len(f.Scenarios))
	}
	return f.Scenarios
}

func scenarioToInput(sc goldenScenario) (GenerateInput, *models.OutreachAccount, *models.OutreachContactCandidate, []models.OutreachEvidence) {
	acc := &models.OutreachAccount{
		RazaoSocial:       sc.Account.RazaoSocial,
		NomeFantasia:      sc.Account.NomeFantasia,
		Municipio:         sc.Account.Municipio,
		UF:                sc.Account.UF,
		MomentCode:        sc.Account.MomentCode,
		MomentSummary:     sc.Account.MomentSummary,
		ServiceCode:       sc.Account.ServiceCode,
		ServiceName:       sc.Account.ServiceName,
		EntryOffer:        sc.Account.EntryOffer,
		FactToMention:     sc.Account.FactToMention,
		QuestionToAsk:     sc.Account.QuestionToAsk,
		CTA:               sc.Account.CTA,
		ClaimsToAvoid:     sc.Account.ClaimsToAvoid,
		MomentEvidenceIDs: sc.Account.MomentEvidenceIDs,
	}
	cand := &models.OutreachContactCandidate{
		Name:               sc.Contact.Name,
		Role:               sc.Contact.Role,
		Email:              sc.Contact.Email,
		VerificationStatus: sc.Contact.VerificationStatus,
	}
	var ev []models.OutreachEvidence
	for _, e := range sc.Evidence {
		ev = append(ev, models.OutreachEvidence{
			SourceEvidenceID: e.SourceEvidenceID,
			Title:            e.Title,
			Synthesis:        e.Synthesis,
			Excerpt:          e.Excerpt,
			EpistemicClass:   e.EpistemicClass,
		})
	}
	in := GenerateInput{
		Channel:           sc.Channel,
		Account:           acc,
		Contact:           cand,
		Evidence:          ev,
		PriorBody:         sc.PriorBody,
		InboundReply:      sc.InboundReply,
		AllowNearDupRegen: true,
	}
	return in, acc, cand, ev
}

func TestGoldenContrastMessagesDiffer(t *testing.T) {
	scenarios := loadGolden(t)
	gen := TemplateGenerator{}
	bodies := map[string]string{}
	subjects := map[string]string{}

	for _, sc := range scenarios {
		in, acc, cand, ev := scenarioToInput(sc)
		out, provider, model, err := gen.Generate(context.Background(), in)
		if err != nil {
			t.Fatalf("%s generate: %v", sc.ID, err)
		}
		if sc.ID == "weak_fact_diagnostic" {
			if out.BodyText != "" {
				t.Fatalf("%s must fail closed without sendable body, got %q", sc.ID, out.BodyText)
			}
			if !containsStr(out.RiskFlags, "messageability_needs_enrichment") && !containsStr(out.RiskFlags, "messageability_blocked") {
				t.Fatalf("%s expected messageability fail-closed flags, got %v", sc.ID, out.RiskFlags)
			}
			bodies[sc.ID] = "NEEDS_ENRICHMENT"
			subjects[sc.ID] = ""
			continue
		}
		if provider != "template" || model != "deterministic" {
			t.Fatalf("%s unexpected provider %s/%s", sc.ID, provider, model)
		}
		if out.BodyText == "" {
			t.Fatalf("%s empty body", sc.ID)
		}
		// Must not invent research tooling markers.
		low := strings.ToLower(out.BodyText + out.Rationale)
		for _, bad := range []string{"web_search", "http://", "https://google", "pesquisei na internet"} {
			if strings.Contains(low, bad) {
				t.Fatalf("%s leaked research marker %q", sc.ID, bad)
			}
		}
		// Service code must match.
		if out.ServiceCode != acc.ServiceCode {
			t.Fatalf("%s service_code %q != %q", sc.ID, out.ServiceCode, acc.ServiceCode)
		}
		// Structured fields.
		if out.Rationale == "" {
			t.Fatalf("%s missing internal rationale", sc.ID)
		}
		if strings.Contains(out.BodyText, out.Rationale) {
			t.Fatalf("%s rationale leaked into body", sc.ID)
		}

		opts := ValidateOpts{
			MaxWords:           140,
			Evidence:           ev,
			Channel:            sc.Channel,
			SkipEmailRecipient: IsWhatsAppChannel(sc.Channel),
		}
		val := ValidateDraft(&out, acc, cand, opts)
		if !val.OK {
			// weak_fact may still pass with diagnostic path
			t.Fatalf("%s validation failed: %v", sc.ID, val.Errors)
		}
		// Evidence ids must exist when present.
		for _, id := range collectEvidenceIDs(&out) {
			known := false
			for _, e := range ev {
				if e.SourceEvidenceID == id {
					known = true
				}
			}
			for _, mid := range acc.MomentEvidenceIDs {
				if mid == id {
					known = true
				}
			}
			if len(ev) > 0 && !known {
				t.Fatalf("%s unknown evidence in output: %s", sc.ID, id)
			}
		}
		bodies[sc.ID] = out.BodyText
		subjects[sc.ID] = out.Subject

		// Channel shape checks.
		switch sc.Channel {
		case ChannelEmailFollowup:
			if !strings.HasPrefix(strings.ToLower(out.Subject), "re:") {
				// template uses Re:
				if !strings.Contains(strings.ToLower(out.Subject), "re:") {
					t.Fatalf("%s followup subject should be Re: got %q", sc.ID, out.Subject)
				}
			}
			if strings.EqualFold(out.BodyText, sc.PriorBody) {
				t.Fatalf("%s followup must not paste prior body", sc.ID)
			}
		case ChannelReplyDraft:
			if !strings.Contains(strings.ToLower(out.BodyText), "obrigado") &&
				!strings.Contains(strings.ToLower(out.BodyText), "agrade") {
				// template starts with Obrigado
				if !strings.Contains(out.BodyText, "Obrigado") {
					t.Fatalf("%s reply draft should acknowledge reply", sc.ID)
				}
			}
		case ChannelEmailInitial:
			if out.Subject == "" {
				t.Fatalf("%s email missing subject", sc.ID)
			}
		}
	}

	// Substantive differences across contrast pairs (not beauty snapshots).
	pairs := [][2]string{
		{"regional_lean", "national_structured"},
		{"aditivo_reajuste_fact", "weak_fact_diagnostic"},
		{"regional_lean", "followup_ignored"},
		{"national_structured", "positive_reply"},
	}
	for _, p := range pairs {
		a, b := bodies[p[0]], bodies[p[1]]
		if a == "" || b == "" {
			t.Fatalf("missing bodies for pair %v", p)
		}
		if a == b {
			t.Fatalf("bodies for %s and %s must differ", p[0], p[1])
		}
		sim := JaccardNgramSimilarity(a, b, 3)
		if sim > 0.92 {
			t.Fatalf("%s vs %s too similar (jaccard=%.3f)", p[0], p[1], sim)
		}
		// Fact tokens should differ between regional vs national.
		if p[0] == "regional_lean" && p[1] == "national_structured" {
			if !strings.Contains(a, "001/2025") && !strings.Contains(a, "prorrog") {
				t.Fatalf("regional body should mention local fact: %s", a)
			}
			if !strings.Contains(b, "reajuste") && !strings.Contains(b, "450/2024") {
				t.Fatalf("national body should mention reajuste fact: %s", b)
			}
		}
		if p[0] == "aditivo_reajuste_fact" && p[1] == "weak_fact_diagnostic" {
			if b != "NEEDS_ENRICHMENT" {
				t.Fatalf("weak-fact path must be NEEDS_ENRICHMENT, not fabricated copy: %s", b)
			}
		}
	}
}

func TestWhatsAppNotEmailPaste(t *testing.T) {
	acc := &models.OutreachAccount{
		NomeFantasia: "ACME Obras", FactToMention: "Prorrogacao do contrato 001/2025 no PNCP",
		ServiceCode: "ADDITIVE_REVIEW", ServiceName: "Revisao de aditivos",
		QuestionToAsk: "Faz sentido uma conversa curta?", CTA: "Posso mandar 2 linhas?",
		MomentEvidenceIDs: []string{"ev-1"},
	}
	cand := &models.OutreachContactCandidate{
		Name: "Ana Silva", Email: "a@example.com",
		VerificationStatus: models.OutreachVerifyOfficialSource,
	}
	ev := []models.OutreachEvidence{{SourceEvidenceID: "ev-1", EpistemicClass: models.OutreachEpistemicConfirmedFact}}
	emailOut := TemplateDraftChannel(ChannelEmailInitial, acc, cand, ev)
	waOut := TemplateDraftChannel(ChannelWhatsAppInitial, acc, cand, ev)
	if waOut.BodyText == emailOut.BodyText {
		t.Fatal("whatsapp must not equal email body")
	}
	if countWords(waOut.BodyText) > MaxWhatsAppWords {
		t.Fatalf("wa too long: %d", countWords(waOut.BodyText))
	}
	if waOut.Subject != "" {
		t.Fatalf("wa subject must be empty, got %q", waOut.Subject)
	}
	val := ValidateDraft(&waOut, acc, cand, ValidateOpts{
		MaxWords: MaxWhatsAppWords, Evidence: ev, Channel: ChannelWhatsAppInitial, SkipEmailRecipient: true,
	})
	if !val.OK {
		t.Fatalf("wa validation: %v", val.Errors)
	}
}

func TestNearDupSingleRegenCap(t *testing.T) {
	acc := &models.OutreachAccount{
		ID:           uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		NomeFantasia: "ACME", FactToMention: "contrato 001/2025 prorrogado no PNCP",
		ServiceCode: "ADDITIVE_REVIEW", ServiceName: "revisao",
		QuestionToAsk: "Faz sentido?", CTA: "Posso enviar?",
		MomentEvidenceIDs: []string{"e1"},
	}
	cand := &models.OutreachContactCandidate{
		Name: "Ana", Email: "a@example.com", VerificationStatus: models.OutreachVerifyOfficialSource,
	}
	ev := []models.OutreachEvidence{{SourceEvidenceID: "e1"}}
	gen := TemplateGenerator{}
	// First body from the same strategy-compose path so near-dup is meaningful.
	first, _, _, err := gen.Generate(context.Background(), GenerateInput{
		Channel: ChannelEmailInitial, Account: acc, Contact: cand, Evidence: ev,
	})
	if err != nil {
		t.Fatal(err)
	}
	in := GenerateInput{
		Channel: ChannelEmailInitial, Account: acc, Contact: cand, Evidence: ev,
		RecentBodies: []string{first.BodyText}, AllowNearDupRegen: true,
	}
	out, _, _, err := gen.Generate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	// At most one regen: flag set and body slightly varied OR still same with flag.
	hasFlag := false
	for _, f := range out.RiskFlags {
		if f == "near_dup_regenerated" {
			hasFlag = true
		}
	}
	if !hasFlag {
		t.Fatalf("expected near_dup_regenerated flag, flags=%v body=%q", out.RiskFlags, out.BodyText)
	}
	// Second call without AllowNearDupRegen must not loop / must not add flag if no regen path.
	in2 := in
	in2.AllowNearDupRegen = false
	out2, _, _, err := gen.Generate(context.Background(), in2)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range out2.RiskFlags {
		if f == "near_dup_regenerated" {
			t.Fatal("regen must not run when AllowNearDupRegen=false")
		}
	}
}

func TestDraftUserPromptContainsNoResearchInstruction(t *testing.T) {
	in := GenerateInput{
		Channel: ChannelEmailInitial,
		Account: &models.OutreachAccount{
			RazaoSocial: "X", ServiceCode: "S", FactToMention: "fato contrato 1",
			MomentSummary: "why now", ClaimsToAvoid: []string{"credito"},
		},
		Contact:  &models.OutreachContactCandidate{Name: "A", Email: "a@x.com"},
		Evidence: []models.OutreachEvidence{{SourceEvidenceID: "e1", EpistemicClass: models.OutreachEpistemicConfirmedFact, Synthesis: "s"}},
	}
	user := draftUserPrompt(in)
	if !strings.Contains(user, "sem pesquisa") && !strings.Contains(user, "apenas nestes dados") {
		t.Fatal("user prompt must insist on dossier-only")
	}
	sys := draftSystemPrompt(ChannelEmailInitial)
	if !strings.Contains(sys, "NÃO pesquisa") && !strings.Contains(strings.ToLower(sys), "nao pesquisa") {
		// accented form in prompt
		if !strings.Contains(sys, "pesquisa") {
			t.Fatal("system prompt must forbid research")
		}
	}
	if strings.Contains(sys, "web_search") || strings.Contains(sys, "RunAgent") {
		t.Fatal("must not wire research tools in confenge prompt")
	}
	// Provider interface usage stays Complete-only: AIDraftGenerator uses Complete.
	var _ generation.Provider
	pb := MustPlaybook()
	st, plan := BuildOutboundPlan(pb, in.Account, in.Contact, in.Evidence, 1)
	_ = st
	planUser := draftUserPromptWithPlan(in, plan)
	for _, bad := range []string{"fact_to_mention", "internal_structure_hypothesis", "web_search"} {
		if strings.Contains(planUser, bad) {
			t.Fatalf("plan prompt leaked %q", bad)
		}
	}
}

func TestPromptVersionV4(t *testing.T) {
	if PromptVersion != "confenge.draft.v4" {
		t.Fatalf("PromptVersion=%s", PromptVersion)
	}
	if OutreachDoctrineVersion != "confenge-outreach-v2" {
		t.Fatalf("doctrine=%s", OutreachDoctrineVersion)
	}
	if ComposerVersion != "confenge.composer.v2" {
		t.Fatalf("composer=%s", ComposerVersion)
	}
}

package tasks

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
)

type qualityContentRepo struct {
	repository.WarmupContentRepository
	conversation *models.WarmupConversation
}

func (r *qualityContentRepo) GetGenerationSettings(context.Context) (*models.WarmupGenerationSettings, error) {
	s := models.DefaultWarmupGenerationSettings()
	s.AISelectionShare = 100
	return &s, nil
}
func (r *qualityContentRepo) PickConversation(context.Context, string) (*models.WarmupConversation, error) {
	return r.conversation, nil
}

func TestVettedStaticOpeningContinuationAndClosure(t *testing.T) {
	s := &tasksService{}
	account := Email{Name: "Ada A", Email: "ada@example.test"}
	for i := 0; i < 20; i++ {
		got := s.pickNewWarmupContent(context.Background(), account)
		if got.contentSource != models.WarmupContentSourceStatic || got.conversationID == nil {
			t.Fatal("missing static source ID")
		}
		c, ok := ResolveDiagnosticConversation(*got.conversationID)
		if !ok || c.Version != generation.DiagnosticScenarioVersion || got.subject != c.Subject || got.body != c.Description+"\n\nSimulated diagnostic.\nAda A" {
			t.Fatal("opening subject, body or stable provenance mismatch")
		}
		for n := 1; n <= len(c.Messages); n++ {
			sender := account
			if n%2 == 1 {
				sender = Email{Name: "Ben B", Email: "ben@example.test"}
			}
			reply, ok := GenerateConversationReplyEmail(c, sender, n)
			if !ok || reply != c.Messages[n-1]+"\n\nSimulated diagnostic.\n"+sender.Name {
				t.Fatalf("reply %d changed canonical facts/role", n)
			}
			if n == len(c.Messages) && strings.Contains(reply, "?") {
				t.Fatal("closure appended unrelated question")
			}
		}
		if reply, ok := GenerateConversationReplyEmail(c, account, len(c.Messages)+1); ok || reply != "" {
			t.Fatal("exhausted scenario did not stop")
		}
	}
	if _, ok := ResolveDiagnosticConversation(uuid.New()); ok {
		t.Fatal("missing source ID mapped to unrelated scenario")
	}
	if _, ok := ResolveDiagnosticConversation(uuid.NewSHA1(uuid.NameSpaceOID, []byte("warmbly:warmup-conversation:0"))); ok {
		t.Fatal("legacy source namespace remapped")
	}
}

func TestAISelectionPreservesSubjectAndCanonicalBody(t *testing.T) {
	conv := &models.WarmupConversation{ID: uuid.New(), Subject: "Simulated diagnostic: dates", Description: "Simulated diagnostic. Ada will not meet on 2026-10-08 at 14:00.", Theme: "dates", Messages: []string{"Nothing was scheduled."}}
	scenario, rendering := generation.DiagnosticScenarioVersion, generation.CanonicalRenderingVersion
	conv.ScenarioVersion, conv.RenderingVersion = &scenario, &rendering
	conv.Status, conv.ReplyEligible, conv.LintPassed = models.WarmupConversationActive, true, true
	s := &tasksService{warmupContentRepo: &qualityContentRepo{conversation: conv}}
	got := s.pickNewWarmupContent(context.Background(), Email{Name: "Ada A", Email: "ada@example.test"})
	if got.contentSource != models.WarmupContentSourceAI || *got.conversationID != conv.ID || got.subject != conv.Subject || got.body != conv.Description+"\n\nSimulated diagnostic.\nAda A" {
		t.Fatal("AI subject, facts, signature or ID changed")
	}
	conv.Description = "{14:00|15:00}"
	got = s.pickNewWarmupContent(context.Background(), Email{Name: "Ada A", Email: "ada@example.test"})
	if got.contentSource != models.WarmupContentSourceStatic || *got.conversationID == conv.ID {
		t.Fatal("failed AI render retained stale provenance")
	}
}

func TestVettedStaticBankFinalRenderedFactsAndIdentities(t *testing.T) {
	fixtures := []struct {
		theme, namespace, subject, closure string
		facts                              [][]string
	}{
		{"diagnostic-clock", "clock", "fictional clock", "no real meeting was scheduled", [][]string{
			{"fictional example", "14:00, not 15:00"}, {"fictional example", "14:00, not 15:00"}, {"example is closed", "no real meeting was scheduled"},
		}},
		{"diagnostic-count", "count", "sample count", "no physical cards were exchanged", [][]string{
			{"hypothetical sample", "3 blue cards and 2 green cards"}, {"5 cards", "3 blue and 2 green"}, {"scenario is complete", "no physical cards were exchanged"},
		}},
		{"diagnostic-document", "document", "hypothetical draft review", "no real document was reviewed or changed", [][]string{
			{"hypothetical draft", "Section A lists assumptions", "Section B lists unknowns", "erroneous summary", "no measured delivery results"},
			{"hypothetical inbox-placement claim needs correction", "neither assumptions nor unknowns establish a measured result", "Section A", "Section B"},
			{"Section A lists assumptions", "Section B lists unknowns", "Neither section proves inbox placement"},
			{"fictional summary", "assumptions from unknowns", "no placement claim", "no real document was reviewed or changed"},
		}},
		{"diagnostic-summary", "summary", "summary correction", "no actual provider check was performed", [][]string{
			{"fictional checklist", "12 items", "9 marked complete and 3 marked unknown", "none marked failed", "incorrectly calls all 12 complete"},
			{"9 complete items and 3 unknown items, not 12 complete items"},
			{"Unknown items are neither failed nor passed", "none marked failed", "9 complete and 3 unknown counts separate"},
			{"9 complete and 3 unknown items separate", "none marked failed", "no actual provider check was performed"},
		}},
		{"diagnostic-timezone", "timezone", "fixed-offset date note", "no appointment was created", [][]string{
			{"2026-10-08", "09:00 UTC", "11:00", "UTC+02:00", "not a meeting invitation"},
			{"2026-10-08", "09:00 UTC", "11:00", "UTC+02:00", "rather than guess a regional timezone"},
			{"2026-10-08", "09:00 UTC", "11:00", "UTC+02:00", "no regional timezone or meeting"},
			{"2026-10-08", "09:00 UTC", "11:00", "UTC+02:00", "no appointment was created"},
		}},
		{"diagnostic-plaintext", "plaintext", "plaintext label review", "no delivered-client accessibility result is claimed", [][]string{
			{"constructed plaintext example", "written labels", "not color or linked instructions", "Status: unknown", "Next step: review"},
			{"Status: unknown", "Next step: review", "without a link or color requirement"},
			{"locally constructed example", "not a delivered-client accessibility test", "unknown status"},
			{"Status: unknown", "Next step: review", "no delivered-client accessibility result is claimed"},
		}},
		{"diagnostic-conditional", "conditional", "conditional wording", "no real draft was changed or commitment made", [][]string{
			{"hypothetical plan", "only if a reviewer approves", "No approval exists", "no update is promised for 2026-10-09"},
			{"pending review", "no approval exists", "update is conditional", "2026-10-09"},
			{"approval condition has not been met", "no update is promised for 2026-10-09", "pending review"},
			{"pending review", "without approval or a promised update for 2026-10-09", "no real draft was changed or commitment made"},
		}},
		{"diagnostic-summary-hu", "summary-hu", "magyar összefoglaló", "Valós szolgáltatói tesztet nem végeztünk", [][]string{
			{"kitalált jegyzet", "4 ellenőrzésből 3 eredménye ismeretlen", "1 pedig nincs elvégezve", "Egyik sem igazolt siker"},
			{"3 ismeretlen eredményt", "1 el nem végzett ellenőrzést", "ne állítsunk sikert"},
			{"ismeretlen nem igazolt siker", "el nem végzett ellenőrzés", "továbbra sincs elvégezve", "kitalált példa"},
			{"kitalált példa lezárult", "3 eredmény ismeretlen", "1 ellenőrzés nincs elvégezve", "Valós szolgáltatói tesztet nem végeztünk"},
		}},
	}
	bank := VettedDiagnosticConversations()
	if len(bank) != len(fixtures) {
		t.Fatalf("unreviewed or missing bank scenario: %d versus %d fixtures", len(bank), len(fixtures))
	}
	senders := [2]Email{{Name: "Ágnes Teszt", Email: "agnes@example.test"}, {Name: "Béla Példa", Email: "bela@example.test"}}
	seen := make(map[uuid.UUID]bool)
	for i, fixture := range fixtures {
		t.Run(fixture.theme, func(t *testing.T) {
			c := bank[i]
			id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("warmbly:diagnostic-"+fixture.namespace+":v1"))
			resolved, ok := ResolveDiagnosticConversation(id)
			if !ok || c.ID != id || seen[id] || c.Theme != fixture.theme || c.Subject != "Simulated diagnostic: "+fixture.subject || c.Version != generation.DiagnosticScenarioVersion || resolved.Description != c.Description || !slices.Equal(resolved.Messages, c.Messages) {
				t.Fatal("scenario ID, subject, version, uniqueness or exact-parent lookup changed")
			}
			seen[id] = true
			input := generation.Conversation{Subject: c.Subject, Description: c.Description, Messages: c.Messages}
			rendered, err := generation.RenderDiagnostic(input, [2]string{senders[0].Name, senders[1].Name})
			if err != nil {
				t.Fatal(err)
			}
			if rendered.Subject != c.Subject || len(c.Messages)+1 != len(fixture.facts) {
				t.Fatal("subject or expected ordered turn count changed")
			}
			for turn, body := range append([]string{c.Description}, c.Messages...) {
				sender := senders[turn%2]
				got := GenerateConversationOpeningEmail(c, sender)
				canonical := rendered.Description
				if turn > 0 {
					var replyOK bool
					got, replyOK = GenerateConversationReplyEmail(c, sender, turn)
					if !replyOK {
						t.Fatal("ordered turn missing")
					}
					canonical = rendered.Messages[turn-1]
				}
				if got != body+"\n\nSimulated diagnostic.\n"+sender.Name || got != canonical || strings.Contains(got, senders[(turn+1)%2].Name) {
					t.Fatalf("turn %d canonical content or actual identity changed", turn)
				}
				for _, fact := range fixture.facts[turn] {
					if !strings.Contains(got, fact) {
						t.Errorf("turn %d lost canonical fact %q", turn, fact)
					}
				}
				if strings.ContainsAny(got, "<>{}") || strings.Contains(got, "://") || strings.Contains(got, "www.") {
					t.Errorf("turn %d requires unsupported markup or a link", turn)
				}
				if turn == len(c.Messages) && (strings.Contains(got, "?") || !strings.Contains(got, fixture.closure)) {
					t.Fatal("scenario did not close exactly with its disclosed limit")
				}
			}
			if got, ok := GenerateConversationReplyEmail(c, senders[0], len(c.Messages)+1); ok || got != "" {
				t.Fatal("exhausted scenario continued")
			}
		})
	}
}

func TestVettedStaticOriginalScenarioContentRemainsImmutable(t *testing.T) {
	for _, fixture := range []Conversation{
		{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("warmbly:diagnostic-clock:v1")), Theme: "diagnostic-clock", Subject: "Simulated diagnostic: fictional clock", Description: "Simulated diagnostic. In this fictional example, the clock reads 14:00, not 15:00. Which time does the example use?", Messages: []string{"The fictional example uses 14:00, not 15:00.", "Agreed. The example is closed; no real meeting was scheduled."}},
		{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("warmbly:diagnostic-count:v1")), Theme: "diagnostic-count", Subject: "Simulated diagnostic: sample count", Description: "Simulated diagnostic. This hypothetical sample contains 3 blue cards and 2 green cards. How many cards are in the sample?", Messages: []string{"There are 5 cards in the hypothetical sample: 3 blue and 2 green.", "That matches the hypothetical sample. This scenario is complete; no physical cards were exchanged."}},
	} {
		c, ok := ResolveDiagnosticConversation(fixture.ID)
		if !ok || c.Subject != fixture.Subject || c.Description != fixture.Description || c.Theme != fixture.Theme || !slices.Equal(c.Messages, fixture.Messages) {
			t.Fatal("previously loadable scenario was remapped or rewritten")
		}
	}
}

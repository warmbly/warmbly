package tasks

import (
	"context"
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

package tasks

import (
	"context"
	"errors"
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
	pickErr      error
	lastPick     repository.WarmupContentPick
	seeded       []models.WarmupConversation
}

func (r *qualityContentRepo) GetGenerationSettings(context.Context) (*models.WarmupGenerationSettings, error) {
	s := models.DefaultWarmupGenerationSettings()
	return &s, nil
}
func (r *qualityContentRepo) PickConversation(_ context.Context, pick repository.WarmupContentPick) (*models.WarmupConversation, error) {
	r.lastPick = pick
	return r.conversation, r.pickErr
}
func (r *qualityContentRepo) SeedStaticConversations(_ context.Context, rows []models.WarmupConversation) error {
	r.seeded = rows
	return nil
}

func TestWarmupContentPreservesRepositoryFailure(t *testing.T) {
	want := errors.New("temporary content repository failure")
	s := &tasksService{warmupContentRepo: &qualityContentRepo{pickErr: want}}
	_, err := s.pickNewWarmupContent(t.Context(), Email{ID: uuid.New()}, uuid.New(), uuid.New())
	if !errors.Is(err, want) || errors.Is(err, errWarmupContentUnavailable) {
		t.Fatalf("repository failure became content exhaustion: %v", err)
	}
}

func TestStaticLibraryReadsAsOrdinaryMail(t *testing.T) {
	library := warmupConversations()
	if len(library) < 60 {
		t.Fatalf("static library has %d conversations; it is the only content a self-host without AI sends", len(library))
	}
	senders := [2]Email{{Name: "Ada Lovelace", Email: "[EMAIL]"}, {Name: "Ben Hart", Email: "[EMAIL]"}}
	ids, subjects, openings := map[uuid.UUID]bool{}, map[string]bool{}, map[string]bool{}
	for _, c := range library {
		t.Run(c.Subject, func(t *testing.T) {
			subject, opening := strings.ToLower(c.Subject), strings.ToLower(c.Description)
			if ids[c.ID] || subjects[subject] || openings[opening] {
				t.Fatal("duplicate id, subject or opening")
			}
			ids[c.ID], subjects[subject], openings[opening] = true, true, true
			if c.Theme == "" || c.Version != generation.WarmupScenarioVersion || len(c.Messages) < 2 || len(c.Messages) > 4 {
				t.Fatal("missing theme, wrong version or reply count outside 2 to 4")
			}
			if err := generation.ValidateFreshWarmup(generation.Conversation{Subject: c.Subject, Description: c.Description, Messages: c.Messages}); err != nil {
				t.Fatal(err)
			}
			if resolved, ok := resolveStaticConversation(c.ID); !ok || resolved.Subject != c.Subject {
				t.Fatal("reply lineage cannot resolve the conversation")
			}
			for turn := 0; turn <= len(c.Messages); turn++ {
				sender := senders[turn%2]
				body := GenerateConversationOpeningEmail(c, sender)
				if turn > 0 {
					var ok bool
					if body, ok = GenerateConversationReplyEmail(c, sender, turn); !ok {
						t.Fatalf("turn %d missing", turn)
					}
				}
				if !strings.HasSuffix(body, "\n"+sender.Name) {
					t.Fatalf("turn %d not signed by its sender", turn)
				}
				if err := lintDiagnosticContent(c.Subject, body, turn > 0, sender); err != nil {
					t.Fatalf("turn %d fails the dispatch lint: %v", turn, err)
				}
				if strings.Contains(body, "://") || strings.Contains(body, "@") {
					t.Fatalf("turn %d carries a link or address", turn)
				}
			}
		})
	}
}

func TestAISelectionPreservesSubjectAndCanonicalBody(t *testing.T) {
	conv := &models.WarmupConversation{ID: uuid.New(), Source: models.WarmupContentSourceAI, Subject: "Notebook habits", Description: "I've gone back to paper notes for meetings. Do you still keep yours digital?", Theme: "note-taking", Messages: []string{"Digital, mostly, though paper sticks better.", "That matches what I've noticed. Thanks for the nudge."}}
	scenario, rendering := generation.WarmupScenarioVersion, generation.CanonicalRenderingVersion
	conv.ScenarioVersion, conv.RenderingVersion = &scenario, &rendering
	conv.Status, conv.ReplyEligible, conv.LintPassed = models.WarmupConversationActive, true, true
	conv.SemanticReview = "passed"
	repo := &qualityContentRepo{conversation: conv}
	s := &tasksService{warmupContentRepo: repo}
	got, err := s.pickNewWarmupContent(context.Background(), Email{Name: "Ada A", Email: "[EMAIL]"}, uuid.New(), uuid.New())
	if err != nil || got.contentSource != models.WarmupContentSourceAI || *got.conversationID != conv.ID || got.subject != conv.Subject || got.body != conv.Description+"\n\nAda A" {
		t.Fatal("AI subject, body, signature or ID changed")
	}
	if !repo.lastPick.IncludeAI || len(repo.lastPick.StaticIDs) != len(warmupConversations()) || len(repo.seeded) != len(warmupConversations()) {
		t.Fatal("pick did not admit AI and the current static library, or the library was not seeded")
	}
}

func TestNewWarmupFallsBackToTheStaticLibrary(t *testing.T) {
	scenario, rendering := generation.WarmupScenarioVersion, generation.CanonicalRenderingVersion
	for name, repo := range map[string]*qualityContentRepo{
		"no repository": nil,
		"empty bank":    {},
		"retired wording": {conversation: &models.WarmupConversation{ID: uuid.New(), Source: models.WarmupContentSourceAI, Subject: "Simulated diagnostic: dates", Description: "Simulated diagnostic. A date example.", Messages: []string{"Closed."},
			Status: models.WarmupConversationActive, ReplyEligible: true, LintPassed: true, SemanticReview: "passed", ScenarioVersion: &scenario, RenderingVersion: &rendering}},
	} {
		t.Run(name, func(t *testing.T) {
			s := &tasksService{}
			if repo != nil {
				s.warmupContentRepo = repo
			}
			content, err := s.pickNewWarmupContent(t.Context(), Email{Name: "Ada", Email: "[EMAIL]"}, uuid.New(), uuid.New())
			if err != nil || content.contentSource != models.WarmupContentSourceStatic || content.conversationID == nil || content.body == "" {
				t.Fatalf("warmup did not fall back to the static library: %+v, %v", content, err)
			}
			if _, ok := resolveStaticConversation(*content.conversationID); !ok || generation.ValidateWarmupWording(content.subject, content.body) != nil {
				t.Fatal("fallback content is not a current library conversation")
			}
		})
	}
}

func TestStaticPickFromBankRendersTheInCodeConversation(t *testing.T) {
	source := warmupConversations()[3]
	s := &tasksService{warmupContentRepo: &qualityContentRepo{conversation: &models.WarmupConversation{ID: source.ID, Source: models.WarmupContentSourceStatic, Subject: "stale copy"}}}
	got, err := s.pickNewWarmupContent(t.Context(), Email{Name: "Ada"}, uuid.New(), uuid.New())
	if err != nil || got.subject != source.Subject || got.contentSource != models.WarmupContentSourceStatic || got.scenarioVersion != generation.WarmupScenarioVersion || *got.conversationID != source.ID {
		t.Fatal("static bank row did not render the current in-code conversation")
	}
}

func TestHistoricalDiagnosticRepliesStop(t *testing.T) {
	legacy := uuid.NewSHA1(uuid.NameSpaceOID, []byte("warmbly:diagnostic-clock:v1"))
	candidate := &repository.WarmupReplyCandidate{ConversationID: &legacy, Subject: "Simulated diagnostic: fictional clock", ContentSource: models.WarmupContentSourceStatic, ScenarioVersion: "diagnostic-v1", RenderingVersion: generation.CanonicalRenderingVersion, MessageID: "[EMAIL]", MaxTurns: 3}
	s := &tasksService{}
	if _, _, _, err := s.exactReplyContent(t.Context(), candidate, Email{Name: "Ada"}); err == nil {
		t.Fatal("historical diagnostic thread continued")
	}
	if err := lintDiagnosticContent("A topic", "A reply.\n\nSimulated diagnostic.\n\nAda", true, Email{Name: "Ada"}); err == nil {
		t.Fatal("historical footer passed final dispatch lint")
	}
	if err := lintDiagnosticContent("A topic", "This automated warmup message checks placement.\n\nAda", false, Email{Name: "Ada"}); err == nil {
		t.Fatal("self-describing warmup wording passed final dispatch lint")
	}
}

func TestSenderIdentityIsNotHeldToTheWordingRule(t *testing.T) {
	c := warmupConversations()[0]
	for _, sender := range []Email{{Name: "Warmup Team"}, {Name: "Test Mailbox"}, {Email: "[EMAIL]"}} {
		if err := lintDiagnosticContent(c.Subject, GenerateConversationOpeningEmail(c, sender), false, sender); err != nil {
			t.Fatalf("%q: a sender's own name stopped its warmup: %v", sender.Name+sender.Email, err)
		}
	}
}

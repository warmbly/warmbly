package advanced

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/typesafe"
	"github.com/warmbly/warmbly/internal/repository"
)

type suppressionRepo struct {
	repository.AdvancedOutreachRepository
	events     []*models.DeliverabilityEvent
	suppressed []string
}

func (r *suppressionRepo) CreateDeliverabilityEvent(_ context.Context, e *models.DeliverabilityEvent) error {
	r.events = append(r.events, e)
	return nil
}

func (r *suppressionRepo) GetOutreachSettings(context.Context, uuid.UUID) (*models.AdvancedOutreachSettings, error) {
	s := &models.AdvancedOutreachSettings{}
	s.BouncePipeline.AutoSuppressOnBounce = true
	return s, nil
}

func (r *suppressionRepo) UpsertSuppressedRecipient(_ context.Context, e *models.SuppressedRecipient) error {
	r.suppressed = append(r.suppressed, e.Email)
	return nil
}

type countingJudge struct {
	calls  int
	answer string
}

func (j *countingJudge) Ask(context.Context, any, map[string]typesafe.Question) (*typesafe.Response, error) {
	j.calls++
	return &typesafe.Response{Answers: map[string]typesafe.Answer{"cause": {Choice: j.answer, Confidence: 0.95}}}, nil
}

// A bounce suppresses the address only when it could be about the address;
// the judge is asked only about a reason the status and wording cannot place.
func TestBounceSuppressesOnlyWhenTheAddressFailed(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		judge        string
		suppress     bool
		judgeCalls   int
	}{
		{"user unknown", "550 5.1.1 <lead@example.com>: Recipient address rejected: User unknown", "reputation_block", true, 0},
		{"exchange missing mailbox", "550 5.4.1 Recipient address rejected: Access denied. AS(201806281)", "reputation_block", true, 0},
		{"gmail policy block", "550-5.7.1 Our system has detected an unusual rate of unsolicited mail originating from your IP address.", "recipient_invalid", false, 0},
		{"spamhaus", "550 5.7.1 Service unavailable, Client host [[IP]] blocked using Spamhaus", "recipient_invalid", false, 0},
		{"mailbox full", "552 5.2.2 Mailbox full", "recipient_invalid", false, 0},
		{"synchronous refusal carrying only the status", "The mail server rejected the recipient: The SMTP server refused this recipient (SMTP rcpt to status 550); enhanced status 5.7.1", "recipient_invalid", false, 0},
		{"nothing telling, judge vouches", "Undelivered Mail Returned to Sender", "reputation_block", false, 1},
		{"nothing telling, judge does not", "Undelivered Mail Returned to Sender", "recipient_invalid", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &suppressionRepo{}
			judge := &countingJudge{answer: tc.judge}
			s := &service{repo: repo, bounceJudge: judge}
			if xerr := s.IngestDeliverabilityEvent(context.Background(), uuid.New(), &models.IngestDeliverabilityEventRequest{
				EventType: models.DeliverabilityEventBounce, RecipientEmail: "lead@example.com", Reason: tc.reason,
			}); xerr != nil {
				t.Fatal(xerr)
			}
			if len(repo.events) != 1 {
				t.Fatalf("the bounce event must always be recorded, got %d", len(repo.events))
			}
			if got := len(repo.suppressed) > 0; got != tc.suppress {
				t.Fatalf("suppressed = %v, want %v", got, tc.suppress)
			}
			if judge.calls != tc.judgeCalls {
				t.Fatalf("judge asked %d times, want %d", judge.calls, tc.judgeCalls)
			}
		})
	}

	// No judge configured: a reason nothing places keeps the conservative default.
	repo := &suppressionRepo{}
	s := &service{repo: repo}
	if xerr := s.IngestDeliverabilityEvent(context.Background(), uuid.New(), &models.IngestDeliverabilityEventRequest{
		EventType: models.DeliverabilityEventBounce, RecipientEmail: "lead@example.com", Reason: "554 Transaction failed",
	}); xerr != nil {
		t.Fatal(xerr)
	}
	if len(repo.suppressed) != 1 {
		t.Fatal("an unplaced bounce with no judge must still suppress")
	}
}

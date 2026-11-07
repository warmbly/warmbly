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
	calls      int
	answer     string
	confidence float64
}

func (j *countingJudge) Ask(context.Context, any, map[string]typesafe.Question) (*typesafe.Response, error) {
	j.calls++
	conf := j.confidence
	if conf == 0 {
		conf = 0.95
	}
	return &typesafe.Response{Answers: map[string]typesafe.Answer{"cause": {Choice: j.answer, Confidence: conf}}}, nil
}

const recipient = "lead" + "@" + "example.com"

func ingestBounce(t *testing.T, s *service, reason string, metadata map[string]interface{}) *models.IngestDeliverabilityEventRequest {
	t.Helper()
	req := &models.IngestDeliverabilityEventRequest{
		EventType: models.DeliverabilityEventBounce, RecipientEmail: recipient, Reason: reason, Metadata: metadata,
	}
	if xerr := s.IngestDeliverabilityEvent(context.Background(), uuid.New(), req); xerr != nil {
		t.Fatal(xerr)
	}
	return req
}

// A bounce suppresses the address only on clear evidence that the address
// failed; the judge is asked only about a reason the status and wording cannot place.
func TestBounceSuppressesOnlyWhenTheAddressFailed(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		judge        string
		confidence   float64
		suppress     bool
		kept         bool
		judgeCalls   int
	}{
		{"user unknown", "550 5.1.1 <" + recipient + ">: Recipient address rejected: User unknown", "reputation_block", 0, true, false, 0},
		{"exchange missing mailbox", "550 5.4.1 Recipient address rejected: Access denied. AS(201806281)", "reputation_block", 0, true, false, 0},
		{"gmail policy block", "550-5.7.1 Our system has detected an unusual rate of unsolicited mail originating from your IP address.", "recipient_invalid", 0, false, true, 0},
		{"spamhaus", "550 5.7.1 Service unavailable, Client host [192.0.2.1] blocked using Spamhaus", "recipient_invalid", 0, false, true, 0},
		{"mailbox full", "552 5.2.2 Mailbox full", "recipient_invalid", 0, false, true, 0},
		{"synchronous refusal carrying only the status", "The mail server rejected the recipient: The SMTP server refused this recipient (SMTP rcpt to status 550); enhanced status 5.7.1", "recipient_invalid", 0, false, true, 0},
		{"synchronous refusal quoting a block", "The mail server rejected the recipient: The SMTP server refused this recipient (SMTP rcpt to status 550); server replied: 550 Blocked by policy", "recipient_invalid", 0, false, true, 0},
		{"policy code beats weak wording", "550 5.7.1 mailbox unavailable", "recipient_invalid", 0, false, true, 0},
		{"nothing telling, judge vouches", "Undelivered Mail Returned to Sender", "reputation_block", 0, false, true, 1},
		{"nothing telling, judge names the address", "Undelivered Mail Returned to Sender", "recipient_invalid", 0, true, false, 1},
		{"nothing telling, judge unsure", "Undelivered Mail Returned to Sender", "recipient_invalid", 0.5, false, false, 1},
		{"nothing telling, judge says other", "Undelivered Mail Returned to Sender", "other", 0, false, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &suppressionRepo{}
			judge := &countingJudge{answer: tc.judge, confidence: tc.confidence}
			req := ingestBounce(t, &service{repo: repo, bounceJudge: judge}, tc.reason, nil)
			if len(repo.events) != 1 {
				t.Fatalf("the bounce event must always be recorded, got %d", len(repo.events))
			}
			if got := len(repo.suppressed) > 0; got != tc.suppress {
				t.Fatalf("suppressed = %v, want %v", got, tc.suppress)
			}
			if req.AddressKept != tc.kept {
				t.Fatalf("kept = %v, want %v", req.AddressKept, tc.kept)
			}
			if judge.calls != tc.judgeCalls {
				t.Fatalf("judge asked %d times, want %d", judge.calls, tc.judgeCalls)
			}
		})
	}

	// No judge configured: a reason nothing places leaves the address sendable.
	repo := &suppressionRepo{}
	ingestBounce(t, &service{repo: repo}, "554 Transaction failed", nil)
	if len(repo.suppressed) != 0 {
		t.Fatal("an unplaced bounce must not suppress the address")
	}

	// A cause in the request body never stands in for the classification.
	repo = &suppressionRepo{}
	ingestBounce(t, &service{repo: repo}, "550 5.1.1 User unknown", map[string]interface{}{"bounce_cause": "transient", "bounce_cause_source": "judge"})
	if got := repo.events[0].Metadata["bounce_cause"]; got != "recipient_invalid" || len(repo.suppressed) != 1 {
		t.Fatalf("bounce_cause = %v, suppressed %d", got, len(repo.suppressed))
	}
	repo = &suppressionRepo{}
	ingestBounce(t, &service{repo: repo}, "554 Transaction failed", map[string]interface{}{"bounce_cause": "mailbox_full"})
	if _, ok := repo.events[0].Metadata["bounce_cause"]; ok {
		t.Fatal("a caller-supplied cause survived an unplaced bounce")
	}
}

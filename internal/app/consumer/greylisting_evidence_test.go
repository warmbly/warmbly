package jobs

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// recordingEvidence captures what the send path taught verification.
type recordingEvidence struct {
	kinds []string
}

func (r *recordingEvidence) RecordEvidence(_ context.Context, _ uuid.UUID, _ models.EvidenceStep, kind, _, _ string) {
	r.kinds = append(r.kinds, kind)
}

// A greylisted recipient is retryable and supplies no address evidence.
func TestLiveGreylistedSendIsNotRecordedAsABounce(t *testing.T) {
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	ctx := context.Background()
	handle, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { handle.Pool.Close() })

	f := newSendResultFixture(t, handle)
	for _, tc := range []struct {
		name   string
		code   string
		reason string
		want   int
	}{
		{
			"greylisted",
			string(errx.MailErrorCodeServerUnreachable),
			`The connection to the mail server could not be established (rcpt to: 450 "4.7.1 <box@example.com>: Recipient address rejected: Greylisted"). The server may be offline or blocking the connection.`,
			0,
		},
		{
			"no such user",
			string(errx.MailErrorCodeRecipientRejected),
			`The mail server rejected the recipient: 550 "5.1.1 <box@example.com>: no such user"`,
			1,
		},
	} {
		ev := &recordingEvidence{}
		s := &JobsService{
			TaskRepo:             repository.NewTaskRepository(handle.Pool),
			CampaignRepo:         repository.NewCampaignRepostory(handle),
			CampaignProgressRepo: repository.NewCampaignProgressRepository(handle.Pool),
			CampaignLogRepo:      repository.NewCampaignLogRepository(handle),
			ContactRepo:          repository.NewContactRepostory(handle),
			Evidence:             ev,
		}
		taskID := f.stampSend(t, s)
		if err := s.HandleEmailFailed(ctx, models.SendEmailResult{
			TaskID: taskID, Success: false,
			Error: &models.EmailSendError{Code: tc.code, Message: tc.reason, Failure: &errx.SendFailure{Disposition: map[string]string{"greylisted": errx.SendRetry, "no such user": errx.SendPermanent}[tc.name], Scope: "recipient", Stage: "rcpt to"}},
		}); err != nil {
			t.Fatalf("%s: handle failed: %v", tc.name, err)
		}
		if len(ev.kinds) != tc.want {
			t.Fatalf("%s: recorded %v, want %d evidence records", tc.name, ev.kinds, tc.want)
		}
	}
}

// evidenceTaskRepo supplies a stamped campaign task to the real failure handler.
type evidenceTaskRepo struct {
	repository.TaskRepository

	task *repository.Task
	ct   *repository.CampaignTask
}

func (r *evidenceTaskRepo) GetTask(context.Context, uuid.UUID) (*repository.Task, error) {
	return r.task, nil
}

func (r *evidenceTaskRepo) RecordTaskFailure(context.Context, uuid.UUID, string, string) error {
	return nil
}

func (r *evidenceTaskRepo) GetCampaignTask(context.Context, uuid.UUID) (*repository.CampaignTask, error) {
	return r.ct, nil
}

// evidenceProgressRepo reports that the campaign step was rolled back.
type evidenceProgressRepo struct {
	repository.CampaignProgressRepository
}

func (evidenceProgressRepo) RecordSendFailure(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) (int, bool, bool, error) {
	return 1, false, false, nil
}

// runFailedSend returns address evidence produced by the real failure handler.
func runFailedSend(t *testing.T, code, reason string) []string {
	t.Helper()
	campaign, contact, step := uuid.New(), uuid.New(), uuid.New()
	taskID := uuid.New()
	ev := &recordingEvidence{}
	s := &JobsService{
		TaskRepo: &evidenceTaskRepo{
			task: &repository.Task{ID: taskID, TaskType: "campaign", EmailAccountID: uuid.New(), Status: "completed"},
			ct:   &repository.CampaignTask{TaskID: taskID, CampaignID: &campaign, ContactID: &contact, SequenceID: &step},
		},
		CampaignProgressRepo: evidenceProgressRepo{},
		Evidence:             ev,
	}
	if err := s.HandleEmailFailed(context.Background(), models.SendEmailResult{
		TaskID: taskID, Success: false,
		Error: &models.EmailSendError{Code: code, Message: reason},
	}); err != nil {
		t.Fatalf("handle failed: %v", err)
	}
	return ev.kinds
}

func TestRetryableFailuresAreNotEvidenceAboutTheAddress(t *testing.T) {
	// Keep the evidence gate covered in CI without a live database.
	evidence := func(code, reason string) bool {
		return len(runFailedSend(t, code, reason)) > 0
	}

	for _, tc := range []struct {
		name   string
		code   string
		reason string
		want   bool
	}{
		{
			"greylisted",
			string(errx.MailErrorCodeServerUnreachable),
			`The connection to the mail server could not be established (rcpt to: 450 "4.7.1 <box@example.com>: Recipient address rejected: Greylisted, try again later"). The server may be offline or blocking the connection.`,
			false,
		},
		{
			"mailbox busy",
			string(errx.MailErrorCodeServerUnreachable),
			`The connection to the mail server could not be established (rcpt to: 452 "4.2.2 Mailbox unavailable, over quota"). The server may be offline or blocking the connection.`,
			false,
		},
		// Permanent refusal remains address evidence.
		{
			"address does not exist",
			string(errx.MailErrorCodeRecipientRejected),
			`The mail server rejected the recipient: 550 "5.1.1 <box@example.com>: no such user"`,
			true,
		},
		{
			"message refused naming the recipient",
			string(errx.MailErrorCodeSendRejected),
			`The receiving mail server refused this message: 550 "5.1.1 recipient not found"`,
			true,
		},
		// A dial that never reached a server names nobody either way.
		{
			"refused dial",
			string(errx.MailErrorCodeServerUnreachable),
			`The connection to the mail server could not be established (dial smtp.example.com:465: connect: connection refused). The server may be offline or blocking the connection.`,
			false,
		},
	} {
		if got := evidence(tc.code, tc.reason); got != tc.want {
			t.Errorf("%s: evidence = %v, want %v", tc.name, got, tc.want)
		}
	}
}

package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type retryResultAuthority struct {
	repository.SyncContextRepository
	calls, failures int
	result          *models.SendEmailResult
	t               *testing.T
}

func (a *retryResultAuthority) WarmupDispatch(ctx context.Context, task, _, _ uuid.UUID, _ uuid.UUID, start bool, result *models.SendEmailResult) (*repository.WarmupDispatchState, error) {
	a.t.Helper()
	if result == nil || start {
		a.t.Fatal("report retry attempted provider execution")
	}
	if ctx.Err() != nil {
		a.t.Fatal("recording inherited cancelled send context")
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 10*time.Second {
		a.t.Fatal("recording attempt is not bounded")
	}
	if task != result.TaskID {
		a.t.Fatal("report rebound outcome to another task")
	}
	a.calls++
	if a.calls <= a.failures {
		return nil, errors.New("temporary recording failure")
	}
	copy := *result
	a.result = &copy
	return nil, nil
}

func TestSendResultReportSurvivesCancelledSendAndRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	authority := &retryResultAuthority{t: t, failures: 1}
	bus := &dispatchResultBus{fail: true}
	w := &WorkerService{Codec: codec.NewJSON(), Bus: bus}
	p := pendingSendResult{result: models.SendEmailResult{TaskID: uuid.New(), Success: true, MessageID: "provider@example.test"}, dispatch: authority}
	w.pendingSendResults.Store(p.result.TaskID, p)
	if err := w.reportSendResult(ctx, p); err != nil {
		t.Fatal(err)
	}
	if authority.calls != 3 || authority.result.MessageID != p.result.MessageID || bus.events != 1 {
		t.Fatal("outcome not durably retried before publication", authority.calls, bus.events)
	}
	if _, ok := w.pendingSendResults.Load(p.result.TaskID); ok {
		t.Fatal("successful outcome remains pending")
	}
}

func TestSendResultExhaustionRetainsOutcomeForRedelivery(t *testing.T) {
	bus := &dispatchResultBus{fail: true, failures: 4}
	w := &WorkerService{Codec: codec.NewJSON(), Bus: bus}
	p := pendingSendResult{result: models.SendEmailResult{TaskID: uuid.New(), Success: true, MessageID: "accepted@example.test"}, mailboxID: uuid.New(), orgID: uuid.New(), recipients: []string{"recipient@example.test"}}
	w.pendingSendResults.Store(p.result.TaskID, p)
	if err := w.reportSendResult(t.Context(), p); err == nil || bus.events != 0 || bus.failures != 0 {
		t.Fatal("exhausted reporting was acknowledged")
	}
	command := models.SendEmail{TaskID: p.result.TaskID, EmailID: p.mailboxID, OrgID: p.orgID, To: []string{"recipient@example.test"}}
	foreign := command
	foreign.EmailID = uuid.New()
	if err := w.HandleSendEmail(t.Context(), foreign); err == nil {
		t.Fatal("foreign mailbox replayed outcome")
	}
	if err := w.HandleSendEmail(t.Context(), command); err != nil {
		t.Fatal(err)
	}
	if bus.events != 1 {
		t.Fatal("redelivery lost outcome")
	}
}

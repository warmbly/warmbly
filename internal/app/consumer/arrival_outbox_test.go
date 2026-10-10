package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type priorityArrivalStub struct {
	repository.ArrivalOutbox
	user, email, id uuid.UUID
	deliver         func(context.Context, func(context.Context, models.JobEventType, any) error) (bool, error)
}

func (s *priorityArrivalStub) DeliverPendingArrival(ctx context.Context, user, email, id uuid.UUID, deliver func(context.Context, models.JobEventType, any) error) (bool, error) {
	s.user, s.email, s.id = user, email, id
	return s.deliver(ctx, deliver)
}

func TestDurableArrivalUnblocksDependentEventBeforeAcknowledgement(t *testing.T) {
	for _, outcome := range []string{"delivered", "leased", "failed", "finished concurrently"} {
		t.Run(outcome, func(t *testing.T) {
			user, email, id := uuid.New(), uuid.New(), uuid.New()
			pending := &syncArrivalPendingError{user: user, email: email, id: id}
			ready, dependentCalls, arrivalCalls := false, 0, 0
			failed := errors.New("arrival delivery failed")
			outbox := &priorityArrivalStub{}
			s := &JobsService{ArrivalOutbox: outbox}
			s.InitEvents()
			Register(s, models.JobEventTypeNewEmail, func(context.Context, *models.JobEventNewEmail) error {
				arrivalCalls++
				ready = true
				return nil
			})
			Register(s, models.JobEventTypeRemoveEmail, func(context.Context, *models.JobEventRemoveEmail) error {
				dependentCalls++
				if !ready {
					return fmt.Errorf("dependent event: %w", pending)
				}
				return nil
			})
			outbox.deliver = func(ctx context.Context, deliver func(context.Context, models.JobEventType, any) error) (bool, error) {
				switch outcome {
				case "leased":
					return false, nil
				case "failed":
					return true, failed
				case "finished concurrently":
					ready = true
					return false, nil
				default:
					return true, deliver(ctx, models.JobEventTypeNewEmail, &models.JobEventNewEmail{})
				}
			}
			err := s.HandleEvent(t.Context(), &models.JobEvent{Type: models.JobEventTypeRemoveEmail, Body: &models.JobEventRemoveEmail{}})
			if outbox.user != user || outbox.email != email || outbox.id != id {
				t.Fatal("dependency lookup lost tenant/message scope")
			}
			switch outcome {
			case "leased":
				if !errors.Is(err, ErrSyncArrivalPending) || dependentCalls != 2 {
					t.Fatalf("leased arrival acknowledged: calls=%d err=%v", dependentCalls, err)
				}
			case "failed":
				if !errors.Is(err, failed) || dependentCalls != 1 {
					t.Fatalf("failed arrival overtaken: calls=%d err=%v", dependentCalls, err)
				}
			default:
				if err != nil || dependentCalls != 2 {
					t.Fatalf("dependency did not unblock: calls=%d err=%v", dependentCalls, err)
				}
			}
			if outcome == "delivered" && arrivalCalls != 1 {
				t.Fatalf("arrival calls=%d", arrivalCalls)
			}
		})
	}
}

func TestDurableArrivalDoesNotRetryOtherErrorsOrUnsupportedOutbox(t *testing.T) {
	for _, err := range []error{errors.New("unrelated handler failure"), &syncArrivalPendingError{user: uuid.New(), email: uuid.New(), id: uuid.New()}} {
		s := &JobsService{}
		s.InitEvents()
		calls := 0
		Register(s, models.JobEventTypeRemoveEmail, func(context.Context, *models.JobEventRemoveEmail) error { calls++; return err })
		if got := s.HandleEvent(t.Context(), &models.JobEvent{Type: models.JobEventTypeRemoveEmail, Body: &models.JobEventRemoveEmail{}}); !errors.Is(got, err) || calls != 1 {
			t.Fatalf("unsupported recovery changed handler result: calls=%d err=%v", calls, got)
		}
	}
}

func TestDurableArrivalRequiresRegisteredHandler(t *testing.T) {
	s := &JobsService{}
	if err := s.deliverArrivalEvent(context.Background(), models.JobEventTypeNewEmail, &models.JobEventNewEmail{}); err == nil {
		t.Fatal("unregistered durable event dropped")
	}
	called := false
	s.InitEvents()
	Register(s, models.JobEventTypeNewEmail, func(context.Context, *models.JobEventNewEmail) error { called = true; return nil })
	if err := s.deliverArrivalEvent(context.Background(), models.JobEventTypeNewEmail, &models.JobEventNewEmail{}); err != nil || !called {
		t.Fatalf("called=%v err=%v", called, err)
	}
}

type arrivalBacklogStub struct {
	repository.ArrivalOutbox
	backlog repository.ArrivalBacklog
	err     error
}

func (s *arrivalBacklogStub) ArrivalBacklog(context.Context) (repository.ArrivalBacklog, error) {
	return s.backlog, s.err
}

type arrivalOperatorStub struct {
	calls   int
	content string
	fields  map[string]string
}

func (s *arrivalOperatorStub) NotifyOperator(key, title, summary string, fields map[string]string) {
	s.calls++
	s.content = key + title + summary
	s.fields = fields
	if len(fields) != 2 {
		panic("unexpected private fields")
	}
}

func TestDurableArrivalBacklogAlertsAreAgedAndDeduplicated(t *testing.T) {
	now := time.Now()
	old := now.Add(-6 * time.Minute)
	outbox := &arrivalBacklogStub{backlog: repository.ArrivalBacklog{Pending: 8, Oldest: &old}}
	ops := &arrivalOperatorStub{}
	s := &JobsService{ArrivalOutbox: outbox, OpsNotifier: ops}
	var failed, alerted time.Time
	s.observeArrivalBacklog(t.Context(), nil, now, &failed, &alerted)
	s.observeArrivalBacklog(t.Context(), nil, now.Add(time.Minute), &failed, &alerted)
	if ops.calls != 1 {
		t.Fatalf("repeat alerts=%d", ops.calls)
	}
	outbox.backlog = repository.ArrivalBacklog{}
	s.observeArrivalBacklog(t.Context(), nil, now.Add(16*time.Minute), &failed, &alerted)
	if ops.calls != 1 {
		t.Fatal("empty queue claimed ongoing backlog")
	}
	outbox.err = errors.New("private database error")
	s.observeArrivalBacklog(t.Context(), nil, now.Add(17*time.Minute), &failed, &alerted)
	if ops.calls != 1 {
		t.Fatal("transient read failure alerted too soon")
	}
	s.observeArrivalBacklog(t.Context(), nil, now.Add(18*time.Minute), &failed, &alerted)
	if ops.calls != 2 {
		t.Fatal("persistent queue inspection failure was silent")
	}
	if strings.Contains(ops.content, "private database error") || ops.fields["Pending"] != "unavailable" || ops.fields["Oldest age seconds"] != "unavailable" {
		t.Fatal("operator notice exposed an error or fabricated unavailable counts")
	}
}

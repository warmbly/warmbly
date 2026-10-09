package jobs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

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

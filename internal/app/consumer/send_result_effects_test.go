package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type storedSendResultStub struct {
	repository.TaskRepository
	repository.SendResultRecovery
	list    func(context.Context, uuid.UUID, int) ([]repository.StoredSendResult, error)
	applied []models.SendEmailResult
	fail    uuid.UUID
}

func (r *storedSendResultStub) ListPendingSendResults(ctx context.Context, after uuid.UUID, limit int) ([]repository.StoredSendResult, error) {
	return r.list(ctx, after, limit)
}

func (r *storedSendResultStub) ApplySendResult(_ context.Context, result models.SendEmailResult, _ func(context.Context) error) error {
	r.applied = append(r.applied, result)
	if result.TaskID == r.fail {
		return errors.New("transient apply failure")
	}
	return nil
}

func TestStoredSendResultRecoveryContinuesPastInvalidAndFailedResults(t *testing.T) {
	malformed, mismatch, failed, sent := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	payload := func(id uuid.UUID, success bool) json.RawMessage {
		raw, err := json.Marshal(models.SendEmailResult{TaskID: id, Success: success})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	r := &storedSendResultStub{fail: failed}
	r.list = func(_ context.Context, after uuid.UUID, limit int) ([]repository.StoredSendResult, error) {
		if limit != 200 {
			t.Fatalf("unbounded replay: %d", limit)
		}
		if after == sent {
			return nil, nil
		}
		return []repository.StoredSendResult{
			{TaskID: malformed, Payload: json.RawMessage(`{"success":`)},
			{TaskID: mismatch, Payload: payload(uuid.New(), true)},
			{TaskID: failed, Payload: payload(failed, false)},
			{TaskID: sent, Payload: payload(sent, true)},
		}, nil
	}
	s := &JobsService{TaskRepo: r}
	after, err := s.recoverStoredSendResults(t.Context(), uuid.Nil)
	if err == nil || after != sent || len(r.applied) != 2 || r.applied[0].TaskID != failed || r.applied[1].TaskID != sent {
		t.Fatalf("one failed result starved the rest or an invalid result was applied: %s %v %+v", after, err, r.applied)
	}
	if r.applied[0].Success || !r.applied[1].Success {
		t.Fatal("result authority changed during replay")
	}
	after, err = s.recoverStoredSendResults(t.Context(), after)
	if err != nil || after != uuid.Nil {
		t.Fatalf("replay cursor did not wrap: %s %v", after, err)
	}
}

func TestStoredSendResultRecoveryRetainsCursorOnReadFailureOrCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		cursor := uuid.New()
		ctx, cancel := context.WithCancel(t.Context())
		r := &storedSendResultStub{}
		r.list = func(_ context.Context, after uuid.UUID, _ int) ([]repository.StoredSendResult, error) {
			if after != cursor {
				t.Fatal("cursor not forwarded")
			}
			if cancelled {
				return []repository.StoredSendResult{{TaskID: uuid.New()}}, nil
			}
			return nil, errors.New("database unavailable")
		}
		if cancelled {
			cancel()
		}
		s := &JobsService{TaskRepo: r}
		after, err := s.recoverStoredSendResults(ctx, cursor)
		cancel()
		if err == nil || after != cursor || len(r.applied) != 0 {
			t.Fatalf("unfinished batch skipped: %s %v %+v", after, err, r.applied)
		}
	}
}

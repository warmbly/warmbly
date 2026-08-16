package email

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/repository"
)

type stubHistoryRepo struct {
	data *repository.EmailHistoryIDData
	err  error
}

func (s *stubHistoryRepo) Put(ctx context.Context, userID, emailID uuid.UUID, historyID uint64) error {
	return nil
}

func (s *stubHistoryRepo) Get(ctx context.Context, userID, emailID uuid.UUID) (*repository.EmailHistoryIDData, error) {
	return s.data, s.err
}

func int64Ptr(v int64) *int64 { return &v }

func TestLastHistoryForPrefersSavedCheckpoint(t *testing.T) {
	s := &emailService{historyID: &stubHistoryRepo{data: &repository.EmailHistoryIDData{HistoryID: 65207}}}

	// The legacy column is deliberately set to a different value: the
	// checkpoint the consumer maintains is the one that must win.
	got := s.lastHistoryFor(context.Background(), uuid.New(), uuid.New(), int64Ptr(1))
	if got != 65207 {
		t.Fatalf("got %d, want the saved checkpoint 65207", got)
	}
}

// A zero cursor is what silently re-bootstraps the mailbox and skips mail, so
// the fallbacks matter as much as the happy path.
func TestLastHistoryForFallsBackToLegacyColumn(t *testing.T) {
	tests := []struct {
		name string
		repo repository.EmailHistoryIDRepository
	}{
		{"no row yet", &stubHistoryRepo{}},
		{"lookup failed", &stubHistoryRepo{err: errors.New("boom")}},
		{"row with zero id", &stubHistoryRepo{data: &repository.EmailHistoryIDData{HistoryID: 0}}},
		{"repository not wired", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &emailService{historyID: tt.repo}
			if got := s.lastHistoryFor(context.Background(), uuid.New(), uuid.New(), int64Ptr(42)); got != 42 {
				t.Errorf("got %d, want the legacy value 42", got)
			}
		})
	}
}

func TestLastHistoryForZeroWhenNothingKnown(t *testing.T) {
	s := &emailService{historyID: &stubHistoryRepo{}}

	if got := s.lastHistoryFor(context.Background(), uuid.New(), uuid.New(), nil); got != 0 {
		t.Fatalf("got %d, want 0 so the worker bootstraps a fresh baseline", got)
	}
}

package jobs

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type countingEmailRepo struct {
	repository.EmailRepository
	reads int
}

func (r *countingEmailRepo) GetByID(_ context.Context, id uuid.UUID) (*models.Email, *errx.Error) {
	r.reads++
	return &models.Email{ID: id}, nil
}

func TestMailboxMemoLoadsEachMailboxOncePerEvent(t *testing.T) {
	repo := &countingEmailRepo{}
	s := &JobsService{EmailRepository: repo}
	a, b := uuid.New(), uuid.New()
	ctx := withMailboxMemo(t.Context())
	for range 3 {
		s.recipientAccount(ctx, a)
		s.emailInboxEvent(ctx, uuid.New(), &models.EmailMessageStoreData{EmailID: a})
	}
	s.recipientAccount(ctx, b)
	if repo.reads != 2 {
		t.Fatalf("read %d times within one event, want once per mailbox", repo.reads)
	}
	s.recipientAccount(t.Context(), a)
	s.recipientAccount(t.Context(), a)
	if repo.reads != 4 {
		t.Fatal("reads outside an event were memoized")
	}
}

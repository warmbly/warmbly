package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type stubWarmupTokenRepo struct {
	repository.WarmupRepository

	live  *models.WarmupToken
	err   error
	reads int
}

func (s *stubWarmupTokenRepo) FindWarmupToken(context.Context, uuid.UUID) (*models.WarmupToken, error) {
	s.reads++
	return s.live, s.err
}

// Foreign or missing tokens remain ordinary mail and never trigger a tampering charge.
func TestHandleWarmupEmailFilesOtherTokensAsOrdinaryMail(t *testing.T) {
	mailbox := uuid.New()
	partner := uuid.New()
	token := uuid.New()
	e := &models.JobEventNewEmail{Message: &models.EmailMessageStoreData{EmailID: mailbox, Folder: models.FolderInbox}}

	foreign := &models.WarmupToken{Token: token, RecipientAccountID: partner, SenderAccountID: uuid.New()}

	tests := []struct {
		name    string
		tokStr  string
		repo    *stubWarmupTokenRepo
		wantErr bool
		reads   int
	}{
		{"a live token naming another pair: an attacker's to place, not evidence", token.String(), &stubWarmupTokenRepo{live: foreign}, false, 1},
		{"a token that is gone", token.String(), &stubWarmupTokenRepo{}, false, 1},
		{"a marker that does not parse", "not-a-uuid", &stubWarmupTokenRepo{}, false, 0},
		{"a failed lookup is surfaced, not charged", token.String(), &stubWarmupTokenRepo{err: errors.New("connection reset by peer")}, true, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &JobsService{WarmupRepo: tc.repo}
			handled, err := s.handleWarmupEmail(context.Background(), e, tc.tokStr)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if handled {
				t.Fatal("filed a token that was not this mailbox's as warmup")
			}
			if tc.repo.reads != tc.reads {
				t.Fatalf("FindWarmupToken called %d times, want %d", tc.repo.reads, tc.reads)
			}
		})
	}
}

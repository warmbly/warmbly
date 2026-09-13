package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type stubWarmupTokenRepo struct {
	repository.WarmupRepository

	live  *models.WarmupToken
	err   error
	reads int
}

func (s *stubWarmupTokenRepo) GetWarmupToken(context.Context, uuid.UUID) (*models.WarmupToken, error) {
	s.reads++
	return s.live, s.err
}

// FindWarmupToken must never be reached: telling a foreign token from a
// vanished one only mattered when the difference decided a charge.
func (s *stubWarmupTokenRepo) FindWarmupToken(context.Context, uuid.UUID) (*models.WarmupToken, error) {
	panic("FindWarmupToken: the inbound path has no reason to read a token twice")
}

// stubWarmupService observes the one thing that matters: was the mailbox
// charged. markRiskBandFromWarmupHealth is a no-op without a WorkerRepo.
type stubWarmupService struct {
	warmupapp.Service

	charged []int
}

func (s *stubWarmupService) ApplyInvalidTokenAttempt(_ context.Context, _ uuid.UUID, _ string, scoreDelta int) (*models.WarmupParticipantHealth, *errx.Error) {
	s.charged = append(s.charged, scoreDelta)
	return nil, nil
}

// Every invalid-token attempt on a live self-host was the mailbox re-reading
// its own mail: 88 of 211 a message whose token had expired, 67 the Sent copy
// carrying the recipient's token, and 56 a reconnected mailbox replaying a
// history whose tokens had cascaded away. None was tampering, and three in a
// day blocked a mailbox from the pool for a month.
//
// The rule that survives that data is that no inbound token is evidence
// against the mailbox that received it. The one shape that looked like
// evidence, a token naming another pair, is the one an attacker can put in
// any inbox at will, and the recipient check already makes it worthless.
// Acceptance of a live token for this mailbox is exercised against the real
// store in warmup_verification_live_test.go; this covers every other path.
func TestHandleWarmupEmailNeverChargesTheRecipient(t *testing.T) {
	mailbox := uuid.New()
	partner := uuid.New()
	token := uuid.New()
	e := &models.JobEventNewEmail{Message: &models.EmailMessageStoreData{EmailID: mailbox, Folder: models.FolderInbox}}

	foreign := &models.WarmupToken{Token: token, RecipientAccountID: partner, SenderAccountID: uuid.New()}
	sent := &models.WarmupToken{Token: token, RecipientAccountID: partner, SenderAccountID: mailbox}

	tests := []struct {
		name    string
		tokStr  string
		repo    *stubWarmupTokenRepo
		wantErr bool
		reads   int
	}{
		{"a live token naming another pair: an attacker's to place, not evidence", token.String(), &stubWarmupTokenRepo{live: foreign}, false, 1},
		{"the Sent copy carrying the token this mailbox sent", token.String(), &stubWarmupTokenRepo{live: sent}, false, 1},
		{"a token that is consumed, expired or gone", token.String(), &stubWarmupTokenRepo{}, false, 1},
		{"a marker that does not parse", "not-a-uuid", &stubWarmupTokenRepo{}, false, 0},
		{"a failed lookup is surfaced, not charged", token.String(), &stubWarmupTokenRepo{err: errors.New("connection reset by peer")}, true, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &stubWarmupService{}
			s := &JobsService{WarmupRepo: tc.repo, WarmupService: svc}
			handled, err := s.handleWarmupEmail(context.Background(), e, tc.tokStr)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if handled {
				t.Fatal("filed a token that was not this mailbox's as warmup")
			}
			if len(svc.charged) != 0 {
				t.Fatalf("the recipient was charged %v for mail it merely received", svc.charged)
			}
			if tc.repo.reads != tc.reads {
				t.Fatalf("GetWarmupToken called %d times, want %d", tc.repo.reads, tc.reads)
			}
		})
	}
}

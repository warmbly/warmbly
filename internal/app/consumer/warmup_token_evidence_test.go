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

func (s *stubWarmupTokenRepo) GetWarmupToken(context.Context, uuid.UUID) (*models.WarmupToken, error) {
	s.reads++
	return s.live, s.err
}

// FindWarmupToken must never be reached: telling a foreign token from a
// vanished one only mattered when the difference decided a charge.
func (s *stubWarmupTokenRepo) FindWarmupToken(context.Context, uuid.UUID) (*models.WarmupToken, error) {
	panic("FindWarmupToken: the inbound path has no reason to read a token twice")
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
// Since #482 there is nothing left to charge with: the signal, its band and
// its table are gone. What remains to pin is that no other shape is filed as
// warmup and that the path reads the token exactly once. Acceptance of a
// live token for this mailbox is exercised against the real store in
// warmup_verification_live_test.go.
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
			s := &JobsService{WarmupRepo: tc.repo}
			handled, err := s.handleWarmupEmail(context.Background(), e, tc.tokStr)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if handled {
				t.Fatal("filed a token that was not this mailbox's as warmup")
			}
			if tc.repo.reads != tc.reads {
				t.Fatalf("GetWarmupToken called %d times, want %d", tc.repo.reads, tc.reads)
			}
		})
	}
}

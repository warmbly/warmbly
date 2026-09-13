package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Every invalid-token attempt on a live self-host was the mailbox re-reading
// its own mail: 88 of 211 a message whose token had expired, 67 the Sent copy
// carrying the recipient's token, and 56 a reconnected mailbox replaying a
// history whose tokens had cascaded away with its previous row. None was
// tampering, and three in a day blocks a mailbox from the pool for a month.
//
// The rule that survives that data: a token is evidence only when it resolves
// and names neither side of the mailbox. It is deliberately not a window, not a
// folder check and not a clock, because each of those was a way to be wrong.
func TestWarmupTokenIsForeign(t *testing.T) {
	mailbox := uuid.New()
	partner := uuid.New()

	received := &models.WarmupToken{RecipientAccountID: mailbox, SenderAccountID: partner}
	sent := &models.WarmupToken{RecipientAccountID: partner, SenderAccountID: mailbox}
	foreign := &models.WarmupToken{RecipientAccountID: partner, SenderAccountID: uuid.New()}

	tests := []struct {
		name string
		tok  *models.WarmupToken
		want bool
	}{
		{"a token naming neither side is evidence", foreign, true},
		{"the expired token of mail this mailbox received is not", received, false},
		{"the Sent copy, carrying the token this mailbox sent, is not", sent, false},
		{"a token that resolves to nothing is not: it cannot be redeemed", nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := warmupTokenIsForeign(mailbox, tc.tok); got != tc.want {
				t.Errorf("warmupTokenIsForeign = %v, want %v", got, tc.want)
			}
		})
	}
}

type stubWarmupTokenRepo struct {
	repository.WarmupRepository

	live  *models.WarmupToken
	found *models.WarmupToken
	err   error
	finds int
}

func (s *stubWarmupTokenRepo) GetWarmupToken(context.Context, uuid.UUID) (*models.WarmupToken, error) {
	return s.live, s.err
}

// FindWarmupToken mirrors scanWarmupToken: a missing row is (nil, nil), so a
// non-nil error is only ever a failed question.
func (s *stubWarmupTokenRepo) FindWarmupToken(context.Context, uuid.UUID) (*models.WarmupToken, error) {
	s.finds++
	return s.found, s.err
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

// Acceptance of a live token for this mailbox is exercised against the real
// store in warmup_verification_live_test.go; this covers every path that ends
// in a charge decision.
func TestHandleWarmupEmailChargesOnlyForeignTokens(t *testing.T) {
	mailbox := uuid.New()
	partner := uuid.New()
	token := uuid.New()
	e := &models.JobEventNewEmail{Message: &models.EmailMessageStoreData{EmailID: mailbox, Folder: models.FolderInbox}}

	foreign := &models.WarmupToken{Token: token, RecipientAccountID: partner, SenderAccountID: uuid.New()}
	received := &models.WarmupToken{Token: token, RecipientAccountID: mailbox, SenderAccountID: partner}
	sent := &models.WarmupToken{Token: token, RecipientAccountID: partner, SenderAccountID: mailbox}

	tests := []struct {
		name    string
		tokStr  string
		repo    *stubWarmupTokenRepo
		want    []int
		handled bool
		wantErr bool
		finds   int
	}{
		{
			name:   "a live token naming another pair is charged without a second read",
			tokStr: token.String(),
			repo:   &stubWarmupTokenRepo{live: foreign},
			want:   []int{config.WarmupForeignTokenScore},
		},
		{
			name:   "a lapsed token naming another pair is still charged",
			tokStr: token.String(),
			repo:   &stubWarmupTokenRepo{found: foreign},
			want:   []int{config.WarmupForeignTokenScore},
			finds:  1,
		},
		{
			name:   "the Sent copy is the mailbox's own mail",
			tokStr: token.String(),
			repo:   &stubWarmupTokenRepo{live: sent},
		},
		{
			name:   "an expired token of mail this mailbox received is a re-read",
			tokStr: token.String(),
			repo:   &stubWarmupTokenRepo{found: received},
			finds:  1,
		},
		{
			name:   "a token that resolves to nothing is not charged, whatever the mailbox's age",
			tokStr: token.String(),
			repo:   &stubWarmupTokenRepo{},
			finds:  1,
		},
		{
			name:   "a marker that does not parse is not charged",
			tokStr: "not-a-uuid",
			repo:   &stubWarmupTokenRepo{},
		},
		{
			name:    "a failed lookup is a failed question, surfaced and not charged",
			tokStr:  token.String(),
			repo:    &stubWarmupTokenRepo{err: errors.New("connection reset by peer")},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &stubWarmupService{}
			s := &JobsService{WarmupRepo: tc.repo, WarmupService: svc}
			handled, err := s.handleWarmupEmail(context.Background(), e, tc.tokStr)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if handled != tc.handled {
				t.Fatalf("handled = %v, want %v", handled, tc.handled)
			}
			if len(svc.charged) != len(tc.want) {
				t.Fatalf("charged %v, want %v", svc.charged, tc.want)
			}
			for i := range tc.want {
				if svc.charged[i] != tc.want[i] {
					t.Fatalf("charged %v, want %v", svc.charged, tc.want)
				}
			}
			if tc.repo.finds != tc.finds {
				t.Fatalf("FindWarmupToken called %d times, want %d", tc.repo.finds, tc.finds)
			}
		})
	}
}

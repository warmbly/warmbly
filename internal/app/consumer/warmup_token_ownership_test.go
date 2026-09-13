package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

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
func TestWarmupTokenIsOwnMail(t *testing.T) {
	mailbox := uuid.New()
	partner := uuid.New()
	grace := time.Duration(config.WarmupReconnectGraceMinutes) * time.Minute

	own := &models.WarmupToken{RecipientAccountID: mailbox, SenderAccountID: partner}
	sentByMe := &models.WarmupToken{RecipientAccountID: partner, SenderAccountID: mailbox}
	strangers := &models.WarmupToken{RecipientAccountID: partner, SenderAccountID: uuid.New()}

	old := grace * 4

	tests := []struct {
		name   string
		folder string
		tok    *models.WarmupToken
		age    time.Duration
		want   bool
	}{
		{"sent copy carries the recipient's token", models.FolderSent, sentByMe, old, true},
		{"sent copy whose token cascaded away is still ours", models.FolderSent, nil, old, true},
		{"a stranger's token appended to sent is signal", models.FolderSent, strangers, old, false},
		{"expired token of mail this mailbox received", models.FolderInbox, own, old, true},
		{"consumed token of mail this mailbox sent", models.FolderInbox, sentByMe, old, true},
		{"first sync after a reconnect, token cascaded away", models.FolderInbox, nil, grace / 2, true},
		{"a token that belongs to neither side is signal", models.FolderInbox, strangers, old, false},
		{"an unknown token on an established mailbox is signal", models.FolderInbox, nil, old, false},
		{"an unknown token with no mailbox age is signal", models.FolderInbox, nil, -1, false},
		// The reconnect window forgives a token that resolves to nothing. A
		// token that resolves to another pair is the only shape that was ever
		// evidence, so the window must not reach it: otherwise a mailbox that
		// keeps reconnecting is never charged for probing.
		{"a stranger's token on a young mailbox is still signal", models.FolderInbox, strangers, grace / 2, false},
		{"our own expired token on a young mailbox is not", models.FolderInbox, own, grace / 2, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := warmupTokenIsOwnMail(tc.folder, mailbox, tc.tok, tc.age); got != tc.want {
				t.Errorf("warmupTokenIsOwnMail = %v, want %v", got, tc.want)
			}
		})
	}
}

type stubWarmupTokenRepo struct {
	repository.WarmupRepository

	tok   *models.WarmupToken
	err   error
	calls int
}

// FindWarmupToken mirrors scanWarmupToken: a missing row is (nil, nil), so a
// non-nil error is only ever a failed question.
func (s *stubWarmupTokenRepo) FindWarmupToken(context.Context, uuid.UUID) (*models.WarmupToken, error) {
	s.calls++
	return s.tok, s.err
}

type stubAccountAgeRepo struct {
	repository.EmailRepository

	createdAt time.Time
}

func (s *stubAccountAgeRepo) GetByID(context.Context, uuid.UUID) (*models.Email, *errx.Error) {
	return &models.Email{CreatedAt: s.createdAt}, nil
}

// The token lookup is the whole difference between a re-read and a forgery.
// Charging a mailbox when the lookup itself fails re-creates the false positive
// this file exists to stop, at 5 points a message and a thirty-day pool block
// on the third one, and a DB blip arrives during exactly the re-read burst that
// makes those three.
func TestResolveWarmupTokenOwnership(t *testing.T) {
	mailbox := uuid.New()
	e := &models.JobEventNewEmail{Message: &models.EmailMessageStoreData{
		EmailID: mailbox,
		Folder:  models.FolderInbox,
	}}
	established := &stubAccountAgeRepo{createdAt: time.Now().Add(-30 * 24 * time.Hour)}

	t.Run("a failed lookup is not evidence", func(t *testing.T) {
		repo := &stubWarmupTokenRepo{err: errors.New("connection reset by peer")}
		s := &JobsService{WarmupRepo: repo, EmailRepository: established}
		if !s.resolveWarmupTokenOwnership(context.Background(), e, uuid.New(), nil) {
			t.Fatal("charged a mailbox for a token lookup that never answered")
		}
	})

	t.Run("a missing row on an established mailbox is still signal", func(t *testing.T) {
		repo := &stubWarmupTokenRepo{}
		s := &JobsService{WarmupRepo: repo, EmailRepository: established}
		if s.resolveWarmupTokenOwnership(context.Background(), e, uuid.New(), nil) {
			t.Fatal("an unknown token on an established mailbox went unrecorded")
		}
	})

	t.Run("a token the caller already holds is not re-read", func(t *testing.T) {
		repo := &stubWarmupTokenRepo{err: errors.New("must not be called")}
		s := &JobsService{WarmupRepo: repo, EmailRepository: established}
		strangers := &models.WarmupToken{RecipientAccountID: uuid.New(), SenderAccountID: uuid.New()}
		if s.resolveWarmupTokenOwnership(context.Background(), e, uuid.New(), strangers) {
			t.Fatal("forgave a token that names neither side of this mailbox")
		}
		if repo.calls != 0 {
			t.Fatalf("re-read a token the caller already held: %d lookups", repo.calls)
		}
	})
}

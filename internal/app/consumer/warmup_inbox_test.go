package jobs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type warmupInboxRepo struct {
	repository.UniboxRepository
	entries int
}

func (r *warmupInboxRepo) CreateEntry(context.Context, uuid.UUID, *models.EmailMessageStoreData) error {
	r.entries++
	return nil
}

type warmupInboxEmailRepo struct{ repository.EmailRepository }

func (warmupInboxEmailRepo) GetByID(context.Context, uuid.UUID) (*models.Email, *errx.Error) {
	return nil, nil
}

func TestHandleNewEmailKeepsLocalWarmupSentCopyOutOfInbox(t *testing.T) {
	account, token := uuid.New(), uuid.New()
	inbox := &warmupInboxRepo{}
	s := &JobsService{
		WarmupRepo: &stubWarmupTokenRepo{live: &models.WarmupToken{
			Token: token, SenderAccountID: account, RecipientAccountID: uuid.New(),
		}},
		UniboxRepository: inbox,
		EmailRepository:  warmupInboxEmailRepo{},
	}
	err := s.HandleNewEmail(context.Background(), &models.JobEventNewEmail{
		UserID: uuid.New(),
		Message: &models.EmailMessageStoreData{
			EmailID: account, Folder: models.FolderSent,
			Flags: []string{config.WarmupVerifyHeader + ":" + token.String()},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if inbox.entries != 0 {
		t.Fatal("warmup Sent copy was exposed as an ordinary Unibox conversation")
	}
}

type warmupInboxCloud struct {
	tokenOK, deliveryOK       bool
	tokenErr, deliveryErr     error
	enrollmentErr             error
	tokenCalls, deliveryCalls int
}

func (c *warmupInboxCloud) CheckEnrollment(context.Context, uuid.UUID) (bool, error) {
	return true, c.enrollmentErr
}

func (c *warmupInboxCloud) VerifyWarmupToken(ctx context.Context, _ uuid.UUID, _ string) (bool, error) {
	if _, ok := ctx.Deadline(); !ok {
		panic("cloud verification needs a deadline")
	}
	c.tokenCalls++
	return c.tokenOK, c.tokenErr
}

func (c *warmupInboxCloud) IsCloudWarmupDelivery(ctx context.Context, _ uuid.UUID, _, _, _ string) (bool, error) {
	if _, ok := ctx.Deadline(); !ok {
		panic("cloud verification needs a deadline")
	}
	c.deliveryCalls++
	return c.deliveryOK, c.deliveryErr
}

func TestHandleNewEmailCloudWarmupVisibility(t *testing.T) {
	token := uuid.NewString()
	for _, tc := range []struct {
		name, marker                string
		cloud                       warmupInboxCloud
		wantEntries, wantTokenCalls int
		wantErr                     bool
	}{
		{name: "verified sender or recipient", marker: config.WarmupVerifyHeader + ":" + token, cloud: warmupInboxCloud{tokenOK: true}, wantTokenCalls: 1},
		{name: "legacy header", marker: "X-Warmbly-Token:" + token, cloud: warmupInboxCloud{tokenOK: true}, wantTokenCalls: 1},
		{name: "header case and whitespace", marker: strings.ToLower(config.WarmupVerifyHeader) + ": " + token + " ", cloud: warmupInboxCloud{tokenOK: true}, wantTokenCalls: 1},
		{name: "stripped header", cloud: warmupInboxCloud{deliveryOK: true}},
		{name: "unknown header falls back to delivery", marker: config.WarmupVerifyHeader + ":" + token, cloud: warmupInboxCloud{deliveryOK: true}, wantTokenCalls: 1},
		{name: "ordinary message", wantEntries: 1},
		{name: "foreign token", marker: config.WarmupVerifyHeader + ":" + token, wantEntries: 1, wantTokenCalls: 1},
		{name: "malformed marker", marker: config.WarmupVerifyHeader + ":not-a-token", wantEntries: 1},
		{name: "token outage retries without exposure", marker: config.WarmupVerifyHeader + ":" + token, cloud: warmupInboxCloud{tokenErr: errors.New("cloud unavailable")}, wantTokenCalls: 1, wantErr: true},
		{name: "delivery outage retries without exposure", cloud: warmupInboxCloud{deliveryErr: errors.New("cloud unavailable")}, wantErr: true},
		{name: "enrollment lookup failure retries without exposure", cloud: warmupInboxCloud{enrollmentErr: errors.New("database unavailable")}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inbox := &warmupInboxRepo{}
			s := &JobsService{CloudLink: &tc.cloud, UniboxRepository: inbox, EmailRepository: warmupInboxEmailRepo{}}
			e := &models.JobEventNewEmail{UserID: uuid.New(), Message: &models.EmailMessageStoreData{
				ID: uuid.New(), EmailID: uuid.New(), Folder: models.FolderSent,
				Flags: []string{tc.marker}, FromAddr: []string{"sender@example.com"}, MessageID: "<mail@example.com>", Subject: "Quick review",
			}}
			err := s.ingestNewEmail(context.Background(), e)
			if (err != nil) != tc.wantErr || inbox.entries != tc.wantEntries || tc.cloud.tokenCalls != tc.wantTokenCalls {
				t.Fatalf("error = %v, inbox entries = %d, token calls = %d", err, inbox.entries, tc.cloud.tokenCalls)
			}
			if tc.wantErr {
				tc.cloud.enrollmentErr = nil
				tc.cloud.tokenErr, tc.cloud.deliveryErr = nil, nil
				tc.cloud.tokenOK, tc.cloud.deliveryOK = true, true
				if err := s.ingestNewEmail(context.Background(), e); err != nil || inbox.entries != 0 {
					t.Fatalf("retry exposed warmup: error = %v, entries = %d", err, inbox.entries)
				}
			}
		})
	}
}

func TestHandleNewEmailConsumedAndExpiredWarmupStayHidden(t *testing.T) {
	for _, senderCopy := range []bool{false, true} {
		for _, consumed := range []bool{false, true} {
			account, token := uuid.New(), uuid.New()
			known := &models.WarmupToken{Token: token, SenderAccountID: uuid.New(), RecipientAccountID: account, ExpiresAt: time.Now().Add(-time.Hour)}
			if senderCopy {
				known.SenderAccountID, known.RecipientAccountID = account, uuid.New()
			}
			if consumed {
				now := time.Now()
				known.ConsumedAt = &now
				known.ExpiresAt = now.Add(time.Hour)
			}
			inbox := &warmupInboxRepo{}
			s := &JobsService{WarmupRepo: &stubWarmupTokenRepo{live: known}, UniboxRepository: inbox, EmailRepository: warmupInboxEmailRepo{}}
			err := s.HandleNewEmail(context.Background(), &models.JobEventNewEmail{Message: &models.EmailMessageStoreData{
				EmailID: account, Flags: []string{config.WarmupVerifyHeader + ":" + token.String()},
			}})
			if err != nil || inbox.entries != 0 {
				t.Fatalf("sender=%v consumed=%v: error=%v entries=%d", senderCopy, consumed, err, inbox.entries)
			}
		}
	}
}

type warmupCleanupInbox struct {
	repository.UniboxRepository
	events  []models.JobEventNewEmail
	deleted []uuid.UUID
}

func (r *warmupCleanupInbox) ListWarmupReviewCandidates(_ context.Context, afterID uuid.UUID, limit int) ([]models.JobEventNewEmail, error) {
	var out []models.JobEventNewEmail
	for _, e := range r.events {
		if e.Message.ID.String() > afterID.String() {
			out = append(out, e)
		}
	}
	return out[:min(len(out), limit)], nil
}

func (r *warmupCleanupInbox) Delete(_ context.Context, owner, id uuid.UUID) error {
	for _, e := range r.events {
		if e.Message.ID == id && e.UserID != owner {
			panic("cleanup crossed mailbox owners")
		}
	}
	r.deleted = append(r.deleted, id)
	return nil
}

func TestWarmupInboxCleanupRetriesCloudFailureWithoutDeletingMail(t *testing.T) {
	e := models.JobEventNewEmail{UserID: uuid.New(), Message: &models.EmailMessageStoreData{ID: uuid.New(), EmailID: uuid.New(), MessageID: "<cloud@test.local>"}}
	inbox := &warmupCleanupInbox{events: []models.JobEventNewEmail{e}}
	cloud := &warmupInboxCloud{deliveryErr: errors.New("unavailable")}
	s := &JobsService{UniboxRepository: inbox, CloudLink: cloud}
	next, done, err := s.cleanWarmupInboxBatch(context.Background(), uuid.Nil)
	if err == nil || done || next != uuid.Nil || len(inbox.deleted) != 0 {
		t.Fatalf("failed verification advanced cleanup or deleted mail: %v %v %v", next, done, err)
	}
	cloud.deliveryErr, cloud.deliveryOK = nil, true
	next, done, err = s.cleanWarmupInboxBatch(context.Background(), next)
	if err != nil || !done || next != e.Message.ID || len(inbox.deleted) != 1 || inbox.deleted[0] != e.Message.ID {
		t.Fatalf("cleanup retry failed: %v %v %v", next, done, err)
	}
}

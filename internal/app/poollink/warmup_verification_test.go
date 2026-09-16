package poollink

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type verificationMailboxRepo struct {
	repository.PoolLinkRepository
	mailbox *models.PoolLinkMailbox
}

func (r verificationMailboxRepo) GetMailboxByRemote(context.Context, uuid.UUID, uuid.UUID) (*models.PoolLinkMailbox, error) {
	return r.mailbox, nil
}

type verificationTokenRepo struct {
	repository.WarmupRepository
	token       *models.WarmupToken
	deliveryErr error
}

func (r verificationTokenRepo) IsWarmupDelivery(context.Context, uuid.UUID, string, string, string) (bool, error) {
	return false, r.deliveryErr
}

func (r verificationTokenRepo) FindWarmupToken(context.Context, uuid.UUID) (*models.WarmupToken, error) {
	return r.token, nil
}

func TestVerifyWarmupDeliveryDefersUnconfirmedSenderCopy(t *testing.T) {
	s := &service{
		repo:   verificationMailboxRepo{mailbox: &models.PoolLinkMailbox{EmailAccountID: uuid.New()}},
		warmup: verificationTokenRepo{deliveryErr: repository.ErrWarmupDeliveryPending},
	}
	known, err := s.VerifyWarmupDelivery(context.Background(), &models.PoolLinkInstance{ID: uuid.New()}, uuid.New(), models.PoolLinkWarmupDeliveryQuery{Sender: "sender@test.local", MessageID: "<restamped@test.local>", Subject: "Review"})
	if known || err != errx.ErrServiceDown {
		t.Fatalf("unconfirmed delivery must ask the instance to retry: known=%v error=%v", known, err)
	}
}

func TestVerifyWarmupTokenRecognizesBothMailboxCopies(t *testing.T) {
	sender, recipient := uuid.New(), uuid.New()
	token := &models.WarmupToken{Token: uuid.New(), SenderAccountID: sender, RecipientAccountID: recipient}
	for _, tc := range []struct {
		name    string
		account uuid.UUID
		want    bool
	}{
		{"recipient", recipient, true},
		{"sender Sent copy", sender, true},
		{"unrelated mailbox", uuid.New(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &service{
				repo:   verificationMailboxRepo{mailbox: &models.PoolLinkMailbox{EmailAccountID: tc.account}},
				warmup: verificationTokenRepo{token: token},
			}
			got, err := s.VerifyWarmupToken(context.Background(), &models.PoolLinkInstance{ID: uuid.New()}, uuid.New(), token.Token)
			if err != nil || got != tc.want {
				t.Fatalf("warmup = %v, error = %v; want %v", got, err, tc.want)
			}
		})
	}
}

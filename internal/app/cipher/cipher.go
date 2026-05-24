package cipher

import (
	"context"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/repository"
)

type Cipher struct {
	plainDEK []byte
}

func (s *cipherService) Cipher(ctx context.Context, userID uuid.UUID) (*Cipher, error) {
	key, err := s.getDecryptedKey(ctx, userID)
	if err != nil {
		return nil, err
	}

	encDEKB64, err := s.userEncryptedKeysRepository.Get(ctx, userID)
	if err != nil {
		return nil, err
	}

	if encDEKB64 == "" {
		var encryptedDEK string
		key, encryptedDEK, err = s.kms.GenerateDataKey(ctx)
		if err != nil {
			return nil, err
		}

		if err := s.userEncryptedKeysRepository.Put(ctx, repository.UserEncryptedKeysItem{
			UserID:           userID.String(),
			EncryptedDataKey: encryptedDEK,
		}); err != nil {
			return nil, err
		}
	} else {
		key, err = s.kms.GetDecryptedKey(ctx, encDEKB64)
		if err != nil {
			return nil, err
		}
	}

	if err := s.saveDecryptedKey(ctx, userID, key); err != nil {
		sentry.CaptureException(err)
	}

	return &Cipher{
		plainDEK: key,
	}, nil
}

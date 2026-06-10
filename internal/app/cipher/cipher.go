package cipher

import (
	"context"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
)

type Cipher struct {
	plainDEK []byte
}

func (s *cipherService) Cipher(ctx context.Context, orgID uuid.UUID) (*Cipher, error) {
	key, err := s.getDecryptedKey(ctx, orgID)
	if err != nil {
		return nil, err
	}

	encDEKB64, err := s.encryptedKeys.Get(ctx, orgID)
	if err != nil {
		return nil, err
	}

	if encDEKB64 == "" {
		var encryptedDEK string
		key, encryptedDEK, err = s.kms.GenerateDataKey(ctx)
		if err != nil {
			return nil, err
		}

		if err := s.encryptedKeys.Put(ctx, orgID, encryptedDEK); err != nil {
			return nil, err
		}
	} else {
		key, err = s.kms.GetDecryptedKey(ctx, encDEKB64)
		if err != nil {
			return nil, err
		}
	}

	if err := s.saveDecryptedKey(ctx, orgID, key); err != nil {
		sentry.CaptureException(err)
	}

	return &Cipher{
		plainDEK: key,
	}, nil
}

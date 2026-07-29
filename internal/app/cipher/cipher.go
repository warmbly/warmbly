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
	// Cache hit: reuse the decrypted DEK. Any miss or cache error (redis.Nil
	// on first use of an org's key) falls through to the KMS path — a cache
	// problem must never block crypto.
	if key, err := s.getDecryptedKey(ctx, orgID); err == nil && len(key) > 0 {
		return &Cipher{plainDEK: key}, nil
	}

	var key []byte

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

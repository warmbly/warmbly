package cipher

import (
	"context"

	"github.com/google/uuid"
)

func getDecryptedKeyKey(userID uuid.UUID) string {
	return "decrypted_key:" + userID.String()
}

func (s *cipherService) getDecryptedKey(ctx context.Context, userID uuid.UUID) ([]byte, error) {
	key := getDecryptedKeyKey(userID)

	deckey, err := s.cache.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}

	return deckey, nil
}

func (s *cipherService) saveDecryptedKey(ctx context.Context, userID uuid.UUID, decryptedKey []byte) error {
	key := getDecryptedKeyKey(userID)

	if err := s.cache.SetNX(ctx, key, decryptedKey, DecryptedKeyTTL).Err(); err != nil {
		return err
	}

	return nil
}

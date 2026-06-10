package cipher

import (
	"context"

	"github.com/google/uuid"
)

func getDecryptedKeyKey(orgID uuid.UUID) string {
	return "decrypted_key:" + orgID.String()
}

func (s *cipherService) getDecryptedKey(ctx context.Context, orgID uuid.UUID) ([]byte, error) {
	key := getDecryptedKeyKey(orgID)

	deckey, err := s.cache.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}

	return deckey, nil
}

func (s *cipherService) saveDecryptedKey(ctx context.Context, orgID uuid.UUID, decryptedKey []byte) error {
	key := getDecryptedKeyKey(orgID)

	if err := s.cache.SetNX(ctx, key, decryptedKey, DecryptedKeyTTL).Err(); err != nil {
		return err
	}

	return nil
}

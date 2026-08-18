package cipher

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// errNoCache means this process was built without Redis. The DEK cache is an
// optimisation, never a requirement, so a cacheless caller (warmblyctl, which
// exists to work while things are down) falls through to KMS on every call.
var errNoCache = errors.New("cipher: no cache configured")

func getDecryptedKeyKey(orgID uuid.UUID) string {
	return "decrypted_key:" + orgID.String()
}

func (s *cipherService) getDecryptedKey(ctx context.Context, orgID uuid.UUID) ([]byte, error) {
	if s.cache == nil {
		return nil, errNoCache
	}
	key := getDecryptedKeyKey(orgID)

	deckey, err := s.cache.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}

	return deckey, nil
}

func (s *cipherService) saveDecryptedKey(ctx context.Context, orgID uuid.UUID, decryptedKey []byte) error {
	if s.cache == nil {
		return nil
	}
	key := getDecryptedKeyKey(orgID)

	if err := s.cache.SetNX(ctx, key, decryptedKey, DecryptedKeyTTL).Err(); err != nil {
		return err
	}

	return nil
}

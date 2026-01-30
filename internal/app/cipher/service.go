package cipher

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/kms"
	"github.com/warmbly/warmbly/internal/repository"
)

type CipherService interface {
	Cipher(ctx context.Context, userID uuid.UUID) (*Cipher, error)
}

type cipherService struct {
	userEncryptedKeysRepository repository.UserEncryptedKeysRepository
	cache                       *cache.Cache
	kms                         *kms.KMS
}

func NewService(kms *kms.KMS, cache *cache.Cache, userEncryptedKeysRepository repository.UserEncryptedKeysRepository) CipherService {
	return &cipherService{
		kms:                         kms,
		cache:                       cache,
		userEncryptedKeysRepository: userEncryptedKeysRepository,
	}
}

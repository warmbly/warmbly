package email

import (
	"context"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/feature"
	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type EmailService interface {
	Search(ctx context.Context, userID, search, cursor, tag, limit string) (*models.EmailsResult, *errx.Error)
	Get(ctx context.Context, userID, emailAccountID string) (*models.Email, *errx.Error)
	Update(ctx context.Context, userID, emailAccountID string, udata *models.UpdateEmail) (*models.Email, *errx.Error)
	UpdateTrackingDomain(ctx context.Context, userID, emailAccountID, domain string) *errx.Error
	Delete(ctx context.Context, userID, emailAccountID string) *errx.Error
}

type emailService struct {
	emailRepository repository.EmailRepository
	cipherService   cipher.CipherService
	featureGate     feature.FeatureGateService
	warmupService   warmupapp.Service
	publisher       events.Publisher
	producer        *kafka.Producer
	r               *cache.Cache
}

func NewService(
	emailRepository repository.EmailRepository,
	cipherService cipher.CipherService,
	featureGate feature.FeatureGateService,
	warmupService warmupapp.Service,
	publisher events.Publisher,
) EmailService {
	return &emailService{
		emailRepository: emailRepository,
		cipherService:   cipherService,
		featureGate:     featureGate,
		warmupService:   warmupService,
		publisher:       publisher,
	}
}

func NewServiceWithKafka(
	emailRepository repository.EmailRepository,
	cipherService cipher.CipherService,
	featureGate feature.FeatureGateService,
	warmupService warmupapp.Service,
	publisher events.Publisher,
	producer *kafka.Producer,
	r *cache.Cache,
) EmailService {
	return &emailService{
		emailRepository: emailRepository,
		cipherService:   cipherService,
		featureGate:     featureGate,
		warmupService:   warmupService,
		publisher:       publisher,
		producer:        producer,
		r:               r,
	}
}

package contact

import (
	"context"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type ContactService interface {
	Add(ctx context.Context, userID string, contacts []models.AddContact) ([]models.Contact, *errx.Error)
	Search(ctx context.Context, userID, cursor, category, limit string, filters models.SearchContacts) (*models.ContactsResult, *errx.Error)
	BulkUpdate(ctx context.Context, userID string, data *models.BulkEditContactsData) ([]models.Contact, *errx.Error)
	Update(ctx context.Context, userID, contactID string, data *models.UpdateContact) (*models.Contact, *errx.Error)
	BulkDelete(ctx context.Context, userID string, contactIDs []string) *errx.Error
	Delete(ctx context.Context, userID string, contactID string) *errx.Error
}

type contactService struct {
	contactRepository repository.ContactRepository
	subRepo           repository.SubscriptionRepository
	planRepo          repository.PlanRepository
}

func NewService(
	contactRepository repository.ContactRepository,
	subRepo repository.SubscriptionRepository,
	planRepo repository.PlanRepository,
) ContactService {
	return &contactService{
		contactRepository: contactRepository,
		subRepo:           subRepo,
		planRepo:          planRepo,
	}
}

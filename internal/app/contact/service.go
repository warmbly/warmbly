package contact

import (
	"context"
	"io"

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

	// Export streams every contact matching the request into the given
	// writer. The format / filename / content-type are returned so the
	// handler can set headers correctly.
	Export(ctx context.Context, userID string, req *models.ContactExportRequest, w io.Writer) (filename, contentType string, count int, err *errx.Error)

	// ImportPreview parses an uploaded CSV/XLSX file and reports back
	// the columns + first N rows + suggested mapping — no DB writes.
	ImportPreview(ctx context.Context, file io.Reader, filename string) (*models.ContactImportPreview, *errx.Error)

	// ImportCommit re-parses the uploaded file with the chosen mapping
	// and performs the upsert / skip / dedup work. Returns per-row
	// result counts plus a list of rows that failed (with reasons).
	ImportCommit(ctx context.Context, userID string, file io.Reader, filename string, opts *models.ContactImportCommit) (*models.ContactImportResult, *errx.Error)
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

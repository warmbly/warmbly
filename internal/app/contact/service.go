package contact

import (
	"context"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
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

	// GetDetail returns the 360 read model used by the contact
	// slide-over: hydrated contact + engagement summary + suppression.
	GetDetail(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, contactID uuid.UUID) (*models.ContactDetail, *errx.Error)

	// GetByEmail resolves a sender address to a contact within the
	// organization (newest match wins). Returns (nil, nil) when the org has
	// no contact for that address — a "not a known contact" is a normal,
	// non-error outcome used by the unibox CRM panel.
	GetByEmail(ctx context.Context, orgID *uuid.UUID, email string) (*models.Contact, *errx.Error)

	// ListSentEmails enumerates every send (or attempted send) we made
	// to the contact, newest first.
	ListSentEmails(ctx context.Context, userID, contactID uuid.UUID, limit int, beforeSentAt *time.Time, beforeTaskID *uuid.UUID) (*models.ContactSentEmailsResult, *errx.Error)

	// ListTimeline returns a merged, reverse-chronological feed of all
	// engagement + CRM events for the contact.
	ListTimeline(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, contactID uuid.UUID, limit int, before *time.Time) (*models.ContactTimelineResult, *errx.Error)
}

type contactService struct {
	contactRepository  repository.ContactRepository
	subRepo            repository.SubscriptionRepository
	planRepo           repository.PlanRepository
	streamingPublisher *pubsub.StreamingPublisher
}

func NewService(
	contactRepository repository.ContactRepository,
	subRepo repository.SubscriptionRepository,
	planRepo repository.PlanRepository,
	streamingPublisher ...*pubsub.StreamingPublisher,
) ContactService {
	var publisher *pubsub.StreamingPublisher
	if len(streamingPublisher) > 0 {
		publisher = streamingPublisher[0]
	}

	return &contactService{
		contactRepository:  contactRepository,
		subRepo:            subRepo,
		planRepo:           planRepo,
		streamingPublisher: publisher,
	}
}

func (s *contactService) publishContactsReload(ctx context.Context, userID string, operationID string) {
	if s.streamingPublisher == nil {
		return
	}
	s.streamingPublisher.PublishContactsReload(ctx, userID, operationID)
}

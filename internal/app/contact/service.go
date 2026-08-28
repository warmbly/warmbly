package contact

import (
	"context"
	"github.com/warmbly/warmbly/internal/app/orgrisk"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type ContactService interface {
	Add(ctx context.Context, userID string, orgID uuid.UUID, contacts []models.AddContact) ([]models.Contact, *errx.Error)
	Search(ctx context.Context, userID, cursor, category, limit string, filters models.SearchContacts) (*models.ContactsResult, *errx.Error)
	// SearchCounts returns org-wide contact facet totals for the browse sidebar.
	SearchCounts(ctx context.Context, orgID string) (*models.ContactsCounts, *errx.Error)
	// CampaignLeadCounts returns per-status lead totals for one campaign.
	CampaignLeadCounts(ctx context.Context, orgID, campaignID string) (*models.CampaignLeadCounts, *errx.Error)
	BulkUpdate(ctx context.Context, userID string, orgID uuid.UUID, data *models.BulkEditContactsData) ([]models.Contact, *errx.Error)
	Update(ctx context.Context, userID, contactID string, orgID uuid.UUID, data *models.UpdateContact) (*models.Contact, *errx.Error)
	BulkDelete(ctx context.Context, userID string, orgID uuid.UUID, contactIDs []string) *errx.Error
	Delete(ctx context.Context, userID string, orgID uuid.UUID, contactID string) *errx.Error

	// Export streams every contact matching the request into the given
	// writer. The format / filename / content-type are returned so the
	// handler can set headers correctly.
	Export(ctx context.Context, userID string, req *models.ContactExportRequest, w io.Writer) (filename, contentType string, count int, err *errx.Error)

	// ImportPreview parses an uploaded CSV/XLSX file and reports back
	// the columns + first N rows + suggested mapping — no DB writes.
	ImportPreview(ctx context.Context, file io.Reader, filename string) (*models.ContactImportPreview, *errx.Error)

	// ValidateImportMapping reports whether a column mapping is usable:
	// exactly the checks ImportCommit runs before it touches a row. Callers
	// that persist a mapping for later (the Google Sheets sync sources) use
	// it so a bad mapping is caught when it is saved, not on the next sync.
	ValidateImportMapping(mapping []models.ContactImportColumnMapping) *errx.Error

	// ImportCommit re-parses the uploaded file with the chosen mapping
	// and performs the upsert / skip / dedup work. Returns per-row
	// result counts plus a list of rows that failed (with reasons).
	ImportCommit(ctx context.Context, userID string, orgID uuid.UUID, file io.Reader, filename string, opts *models.ContactImportCommit) (*models.ContactImportResult, *errx.Error)

	// ListCustomFieldKeys returns the org's distinct contact custom-field keys,
	// frequency-ranked then alphabetical, capped at 200. Powers the dashboard
	// variable picker's real-field suggestions.
	ListCustomFieldKeys(ctx context.Context, orgID uuid.UUID) ([]string, *errx.Error)

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

	// SetCampaignWaker wires the campaign service so attaching a lead to a
	// running campaign wakes that campaign's parked send chain. Optional: with
	// no waker the leads still send, just not until the chain's next tick.
	SetCampaignWaker(w CampaignWaker)
}

// CampaignWaker pulls the parked wakeup of a running campaign forward when
// leads are attached to it. A campaign is a single self-perpetuating task, so a
// campaign that had nothing to send is parked and would otherwise not look at
// these leads until that park fires. Satisfied structurally by
// campaign.CampaignService, so this package needs no import of it.
type CampaignWaker interface {
	WakeCampaigns(ctx context.Context, orgID uuid.UUID, campaignIDs []string)
}

type contactService struct {
	contactRepository  repository.ContactRepository
	subRepo            repository.SubscriptionRepository
	planRepo           repository.PlanRepository
	streamingPublisher *pubsub.StreamingPublisher
	campaignWaker      CampaignWaker
	// orgRisk files import-quality findings on the workspace's posture.
	// Optional/nil-safe: without it a bad import is reported but not fused.
	orgRisk orgrisk.Service
}

// WireOrgRisk attaches the organization risk posture.
func (s *contactService) WireOrgRisk(r orgrisk.Service) { s.orgRisk = r }

// OrgRiskAware is the optional capability the caller uses to attach it.
type OrgRiskAware interface {
	WireOrgRisk(r orgrisk.Service)
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

func (s *contactService) SetCampaignWaker(w CampaignWaker) { s.campaignWaker = w }

// wakeCampaigns is the single place every lead-attaching path funnels through
// (add, update, bulk edit, CSV/XLSX import and the Google Sheets sync, which
// runs through ImportCommit). Best effort: the lead is already stored, and the
// reconciler plus the capped deferral horizon are the backstops.
func (s *contactService) wakeCampaigns(ctx context.Context, orgID uuid.UUID, campaignIDs []string) {
	if s.campaignWaker == nil || len(campaignIDs) == 0 {
		return
	}
	s.campaignWaker.WakeCampaigns(ctx, orgID, campaignIDs)
}

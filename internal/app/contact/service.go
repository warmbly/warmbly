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
	"github.com/warmbly/warmbly/internal/pkg/typesafe"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
)

type ContactService interface {
	Add(ctx context.Context, userID string, orgID uuid.UUID, contacts []models.AddContact) ([]models.Contact, *errx.Error)
	Search(ctx context.Context, userID, cursor, category, limit string, filters models.SearchContacts) (*models.ContactsResult, *errx.Error)
	// SearchCounts returns org-wide contact facet totals for the browse sidebar.
	SearchCounts(ctx context.Context, orgID string) (*models.ContactsCounts, *errx.Error)
	// CampaignLeadCounts returns per-status lead totals for one campaign.
	CampaignLeadCounts(ctx context.Context, orgID, campaignID string) (*models.CampaignLeadCounts, *errx.Error)
	BulkUpdate(ctx context.Context, userID string, orgID uuid.UUID, data *models.BulkEditContactsData) ([]models.Contact, *errx.Error)
	// ResolveSelection turns a bulk action's selection into the contact ids it
	// applies to: an explicit list as given, or everything matching the search
	// the dashboard's "select all matching" sends, minus its exclusions.
	ResolveSelection(ctx context.Context, orgID uuid.UUID, sel models.ContactSelection) ([]string, *errx.Error)
	Update(ctx context.Context, userID, contactID string, orgID uuid.UUID, data *models.UpdateContact) (*models.Contact, *errx.Error)
	BulkDelete(ctx context.Context, userID string, orgID uuid.UUID, contactIDs []string) *errx.Error
	Delete(ctx context.Context, userID string, orgID uuid.UUID, contactID string) *errx.Error

	// Export streams every contact matching the request into the given
	// writer. The format / filename / content-type are returned so the
	// handler can set headers correctly.
	Export(ctx context.Context, orgID string, req *models.ContactExportRequest, w io.Writer) (filename, contentType string, count int, err *errx.Error)

	// ImportPreview parses an uploaded CSV/XLSX file and reports back
	// the columns + first N rows + suggested mapping — no DB writes.
	ImportPreview(ctx context.Context, orgID uuid.UUID, file io.Reader, filename string) (*models.ContactImportPreview, *errx.Error)

	// SuggestImportMapping suggests a column mapping for any importer's
	// preview, and reports which columns the TypeSafe judgment placed.
	SuggestImportMapping(ctx context.Context, orgID uuid.UUID, headers []string, sample [][]string) ([]models.ContactImportColumnMapping, []int)

	// ValidateImportMapping reports whether a column mapping is usable:
	// exactly the checks ImportCommit runs before it touches a row. Callers
	// that persist a mapping for later (the Google Sheets sync sources) use
	// it so a bad mapping is caught when it is saved, not on the next sync.
	ValidateImportMapping(mapping []models.ContactImportColumnMapping) *errx.Error
	// ValidateSegmentTargets reports whether every id names a segment in the
	// organization, so a saved source is refused when it is written rather
	// than on its next run.
	ValidateSegmentTargets(ctx context.Context, orgID uuid.UUID, ids []string) *errx.Error

	// ImportCommit re-parses the uploaded file with the chosen mapping
	// and performs the upsert / skip / dedup work. Returns per-row
	// result counts plus a list of rows that failed (with reasons).
	ImportCommit(ctx context.Context, userID string, orgID uuid.UUID, file io.Reader, filename string, opts *models.ContactImportCommit) (*models.ContactImportResult, *errx.Error)

	// ValidateImportOptions runs every check an import makes before it reads a
	// row, so a background import is refused when it is started.
	ValidateImportOptions(ctx context.Context, userID string, orgID uuid.UUID, opts *models.ContactImportCommit) *errx.Error
	// BuildImportPreview describes already-parsed rows for the column mapper.
	BuildImportPreview(ctx context.Context, orgID uuid.UUID, filename, format string, rows [][]string) (*models.ContactImportPreview, *errx.Error)
	// AnalyzeImport reports what an import of rows would do under a mapping,
	// without writing anything.
	AnalyzeImport(ctx context.Context, userID string, orgID uuid.UUID, rows []ImportRow, mapping []models.ContactImportColumnMapping) (*models.ContactImportAnalysis, *errx.Error)
	// RunImport applies an import to rows chunk by chunk, settling each chunk
	// with sink (nil to only collect the result). prior are contacts an
	// earlier run of the same import touched, so they still join its segments.
	RunImport(ctx context.Context, userID string, orgID uuid.UUID, rows []ImportRow, opts *models.ContactImportCommit, sink ImportSink, prior []uuid.UUID) (*models.ContactImportResult, *errx.Error)

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

	// LookupSender matches the address first, then the thread's campaign send.
	LookupSender(ctx context.Context, orgID *uuid.UUID, email string, thread models.ContactLookupThread) (*models.ContactLookup, *errx.Error)

	// ListSentEmails enumerates every send (or attempted send) we made
	// to the contact, newest first.
	ListSentEmails(ctx context.Context, orgID, contactID uuid.UUID, limit int, beforeSentAt *time.Time, beforeTaskID *uuid.UUID) (*models.ContactSentEmailsResult, *errx.Error)

	// ListTimeline returns a merged, reverse-chronological feed of all
	// engagement + CRM events for the contact.
	ListTimeline(ctx context.Context, orgID, contactID uuid.UUID, limit int, cursor *models.ContactTimelineKey) (*models.ContactTimelineResult, *errx.Error)

	// SetCampaignWaker wires the campaign service so attaching a lead to a
	// running campaign wakes that campaign's parked send chain. Optional: with
	// no waker the leads still send, just not until the chain's next tick.
	SetCampaignWaker(w CampaignWaker)

	// CampaignStates returns the contact's campaigns with steps, progress,
	// lead status and a scheduler-backed next action.
	CampaignStates(ctx context.Context, orgID, contactID uuid.UUID) ([]models.ContactCampaignState, *errx.Error)
	// SetNextSendPreviewer wires the scheduler preview behind CampaignStates.
	// Optional; without it the panel carries no next action.
	SetNextSendPreviewer(p scheduler.ContactSendPreviewer)
}

// CampaignWaker pulls the parked wakeup of a running campaign forward when
// leads are attached to it. A campaign is a single self-perpetuating task, so a
// campaign that had nothing to send is parked and would otherwise not look at
// these leads until that park fires. Satisfied structurally by
// campaign.CampaignService, so this package needs no import of it.
type CampaignWaker interface {
	WakeCampaigns(ctx context.Context, orgID uuid.UUID, campaignIDs []string)
}

// SegmentLinker writes segment include overrides for imported contacts.
// Satisfied structurally by repository.SegmentRepository.
type SegmentLinker interface {
	Get(ctx context.Context, orgID, id uuid.UUID) (*models.Segment, *errx.Error)
	SetMembers(ctx context.Context, orgID, segmentID uuid.UUID, contactIDs []uuid.UUID, mode models.SegmentMemberMode) (int, *errx.Error)
}

// SegmentCampaignSyncer re-enrols the org's segment-linked campaigns after
// contacts changed. Satisfied structurally by segment.Service.
type SegmentCampaignSyncer interface {
	SyncOrgLinkedCampaigns(ctx context.Context, orgID uuid.UUID)
}

// SegmentAware is the optional capability the caller uses to attach both.
type SegmentAware interface {
	WireSegments(linker SegmentLinker, syncer SegmentCampaignSyncer)
}

// WebhookDispatcher delivers contact.created to customer webhooks and
// automations. Satisfied structurally by webhook.Service.
type WebhookDispatcher interface {
	Dispatch(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data any) (uuid.UUID, error)
}

// WebhookAware is the optional capability the caller uses to attach it.
type WebhookAware interface {
	WireWebhooks(w WebhookDispatcher)
}

type contactService struct {
	contactRepository  repository.ContactRepository
	subRepo            repository.SubscriptionRepository
	planRepo           repository.PlanRepository
	streamingPublisher *pubsub.StreamingPublisher
	campaignWaker      CampaignWaker
	segmentLinker      SegmentLinker
	segmentSyncer      SegmentCampaignSyncer
	previewer          scheduler.ContactSendPreviewer
	// orgRisk files import-quality findings on the workspace's posture.
	// Optional/nil-safe: without it a bad import is reported but not fused.
	orgRisk orgrisk.Service
	// explainer builds the verification "why" for the contact drawer.
	explainer VerificationExplainer
	// webhooks fans contact.created out; nil-safe (no events).
	webhooks WebhookDispatcher
	// columnJudge places import columns no header matched; nil leaves the
	// suggestion deterministic.
	columnJudge typesafe.Asker
}

// VerificationAware is implemented by the contact service so main can hand
// it the explainer.
type VerificationAware interface {
	WireVerification(e VerificationExplainer)
}

// VerificationExplainer mirrors emailverify.Service.Explain.
type VerificationExplainer interface {
	Explain(ctx context.Context, contactID uuid.UUID) *models.ContactVerificationDetail
}

// WireVerification attaches the verification explainer.
func (s *contactService) WireVerification(e VerificationExplainer) { s.explainer = e }

// WireOrgRisk attaches the organization risk posture.
func (s *contactService) WireOrgRisk(r orgrisk.Service) { s.orgRisk = r }

// WireWebhooks attaches the event dispatcher behind contact.created.
func (s *contactService) WireWebhooks(w WebhookDispatcher) { s.webhooks = w }

// WireColumnJudge attaches the TypeSafe asker behind import column inference.
func (s *contactService) WireColumnJudge(a typesafe.Asker) { s.columnJudge = a }

// ColumnJudgeAware is the optional capability the caller uses to attach it.
type ColumnJudgeAware interface {
	WireColumnJudge(a typesafe.Asker)
}

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

func (s *contactService) publishContactsReload(ctx context.Context, userID string, operationID string, orgID uuid.UUID) {
	if s.streamingPublisher == nil {
		return
	}
	s.streamingPublisher.PublishContactsReload(ctx, userID, operationID, orgID)
}

func (s *contactService) SetCampaignWaker(w CampaignWaker) { s.campaignWaker = w }

// WireSegments attaches the segment override writer and linked-campaign syncer.
func (s *contactService) WireSegments(linker SegmentLinker, syncer SegmentCampaignSyncer) {
	s.segmentLinker = linker
	s.segmentSyncer = syncer
}

// syncSegmentCampaigns follows every contact-mutating path: a changed contact
// may now match a segment linked to a campaign. Best effort like wakeCampaigns;
// the periodic sweep is the backstop.
func (s *contactService) syncSegmentCampaigns(ctx context.Context, orgID uuid.UUID) {
	if s.segmentSyncer == nil {
		return
	}
	s.segmentSyncer.SyncOrgLinkedCampaigns(ctx, orgID)
}

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

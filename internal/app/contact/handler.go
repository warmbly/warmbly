package contact

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/paging"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

// checkContactLimit enforces the plan's contact ceiling for a batch about to
// be created. Callers that write in chunks (the importer) must ask once for
// the whole batch, or a rejected chunk turns one plan problem into one error
// per row.
//
// This path reads the plan directly instead of going through the feature
// gate, so it never saw the self-host short-circuit: every self-hosted org
// was capped at the seeded Free Trial plan's 100 contacts even though
// BILLING_PROVIDER=none unlocks every other limit.
func (s *contactService) checkContactLimit(ctx context.Context, userID string, adding int) *errx.Error {
	if config.SelfHosted() || s.subRepo == nil || s.planRepo == nil || adding <= 0 {
		return nil
	}
	uid, parseErr := uuid.Parse(userID)
	if parseErr != nil {
		return nil
	}
	sub, err := s.subRepo.GetByUserID(ctx, uid)
	if err != nil || sub == nil {
		return nil
	}
	plan, err := s.planRepo.GetByID(ctx, sub.EffectivePlanID())
	if err != nil || plan == nil || plan.MaxContacts <= 0 {
		return nil
	}
	currentCount, xerr := s.contactRepository.GetContactCount(ctx, userID)
	if xerr != nil {
		return nil
	}
	if currentCount+adding > int(plan.MaxContacts) {
		return errx.New(errx.Forbidden, fmt.Sprintf(
			"adding %d contacts would put you over your plan's limit of %d (you have %d)",
			adding, plan.MaxContacts, currentCount))
	}
	return nil
}

func (s *contactService) Add(ctx context.Context, userID string, orgID uuid.UUID, contacts []models.AddContact) ([]models.Contact, *errx.Error) {
	if xerr := s.checkContactLimit(ctx, userID, len(contacts)); xerr != nil {
		return nil, xerr
	}

	// Checked before the write so an unknown segment is a 400 rather than a
	// silently-skipped link. The override itself is written inside the
	// repository's transaction, so a contact is never created without the
	// segment membership the caller asked for.
	if xerr := s.checkSegmentTargets(ctx, orgID, contacts); xerr != nil {
		return nil, xerr
	}

	created, xerr := s.contactRepository.Add(ctx, userID, orgID, contacts)
	if xerr != nil {
		return nil, xerr
	}

	s.publishContactsReload(ctx, userID, "contacts:add")
	var attached []string
	for i := range contacts {
		attached = append(attached, contacts[i].Campaigns...)
	}
	s.wakeCampaigns(ctx, orgID, attached)
	s.syncSegmentCampaigns(ctx, orgID)
	s.emitCreated(ctx, orgID, contacts, created)
	return created, nil
}

// checkSegmentTargets rejects a create whose segments do not exist in the
// organization. Existence is checked once for the whole batch, so N contacts
// naming the same segment cost one lookup; the repository writes the overrides
// itself, inside the same transaction as the contacts.
func (s *contactService) checkSegmentTargets(ctx context.Context, orgID uuid.UUID, contacts []models.AddContact) *errx.Error {
	var raw []string
	for i := range contacts {
		raw = append(raw, contacts[i].Segments...)
	}
	if len(raw) == 0 {
		return nil
	}
	_, xerr := s.parseSegmentIDs(ctx, orgID, raw)
	return xerr
}

func (s *contactService) Search(ctx context.Context, orgID, cursor, category, limit string, filters models.SearchContacts) (*models.ContactsResult, *errx.Error) {
	cursorPos, err := paging.DecodeSortCursor(cursor)
	if err != nil {
		return nil, err
	}
	categoryId, err := validate.Uuid(category)
	if err != nil {
		return nil, err
	}

	limitN, err := validate.Limit(limit)
	if err != nil {
		return nil, err
	}

	if err := validateLeadFilters(filters); err != nil {
		return nil, err
	}

	return s.contactRepository.Search(ctx, orgID, categoryId, cursorPos, filters, limitN)
}

// validateLeadFilters gates the single-campaign Leads-view filters: an unknown
// value or the wrong campaign cardinality is a client contract error (400 with
// a stable code), not a silently-ignored no-op.
func validateLeadFilters(filters models.SearchContacts) *errx.Error {
	if filters.LeadStatus != "" && !models.ValidLeadStatus(filters.LeadStatus) {
		return errx.NewWithIdentifier(errx.BadRequest, "invalid_lead_status", "invalid lead_status")
	}
	if filters.Engagement != "" && !models.ValidLeadEngagement(filters.Engagement) {
		return errx.NewWithIdentifier(errx.BadRequest, "invalid_engagement", "invalid engagement")
	}
	if (filters.LeadStatus != "" || filters.Engagement != "") && len(filters.CampaignIDs) != 1 {
		return errx.NewWithIdentifier(errx.BadRequest, "lead_filter_requires_campaign", "lead_status and engagement require exactly one campaign_id")
	}
	return nil
}

func (s *contactService) SearchCounts(ctx context.Context, orgID string) (*models.ContactsCounts, *errx.Error) {
	return s.contactRepository.SearchCounts(ctx, orgID)
}

func (s *contactService) ListCustomFieldKeys(ctx context.Context, orgID uuid.UUID) ([]string, *errx.Error) {
	keys, err := s.contactRepository.DistinctCustomFieldKeys(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	return keys, nil
}

func (s *contactService) CampaignLeadCounts(ctx context.Context, orgID, campaignID string) (*models.CampaignLeadCounts, *errx.Error) {
	if _, err := validate.Uuid(campaignID); err != nil {
		return nil, err
	}
	return s.contactRepository.CampaignLeadCounts(ctx, orgID, campaignID)
}

func (s *contactService) BulkUpdate(ctx context.Context, userID string, orgID uuid.UUID, data *models.BulkEditContactsData) ([]models.Contact, *errx.Error) {
	updated, xerr := s.contactRepository.BulkUpdate(ctx, userID, orgID, data)
	if xerr != nil {
		return nil, xerr
	}

	s.publishContactsReload(ctx, userID, "contacts:bulk_update")
	s.wakeCampaigns(ctx, orgID, data.AddCampaigns)
	s.syncSegmentCampaigns(ctx, orgID)
	return updated, nil
}

func (s *contactService) Update(ctx context.Context, userID, contactID string, orgID uuid.UUID, data *models.UpdateContact) (*models.Contact, *errx.Error) {
	updated, xerr := s.contactRepository.Update(ctx, userID, contactID, orgID, data)
	if xerr != nil {
		return nil, xerr
	}

	s.publishContactsReload(ctx, userID, "contacts:update:"+contactID)
	s.wakeCampaigns(ctx, orgID, data.Campaigns)
	s.syncSegmentCampaigns(ctx, orgID)
	return updated, nil
}

func (s *contactService) BulkDelete(ctx context.Context, userID string, orgID uuid.UUID, contactIDs []string) *errx.Error {
	if xerr := s.contactRepository.BulkDelete(ctx, userID, orgID, contactIDs); xerr != nil {
		return xerr
	}

	s.publishContactsReload(ctx, userID, "contacts:bulk_delete")
	return nil
}

func (s *contactService) Delete(ctx context.Context, userID string, orgID uuid.UUID, contactID string) *errx.Error {
	if xerr := s.contactRepository.Delete(ctx, userID, orgID, contactID); xerr != nil {
		return xerr
	}

	s.publishContactsReload(ctx, userID, "contacts:delete:"+contactID)
	return nil
}

func (s *contactService) GetDetail(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, contactID uuid.UUID) (*models.ContactDetail, *errx.Error) {
	detail, xerr := s.contactRepository.GetDetail(ctx, userID, orgID, contactID)
	if xerr != nil || detail == nil {
		return detail, xerr
	}
	if s.explainer != nil {
		detail.Verification = s.explainer.Explain(ctx, contactID)
	}
	return detail, nil
}

func (s *contactService) GetByEmail(ctx context.Context, orgID *uuid.UUID, email string) (*models.Contact, *errx.Error) {
	if orgID == nil || strings.TrimSpace(email) == "" {
		return nil, nil
	}
	// The repo already returns (nil, nil) when no contact matches, so an
	// unknown sender flows through as a clean "no contact" rather than an error.
	return s.contactRepository.GetByEmailAndOrganization(ctx, *orgID, email)
}

func (s *contactService) ListSentEmails(ctx context.Context, userID, contactID uuid.UUID, limit int, beforeSentAt *time.Time, beforeTaskID *uuid.UUID) (*models.ContactSentEmailsResult, *errx.Error) {
	return s.contactRepository.ListSentEmails(ctx, userID, contactID, limit, beforeSentAt, beforeTaskID)
}

func (s *contactService) ListTimeline(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, contactID uuid.UUID, limit int, cursor *models.ContactTimelineKey) (*models.ContactTimelineResult, *errx.Error) {
	return s.contactRepository.ListTimeline(ctx, userID, orgID, contactID, limit, cursor)
}

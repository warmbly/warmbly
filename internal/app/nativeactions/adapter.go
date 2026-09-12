package nativeactions

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/advanced"
	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Adapter satisfies integration.NativeActions, bridging the integration
// package's automation executor to the advanced/contact/org services. It
// converts *errx.Error to error (so a nil error stays nil) and resolves the
// contact + org owner the native CRM/contact actions need. Shared by the
// backend and consumer binaries, since automation actions run in BOTH (the
// consumer dispatches reply/bounce/warmup events, the backend the rest).
type Adapter struct {
	Adv      advanced.Service
	Contacts repository.ContactRepository
	Orgs     repository.OrganizationRepository
	// ContactSvc is the contact service behind the lead-intake actions: its
	// upsert runs the plan check, wakes campaigns and fires contact.created,
	// which a bare repository write would not.
	ContactSvc contact.ContactService
	// Campaigns flips "Keep running for new leads" on a campaign an
	// automation feeds, so it waits for leads instead of finishing.
	Campaigns CampaignKeeper
}

func (a Adapter) ResolveContact(ctx context.Context, orgID uuid.UUID, contactID, email string) (*models.Contact, error) {
	// Both lookups are ORG-SCOPED — never resolve a contact id from another org,
	// even if a stale/crafted id reaches the event data. A failed lookup is an
	// error, not a miss: "skip if it exists" must not write over a contact it
	// could not see.
	if contactID != "" {
		if id, perr := uuid.Parse(contactID); perr == nil {
			cs, e := a.Contacts.GetByIDsAndOrganization(ctx, orgID, []uuid.UUID{id})
			if e != nil {
				return nil, e
			}
			if len(cs) > 0 {
				return &cs[0], nil
			}
		}
	}
	if email != "" {
		c, e := a.Contacts.GetByEmailAndOrganization(ctx, orgID, email)
		if e != nil {
			return nil, e
		}
		if c != nil {
			return c, nil
		}
	}
	return nil, nil
}

func (a Adapter) OrgOwner(ctx context.Context, orgID uuid.UUID) (uuid.UUID, error) {
	org, err := a.Orgs.GetByID(ctx, orgID)
	if err != nil {
		return uuid.Nil, err
	}
	if org == nil {
		return uuid.Nil, fmt.Errorf("organization not found")
	}
	return org.OwnerUserID, nil
}

func (a Adapter) AddTag(ctx context.Context, orgID, actorID, contactID, categoryID uuid.UUID) error {
	if _, e := a.Contacts.Update(ctx, actorID.String(), contactID.String(), orgID, &models.UpdateContact{
		AddCategories: []string{categoryID.String()},
	}); e != nil {
		return e
	}
	return nil
}

func (a Adapter) RemoveTag(ctx context.Context, orgID, actorID, contactID, categoryID uuid.UUID) error {
	if _, e := a.Contacts.Update(ctx, actorID.String(), contactID.String(), orgID, &models.UpdateContact{
		RemoveCategories: []string{categoryID.String()},
	}); e != nil {
		return e
	}
	return nil
}

func (a Adapter) CreateTask(ctx context.Context, orgID, createdBy uuid.UUID, data *models.CreateCRMTask) error {
	if _, e := a.Adv.CreateContactTask(ctx, orgID, createdBy, data); e != nil {
		return e
	}
	return nil
}

func (a Adapter) CreateDeal(ctx context.Context, orgID, createdBy uuid.UUID, data *models.CreateDeal) error {
	if _, e := a.Adv.CreateContactDeal(ctx, orgID, createdBy, data); e != nil {
		return e
	}
	return nil
}

func (a Adapter) MoveDealStage(ctx context.Context, orgID, contactID, pipelineID, stageID uuid.UUID) error {
	if _, e := a.Adv.MoveContactDealStage(ctx, orgID, contactID, pipelineID, stageID); e != nil {
		return e
	}
	return nil
}

func (a Adapter) Unsubscribe(ctx context.Context, campaignID, contactID uuid.UUID) error {
	if e := a.Adv.Unsubscribe(ctx, campaignID, contactID); e != nil {
		return e
	}
	return nil
}

// UpsertContact writes one contact through the contact service (upsert by
// email, tags, campaign and segment links, contact.created for a new row).
func (a Adapter) UpsertContact(ctx context.Context, orgID, actorID uuid.UUID, in models.AddContact) (*models.Contact, error) {
	if a.ContactSvc == nil {
		return nil, fmt.Errorf("contact writes are not available")
	}
	created, xerr := a.ContactSvc.Add(ctx, actorID.String(), orgID, []models.AddContact{in})
	if xerr != nil {
		return nil, xerr
	}
	if len(created) == 0 {
		return nil, fmt.Errorf("the contact write returned nothing")
	}
	return &created[0], nil
}

// CampaignKeeper is the campaign service's "Keep running for new leads" switch.
type CampaignKeeper interface {
	KeepRunning(ctx context.Context, orgID, campaignID uuid.UUID, reason string) *errx.Error
}

// KeepCampaignRunning turns on "Keep running for new leads" on a campaign an
// automation feeds. A campaign that is not the organization's is reported,
// never touched.
func (a Adapter) KeepCampaignRunning(ctx context.Context, orgID, campaignID uuid.UUID, reason string) error {
	if a.Campaigns == nil {
		return fmt.Errorf("campaign settings are not available")
	}
	if xerr := a.Campaigns.KeepRunning(ctx, orgID, campaignID, reason); xerr != nil {
		if xerr.Code == errx.ErrNotFound.Code {
			return fmt.Errorf("campaign %s was not found in this workspace", campaignID)
		}
		return xerr
	}
	return nil
}

// AddToCampaign enrols an existing contact in a campaign through the bulk
// edit path, which also wakes the campaign's parked send chain.
func (a Adapter) AddToCampaign(ctx context.Context, orgID, actorID, contactID, campaignID uuid.UUID) error {
	if a.ContactSvc == nil {
		return fmt.Errorf("contact writes are not available")
	}
	if _, xerr := a.ContactSvc.BulkUpdate(ctx, actorID.String(), orgID, &models.BulkEditContactsData{
		ContactSelection: models.ContactSelection{Contacts: []string{contactID.String()}},
		AddCampaigns:     []string{campaignID.String()},
	}); xerr != nil {
		return xerr
	}
	return nil
}

// LabelThread applies unibox conversation labels to a thread on behalf of the
// mailbox owner (the advanced service guards category ownership). The error is
// already a plain error, so it passes straight through.
func (a Adapter) LabelThread(ctx context.Context, orgID uuid.UUID, threadID string, categoryIDs []uuid.UUID) error {
	return a.Adv.LabelThread(ctx, orgID, threadID, categoryIDs)
}

// ListCategories / CreateCategory / ListPipelines back the AI agent step's
// argument-based tag/label/deal tools (the model picks a name, resolved live).
// Categories and pipelines are both org-scoped (tags == unibox labels;
// pipelines hydrate their stages). All three delegate straight to the
// advanced service.
func (a Adapter) ListCategories(ctx context.Context, orgID uuid.UUID) ([]models.MiniCategory, error) {
	return a.Adv.ListCategories(ctx, orgID)
}

func (a Adapter) CreateCategory(ctx context.Context, orgID uuid.UUID, title, color string) (models.MiniCategory, error) {
	return a.Adv.CreateCategory(ctx, orgID, title, color)
}

func (a Adapter) ListPipelines(ctx context.Context, orgID uuid.UUID) ([]models.Pipeline, error) {
	return a.Adv.ListPipelines(ctx, orgID)
}

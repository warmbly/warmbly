package nativeactions

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/advanced"
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
}

func (a Adapter) ResolveContact(ctx context.Context, orgID uuid.UUID, contactID, email string) (*models.Contact, error) {
	// Both lookups are ORG-SCOPED — never resolve a contact id from another org,
	// even if a stale/crafted id reaches the event data.
	if contactID != "" {
		if id, perr := uuid.Parse(contactID); perr == nil {
			if cs, e := a.Contacts.GetByIDsAndOrganization(ctx, orgID, []uuid.UUID{id}); e == nil && len(cs) > 0 {
				return &cs[0], nil
			}
		}
	}
	if email != "" {
		if c, e := a.Contacts.GetByEmailAndOrganization(ctx, orgID, email); e == nil && c != nil {
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

// LabelThread applies unibox conversation labels to a thread on behalf of the
// mailbox owner (the advanced service guards category ownership). The error is
// already a plain error, so it passes straight through.
func (a Adapter) LabelThread(ctx context.Context, userID uuid.UUID, threadID string, categoryIDs []uuid.UUID) error {
	return a.Adv.LabelThread(ctx, userID, threadID, categoryIDs)
}

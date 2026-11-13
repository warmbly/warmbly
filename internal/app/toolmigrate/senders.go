package toolmigrate

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// LinkSenders adds mailboxes connected since the migration ran to the draft
// campaigns that named them. Running it twice changes nothing more.
func (s *Service) LinkSenders(ctx context.Context, scope Scope, actor Actor, id uuid.UUID) (*models.ToolMigration, *errx.Error) {
	if _, xerr := repoScope(scope); xerr != nil {
		return nil, xerr
	}
	if xerr := s.authorize(ctx, scope, actor, true); xerr != nil {
		return nil, xerr
	}
	m, xerr := s.Get(ctx, scope, id)
	if xerr != nil {
		return nil, xerr
	}
	need := []permNeed{{models.PermManageCampaigns, "Linking senders needs permission to manage campaigns."}}
	for _, t := range m.Targets {
		if !hasMissing(t.Followup) {
			continue
		}
		if scope.OrgID != nil {
			if !actor.OrgPerms.HasPermission(models.PermManageCampaigns) {
				return nil, errPermission(need[0].msg)
			}
		} else if xerr := s.requireMember(ctx, t.OrganizationID, actor.UserID, need); xerr != nil {
			return nil, xerr
		}
		changed, xerr := s.linkTarget(ctx, t.OrganizationID, &t.Followup)
		if xerr != nil {
			return nil, xerr
		}
		if !changed {
			continue
		}
		raw, err := json.Marshal(t.Followup)
		if err != nil {
			return nil, errx.InternalError()
		}
		if err := s.d.Repo.SetFollowup(ctx, id, t.ID, raw); err != nil {
			return nil, errx.InternalError()
		}
		s.audit(ctx, t.OrganizationID, actor.UserID, models.AuditActionUpdate, id, map[string]string{"senders": "linked"})
	}
	return s.Get(ctx, scope, id)
}

func hasMissing(f models.ToolMigrationFollowup) bool {
	for _, c := range f.Campaigns {
		if len(c.MissingSenders) > 0 {
			return true
		}
	}
	return false
}

// linkTarget resolves one target's missing senders and adds the ones now connected.
func (s *Service) linkTarget(ctx context.Context, orgID uuid.UUID, f *models.ToolMigrationFollowup) (bool, *errx.Error) {
	changed := false
	for i := range f.Campaigns {
		c := &f.Campaigns[i]
		if len(c.MissingSenders) == 0 {
			continue
		}
		found, err := s.d.Repo.MailboxIDsByEmail(ctx, orgID, c.MissingSenders)
		if err != nil {
			return false, errx.InternalError()
		}
		if len(found) == 0 {
			continue
		}
		camp, xerr := s.d.Campaigns.Get(ctx, orgID.String(), c.CampaignID.String())
		if xerr != nil {
			if xerr.Code == errx.NotFound {
				// The campaign was deleted; nothing is left to link.
				c.MissingSenders, changed = nil, true
				continue
			}
			return false, xerr
		}
		current, xerr := s.d.Campaigns.ListCampaignSenders(ctx, orgID, c.CampaignID.String())
		if xerr != nil {
			return false, xerr
		}
		in := make([]models.CampaignSenderInput, 0, len(current)+len(found))
		have := map[uuid.UUID]bool{}
		if camp.SenderStrategy == "explicit" {
			for _, cs := range current {
				weight, enabled := cs.Weight, cs.Enabled
				in = append(in, models.CampaignSenderInput{EmailAccountID: cs.EmailAccountID, Weight: &weight, Enabled: &enabled})
				have[cs.EmailAccountID] = true
			}
		}
		var still []string
		for _, addr := range c.MissingSenders {
			mid, ok := found[addr]
			if !ok {
				still = append(still, addr)
				continue
			}
			if !have[mid] {
				have[mid] = true
				in = append(in, models.CampaignSenderInput{EmailAccountID: mid})
			}
		}
		if _, xerr := s.d.Campaigns.ReplaceCampaignSenders(ctx, orgID, c.CampaignID.String(), in); xerr != nil {
			return false, xerr
		}
		if camp.SenderStrategy != "explicit" {
			explicit := "explicit"
			if _, xerr := s.d.Campaigns.Update(ctx, orgID.String(), c.CampaignID.String(), &models.UpdateCampaign{SenderStrategy: &explicit}); xerr != nil {
				return false, xerr
			}
		}
		c.MissingSenders, changed = still, true
	}
	return changed, nil
}

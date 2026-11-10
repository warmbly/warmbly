package agency

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
)

// adminReasonMax bounds the note an operator leaves on a grant, an override or a credit.
const adminReasonMax = 500

// AdminAgencyDetail is one agency as the admin panel shows it.
type AdminAgencyDetail struct {
	Agency  models.AdminAgency    `json:"agency"`
	Members []models.AgencyMember `json:"members"`
	Clients []models.AgencyClient `json:"clients"`
	Usage   *models.AgencyUsage   `json:"usage"`
	// AIBalance is the shared AI balance; Unmetered where AI is not charged.
	AIBalance *models.AgencyAIBalance `json:"ai_balance"`
}

// AdminList lists agencies with their plan, status, client count and pool use.
func (s *Service) AdminList(ctx context.Context, search string, limit, offset int) ([]models.AdminAgency, int, *errx.Error) {
	limit = min(max(limit, 1), 100)
	offset = max(offset, 0)
	list, total, err := s.repo.ListForAdmin(ctx, search, limit, offset)
	if err != nil {
		return nil, 0, internal(err)
	}
	for i := range list {
		a := &list[i].Agency
		s.hydrate(ctx, a)
		list[i].PoolUsed, list[i].PoolTotal, list[i].Unlimited = s.PoolTotals(ctx, a)
		s.describe(ctx, &list[i])
	}
	if list == nil {
		list = []models.AdminAgency{}
	}
	return list, total, nil
}

// AdminGet is one agency with its staff, clients and pool.
func (s *Service) AdminGet(ctx context.Context, agencyID uuid.UUID) (*AdminAgencyDetail, *errx.Error) {
	a, err := s.repo.Get(ctx, agencyID)
	if err != nil {
		return nil, internal(err)
	}
	if a == nil {
		return nil, errNotFound
	}
	s.hydrate(ctx, a)
	out := &AdminAgencyDetail{Agency: models.AdminAgency{Agency: *a}}
	if owner, err := s.users.GetUser(ctx, a.OwnerUserID); err == nil && owner != nil {
		out.Agency.OwnerEmail = owner.Email
	}
	if out.Members, err = s.repo.ListMembers(ctx, agencyID); err != nil {
		return nil, internal(err)
	}
	if out.Clients, err = s.repo.ListClientOrganizations(ctx, agencyID); err != nil {
		return nil, internal(err)
	}
	usage, xerr := s.usage(ctx, a)
	if xerr != nil {
		return nil, xerr
	}
	out.Usage = usage
	if out.AIBalance, xerr = s.aiBalance(ctx, a); xerr != nil {
		return nil, xerr
	}
	out.Agency.PoolUsed, out.Agency.PoolTotal, out.Agency.Unlimited = usage.Used, usage.Total, usage.Unlimited
	s.describe(ctx, &out.Agency)
	if out.Members == nil {
		out.Members = []models.AgencyMember{}
	}
	if out.Clients == nil {
		out.Clients = []models.AgencyClient{}
	}
	return out, nil
}

// describe fills what an operator recorded on the agency and whether a plan is in force.
func (s *Service) describe(ctx context.Context, out *models.AdminAgency) {
	a := &out.Agency
	now := s.now()
	_, _, entitled := s.grantPlan(a)
	out.Entitled = entitled || !s.billing
	if a.ManagedAt != nil && a.ManagedPlanID != nil {
		m := &models.AdminAgencyManagedPlan{
			PlanID: *a.ManagedPlanID, Until: a.ManagedUntil, GrantedAt: *a.ManagedAt, GrantedBy: a.ManagedBy, Active: a.Granted(now),
		}
		if a.ManagedReason != nil {
			m.Reason = *a.ManagedReason
		}
		if p, err := s.plans.GetByID(ctx, m.PlanID); err == nil && p != nil && p.Name != nil {
			m.PlanName = *p.Name
		}
		out.ManagedPlan = m
	}
	if a.PoolOverride != nil || a.AIAllowanceOverride != nil {
		o := &models.AdminAgencyOverrides{MonthlySendPool: a.PoolOverride, AIAllowanceUSD: usdPtr(a.AIAllowanceOverride), UpdatedAt: a.OverrideAt, UpdatedBy: a.OverrideBy}
		if a.OverrideReason != nil {
			o.Reason = *a.OverrideReason
		}
		out.Overrides = o
	}
}

func (s *Service) adminLoad(ctx context.Context, agencyID uuid.UUID) (*models.Agency, *errx.Error) {
	a, err := s.repo.Get(ctx, agencyID)
	if err != nil {
		return nil, internal(err)
	}
	if a == nil {
		return nil, errNotFound
	}
	return a, nil
}

// adminReason is the note every operator change carries, so it is answerable later.
func adminReason(reason string) (string, *errx.Error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", errx.New(errx.BadRequest, "a reason is required, so the change is answerable later")
	}
	if len([]rune(reason)) > adminReasonMax {
		return "", errx.New(errx.BadRequest, "the reason is too long")
	}
	return reason, nil
}

// AdminCreate opens an agency owned by the account with that address, for an operator setting one up.
func (s *Service) AdminCreate(ctx context.Context, ownerEmail, name string) (*models.Agency, *errx.Error) {
	name, xerr := displayname.Validate("Agency name", name, displayname.Workspace, false)
	if xerr != nil {
		return nil, xerr
	}
	owner, err := s.users.GetUserByEmail(ctx, strings.TrimSpace(ownerEmail))
	if err != nil || owner == nil {
		return nil, errx.NewWithIdentifier(errx.NotFound, "user_not_found", "No account uses that address.")
	}
	a := &models.Agency{ID: uuid.New(), Name: name, OwnerUserID: owner.ID, Status: models.SubscriptionStatusIncomplete}
	if err := s.repo.Create(ctx, a); err != nil {
		return nil, internal(err)
	}
	return a, nil
}

// AdminGrantPlan puts the agency on an agency plan without Stripe; a nil until has no end.
func (s *Service) AdminGrantPlan(ctx context.Context, agencyID, planID, adminID uuid.UUID, reason string, until *time.Time) (*AdminAgencyDetail, *errx.Error) {
	reason, xerr := adminReason(reason)
	if xerr != nil {
		return nil, xerr
	}
	if until != nil && !until.After(s.now()) {
		return nil, errx.New(errx.BadRequest, "the end date must be in the future")
	}
	if _, xerr := s.adminLoad(ctx, agencyID); xerr != nil {
		return nil, xerr
	}
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil || plan == nil || !plan.IsAgencyPlan() {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "agency_plan_invalid", "That is not an agency plan.")
	}
	if err := s.repo.SetManagedPlan(ctx, agencyID, planID, adminID, reason, until); err != nil {
		return nil, internal(err)
	}
	return s.adminSynced(ctx, agencyID)
}

// AdminRevokePlan ends an operator's plan; the agency falls back to its own subscription.
func (s *Service) AdminRevokePlan(ctx context.Context, agencyID uuid.UUID) (*AdminAgencyDetail, *errx.Error) {
	if _, xerr := s.adminLoad(ctx, agencyID); xerr != nil {
		return nil, xerr
	}
	if err := s.repo.ClearManagedPlan(ctx, agencyID); err != nil {
		return nil, internal(err)
	}
	return s.adminSynced(ctx, agencyID)
}

// adminSynced converges every client on the agency's new state and reads it back.
func (s *Service) adminSynced(ctx context.Context, agencyID uuid.UUID) (*AdminAgencyDetail, *errx.Error) {
	s.forgetAgency(agencyID)
	if err := s.SyncAgency(ctx, agencyID); err != nil {
		return nil, internal(err)
	}
	return s.AdminGet(ctx, agencyID)
}

// AdminSetOverrides replaces the pool and AI allowance set for this agency; nil is the plan's own.
func (s *Service) AdminSetOverrides(ctx context.Context, agencyID, adminID uuid.UUID, pool *int, aiAllowance *models.Micro, reason string) (*AdminAgencyDetail, *errx.Error) {
	if pool != nil && (*pool < 1 || *pool > models.AgencyPoolOverrideMax) {
		return nil, errx.New(errx.BadRequest, "the monthly pool must be between 1 and 100,000,000 sends")
	}
	if aiAllowance != nil && (*aiAllowance < 0 || *aiAllowance > models.AgencyAIOperatorMax) {
		return nil, errx.New(errx.BadRequest, "the AI allowance must be between $0 and $100,000")
	}
	reason = strings.TrimSpace(reason)
	if pool != nil || aiAllowance != nil {
		var xerr *errx.Error
		if reason, xerr = adminReason(reason); xerr != nil {
			return nil, xerr
		}
	}
	if _, xerr := s.adminLoad(ctx, agencyID); xerr != nil {
		return nil, xerr
	}
	if err := s.repo.SetOverrides(ctx, agencyID, pool, aiAllowance, adminID, reason); err != nil {
		return nil, internal(err)
	}
	s.forgetAgency(agencyID)
	return s.AdminGet(ctx, agencyID)
}

// AdminCreditPool adds sends to the agency's current period, like a pack nobody paid for.
func (s *Service) AdminCreditPool(ctx context.Context, agencyID, adminID uuid.UUID, sends int, reason string) (*AdminAgencyDetail, *errx.Error) {
	reason, xerr := adminReason(reason)
	if xerr != nil {
		return nil, xerr
	}
	if sends < 1 || sends > models.AgencyPoolCreditMax {
		return nil, errx.New(errx.BadRequest, "add between 1 and 10,000,000 sends")
	}
	a, xerr := s.adminLoad(ctx, agencyID)
	if xerr != nil {
		return nil, xerr
	}
	if _, unlimited, active := s.poolSize(ctx, a); !active || unlimited {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "agency_no_pool", "This agency has no send pool to add to. Give it a plan first.")
	}
	start, _ := models.AgencyPeriod(a.CurrentPeriodStart, a.CurrentPeriodEnd, s.now())
	if _, err := s.repo.RecordTopup(ctx, &models.AgencyPoolTopup{
		AgencyID: agencyID, Sends: sends, PeriodStart: start, GrantedBy: &adminID, Reason: &reason,
	}); err != nil {
		return nil, internal(err)
	}
	s.forgetAgency(agencyID)
	return s.AdminGet(ctx, agencyID)
}

// AdminCreditAI adds money to the agency's shared AI balance that never expires, once per key.
func (s *Service) AdminCreditAI(ctx context.Context, agencyID, adminID uuid.UUID, amount models.Micro, reason, key string) (*AdminAgencyDetail, *errx.Error) {
	reason, xerr := adminReason(reason)
	if xerr != nil {
		return nil, xerr
	}
	if amount <= 0 || amount > models.AgencyAIOperatorMax {
		return nil, errx.New(errx.BadRequest, "add between $0.01 and $100,000")
	}
	if s.ai == nil || !s.aiMetered {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "agency_ai_unmetered", "AI is not charged on this instance, so there is no balance to add to.")
	}
	a, xerr := s.adminLoad(ctx, agencyID)
	if xerr != nil {
		return nil, xerr
	}
	if _, err := s.ai.Credit(ctx, agencyID, amount, s.aiPeriod(ctx, a), reason, key, adminID); err != nil {
		return nil, internal(err)
	}
	return s.AdminGet(ctx, agencyID)
}

// AdminAttachClient brings a workspace under the agency for an operator. Its
// owner must already be on the agency's staff: attaching hands the staff the
// workspace, so it is never one whose owner has nothing to do with the agency.
func (s *Service) AdminAttachClient(ctx context.Context, agencyID, orgID uuid.UUID) (*AdminAgencyDetail, *errx.Error) {
	a, xerr := s.adminLoad(ctx, agencyID)
	if xerr != nil {
		return nil, xerr
	}
	org, xerr := s.orgs.Get(ctx, orgID)
	if xerr != nil || org == nil {
		return nil, errx.NewWithIdentifier(errx.NotFound, "organization_not_found", "That workspace does not exist.")
	}
	if m, err := s.repo.GetMember(ctx, agencyID, org.OwnerUserID); err != nil {
		return nil, internal(err)
	} else if m == nil {
		return nil, errx.NewWithIdentifier(errx.Forbidden, "agency_client_owner_not_staff",
			"The workspace's owner is not on this agency's staff. Add them to the agency first, or have them attach the workspace themselves.")
	}
	if sub, err := s.subs.GetByOrganizationID(ctx, orgID); err != nil {
		return nil, internal(err)
	} else if ownsPaidStripePlan(sub) {
		return nil, errx.NewWithIdentifier(errx.Conflict, "agency_client_has_subscription",
			"This workspace pays for its own plan. It has to be cancelled first, so it is not billed twice.")
	}
	if _, xerr := s.attach(ctx, a, orgID, a.OwnerUserID); xerr != nil {
		return nil, xerr
	}
	return s.AdminGet(ctx, agencyID)
}

// AdminDetachClient releases a workspace from the agency: it keeps its data and loses the agency's plan and staff.
func (s *Service) AdminDetachClient(ctx context.Context, agencyID, orgID uuid.UUID) (*AdminAgencyDetail, *errx.Error) {
	removed, err := s.repo.RemoveClient(ctx, agencyID, orgID)
	if err != nil {
		return nil, internal(err)
	}
	if !removed {
		return nil, errx.NewWithIdentifier(errx.NotFound, "agency_client_not_found", "That workspace is not one of this agency's clients.")
	}
	s.released(ctx, agencyID, orgID)
	return s.AdminGet(ctx, agencyID)
}

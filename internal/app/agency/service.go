// Package agency runs agencies: staff, client workspaces, the shared monthly
// send pool and the agency's own billing.
package agency

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/repository"
)

// MaxOwnedAgencies bounds how many agencies one account may own.
const MaxOwnedAgencies = 5

// MaxClientMonthlyLimit bounds a per-client monthly cap.
const MaxClientMonthlyLimit = 100_000_000

// Organizations is the slice of the organization service the agency needs.
type Organizations interface {
	Create(ctx context.Context, userID uuid.UUID, name, timezone string) (*models.Organization, *errx.Error)
	CreateAgencyClient(ctx context.Context, userID uuid.UUID, name, timezone string) (*models.Organization, *errx.Error)
	Get(ctx context.Context, orgID uuid.UUID) (*models.Organization, *errx.Error)
}

// Users looks up accounts.
type Users interface {
	GetUser(ctx context.Context, id uuid.UUID) (*models.User, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
}

// Subscriptions reads a workspace's own subscription.
type Subscriptions interface {
	GetByOrganizationID(ctx context.Context, orgID uuid.UUID) (*models.Subscription, error)
}

// Plans reads plan rows.
type Plans interface {
	GetByID(ctx context.Context, id uuid.UUID) (*models.Plan, error)
	GetByStripePriceID(ctx context.Context, priceID string) (*models.Plan, error)
	List(ctx context.Context, publicOnly bool) ([]*models.Plan, error)
}

// Notifier raises in-app notifications.
type Notifier interface {
	Notify(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any)
}

type Service struct {
	repo     repository.AgencyRepository
	orgs     Organizations
	users    Users
	subs     Subscriptions
	plans    Plans
	notifier Notifier
	egress   IsolatedEgress
	ai       repository.AgencyAIRepository
	// aiMetered is false where AI is not charged, so the shared AI balance does not apply.
	aiMetered bool
	removals  []func(ctx context.Context, orgID, userID uuid.UUID) error
	// billing is true when Stripe bills agencies; false on a self-host.
	billing bool
	now     func() time.Time

	// switches are the locks plan switches take, picked by agency id.
	switches [64]sync.Mutex

	mu        sync.Mutex
	pools     map[uuid.UUID]poolEntry
	orgAgency map[uuid.UUID]orgEntry
}

func NewService(repo repository.AgencyRepository, orgs Organizations, users Users, subs Subscriptions, plans Plans) *Service {
	return &Service{
		repo: repo, orgs: orgs, users: users, subs: subs, plans: plans,
		billing:   config.BillingProvider() == "stripe",
		now:       time.Now,
		pools:     map[uuid.UUID]poolEntry{},
		orgAgency: map[uuid.UUID]orgEntry{},
	}
}

// WireNotifier attaches pool alerts.
func (s *Service) WireNotifier(n Notifier) { s.notifier = n }

// WireMemberRemoval runs fn after an agency-added membership ends, like the organization service's hook.
func (s *Service) WireMemberRemoval(fn func(ctx context.Context, orgID, userID uuid.UUID) error) {
	s.removals = append(s.removals, fn)
}

// BillingEnabled reports whether agencies are billed on this instance.
func (s *Service) BillingEnabled() bool { return s.billing }

var (
	errNotFound  = errx.NewWithIdentifier(errx.NotFound, "agency_not_found", "That agency does not exist or you are not part of it.")
	errForbidden = errx.NewWithIdentifier(errx.Forbidden, "agency_forbidden", "Only the agency's owner and admins can do that.")
)

func internal(err error) *errx.Error {
	errs.CaptureException(err)
	return errx.InternalError()
}

// access loads the agency and the caller's role, refusing anyone not on its staff.
func (s *Service) access(ctx context.Context, agencyID, userID uuid.UUID, manage bool) (*models.Agency, *models.AgencyMember, *errx.Error) {
	m, err := s.repo.GetMember(ctx, agencyID, userID)
	if err != nil {
		return nil, nil, internal(err)
	}
	if m == nil {
		return nil, nil, errNotFound
	}
	if manage && !m.Role.CanManage() {
		return nil, nil, errForbidden
	}
	a, err := s.repo.Get(ctx, agencyID)
	if err != nil {
		return nil, nil, internal(err)
	}
	if a == nil {
		return nil, nil, errNotFound
	}
	a.Role = m.Role
	s.hydrate(ctx, a)
	return a, m, nil
}

// hydrate joins the plan in force, the operator's while it stands in for the subscription's.
func (s *Service) hydrate(ctx context.Context, a *models.Agency) {
	now := s.now()
	a.BillingInterval = a.SubscriptionInterval()
	if a.Granted(now) {
		a.Grant = &models.AgencyGrant{PlanID: *a.ManagedPlanID, Until: a.ManagedUntil}
	}
	planID := a.EffectivePlanID(now)
	if planID == nil || s.plans == nil {
		return
	}
	if p, err := s.plans.GetByID(ctx, *planID); err == nil {
		a.Plan = p
	}
}

// ListForUser claims invitations sent to the account's address, then lists its agencies.
func (s *Service) ListForUser(ctx context.Context, userID uuid.UUID) ([]models.Agency, *errx.Error) {
	if u, err := s.users.GetUser(ctx, userID); err == nil && u != nil && u.Email != "" {
		joined, cerr := s.repo.ClaimInvitations(ctx, userID, u.Email)
		if cerr != nil {
			errs.CaptureException(cerr)
		}
		for _, agencyID := range joined {
			if m, merr := s.repo.GetMember(ctx, agencyID, userID); merr == nil && m != nil {
				s.mirrorEverywhere(ctx, agencyID, userID, m.Role, userID)
			}
		}
	}
	list, err := s.repo.ListAgenciesForUser(ctx, userID)
	if err != nil {
		return nil, internal(err)
	}
	for i := range list {
		s.hydrate(ctx, &list[i])
	}
	if list == nil {
		list = []models.Agency{}
	}
	return list, nil
}

func (s *Service) Get(ctx context.Context, agencyID, userID uuid.UUID) (*models.Agency, *errx.Error) {
	a, _, xerr := s.access(ctx, agencyID, userID, false)
	return a, xerr
}

// Create opens an agency owned by the caller.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, name string) (*models.Agency, *errx.Error) {
	name, xerr := displayname.Validate("Agency name", name, displayname.Workspace, false)
	if xerr != nil {
		return nil, xerr
	}
	mine, err := s.repo.ListAgenciesForUser(ctx, userID)
	if err != nil {
		return nil, internal(err)
	}
	owned := 0
	for _, a := range mine {
		if a.Role == models.AgencyRoleOwner {
			owned++
		}
	}
	if owned >= MaxOwnedAgencies {
		return nil, errx.NewWithIdentifier(errx.Forbidden, "agency_limit_reached", "You already own the most agencies one account can.")
	}
	a := &models.Agency{ID: uuid.New(), Name: name, OwnerUserID: userID, Status: models.SubscriptionStatusIncomplete}
	if err := s.repo.Create(ctx, a); err != nil {
		return nil, internal(err)
	}
	a.Role = models.AgencyRoleOwner
	return a, nil
}

func (s *Service) Rename(ctx context.Context, agencyID, userID uuid.UUID, name string) (*models.Agency, *errx.Error) {
	if _, _, xerr := s.access(ctx, agencyID, userID, true); xerr != nil {
		return nil, xerr
	}
	name, xerr := displayname.Validate("Agency name", name, displayname.Workspace, false)
	if xerr != nil {
		return nil, xerr
	}
	if err := s.repo.Rename(ctx, agencyID, name); err != nil {
		return nil, internal(err)
	}
	s.forgetAgency(agencyID)
	return s.Get(ctx, agencyID, userID)
}

// Staff lists the agency's staff and pending invitations.
func (s *Service) Staff(ctx context.Context, agencyID, userID uuid.UUID) ([]models.AgencyMember, []models.AgencyInvitation, *errx.Error) {
	if _, _, xerr := s.access(ctx, agencyID, userID, false); xerr != nil {
		return nil, nil, xerr
	}
	members, err := s.repo.ListMembers(ctx, agencyID)
	if err != nil {
		return nil, nil, internal(err)
	}
	invites, err := s.repo.ListInvitations(ctx, agencyID)
	if err != nil {
		return nil, nil, internal(err)
	}
	if members == nil {
		members = []models.AgencyMember{}
	}
	if invites == nil {
		invites = []models.AgencyInvitation{}
	}
	return members, invites, nil
}

// AddStaff adds an existing account as staff, or invites an address no account uses yet.
func (s *Service) AddStaff(ctx context.Context, agencyID, actorID uuid.UUID, email string, role models.AgencyRole) (*models.AgencyMember, *models.AgencyInvitation, *errx.Error) {
	_, actor, xerr := s.access(ctx, agencyID, actorID, true)
	if xerr != nil {
		return nil, nil, xerr
	}
	if xerr := checkAssignable(actor.Role, role); xerr != nil {
		return nil, nil, xerr
	}
	addr, err := mail.ParseAddress(strings.TrimSpace(email))
	if err != nil || addr.Address != strings.TrimSpace(email) {
		return nil, nil, errx.NewWithIdentifier(errx.BadRequest, "invalid_email", "Enter a valid email address.")
	}
	u, err := s.users.GetUserByEmail(ctx, addr.Address)
	if err != nil && !errors.Is(err, errx.ErrUser) {
		return nil, nil, internal(err)
	}
	if u == nil {
		inv := &models.AgencyInvitation{ID: uuid.New(), AgencyID: agencyID, Email: strings.ToLower(addr.Address),
			Role: role, InvitedBy: &actorID, ExpiresAt: s.now().Add(models.AgencyInvitationTTL)}
		if err := s.repo.UpsertInvitation(ctx, inv); err != nil {
			return nil, nil, internal(err)
		}
		return nil, inv, nil
	}
	if existing, err := s.repo.GetMember(ctx, agencyID, u.ID); err != nil {
		return nil, nil, internal(err)
	} else if existing != nil && existing.Role == models.AgencyRoleOwner {
		return existing, nil, nil
	} else if existing != nil && !canChange(actor.Role, existing.Role) {
		return nil, nil, errForbidden
	}
	if err := s.repo.UpsertMember(ctx, agencyID, u.ID, role); err != nil {
		return nil, nil, internal(err)
	}
	s.mirrorEverywhere(ctx, agencyID, u.ID, role, actorID)
	m, err := s.repo.GetMember(ctx, agencyID, u.ID)
	if err != nil {
		return nil, nil, internal(err)
	}
	return m, nil, nil
}

// checkAssignable: owners assign admin or member, admins assign member only.
func checkAssignable(actor, target models.AgencyRole) *errx.Error {
	if target != models.AgencyRoleAdmin && target != models.AgencyRoleMember {
		return errx.NewWithIdentifier(errx.BadRequest, "invalid_agency_role", "Role must be admin or member.")
	}
	if target == models.AgencyRoleAdmin && actor != models.AgencyRoleOwner {
		return errx.NewWithIdentifier(errx.Forbidden, "agency_forbidden", "Only the agency owner can make someone an admin.")
	}
	return nil
}

// canChange reports whether actor may change or remove a staff member holding target.
func canChange(actor, target models.AgencyRole) bool {
	switch {
	case target == models.AgencyRoleOwner:
		return false
	case actor == models.AgencyRoleOwner:
		return true
	case actor == models.AgencyRoleAdmin:
		return target == models.AgencyRoleMember
	}
	return false
}

func (s *Service) UpdateStaffRole(ctx context.Context, agencyID, actorID, userID uuid.UUID, role models.AgencyRole) (*models.AgencyMember, *errx.Error) {
	_, actor, xerr := s.access(ctx, agencyID, actorID, true)
	if xerr != nil {
		return nil, xerr
	}
	target, err := s.repo.GetMember(ctx, agencyID, userID)
	if err != nil {
		return nil, internal(err)
	}
	if target == nil {
		return nil, errx.NewWithIdentifier(errx.NotFound, "agency_staff_not_found", "That person is not on the agency's staff.")
	}
	if !canChange(actor.Role, target.Role) {
		return nil, errForbidden
	}
	if xerr := checkAssignable(actor.Role, role); xerr != nil {
		return nil, xerr
	}
	if err := s.repo.UpsertMember(ctx, agencyID, userID, role); err != nil {
		return nil, internal(err)
	}
	s.mirrorEverywhere(ctx, agencyID, userID, role, actorID)
	target.Role = role
	return target, nil
}

// RemoveStaff takes a person off the staff and out of every client workspace the agency added them to. Staff may remove themselves.
func (s *Service) RemoveStaff(ctx context.Context, agencyID, actorID, userID uuid.UUID) *errx.Error {
	_, actor, xerr := s.access(ctx, agencyID, actorID, actorID != userID)
	if xerr != nil {
		return xerr
	}
	target, err := s.repo.GetMember(ctx, agencyID, userID)
	if err != nil {
		return internal(err)
	}
	if target == nil {
		return nil
	}
	if target.Role == models.AgencyRoleOwner {
		return errx.NewWithIdentifier(errx.Forbidden, "agency_owner_fixed", "The agency owner cannot be removed.")
	}
	if actorID != userID && !canChange(actor.Role, target.Role) {
		return errForbidden
	}
	if err := s.repo.RemoveMember(ctx, agencyID, userID); err != nil {
		return internal(err)
	}
	clients, err := s.repo.ListClientOrganizations(ctx, agencyID)
	if err != nil {
		return internal(err)
	}
	for _, c := range clients {
		s.unmirror(ctx, agencyID, c.OrganizationID, userID)
	}
	return nil
}

func (s *Service) CancelInvitation(ctx context.Context, agencyID, actorID, invitationID uuid.UUID) *errx.Error {
	if _, _, xerr := s.access(ctx, agencyID, actorID, true); xerr != nil {
		return xerr
	}
	if _, err := s.repo.DeleteInvitation(ctx, agencyID, invitationID); err != nil {
		return internal(err)
	}
	return nil
}

// mirrorEverywhere gives a staff member their mapped role in every client workspace.
func (s *Service) mirrorEverywhere(ctx context.Context, agencyID, userID uuid.UUID, role models.AgencyRole, by uuid.UUID) {
	clients, err := s.repo.ListClientOrganizations(ctx, agencyID)
	if err != nil {
		errs.CaptureException(err)
		return
	}
	for _, c := range clients {
		if err := s.repo.MirrorStaff(ctx, agencyID, c.OrganizationID, userID, role, by); err != nil {
			errs.CaptureException(err)
		}
	}
}

// mirrorStaffInto gives every staff member their mapped role in one client workspace.
func (s *Service) mirrorStaffInto(ctx context.Context, agencyID, orgID, by uuid.UUID) *errx.Error {
	staff, err := s.repo.ListMembers(ctx, agencyID)
	if err != nil {
		return internal(err)
	}
	for _, m := range staff {
		if err := s.repo.MirrorStaff(ctx, agencyID, orgID, m.UserID, m.Role, by); err != nil {
			return internal(err)
		}
	}
	return nil
}

func (s *Service) unmirror(ctx context.Context, agencyID, orgID, userID uuid.UUID) {
	removed, err := s.repo.UnmirrorStaff(ctx, agencyID, orgID, userID)
	if err != nil {
		errs.CaptureException(err)
		return
	}
	if !removed {
		return
	}
	hookCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	for _, fn := range s.removals {
		if err := fn(hookCtx, orgID, userID); err != nil {
			errs.CaptureException(err)
		}
	}
}

// Clients lists the agency's client workspaces.
func (s *Service) Clients(ctx context.Context, agencyID, userID uuid.UUID) ([]models.AgencyClient, *errx.Error) {
	if _, _, xerr := s.access(ctx, agencyID, userID, false); xerr != nil {
		return nil, xerr
	}
	list, err := s.repo.ListClientOrganizations(ctx, agencyID)
	if err != nil {
		return nil, internal(err)
	}
	if list == nil {
		list = []models.AgencyClient{}
	}
	return list, nil
}

// CreateClient opens a new workspace owned by the agency owner and attaches it.
func (s *Service) CreateClient(ctx context.Context, agencyID, actorID uuid.UUID, name, timezone string) (*models.AgencyClient, *errx.Error) {
	a, _, xerr := s.access(ctx, agencyID, actorID, true)
	if xerr != nil {
		return nil, xerr
	}
	// Only an agency whose plan is in force is exempt from the owner's workspace cap.
	create := s.orgs.Create
	if _, _, ok := s.grantPlan(a); ok || !s.billing {
		create = s.orgs.CreateAgencyClient
	}
	org, xerr := create(ctx, a.OwnerUserID, name, timezone)
	if xerr != nil {
		return nil, xerr
	}
	return s.attach(ctx, a, org.ID, actorID)
}

// AttachClient brings a workspace the caller owns under the agency.
func (s *Service) AttachClient(ctx context.Context, agencyID, actorID, orgID uuid.UUID) (*models.AgencyClient, *errx.Error) {
	a, _, xerr := s.access(ctx, agencyID, actorID, true)
	if xerr != nil {
		return nil, xerr
	}
	org, xerr := s.orgs.Get(ctx, orgID)
	if xerr != nil {
		return nil, xerr
	}
	if org == nil || org.OwnerUserID != actorID {
		return nil, errx.NewWithIdentifier(errx.Forbidden, "agency_client_not_owner", "Only the workspace's owner can bring it into an agency.")
	}
	if sub, err := s.subs.GetByOrganizationID(ctx, orgID); err != nil {
		return nil, internal(err)
	} else if ownsPaidStripePlan(sub) {
		return nil, errx.NewWithIdentifier(errx.Conflict, "agency_client_has_subscription",
			"This workspace pays for its own plan. Cancel it in the workspace's billing settings first, so it is not billed twice.")
	}
	return s.attach(ctx, a, orgID, actorID)
}

func ownsPaidStripePlan(sub *models.Subscription) bool {
	return sub != nil && sub.StripeSubscriptionID != nil && stripeWillCharge(sub.Status) && !sub.CancelAtPeriodEnd
}

// stripeWillCharge is a subscription Stripe still bills or retries, so a second one would bill twice.
func stripeWillCharge(st models.SubscriptionStatus) bool {
	switch st {
	case models.SubscriptionStatusActive, models.SubscriptionStatusTrialing, models.SubscriptionStatusPastDue,
		models.SubscriptionStatusUnpaid, models.SubscriptionStatusPaused:
		return true
	}
	return false
}

func (s *Service) attach(ctx context.Context, a *models.Agency, orgID, actorID uuid.UUID) (*models.AgencyClient, *errx.Error) {
	if err := s.repo.AddClient(ctx, a.ID, orgID, actorID); err != nil {
		if errors.Is(err, repository.ErrAgencyClientTaken) {
			return nil, errx.NewWithIdentifier(errx.Conflict, "agency_client_taken", "That workspace already belongs to an agency.")
		}
		return nil, internal(err)
	}
	s.forgetOrg(orgID)
	s.forgetAgency(a.ID)
	if xerr := s.mirrorStaffInto(ctx, a.ID, orgID, actorID); xerr != nil {
		return nil, xerr
	}
	if err := s.syncGrant(ctx, a, orgID); err != nil {
		return nil, internal(err)
	}
	c, err := s.repo.GetClient(ctx, orgID)
	if err != nil {
		return nil, internal(err)
	}
	return c, nil
}

// DetachClient releases a workspace: it keeps its data and members it added
// itself, and loses the agency's plan and staff.
func (s *Service) DetachClient(ctx context.Context, agencyID, actorID, orgID uuid.UUID) *errx.Error {
	if _, _, xerr := s.access(ctx, agencyID, actorID, true); xerr != nil {
		return xerr
	}
	removed, err := s.repo.RemoveClient(ctx, agencyID, orgID)
	if err != nil {
		return internal(err)
	}
	if !removed {
		return errx.NewWithIdentifier(errx.NotFound, "agency_client_not_found", "That workspace is not one of this agency's clients.")
	}
	s.released(ctx, agencyID, orgID)
	return nil
}

// released takes back what the agency gave a workspace it no longer holds: its
// grant, its reserved worker and the memberships the agency added.
func (s *Service) released(ctx context.Context, agencyID, orgID uuid.UUID) {
	s.forgetOrg(orgID)
	s.forgetAgency(agencyID)
	if err := s.repo.ClearClientGrant(ctx, agencyID, orgID); err != nil {
		errs.CaptureException(err)
	}
	s.syncEgress(ctx, orgID)
	staff, err := s.repo.AgencyMembersOf(ctx, agencyID, orgID)
	if err != nil {
		errs.CaptureException(err)
	}
	for _, userID := range staff {
		s.unmirror(ctx, agencyID, orgID, userID)
	}
}

// LeaveAgency takes a workspace out of its agency from the workspace side.
// Only its owner may, and not when they own the agency too: that owner
// detaches it from the agency. Nil agency means it belonged to none.
func (s *Service) LeaveAgency(ctx context.Context, orgID, userID uuid.UUID) (*models.AgencyRef, *errx.Error) {
	org, xerr := s.orgs.Get(ctx, orgID)
	if xerr != nil {
		return nil, xerr
	}
	if org == nil || org.OwnerUserID != userID {
		return nil, errx.NewWithIdentifier(errx.Forbidden, "agency_leave_not_owner", "Only the workspace's owner can take it out of its agency.")
	}
	a, err := s.repo.GetAgencyForOrganization(ctx, orgID)
	if err != nil {
		return nil, internal(err)
	}
	if a == nil {
		return nil, nil
	}
	if a.OwnerUserID == org.OwnerUserID {
		return nil, errx.NewWithIdentifier(errx.Conflict, "agency_leave_agency_owner",
			"You own this workspace's agency. Remove the workspace under Agency > Settings > Clients instead.")
	}
	removed, err := s.repo.RemoveClient(ctx, a.ID, orgID)
	if err != nil {
		return nil, internal(err)
	}
	if removed {
		s.released(ctx, a.ID, orgID)
	}
	return &models.AgencyRef{ID: a.ID, Name: a.Name}, nil
}

// SetClientLimit caps one client's share of the pool; nil removes the cap.
func checkSendLimit(limit *int) *errx.Error {
	if limit != nil && (*limit < 0 || *limit > MaxClientMonthlyLimit) {
		return errx.NewWithIdentifier(errx.BadRequest, "invalid_monthly_send_limit", "The monthly limit must be between 0 and 100,000,000 sends.")
	}
	return nil
}

func (s *Service) SetClientLimit(ctx context.Context, agencyID, actorID, orgID uuid.UUID, limit *int) (*models.AgencyClient, *errx.Error) {
	if _, _, xerr := s.access(ctx, agencyID, actorID, true); xerr != nil {
		return nil, xerr
	}
	if xerr := checkSendLimit(limit); xerr != nil {
		return nil, xerr
	}
	ok, err := s.repo.SetClientLimit(ctx, agencyID, orgID, limit)
	if err != nil {
		return nil, internal(err)
	}
	if !ok {
		return nil, errClientNotFound
	}
	s.forgetOrg(orgID)
	c, err := s.repo.GetClient(ctx, orgID)
	if err != nil {
		return nil, internal(err)
	}
	return c, nil
}

// AgencyOf names the agency a workspace belongs to, for the dashboard.
func (s *Service) AgencyOf(ctx context.Context, orgID uuid.UUID) *models.AgencyRef {
	a, err := s.repo.GetAgencyForOrganization(ctx, orgID)
	if err != nil {
		errs.CaptureException(err)
		return nil
	}
	if a == nil {
		return nil
	}
	ref := &models.AgencyRef{ID: a.ID, Name: a.Name}
	if c, err := s.repo.GetClient(ctx, orgID); err != nil {
		errs.CaptureException(err)
	} else if c != nil {
		ref.CanLeave = c.OwnerUserID != a.OwnerUserID
	}
	return ref
}

// Plans lists the agency plans this instance sells.
func (s *Service) Plans(ctx context.Context) ([]*models.Plan, *errx.Error) {
	all, err := s.plans.List(ctx, false)
	if err != nil {
		return nil, internal(err)
	}
	out := []*models.Plan{}
	for _, p := range all {
		if p.IsAgencyPlan() {
			out = append(out, p)
		}
	}
	return out, nil
}

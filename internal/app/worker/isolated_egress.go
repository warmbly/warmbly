package worker

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

// egressStep is what converging an organization's reservation on its plan does next.
type egressStep int

const (
	egressKeep egressStep = iota
	egressReserve
	egressRelease
)

// isolatedEgressStep reserves only once the organization has a mailbox, so an empty workspace holds no worker out of the fleet.
func isolatedEgressStep(entitled, reserved bool, mailboxes int) egressStep {
	switch {
	case entitled && !reserved && mailboxes > 0:
		return egressReserve
	case !entitled && reserved:
		return egressRelease
	}
	return egressKeep
}

// isolatedEgressPlanID is the plan that can hold a reservation: a grant in force, else a subscription that has not ended.
func isolatedEgressPlanID(sub *models.Subscription) (uuid.UUID, bool) {
	switch {
	case sub == nil:
		return uuid.Nil, false
	case sub.IsManaged():
		return sub.EffectivePlanID(), true
	case sub.IsEnterprise:
		return sub.PlanID, true
	case sub.StripeSubscriptionID != nil &&
		sub.Status != models.SubscriptionStatusCanceled &&
		sub.Status != models.SubscriptionStatusIncompleteExpired:
		return sub.PlanID, true
	}
	return uuid.Nil, false
}

// isolatedEgressEntitlement reports whether the org's plan reserves a worker. Unlike hasIsolatedEgress it returns lookup errors, because a release must never follow a failed read.
func (s *workerAssignmentService) isolatedEgressEntitlement(ctx context.Context, orgID uuid.UUID) (bool, uuid.UUID, error) {
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil || sub == nil {
		return false, uuid.Nil, err
	}
	planID, ok := isolatedEgressPlanID(sub)
	if !ok {
		return false, sub.ID, nil
	}
	plan, err := s.planRepo.GetByID(ctx, planID)
	if err != nil {
		return false, sub.ID, err
	}
	if !plan.IsolatedEgress() {
		return false, sub.ID, nil
	}
	// Fair use: an agency's grant reserves for its oldest clients only; the rest keep the placement preference.
	if sub.IsManaged() && sub.ManagedByAgencyID != nil {
		ok, err := s.workerRepo.AgencyEgressSlot(ctx, *sub.ManagedByAgencyID, orgID, config.AgencyIsolatedEgressMaxClients)
		return ok, sub.ID, err
	}
	return true, sub.ID, nil
}

// SyncIsolatedEgress converges an organization's reserved worker on its plan,
// for entitlements no Stripe plan change announces: agency and operator grants.
func (s *workerAssignmentService) SyncIsolatedEgress(ctx context.Context, orgID uuid.UUID) error {
	return s.syncIsolatedEgress(ctx, orgID, true)
}

func (s *workerAssignmentService) syncIsolatedEgress(ctx context.Context, orgID uuid.UUID, allowRelease bool) error {
	// No billing means no plans to entitle anything, so reservations stay as the operator left them.
	if config.BillingProvider() == "none" || s.subRepo == nil || s.planRepo == nil {
		return nil
	}
	entitled, subID, err := s.isolatedEgressEntitlement(ctx, orgID)
	if err != nil {
		return err
	}
	reserved, err := s.workerRepo.GetActiveDedicatedAssignment(ctx, orgID)
	if err != nil {
		return err
	}
	mailboxes := 0
	if entitled && reserved == nil {
		if mailboxes, err = s.workerRepo.CountOrgMailboxes(ctx, orgID); err != nil {
			return err
		}
	}
	switch isolatedEgressStep(entitled, reserved != nil, mailboxes) {
	case egressReserve:
		err := s.ReserveIsolatedWorker(ctx, orgID, subID)
		switch {
		case errors.Is(err, ErrOrgAlreadyReserved):
			return nil
		case errors.Is(err, ErrNoIdleWorkers):
			// Placement still prefers workers without other tenants; the next sync or placement retries.
			log.Info().Str("org_id", orgID.String()).Msg("placement: no idle worker to reserve for isolated egress")
			return nil
		}
		return err
	case egressRelease:
		if !allowRelease {
			return nil
		}
		// Release the row read above, so a reservation made meanwhile is never touched.
		released, err := s.workerRepo.ReleaseDedicatedAssignmentByID(ctx, reserved.ID)
		if released {
			log.Info().Str("worker_id", reserved.WorkerID.String()).Str("org_id", orgID.String()).
				Msg("placement: released isolated-egress worker the plan no longer includes")
		}
		return err
	}
	return nil
}

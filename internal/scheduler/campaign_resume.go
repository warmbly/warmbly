package scheduler

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// NoAccountsResumer is implemented by the scheduler; the reconciler asks it before reactivating a parked campaign.
type NoAccountsResumer interface {
	CanResumeNoAccounts(ctx context.Context, campaignID uuid.UUID) (bool, error)
}

// CanResumeNoAccounts reports whether a campaign parked for having no usable mailbox has one again: active, not failing domain authentication, with a working day.
func (s *schedulerService) CanResumeNoAccounts(ctx context.Context, campaignID uuid.UUID) (bool, error) {
	campaign, err := s.campaignRepo.GetByID(ctx, campaignID)
	if err != nil || campaign == nil || campaign.Status != "paused_no_accounts" {
		return false, err
	}
	accounts, _, err := s.campaignSenders(ctx, campaign)
	if err != nil || len(accounts) == 0 {
		return false, err
	}
	pass := s.newCampaignPass(ctx, campaign, accounts)
	now := time.Now()
	for _, acct := range accounts {
		if acct.Status != "active" {
			continue
		}
		if pass.enforceAuth && acct.DomainAuthBlocked(now, pass.authGrace) {
			continue
		}
		if bhv := pass.behaviors[acct.ID]; bhv.Enabled {
			if _, ok := s.placeWithinBehavior(ctx, bhv, now); !ok {
				continue
			}
		}
		return true, nil
	}
	return false, nil
}

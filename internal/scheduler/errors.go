package scheduler

import (
	"errors"
	"fmt"
)

var (
	// ErrWarmupNotEnabled is returned when warmup is not enabled for an account
	ErrWarmupNotEnabled = errors.New("warmup not enabled for this account")

	// ErrCampaignNotActive is returned when a campaign is not active
	ErrCampaignNotActive = errors.New("campaign is not active")

	// ErrCampaignCompleted is returned when all emails in a campaign have been sent
	ErrCampaignCompleted = errors.New("campaign completed - no more emails to send")

	// ErrCampaignEnded is returned when a campaign has passed its end date
	ErrCampaignEnded = errors.New("campaign ended - past end date")

	// ErrNoEmailAccounts is returned when no email accounts are available for sending
	ErrNoEmailAccounts = errors.New("no email accounts available for this campaign")

	// ErrNoEligibleMailbox is the narrower case: the campaign HAS mailboxes,
	// but every one was gated out for both today and tomorrow (daily cap
	// reached, warmup health, or outside its own sending window). Reporting
	// that as ErrNoEmailAccounts sent people looking at their tag configuration
	// for a problem that was never there.
	//
	// It wraps ErrNoEmailAccounts so existing callers that pause the campaign
	// on errors.Is(err, ErrNoEmailAccounts) keep behaving exactly as before.
	ErrNoEligibleMailbox = fmt.Errorf(
		"%w: every mailbox is outside its sending window or over its daily budget", ErrNoEmailAccounts)

	// ErrDomainAuthFailing is the narrower case again: every mailbox in the
	// campaign's pool was gated by the sending-domain authentication check.
	// Reporting that as ErrNoEligibleMailbox would send people to check
	// timezones and daily caps for a DNS problem, which is exactly the class of
	// mislabelling ErrNoEligibleMailbox was introduced to fix.
	//
	// It wraps ErrNoEmailAccounts so existing callers that pause the campaign
	// keep behaving as before; callers that want the specific reason must test
	// for it BEFORE ErrNoEligibleMailbox and ErrNoEmailAccounts.
	ErrDomainAuthFailing = fmt.Errorf(
		"%w: every mailbox is sending from a domain that fails SPF/DMARC authentication", ErrNoEmailAccounts)

	// ErrDailyLimitReached is returned when the daily limit has been reached
	ErrDailyLimitReached = errors.New("daily email limit reached")

	// ErrCampaignDeferred is returned when there IS a valid contact to send but
	// no eligible mailbox right now — ESP-strict has no same-provider mailbox
	// under budget, or the daily new-lead cap is reached. The caller must
	// reschedule at the returned (defer) time WITHOUT sending. The returned pair
	// is always nil on this path so it can never be mistaken for a sendable
	// contact; the returned accountID is a nominal pool mailbox for the wakeup
	// task only (the next invocation re-evaluates selection from scratch).
	ErrCampaignDeferred = errors.New("campaign send deferred - no eligible mailbox for this contact right now")
)

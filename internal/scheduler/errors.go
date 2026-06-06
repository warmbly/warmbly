package scheduler

import "errors"

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

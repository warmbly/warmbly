package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

// CampaignContactProgress represents the progress of a contact in a campaign
type CampaignContactProgress struct {
	CampaignID   uuid.UUID
	ContactID    uuid.UUID
	SequenceID   uuid.UUID
	SentAt       *time.Time
	OpenedAt     *time.Time
	ClickedAt    *time.Time
	RepliedAt    *time.Time
	BouncedAt    *time.Time
	ComplainedAt *time.Time
	// ReplyClass is the layered classifier verdict for the contact's reply
	// (positive | negative | neutral | auto_reply | out_of_office | unsubscribe |
	// unknown; "" when no reply was classified). Read by the reply_* branch
	// conditions. RepliedAt is set ONLY for human replies, so an automated reply
	// can carry a ReplyClass here without ever tripping "replied"/stop_on_reply.
	ReplyClass string
	// AILabel is the case a "switch" sequence step stored for the contact on this
	// step ("" when the step has no labels or the AI could not decide). Read by
	// the ai_label branch conditions.
	AILabel string
}

// CampaignProgress represents overall campaign progress
type CampaignProgress struct {
	TotalContacts    int
	TotalSequences   int
	EmailsSent       int
	EmailsPending    int
	EmailsOpened     int
	EmailsClicked    int
	EmailsReplied    int
	EmailsBounced    int
	EmailsComplained int
}

// CampaignRollingRates holds windowed send/bounce/complaint counts for a
// campaign, used by the deliverability circuit breaker so it reacts to recent
// behaviour rather than a campaign's lifetime average.
type CampaignRollingRates struct {
	Sent       int
	Bounced    int
	Complained int
}

// ContactSequencePair represents a contact and sequence combination
type ContactSequencePair struct {
	ContactID  uuid.UUID
	SequenceID uuid.UUID
	// IsNewLead is true when this pair is the contact's first step (sequence
	// position 1). Drives the per-day new-lead counter and cap.
	IsNewLead bool
	// NotBefore is the earliest instant routing allows the step: the entry
	// delay for a new lead, the previous step's wait for a follow-up. Nil means
	// "due now". The placer floors its base time with it, so a wait routing
	// honours can never be dropped by the placement pass.
	NotBefore *time.Time
	// AssignedSender is the mailbox this lead's sequence is bound to, set when
	// its first email was reserved. The placer sends from it rather than
	// rotating, so every step of one conversation comes from one address.
	AssignedSender *uuid.UUID
}

type CampaignSequencePair struct {
	CampaignID uuid.UUID
	SequenceID uuid.UUID
}

// StuckDispatch is a send that was reserved and handed to a worker but whose
// outcome never came back: no EMAIL_SENT stamped it, no EMAIL_FAILED walked it
// back. The reclaimer resolves these.
type StuckDispatch struct {
	CampaignID   uuid.UUID
	ContactID    uuid.UUID
	SequenceID   uuid.UUID
	TaskID       *uuid.UUID
	DispatchedAt time.Time
}

// CampaignProgressRepository defines methods for campaign progress tracking
type CampaignProgressRepository interface {
	// ReserveSend claims (campaign, contact, step) for one send BEFORE the
	// command goes on the bus, which is what lets a later tick tell "dispatched,
	// outcome unknown" apart from "never attempted". It stamps dispatched_at and
	// counts the send against the day's counters in one transaction, and reports
	// false when the step is already in flight or already sent — in which case
	// the caller must NOT dispatch. Resolve every reservation exactly once:
	// RecordEmailSent on a successful hand-off, ReleaseSend when the command
	// provably never left, RecordSendFailure on a worker failure.
	//
	// senderID binds the lead to the mailbox this send goes out from, in the
	// same transaction, so the rest of its sequence follows the address the
	// contact has already seen. Pass uuid.Nil to leave the binding alone.
	ReserveSend(ctx context.Context, campaignID, contactID, sequenceID, taskID, senderID uuid.UUID, newLead bool) (bool, error)
	// ReleaseSend gives a reservation back when the send provably never reached
	// the bus (no worker assigned, worker offline), so the step is retried on the
	// next tick without spending an attempt. Only an unstamped reservation is
	// touched, so it can never undo a real send. A lead whose binding to a
	// mailbox came from this reservation alone is unbound again, so a mailbox
	// that never actually wrote to the contact cannot keep their sequence.
	ReleaseSend(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID, newLead bool) error
	// Record email status
	RecordEmailSent(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error
	// StampDispatchedSend is the repair path for a send whose reservation was
	// written but whose sent_at stamp was lost (the control plane died, or its
	// write failed, between the dispatch and the stamp). The worker's own
	// EMAIL_SENT calls it, so a delivered email always ends up with the timing
	// stamp follow-up pacing reads. Returns true when it actually repaired one.
	StampDispatchedSend(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) (bool, error)
	// ListStuckDispatches returns reservations older than olderThan that no
	// worker result ever resolved.
	ListStuckDispatches(ctx context.Context, olderThan time.Duration, limit int) ([]StuckDispatch, error)
	// RecordSendFailure walks back a step the worker could not send: the
	// reservation and sent_at are cleared so routing offers the step again, and
	// the attempt is counted. It returns the attempts so far and whether the
	// lead has now exhausted
	// config.CampaignSendMaxAttempts. rolledBack is false when there was
	// nothing to walk back (a duplicate result, or the send was already retried).
	// A lead with nothing else delivered is unbound from the mailbox that
	// failed, so rotation can offer it a working one.
	RecordSendFailure(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID, reason string) (attempts int, exhausted bool, rolledBack bool, err error)
	// LastSenderForLead is the mailbox a lead was LAST actually sent from,
	// read from the campaign tasks that dispatched its steps. It answers the
	// case campaign_leads.email_account_id cannot: a lead removed from the
	// campaign and added back keeps every step it was sent but starts a new
	// row, and the address it last heard from is still the one to keep. Nil
	// when nothing was ever sent to it in this campaign.
	LastSenderForLead(ctx context.Context, campaignID, contactID uuid.UUID) (*uuid.UUID, error)
	// HasSentSteps reports whether the contact has any other step of the
	// campaign stamped sent, which is what decides if a failed send was the
	// lead's first step (a "new lead" for the daily new-lead counter).
	HasSentSteps(ctx context.Context, campaignID, contactID uuid.UUID) (bool, error)
	RecordEmailOpened(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID, machine bool) error
	RecordEmailClicked(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error
	// UnrecordEmailClicked clears clicked_at when every logged click on the
	// step turned out to be automated (a burst recognised after the first
	// click already stamped it). clicked_at keeps meaning "a person clicked".
	UnrecordEmailClicked(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error
	// GetStepSentAt returns when the step was dispatched (nil when it was not),
	// the reference point for telling an instant machine open or click from a
	// person's.
	GetStepSentAt(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) (*time.Time, error)
	RecordEmailReplied(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error
	RecordEmailBounced(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error
	RecordEmailComplained(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error

	// RecordReplyClassification stores the layered classifier verdict
	// (class/confidence/source) for the contact's reply on the given step. This
	// is the data the reply_* branch conditions read. It does NOT stamp
	// replied_at — only a HUMAN reply should set replied_at (so automated replies
	// never trip stop_on_reply / the "replied" condition). Callers stamp
	// replied_at separately via RecordEmailReplied for human replies only.
	RecordReplyClassification(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID, class, source string, confidence float64) error
	// RecordAILabel stores the case a "switch" sequence step chose for the contact
	// on that step. Upserts (the AI step runs before its progress row is stamped
	// sent). Read by the ai_label branch conditions when routing out of the step.
	RecordAILabel(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID, label string) error
	// GetLatestReplyClass returns the most-recent classified reply class for a
	// contact in a campaign ("" when none). Convenience getter for the branch
	// evaluator / callers that need only the class.
	GetLatestReplyClass(ctx context.Context, contactID, campaignID uuid.UUID) (string, error)

	// GetResolvedAIVariables returns the per-recipient AI variable text already
	// generated for this (campaign, contact, step), keyed by variable id (empty
	// map when the row or column is empty). The send path reads this first so a
	// task redelivery reuses cached copy instead of re-generating and re-charging.
	GetResolvedAIVariables(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) (map[string]string, error)
	// SaveResolvedAIVariable upserts one resolved AI variable (varID -> text) into
	// the ai_variables_resolved jsonb, creating the progress row if it is missing
	// (the AI resolve runs before the row is stamped sent), mirroring RecordAILabel.
	SaveResolvedAIVariable(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID, varID, text string) error

	// ClaimInstantFire atomically claims the one-time right to run the instant
	// action chain for the contact's current step FOR A SINGLE EVENT KIND
	// ("reply" / "open" / "click"). It appends eventKind to the instant_fired
	// array only when that kind is not already present and reports whether THIS
	// call won the claim (true) or that kind had already fired (false). This is
	// the per-(step, event) exactly-once gate behind instant automations: a
	// redelivered event of the same kind (or an auto-reply followed by a human
	// reply on the same step) sees the kind in the array and is a no-op, while a
	// DIFFERENT kind on the same step (a contact who opens AND clicks AND replies)
	// is still free to fire its own chain exactly once. The progress row already
	// exists at call time (RecordReplyClassification / RecordEmailOpened /
	// RecordEmailClicked upsert/update it just before), so a missing row
	// (claimed == false) safely means "nothing to fire".
	ClaimInstantFire(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID, eventKind string) (bool, error)

	// Query methods
	GetCampaignProgress(ctx context.Context, campaignID uuid.UUID) (*CampaignProgress, error)
	GetCampaignRollingRates(ctx context.Context, campaignID uuid.UUID, since time.Time) (*CampaignRollingRates, error)
	GetContactProgress(ctx context.Context, campaignID, contactID uuid.UUID) ([]CampaignContactProgress, error)
	GetContactLastSequenceTime(ctx context.Context, contactID, campaignID uuid.UUID) (*time.Time, error)
	CheckContactHasReplied(ctx context.Context, contactID, campaignID uuid.UUID) (bool, error)
	CountEmailsSentTodayByOrganization(ctx context.Context, organizationID uuid.UUID) (int, error)
	GetLatestCampaignSequenceForContact(ctx context.Context, contactID uuid.UUID) (*CampaignSequencePair, error)

	// FindRoutedPairs selects the (contact, step) pairs that are due now by
	// following each contact's step rules (the branching tree) rather than a
	// flat position order, in routing order, up to limit of them.
	// prioritizeNewLeads sorts first-step pairs first; excludeNewLeads drops
	// first-step pairs entirely so the new-lead/day cap can be enforced while
	// follow-ups keep flowing. The second return value, when NO pair is due, is
	// the soonest time a waiting contact's condition window elapses — the
	// scheduler should defer and re-check then rather than completing.
	// paced names the pool mailboxes that cannot take a send right now but will
	// be able to on their own, mapped to when; a lead already bound to one is
	// skipped for this pass and reported through the next-due time instead of
	// parking every lead behind it. Nil applies no sender gate.
	//
	// More than one pair is returned because placement can refuse a SINGLE
	// lead (ESP-strict finds no same-provider mailbox, the lead's own mailbox
	// is busy, the recipient's preferred hours are hours away) and the leads
	// behind them are still sendable. One pair and one refusal used to park the
	// whole campaign.
	//
	// waitingOnSender reports that the returned next-due moment is a lead
	// waiting for its own mailbox rather than for a step's wait or a condition
	// window, so the caller can say which.
	FindRoutedPairs(ctx context.Context, campaignID uuid.UUID, orderBy, orderDir, orderField string, prioritizeNewLeads, excludeNewLeads bool, paced PacedSenders, limit int) (pairs []ContactSequencePair, nextDue *time.Time, waitingOnSender bool, err error)
	// RouteContact runs the same routing for ONE contact and reports where
	// their flow goes next, plus the pre-send gate that excludes them, so a
	// per-contact preview reads the facts the send path reads.
	RouteContact(ctx context.Context, campaignID, contactID uuid.UUID) (*ContactRoute, error)

	// CountUndeliverableLeads counts the leads FindRoutedPairs excludes
	// because address verification refused them. Reported when a campaign
	// finishes, so "completed" never silently means "skipped everybody".
	CountUndeliverableLeads(ctx context.Context, campaignID uuid.UUID) (int, error)
}

type campaignProgressRepository struct {
	db *pgxpool.Pool
}

// NewCampaignProgressRepository creates a new campaign progress repository
func NewCampaignProgressRepository(db *pgxpool.Pool) CampaignProgressRepository {
	return &campaignProgressRepository{db: db}
}

// ReserveSend claims the step for one send before the command is published.
// The claim and the day's counters move together because both must survive a
// crash in the dispatch window: a reservation without its count would let the
// daily cap over-send, and a count without its reservation would charge for a
// send nobody ever made.
//
// The ON CONFLICT ... WHERE is the whole exactly-once gate: a row already in
// flight (dispatched_at set) or already sent updates nothing and returns no
// row, so two ticks that picked the same pair cannot both dispatch. A step
// walked back after a worker failure has both cleared and is claimable again.
func (r *campaignProgressRepository) ReserveSend(ctx context.Context, campaignID, contactID, sequenceID, taskID, senderID uuid.UUID, newLead bool) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var claimed bool
	err = tx.QueryRow(ctx, `
		INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, dispatched_at, dispatch_task_id)
		VALUES ($1, $2, $3, NOW(), $4)
		ON CONFLICT (campaign_id, contact_id, sequence_id)
		DO UPDATE SET dispatched_at = NOW(), dispatch_task_id = $4, failed_at = NULL, failure_reason = ''
		WHERE campaign_contact_progress.sent_at IS NULL
		  AND campaign_contact_progress.dispatched_at IS NULL
		RETURNING true
	`, campaignID, contactID, sequenceID, taskID).Scan(&claimed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	// Bind the lead to the mailbox this send leaves from. Written with the
	// claim, so the binding exists for every send that was ever dispatched and
	// for none that was not. Re-stated on every step: the placer only hands a
	// different mailbox when the bound one is gone, and that decision is what
	// this records.
	if senderID != uuid.Nil {
		if _, err := tx.Exec(ctx, `
			UPDATE campaign_leads
			SET email_account_id = $3, sender_assigned_at = NOW()
			WHERE campaign_id = $1 AND contact_id = $2
			  AND email_account_id IS DISTINCT FROM $3
		`, campaignID, contactID, senderID); err != nil {
			return false, err
		}
	}

	newLeadInc := 0
	if newLead {
		newLeadInc = 1
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO campaign_daily_sends (campaign_id, send_date, emails_sent, new_leads_started)
		VALUES ($1, CURRENT_DATE, 1, $2)
		ON CONFLICT (campaign_id, send_date)
		DO UPDATE SET emails_sent = campaign_daily_sends.emails_sent + 1,
		              new_leads_started = campaign_daily_sends.new_leads_started + $2
	`, campaignID, newLeadInc); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// ReleaseSend undoes a reservation whose command never left, counters included.
// Guarded on sent_at IS NULL so a reservation that was stamped in the meantime
// (the worker answered first) is never released.
func (r *campaignProgressRepository) ReleaseSend(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID, newLead bool) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var released bool
	err = tx.QueryRow(ctx, `
		UPDATE campaign_contact_progress
		SET dispatched_at = NULL, dispatch_task_id = NULL
		WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3
		  AND sent_at IS NULL AND dispatched_at IS NOT NULL
		RETURNING true
	`, campaignID, contactID, sequenceID).Scan(&released)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}

	// The mailbox never wrote to this contact, so it must not keep their
	// sequence: a lead bound by this reservation alone is unbound again, and
	// the next tick picks by rotation as it would for any new lead. A lead with
	// an earlier delivered step keeps its binding.
	if _, err := tx.Exec(ctx, `
		UPDATE campaign_leads cl
		SET email_account_id = NULL, sender_assigned_at = NULL
		WHERE cl.campaign_id = $1 AND cl.contact_id = $2
		  AND cl.email_account_id IS NOT NULL
		  AND NOT EXISTS (
		    SELECT 1 FROM campaign_contact_progress p
		    WHERE p.campaign_id = $1 AND p.contact_id = $2 AND p.sent_at IS NOT NULL
		  )
	`, campaignID, contactID); err != nil {
		return err
	}

	newLeadDec := 0
	if newLead {
		newLeadDec = 1
	}
	if _, err := tx.Exec(ctx, `
		UPDATE campaign_daily_sends
		SET emails_sent = GREATEST(emails_sent - 1, 0),
		    new_leads_started = GREATEST(new_leads_started - $2, 0)
		WHERE campaign_id = $1 AND send_date = CURRENT_DATE
	`, campaignID, newLeadDec); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RecordEmailSent stamps a step as handed to the worker. A retry after a
// worker-reported failure clears the failure marker again; send_attempts is
// kept as history so the retry cap still holds. dispatched_at is backfilled for
// callers that stamp without reserving first (action nodes, instant chains),
// so "attempted" is never narrower than "sent".
func (r *campaignProgressRepository) RecordEmailSent(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error {
	query := `
		INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at, dispatched_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		ON CONFLICT (campaign_id, contact_id, sequence_id)
		DO UPDATE SET sent_at = NOW(),
		              dispatched_at = COALESCE(campaign_contact_progress.dispatched_at, NOW()),
		              failed_at = NULL, failure_reason = ''
	`

	_, err := r.db.Exec(ctx, query, campaignID, contactID, sequenceID)
	return err
}

// StampDispatchedSend stamps a reservation the control plane never got to
// stamp itself. Guarded on dispatched_at so it can only ever complete a send
// somebody really reserved, and on sent_at so it never moves a stamp that is
// already there.
func (r *campaignProgressRepository) StampDispatchedSend(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE campaign_contact_progress
		SET sent_at = NOW(), failed_at = NULL, failure_reason = ''
		WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3
		  AND sent_at IS NULL AND dispatched_at IS NOT NULL
	`, campaignID, contactID, sequenceID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ListStuckDispatches returns reservations no worker result ever resolved,
// oldest first.
func (r *campaignProgressRepository) ListStuckDispatches(ctx context.Context, olderThan time.Duration, limit int) ([]StuckDispatch, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.Query(ctx, `
		SELECT campaign_id, contact_id, sequence_id, dispatch_task_id, dispatched_at
		FROM campaign_contact_progress
		WHERE sent_at IS NULL
		  AND dispatched_at IS NOT NULL
		  AND dispatched_at < NOW() - make_interval(secs => $1)
		ORDER BY dispatched_at ASC
		LIMIT $2
	`, olderThan.Seconds(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StuckDispatch
	for rows.Next() {
		var s StuckDispatch
		if err := rows.Scan(&s.CampaignID, &s.ContactID, &s.SequenceID, &s.TaskID, &s.DispatchedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// RecordSendFailure clears the reservation and sent_at on a step and counts the
// attempt. Only a row that is currently in flight or stamped sent is touched,
// so a duplicate worker result after the step was already walked back (or
// re-sent) is a no-op.
func (r *campaignProgressRepository) RecordSendFailure(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID, reason string) (int, bool, bool, error) {
	if len(reason) > 500 {
		reason = reason[:500]
	}
	// The unbind rides along in the same statement so a failure can never leave
	// a lead pinned to a mailbox that delivered it nothing. Its NOT EXISTS reads
	// the pre-statement snapshot, so the step being walked back is excluded by
	// hand — otherwise a step this call is clearing would still count as
	// delivered and hold the binding.
	query := `
		WITH walked AS (
			UPDATE campaign_contact_progress
			SET sent_at = NULL,
			    dispatched_at = NULL,
			    dispatch_task_id = NULL,
			    send_attempts = send_attempts + 1,
			    failed_at = NOW(),
			    failure_reason = $4
			WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3
			  AND (sent_at IS NOT NULL OR dispatched_at IS NOT NULL)
			RETURNING send_attempts
		), unbound AS (
			UPDATE campaign_leads cl
			SET email_account_id = NULL, sender_assigned_at = NULL
			WHERE EXISTS (SELECT 1 FROM walked)
			  AND cl.campaign_id = $1 AND cl.contact_id = $2
			  AND cl.email_account_id IS NOT NULL
			  AND NOT EXISTS (
			    SELECT 1 FROM campaign_contact_progress p
			    WHERE p.campaign_id = $1 AND p.contact_id = $2
			      AND p.sequence_id <> $3 AND p.sent_at IS NOT NULL
			  )
		)
		SELECT send_attempts FROM walked
	`
	var attempts int
	err := r.db.QueryRow(ctx, query, campaignID, contactID, sequenceID, reason).Scan(&attempts)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, false, nil
		}
		return 0, false, false, err
	}
	return attempts, attempts >= config.CampaignSendMaxAttempts, true, nil
}

// LastSenderForLead reads the mailbox of the most recent campaign task that
// dispatched a step to this lead. Only completed email tasks carry a contact
// id (action nodes return before the tracking stamp), so this is exactly the
// set of sends the contact actually received.
func (r *campaignProgressRepository) LastSenderForLead(ctx context.Context, campaignID, contactID uuid.UUID) (*uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `
		SELECT t.email_account_id
		FROM campaign_tasks ct
		JOIN tasks t ON t.id = ct.task_id
		WHERE ct.campaign_id = $1 AND ct.contact_id = $2
		  AND t.task_type = 'campaign' AND t.status = 'completed'
		ORDER BY t.created_at DESC
		LIMIT 1
	`, campaignID, contactID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// HasSentSteps reports whether any step of the campaign is stamped sent for
// the contact.
func (r *campaignProgressRepository) HasSentSteps(ctx context.Context, campaignID, contactID uuid.UUID) (bool, error) {
	var has bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM campaign_contact_progress
			WHERE campaign_id = $1 AND contact_id = $2 AND sent_at IS NOT NULL
		)
	`, campaignID, contactID).Scan(&has)
	return has, err
}

// RecordEmailOpened records that an email was opened. machine marks automated
// fetches (Apple MPP prefetch, UA-less clients): the first open stamps
// opened_at with the flag, and a later HUMAN open upgrades a machine open to
// human (keeping the original timestamp). Human opens are never downgraded.
func (r *campaignProgressRepository) RecordEmailOpened(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID, machine bool) error {
	query := `
		UPDATE campaign_contact_progress
		SET opened_at = COALESCE(opened_at, NOW()),
		    opened_machine = $4
		WHERE campaign_id = $1
		  AND contact_id = $2
		  AND sequence_id = $3
		  AND (opened_at IS NULL OR (opened_machine = true AND $4 = false))
	`

	_, err := r.db.Exec(ctx, query, campaignID, contactID, sequenceID, machine)
	return err
}

// RecordEmailClicked records that an email link was clicked
// RecordEmailClicked stamps a person's click. It also counts as an open:
// the person had the email in front of them whatever the pixel saw, so a
// client that blocks images no longer reads "clicked, not opened". The
// implied open shares the click's timestamp, which is how UnrecordEmailClicked
// tells it from a pixel open.
func (r *campaignProgressRepository) RecordEmailClicked(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error {
	query := `
		UPDATE campaign_contact_progress
		SET clicked_at = NOW(),
		    opened_at = COALESCE(opened_at, NOW()),
		    opened_machine = false
		WHERE campaign_id = $1
		  AND contact_id = $2
		  AND sequence_id = $3
		  AND clicked_at IS NULL
	`

	_, err := r.db.Exec(ctx, query, campaignID, contactID, sequenceID)
	return err
}

// UnrecordEmailClicked walks a click stamp back once no human click remains
// on the step. Guarded by the click log so a concurrent human click is never
// erased, and only a stamp written alongside a logged click is touched: a
// stamp older than the step's earliest logged click predates per-link
// logging, so it came from a person the log never saw.
func (r *campaignProgressRepository) UnrecordEmailClicked(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error {
	query := `
		UPDATE campaign_contact_progress ccp
		SET clicked_at = NULL,
		    opened_at = CASE
		        WHEN ccp.opened_at = ccp.clicked_at AND NOT EXISTS (
		            SELECT 1 FROM email_opens o
		            WHERE o.campaign_id = $1 AND o.contact_id = $2 AND o.sequence_id = $3 AND o.machine = false
		        ) THEN NULL
		        ELSE ccp.opened_at
		    END
		WHERE ccp.campaign_id = $1
		  AND ccp.contact_id = $2
		  AND ccp.sequence_id = $3
		  AND ccp.clicked_at IS NOT NULL
		  AND ccp.clicked_at >= (
			SELECT MIN(lc.clicked_at) - INTERVAL '1 minute' FROM email_link_clicks lc
			WHERE lc.campaign_id = $1 AND lc.contact_id = $2 AND lc.sequence_id = $3
		  )
		  AND NOT EXISTS (
			SELECT 1 FROM email_link_clicks lc
			WHERE lc.campaign_id = $1 AND lc.contact_id = $2 AND lc.sequence_id = $3 AND lc.machine = false
		  )
	`

	_, err := r.db.Exec(ctx, query, campaignID, contactID, sequenceID)
	return err
}

// GetStepSentAt returns the step's dispatch time, or nil when unsent/unknown.
func (r *campaignProgressRepository) GetStepSentAt(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) (*time.Time, error) {
	query := `
		SELECT LEAST(dispatched_at, sent_at)
		FROM campaign_contact_progress
		WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3
	`

	var sentAt *time.Time
	err := r.db.QueryRow(ctx, query, campaignID, contactID, sequenceID).Scan(&sentAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return sentAt, nil
}

// RecordEmailReplied records that a contact replied
func (r *campaignProgressRepository) RecordEmailReplied(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error {
	query := `
		UPDATE campaign_contact_progress
		SET replied_at = NOW()
		WHERE campaign_id = $1
		  AND contact_id = $2
		  AND sequence_id = $3
		  AND replied_at IS NULL
	`

	_, err := r.db.Exec(ctx, query, campaignID, contactID, sequenceID)
	return err
}

// RecordEmailBounced records that an email bounced
func (r *campaignProgressRepository) RecordEmailBounced(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error {
	query := `
		UPDATE campaign_contact_progress
		SET bounced_at = NOW()
		WHERE campaign_id = $1
		  AND contact_id = $2
		  AND sequence_id = $3
		  AND bounced_at IS NULL
	`

	_, err := r.db.Exec(ctx, query, campaignID, contactID, sequenceID)
	return err
}

// RecordEmailComplained records that a contact filed a spam complaint
func (r *campaignProgressRepository) RecordEmailComplained(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error {
	query := `
		UPDATE campaign_contact_progress
		SET complained_at = NOW()
		WHERE campaign_id = $1
		  AND contact_id = $2
		  AND sequence_id = $3
		  AND complained_at IS NULL
	`

	_, err := r.db.Exec(ctx, query, campaignID, contactID, sequenceID)
	return err
}

// RecordReplyClassification persists the classifier verdict on the progress row.
// It upserts so the classification lands even if the reply arrives before the
// step's progress row is materialized (rare, but threading can race). It never
// touches replied_at — human-vs-automated gating lives in the caller, which
// stamps replied_at via RecordEmailReplied only for human replies.
func (r *campaignProgressRepository) RecordReplyClassification(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID, class, source string, confidence float64) error {
	query := `
		INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, reply_class, reply_confidence, reply_source)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (campaign_id, contact_id, sequence_id)
		DO UPDATE SET reply_class = EXCLUDED.reply_class,
		              reply_confidence = EXCLUDED.reply_confidence,
		              reply_source = EXCLUDED.reply_source
	`
	_, err := r.db.Exec(ctx, query, campaignID, contactID, sequenceID, class, confidence, source)
	return err
}

// RecordAILabel persists an AI step's chosen label on the progress row.
func (r *campaignProgressRepository) RecordAILabel(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID, label string) error {
	query := `
		INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, ai_label)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (campaign_id, contact_id, sequence_id)
		DO UPDATE SET ai_label = EXCLUDED.ai_label
	`
	_, err := r.db.Exec(ctx, query, campaignID, contactID, sequenceID, label)
	return err
}

// GetResolvedAIVariables reads the ai_variables_resolved jsonb for the row and
// decodes it into a var-id -> text map. A missing row or empty column yields an
// empty (non-nil) map, never an error.
func (r *campaignProgressRepository) GetResolvedAIVariables(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) (map[string]string, error) {
	query := `
		SELECT COALESCE(ai_variables_resolved, '{}'::jsonb)
		FROM campaign_contact_progress
		WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3
	`
	var raw []byte
	err := r.db.QueryRow(ctx, query, campaignID, contactID, sequenceID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	if len(raw) > 0 {
		if uerr := json.Unmarshal(raw, &out); uerr != nil {
			return map[string]string{}, nil
		}
	}
	return out, nil
}

// SaveResolvedAIVariable upserts one key into ai_variables_resolved. It inserts
// the progress row (jsonb built from the single key) when absent — the AI
// resolve runs before the row is stamped sent — and otherwise merges the key in
// with jsonb_set, mirroring RecordAILabel's missing-row handling.
func (r *campaignProgressRepository) SaveResolvedAIVariable(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID, varID, text string) error {
	query := `
		INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, ai_variables_resolved)
		VALUES ($1, $2, $3, jsonb_build_object($4::text, $5::text))
		ON CONFLICT (campaign_id, contact_id, sequence_id)
		DO UPDATE SET ai_variables_resolved =
			COALESCE(campaign_contact_progress.ai_variables_resolved, '{}'::jsonb)
			|| jsonb_build_object($4::text, $5::text)
	`
	_, err := r.db.Exec(ctx, query, campaignID, contactID, sequenceID, varID, text)
	return err
}

// GetLatestReplyClass returns the most-recent non-empty reply_class for a
// contact in a campaign, or "" when none has been classified.
func (r *campaignProgressRepository) GetLatestReplyClass(ctx context.Context, contactID, campaignID uuid.UUID) (string, error) {
	query := `
		SELECT reply_class
		FROM campaign_contact_progress
		WHERE contact_id = $1 AND campaign_id = $2 AND reply_class <> ''
		ORDER BY COALESCE(sent_at, '-infinity'::timestamptz) DESC
		LIMIT 1
	`
	var class string
	err := r.db.QueryRow(ctx, query, contactID, campaignID).Scan(&class)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return class, err
}

// ClaimInstantFire appends eventKind to instant_fired exactly once per (campaign,
// contact, step, eventKind). The conditional NOT ($4 = ANY(instant_fired)) makes
// the claim atomic under concurrent events of the same kind: at most one UPDATE
// affects a row for a given kind, so RowsAffected() == 1 identifies the single
// caller that may run that kind's chain. Different kinds on the same step append
// independently, so an open, a click, and a reply on one step each fire once.
func (r *campaignProgressRepository) ClaimInstantFire(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID, eventKind string) (bool, error) {
	query := `
		UPDATE campaign_contact_progress
		SET instant_fired = array_append(instant_fired, $4)
		WHERE campaign_id = $1
		  AND contact_id = $2
		  AND sequence_id = $3
		  AND NOT ($4 = ANY(instant_fired))
	`
	tag, err := r.db.Exec(ctx, query, campaignID, contactID, sequenceID, eventKind)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// GetCampaignProgress retrieves overall campaign progress statistics
func (r *campaignProgressRepository) GetCampaignProgress(ctx context.Context, campaignID uuid.UUID) (*CampaignProgress, error) {
	query := `
		WITH campaign_stats AS (
			SELECT
				COUNT(DISTINCT cl.contact_id) as total_contacts,
				COUNT(DISTINCT s.id) as total_sequences,
				COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) as emails_sent,
				COUNT(CASE WHEN ccp.opened_at IS NOT NULL THEN 1 END) as emails_opened,
				COUNT(CASE WHEN ccp.clicked_at IS NOT NULL THEN 1 END) as emails_clicked,
				COUNT(CASE WHEN ccp.replied_at IS NOT NULL THEN 1 END) as emails_replied,
				COUNT(CASE WHEN ccp.bounced_at IS NOT NULL THEN 1 END) as emails_bounced,
				COUNT(CASE WHEN ccp.complained_at IS NOT NULL THEN 1 END) as emails_complained
			FROM campaigns c
			LEFT JOIN campaign_leads cl ON c.id = cl.campaign_id
			LEFT JOIN sequences s ON c.id = s.campaign_id
			LEFT JOIN campaign_contact_progress ccp ON c.id = ccp.campaign_id
			WHERE c.id = $1
			GROUP BY c.id
		)
		SELECT
			total_contacts,
			total_sequences,
			emails_sent,
			(total_contacts * total_sequences) - emails_sent as emails_pending,
			emails_opened,
			emails_clicked,
			emails_replied,
			emails_bounced,
			emails_complained
		FROM campaign_stats
	`

	progress := &CampaignProgress{}
	err := r.db.QueryRow(ctx, query, campaignID).Scan(
		&progress.TotalContacts,
		&progress.TotalSequences,
		&progress.EmailsSent,
		&progress.EmailsPending,
		&progress.EmailsOpened,
		&progress.EmailsClicked,
		&progress.EmailsReplied,
		&progress.EmailsBounced,
		&progress.EmailsComplained,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return &CampaignProgress{}, nil
	}

	return progress, err
}

// GetCampaignRollingRates returns send/bounce/complaint counts for a campaign
// within the window [since, now], computed from the per-contact progress
// timestamps so the breaker can react to recent behaviour.
func (r *campaignProgressRepository) GetCampaignRollingRates(ctx context.Context, campaignID uuid.UUID, since time.Time) (*CampaignRollingRates, error) {
	query := `
		SELECT
			COUNT(*) FILTER (WHERE sent_at IS NOT NULL AND sent_at >= $2)             AS sent,
			COUNT(*) FILTER (WHERE bounced_at IS NOT NULL AND bounced_at >= $2)       AS bounced,
			COUNT(*) FILTER (WHERE complained_at IS NOT NULL AND complained_at >= $2) AS complained
		FROM campaign_contact_progress
		WHERE campaign_id = $1
	`
	out := &CampaignRollingRates{}
	err := r.db.QueryRow(ctx, query, campaignID, since).Scan(&out.Sent, &out.Bounced, &out.Complained)
	if errors.Is(err, sql.ErrNoRows) {
		return &CampaignRollingRates{}, nil
	}
	return out, err
}

// GetContactProgress retrieves progress for a specific contact in a campaign.
// A machine open comes back as NULL: this feeds routing and instant actions,
// and an automated fetch is not intent (machine clicks never stamp at all).
func (r *campaignProgressRepository) GetContactProgress(ctx context.Context, campaignID, contactID uuid.UUID) ([]CampaignContactProgress, error) {
	query := `
		SELECT campaign_id, contact_id, sequence_id, sent_at,
		       CASE WHEN opened_machine THEN NULL ELSE opened_at END,
		       clicked_at, replied_at, bounced_at, complained_at, COALESCE(reply_class, ''), COALESCE(ai_label, '')
		FROM campaign_contact_progress
		WHERE campaign_id = $1 AND contact_id = $2
		ORDER BY sent_at ASC
	`

	rows, err := r.db.Query(ctx, query, campaignID, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var progressList []CampaignContactProgress
	for rows.Next() {
		progress := CampaignContactProgress{}
		err := rows.Scan(
			&progress.CampaignID,
			&progress.ContactID,
			&progress.SequenceID,
			&progress.SentAt,
			&progress.OpenedAt,
			&progress.ClickedAt,
			&progress.RepliedAt,
			&progress.BouncedAt,
			&progress.ComplainedAt,
			&progress.ReplyClass,
			&progress.AILabel,
		)
		if err != nil {
			return nil, err
		}
		progressList = append(progressList, progress)
	}

	return progressList, rows.Err()
}

// GetContactLastSequenceTime retrieves the last email sent time for a contact
func (r *campaignProgressRepository) GetContactLastSequenceTime(ctx context.Context, contactID, campaignID uuid.UUID) (*time.Time, error) {
	query := `
		SELECT MAX(sent_at)
		FROM campaign_contact_progress
		WHERE contact_id = $1 AND campaign_id = $2
	`

	var lastTime *time.Time
	err := r.db.QueryRow(ctx, query, contactID, campaignID).Scan(&lastTime)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return lastTime, err
}

// CheckContactHasReplied checks if a contact has replied to any email in the campaign
func (r *campaignProgressRepository) CheckContactHasReplied(ctx context.Context, contactID, campaignID uuid.UUID) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1
			FROM campaign_contact_progress
			WHERE contact_id = $1
			  AND campaign_id = $2
			  AND replied_at IS NOT NULL
		)
	`

	var hasReplied bool
	err := r.db.QueryRow(ctx, query, contactID, campaignID).Scan(&hasReplied)
	return hasReplied, err
}

// CountEmailsSentTodayByOrganization returns how many campaign emails were sent
// today by an organization. Action and wait steps stamp sent_at too, for
// routing, but send nothing, so only email steps count.
func (r *campaignProgressRepository) CountEmailsSentTodayByOrganization(ctx context.Context, organizationID uuid.UUID) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM campaign_contact_progress ccp
		JOIN campaigns c ON c.id = ccp.campaign_id
		JOIN sequences s ON s.id = ccp.sequence_id AND s.kind = 'email'
		WHERE c.organization_id = $1
		  AND ccp.sent_at IS NOT NULL
		  AND DATE(ccp.sent_at) = CURRENT_DATE
	`

	var count int
	err := r.db.QueryRow(ctx, query, organizationID).Scan(&count)
	return count, err
}

func (r *campaignProgressRepository) GetLatestCampaignSequenceForContact(ctx context.Context, contactID uuid.UUID) (*CampaignSequencePair, error) {
	query := `
		SELECT campaign_id, sequence_id
		FROM campaign_contact_progress
		WHERE contact_id = $1
		  AND sent_at IS NOT NULL
		ORDER BY sent_at DESC
		LIMIT 1
	`
	out := &CampaignSequencePair{}
	if err := r.db.QueryRow(ctx, query, contactID).Scan(&out.CampaignID, &out.SequenceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return out, nil
}

// undeliverableClause is the ONE definition of "the campaign will never send to
// this lead": address verification marked it invalid, or marked it risky while
// the campaign's "send to risky emails" toggle is off. Routing excludes these
// (see FindRoutedPairs), the campaign task's pre-send gates refuse them, and
// the Leads view reports them as undeliverable, so all three read the same rule
// rather than three copies that can drift apart.
//
// cp is the SQL placeholder holding the campaign id. The caller must alias
// contacts as `c`.
func undeliverableClause(cp string) string {
	return fmt.Sprintf(
		"(c.verification_status = 'invalid' OR (c.verification_status = 'risky' "+
			"AND NOT COALESCE((SELECT rc.risky_emails FROM campaigns rc WHERE rc.id = %[1]s), true)))",
		cp,
	)
}

// FindRoutedPairs selects the (contact, step) pairs to send by FOLLOWING THE
// FLOW graph. For each contact, the next step is the route out of their
// last-sent step:
//  1. conditional branches (first match wins, evaluated against engagement),
//  2. then the explicit "else" catch-all branch (empty conditions; target nil = STOP).
//
// There is no implicit advance by position: a step with no outgoing connection
// ends the contact's flow. The campaign wizard connects the steps it creates
// in order, and the canvas connects steps as they are dragged.
//
// A contact who has never been sent starts at the entry step (position 1). A
// step is sent only if the route reaches it, so branch-only steps are never sent
// linearly, and a routed step that was already ATTEMPTED (a loop) stops the
// contact. Attempted means sent_at OR dispatched_at: a step reserved before its
// command went on the bus counts, even if the sent_at stamp was lost, which is
// what stops a crash or a failed progress write in that window from handing the
// same email to a worker twice.
// A contact whose step failed in the worker more than CampaignSendMaxAttempts
// times is dropped, the same way a bounced or suppressed contact is. So is a
// contact the pre-send gates would refuse: an address verification marked
// invalid, or marked risky while the campaign's "send to risky" toggle is off.
// Those gates skip WITHOUT recording progress, so leaving such a contact in the
// candidate set wedges the whole campaign on it forever (issue #200).
//
// Conditions are evaluated SEND-RELATIVE with a three-valued result: a contact
// whose next step isn't decidable yet (an engagement window still open) is not
// returned.
//
// Only pairs that are DUE are returned: a new lead is due once the campaign's
// entry delay has elapsed since they entered it (immediately when there is
// none); a routed step is due wait_after days after the contact's last step
// (plus a wait node's minutes). The first `limit` due contacts in list order
// win, in order, so the caller can move on to the next one when placement
// refuses the one in front.
// Contacts whose next step is due later never block the ones behind them: without this, the one lead
// routed to a "wait 3 days" follow-up parks the whole campaign for 3 days while
// every other lead's first email sits queued. When nothing is due, the second
// value is the soonest moment something will be (a step becoming due or a
// condition window closing) so the scheduler defers exactly until then.
// A lead already bound to a mailbox (its first email went out from it) is only
// offered while that mailbox can take a send: one that is merely out of budget
// for today or outside its hours is listed in `paced`, and the lead waits for
// it rather than switching addresses mid-conversation. Its reopening is
// reported as a next-due time, so the leads behind it keep sending and the
// campaign defers instead of completing when they are all waiting. A mailbox
// that is NOT coming back on its own is deliberately absent from `paced`: the
// lead is offered, and the placer moves it to another mailbox.
//
// Returns no pairs and a nil next-due when the campaign is genuinely complete.
func (r *campaignProgressRepository) FindRoutedPairs(ctx context.Context, campaignID uuid.UUID, orderBy, orderDir, orderField string, prioritizeNewLeads, excludeNewLeads bool, paced PacedSenders, limit int) ([]ContactSequencePair, *time.Time, bool, error) {
	if limit < 1 {
		limit = 1
	}
	router, err := r.loadRouter(ctx, campaignID)
	if err != nil {
		return nil, nil, false, err
	}
	if router == nil {
		return nil, nil, false, nil
	}

	// 2. Ordered candidate contacts + their last-sent step (with engagement) + sent set.
	var contactOrder string
	switch orderBy {
	case "email":
		contactOrder = "c.email"
	case "name":
		contactOrder = "c.first_name, c.last_name"
	case "custom_field":
		if orderField != "" {
			contactOrder = "c.custom_fields->>'" + orderField + "'"
		} else {
			contactOrder = "c.created_at"
		}
	case "manual":
		contactOrder = "cl.position NULLS LAST, c.created_at"
	default:
		contactOrder = "c.created_at"
	}
	dir := "ASC"
	if orderDir == "desc" {
		dir = "DESC"
	}
	orderPrefix := ""
	if prioritizeNewLeads {
		// New leads (no last-sent step) first.
		orderPrefix = "(lp.sequence_id IS NULL) DESC, "
	}

	query := `
		SELECT cl.contact_id, cl.added_at, cl.email_account_id,
		       lp.sequence_id, lp.sent_at, lp.opened_at, lp.clicked_at, lp.replied_at, COALESCE(lp.reply_class, ''), COALESCE(lp.ai_label, ''),
		       COALESCE(ss.ids, '{}') AS sent_ids,
		       EXISTS (
		         SELECT 1 FROM campaign_contact_progress rp
		         WHERE rp.campaign_id = $1 AND rp.contact_id = cl.contact_id AND rp.replied_at IS NOT NULL
		       ) AS has_replied
		FROM campaign_leads cl
		JOIN contacts c ON c.id = cl.contact_id
		LEFT JOIN LATERAL (
			SELECT sequence_id, sent_at,
			       CASE WHEN p.opened_machine THEN NULL ELSE p.opened_at END AS opened_at,
			       clicked_at, replied_at, reply_class, ai_label
			FROM campaign_contact_progress p
			WHERE p.campaign_id = $1 AND p.contact_id = cl.contact_id AND p.sent_at IS NOT NULL
			ORDER BY p.sent_at DESC LIMIT 1
		) lp ON true
		LEFT JOIN LATERAL (
			SELECT array_agg(sequence_id) AS ids
			FROM campaign_contact_progress p2
			WHERE p2.campaign_id = $1 AND p2.contact_id = cl.contact_id
			  AND (p2.sent_at IS NOT NULL OR p2.dispatched_at IS NOT NULL)
		) ss ON true
		WHERE cl.campaign_id = $1
		  AND NOT EXISTS (
		    SELECT 1 FROM campaign_contact_progress b
		    WHERE b.contact_id = cl.contact_id AND b.bounced_at IS NOT NULL
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM campaign_contact_progress f
		    WHERE f.campaign_id = $1 AND f.contact_id = cl.contact_id
		      AND f.sent_at IS NULL AND f.failed_at IS NOT NULL
		      AND f.send_attempts >= $2
		  )
		  -- The workspace suppression list (addresses and domains) and the
		  -- contact's own subscription flag are both send gates; the audience
		  -- count applies the same two, so the number shown is the number sent.
		  AND NOT recipient_suppressed((SELECT organization_id FROM campaigns WHERE id = $1), c.email)
		  AND c.subscribed IS NOT FALSE
		  -- Addresses the pre-send gates in the campaign task would refuse.
		  -- Without this the finder keeps handing back the same undeliverable
		  -- contact, the task skips it, and the campaign never reaches the
		  -- healthy leads behind it (issue #200). Read from the contact's
		  -- CURRENT verification state, so re-verifying an address puts it
		  -- straight back into routing.
		  AND NOT ` + undeliverableClause("$1") + `
		ORDER BY ` + orderPrefix + contactOrder + ` ` + dir + `
	`

	rows, err := r.db.Query(ctx, query, campaignID, config.CampaignSendMaxAttempts)
	if err != nil {
		return nil, nil, false, err
	}
	defer rows.Close()
	// nextDue is the soonest moment anything becomes sendable: a step whose
	// wait elapses or a condition window that closes. Only reported when no
	// contact is due right now.
	var nextDue *time.Time
	pairs := make([]ContactSequencePair, 0, limit)
	// waitingOnSender is true while the soonest moment belongs to a lead
	// waiting for its own mailbox, so the caller can say "waiting for its
	// mailbox" rather than "waiting for a step's delay".
	waitingOnSender := false
	noteDue := func(at time.Time, onSender bool) {
		if nextDue == nil || at.Before(*nextDue) {
			nextDue = &at
			waitingOnSender = onSender
		}
	}
	for rows.Next() {
		var in routeInput
		var contactID uuid.UUID
		if serr := rows.Scan(&contactID, &in.addedAt, &in.sender, &in.lastSeq, &in.sentAt, &in.openedAt, &in.clickedAt, &in.repliedAt, &in.replyClass, &in.aiLabel, &in.sentIDs, &in.hasReplied); serr != nil {
			return nil, nil, false, serr
		}
		if in.lastSeq == nil && excludeNewLeads {
			continue
		}
		res := router.route(campaignID, contactID, in)
		if res.WaitUntil != nil {
			// Not decidable yet — remember the soonest window so the scheduler
			// can re-check exactly then instead of guessing or completing.
			noteDue(*res.WaitUntil, false)
			continue
		}
		if res.Target == nil {
			continue // reached the end / a STOP / a step already received
		}
		// When is this step due? Skip it (but remember when) if not yet: the
		// contacts behind this one may be sendable right now.
		if res.DueAt != nil && res.DueAt.After(router.dueBy) {
			noteDue(*res.DueAt, false)
			continue
		}
		// Due, but the mailbox this lead's conversation belongs to has nothing
		// left today. Wait for it — the address a contact has already heard
		// from is worth more than sending the follow-up a few hours earlier
		// from a stranger. Only an email step waits: an action or wait node
		// sends nothing, so no mailbox has to be free for it.
		if back, ok := paced.reopening(in.sender); ok && router.isEmailStep(*res.Target) {
			noteDue(back, true)
			continue
		}
		pairs = append(pairs, ContactSequencePair{ContactID: contactID, SequenceID: *res.Target, IsNewLead: res.IsNewLead, NotBefore: res.DueAt, AssignedSender: in.sender})
		if len(pairs) >= limit {
			break
		}
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, nil, false, rerr
	}
	// A due pair makes the deferral times collected so far irrelevant: the
	// caller is sending, not waiting.
	if len(pairs) > 0 {
		return pairs, nil, false, nil
	}
	// Nobody sendable now. Hand back the soonest moment somebody will be so the
	// scheduler defers until then rather than completing.
	return nil, nextDue, waitingOnSender, nil
}

// ContactRoute is where a contact's flow goes next inside one campaign.
type ContactRoute struct {
	// Target is the next step to send. Nil when the flow has ended for the
	// contact, a step they already received is next (loop guard), or a
	// condition window is still open (WaitUntil set).
	Target    *uuid.UUID
	IsNewLead bool
	// DueAt is when the target's wait elapses: the campaign's entry delay after
	// the contact entered for a first step, otherwise the step's wait_after plus
	// a preceding wait node. Nil means due now.
	DueAt *time.Time
	// WaitUntil is when an undecided condition window closes.
	WaitUntil *time.Time
	// Excluded names the pre-send gate that keeps routing from ever offering
	// the contact: "bounced", "failed", "suppressed", "undeliverable" or
	// "not_a_lead". Empty when routing considers them.
	Excluded string
	// LastSentStep is the step the contact is on now (nil before any send).
	LastSentStep *uuid.UUID
	// AssignedSender is the mailbox this lead's sequence is bound to, nil
	// until its first email was reserved.
	AssignedSender *uuid.UUID
}

// RouteContact runs the campaign's routing for ONE contact and reports where
// their flow goes next, including the gate that would exclude them. It reads
// exactly what FindRoutedPairs reads, so a preview never disagrees with
// the send path.
func (r *campaignProgressRepository) RouteContact(ctx context.Context, campaignID, contactID uuid.UUID) (*ContactRoute, error) {
	router, err := r.loadRouter(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	query := `
		SELECT cl.added_at, cl.email_account_id,
		       lp.sequence_id, lp.sent_at, lp.opened_at, lp.clicked_at, lp.replied_at, COALESCE(lp.reply_class, ''), COALESCE(lp.ai_label, ''),
		       COALESCE(ss.ids, '{}') AS sent_ids,
		       EXISTS (
		         SELECT 1 FROM campaign_contact_progress rp
		         WHERE rp.campaign_id = $1 AND rp.contact_id = cl.contact_id AND rp.replied_at IS NOT NULL
		       ) AS has_replied,
		       EXISTS (
		         SELECT 1 FROM campaign_contact_progress b
		         WHERE b.contact_id = cl.contact_id AND b.bounced_at IS NOT NULL
		       ) AS bounced,
		       EXISTS (
		         SELECT 1 FROM campaign_contact_progress f
		         WHERE f.campaign_id = $1 AND f.contact_id = cl.contact_id
		           AND f.sent_at IS NULL AND f.failed_at IS NOT NULL
		           AND f.send_attempts >= $2
		       ) AS failed,
		       (recipient_suppressed((SELECT organization_id FROM campaigns WHERE id = $1), c.email)
		        OR c.subscribed IS FALSE) AS suppressed,
		       ` + undeliverableClause("$1") + ` AS undeliverable
		FROM campaign_leads cl
		JOIN contacts c ON c.id = cl.contact_id
		LEFT JOIN LATERAL (
			SELECT sequence_id, sent_at,
			       CASE WHEN p.opened_machine THEN NULL ELSE p.opened_at END AS opened_at,
			       clicked_at, replied_at, reply_class, ai_label
			FROM campaign_contact_progress p
			WHERE p.campaign_id = $1 AND p.contact_id = cl.contact_id AND p.sent_at IS NOT NULL
			ORDER BY p.sent_at DESC LIMIT 1
		) lp ON true
		LEFT JOIN LATERAL (
			SELECT array_agg(sequence_id) AS ids
			FROM campaign_contact_progress p2
			WHERE p2.campaign_id = $1 AND p2.contact_id = cl.contact_id
			  AND (p2.sent_at IS NOT NULL OR p2.dispatched_at IS NOT NULL)
		) ss ON true
		WHERE cl.campaign_id = $1 AND cl.contact_id = $3
	`
	var in routeInput
	var bounced, failed, suppressed, undeliverable bool
	err = r.db.QueryRow(ctx, query, campaignID, config.CampaignSendMaxAttempts, contactID).Scan(
		&in.addedAt, &in.sender, &in.lastSeq, &in.sentAt, &in.openedAt, &in.clickedAt, &in.repliedAt, &in.replyClass, &in.aiLabel, &in.sentIDs, &in.hasReplied,
		&bounced, &failed, &suppressed, &undeliverable,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return &ContactRoute{Excluded: "not_a_lead"}, nil
	}
	if err != nil {
		return nil, err
	}
	out := &ContactRoute{LastSentStep: in.lastSeq, AssignedSender: in.sender}
	switch {
	case bounced:
		out.Excluded = "bounced"
	case failed:
		out.Excluded = "failed"
	case suppressed:
		out.Excluded = "suppressed"
	case undeliverable:
		out.Excluded = "undeliverable"
	}
	if router == nil {
		return out, nil
	}
	res := router.route(campaignID, contactID, in)
	out.Target, out.IsNewLead, out.DueAt, out.WaitUntil = res.Target, res.IsNewLead, res.DueAt, res.WaitUntil
	return out, nil
}

// routeInput is a lead's routing facts as the finder query returns them.
type routeInput struct {
	lastSeq                                *uuid.UUID
	sentAt, openedAt, clickedAt, repliedAt *time.Time
	// addedAt is when the contact entered the campaign; the anchor the entry
	// delay counts from. Nil for a lead added before the column existed.
	addedAt *time.Time
	// sender is the mailbox this lead's sequence is bound to, nil until its
	// first email was reserved.
	sender              *uuid.UUID
	replyClass, aiLabel string
	sentIDs             []uuid.UUID
	hasReplied          bool
}

// PacedSenders maps a mailbox that cannot take a send right now, but will be
// able to on its own (its daily budget resets, its sending hours reopen), to
// when it is expected back. It is deliberately NOT the set of every unusable
// mailbox: one that is disconnected, failing authentication, resting or held
// by warmup health is absent, so the leads bound to it are routed and moved
// off it rather than left waiting for a return that is not coming.
type PacedSenders map[uuid.UUID]time.Time

// reopening reports when a lead's bound mailbox is expected back, and whether
// it is paced at all. An unassigned lead is never paced: rotation is still
// free to pick for it.
func (p PacedSenders) reopening(sender *uuid.UUID) (time.Time, bool) {
	if p == nil || sender == nil {
		return time.Time{}, false
	}
	at, ok := p[*sender]
	return at, ok
}

// campaignRouter is a campaign's flow graph loaded once per pass, with the
// routing rules that decide where a contact goes out of their current step.
type campaignRouter struct {
	steps   []routeStep
	idxByID map[uuid.UUID]int
	entry   uuid.UUID
	now     time.Time
	// dueBy tolerates the same grace the scheduler's hard-floor check does: a
	// task fires at (or a breath before) the slot it was scheduled for.
	dueBy          time.Time
	stopOnReply    bool
	replyFlowSteps map[uuid.UUID]bool
	// entryDelay holds a new lead's first email back for this long after they
	// entered the campaign; zero means the first step is due immediately.
	entryDelay time.Duration
	// campaignCreatedAt stands in as the entry time for a lead added before
	// campaign_leads.added_at existed (the column is nullable and unbackfilled).
	campaignCreatedAt time.Time
}

type routeStep struct {
	id          uuid.UUID
	kind        string
	bc          models.BranchConditions
	waitAfter   int
	waitMinutes int // a "wait" node's own delay, gating the step after it
}

// routeResult is the outcome of routing a contact out of their current step:
// send `target`, fully `stop`, or `wait` until a condition window elapses.
type routeResult struct {
	target *uuid.UUID
	stop   bool
	wait   *time.Time
}

// loadRouter reads the steps (position + branch tree + wait) once, ordered by
// position, and precomputes the reply-flow step set for route-aware
// stop_on_reply. Returns nil when the campaign has no steps.
func (r *campaignProgressRepository) loadRouter(ctx context.Context, campaignID uuid.UUID) (*campaignRouter, error) {
	srows, err := r.db.Query(ctx, `SELECT id, conditions, wait_after, kind, action FROM sequences WHERE campaign_id = $1 ORDER BY position ASC, created_at ASC`, campaignID)
	if err != nil {
		return nil, err
	}
	cr := &campaignRouter{idxByID: map[uuid.UUID]int{}, replyFlowSteps: map[uuid.UUID]bool{}}
	for srows.Next() {
		var si routeStep
		var raw, action []byte
		if serr := srows.Scan(&si.id, &raw, &si.waitAfter, &si.kind, &action); serr != nil {
			srows.Close()
			return nil, serr
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &si.bc)
		}
		if si.kind != "email" && len(action) > 0 {
			var cfg models.ActionConfig
			if json.Unmarshal(action, &cfg) == nil && cfg.Type == "wait" && cfg.WaitMinutes != nil && *cfg.WaitMinutes > 0 {
				si.waitMinutes = *cfg.WaitMinutes
			}
		}
		cr.idxByID[si.id] = len(cr.steps)
		cr.steps = append(cr.steps, si)
	}
	srows.Close()
	if serr := srows.Err(); serr != nil {
		return nil, serr
	}
	if len(cr.steps) == 0 {
		return nil, nil
	}
	cr.entry = cr.steps[0].id
	cr.now = time.Now()
	cr.dueBy = cr.now.Add(config.CampaignNotDueGraceSeconds * time.Second)

	// Route-aware stop_on_reply. Rather than blanket-excluding every contact who
	// has replied (which also kills the reply branch's OWN follow-up steps), a
	// replied contact keeps moving ONLY while their next routed step is part of the
	// REPLY FLOW: the subgraph downstream of a reply branch that is NOT also part
	// of the cold sequence. The normal / fall-through cold sequence stops; the
	// reply branch's path (its actions AND any follow-up emails) runs to
	// completion. Compute the reply-flow step set once and load the flag.
	var entryDelayMinutes int
	if serr := r.db.QueryRow(ctx,
		`SELECT stop_on_reply, entry_delay_minutes, created_at FROM campaigns WHERE id = $1`,
		campaignID).Scan(&cr.stopOnReply, &entryDelayMinutes, &cr.campaignCreatedAt); serr != nil {
		return nil, serr
	}
	cr.entryDelay = time.Duration(entryDelayMinutes) * time.Minute
	if cr.stopOnReply {
		steps := cr.steps
		idxByID := cr.idxByID
		// bfs walks a directed reachability closure from `seeds`, following the
		// targets selected by `follow`, and records every visited step into `into`.
		bfs := func(seeds []uuid.UUID, into map[uuid.UUID]bool, follow func(b *models.Branch) bool) {
			queue := append([]uuid.UUID(nil), seeds...)
			for _, s := range seeds {
				into[s] = true
			}
			for len(queue) > 0 {
				cur := queue[0]
				queue = queue[1:]
				idx, ok := idxByID[cur]
				if !ok {
					continue
				}
				for j := range steps[idx].bc.Branches {
					b := &steps[idx].bc.Branches[j]
					if b.TargetSequenceID == nil || into[*b.TargetSequenceID] || !follow(b) {
						continue
					}
					into[*b.TargetSequenceID] = true
					queue = append(queue, *b.TargetSequenceID)
				}
			}
		}

		// 1. Cold trunk: every step a NON-replier reaches from the entry, i.e.
		//    following every branch EXCEPT the ones that route on a positive reply.
		//    A step that also sits here (a reply branch merged back into the cold
		//    sequence) is NOT reply flow — a replier must stop when they reach it.
		coldReachable := map[uuid.UUID]bool{}
		bfs([]uuid.UUID{cr.entry}, coldReachable, func(b *models.Branch) bool {
			return !branchHasPositiveReplyCondition(b)
		})

		// 2. Reply flow: everything reachable from a positive-reply branch target
		//    (following ALL onward branches, so internal reply-flow branching is
		//    kept), MINUS the cold trunk.
		var seeds []uuid.UUID
		for i := range steps {
			for j := range steps[i].bc.Branches {
				b := &steps[i].bc.Branches[j]
				if b.TargetSequenceID != nil && branchHasPositiveReplyCondition(b) && !coldReachable[*b.TargetSequenceID] {
					seeds = append(seeds, *b.TargetSequenceID)
				}
			}
		}
		bfs(seeds, cr.replyFlowSteps, func(b *models.Branch) bool {
			return b.TargetSequenceID != nil && !coldReachable[*b.TargetSequenceID]
		})
	}
	return cr, nil
}

// routeNext follows the first DECIDABLE branch out of fromID. A branch whose
// window is still open leaves the contact waiting (so "if opened within 3d"
// gets its 3 days instead of being judged the instant the step is sent).
func (cr *campaignRouter) routeNext(fromID uuid.UUID, prog *CampaignContactProgress, sentAt time.Time) routeResult {
	idx, ok := cr.idxByID[fromID]
	if !ok {
		return routeResult{stop: true}
	}
	bc := cr.steps[idx].bc
	// Routing is purely the connections the user drew. A step with no
	// outgoing connection, or whose connections don't match, ends the
	// contact (STOP). There is NO implicit "advance to the next step by
	// position" — steps are only linked when explicitly connected, and an
	// unconditional connection (a branch with no conditions) is the "just go
	// there after the wait" default.
	if len(bc.Branches) == 0 {
		return routeResult{stop: true}
	}
	for i := range bc.Branches {
		b := &bc.Branches[i]
		st, recheck := evaluateBranchState(b, prog, sentAt, cr.now)
		if st == BranchNoMatch {
			continue
		}
		if st == BranchUndecided {
			rc := recheck
			return routeResult{wait: &rc}
		}
		// Matched: a nil / deleted target ends the contact (STOP).
		if b.TargetSequenceID == nil {
			return routeResult{stop: true}
		}
		if _, live := cr.idxByID[*b.TargetSequenceID]; !live {
			return routeResult{stop: true}
		}
		t := *b.TargetSequenceID
		return routeResult{target: &t}
	}
	// Nothing matched -> the flow ends with STOP.
	return routeResult{stop: true}
}

// routeReplyOnly is the routing used for a contact who HAS REPLIED but is still
// on the cold trunk (stop_on_reply on). It considers ONLY positive-reply
// branches that lead into the reply flow, giving them priority regardless of
// declared order, and never waits on a cold window. The first such branch that
// matches right now routes the contact into the reply flow; anything else means
// "no reply handling here for this reply" -> STOP the cold sequence. This is
// what lets a non-instant reply branch fire even when an engagement branch is
// declared ahead of it, and stops a replier instead of deferring on a cold
// "did not open within N days" window forever.
func (cr *campaignRouter) routeReplyOnly(fromID uuid.UUID, prog *CampaignContactProgress, sentAt time.Time) routeResult {
	idx, ok := cr.idxByID[fromID]
	if !ok {
		return routeResult{stop: true}
	}
	for i := range cr.steps[idx].bc.Branches {
		b := &cr.steps[idx].bc.Branches[i]
		if b.TargetSequenceID == nil || !branchHasPositiveReplyCondition(b) || !cr.replyFlowSteps[*b.TargetSequenceID] {
			continue
		}
		if st, _ := evaluateBranchState(b, prog, sentAt, cr.now); st != BranchMatch {
			continue
		}
		if _, live := cr.idxByID[*b.TargetSequenceID]; !live {
			continue
		}
		t := *b.TargetSequenceID
		return routeResult{target: &t}
	}
	return routeResult{stop: true}
}

// route decides one contact's next step from their routing facts: the entry
// step for a new lead, otherwise the route out of their last-sent step, with
// the loop guard and the step's wait applied.
func (cr *campaignRouter) route(campaignID, contactID uuid.UUID, in routeInput) ContactRoute {
	out := ContactRoute{LastSentStep: in.lastSeq, IsNewLead: in.lastSeq == nil}
	var res routeResult
	if out.IsNewLead {
		e := cr.entry
		res = routeResult{target: &e}
	} else {
		prog := &CampaignContactProgress{
			CampaignID: campaignID, ContactID: contactID, SequenceID: *in.lastSeq,
			SentAt: in.sentAt, OpenedAt: in.openedAt, ClickedAt: in.clickedAt, RepliedAt: in.repliedAt,
			ReplyClass: in.replyClass, AILabel: in.aiLabel,
		}
		sa := time.Time{}
		if in.sentAt != nil {
			sa = *in.sentAt
		}
		res = cr.routeNext(*in.lastSeq, prog, sa)

		// Route-aware stop_on_reply for a contact who has replied:
		//   - still on the cold trunk (lastSeq not in the reply flow): they may
		//     only ENTER the reply flow via a positive-reply branch. Any cold
		//     route, an undecided cold window, or a stop ends them here — no cold
		//     sends and no deferring on a cold window.
		//   - already inside the reply flow: keep the reply flow's own routing
		//     (its waits are legitimate), but if it would route back out into the
		//     cold sequence, stop instead.
		if cr.stopOnReply && in.hasReplied {
			if !cr.replyFlowSteps[*in.lastSeq] {
				res = cr.routeReplyOnly(*in.lastSeq, prog, sa)
			} else if res.target != nil && !cr.replyFlowSteps[*res.target] {
				res = routeResult{stop: true}
			}
		}
	}

	if res.wait != nil {
		out.WaitUntil = res.wait
		return out
	}
	if res.stop || res.target == nil {
		return out // reached the end / a STOP (incl. a replied contact stopped above)
	}
	// Loop guard: never re-send a step the contact already received.
	for _, sid := range in.sentIDs {
		if sid == *res.target {
			return out
		}
	}
	out.Target = res.target
	if out.IsNewLead {
		// The entry delay: a contact's first email waits this long after they
		// entered the campaign. Anchored on when they entered, not on when the
		// campaign started, so a contact who joins a linked segment weeks later
		// gets the same delay.
		if cr.entryDelay > 0 {
			due := cr.enteredAt(in.addedAt).Add(cr.entryDelay)
			out.DueAt = &due
		}
		return out
	}
	if in.sentAt != nil {
		due := in.sentAt.Add(24 * time.Hour * time.Duration(cr.steps[cr.idxByID[*res.target]].waitAfter))
		if last, ok := cr.idxByID[*in.lastSeq]; ok && cr.steps[last].waitMinutes > 0 {
			due = due.Add(time.Duration(cr.steps[last].waitMinutes) * time.Minute)
		}
		out.DueAt = &due
	}
	return out
}

// isEmailStep reports whether a step sends mail. Only those need a mailbox, so
// only those wait for one.
func (cr *campaignRouter) isEmailStep(id uuid.UUID) bool {
	idx, ok := cr.idxByID[id]
	return ok && cr.steps[idx].kind == "email"
}

// enteredAt is when a lead entered the campaign. campaign_leads.added_at is
// nullable and deliberately not backfilled, so a lead that predates the column
// falls back to the campaign's own creation time — which keeps turning an entry
// delay on from re-delaying leads that have been in the campaign for weeks.
func (cr *campaignRouter) enteredAt(addedAt *time.Time) time.Time {
	if addedAt != nil {
		return *addedAt
	}
	return cr.campaignCreatedAt
}

// branchHasPositiveReplyCondition reports whether a branch is a "reply branch":
// one that routes on a POSITIVE reply signal (a human reply, or a classified
// reply intent). Its target seeds the reply flow used by route-aware
// stop_on_reply. not_replied is deliberately excluded — it is the "did NOT
// reply" continuation of the normal cold sequence, not the reply flow.
func branchHasPositiveReplyCondition(b *models.Branch) bool {
	for i := range b.Conditions {
		switch b.Conditions[i].Field {
		case "replied", "reply_positive", "reply_negative", "reply_neutral", "reply_automated":
			return true
		}
	}
	return false
}

// CountUndeliverableLeads counts the leads that verification alone keeps out
// of routing: the campaign's flow would still send them a step if their
// verdict were ignored. It runs the same router the send path runs, so a lead
// whose flow has ended (a STOP branch, no outgoing connection, every step
// attempted, replied) is not counted, and neither is one another gate
// excludes (bounced, failed, suppressed).
func (r *campaignProgressRepository) CountUndeliverableLeads(ctx context.Context, campaignID uuid.UUID) (int, error) {
	router, err := r.loadRouter(ctx, campaignID)
	if err != nil {
		return 0, err
	}
	if router == nil {
		return 0, nil
	}
	query := `
		SELECT cl.contact_id, cl.added_at,
		       lp.sequence_id, lp.sent_at, lp.opened_at, lp.clicked_at, lp.replied_at, COALESCE(lp.reply_class, ''), COALESCE(lp.ai_label, ''),
		       COALESCE(ss.ids, '{}') AS sent_ids,
		       EXISTS (
		         SELECT 1 FROM campaign_contact_progress rp
		         WHERE rp.campaign_id = $1 AND rp.contact_id = cl.contact_id AND rp.replied_at IS NOT NULL
		       ) AS has_replied
		FROM campaign_leads cl
		JOIN contacts c ON c.id = cl.contact_id
		LEFT JOIN LATERAL (
			SELECT sequence_id, sent_at,
			       CASE WHEN p.opened_machine THEN NULL ELSE p.opened_at END AS opened_at,
			       clicked_at, replied_at, reply_class, ai_label
			FROM campaign_contact_progress p
			WHERE p.campaign_id = $1 AND p.contact_id = cl.contact_id AND p.sent_at IS NOT NULL
			ORDER BY p.sent_at DESC LIMIT 1
		) lp ON true
		LEFT JOIN LATERAL (
			SELECT array_agg(sequence_id) AS ids
			FROM campaign_contact_progress p2
			WHERE p2.campaign_id = $1 AND p2.contact_id = cl.contact_id
			  AND (p2.sent_at IS NOT NULL OR p2.dispatched_at IS NOT NULL)
		) ss ON true
		WHERE cl.campaign_id = $1
		  AND NOT EXISTS (
		    SELECT 1 FROM campaign_contact_progress b
		    WHERE b.contact_id = cl.contact_id AND b.bounced_at IS NOT NULL
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM campaign_contact_progress f
		    WHERE f.campaign_id = $1 AND f.contact_id = cl.contact_id
		      AND f.sent_at IS NULL AND f.failed_at IS NOT NULL
		      AND f.send_attempts >= $2
		  )
		  -- The workspace suppression list (addresses and domains) and the
		  -- contact's own subscription flag are both send gates; the audience
		  -- count applies the same two, so the number shown is the number sent.
		  AND NOT recipient_suppressed((SELECT organization_id FROM campaigns WHERE id = $1), c.email)
		  AND c.subscribed IS NOT FALSE
		  AND ` + undeliverableClause("$1") + `
	`
	rows, err := r.db.Query(ctx, query, campaignID, config.CampaignSendMaxAttempts)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var in routeInput
		var contactID uuid.UUID
		if serr := rows.Scan(&contactID, &in.addedAt, &in.lastSeq, &in.sentAt, &in.openedAt, &in.clickedAt, &in.repliedAt, &in.replyClass, &in.aiLabel, &in.sentIDs, &in.hasReplied); serr != nil {
			return 0, serr
		}
		res := router.route(campaignID, contactID, in)
		// A step to send (now or later) or a condition still deciding: the
		// flow is not over for this lead.
		if res.Target != nil || res.WaitUntil != nil {
			n++
		}
	}
	return n, rows.Err()
}

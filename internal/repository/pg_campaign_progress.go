package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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
}

type CampaignSequencePair struct {
	CampaignID uuid.UUID
	SequenceID uuid.UUID
}

// CampaignProgressRepository defines methods for campaign progress tracking
type CampaignProgressRepository interface {
	// Record email status
	RecordEmailSent(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error
	RecordEmailOpened(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID, machine bool) error
	RecordEmailClicked(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error
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
	// GetLatestReplyClass returns the most-recent classified reply class for a
	// contact in a campaign ("" when none). Convenience getter for the branch
	// evaluator / callers that need only the class.
	GetLatestReplyClass(ctx context.Context, contactID, campaignID uuid.UUID) (string, error)

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

	// FindNextRoutedPair selects the next (contact, step) to send by following
	// each contact's step rules (the branching tree) rather than a flat position
	// order. prioritizeNewLeads sorts first-step pairs first; excludeNewLeads
	// drops first-step pairs entirely so the new-lead/day cap can be enforced
	// while follow-ups keep flowing. The second return value, when the pair is
	// nil, is the soonest time a waiting contact's condition window elapses — the
	// scheduler should defer and re-check then rather than completing.
	FindNextRoutedPair(ctx context.Context, campaignID uuid.UUID, orderBy, orderDir, orderField string, prioritizeNewLeads, excludeNewLeads bool) (*ContactSequencePair, *time.Time, error)
}

type campaignProgressRepository struct {
	db *pgxpool.Pool
}

// NewCampaignProgressRepository creates a new campaign progress repository
func NewCampaignProgressRepository(db *pgxpool.Pool) CampaignProgressRepository {
	return &campaignProgressRepository{db: db}
}

// RecordEmailSent records that an email was sent
func (r *campaignProgressRepository) RecordEmailSent(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error {
	query := `
		INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (campaign_id, contact_id, sequence_id)
		DO UPDATE SET sent_at = NOW()
	`

	_, err := r.db.Exec(ctx, query, campaignID, contactID, sequenceID)
	return err
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
func (r *campaignProgressRepository) RecordEmailClicked(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error {
	query := `
		UPDATE campaign_contact_progress
		SET clicked_at = NOW()
		WHERE campaign_id = $1
		  AND contact_id = $2
		  AND sequence_id = $3
		  AND clicked_at IS NULL
	`

	_, err := r.db.Exec(ctx, query, campaignID, contactID, sequenceID)
	return err
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
	if err == sql.ErrNoRows {
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

	if err == sql.ErrNoRows {
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
	if err == sql.ErrNoRows {
		return &CampaignRollingRates{}, nil
	}
	return out, err
}

// GetContactProgress retrieves progress for a specific contact in a campaign
func (r *campaignProgressRepository) GetContactProgress(ctx context.Context, campaignID, contactID uuid.UUID) ([]CampaignContactProgress, error) {
	query := `
		SELECT campaign_id, contact_id, sequence_id, sent_at, opened_at, clicked_at, replied_at, bounced_at, complained_at, COALESCE(reply_class, '')
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

	if err == sql.ErrNoRows {
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

// CountEmailsSentTodayByOrganization returns how many campaign emails were sent today by an organization.
func (r *campaignProgressRepository) CountEmailsSentTodayByOrganization(ctx context.Context, organizationID uuid.UUID) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM campaign_contact_progress ccp
		JOIN campaigns c ON c.id = ccp.campaign_id
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
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return out, nil
}

// FindNextRoutedPair selects the next (contact, step) to send by FOLLOWING THE
// FLOW graph instead of linear position order. For each contact, the next step
// is the route out of their last-sent step:
//  1. conditional branches (first match wins, evaluated against engagement),
//  2. then the explicit "else" catch-all branch (empty conditions; target nil = STOP),
//  3. then linear position+1 — but ONLY when the step defines no branches at all
//     (so plain linear campaigns keep working unchanged).
//
// A contact who has never been sent starts at the entry step (position 1). A
// step is sent only if the route reaches it, so branch-only steps are never sent
// linearly, and a routed step that was already sent (a loop) stops the contact.
//
// Conditions are evaluated SEND-RELATIVE with a three-valued result: a contact
// whose next step isn't decidable yet (an engagement window still open) is not
// returned; instead the soonest re-check time is returned (second value) so the
// scheduler defers and checks again exactly then. Returns (nil, nil, nil) when
// the campaign is genuinely complete (no sendable and nothing pending).
func (r *campaignProgressRepository) FindNextRoutedPair(ctx context.Context, campaignID uuid.UUID, orderBy, orderDir, orderField string, prioritizeNewLeads, excludeNewLeads bool) (*ContactSequencePair, *time.Time, error) {
	// 1. Load steps (position + branch tree) once, ordered by position.
	type stepInfo struct {
		id uuid.UUID
		bc models.BranchConditions
	}
	srows, err := r.db.Query(ctx, `SELECT id, conditions FROM sequences WHERE campaign_id = $1 ORDER BY position ASC, created_at ASC`, campaignID)
	if err != nil {
		return nil, nil, err
	}
	var steps []stepInfo
	idxByID := map[uuid.UUID]int{}
	for srows.Next() {
		var si stepInfo
		var raw []byte
		if serr := srows.Scan(&si.id, &raw); serr != nil {
			srows.Close()
			return nil, nil, serr
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &si.bc)
		}
		idxByID[si.id] = len(steps)
		steps = append(steps, si)
	}
	srows.Close()
	if serr := srows.Err(); serr != nil {
		return nil, nil, serr
	}
	if len(steps) == 0 {
		return nil, nil, nil
	}
	entry := steps[0].id
	now := time.Now()

	// Route-aware stop_on_reply. Rather than blanket-excluding every contact who
	// has replied (which also kills the reply branch's OWN follow-up steps), a
	// replied contact keeps moving ONLY while their next routed step is part of the
	// REPLY FLOW: the subgraph downstream of a reply branch that is NOT also part
	// of the cold sequence. The normal / fall-through cold sequence stops; the
	// reply branch's path (its actions AND any follow-up emails) runs to
	// completion. Compute the reply-flow step set once and load the flag.
	var stopOnReply bool
	if serr := r.db.QueryRow(ctx, `SELECT stop_on_reply FROM campaigns WHERE id = $1`, campaignID).Scan(&stopOnReply); serr != nil {
		return nil, nil, serr
	}
	replyFlowSteps := map[uuid.UUID]bool{}
	if stopOnReply {
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
		bfs([]uuid.UUID{entry}, coldReachable, func(b *models.Branch) bool {
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
		bfs(seeds, replyFlowSteps, func(b *models.Branch) bool {
			return b.TargetSequenceID != nil && !coldReachable[*b.TargetSequenceID]
		})
	}

	// routeResult is the outcome of routing a contact out of their current step:
	// send `target`, fully `stop`, or `wait` until a condition window elapses.
	type routeResult struct {
		target *uuid.UUID
		stop   bool
		wait   *time.Time
	}
	// routeNext follows the first DECIDABLE branch out of fromID. A branch whose
	// window is still open leaves the contact waiting (so "if opened within 3d"
	// gets its 3 days instead of being judged the instant the step is sent).
	routeNext := func(fromID uuid.UUID, prog *CampaignContactProgress, sentAt time.Time) routeResult {
		idx, ok := idxByID[fromID]
		if !ok {
			return routeResult{stop: true}
		}
		bc := steps[idx].bc
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
			st, recheck := evaluateBranchState(b, prog, sentAt, now)
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
			if _, live := idxByID[*b.TargetSequenceID]; !live {
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
	routeReplyOnly := func(fromID uuid.UUID, prog *CampaignContactProgress, sentAt time.Time) routeResult {
		idx, ok := idxByID[fromID]
		if !ok {
			return routeResult{stop: true}
		}
		for i := range steps[idx].bc.Branches {
			b := &steps[idx].bc.Branches[i]
			if b.TargetSequenceID == nil || !branchHasPositiveReplyCondition(b) || !replyFlowSteps[*b.TargetSequenceID] {
				continue
			}
			if st, _ := evaluateBranchState(b, prog, sentAt, now); st != BranchMatch {
				continue
			}
			if _, live := idxByID[*b.TargetSequenceID]; !live {
				continue
			}
			t := *b.TargetSequenceID
			return routeResult{target: &t}
		}
		return routeResult{stop: true}
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
		SELECT cl.contact_id,
		       lp.sequence_id, lp.sent_at, lp.opened_at, lp.clicked_at, lp.replied_at, COALESCE(lp.reply_class, ''),
		       COALESCE(ss.ids, '{}') AS sent_ids,
		       EXISTS (
		         SELECT 1 FROM campaign_contact_progress rp
		         WHERE rp.campaign_id = $1 AND rp.contact_id = cl.contact_id AND rp.replied_at IS NOT NULL
		       ) AS has_replied
		FROM campaign_leads cl
		JOIN contacts c ON c.id = cl.contact_id
		LEFT JOIN LATERAL (
			SELECT sequence_id, sent_at, opened_at, clicked_at, replied_at, reply_class
			FROM campaign_contact_progress p
			WHERE p.campaign_id = $1 AND p.contact_id = cl.contact_id AND p.sent_at IS NOT NULL
			ORDER BY p.sent_at DESC LIMIT 1
		) lp ON true
		LEFT JOIN LATERAL (
			SELECT array_agg(sequence_id) AS ids
			FROM campaign_contact_progress p2
			WHERE p2.campaign_id = $1 AND p2.contact_id = cl.contact_id AND p2.sent_at IS NOT NULL
		) ss ON true
		WHERE cl.campaign_id = $1
		  AND NOT EXISTS (
		    SELECT 1 FROM campaign_contact_progress b
		    WHERE b.contact_id = cl.contact_id AND b.bounced_at IS NOT NULL
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM suppressed_recipients sr
		    JOIN campaigns camp ON camp.organization_id = sr.organization_id
		    WHERE camp.id = $1
		      AND LOWER(sr.email) = LOWER(c.email)
		      AND (sr.expires_at IS NULL OR sr.expires_at > NOW())
		  )
		ORDER BY ` + orderPrefix + contactOrder + ` ` + dir + `
	`

	rows, err := r.db.Query(ctx, query, campaignID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var earliestWait *time.Time
	for rows.Next() {
		var contactID uuid.UUID
		var lastSeq *uuid.UUID
		var sentAt, openedAt, clickedAt, repliedAt *time.Time
		var replyClass string
		var sentIDs []uuid.UUID
		var hasReplied bool
		if serr := rows.Scan(&contactID, &lastSeq, &sentAt, &openedAt, &clickedAt, &repliedAt, &replyClass, &sentIDs, &hasReplied); serr != nil {
			return nil, nil, serr
		}

		isNew := lastSeq == nil
		var res routeResult
		if isNew {
			if excludeNewLeads {
				continue
			}
			e := entry
			res = routeResult{target: &e}
		} else {
			prog := &CampaignContactProgress{
				CampaignID: campaignID, ContactID: contactID, SequenceID: *lastSeq,
				SentAt: sentAt, OpenedAt: openedAt, ClickedAt: clickedAt, RepliedAt: repliedAt,
				ReplyClass: replyClass,
			}
			sa := time.Time{}
			if sentAt != nil {
				sa = *sentAt
			}
			res = routeNext(*lastSeq, prog, sa)

			// Route-aware stop_on_reply for a contact who has replied:
			//   - still on the cold trunk (lastSeq not in the reply flow): they may
			//     only ENTER the reply flow via a positive-reply branch. Any cold
			//     route, an undecided cold window, or a stop ends them here — no cold
			//     sends and no deferring on a cold window.
			//   - already inside the reply flow: keep the reply flow's own routing
			//     (its waits are legitimate), but if it would route back out into the
			//     cold sequence, stop instead.
			if stopOnReply && hasReplied {
				if !replyFlowSteps[*lastSeq] {
					res = routeReplyOnly(*lastSeq, prog, sa)
				} else if res.target != nil && !replyFlowSteps[*res.target] {
					res = routeResult{stop: true}
				}
			}
		}

		if res.wait != nil {
			// Not decidable yet — remember the soonest window so the scheduler
			// can re-check exactly then instead of guessing or completing.
			if earliestWait == nil || res.wait.Before(*earliestWait) {
				earliestWait = res.wait
			}
			continue
		}
		if res.stop || res.target == nil {
			continue // reached the end / a STOP (incl. a replied contact stopped above)
		}
		// Loop guard: never re-send a step the contact already received.
		already := false
		for _, sid := range sentIDs {
			if sid == *res.target {
				already = true
				break
			}
		}
		if already {
			continue
		}
		return &ContactSequencePair{ContactID: contactID, SequenceID: *res.target, IsNewLead: isNew}, nil, nil
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, nil, rerr
	}
	// Nobody sendable now. If contacts are waiting on a window, hand back the
	// soonest re-check time so the scheduler defers rather than completing.
	return nil, earliestWait, nil
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

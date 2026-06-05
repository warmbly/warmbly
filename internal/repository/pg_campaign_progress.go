package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// ErrUseLinearProgression is returned by ResolveSequenceBranch when the current
// step has no branching configured, or no branch matched. The caller should
// keep the default linear next-step behaviour. It is NOT an error condition —
// it is the explicit "no override" signal, kept distinct from a real error and
// from a nil ("stop") target.
var ErrUseLinearProgression = errors.New("use linear sequence progression")

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
	RecordEmailOpened(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error
	RecordEmailClicked(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error
	RecordEmailReplied(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error
	RecordEmailBounced(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error
	RecordEmailComplained(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error

	// Query methods
	GetCampaignProgress(ctx context.Context, campaignID uuid.UUID) (*CampaignProgress, error)
	GetCampaignRollingRates(ctx context.Context, campaignID uuid.UUID, since time.Time) (*CampaignRollingRates, error)
	GetContactProgress(ctx context.Context, campaignID, contactID uuid.UUID) ([]CampaignContactProgress, error)
	GetContactLastSequenceTime(ctx context.Context, contactID, campaignID uuid.UUID) (*time.Time, error)
	CheckContactHasReplied(ctx context.Context, contactID, campaignID uuid.UUID) (bool, error)
	CountEmailsSentTodayByOrganization(ctx context.Context, organizationID uuid.UUID) (int, error)
	GetLatestCampaignSequenceForContact(ctx context.Context, contactID uuid.UUID) (*CampaignSequencePair, error)

	// Find next email to send. prioritizeNewLeads sorts position-1 (new lead)
	// pairs first; excludeNewLeads drops position-1 pairs entirely (so the
	// new-lead/day cap can be enforced while follow-ups keep flowing).
	FindNextContactSequence(ctx context.Context, campaignID uuid.UUID, orderBy, orderDir, orderField string, prioritizeNewLeads, excludeNewLeads bool) (*ContactSequencePair, error)

	// FindNextRoutedPair selects the next (contact, step) by following the flow
	// graph (branch routing) rather than linear position order. A step is sent
	// only if the route reaches it. Supersedes FindNextContactSequence + the
	// scheduler's separate branch-override step.
	FindNextRoutedPair(ctx context.Context, campaignID uuid.UUID, orderBy, orderDir, orderField string, prioritizeNewLeads, excludeNewLeads bool) (*ContactSequencePair, error)

	// ResolveSequenceBranch evaluates the branching tree on currentSequenceID
	// against the contact's recorded engagement and decides what comes next:
	//   - (*uuid.UUID, nil)          → jump to that target step
	//   - (nil, nil)                 → STOP (a matched branch routes to "stop")
	//   - (nil, ErrUseLinearProgression) → no branching / no branch matched;
	//     keep the default linear next step
	//   - (nil, err)                 → a real lookup error
	// Conditions are evaluated at schedule time against the contact's
	// campaign_contact_progress row for currentSequenceID (see the race note in
	// the scheduler).
	ResolveSequenceBranch(ctx context.Context, campaignID, contactID, currentSequenceID uuid.UUID) (*uuid.UUID, error)

	// GetCurrentSequenceForContact returns the contact's CURRENT step in a
	// campaign (the most recently sent sequence). Returns (nil, nil) when the
	// contact has not been sent anything yet (a brand-new lead, for which there
	// is no prior step to branch from). Used by the scheduler to know which
	// step's branching tree to evaluate.
	GetCurrentSequenceForContact(ctx context.Context, campaignID, contactID uuid.UUID) (*uuid.UUID, error)

	// HasSentSequence reports whether a specific step has already been sent to a
	// contact. The scheduler uses it so a branch override never re-sends a step
	// the contact already received.
	HasSentSequence(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) (bool, error)

	// FindNextContactSequenceExcluding behaves exactly like
	// FindNextContactSequence but additionally skips the given contacts. The
	// scheduler uses it to step past contacts whose branching resolved to STOP
	// for this pass without re-sending them.
	FindNextContactSequenceExcluding(ctx context.Context, campaignID uuid.UUID, orderBy, orderDir, orderField string, prioritizeNewLeads, excludeNewLeads bool, excludeContacts []uuid.UUID) (*ContactSequencePair, error)
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

// RecordEmailOpened records that an email was opened
func (r *campaignProgressRepository) RecordEmailOpened(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error {
	query := `
		UPDATE campaign_contact_progress
		SET opened_at = NOW()
		WHERE campaign_id = $1
		  AND contact_id = $2
		  AND sequence_id = $3
		  AND opened_at IS NULL
	`

	_, err := r.db.Exec(ctx, query, campaignID, contactID, sequenceID)
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
		SELECT campaign_id, contact_id, sequence_id, sent_at, opened_at, clicked_at, replied_at, bounced_at, complained_at
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

// FindNextContactSequence finds the next contact/sequence pair that needs to be sent
// orderBy: "created_at", "email", "name", "custom_field", "manual"
// orderDir: "asc", "desc"
// orderField: custom field name (used when orderBy is "custom_field")
func (r *campaignProgressRepository) FindNextContactSequence(ctx context.Context, campaignID uuid.UUID, orderBy, orderDir, orderField string, prioritizeNewLeads, excludeNewLeads bool) (*ContactSequencePair, error) {
	return r.FindNextContactSequenceExcluding(ctx, campaignID, orderBy, orderDir, orderField, prioritizeNewLeads, excludeNewLeads, nil)
}

// FindNextContactSequenceExcluding is FindNextContactSequence plus an optional
// set of contacts to skip (used by the branching STOP path).
func (r *campaignProgressRepository) FindNextContactSequenceExcluding(ctx context.Context, campaignID uuid.UUID, orderBy, orderDir, orderField string, prioritizeNewLeads, excludeNewLeads bool, excludeContacts []uuid.UUID) (*ContactSequencePair, error) {
	// Build the ORDER BY clause based on ordering settings
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
	default: // created_at
		contactOrder = "c.created_at"
	}

	// Apply direction
	dir := "ASC"
	if orderDir == "desc" {
		dir = "DESC"
	}

	// When prioritizing new leads, sort position-1 pairs first; otherwise the
	// existing contact/position/created_at order is preserved exactly.
	orderPrefix := ""
	if prioritizeNewLeads {
		orderPrefix = "(s.position = 1) DESC, "
	}

	// When excluding new leads (the per-day cap is hit), drop position-1 pairs
	// so only follow-ups remain eligible.
	excludeClause := ""
	if excludeNewLeads {
		excludeClause = "AND s.position > 1"
	}

	// Optional per-contact exclusion (branching STOP path). Bound as $2 so the
	// list is parameterised; empty list means "exclude nothing".
	contactExcludeClause := "AND NOT (cl.contact_id = ANY($2::uuid[]))"
	if len(excludeContacts) == 0 {
		excludeContacts = []uuid.UUID{}
	}

	query := `
		WITH all_pairs AS (
			-- Generate all possible contact-sequence combinations for this campaign
			SELECT
				cl.contact_id,
				s.id as sequence_id,
				(s.position = 1) as is_new_lead,
				ROW_NUMBER() OVER (ORDER BY ` + orderPrefix + contactOrder + ` ` + dir + `, s.position, s.created_at) as pair_order
			FROM campaign_leads cl
			JOIN contacts c ON c.id = cl.contact_id
			CROSS JOIN sequences s
			WHERE cl.campaign_id = $1
			  AND s.campaign_id = $1
			  ` + excludeClause + `
			  ` + contactExcludeClause + `
			  -- Skip contacts that bounced in ANY campaign
			  AND NOT EXISTS (
			    SELECT 1 FROM campaign_contact_progress ccp2
			    WHERE ccp2.contact_id = cl.contact_id
			      AND ccp2.bounced_at IS NOT NULL
			  )
			  -- Skip suppressed recipients (bounce, complaint, unsubscribe from deliverability)
			  AND NOT EXISTS (
			    SELECT 1 FROM suppressed_recipients sr
			    JOIN campaigns camp ON camp.organization_id = sr.organization_id
			    WHERE camp.id = $1
			      AND LOWER(sr.email) = LOWER(c.email)
			      AND (sr.expires_at IS NULL OR sr.expires_at > NOW())
			  )
		),
		sent_pairs AS (
			-- Get all already-sent pairs
			SELECT contact_id, sequence_id
			FROM campaign_contact_progress
			WHERE campaign_id = $1
			  AND sent_at IS NOT NULL
		)
		SELECT ap.contact_id, ap.sequence_id, ap.is_new_lead
		FROM all_pairs ap
		LEFT JOIN sent_pairs sp ON ap.contact_id = sp.contact_id AND ap.sequence_id = sp.sequence_id
		WHERE sp.contact_id IS NULL  -- Not yet sent
		ORDER BY ap.pair_order
		LIMIT 1
	`

	pair := &ContactSequencePair{}
	err := r.db.QueryRow(ctx, query, campaignID, excludeContacts).Scan(&pair.ContactID, &pair.SequenceID, &pair.IsNewLead)

	if err == sql.ErrNoRows {
		return nil, nil // No more emails to send
	}

	return pair, err
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
// Returns nil when no contact has a due next step.
func (r *campaignProgressRepository) FindNextRoutedPair(ctx context.Context, campaignID uuid.UUID, orderBy, orderDir, orderField string, prioritizeNewLeads, excludeNewLeads bool) (*ContactSequencePair, error) {
	// 1. Load steps (position + branch tree) once, ordered by position.
	type stepInfo struct {
		id uuid.UUID
		bc models.BranchConditions
	}
	srows, err := r.db.Query(ctx, `SELECT id, conditions FROM sequences WHERE campaign_id = $1 ORDER BY position ASC, created_at ASC`, campaignID)
	if err != nil {
		return nil, err
	}
	var steps []stepInfo
	idxByID := map[uuid.UUID]int{}
	for srows.Next() {
		var si stepInfo
		var raw []byte
		if serr := srows.Scan(&si.id, &raw); serr != nil {
			srows.Close()
			return nil, serr
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &si.bc)
		}
		idxByID[si.id] = len(steps)
		steps = append(steps, si)
	}
	srows.Close()
	if serr := srows.Err(); serr != nil {
		return nil, serr
	}
	if len(steps) == 0 {
		return nil, nil
	}
	entry := steps[0].id
	now := time.Now()

	// routeNext returns the next step out of fromID, or nil for STOP/end.
	routeNext := func(fromID uuid.UUID, prog *CampaignContactProgress) *uuid.UUID {
		idx, ok := idxByID[fromID]
		if !ok {
			return nil
		}
		bc := steps[idx].bc
		linearNext := func() *uuid.UUID {
			if idx+1 < len(steps) {
				return &steps[idx+1].id
			}
			return nil
		}
		if len(bc.Branches) == 0 {
			return linearNext() // pure linear step
		}
		branch, matched := evaluateBranchConditions(&bc, prog, now)
		if !matched {
			return linearNext() // branches exist but none matched (no catch-all)
		}
		return branch.TargetSequenceID // uuid target, or nil = explicit STOP
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
		       lp.sequence_id, lp.sent_at, lp.opened_at, lp.clicked_at, lp.replied_at,
		       COALESCE(ss.ids, '{}') AS sent_ids
		FROM campaign_leads cl
		JOIN contacts c ON c.id = cl.contact_id
		LEFT JOIN LATERAL (
			SELECT sequence_id, sent_at, opened_at, clicked_at, replied_at
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
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var contactID uuid.UUID
		var lastSeq *uuid.UUID
		var sentAt, openedAt, clickedAt, repliedAt *time.Time
		var sentIDs []uuid.UUID
		if serr := rows.Scan(&contactID, &lastSeq, &sentAt, &openedAt, &clickedAt, &repliedAt, &sentIDs); serr != nil {
			return nil, serr
		}

		isNew := lastSeq == nil
		var routed *uuid.UUID
		if isNew {
			if excludeNewLeads {
				continue
			}
			e := entry
			routed = &e
		} else {
			prog := &CampaignContactProgress{
				CampaignID: campaignID, ContactID: contactID, SequenceID: *lastSeq,
				SentAt: sentAt, OpenedAt: openedAt, ClickedAt: clickedAt, RepliedAt: repliedAt,
			}
			routed = routeNext(*lastSeq, prog)
		}
		if routed == nil {
			continue // contact has reached the end / a STOP
		}
		// Loop guard: never re-send a step the contact already received.
		already := false
		for _, sid := range sentIDs {
			if sid == *routed {
				already = true
				break
			}
		}
		if already {
			continue
		}
		return &ContactSequencePair{ContactID: contactID, SequenceID: *routed, IsNewLead: isNew}, nil
	}
	return nil, rows.Err()
}

// GetCurrentSequenceForContact returns the contact's most recently sent step in
// a campaign (its current step). nil when nothing has been sent yet.
func (r *campaignProgressRepository) GetCurrentSequenceForContact(ctx context.Context, campaignID, contactID uuid.UUID) (*uuid.UUID, error) {
	query := `
		SELECT sequence_id
		FROM campaign_contact_progress
		WHERE campaign_id = $1
		  AND contact_id = $2
		  AND sent_at IS NOT NULL
		ORDER BY sent_at DESC
		LIMIT 1
	`
	var seqID uuid.UUID
	if err := r.db.QueryRow(ctx, query, campaignID, contactID).Scan(&seqID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &seqID, nil
}

// HasSentSequence reports whether a step has already been sent to a contact.
func (r *campaignProgressRepository) HasSentSequence(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM campaign_contact_progress
			WHERE campaign_id = $1
			  AND contact_id = $2
			  AND sequence_id = $3
			  AND sent_at IS NOT NULL
		)
	`
	var exists bool
	err := r.db.QueryRow(ctx, query, campaignID, contactID, sequenceID).Scan(&exists)
	return exists, err
}

// ResolveSequenceBranch reads the branching tree on currentSequenceID and the
// contact's engagement for that step, then returns the branch decision. See the
// interface doc for the (target, error) contract. Keeps existing linear
// behaviour by returning ErrUseLinearProgression whenever conditions are empty
// or nothing matches.
func (r *campaignProgressRepository) ResolveSequenceBranch(ctx context.Context, campaignID, contactID, currentSequenceID uuid.UUID) (*uuid.UUID, error) {
	// Load the current step's conditions jsonb. The step must belong to this
	// campaign (defence-in-depth: a stale/foreign sequence id can never branch).
	var raw []byte
	err := r.db.QueryRow(ctx,
		`SELECT conditions FROM sequences WHERE id = $1 AND campaign_id = $2`,
		currentSequenceID, campaignID,
	).Scan(&raw)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrUseLinearProgression
		}
		return nil, err
	}

	if len(raw) == 0 {
		return nil, ErrUseLinearProgression
	}
	var bc models.BranchConditions
	if uerr := json.Unmarshal(raw, &bc); uerr != nil {
		// Malformed stored tree: fail safe to linear rather than stopping or
		// erroring the whole schedule pass.
		return nil, ErrUseLinearProgression
	}
	if len(bc.Branches) == 0 {
		return nil, ErrUseLinearProgression
	}

	// Load engagement for this contact on the CURRENT step. Absence of a row
	// means "no engagement yet" — every signal is treated as not-happened.
	prog := &CampaignContactProgress{CampaignID: campaignID, ContactID: contactID, SequenceID: currentSequenceID}
	scanErr := r.db.QueryRow(ctx,
		`SELECT sent_at, opened_at, clicked_at, replied_at, bounced_at, complained_at
		 FROM campaign_contact_progress
		 WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3`,
		campaignID, contactID, currentSequenceID,
	).Scan(&prog.SentAt, &prog.OpenedAt, &prog.ClickedAt, &prog.RepliedAt, &prog.BouncedAt, &prog.ComplainedAt)
	if scanErr != nil && scanErr != sql.ErrNoRows {
		return nil, scanErr
	}

	branch, ok := evaluateBranchConditions(&bc, prog, time.Now())
	if !ok {
		// No branch matched → keep linear progression.
		return nil, ErrUseLinearProgression
	}

	// Matched. nil target = explicit STOP; otherwise jump to the target step.
	return branch.TargetSequenceID, nil
}

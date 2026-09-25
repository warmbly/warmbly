package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InboxTagResult is one classified inbound message.
type InboxTagResult struct {
	ID               uuid.UUID
	OrganizationID   uuid.UUID
	EmailAccountID   uuid.UUID
	MessageID        string
	ThreadID         string
	Kind             string
	KindConfidence   float64
	KindSource       string
	Intent           string
	IntentConfidence float64
	Relevance        int
	Priority         string
	NeedsReview      bool
	ReviewReason     string
	// Automated is a trusted verdict that no person wrote the message. It is
	// mirrored onto unibox_emails.automated, which is what keeps the
	// conversation out of the inbox.
	Automated   bool
	Answers     json.RawMessage
	Labels      []string
	Model       string
	InputTokens int
	// Actions is what the workspace's switches let this verdict do: "hold",
	// "stop", "task", "suppress". Empty for a verdict that only labelled.
	Actions   []string
	CreatedAt time.Time
}

type InboxTagRepository interface {
	Claim(ctx context.Context, orgID, accountID uuid.UUID, messageID, threadID string) (bool, error)
	ReleaseClaim(ctx context.Context, orgID uuid.UUID, messageID string) error
	Save(ctx context.Context, r *InboxTagResult) error
	// ListForReview backs the phase-1 review page: what was decided, how
	// confident it was, and what it would have done.
	ListForReview(ctx context.Context, orgID uuid.UUID, limit, offset int, needsReviewOnly bool) ([]InboxTagResult, int, error)
	ReviewSummary(ctx context.Context, orgID uuid.UUID) (InboxTagReviewSummary, error)

	// ListUntagged and PreviousOutbound back the historical backfill.
	ListUntagged(ctx context.Context, orgID uuid.UUID, since time.Time, limit int) ([]BackfillCandidate, error)
	PreviousOutbound(ctx context.Context, accountID uuid.UUID, threadID string, before time.Time) (string, string, error)

	// ThreadStates backs the follow-up sweep: who spoke last, when, and how far
	// the thread ever got.
	ThreadStates(ctx context.Context, orgID uuid.UUID, since time.Time, limit int) ([]ThreadFollowUpState, error)

	// GetByMessageID reads one completed verdict, so the reply classifier, the
	// inbox agent and the action executor can reuse a judgment already paid
	// for. Nil when the message was never classified.
	GetByMessageID(ctx context.Context, orgID uuid.UUID, messageID string) (*InboxTagResult, error)
	// RecordActions stores what a verdict was allowed to do.
	RecordActions(ctx context.Context, orgID uuid.UUID, messageID string, actions []string) error
}

type inboxTagRepository struct {
	db *pgxpool.Pool
}

func NewInboxTagRepository(db *pgxpool.Pool) InboxTagRepository {
	return &inboxTagRepository{db: db}
}

func (r *inboxTagRepository) Claim(ctx context.Context, orgID, accountID uuid.UUID, messageID, threadID string) (bool, error) {
	if messageID == "" {
		return false, nil
	}
	const q = `
		INSERT INTO inbox_tag_results (
			organization_id, email_account_id, message_id, thread_id, status, claimed_at
		) VALUES ($1, $2, $3, $4, 'processing', NOW())
		ON CONFLICT (organization_id, message_id) DO UPDATE
		SET email_account_id = EXCLUDED.email_account_id,
		    thread_id = EXCLUDED.thread_id,
		    claimed_at = NOW(),
		    updated_at = NOW()
		WHERE inbox_tag_results.status = 'processing'
		  AND inbox_tag_results.claimed_at < NOW() - INTERVAL '15 minutes'
		RETURNING id
	`
	var id uuid.UUID
	if err := r.db.QueryRow(ctx, q, orgID, accountID, messageID, threadID).Scan(&id); err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *inboxTagRepository) ReleaseClaim(ctx context.Context, orgID uuid.UUID, messageID string) error {
	_, err := r.db.Exec(ctx, `
		DELETE FROM inbox_tag_results
		WHERE organization_id = $1 AND message_id = $2 AND status = 'processing'
	`, orgID, messageID)
	return err
}

func (r *inboxTagRepository) Save(ctx context.Context, res *InboxTagResult) error {
	// One statement, so the verdict and the inbox placement cannot disagree.
	const q = `
		WITH saved AS (
			INSERT INTO inbox_tag_results (
				organization_id, email_account_id, message_id, thread_id,
				kind, kind_confidence, kind_source, intent, intent_confidence,
				relevance, priority, needs_review, review_reason, answers, labels, model, input_tokens,
				automated
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
			ON CONFLICT (organization_id, message_id) DO UPDATE SET
				email_account_id = EXCLUDED.email_account_id,
				thread_id = EXCLUDED.thread_id,
				kind = EXCLUDED.kind,
				kind_confidence = EXCLUDED.kind_confidence,
				kind_source = EXCLUDED.kind_source,
				intent = EXCLUDED.intent,
				intent_confidence = EXCLUDED.intent_confidence,
				relevance = EXCLUDED.relevance,
				priority = EXCLUDED.priority,
				needs_review = EXCLUDED.needs_review,
				review_reason = EXCLUDED.review_reason,
				answers = EXCLUDED.answers,
				labels = EXCLUDED.labels,
				model = EXCLUDED.model,
				input_tokens = EXCLUDED.input_tokens,
				automated = EXCLUDED.automated,
				status = 'complete',
				updated_at = NOW()
			RETURNING email_account_id, message_id, automated
		)
		UPDATE unibox_emails ue
		SET automated = saved.automated
		FROM saved
		WHERE ue.email_id = saved.email_account_id
		  AND ue.message_id = saved.message_id
		  AND ue.automated IS DISTINCT FROM saved.automated
	`
	answers := res.Answers
	if len(answers) == 0 {
		answers = json.RawMessage(`{}`)
	}
	labels := res.Labels
	if labels == nil {
		labels = []string{}
	}
	_, err := r.db.Exec(ctx, q,
		res.OrganizationID, res.EmailAccountID, res.MessageID, res.ThreadID,
		res.Kind, res.KindConfidence, res.KindSource, res.Intent, res.IntentConfidence,
		res.Relevance, res.Priority, res.NeedsReview, res.ReviewReason, answers, labels, res.Model, res.InputTokens,
		res.Automated,
	)
	return err
}

func (r *inboxTagRepository) ListForReview(ctx context.Context, orgID uuid.UUID, limit, offset int, needsReviewOnly bool) ([]InboxTagResult, int, error) {
	where := `WHERE organization_id = $1 AND status = 'complete'`
	if needsReviewOnly {
		where += ` AND needs_review`
	}

	var total int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM inbox_tag_results `+where, orgID).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.db.Query(ctx, `
		SELECT `+inboxTagColumns+`
		FROM inbox_tag_results `+where+`
		ORDER BY relevance DESC, created_at DESC
		LIMIT $2 OFFSET $3`, orgID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]InboxTagResult, 0, limit)
	for rows.Next() {
		x, err := scanInboxTag(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, x)
	}
	return out, total, rows.Err()
}

const inboxTagColumns = `id, organization_id, email_account_id, message_id, thread_id,
		       kind, kind_confidence, kind_source, intent, intent_confidence,
		       relevance, priority, needs_review, review_reason, answers, labels, model, input_tokens, actions, created_at`

func scanInboxTag(row pgx.Row) (InboxTagResult, error) {
	var x InboxTagResult
	err := row.Scan(
		&x.ID, &x.OrganizationID, &x.EmailAccountID, &x.MessageID, &x.ThreadID,
		&x.Kind, &x.KindConfidence, &x.KindSource, &x.Intent, &x.IntentConfidence,
		&x.Relevance, &x.Priority, &x.NeedsReview, &x.ReviewReason, &x.Answers, &x.Labels, &x.Model, &x.InputTokens, &x.Actions, &x.CreatedAt,
	)
	return x, err
}

func (r *inboxTagRepository) GetByMessageID(ctx context.Context, orgID uuid.UUID, messageID string) (*InboxTagResult, error) {
	if messageID == "" {
		return nil, nil
	}
	x, err := scanInboxTag(r.db.QueryRow(ctx, `
		SELECT `+inboxTagColumns+`
		FROM inbox_tag_results
		WHERE organization_id = $1 AND message_id = $2 AND status = 'complete'`, orgID, messageID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &x, nil
}

func (r *inboxTagRepository) RecordActions(ctx context.Context, orgID uuid.UUID, messageID string, actions []string) error {
	if actions == nil {
		actions = []string{}
	}
	_, err := r.db.Exec(ctx, `
		UPDATE inbox_tag_results SET actions = $3, updated_at = NOW()
		WHERE organization_id = $1 AND message_id = $2`, orgID, messageID, actions)
	return err
}

type InboxTagReviewSummary struct {
	Total       int
	NeedsReview int
	FromOffline int
	// Acted counts verdicts that held, stopped, opened a task or suppressed.
	Acted int
}

func (r *inboxTagRepository) ReviewSummary(ctx context.Context, orgID uuid.UUID) (InboxTagReviewSummary, error) {
	const q = `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE needs_review),
		       COUNT(*) FILTER (WHERE kind_source = 'header'),
		       COUNT(*) FILTER (WHERE cardinality(actions) > 0)
		FROM inbox_tag_results
		WHERE organization_id = $1 AND status = 'complete'
	`
	var out InboxTagReviewSummary
	err := r.db.QueryRow(ctx, q, orgID).Scan(&out.Total, &out.NeedsReview, &out.FromOffline, &out.Acted)
	return out, err
}

// BackfillCandidate is one historical message the backfill may classify.
type BackfillCandidate struct {
	EmailAccountID uuid.UUID
	UserID         uuid.UUID
	MessageID      string
	ThreadID       string
	Subject        string
	BodyText       string
	FromAddr       string
	InReplyTo      []string
	// Flags carries the classification headers the sync stores as pseudo-flags.
	Flags        []string
	InternalDate time.Time
}

// ListUntagged returns inbound messages that have never been classified, newest
// first, for the backfill.
//
// Three exclusions, all deliberate:
//
//   - folder = 'inbox' only. Our own sends are never classified, and the folder
//     is the fact that says which is which. Reading direction from content is
//     how our own outbound gets labelled a human reply at 0.94 confidence.
//   - a sender that is one of our own mailboxes is dropped even inside the
//     inbox folder: mail between two connected mailboxes lands in the second
//     one's inbox and is still ours.
//   - anything already in inbox_tag_results, so a re-run resumes rather than
//     repeats. Same key the live path is idempotent on.
func (r *inboxTagRepository) ListUntagged(ctx context.Context, orgID uuid.UUID, since time.Time, limit int) ([]BackfillCandidate, error) {
	const q = `
		SELECT ue.email_id, ue.user_id, ue.message_id, ue.thread_id,
		       ue.subject, ue.body_text, COALESCE(ue.from_addr[1], ''), ue.in_reply_to, ue.flags, ue.internal_date
		FROM unibox_emails ue
		JOIN email_accounts ea ON ea.id = ue.email_id
		WHERE ea.organization_id = $1
		  AND ue.folder = 'inbox'
		  AND ue.internal_date >= $2
		  AND ue.message_id <> ''
		  AND LOWER(COALESCE(
		        NULLIF((regexp_match(COALESCE(ue.from_addr[1], ''), '<([^<>]+)>\s*$'))[1], ''),
		        NULLIF((regexp_match(COALESCE(ue.from_addr[1], ''), '\(([^()]+)\)\s*$'))[1], ''),
		        TRIM(COALESCE(ue.from_addr[1], ''))
		      ))
		      NOT IN (SELECT LOWER(email) FROM email_accounts WHERE organization_id = $1)
		  AND NOT EXISTS (
		        SELECT 1 FROM inbox_tag_results r
			        WHERE r.organization_id = $1 AND r.message_id = ue.message_id
			          AND (r.status = 'complete' OR r.claimed_at >= NOW() - INTERVAL '15 minutes')
		      )
		ORDER BY ue.internal_date DESC
		LIMIT $3
	`
	rows, err := r.db.Query(ctx, q, orgID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BackfillCandidate
	for rows.Next() {
		var c BackfillCandidate
		if err := rows.Scan(&c.EmailAccountID, &c.UserID, &c.MessageID, &c.ThreadID,
			&c.Subject, &c.BodyText, &c.FromAddr, &c.InReplyTo, &c.Flags, &c.InternalDate); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PreviousOutbound is the plain text of the last message we sent in a thread
// before a given moment.
//
// Without it a reply cannot be read: "yes", "that works" and "sounds good" are
// answers, and the question they answer is not in them. Giving the model our
// side of the exchange is what lets the reply mean anything.
func (r *inboxTagRepository) PreviousOutbound(ctx context.Context, accountID uuid.UUID, threadID string, before time.Time) (string, string, error) {
	if threadID == "" {
		return "", "", nil
	}
	const q = `
		SELECT ue.body_text, COALESCE(c.name, '')
		FROM unibox_emails ue
		LEFT JOIN tasks t
		       ON t.email_account_id = ue.email_id
		      AND t.task_type = 'campaign'
		      AND BTRIM(t.message_id, '<> ') = BTRIM(ue.message_id, '<> ')
		LEFT JOIN campaign_tasks ct ON ct.task_id = t.id
		LEFT JOIN campaigns c ON c.id = ct.campaign_id
		WHERE ue.email_id = $1 AND ue.thread_id = $2 AND ue.folder = 'sent' AND ue.internal_date < $3
		ORDER BY ue.internal_date DESC
		LIMIT 1
	`
	var body, campaign string
	if err := r.db.QueryRow(ctx, q, accountID, threadID, before).Scan(&body, &campaign); err != nil {
		// No previous message is the normal case for the first inbound of a
		// thread, not an error worth failing a classification over.
		return "", "", nil
	}
	return body, campaign, nil
}

// ThreadFollowUpState is one thread's follow-up facts. Every field is read from
// the database; none of it is inferred, and none of it is asked of a model.
type ThreadFollowUpState struct {
	ThreadID       string
	LastInboundAt  time.Time
	LastOutboundAt time.Time
	// BestIntent is the most recent trusted intent in this thread.
	BestIntent string
	// LastKind is the classified kind of the newest inbound message, which is
	// what says whether the "reply" was a person or a mail server.
	LastKind string
}

// ThreadStates returns follow-up facts for every thread with activity since a
// cutoff.
//
// Scoped through email_accounts because unibox_emails carries no organization
// of its own. Follow-up labels use the same organization-plus-thread key as the
// rest of the unibox.
func (r *inboxTagRepository) ThreadStates(ctx context.Context, orgID uuid.UUID, since time.Time, limit int) ([]ThreadFollowUpState, error) {
	const q = `
	WITH scoped_emails AS (
		SELECT ue.*
		FROM unibox_emails ue
		JOIN email_accounts ea ON ea.id = ue.email_id
		WHERE ea.organization_id = $1 AND ue.thread_id <> ''
	),
	active_threads AS (
		SELECT DISTINCT thread_id
		FROM scoped_emails
		WHERE internal_date >= $2
	),
	threads AS (
			SELECT ue.thread_id,
			       MAX(ue.internal_date) FILTER (WHERE ue.folder = 'inbox') AS last_in,
			       MAX(ue.internal_date) FILTER (WHERE ue.folder = 'sent')  AS last_out
			FROM scoped_emails ue
			JOIN active_threads active ON active.thread_id = ue.thread_id
			GROUP BY ue.thread_id
	),
	latest_inbound AS (
		SELECT DISTINCT ON (ue.thread_id)
		       ue.thread_id, ue.email_id, ue.message_id
		FROM scoped_emails ue
		JOIN active_threads active ON active.thread_id = ue.thread_id
		WHERE ue.folder = 'inbox'
		ORDER BY ue.thread_id, ue.internal_date DESC
		)
		SELECT t.thread_id, t.last_in, t.last_out,
		       COALESCE(best.intent, ''), COALESCE(newest.kind, '')
		FROM threads t
		LEFT JOIN LATERAL (
			SELECT r.intent
			FROM inbox_tag_results r
			JOIN scoped_emails ue
			  ON ue.email_id = r.email_account_id
			 AND ue.thread_id = r.thread_id
			 AND ue.message_id = r.message_id
			WHERE r.organization_id = $1 AND r.thread_id = t.thread_id
			  AND r.status = 'complete' AND r.review_reason <> 'intent' AND r.intent <> ''
			ORDER BY ue.internal_date DESC, r.created_at DESC LIMIT 1
		) best ON TRUE
		LEFT JOIN latest_inbound latest ON latest.thread_id = t.thread_id
		LEFT JOIN inbox_tag_results newest
		  ON newest.organization_id = $1
		 AND newest.email_account_id = latest.email_id
		 AND newest.thread_id = latest.thread_id
		 AND newest.message_id = latest.message_id
		 AND newest.status = 'complete'
		 AND newest.review_reason <> 'kind'
		WHERE t.last_out IS NOT NULL
		ORDER BY GREATEST(COALESCE(t.last_in, 'epoch'::timestamptz), t.last_out) DESC
		LIMIT $3
	`
	rows, err := r.db.Query(ctx, q, orgID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ThreadFollowUpState
	for rows.Next() {
		var st ThreadFollowUpState
		var lastIn, lastOut *time.Time
		if err := rows.Scan(&st.ThreadID, &lastIn, &lastOut, &st.BestIntent, &st.LastKind); err != nil {
			return nil, err
		}
		if lastIn != nil {
			st.LastInboundAt = *lastIn
		}
		if lastOut != nil {
			st.LastOutboundAt = *lastOut
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

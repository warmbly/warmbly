package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

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
	// Position is the thread's newest message on a cycle page; zero on the changed-thread check.
	Position FollowUpPosition
}

// FollowUpPosition is one message row in the walk: mailboxes by id, then (internal_date, id) descending.
type FollowUpPosition struct {
	MailboxID uuid.UUID
	At        time.Time
	RowID     uuid.UUID
}

// FollowUpPage is one bounded step of the walk.
type FollowUpPage struct {
	// States are the page's threads that we have written to, in walk order.
	States []ThreadFollowUpState
	// Rows is how many messages the page read; fewer than the limit ends the mailbox.
	Rows int
	// Last is the last message read, nil when the page was empty.
	Last *FollowUpPosition
}

// FollowUpMark is a place in the stream of stored messages and written verdicts, ordered by (At, RowID).
type FollowUpMark struct {
	At    time.Time
	RowID uuid.UUID
}

// FollowUpChange is one message stored or verdict written.
type FollowUpChange struct {
	At       time.Time
	RowID    uuid.UUID
	ThreadID string
}

// FollowUpSweepState is a workspace's sweep bookkeeping as its walker claimed it.
type FollowUpSweepState struct {
	// Cursor is where the cycle stopped; nil starts a cycle at the newest.
	Cursor *FollowUpPosition
	// Fresh is how far changed threads have been checked; nil when never.
	Fresh         *FollowUpMark
	PageFailures  int
	FreshFailures int
	// PageFailingSince and FreshFailingSince are when the current run of failures began.
	PageFailingSince  *time.Time
	FreshFailingSince *time.Time
	// Now is the database clock at the claim.
	Now time.Time
}

func (r *inboxTagRepository) FollowUpMailboxes(ctx context.Context, orgID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `SELECT id FROM email_accounts WHERE organization_id = $1 ORDER BY id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// followUpPageSQL is one mailbox's next messages, newest first after an optional keyset; $1 org, $2 mailbox, $3 since, $4 limit.
func followUpPageSQL(keyset string) string {
	return `page AS MATERIALIZED (
		SELECT ue.id, ue.internal_date, ue.thread_id
		FROM unibox_emails ue
		WHERE ue.email_id = $2
		  AND EXISTS (SELECT 1 FROM email_accounts ea WHERE ea.id = $2 AND ea.organization_id = $1)
		  AND ue.internal_date >= $3 ` + keyset + `
		  AND ue.thread_id <> ''
		ORDER BY ue.internal_date DESC, ue.id DESC
		LIMIT $4
	)`
}

// followUpStatesSQL resolves follow-up facts for the threads in a heads(id, thread_id) CTE; $1 is the org.
// MATERIALIZED so the facts resolve once per query, not once per page row when the planner nests the join.
const followUpStatesSQL = `states AS MATERIALIZED (
		SELECT h.id, h.thread_id, agg.last_in, agg.last_out, agg.last_any,
		       COALESCE(best.intent, '') AS intent, COALESCE(newest.kind, '') AS kind
		FROM heads h
		CROSS JOIN LATERAL (
			SELECT MAX(o.internal_date) FILTER (WHERE o.folder = 'inbox') AS last_in,
			       MAX(o.internal_date) FILTER (WHERE o.folder = 'sent')  AS last_out,
			       MAX(o.internal_date) AS last_any
			FROM unibox_emails o
			JOIN email_accounts oa ON oa.id = o.email_id AND oa.organization_id = $1
			WHERE o.thread_id = h.thread_id
		) agg
		LEFT JOIN LATERAL (
			SELECT o.email_id, o.message_id
			FROM unibox_emails o
			JOIN email_accounts oa ON oa.id = o.email_id AND oa.organization_id = $1
			WHERE o.thread_id = h.thread_id AND o.folder = 'inbox'
			ORDER BY o.internal_date DESC, o.id DESC
			LIMIT 1
		) latest ON TRUE
		LEFT JOIN LATERAL (
			SELECT r.intent
			FROM inbox_tag_results r
			JOIN unibox_emails ue
			  ON ue.email_id = r.email_account_id
			 AND ue.thread_id = r.thread_id
			 AND ue.message_id = r.message_id
			JOIN email_accounts oa ON oa.id = ue.email_id AND oa.organization_id = $1
			WHERE r.organization_id = $1 AND r.thread_id = h.thread_id
			  AND r.status = 'complete' AND r.review_reason <> 'intent' AND r.intent <> ''
			ORDER BY ue.internal_date DESC, r.created_at DESC
			LIMIT 1
		) best ON TRUE
		LEFT JOIN inbox_tag_results newest
		  ON newest.organization_id = $1
		 AND newest.email_account_id = latest.email_id
		 AND newest.thread_id = h.thread_id
		 AND newest.message_id = latest.message_id
		 AND newest.status = 'complete'
		 AND newest.review_reason <> 'kind'
		WHERE agg.last_out IS NOT NULL
	)`

// FollowUpPage reads one bounded page of a mailbox and resolves facts only for threads whose newest workspace message is on it.
func (r *inboxTagRepository) FollowUpPage(ctx context.Context, orgID, mailboxID uuid.UUID, since time.Time, after *FollowUpPosition, limit int) (FollowUpPage, error) {
	args, keyset := followUpPageArgs(orgID, mailboxID, since, after, limit)
	q := `WITH ` + followUpPageSQL(keyset) + `,
	heads AS (
		SELECT p.id, p.thread_id
		FROM page p
		WHERE NOT EXISTS (
			SELECT 1
			FROM unibox_emails o
			JOIN email_accounts oa ON oa.id = o.email_id AND oa.organization_id = $1
			WHERE o.thread_id = p.thread_id
			  AND (o.internal_date, o.id) > (p.internal_date, p.id)
		)
	),
	` + followUpStatesSQL + `
	SELECT p.id, p.internal_date, p.thread_id, s.id IS NOT NULL,
	       s.last_in, s.last_out, COALESCE(s.intent, ''), COALESCE(s.kind, '')
	FROM page p
	LEFT JOIN states s ON s.id = p.id
	ORDER BY p.internal_date DESC, p.id DESC
	`
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return FollowUpPage{}, err
	}
	defer rows.Close()

	var out FollowUpPage
	for rows.Next() {
		var (
			pos             = FollowUpPosition{MailboxID: mailboxID}
			threadID        string
			due             bool
			lastIn, lastOut *time.Time
			intent, kind    string
		)
		if err := rows.Scan(&pos.RowID, &pos.At, &threadID, &due, &lastIn, &lastOut, &intent, &kind); err != nil {
			return FollowUpPage{}, err
		}
		out.Rows++
		last := pos
		out.Last = &last
		if due {
			out.States = append(out.States, followUpState(threadID, lastIn, lastOut, intent, kind, pos))
		}
	}
	return out, rows.Err()
}

// FollowUpPagePositions reads only where a page ends, to step past a page whose facts cannot be read.
func (r *inboxTagRepository) FollowUpPagePositions(ctx context.Context, orgID, mailboxID uuid.UUID, since time.Time, after *FollowUpPosition, limit int) (FollowUpPage, error) {
	args, keyset := followUpPageArgs(orgID, mailboxID, since, after, limit)
	rows, err := r.db.Query(ctx, `WITH `+followUpPageSQL(keyset)+`
		SELECT id, internal_date FROM page ORDER BY internal_date DESC, id DESC`, args...)
	if err != nil {
		return FollowUpPage{}, err
	}
	defer rows.Close()
	var out FollowUpPage
	for rows.Next() {
		pos := FollowUpPosition{MailboxID: mailboxID}
		if err := rows.Scan(&pos.RowID, &pos.At); err != nil {
			return FollowUpPage{}, err
		}
		out.Rows++
		out.Last = &pos
	}
	return out, rows.Err()
}

func followUpPageArgs(orgID, mailboxID uuid.UUID, since time.Time, after *FollowUpPosition, limit int) ([]any, string) {
	args := []any{orgID, mailboxID, since, limit}
	if after == nil {
		return args, ""
	}
	return append(args, after.At, after.RowID), `AND (ue.internal_date, ue.id) < ($5, $6)`
}

func followUpState(threadID string, lastIn, lastOut *time.Time, intent, kind string, pos FollowUpPosition) ThreadFollowUpState {
	st := ThreadFollowUpState{ThreadID: threadID, BestIntent: intent, LastKind: kind, Position: pos}
	if lastIn != nil {
		st.LastInboundAt = *lastIn
	}
	if lastOut != nil {
		st.LastOutboundAt = *lastOut
	}
	return st
}

// FollowUpChanges reads the next messages stored and verdicts written in a workspace after a mark, up to until.
func (r *inboxTagRepository) FollowUpChanges(ctx context.Context, orgID uuid.UUID, after FollowUpMark, until time.Time, limit int) ([]FollowUpChange, error) {
	rows, err := r.db.Query(ctx, `
		SELECT c.at, c.id, c.thread_id
		FROM (
			SELECT m.at, m.id, m.thread_id
			FROM email_accounts ea
			CROSS JOIN LATERAL (
				SELECT ue.ingested_at AS at, ue.id, ue.thread_id
				FROM unibox_emails ue
				WHERE ue.email_id = ea.id
				  AND (ue.ingested_at, ue.id) > ($2, $3) AND ue.ingested_at <= $4
				  AND ue.thread_id <> ''
				ORDER BY ue.ingested_at, ue.id
				LIMIT $5
			) m
			WHERE ea.organization_id = $1
			UNION ALL
			(SELECT r.updated_at, r.id, r.thread_id
			 FROM inbox_tag_results r
			 WHERE r.organization_id = $1
			   AND (r.updated_at, r.id) > ($2, $3) AND r.updated_at <= $4
			   AND r.thread_id <> ''
			 ORDER BY r.updated_at, r.id
			 LIMIT $5)
		) c
		ORDER BY c.at, c.id
		LIMIT $5`, orgID, after.At, after.RowID, until, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FollowUpChange
	for rows.Next() {
		var c FollowUpChange
		if err := rows.Scan(&c.At, &c.RowID, &c.ThreadID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// FollowUpThreadStates resolves follow-up facts for named threads with activity since a cutoff.
func (r *inboxTagRepository) FollowUpThreadStates(ctx context.Context, orgID uuid.UUID, threadIDs []string, since time.Time) ([]ThreadFollowUpState, error) {
	if len(threadIDs) == 0 {
		return nil, nil
	}
	rows, err := r.db.Query(ctx, `
	WITH heads AS (
		SELECT DISTINCT NULL::uuid AS id, t AS thread_id FROM unnest($2::text[]) AS t WHERE t <> ''
	),
	`+followUpStatesSQL+`
	SELECT s.thread_id, s.last_in, s.last_out, s.intent, s.kind
	FROM states s
	WHERE s.last_any >= $3
	ORDER BY s.thread_id`, orgID, threadIDs, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ThreadFollowUpState
	for rows.Next() {
		var (
			threadID, intent, kind string
			lastIn, lastOut        *time.Time
		)
		if err := rows.Scan(&threadID, &lastIn, &lastOut, &intent, &kind); err != nil {
			return nil, err
		}
		out = append(out, followUpState(threadID, lastIn, lastOut, intent, kind, FollowUpPosition{}))
	}
	return out, rows.Err()
}

// ClaimFollowUpSweep takes the workspace's sweep lease and returns its state; nil while another walker holds it.
func (r *inboxTagRepository) ClaimFollowUpSweep(ctx context.Context, orgID, owner uuid.UUID, lease time.Duration) (*FollowUpSweepState, error) {
	var (
		st                    FollowUpSweepState
		mailbox, row, freshID *uuid.UUID
		at, freshAt           *time.Time
	)
	err := r.db.QueryRow(ctx, `
		INSERT INTO inbox_follow_up_sweeps (organization_id, lease_owner, leased_until, updated_at)
		VALUES ($1, $2, NOW() + make_interval(secs => $3), NOW())
		ON CONFLICT (organization_id) DO UPDATE SET
			lease_owner = EXCLUDED.lease_owner,
			leased_until = EXCLUDED.leased_until,
			updated_at = NOW()
		WHERE inbox_follow_up_sweeps.leased_until IS NULL
		   OR inbox_follow_up_sweeps.leased_until <= NOW()
		   OR inbox_follow_up_sweeps.lease_owner = EXCLUDED.lease_owner
		RETURNING email_account_id, internal_date, message_row_id, fresh_at, fresh_row_id,
		          page_failures, fresh_failures, page_failing_since, fresh_failing_since, NOW()`, orgID, owner, lease.Seconds(),
	).Scan(&mailbox, &at, &row, &freshAt, &freshID, &st.PageFailures, &st.FreshFailures,
		&st.PageFailingSince, &st.FreshFailingSince, &st.Now)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if mailbox != nil && at != nil && row != nil {
		st.Cursor = &FollowUpPosition{MailboxID: *mailbox, At: *at, RowID: *row}
	}
	if freshAt != nil {
		st.Fresh = &FollowUpMark{At: *freshAt}
		if freshID != nil {
			st.Fresh.RowID = *freshID
		}
	}
	return &st, nil
}

// SaveFollowUpSweep stores the state and renews the lease; false once another walker has taken it over.
func (r *inboxTagRepository) SaveFollowUpSweep(ctx context.Context, orgID, owner uuid.UUID, lease time.Duration, st FollowUpSweepState) (bool, error) {
	var mailbox, row, freshID *uuid.UUID
	var at, freshAt *time.Time
	if st.Cursor != nil {
		mailbox, at, row = &st.Cursor.MailboxID, &st.Cursor.At, &st.Cursor.RowID
	}
	if st.Fresh != nil {
		freshAt, freshID = &st.Fresh.At, &st.Fresh.RowID
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE inbox_follow_up_sweeps SET
			email_account_id = $3, internal_date = $4, message_row_id = $5,
			fresh_at = $6, fresh_row_id = $7,
			page_failures = $8, fresh_failures = $9,
			page_failing_since = $10, fresh_failing_since = $11,
			leased_until = NOW() + make_interval(secs => $12),
			updated_at = NOW()
		WHERE organization_id = $1 AND lease_owner = $2`,
		orgID, owner, mailbox, at, row, freshAt, freshID, st.PageFailures, st.FreshFailures,
		st.PageFailingSince, st.FreshFailingSince, lease.Seconds())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ReleaseFollowUpSweep gives the lease back so the next pass need not wait for it to lapse.
func (r *inboxTagRepository) ReleaseFollowUpSweep(ctx context.Context, orgID, owner uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE inbox_follow_up_sweeps SET leased_until = NULL, updated_at = NOW()
		WHERE organization_id = $1 AND lease_owner = $2`, orgID, owner)
	return err
}

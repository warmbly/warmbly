package repository

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// The batched forms of the per-mailbox reads a status page needs, so a page
// costs a fixed number of round trips instead of several per mailbox. Each
// matches its single-mailbox form row for row.

// GetPartnerDiversityForAccounts is GetPartnerDiversity over a set; every
// requested id is present, zero when nothing falls in the window.
func (r *warmupRepository) GetPartnerDiversityForAccounts(ctx context.Context, accountIDs []uuid.UUID, since time.Time) (map[uuid.UUID]WarmupPartnerDiversity, error) {
	out := make(map[uuid.UUID]WarmupPartnerDiversity, len(accountIDs))
	if len(accountIDs) == 0 {
		return out, nil
	}
	for _, id := range accountIDs {
		out[id] = WarmupPartnerDiversity{}
	}
	rows, err := resultDB(ctx, r.db).Query(ctx, `
		SELECT wt.sender_account_id,
			COUNT(DISTINCT wt.recipient_account_id),
			COUNT(DISTINCT lower(split_part(ea.email, '@', 2))),
			COUNT(DISTINCT ea.organization_id)
		FROM warmup_tokens wt
		JOIN tasks t ON t.id = wt.task_id
		JOIN email_accounts ea ON ea.id = wt.recipient_account_id
		WHERE wt.sender_account_id = ANY($1::uuid[])
		  AND wt.created_at >= $2
		  AND t.status = 'completed'
		  AND wt.sent_message_id <> ''
		GROUP BY 1`, accountIDs, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id uuid.UUID
		var d WarmupPartnerDiversity
		if err := rows.Scan(&id, &d.Mailboxes, &d.Domains, &d.Organizations); err != nil {
			rows.Close()
			return nil, err
		}
		out[id] = d
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = resultDB(ctx, r.db).Query(ctx, `
		SELECT email_account_id, COUNT(*), COUNT(DISTINCT sender_account_id)
		FROM warmup_received
		WHERE email_account_id = ANY($1::uuid[])
		  AND created_at >= $2
		GROUP BY 1`, accountIDs, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var received, senders int
		if err := rows.Scan(&id, &received, &senders); err != nil {
			return nil, err
		}
		d := out[id]
		d.Received, d.Senders = received, senders
		out[id] = d
	}
	return out, rows.Err()
}

// GetCloudStandings is GetCloudStanding over a set; a mailbox Warmbly Cloud
// does not warm is absent.
func (r *warmupRepository) GetCloudStandings(ctx context.Context, accountIDs []uuid.UUID) (map[uuid.UUID]*models.WarmupHealthInfo, error) {
	out := make(map[uuid.UUID]*models.WarmupHealthInfo)
	if len(accountIDs) == 0 {
		return out, nil
	}
	rows, err := resultDB(ctx, r.db).Query(ctx,
		`SELECT `+cloudLinkMailboxColumns+` FROM cloud_link_mailboxes WHERE email_account_id = ANY($1::uuid[])`, accountIDs)
	if err != nil {
		return nil, err
	}
	linked := make(map[uuid.UUID]*models.CloudLinkMailbox)
	for rows.Next() {
		m, err := scanCloudLinkMailbox(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		linked[m.EmailAccountID] = m
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(linked) == 0 {
		return out, err
	}
	return cloudStatuses(ctx, resultDB(ctx, r.db), linked)
}

// SpamPlacementsSinceForAccounts is SpamPlacementsSince over a set; every
// requested id is present, empty when it has none.
func (r *warmupRepository) SpamPlacementsSinceForAccounts(ctx context.Context, accountIDs []uuid.UUID, since time.Time) (map[uuid.UUID][]time.Time, error) {
	out := make(map[uuid.UUID][]time.Time, len(accountIDs))
	if len(accountIDs) == 0 {
		return out, nil
	}
	for _, id := range accountIDs {
		out[id] = nil
	}
	rows, err := resultDB(ctx, r.db).Query(ctx, `
		SELECT reported_account_id, created_at
		FROM warmup_spam_reports sr
		WHERE reported_account_id = ANY($1::uuid[])
		  AND report_type = 'spam_placement'
		  AND created_at >= $2
		  AND `+majorRecipientSQL+`
		ORDER BY reported_account_id, created_at`, accountIDs, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		out[id] = append(out[id], at)
	}
	return out, rows.Err()
}

// SumWarmupSentSinceForAccounts is SumWarmupSentSince over a set; every
// requested id is present.
func (r *warmupRepository) SumWarmupSentSinceForAccounts(ctx context.Context, accountIDs []uuid.UUID, since time.Time) (map[uuid.UUID]int, error) {
	out := make(map[uuid.UUID]int, len(accountIDs))
	if len(accountIDs) == 0 {
		return out, nil
	}
	for _, id := range accountIDs {
		out[id] = 0
	}
	rows, err := resultDB(ctx, r.db).Query(ctx, `
		SELECT email_account_id, COALESCE(SUM(emails_sent), 0)
		FROM warmup_statistics
		WHERE email_account_id = ANY($1::uuid[])
		  AND date >= DATE($2)
		GROUP BY 1`, accountIDs, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var total int
		if err := rows.Scan(&id, &total); err != nil {
			return nil, err
		}
		out[id] = total
	}
	return out, rows.Err()
}

const warmupSendFailuresOneSQL = `WHERE ($1::uuid IS NULL OR e.id = $1)`

// warmupSendFailuresManySQL is warmupSendFailuresSQL scoped to a set of mailboxes.
var warmupSendFailuresManySQL = func() string {
	if !strings.Contains(warmupSendFailuresSQL, warmupSendFailuresOneSQL) {
		panic("warmupSendFailuresSQL no longer carries its mailbox predicate")
	}
	return strings.Replace(warmupSendFailuresSQL, warmupSendFailuresOneSQL, `WHERE e.id = ANY($1::uuid[])`, 1)
}()

// LastWarmupSendFailures is LastWarmupSendFailure over a set; a mailbox with
// nothing to report is absent.
func (r *warmupRepository) LastWarmupSendFailures(ctx context.Context, accountIDs []uuid.UUID, since time.Time) (map[uuid.UUID]*models.WarmupSendFailure, error) {
	out := make(map[uuid.UUID]*models.WarmupSendFailure)
	if len(accountIDs) == 0 {
		return out, nil
	}
	rows, err := resultDB(ctx, r.db).Query(ctx, `
		SELECT id, message, at, first_at FROM (`+warmupSendFailuresManySQL+`) f
		WHERE (message LIKE $2 AND first_at <= $3 - INTERVAL '1 hour' AND at > $3 - INTERVAL '1 hour'
		       AND status = 'active' AND warming AND sending)
		   OR (message NOT LIKE $2 AND at >= $4)
	`, accountIDs, mailboxLoadingPattern, time.Now(), since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var first time.Time
		f := &models.WarmupSendFailure{}
		if err := rows.Scan(&id, &f.Message, &f.At, &first); err != nil {
			return nil, err
		}
		if _, seen := out[id]; seen {
			continue
		}
		if f.Kind = models.WarmupSendFailureKind(f.Message); f.Kind == models.WarmupFailureLoading {
			f.FirstFailureAt = &first
		}
		out[id] = f
	}
	return out, rows.Err()
}

// NextWarmupSends returns each mailbox's earliest pending warmup slot.
func (r *warmupRepository) NextWarmupSends(ctx context.Context, accountIDs []uuid.UUID) (map[uuid.UUID]time.Time, error) {
	out := make(map[uuid.UUID]time.Time)
	if len(accountIDs) == 0 {
		return out, nil
	}
	rows, err := resultDB(ctx, r.db).Query(ctx, `
		SELECT email_account_id, MIN(scheduled_at) FROM tasks
		WHERE email_account_id = ANY($1) AND task_type = 'warmup' AND status = 'pending' AND scheduled_at IS NOT NULL
		GROUP BY email_account_id
	`, accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		out[id] = at
	}
	return out, rows.Err()
}

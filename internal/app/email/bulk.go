package email

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// OnboardSMTPIMAPBulk connects up to config.MailboxBulkBatchMax mailboxes.
//
// The allowance is resolved once up front: rows past what fits are answered
// with mailbox_allowance_reached without dialling anything, and the rows that
// do fit are validated concurrently. Each of those still runs the single
// connect path, so a row is never connected twice and every side effect of a
// single connect (worker load, warmup pool, webhook) happens per mailbox.
func (s *emailService) OnboardSMTPIMAPBulk(ctx context.Context, userID string, orgID *uuid.UUID, rows []models.NewSMTPIMAPAccount) *models.MailboxBulkResult {
	res := &models.MailboxBulkResult{Data: make([]models.MailboxBulkRow, len(rows))}
	res.Summary.Total = len(rows)
	if len(rows) == 0 {
		return res
	}

	fail := func(i int, xerr *errx.Error) {
		res.Data[i] = models.MailboxBulkRow{
			Row: i, Email: rows[i].Email, Status: models.MailboxBulkFailed,
			Code: bulkCode(xerr), Message: xerr.Message,
		}
	}

	// A batch-wide refusal (no org, allowance unreadable) fails every row the
	// same way rather than pretending some rows were tried.
	remaining := len(rows)
	var allowance *models.MailboxAllowance
	if orgID == nil {
		for i := range rows {
			fail(i, errx.ErrNoOrganization)
		}
		res.Summary.Failed = len(rows)
		return res
	}
	if s.allowance != nil {
		a, xerr := s.allowance.MailboxAllowance(ctx, *orgID)
		if xerr != nil {
			for i := range rows {
				fail(i, xerr)
			}
			res.Summary.Failed = len(rows)
			return res
		}
		allowance = a
		if a.Remaining != nil && *a.Remaining < remaining {
			remaining = *a.Remaining
		}
	}

	// Duplicates inside the file are refused before they compete for the
	// allowance; the first occurrence is the one that gets connected.
	seen := make(map[string]bool, len(rows))
	eligible := make([]int, 0, len(rows))
	for i := range rows {
		key := strings.ToLower(strings.TrimSpace(rows[i].Email))
		if key != "" && seen[key] {
			fail(i, errx.NewWithIdentifier(errx.BadRequest, "duplicate_row", "This address appears earlier in the same file."))
			continue
		}
		seen[key] = true
		if len(eligible) >= remaining {
			used, limit := 0, 0
			paid := true
			if allowance != nil {
				used, paid = allowance.Used, allowance.Paid
				if allowance.Allowance != nil {
					limit = *allowance.Allowance
				}
			}
			fail(i, errx.MailboxAllowanceReached(used, limit, paid))
			continue
		}
		eligible = append(eligible, i)
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, config.MailboxBulkConcurrency)
	for _, i := range eligible {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			row := rows[i]
			acc, xerr := s.OnboardSMTPIMAP(ctx, userID, orgID, &row)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case xerr == nil:
				res.Data[i] = models.MailboxBulkRow{Row: i, Email: acc.Email, Status: models.MailboxBulkConnected, ID: &acc.ID}
			case errors.Is(xerr, errx.ErrEmailOnboardAlreadyExists):
				res.Data[i] = models.MailboxBulkRow{
					Row: i, Email: row.Email, Status: models.MailboxBulkSkipped,
					Code: "already_connected", Message: xerr.Message,
				}
			default:
				res.Data[i] = models.MailboxBulkRow{
					Row: i, Email: row.Email, Status: models.MailboxBulkFailed,
					Code: bulkCode(xerr), Message: xerr.Message,
				}
			}
		}(i)
	}
	wg.Wait()

	for _, r := range res.Data {
		switch r.Status {
		case models.MailboxBulkConnected:
			res.Summary.Connected++
		case models.MailboxBulkSkipped:
			res.Summary.Skipped++
		default:
			res.Summary.Failed++
		}
	}
	if s.allowance != nil {
		if a, xerr := s.allowance.MailboxAllowance(ctx, *orgID); xerr == nil {
			res.Allowance = a
		}
	}
	return res
}

// bulkCode is the stable per-row code: the error's own identifier when it
// has one, otherwise the generic one for its HTTP class.
func bulkCode(xerr *errx.Error) string {
	if xerr == nil {
		return ""
	}
	return xerr.ResponseCode()
}

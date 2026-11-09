package mailboxconnect

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// GrantBulkMax caps one batch of handed-over sign-ins; each one is a round trip to the provider.
const GrantBulkMax = 50

const grantBulkParallel = 8

// ConnectGrant connects the mailbox a sign-in the workspace's own app completed belongs to.
// A mailbox the workspace already has gets its token renewed, and its warmup is left as it is.
func (s *Service) ConnectGrant(ctx context.Context, orgID uuid.UUID, userID string, g models.MailboxOAuthGrant, w *models.MailboxConnectWarmup, allow func(uuid.UUID) bool) (*models.Email, bool, string, *errx.Error) {
	if xerr := ValidateWarmup(w); xerr != nil {
		return nil, false, "", xerr
	}
	acc, renewed, xerr := s.mailboxes.ConnectWithGrant(ctx, userID, orgID, g, allow)
	if xerr != nil {
		return nil, false, "", xerr
	}
	if renewed {
		return acc, true, "", nil
	}
	stored, note := s.ApplyWarmup(ctx, orgID, userID, acc.ID, w)
	if stored != nil {
		acc = stored
	}
	return acc, false, note, nil
}

// ConnectGrantBulk hands over many sign-ins with one warmup setting, answering each one.
func (s *Service) ConnectGrantBulk(ctx context.Context, orgID uuid.UUID, userID string, grants []models.MailboxOAuthGrant, w *models.MailboxConnectWarmup, allow func(uuid.UUID) bool) (*models.GrantBulkResult, *errx.Error) {
	if xerr := ValidateWarmup(w); xerr != nil {
		return nil, xerr
	}
	out := &models.GrantBulkResult{Data: make([]models.MailboxBulkRow, len(grants))}
	sem := make(chan struct{}, grantBulkParallel)
	var wg sync.WaitGroup
	for i, g := range grants {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, g models.MailboxOAuthGrant) {
			defer wg.Done()
			defer func() { <-sem }()
			row := models.MailboxBulkRow{Row: i + 1, Email: strings.TrimSpace(g.Email)}
			acc, renewed, note, xerr := s.ConnectGrant(ctx, orgID, userID, g, w, allow)
			switch {
			case xerr != nil:
				row.Status, row.Code, row.Message = models.MailboxBulkFailed, xerr.ResponseCode(), xerr.Message
			case renewed:
				row.Status, row.ID, row.Email = models.MailboxBulkRenewed, &acc.ID, acc.Email
			default:
				row.Status, row.ID, row.Email, row.Message = models.MailboxBulkConnected, &acc.ID, acc.Email, note
			}
			out.Data[i] = row
		}(i, g)
	}
	wg.Wait()
	out.Summary.Total = len(grants)
	for _, r := range out.Data {
		switch r.Status {
		case models.MailboxBulkConnected:
			out.Summary.Connected++
		case models.MailboxBulkRenewed:
			out.Summary.Renewed++
		default:
			out.Summary.Failed++
		}
	}
	return out, nil
}

// ValidateGrantBatch refuses an empty or oversized batch before any provider is called.
func ValidateGrantBatch(n int) *errx.Error {
	if n == 0 {
		return errx.New(errx.BadRequest, "grants must carry at least one sign-in")
	}
	if n > GrantBulkMax {
		return errx.New(errx.BadRequest, fmt.Sprintf("grants may carry at most %d sign-ins per request", GrantBulkMax))
	}
	return nil
}

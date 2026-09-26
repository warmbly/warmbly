package placement

import (
	"context"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"github.com/warmbly/warmbly/internal/repository"
)

func workspaceSeed(r repository.SeedAccount, blocker string) models.PlacementWorkspaceSeed {
	fam := string(mailhost.ForMailbox(r.MailHost, r.Provider, r.Email))
	return models.PlacementWorkspaceSeed{
		EmailAccountID: r.ID,
		Email:          r.Email,
		Family:         fam,
		Label:          familyLabel(fam),
		Status:         r.Status,
		Seed:           r.SeedScope == models.SeedScopeWorkspace,
		Blocker:        blocker,
	}
}

// ListWorkspaceSeeds lists a workspace's mailboxes with which are its seed
// inboxes and which cannot become one.
func (s *service) ListWorkspaceSeeds(ctx context.Context, orgID uuid.UUID) ([]models.PlacementWorkspaceSeed, *errx.Error) {
	rows, blockers, err := s.Repo.ListOrgMailboxes(ctx, orgID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	out := make([]models.PlacementWorkspaceSeed, 0, len(rows))
	for _, r := range rows {
		out = append(out, workspaceSeed(r, blockers[r.ID]))
	}
	return out, nil
}

// SetWorkspaceSeed makes one of the workspace's mailboxes a seed inbox, or an
// ordinary mailbox again. A seed stops warming and stops sending campaigns:
// a test inbox that talks to the pool or to prospects learns its senders and
// stops being a stranger's inbox.
func (s *service) SetWorkspaceSeed(ctx context.Context, orgID, accountID uuid.UUID, enabled bool) (*models.PlacementWorkspaceSeed, *errx.Error) {
	row, err := s.Repo.GetSeedAccount(ctx, accountID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if row == nil || row.OrganizationID == nil || *row.OrganizationID != orgID {
		return nil, errx.New(errx.NotFound, "mailbox not found")
	}
	if row.SeedScope == models.SeedScopeInstance {
		return nil, placementErr(errx.Conflict, "placement_seed_unavailable", "This mailbox is on the instance seed panel.")
	}
	if enabled == (row.SeedScope == models.SeedScopeWorkspace) {
		v := workspaceSeed(*row, "")
		return &v, nil
	}
	if enabled {
		own, err := s.Repo.ListSeeds(ctx, models.SeedScopeWorkspace, &orgID, false)
		if err != nil {
			errs.CaptureException(err)
			return nil, errx.InternalError()
		}
		if len(own) >= config.PlacementSeedsPerWorkspaceMax {
			return nil, placementErr(errx.Conflict, "placement_seed_limit", "This workspace has the most seed inboxes it can have.")
		}
		if busy, err := s.Repo.SenderBusy(ctx, accountID); err != nil {
			errs.CaptureException(err)
			return nil, errx.InternalError()
		} else if busy {
			return nil, placementErr(errx.Conflict, "placement_seed_unavailable", "A placement test is sending from this mailbox.")
		}
	}
	scope := ""
	if enabled {
		scope = models.SeedScopeWorkspace
	}
	if xerr := s.applySeedScope(ctx, orgID, accountID, scope); xerr != nil {
		return nil, xerr
	}
	row.SeedScope = scope
	v := workspaceSeed(*row, "")
	return &v, nil
}

// applySeedScope writes the scope, stops warmup on a new seed and reloads the
// mailbox so its worker syncs it under the seed allowance.
func (s *service) applySeedScope(ctx context.Context, orgID, accountID uuid.UUID, scope string) *errx.Error {
	if err := s.Repo.SetSeedScope(ctx, accountID, scope); err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}
	if s.Mailboxes == nil {
		return nil
	}
	if scope != "" {
		if _, xerr := s.Mailboxes.SetWarmupLifecycle(ctx, orgID.String(), accountID.String(), "disable"); xerr != nil && xerr.Code != errx.NotFound {
			errs.CaptureException(xerr)
		}
	}
	if err := s.Mailboxes.LoadAccountOntoWorker(ctx, accountID); err != nil {
		// The next reconcile pass reloads it; the scope is already written.
		errs.CaptureException(err)
	}
	return nil
}

func adminSeed(r repository.SeedAccount) AdminSeed {
	fam := string(mailhost.ForMailbox(r.MailHost, r.Provider, r.Email))
	return AdminSeed{
		ID:             r.ID,
		OrganizationID: r.OrganizationID,
		Email:          r.Email,
		Name:           r.Name,
		Provider:       r.Provider,
		Family:         fam,
		FamilyLabel:    familyLabel(fam),
		Status:         r.Status,
		WorkerID:       r.WorkerID,
		SeedScope:      r.SeedScope,
	}
}

// AdminListSeeds is the instance panel.
func (s *service) AdminListSeeds(ctx context.Context) ([]AdminSeed, *errx.Error) {
	rows, err := s.Repo.ListSeeds(ctx, models.SeedScopeInstance, nil, false)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	out := make([]AdminSeed, 0, len(rows))
	for _, r := range rows {
		out = append(out, adminSeed(r))
	}
	return out, nil
}

// AdminSeedCandidates searches connected mailboxes an operator may add.
func (s *service) AdminSeedCandidates(ctx context.Context, search string, limit int) ([]AdminSeed, *errx.Error) {
	rows, err := s.Repo.ListSeedCandidates(ctx, search, limit)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	out := make([]AdminSeed, 0, len(rows))
	for _, r := range rows {
		out = append(out, adminSeed(r))
	}
	return out, nil
}

// AdminSetSeed adds a mailbox to the instance panel or takes it off.
func (s *service) AdminSetSeed(ctx context.Context, accountID uuid.UUID, enabled bool) (*AdminSeed, *errx.Error) {
	row, err := s.Repo.GetSeedAccount(ctx, accountID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if row == nil {
		return nil, errx.New(errx.NotFound, "mailbox not found")
	}
	if !enabled && row.SeedScope != models.SeedScopeInstance {
		v := adminSeed(*row)
		return &v, nil
	}
	scope := ""
	if enabled {
		scope = models.SeedScopeInstance
	}
	orgID := uuid.Nil
	if row.OrganizationID != nil {
		orgID = *row.OrganizationID
	}
	if xerr := s.applySeedScope(ctx, orgID, accountID, scope); xerr != nil {
		return nil, xerr
	}
	row.SeedScope = scope
	v := adminSeed(*row)
	return &v, nil
}

package placement

import (
	"context"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// The cloud side: Warmbly Cloud lends its instance panel to a linked
// self-hosted instance. The instance renders and sends every copy itself, so
// the cloud never holds the template; it hands out seed addresses, takes the
// Message-IDs back, and answers with where each copy landed.

// RemotePanel is the cloud panel and the linked workspace's allowance.
func (s *service) RemotePanel(ctx context.Context, inst *models.PoolLinkInstance) (*models.PlacementCloudPanel, *errx.Error) {
	rows, err := s.Repo.ListSeeds(ctx, models.SeedScopeInstance, nil, true)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	usage, xerr := s.usage(ctx, inst.OrganizationID)
	if xerr != nil {
		return nil, xerr
	}
	panel := panelInfo(models.PlacementPanelCloud, familySeeds(rows), true)
	if !panel.Available {
		panel.Reason = "Warmbly Cloud has no seed inboxes available right now."
	}
	return &models.PlacementCloudPanel{Panel: panel, Usage: usage}, nil
}

// RemoteStart opens one test per requested variant against the same seeds,
// charged to the linked workspace's allowance.
func (s *service) RemoteStart(ctx context.Context, inst *models.PoolLinkInstance, req models.PlacementCloudStartRequest) (*models.PlacementCloudStart, *errx.Error) {
	if req.Tests < 1 || req.Tests > 2 {
		return nil, errx.New(errx.BadRequest, "tests must be 1 or 2")
	}
	usage, xerr := s.usage(ctx, inst.OrganizationID)
	if xerr != nil {
		return nil, xerr
	}
	if usage.Limit != nil && usage.Used+req.Tests > *usage.Limit {
		return nil, placementErr(errx.PaymentRequired, "placement_quota_exceeded",
			"This Warmbly Cloud workspace has used its placement tests for the month.")
	}
	running, err := s.Repo.CountRunning(ctx, inst.OrganizationID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if running+req.Tests > config.PlacementRunningPerOrgMax {
		return nil, placementErr(errx.TooManyRequests, "placement_too_many_running",
			"This workspace already has placement tests running. Wait for one to finish.")
	}
	rows, err := s.Repo.ListSeeds(ctx, models.SeedScopeInstance, nil, true)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	seeds := pickSeeds(rows, uuid.Nil, req.SenderDomain, s.policy(ctx).SeedsPerTest)
	if len(seeds) == 0 {
		return nil, placementErr(errx.Conflict, "placement_no_seeds", noSeedsMessage(models.PlacementPanelCloud))
	}

	org := inst.OrganizationID
	instanceID := inst.ID
	out := &models.PlacementCloudStart{Usage: usage}
	for _, seed := range seeds {
		out.Seeds = append(out.Seeds, models.PlacementCloudSeed{ID: *seed.AccountID, Address: seed.Address, Family: seed.Family})
	}
	for range req.Tests {
		test := models.PlacementTest{
			ID:               uuid.New(),
			OrganizationID:   &org,
			Origin:           models.PlacementOriginRemote,
			Panel:            models.PlacementPanelInstance,
			Status:           models.PlacementStatusRunning,
			RemoteInstanceID: &instanceID,
		}
		results := make([]models.PlacementResult, 0, len(seeds))
		for _, seed := range seeds {
			results = append(results, models.PlacementResult{
				SeedAccountID: seed.AccountID,
				SeedAddress:   seed.Address,
				Family:        seed.Family,
				Folder:        models.PlacementFolderPending,
			})
		}
		if err := s.Repo.CreateTest(ctx, &test, results, nil); err != nil {
			errs.CaptureException(err)
			return nil, errx.InternalError()
		}
		out.TestIDs = append(out.TestIDs, test.ID)
	}
	out.Usage.Used += req.Tests
	return out, nil
}

// RemoteSends records the Message-IDs an instance put on its copies.
func (s *service) RemoteSends(ctx context.Context, inst *models.PoolLinkInstance, testID uuid.UUID, sends []models.PlacementCloudSend) *errx.Error {
	if len(sends) > config.PlacementSeedsPerTestMax {
		return errx.New(errx.BadRequest, "too many sends in one report")
	}
	test, _, err := s.Repo.GetRemoteTest(ctx, inst.ID, testID)
	if err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}
	if test == nil {
		return errx.New(errx.NotFound, "placement test not found")
	}
	for _, snd := range sends {
		if snd.Error == "" && snd.MessageID == "" {
			continue
		}
		if err := s.Repo.RecordRemoteSent(ctx, inst.ID, testID, snd.SeedID, snd.MessageID, snd.SentAt, snd.Error); err != nil {
			errs.CaptureException(err)
			return errx.InternalError()
		}
	}
	return nil
}

// RemoteGet answers with the verdicts so far.
func (s *service) RemoteGet(ctx context.Context, inst *models.PoolLinkInstance, testID uuid.UUID) (*models.PlacementCloudTest, *errx.Error) {
	test, results, err := s.Repo.GetRemoteTest(ctx, inst.ID, testID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if test == nil {
		return nil, errx.New(errx.NotFound, "placement test not found")
	}
	out := &models.PlacementCloudTest{TestID: test.ID, Status: test.Status, Verdicts: []models.PlacementCloudVerdict{}}
	for _, r := range results {
		if r.SeedAccountID == nil || !models.PlacementFolderResolved(r.Folder) {
			continue
		}
		out.Verdicts = append(out.Verdicts, models.PlacementCloudVerdict{SeedID: *r.SeedAccountID, Folder: r.Folder, DetectedAt: r.DetectedAt})
	}
	return out, nil
}

// syncCloud is the instance side: report what left, pull back what landed.
// It returns the local tests that got a verdict.
func (s *service) syncCloud(ctx context.Context) map[uuid.UUID]bool {
	touched := map[uuid.UUID]bool{}
	if s.Cloud == nil {
		return touched
	}
	reports, err := s.Repo.ListRemoteReports(ctx, 200)
	if err != nil {
		errs.CaptureException(err)
		return touched
	}
	byTest := map[uuid.UUID][]int{}
	for i, r := range reports {
		byTest[r.RemoteTestID] = append(byTest[r.RemoteTestID], i)
	}
	for remoteTest, idx := range byTest {
		sends := make([]models.PlacementCloudSend, 0, len(idx))
		ids := make([]uuid.UUID, 0, len(idx))
		for _, i := range idx {
			r := reports[i]
			snd := models.PlacementCloudSend{SeedID: r.RemoteSeedID, MessageID: r.MessageID, SentAt: r.SentAt}
			if r.Folder == models.PlacementFolderFailed || r.Folder == models.PlacementFolderCancelled {
				snd = models.PlacementCloudSend{SeedID: r.RemoteSeedID, Error: firstNonEmpty(r.Error, "Not sent")}
			}
			sends = append(sends, snd)
			ids = append(ids, r.ResultID)
		}
		if xerr := s.Cloud.ReportPlacementSends(ctx, remoteTest, sends); xerr != nil {
			continue
		}
		if err := s.Repo.MarkRemoteReported(ctx, ids); err != nil {
			errs.CaptureException(err)
		}
	}

	open, err := s.Repo.ListRemoteOpenTests(ctx, 50)
	if err != nil {
		errs.CaptureException(err)
		return touched
	}
	for _, t := range open {
		if t.RemoteTestID == nil {
			continue
		}
		answer, xerr := s.Cloud.PlacementVerdicts(ctx, *t.RemoteTestID)
		if xerr != nil || answer == nil {
			continue
		}
		for _, v := range answer.Verdicts {
			if !models.PlacementFolderResolved(v.Folder) {
				continue
			}
			if err := s.Repo.RecordRemoteVerdict(ctx, t.ID, v.SeedID, v.Folder, v.DetectedAt); err != nil {
				errs.CaptureException(err)
				continue
			}
			touched[t.ID] = true
		}
	}
	return touched
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

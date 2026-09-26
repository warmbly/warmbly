package placement

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/repository"
)

// Tick is one pass of the poller: read verdicts, expire what never arrived,
// sync the cloud's panel, close finished tests and start due monitors.
func (s *service) Tick(ctx context.Context) error {
	now := s.now()
	touched := map[uuid.UUID]bool{}

	landings, err := s.Repo.FindLandings(ctx, 500)
	if err != nil {
		errs.CaptureException(err)
		return err
	}
	for _, l := range landings {
		folder := models.ClassifyPlacementLanding(l.Folder, l.Flags)
		if err := s.Repo.RecordLanding(ctx, l.ResultID, folder, strings.Join(l.Flags, ","), now); err != nil {
			errs.CaptureException(err)
			continue
		}
		touched[l.TestID] = true
	}

	timeout := now.Add(-time.Duration(config.PlacementClassifyTimeoutMinutes) * time.Minute)
	// An instance reports failed and cancelled copies itself; this only
	// catches one that went away. The longest test the settings allow (100
	// seeds, twice, 600 seconds apart) sends in about 33 hours.
	if err := s.Repo.ExpireProbes(ctx, timeout, timeout, now.Add(-48*time.Hour)); err != nil {
		errs.CaptureException(err)
	}

	for id := range s.syncCloud(ctx) {
		touched[id] = true
	}

	finished, err := s.Repo.FinishTests(ctx)
	if err != nil {
		errs.CaptureException(err)
	}
	notifiedGroups := map[uuid.UUID]bool{}
	for _, f := range finished {
		delete(touched, f.ID)
		if f.OrganizationID != nil && s.Publisher != nil {
			s.Publisher.PublishPlacementTest(ctx, *f.OrganizationID, f.ID, f.CampaignID, f.Status)
		}
		s.onFinished(ctx, f, notifiedGroups)
	}
	for id := range touched {
		if t, err := s.Repo.GetTest(ctx, id); err == nil && t != nil {
			s.publish(ctx, t)
		}
	}

	s.runMonitors(ctx)
	return nil
}

// onFinished tells whoever started a test where it landed, and checks a
// monitor's test against its threshold. A tracking comparison reports once,
// when both halves are done.
func (s *service) onFinished(ctx context.Context, f repository.PlacementFinished, notifiedGroups map[uuid.UUID]bool) {
	if f.OrganizationID == nil || f.Status == models.PlacementStatusCancelled {
		return
	}
	ids := []uuid.UUID{f.ID}
	var tests []models.PlacementTest
	if f.CompareGroupID != nil {
		if notifiedGroups[*f.CompareGroupID] {
			return
		}
		group, err := s.Repo.ListGroup(ctx, *f.OrganizationID, *f.CompareGroupID)
		if err != nil {
			return
		}
		for _, t := range group {
			if t.FinishedAt == nil && t.ID != f.ID {
				return
			}
		}
		notifiedGroups[*f.CompareGroupID] = true
		tests = group
		ids = ids[:0]
		for _, t := range group {
			ids = append(ids, t.ID)
		}
	} else {
		t, err := s.Repo.GetTest(ctx, f.ID)
		if err != nil || t == nil {
			return
		}
		tests = []models.PlacementTest{*t}
	}
	results, err := s.Repo.ListResults(ctx, ids)
	if err != nil {
		return
	}

	var headline models.PlacementCounts
	lines := make([]string, 0, len(tests))
	for _, t := range tests {
		v := s.view(t, results[t.ID], false)
		if len(tests) > 1 {
			label := "Without tracking"
			if t.Tracked() {
				label = "With tracking"
			}
			lines = append(lines, label+": "+summaryLine(v.Summary))
		} else {
			lines = append(lines, summaryLine(v.Summary))
		}
		// The alert reads the copy the campaign really sends.
		if len(tests) == 1 || t.Tracked() {
			headline = v.Summary
		}
	}
	link := "/app/placement/" + f.ID.String()
	body := strings.Join(lines, "\n")

	switch f.Origin {
	case models.PlacementOriginManual:
		if f.CreatedBy != nil && s.Notifier != nil {
			s.Notifier.Notify(ctx, *f.CreatedBy, f.OrganizationID, models.NotifPlacementFinished,
				"Placement test finished", body, link, map[string]any{"placement_test_id": f.ID.String()})
		}
	case models.PlacementOriginMonitor:
		if f.MonitorID != nil {
			s.checkMonitorAlert(ctx, *f.MonitorID, *f.OrganizationID, f.CampaignID, headline, body, link)
		}
	}
}

// summaryLine reads a test in one sentence.
func summaryLine(c models.PlacementCounts) string {
	if c.Delivered == 0 {
		return "No copy reached a seed inbox."
	}
	return fmt.Sprintf("%d of %d copies reached the inbox, %d a Gmail tab, %d spam and %d never arrived.",
		c.Inbox, c.Delivered, c.Promotions+c.Other, c.Spam, c.Missing)
}

// percent is a rate as a whole percentage, -1 when there is none.
func percent(r *float64) int {
	if r == nil {
		return -1
	}
	return int(math.Round(*r * 100))
}

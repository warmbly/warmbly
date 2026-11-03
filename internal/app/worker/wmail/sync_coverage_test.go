package wmail

import (
	"github.com/warmbly/warmbly/internal/models"
	"testing"
	"time"
)

func TestSyncCoverageChangesWithoutCompletedSync(t *testing.T) {
	completed := time.Now().Add(-time.Hour)
	var reports []models.SyncState
	tracker := newSyncTracker(&models.SyncState{LastSyncedAt: &completed}, func(state models.SyncState) error {
		reports = append(reports, state)
		return nil
	})
	tracker.setFoldersSkipped(17, 2)
	tracker.flush(time.Now())
	tracker.setFoldersSkipped(0, 0)
	tracker.flush(time.Now())
	if len(reports) != 2 {
		t.Fatalf("reports=%d", len(reports))
	}
	for _, report := range reports {
		if report.LastSyncedAt == nil || !report.LastSyncedAt.Equal(completed) {
			t.Fatal("counter changes claimed a completed sync")
		}
	}
	if reports[0].FoldersSkippedCap != 17 || reports[1].FoldersSkippedCap != 0 {
		t.Fatalf("counter observations: %+v", reports)
	}
}

func TestSyncCoverageObservationsRemainOrderedAcrossReload(t *testing.T) {
	var reports []models.SyncState
	emit := func(state models.SyncState) error { reports = append(reports, state); return nil }
	tracker := newSyncTracker(nil, emit)
	tracker.setFoldersSkipped(17, 2)
	tracker.flush(time.Now())
	seed := reports[0]
	completed := seed.FolderCoverageObservedAt.Add(time.Millisecond)
	seed.LastSyncedAt = &completed
	tracker = newSyncTracker(&seed, emit)
	tracker.setFoldersSkipped(0, 0)
	tracker.flush(time.Now())
	if reports[0].FolderCoverageObservedAt == nil || reports[1].FolderCoverageObservedAt == nil || !reports[1].FolderCoverageObservedAt.After(completed) {
		t.Fatalf("unordered coverage observations: %+v", reports)
	}
	tracker.setFoldersSkipped(0, 0)
	if tracker.dirty {
		t.Fatal("unchanged counters dirtied the tracker")
	}
}

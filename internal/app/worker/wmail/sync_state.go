package wmail

import (
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

// syncStateHeartbeat bounds how often an unchanged state is still relayed, so
// last_synced_at stays fresh without a database write per mailbox per tick.
const syncStateHeartbeat = 10 * time.Minute

// syncTracker is the worker's live copy of a mailbox's sync state. Every
// mutation goes through it so the change is relayed as SYNC_STATE at the end
// of the tick; the consumer persists it and the loader hands it back on the
// next assignment.
type syncTracker struct {
	state        models.SyncState
	dirty        bool
	lastSent     time.Time
	emit         func(models.SyncState) error
	tickComplete bool
}

func newSyncTracker(seed *models.SyncState, emit func(models.SyncState) error) *syncTracker {
	t := &syncTracker{emit: emit}
	if seed != nil {
		t.state = *seed
	}
	if t.state.BackfillStatus == "" {
		t.state.BackfillStatus = models.SyncBackfillPending
	}
	if t.state.BackfillCursor.Folders == nil {
		t.state.BackfillCursor.Folders = map[string]models.SyncFolderCursor{}
	}
	return t
}

func (t *syncTracker) mark() { t.dirty = true }

// throttle records that fair use is holding live mail. Only a later Until
// extends the hold; a burst denial never shortens a daily one.
func (t *syncTracker) throttle(a Admission) {
	if a.OK {
		return
	}
	if t.state.ThrottledUntil == nil || a.Until.After(*t.state.ThrottledUntil) {
		until := a.Until
		t.state.ThrottledUntil = &until
		t.state.ThrottleReason = a.Reason
		t.dirty = true
	}
}

// release clears an expired hold. Called at the start of a tick so a mailbox
// that is back within budget reports itself so even before it stores anything.
func (t *syncTracker) release(now time.Time) {
	if t.state.ThrottledUntil != nil && !t.state.ThrottledUntil.After(now) {
		t.state.ThrottledUntil = nil
		t.state.ThrottleReason = ""
		t.dirty = true
	}
}

func (t *syncTracker) setDeferred(n int) {
	if t.state.Deferred != n {
		t.state.Deferred = n
		t.dirty = true
	}
}

// setFoldersSkipped records what the folder listing could not follow. Both
// are conditions the user can fix, so they are relayed every pass and clear
// themselves rather than leaving a warning nobody can withdraw.
func (t *syncTracker) setFoldersSkipped(cap, conflict int) {
	if t.state.FoldersSkippedCap != cap || t.state.FoldersSkippedConflict != conflict {
		t.state.FoldersSkippedCap = cap
		t.state.FoldersSkippedConflict = conflict
		t.dirty = true
	}
}

// touch stamps the tick and relays the state when it changed or the
// heartbeat is due.
func (t *syncTracker) touch(now time.Time) {
	stamp := now
	t.state.LastSyncedAt = &stamp
	t.flush(now)
}

// flush publishes safe progress without implying a successful provider check.
func (t *syncTracker) flush(now time.Time) {
	if !t.dirty && now.Sub(t.lastSent) < syncStateHeartbeat {
		return
	}
	if err := t.emit(t.state); err != nil {
		log.Warn().Err(err).Msg("sync state relay failed")
		return
	}
	t.dirty = false
	t.lastSent = now
}

// startBackfill fixes the window when the import first runs, so an operator
// changing the setting later does not move the goalposts mid-walk.
func (t *syncTracker) startBackfill(now time.Time, days int) {
	if t.state.BackfillStatus != models.SyncBackfillPending {
		return
	}
	since := now.Add(-time.Duration(days) * 24 * time.Hour)
	t.state.BackfillSince = &since
	t.state.BackfillStartedAt = &now
	t.state.BackfillStatus = models.SyncBackfillRunning
	t.dirty = true
}

func (t *syncTracker) completeBackfill(now time.Time) {
	if t.state.BackfillStatus == models.SyncBackfillComplete {
		return
	}
	t.state.BackfillStatus = models.SyncBackfillComplete
	t.state.BackfillCompletedAt = &now
	t.dirty = true
}

func (t *syncTracker) folder(key string) models.SyncFolderCursor {
	return t.state.BackfillCursor.Folders[key]
}

func (t *syncTracker) setFolder(key string, c models.SyncFolderCursor) {
	t.state.BackfillCursor.Folders[key] = c
	t.dirty = true
}

// clearFolder forgets a folder's backfill floor.
//
// It has to go when the folder does. A name is reusable, so a floor left
// behind is inherited by whatever is created under that name next: a "done"
// cursor skips the new folder's history entirely, and a UID floor skips
// everything below it. Neither shows up as an error, because the messages are
// not new mail either; they are simply never imported.
func (t *syncTracker) clearFolder(name string) {
	if _, ok := t.state.BackfillCursor.Folders[name]; !ok {
		return
	}
	delete(t.state.BackfillCursor.Folders, name)
	t.dirty = true
}

// renameFolder moves a folder's backfill floor to its new name, so a rename
// costs nothing rather than restarting the folder's import from the top.
func (t *syncTracker) renameFolder(from, to string) {
	cur, ok := t.state.BackfillCursor.Folders[from]
	if !ok {
		return
	}
	delete(t.state.BackfillCursor.Folders, from)
	t.state.BackfillCursor.Folders[to] = cur
	t.dirty = true
}

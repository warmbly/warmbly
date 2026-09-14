package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// The bulk task actions run their WHERE inside a DELETE and an UPDATE, reusing
// the aliased clause the search builds. Nothing but a real database proves the
// alias is accepted there and the $N numbering survives the SET args being
// appended after the filter's, so this runs against one.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run Live -v

type bulkTaskFixture struct {
	pool  *pgxpool.Pool
	repo  CRMRepository
	org   uuid.UUID
	user  uuid.UUID
	tasks []uuid.UUID
}

func newBulkTaskFixture(t *testing.T) *bulkTaskFixture {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	f := &bulkTaskFixture{pool: pool, repo: NewCRMRepository(pool), org: uuid.New(), user: uuid.New()}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Bulk', 'Test')`,
		f.user, "bulk-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Bulk Test', $2, $3)`,
		f.org, "bulk-"+f.org.String()[:8], f.user)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM crm_tasks WHERE organization_id = $1`, f.org)
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, f.org)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.user)
	})
	return f
}

func (f *bulkTaskFixture) contact(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, subscribed, updated_at, created_at)
		 VALUES ($1, $2, $3, $4, 'Bulk', 'Live', '', '', '{}'::jsonb, true, NOW(), NOW())`,
		id, f.user, f.org, "bulk-"+id.String()[:8]+"@test.local"); err != nil {
		t.Fatalf("fixture contact: %v", err)
	}
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM contact_activities WHERE contact_id = $1`, id)
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM contacts WHERE id = $1`, id)
	})
	return id
}

func (f *bulkTaskFixture) activities(t *testing.T, contactID uuid.UUID) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM contact_activities WHERE contact_id = $1 AND activity_type = $2`,
		contactID, models.ActivityTaskCompleted).Scan(&n); err != nil {
		t.Fatalf("count activities: %v", err)
	}
	return n
}

func (f *bulkTaskFixture) task(t *testing.T, title, priority string) uuid.UUID {
	t.Helper()
	return f.contactTask(t, title, priority, nil)
}

func (f *bulkTaskFixture) contactTask(t *testing.T, title, priority string, contactID *uuid.UUID) uuid.UUID {
	t.Helper()
	created, err := f.repo.CreateCRMTask(context.Background(), f.org, f.user, &models.CreateCRMTask{
		ContactID: contactID,
		Title:     title,
		Priority:  priority,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	f.tasks = append(f.tasks, created.ID)
	return created.ID
}

func TestLiveBulkUpdateTasksByIDList(t *testing.T) {
	f := newBulkTaskFixture(t)
	ctx := context.Background()
	a := f.task(t, "Follow up: reply from a@b.com", "high")
	b := f.task(t, "Follow up: reply from c@d.com", "high")
	untouched := f.task(t, "Leave me alone", "low")

	status := string(models.CRMTaskStatusCompleted)
	sel := models.TaskSelection{Tasks: []string{a.String(), b.String()}}
	_, n, err := f.repo.BulkUpdateCRMTasks(ctx, f.org, &f.user, sel, &models.BulkUpdateTasks{
		TaskSelection: sel,
		Status:        &status,
	}, models.MaxTaskBulkSelection)
	if err != nil {
		t.Fatalf("bulk update: %v", err)
	}
	if n != 2 {
		t.Fatalf("updated %d, want 2", n)
	}
	for _, id := range []uuid.UUID{a, b} {
		got, err := f.repo.GetCRMTask(ctx, f.org, id)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if got.Status != models.CRMTaskStatusCompleted {
			t.Errorf("status %q, want completed", got.Status)
		}
		if got.CompletedAt == nil {
			t.Error("completing a task must stamp completed_at")
		}
	}
	left, err := f.repo.GetCRMTask(ctx, f.org, untouched)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if left.Status == models.CRMTaskStatusCompleted {
		t.Error("a task outside the selection was written")
	}
}

// The select-all form is the one that reuses the search's WHERE, exclusions
// and all, so it is the one that can go wrong silently.
func TestLiveBulkDeleteTasksByFilter(t *testing.T) {
	f := newBulkTaskFixture(t)
	ctx := context.Background()
	f.task(t, "Follow up: out-of-office reply from a@b.com", "high")
	f.task(t, "Follow up: out-of-office reply from c@d.com", "high")
	kept := f.task(t, "Follow up: out-of-office reply from e@f.com", "high")
	other := f.task(t, "Call the customer", "medium")

	sel := models.TaskSelection{
		All:     true,
		Filters: &models.SearchTasks{Query: "out-of-office", ContactID: nil},
		Exclude: []string{kept.String()},
	}
	// The filter is title-only, so scope it to this fixture's org through the
	// repo call itself; a shared dev database holds other organizations' rows.
	_, n, err := f.repo.BulkDeleteCRMTasks(ctx, f.org, sel, models.MaxTaskBulkSelection)
	if err != nil {
		t.Fatalf("bulk delete: %v", err)
	}
	if n != 2 {
		t.Fatalf("deleted %d, want 2", n)
	}
	if _, err := f.repo.GetCRMTask(ctx, f.org, kept); err != nil {
		t.Errorf("the excluded task was deleted: %v", err)
	}
	if _, err := f.repo.GetCRMTask(ctx, f.org, other); err != nil {
		t.Errorf("a task the filter never matched was deleted: %v", err)
	}
}

// A filter with a team facet binds a uuid[] once and reuses its $N; the SET
// args are numbered after it. This is the shape that breaks when a clause is
// appended without counting.
func TestLiveBulkUpdateTasksWithEveryFilterBound(t *testing.T) {
	f := newBulkTaskFixture(t)
	ctx := context.Background()
	id := f.task(t, "Follow up: reply from a@b.com", "low")

	priority := string(models.CRMTaskPriorityUrgent)
	status := string(models.CRMTaskStatusInProgress)
	sel := models.TaskSelection{
		All: true,
		Filters: &models.SearchTasks{
			Query:      "Follow up",
			Statuses:   []string{"pending"},
			Priorities: []string{"low"},
			TeamIDs:    []uuid.UUID{uuid.New()},
		},
	}
	// The team facet matches nothing, so this must write nothing rather than
	// error or fall through to the whole org.
	_, n, err := f.repo.BulkUpdateCRMTasks(ctx, f.org, &f.user, sel, &models.BulkUpdateTasks{
		TaskSelection: sel, Status: &status, Priority: &priority,
	}, models.MaxTaskBulkSelection)
	if err != nil {
		t.Fatalf("bulk update: %v", err)
	}
	if n != 0 {
		t.Fatalf("updated %d rows through a facet that matches nothing", n)
	}

	sel.Filters.TeamIDs = nil
	_, n, err = f.repo.BulkUpdateCRMTasks(ctx, f.org, &f.user, sel, &models.BulkUpdateTasks{
		TaskSelection: sel, Status: &status, Priority: &priority,
	}, models.MaxTaskBulkSelection)
	if err != nil {
		t.Fatalf("bulk update: %v", err)
	}
	if n != 1 {
		t.Fatalf("updated %d, want 1", n)
	}
	got, err := f.repo.GetCRMTask(ctx, f.org, id)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Status != models.CRMTaskStatusInProgress || got.Priority != models.CRMTaskPriorityUrgent {
		t.Errorf("wrote %q/%q, want in_progress/urgent", got.Status, got.Priority)
	}
}

// Completing in bulk has to leave the same trail as completing one at a time,
// and exactly once: the activity is written for the tasks THIS call completed,
// not for the ones already done that the filter happened to cover.
func TestLiveBulkCompleteRecordsTheContactActivityOnce(t *testing.T) {
	f := newBulkTaskFixture(t)
	ctx := context.Background()
	contact := f.contact(t)
	a := f.contactTask(t, "Follow up: reply from a@b.com", "high", &contact)
	b := f.contactTask(t, "Follow up: reply from c@d.com", "high", &contact)
	f.task(t, "Follow up: reply with no contact", "high")

	status := string(models.CRMTaskStatusCompleted)
	sel := models.TaskSelection{Tasks: []string{a.String(), b.String()}}
	if _, _, err := f.repo.BulkUpdateCRMTasks(ctx, f.org, &f.user, sel, &models.BulkUpdateTasks{
		TaskSelection: sel, Status: &status,
	}, models.MaxTaskBulkSelection); err != nil {
		t.Fatalf("bulk update: %v", err)
	}
	if got := f.activities(t, contact); got != 2 {
		t.Fatalf("recorded %d completions, want 2", got)
	}

	// Running it again completes nothing, so it must record nothing.
	if _, _, err := f.repo.BulkUpdateCRMTasks(ctx, f.org, &f.user, sel, &models.BulkUpdateTasks{
		TaskSelection: sel, Status: &status,
	}, models.MaxTaskBulkSelection); err != nil {
		t.Fatalf("bulk update (repeat): %v", err)
	}
	if got := f.activities(t, contact); got != 2 {
		t.Errorf("recorded %d completions after a repeat, want 2", got)
	}
}

// A selection routinely covers tasks that are already done, so repeating a
// bulk complete must leave their completion time alone. Stamping NOW() on
// every row in the selection rewrote history every time the action ran.
func TestLiveBulkCompleteStampsOnlyTheTransition(t *testing.T) {
	f := newBulkTaskFixture(t)
	ctx := context.Background()
	done := f.task(t, "Already finished", "low")
	fresh := f.task(t, "Still pending", "low")

	status := string(models.CRMTaskStatusCompleted)
	first := models.TaskSelection{Tasks: []string{done.String()}}
	if _, _, err := f.repo.BulkUpdateCRMTasks(ctx, f.org, &f.user, first, &models.BulkUpdateTasks{
		TaskSelection: first, Status: &status,
	}, models.MaxTaskBulkSelection); err != nil {
		t.Fatalf("first complete: %v", err)
	}
	was, err := f.repo.GetCRMTask(ctx, f.org, done)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if was.CompletedAt == nil {
		t.Fatal("completing a task must stamp completed_at")
	}

	// Both rows in one call: one already completed, one not.
	both := models.TaskSelection{Tasks: []string{done.String(), fresh.String()}}
	if _, _, err := f.repo.BulkUpdateCRMTasks(ctx, f.org, &f.user, both, &models.BulkUpdateTasks{
		TaskSelection: both, Status: &status,
	}, models.MaxTaskBulkSelection); err != nil {
		t.Fatalf("second complete: %v", err)
	}

	again, err := f.repo.GetCRMTask(ctx, f.org, done)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if again.CompletedAt == nil || !again.CompletedAt.Equal(*was.CompletedAt) {
		t.Errorf("completed_at moved from %v to %v on a task that was already done", was.CompletedAt, again.CompletedAt)
	}
	now, err := f.repo.GetCRMTask(ctx, f.org, fresh)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if now.CompletedAt == nil {
		t.Error("the task that transitioned in the same call was not stamped")
	}
}

// Reopening a task and completing it again is a real transition, so it takes a
// fresh stamp rather than keeping the old one.
func TestLiveReopenedTaskIsStampedAgain(t *testing.T) {
	f := newBulkTaskFixture(t)
	ctx := context.Background()
	id := f.task(t, "Round trip", "low")

	done := string(models.CRMTaskStatusCompleted)
	pending := string(models.CRMTaskStatusPending)
	sel := models.TaskSelection{Tasks: []string{id.String()}}
	apply := func(status string) {
		t.Helper()
		if _, _, err := f.repo.BulkUpdateCRMTasks(ctx, f.org, &f.user, sel, &models.BulkUpdateTasks{
			TaskSelection: sel, Status: &status,
		}, models.MaxTaskBulkSelection); err != nil {
			t.Fatalf("apply %s: %v", status, err)
		}
	}
	apply(done)
	first, err := f.repo.GetCRMTask(ctx, f.org, id)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	apply(pending)
	apply(done)
	second, err := f.repo.GetCRMTask(ctx, f.org, id)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if first.CompletedAt == nil || second.CompletedAt == nil {
		t.Fatal("both completions must stamp")
	}
	if !second.CompletedAt.After(*first.CompletedAt) {
		t.Errorf("a reopened task kept its old completion time: %v then %v", first.CompletedAt, second.CompletedAt)
	}
}

// The cap is a promise that an over-large selection is refused WHOLE. Checking
// it with a COUNT before the write left a gap a concurrent insert could widen,
// so the statement itself carries the bound: over it, the row count comes back
// and nothing is written.
func TestLiveBulkOverTheCapWritesNothing(t *testing.T) {
	f := newBulkTaskFixture(t)
	ctx := context.Background()
	ids := []string{}
	for i := 0; i < 3; i++ {
		ids = append(ids, f.task(t, "Capped", "low").String())
	}

	status := string(models.CRMTaskStatusCompleted)
	sel := models.TaskSelection{Tasks: ids}
	matched, affected, err := f.repo.BulkUpdateCRMTasks(ctx, f.org, &f.user, sel, &models.BulkUpdateTasks{
		TaskSelection: sel, Status: &status,
	}, 2)
	if err != nil {
		t.Fatalf("bulk update: %v", err)
	}
	if matched != 3 || affected != 0 {
		t.Fatalf("matched %d / wrote %d, want 3 matched and nothing written", matched, affected)
	}
	for _, raw := range ids {
		id, _ := uuid.Parse(raw)
		got, err := f.repo.GetCRMTask(ctx, f.org, id)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if got.Status == models.CRMTaskStatusCompleted {
			t.Error("a task was written by a refused bulk update")
		}
	}

	matched, affected, err = f.repo.BulkDeleteCRMTasks(ctx, f.org, sel, 2)
	if err != nil {
		t.Fatalf("bulk delete: %v", err)
	}
	if matched != 3 || affected != 0 {
		t.Fatalf("matched %d / deleted %d, want 3 matched and nothing deleted", matched, affected)
	}
	for _, raw := range ids {
		id, _ := uuid.Parse(raw)
		if _, err := f.repo.GetCRMTask(ctx, f.org, id); err != nil {
			t.Errorf("a task was deleted by a refused bulk delete: %v", err)
		}
	}

	// Exactly at the cap it goes through, so the bound is <= and not <.
	_, affected, err = f.repo.BulkDeleteCRMTasks(ctx, f.org, sel, 3)
	if err != nil {
		t.Fatalf("bulk delete at the cap: %v", err)
	}
	if affected != 3 {
		t.Errorf("deleted %d at the cap, want 3", affected)
	}
}

// The refusal is decided from cap+1 candidates, never from the whole matching
// set: a broad filter on a big workspace must not lock every row it matches
// only to write nothing.
func TestLiveBulkOverTheCapLocksNoMoreThanItNeeds(t *testing.T) {
	f := newBulkTaskFixture(t)
	ctx := context.Background()
	ids := []string{}
	for i := 0; i < 5; i++ {
		ids = append(ids, f.task(t, "Bounded", "low").String())
	}

	status := string(models.CRMTaskStatusCompleted)
	sel := models.TaskSelection{Tasks: ids}
	matched, affected, err := f.repo.BulkUpdateCRMTasks(ctx, f.org, &f.user, sel, &models.BulkUpdateTasks{
		TaskSelection: sel, Status: &status,
	}, 2)
	if err != nil {
		t.Fatalf("bulk update: %v", err)
	}
	if matched != 3 || affected != 0 {
		t.Fatalf("matched %d / wrote %d, want 3 (the cap plus one) matched and nothing written", matched, affected)
	}

	matched, affected, err = f.repo.BulkDeleteCRMTasks(ctx, f.org, sel, 2)
	if err != nil {
		t.Fatalf("bulk delete: %v", err)
	}
	if matched != 3 || affected != 0 {
		t.Fatalf("matched %d / deleted %d, want 3 (the cap plus one) matched and nothing deleted", matched, affected)
	}
}

// completed_at is when the task was finished. Moving a task back off completed
// leaves it with none, rather than showing a pending task as finished days ago,
// and the bulk path and the single-task path agree about that.
func TestLiveMovingOffCompletedClearsTheStamp(t *testing.T) {
	f := newBulkTaskFixture(t)
	ctx := context.Background()
	id := f.task(t, "Reopened", "low")

	done := string(models.CRMTaskStatusCompleted)
	pending := string(models.CRMTaskStatusPending)
	sel := models.TaskSelection{Tasks: []string{id.String()}}
	if _, _, err := f.repo.BulkUpdateCRMTasks(ctx, f.org, &f.user, sel, &models.BulkUpdateTasks{
		TaskSelection: sel, Status: &done,
	}, 10); err != nil {
		t.Fatalf("bulk complete: %v", err)
	}
	got, err := f.repo.GetCRMTask(ctx, f.org, id)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.CompletedAt == nil {
		t.Fatal("completing left no completion time")
	}

	if _, _, err := f.repo.BulkUpdateCRMTasks(ctx, f.org, &f.user, sel, &models.BulkUpdateTasks{
		TaskSelection: sel, Status: &pending,
	}, 10); err != nil {
		t.Fatalf("bulk reopen: %v", err)
	}
	got, err = f.repo.GetCRMTask(ctx, f.org, id)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.CompletedAt != nil {
		t.Errorf("a pending task kept completed_at = %v", got.CompletedAt)
	}

	// The single-task update answers the same way.
	if _, err := f.repo.UpdateCRMTask(ctx, f.org, id, &models.UpdateCRMTask{Status: &done}); err != nil {
		t.Fatalf("update to completed: %v", err)
	}
	reopened, err := f.repo.UpdateCRMTask(ctx, f.org, id, &models.UpdateCRMTask{Status: &pending})
	if err != nil {
		t.Fatalf("update to pending: %v", err)
	}
	if reopened.CompletedAt != nil {
		t.Errorf("single-task update left completed_at = %v on a pending task", reopened.CompletedAt)
	}
}

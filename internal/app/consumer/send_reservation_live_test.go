package jobs

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Live checks of the pre-send reservation (issue #169) against a real
// Postgres. Skipped unless WARMBLY_TEST_DB is set:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/app/consumer/ -run Live -v
//
// The bug these cover: the send goes on the bus first and the progress write
// happens after, so a crash or a failed write in between left the step looking
// "never sent" and routing offered it again, emailing the same person twice.
// The reservation is what closes that window, so every one of these has to hold
// against the real routing query, not a mock.

func liveDB(t *testing.T) *db.DB {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	handle, err := db.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { handle.Pool.Close() })
	return handle
}

func liveJobsService(handle *db.DB) *JobsService {
	return &JobsService{
		TaskRepo:             repository.NewTaskRepository(handle.Pool),
		CampaignRepo:         repository.NewCampaignRepostory(handle),
		CampaignProgressRepo: repository.NewCampaignProgressRepository(handle.Pool),
		CampaignLogRepo:      repository.NewCampaignLogRepository(handle),
		ContactRepo:          repository.NewContactRepostory(handle),
	}
}

// nextPair asks routing what it would send next, exactly as the scheduler does.
func (f *sendResultFixture) nextPair(t *testing.T, s *JobsService) *repository.ContactSequencePair {
	t.Helper()
	pairs, _, _, err := s.CampaignProgressRepo.FindRoutedPairs(context.Background(), f.campaign, "created_at", "asc", "", false, false, nil, 1)
	if err != nil {
		t.Fatalf("next pair: %v", err)
	}
	if len(pairs) == 0 {
		return nil
	}
	return &pairs[0]
}

// TestLiveDispatchedSendIsNeverOfferedTwice is the regression the issue asks
// for: dispatch a send, let the progress write fail (here: never happen at
// all), then run routing again. The same (contact, step) must not come back.
func TestLiveDispatchedSendIsNeverOfferedTwice(t *testing.T) {
	handle := liveDB(t)
	ctx := context.Background()
	s := liveJobsService(handle)
	f := newSendResultFixture(t, handle)

	if pair := f.nextPair(t, s); pair == nil || pair.SequenceID != f.step {
		t.Fatalf("precondition: the lead's first step should be routable, got %+v", pair)
	}

	// The tick reserves, dispatches, and then dies before it can stamp.
	taskID := f.dispatch(t, s)

	var sentAt, dispatchedAt *time.Time
	if err := handle.Pool.QueryRow(ctx, `SELECT sent_at, dispatched_at FROM campaign_contact_progress
		WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3`, f.campaign, f.contact, f.step).
		Scan(&sentAt, &dispatchedAt); err != nil {
		t.Fatalf("read progress: %v", err)
	}
	if sentAt != nil {
		t.Fatal("precondition: the stamp must be missing for this to reproduce the bug")
	}
	if dispatchedAt == nil {
		t.Fatal("the send was dispatched without a reservation")
	}

	if pair := f.nextPair(t, s); pair != nil {
		t.Fatalf("routing offered the same step a second time: %+v (this is issue #169)", pair)
	}

	// The day's counters were taken by the reservation, so a lost stamp cannot
	// let the new-lead cap over-send either.
	var sent, newLeads int
	if err := handle.Pool.QueryRow(ctx, `SELECT emails_sent, new_leads_started FROM campaign_daily_sends
		WHERE campaign_id = $1 AND send_date = CURRENT_DATE`, f.campaign).Scan(&sent, &newLeads); err != nil {
		t.Fatalf("read daily counters: %v", err)
	}
	if sent != 1 || newLeads != 1 {
		t.Fatalf("daily counters = %d/%d, want 1/1", sent, newLeads)
	}

	// The worker's own confirmation repairs the missing stamp, so follow-up
	// pacing has the timing it needs.
	if err := s.HandleEmailSent(ctx, models.SendEmailResult{
		TaskID: taskID, Success: true, MessageID: "<repaired@test.local>",
	}); err != nil {
		t.Fatalf("handle sent: %v", err)
	}
	if err := handle.Pool.QueryRow(ctx, `SELECT sent_at FROM campaign_contact_progress
		WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3`, f.campaign, f.contact, f.step).
		Scan(&sentAt); err != nil {
		t.Fatalf("read progress: %v", err)
	}
	if sentAt == nil {
		t.Fatal("EMAIL_SENT did not repair the missing stamp")
	}
	if pair := f.nextPair(t, s); pair != nil {
		t.Fatalf("routing offered the step after the repair: %+v", pair)
	}

	// Repairing twice must not move the stamp: a redelivered EMAIL_SENT is a
	// no-op, not a second send's timestamp.
	first := *sentAt
	if err := s.HandleEmailSent(ctx, models.SendEmailResult{TaskID: taskID, Success: true, MessageID: "<repaired@test.local>"}); err != nil {
		t.Fatalf("handle sent (duplicate): %v", err)
	}
	if err := handle.Pool.QueryRow(ctx, `SELECT sent_at FROM campaign_contact_progress
		WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3`, f.campaign, f.contact, f.step).
		Scan(&sentAt); err != nil {
		t.Fatalf("read progress: %v", err)
	}
	if !sentAt.Equal(first) {
		t.Fatalf("a duplicate EMAIL_SENT moved the stamp from %v to %v", first, *sentAt)
	}
}

// TestLiveReserveSendClaimsAStepExactlyOnce covers the other way the same email
// went out twice: two ticks that both picked the same pair.
func TestLiveReserveSendClaimsAStepExactlyOnce(t *testing.T) {
	handle := liveDB(t)
	ctx := context.Background()
	s := liveJobsService(handle)
	f := newSendResultFixture(t, handle)

	first, err := s.CampaignProgressRepo.ReserveSend(ctx, f.campaign, f.contact, f.step, uuid.New(), uuid.Nil, true)
	if err != nil || !first {
		t.Fatalf("first reservation: claimed=%v err=%v", first, err)
	}
	second, err := s.CampaignProgressRepo.ReserveSend(ctx, f.campaign, f.contact, f.step, uuid.New(), uuid.Nil, true)
	if err != nil {
		t.Fatalf("second reservation: %v", err)
	}
	if second {
		t.Fatal("two ticks both claimed the same step; both would have sent")
	}

	// The refused claim must not have charged the day a second time.
	var sent int
	if err := handle.Pool.QueryRow(ctx, `SELECT emails_sent FROM campaign_daily_sends
		WHERE campaign_id = $1 AND send_date = CURRENT_DATE`, f.campaign).Scan(&sent); err != nil {
		t.Fatalf("read daily counters: %v", err)
	}
	if sent != 1 {
		t.Fatalf("emails_sent = %d after a refused claim, want 1", sent)
	}

	// A step already stamped sent is equally unclaimable.
	if err := s.CampaignProgressRepo.RecordEmailSent(ctx, f.campaign, f.contact, f.step); err != nil {
		t.Fatalf("record sent: %v", err)
	}
	third, err := s.CampaignProgressRepo.ReserveSend(ctx, f.campaign, f.contact, f.step, uuid.New(), uuid.Nil, true)
	if err != nil {
		t.Fatalf("third reservation: %v", err)
	}
	if third {
		t.Fatal("a step already sent was claimed again")
	}
}

// TestLiveReleaseSendReturnsTheStep covers the path where the command provably
// never left: the step goes straight back to routing, with its count, and
// without spending an attempt.
func TestLiveReleaseSendReturnsTheStep(t *testing.T) {
	handle := liveDB(t)
	ctx := context.Background()
	s := liveJobsService(handle)
	f := newSendResultFixture(t, handle)

	if _, err := s.CampaignProgressRepo.ReserveSend(ctx, f.campaign, f.contact, f.step, uuid.New(), uuid.Nil, true); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := s.CampaignProgressRepo.ReleaseSend(ctx, f.campaign, f.contact, f.step, true); err != nil {
		t.Fatalf("release: %v", err)
	}

	pair := f.nextPair(t, s)
	if pair == nil || pair.SequenceID != f.step || !pair.IsNewLead {
		t.Fatalf("a released step should be routable again as a new lead, got %+v", pair)
	}
	var sent, newLeads, attempts int
	if err := handle.Pool.QueryRow(ctx, `SELECT COALESCE(emails_sent, 0), COALESCE(new_leads_started, 0) FROM campaign_daily_sends
		WHERE campaign_id = $1 AND send_date = CURRENT_DATE`, f.campaign).Scan(&sent, &newLeads); err != nil {
		t.Fatalf("read daily counters: %v", err)
	}
	if sent != 0 || newLeads != 0 {
		t.Fatalf("daily counters after release = %d/%d, want 0/0", sent, newLeads)
	}
	if err := handle.Pool.QueryRow(ctx, `SELECT send_attempts FROM campaign_contact_progress
		WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3`, f.campaign, f.contact, f.step).Scan(&attempts); err != nil {
		t.Fatalf("read attempts: %v", err)
	}
	if attempts != 0 {
		t.Fatalf("a send that never left spent %d attempts, want 0", attempts)
	}

	// A release after the step was stamped (the worker answered first) must
	// never undo a real send.
	if err := s.CampaignProgressRepo.RecordEmailSent(ctx, f.campaign, f.contact, f.step); err != nil {
		t.Fatalf("record sent: %v", err)
	}
	if err := s.CampaignProgressRepo.ReleaseSend(ctx, f.campaign, f.contact, f.step, true); err != nil {
		t.Fatalf("late release: %v", err)
	}
	if pair := f.nextPair(t, s); pair != nil {
		t.Fatalf("a late release un-sent a delivered email: %+v", pair)
	}
}

// TestLiveStuckDispatchIsReclaimed covers the send nobody ever answered: the
// worker died mid-send, so no EMAIL_SENT and no EMAIL_FAILED. The lead must not
// sit in flight forever.
func TestLiveStuckDispatchIsReclaimed(t *testing.T) {
	handle := liveDB(t)
	ctx := context.Background()
	s := liveJobsService(handle)
	f := newSendResultFixture(t, handle)

	taskID := f.dispatch(t, s)
	if err := s.TaskRepo.UpdateTaskStatusWithLock(ctx, taskID, "completed"); err != nil {
		t.Fatalf("complete task: %v", err)
	}

	// Nothing is reclaimed while the outcome could still arrive.
	s.reclaimStuckSends(ctx)
	if pair := f.nextPair(t, s); pair != nil {
		t.Fatalf("a fresh dispatch was reclaimed too early: %+v", pair)
	}

	// Age it past the window.
	if _, err := handle.Pool.Exec(ctx, `UPDATE campaign_contact_progress
		SET dispatched_at = NOW() - make_interval(mins => $4)
		WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3`,
		f.campaign, f.contact, f.step, config.CampaignSendReclaimAfterMinutes+5); err != nil {
		t.Fatalf("age dispatch: %v", err)
	}
	s.reclaimStuckSends(ctx)

	var sentAt, dispatchedAt *time.Time
	var attempts int
	if err := handle.Pool.QueryRow(ctx, `SELECT sent_at, dispatched_at, send_attempts FROM campaign_contact_progress
		WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3`, f.campaign, f.contact, f.step).
		Scan(&sentAt, &dispatchedAt, &attempts); err != nil {
		t.Fatalf("read progress: %v", err)
	}
	if sentAt != nil || dispatchedAt != nil {
		t.Fatalf("the reclaimed step still looks in flight: sent=%v dispatched=%v", sentAt, dispatchedAt)
	}
	if attempts != 1 {
		t.Fatalf("the lost send spent %d attempts, want 1", attempts)
	}
	if pair := f.nextPair(t, s); pair == nil || pair.SequenceID != f.step {
		t.Fatalf("a reclaimed step should be retryable, got %+v", pair)
	}
	var sent int
	if err := handle.Pool.QueryRow(ctx, `SELECT emails_sent FROM campaign_daily_sends
		WHERE campaign_id = $1 AND send_date = CURRENT_DATE`, f.campaign).Scan(&sent); err != nil {
		t.Fatalf("read daily counters: %v", err)
	}
	if sent != 0 {
		t.Fatalf("emails_sent = %d after a reclaim, want the count given back", sent)
	}
	var status string
	if err := handle.Pool.QueryRow(ctx, `SELECT status FROM campaigns WHERE id = $1`, f.campaign).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "active" {
		t.Fatalf("campaign status = %s, want active (reopened for the retry)", status)
	}
	var logs int
	if err := handle.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM campaign_logs
		WHERE campaign_id = $1 AND event_type = 'email_failed' AND metadata->>'code' = 'SEND_OUTCOME_LOST'`, f.campaign).Scan(&logs); err != nil {
		t.Fatal(err)
	}
	if logs != 1 {
		t.Fatalf("the lost outcome produced %d activity log entries, want 1", logs)
	}

	// A second pass has nothing left to do.
	s.reclaimStuckSends(ctx)
	if err := handle.Pool.QueryRow(ctx, `SELECT send_attempts FROM campaign_contact_progress
		WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3`, f.campaign, f.contact, f.step).Scan(&attempts); err != nil {
		t.Fatalf("read attempts: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("a second reclaim pass spent another attempt (%d)", attempts)
	}
}

// TestLiveReclaimBelievesADeliveredSend covers the other half: when the worker
// did report a Message-ID, the email left. The reclaimer must stamp it, never
// send it again.
func TestLiveReclaimBelievesADeliveredSend(t *testing.T) {
	handle := liveDB(t)
	ctx := context.Background()
	s := liveJobsService(handle)
	f := newSendResultFixture(t, handle)

	taskID := f.dispatch(t, s)
	if err := s.TaskRepo.UpdateTaskMessageID(ctx, taskID, "<delivered@test.local>"); err != nil {
		t.Fatalf("set message id: %v", err)
	}
	if _, err := handle.Pool.Exec(ctx, `UPDATE campaign_contact_progress
		SET dispatched_at = NOW() - make_interval(mins => $4)
		WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3`,
		f.campaign, f.contact, f.step, config.CampaignSendReclaimAfterMinutes+5); err != nil {
		t.Fatalf("age dispatch: %v", err)
	}

	s.reclaimStuckSends(ctx)

	var sentAt *time.Time
	var attempts int
	if err := handle.Pool.QueryRow(ctx, `SELECT sent_at, send_attempts FROM campaign_contact_progress
		WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3`, f.campaign, f.contact, f.step).
		Scan(&sentAt, &attempts); err != nil {
		t.Fatalf("read progress: %v", err)
	}
	if sentAt == nil {
		t.Fatal("a send the worker confirmed was not stamped")
	}
	if attempts != 0 {
		t.Fatalf("a delivered send was charged %d retry attempts", attempts)
	}
	if pair := f.nextPair(t, s); pair != nil {
		t.Fatalf("a delivered send was offered again: %+v", pair)
	}
}

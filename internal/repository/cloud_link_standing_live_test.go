package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveCloudReconciliationLockPreservesCommittedIntentWithASingleConnectionPool(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	instance := liveCloudLink(t, f)
	defer cancel()
	cfg := f.pool.Config()
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	r := NewCloudLinkRepository(pool, nil)
	entered, release, secondEntered := make(chan struct{}), make(chan struct{}), make(chan struct{})
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	fault := errors.New("remote unavailable")
	go func() {
		firstDone <- r.WithReconciliationLock(ctx, func() error {
			defer close(entered)
			if err := r.BeginEnrollment(ctx, f.sender, f.sender, instance); err != nil {
				return err
			}
			entered <- struct{}{}
			<-release
			return fault
		})
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		close(release)
		t.Fatal("first lock did not enter")
	}
	go func() {
		secondDone <- r.WithReconciliationLock(ctx, func() error {
			close(secondEntered)
			m, err := r.GetByAccount(ctx, f.sender)
			if err != nil {
				return err
			}
			if m == nil || m.EnrollmentState != "pending_enroll" {
				return errors.New("intent rolled back with failed operation")
			}
			return nil
		})
	}()
	select {
	case <-secondEntered:
		close(release)
		t.Fatal("concurrent reconciliation entered before release")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-firstDone; !errors.Is(err, fault) {
		t.Fatalf("first result: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second result: %v", err)
	}
}

func TestLiveCloudStandingFreshnessIsBoundedWithoutInventingAProviderBlock(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx := context.Background()
	links := NewCloudLinkRepository(f.pool, nil)
	if _, err := links.Enroll(ctx, f.sender, f.sender, liveCloudLink(t, f), false); err != nil {
		t.Fatal(err)
	}
	assertState := func(want models.WarmupHealthState, reason string) {
		t.Helper()
		state, _, err := f.warmup.GetHealthState(ctx, f.sender)
		if err != nil || state != want {
			t.Fatalf("standing = %s, %v; want %s", state, err, want)
		}
		h, err := f.warmup.GetCloudStanding(ctx, f.sender)
		if reason != "" && (err != nil || h == nil || h.Reason != reason) {
			t.Fatalf("cloud diagnostics = %+v, %v", h, err)
		}
	}
	assertState(models.WarmupHealthBlocked, "cloud_evidence_unavailable")
	if _, err := links.SetStanding(ctx, f.sender, &models.WarmupHealthInfo{State: "healthy"}, false); err != nil {
		t.Fatal(err)
	}
	assertState(models.WarmupHealthHealthy, "")
	if _, err := f.pool.Exec(ctx, `UPDATE cloud_link_mailboxes SET standing_observed_at=NOW()+INTERVAL '1 minute' WHERE email_account_id=$1`, f.sender); err != nil {
		t.Fatal(err)
	}
	assertState(models.WarmupHealthBlocked, "cloud_evidence_unavailable")
	if _, err := f.pool.Exec(ctx, `UPDATE cloud_link_mailboxes SET standing_observed_at = NOW() - INTERVAL '16 minutes' WHERE email_account_id = $1`, f.sender); err != nil {
		t.Fatal(err)
	}
	assertState(models.WarmupHealthBlocked, "cloud_evidence_unavailable")
	var stored string
	if err := f.pool.QueryRow(ctx, `SELECT health_state FROM cloud_link_mailboxes WHERE email_account_id = $1`, f.sender).Scan(&stored); err != nil || stored != "healthy" {
		t.Fatalf("availability invented provider evidence: %s, %v", stored, err)
	}
	if _, err := links.SetStanding(ctx, f.sender, &models.WarmupHealthInfo{State: "watch"}, false); err != nil {
		t.Fatal(err)
	}
	assertState(models.WarmupHealthWatch, "")
	if err := links.InvalidateStanding(ctx, f.sender); err != nil {
		t.Fatal(err)
	}
	assertState(models.WarmupHealthBlocked, "cloud_evidence_unavailable")
}

func TestLiveCloudPositiveObservationCannotClearAHoldFromAnotherLink(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx := context.Background()
	oldLink, newLink := uuid.New(), uuid.New()
	if _, err := f.pool.Exec(ctx, `INSERT INTO cloud_link (cloud_url, instance_id, token, organization_name, connected_by)
	 VALUES ('https://cloud.example.invalid', $1, 'fixture-sealed', 'Fixture', $2)`, oldLink, f.user); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = f.pool.Exec(ctx, `DELETE FROM cloud_link WHERE instance_id IN ($1, $2)`, oldLink, newLink)
	})
	r := NewCloudLinkRepository(f.pool, nil)
	if _, err := r.Enroll(ctx, f.sender, f.sender, oldLink, false); err != nil {
		t.Fatal(err)
	}
	assertState := func(want models.WarmupHealthState) {
		t.Helper()
		if got, _, err := f.warmup.GetHealthState(ctx, f.sender); err != nil || got != want {
			t.Fatalf("health=%s, err=%v, want=%s", got, err, want)
		}
	}
	if _, err := r.SetStanding(ctx, f.sender, &models.WarmupHealthInfo{State: "blocked"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SetStanding(ctx, f.sender, &models.WarmupHealthInfo{State: "healthy"}, false); err != nil {
		t.Fatal(err)
	}
	assertState(models.WarmupHealthHealthy)
	if _, err := r.SetStanding(ctx, f.sender, &models.WarmupHealthInfo{State: "blocked"}, false); err != nil {
		t.Fatal(err)
	}
	if err := r.Unenroll(ctx, f.sender); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(ctx, oldLink); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO cloud_link (cloud_url, instance_id, token, organization_id)
	 VALUES ('https://cloud.example.invalid', $1, 'fixture-sealed', $2)`, newLink, f.org); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Enroll(ctx, f.sender, f.sender, newLink, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SetStanding(ctx, f.sender, &models.WarmupHealthInfo{State: "healthy"}, false); err != nil {
		t.Fatal(err)
	}
	assertState(models.WarmupHealthBlocked)
	if _, err := r.SetStanding(ctx, f.sender, &models.WarmupHealthInfo{State: "quarantined"}, false); err != nil {
		t.Fatal(err)
	}
	assertState(models.WarmupHealthBlocked)
}

func TestLiveCloudHoldSurvivesUnlinkLocalHealthReviewAndMembershipChange(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx := context.Background()
	links := NewCloudLinkRepository(f.pool, nil)
	if err := f.warmup.MoveToPool(ctx, models.WarmupPoolFreeID, f.sender, "sender_receiver"); err != nil {
		t.Fatal(err)
	}
	if _, err := links.Enroll(ctx, f.sender, f.sender, liveCloudLink(t, f), false); err != nil {
		t.Fatal(err)
	}
	if _, err := links.SetStanding(ctx, f.sender, &models.WarmupHealthInfo{State: "blocked", Reason: "cloud_review"}, false); err != nil {
		t.Fatal(err)
	}
	if err := links.Unenroll(ctx, f.sender); err != nil {
		t.Fatal(err)
	}
	if _, err := f.warmup.UpdateParticipantHealth(ctx, f.sender, models.WarmupHealthHealthy, nil, "local_review", 0); err != nil {
		t.Fatal(err)
	}
	if err := f.warmup.MoveToPool(ctx, models.WarmupPoolPremiumID, f.sender, "recipient_only"); err != nil {
		t.Fatal(err)
	}
	state, _, err := f.warmup.GetHealthState(ctx, f.sender)
	if err != nil || state != models.WarmupHealthBlocked {
		t.Fatalf("durable hold lost: %s, %v", state, err)
	}
	if eligible, err := f.warmup.IsPoolEligible(ctx, f.sender, "premium", false); err != nil || eligible {
		t.Fatalf("held recipient admitted: %v, %v", eligible, err)
	}
	if state, _, err := f.warmup.GetHealthState(ctx, f.recipient); err != nil || state != models.WarmupHealthHealthy {
		t.Fatalf("hold leaked to unrelated mailbox: %s, %v", state, err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE warmup_reputation_ledger SET cloud_blocked_until = NOW() - INTERVAL '1 second' WHERE organization_id = $1`, f.org); err != nil {
		t.Fatal(err)
	}
	if state, _, err := f.warmup.GetHealthState(ctx, f.sender); err != nil || state != models.WarmupHealthHealthy {
		t.Fatalf("dated hold became permanent: %s, %v", state, err)
	}
}

func TestLiveCloudEnrollmentIntentDoesNotReverseAnExplicitOptOut(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx := context.Background()
	links := NewCloudLinkRepository(f.pool, nil)
	instance := liveCloudLink(t, f)
	if err := links.BeginEnrollment(ctx, f.sender, f.sender, instance); err != nil {
		t.Fatal(err)
	}
	if err := links.BeginRemoval(ctx, f.sender); err != nil {
		t.Fatal(err)
	}
	if err := links.BeginEnrollment(ctx, f.sender, f.sender, instance); err == nil {
		t.Fatal("re-enrollment reversed pending opt-out")
	}
	if _, err := links.Enroll(ctx, f.sender, f.sender, instance, false); err == nil {
		t.Fatal("late acknowledgment reversed pending opt-out")
	}
	m, err := links.GetByAccount(ctx, f.sender)
	if err != nil || m == nil || m.EnrollmentState != "pending_remove" {
		t.Fatalf("removal intent lost: %+v, %v", m, err)
	}
	if err := links.Unenroll(ctx, f.sender); err != nil {
		t.Fatal(err)
	}
	if enrolled, err := links.IsEnrolled(ctx, f.sender); err != nil || enrolled {
		t.Fatalf("completed opt-out stayed enrolled: %v, %v", enrolled, err)
	}
	if err := links.BeginEnrollment(ctx, f.sender, f.sender, instance); err != nil {
		t.Fatal(err)
	}
}

// A mailbox Warmbly Cloud warms has no local pool row, so the standing the
// cloud reports is the only health the instance's send gates can read.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveCloudLinkStanding -v

func TestLiveCloudLinkStandingGatesTheInstance(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx := context.Background()
	links := NewCloudLinkRepository(f.pool, nil)
	handle, _ := liveContactDB(t)
	lifecycle := NewSendLifecycleRepository(handle)

	if _, err := links.Enroll(ctx, f.sender, f.sender, liveCloudLink(t, f), false); err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	until := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Second)
	prev, err := links.SetStanding(ctx, f.sender, &models.WarmupHealthInfo{
		State: string(models.WarmupHealthQuarantined), PoolType: "premium", Reason: "complaints", Score: 60, BlockedUntil: &until,
	}, true)
	if err != nil {
		t.Fatalf("SetStanding: %v", err)
	}
	if prev != "" {
		t.Fatalf("first standing replaced %q, want nothing", prev)
	}

	state, gotUntil, err := f.warmup.GetHealthState(ctx, f.sender)
	if err != nil {
		t.Fatalf("GetHealthState: %v", err)
	}
	if state != models.WarmupHealthQuarantined || gotUntil == nil || !gotUntil.Equal(until) {
		t.Fatalf("GetHealthState = %s until %v, want quarantined until %v", state, gotUntil, until)
	}
	states, err := f.warmup.GetHealthStates(ctx, []uuid.UUID{f.sender, f.recipient})
	if err != nil {
		t.Fatalf("GetHealthStates: %v", err)
	}
	if states[f.sender].State != models.WarmupHealthQuarantined || states[f.recipient].State != models.WarmupHealthHealthy {
		t.Fatalf("GetHealthStates = %+v", states)
	}
	cand, err := lifecycle.GetLifecycleCandidate(ctx, f.sender)
	if err != nil {
		t.Fatalf("GetLifecycleCandidate: %v", err)
	}
	if cand.HealthState != models.WarmupHealthQuarantined {
		t.Fatalf("lifecycle sees %q, want quarantined", cand.HealthState)
	}
	info, err := f.warmup.GetCloudStanding(ctx, f.sender)
	if err != nil || info == nil {
		t.Fatalf("GetCloudStanding = %v, %v", info, err)
	}
	if info.Source != models.WarmupHealthSourceCloud || info.PoolType != "premium" || info.Reason != "complaints" {
		t.Fatalf("GetCloudStanding = %+v", info)
	}

	summary, xerr := NewAnalyticsRepository(handle).GetAccountHealthSummary(ctx, f.org)
	if xerr != nil || summary.ErrorAccounts != 1 {
		t.Fatalf("GetAccountHealthSummary = %+v, %v; want the quarantined mailbox in error", summary, xerr)
	}
	risk, err := NewWorkerRepository(f.pool).ListRiskCandidates(ctx, 100000)
	if err != nil {
		t.Fatalf("ListRiskCandidates: %v", err)
	}
	found := false
	for _, c := range risk {
		if c.EmailAccountID == f.sender {
			found = c.HealthState == models.WarmupHealthQuarantined
		}
	}
	if !found {
		t.Fatal("risk candidates do not see the sender quarantined")
	}
	if _, _, err := NewAdminRepository(f.pool).GetWorkerEmails(ctx, uuid.New(), time.Time{}, uuid.Nil, 10); err != nil {
		t.Fatalf("GetWorkerEmails: %v", err)
	}

	prev, err = links.SetStanding(ctx, f.sender, &models.WarmupHealthInfo{State: string(models.WarmupHealthHealthy)}, false)
	if err != nil || prev != models.WarmupHealthQuarantined {
		t.Fatalf("recovery replaced %q (%v), want quarantined", prev, err)
	}
	if state, _, _ := f.warmup.GetHealthState(ctx, f.sender); state != models.WarmupHealthHealthy {
		t.Fatalf("after recovery GetHealthState = %s", state)
	}
}

func TestLiveCloudLinkStandingCarriesToTheLocalPool(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx := context.Background()
	links := NewCloudLinkRepository(f.pool, nil)
	if err := f.warmup.MoveToPool(ctx, models.WarmupPoolFreeID, f.sender, "sender_receiver"); err != nil {
		t.Fatalf("MoveToPool: %v", err)
	}
	until := time.Now().Add(48 * time.Hour)
	held := &models.WarmupHealthInfo{State: string(models.WarmupHealthQuarantined), Reason: "bounces", Score: 55, BlockedUntil: &until}
	if err := links.CarryStanding(ctx, f.sender, held); err != nil {
		t.Fatalf("CarryStanding: %v", err)
	}
	// A milder standing never lowers what the local row already holds.
	if err := links.CarryStanding(ctx, f.sender, &models.WarmupHealthInfo{State: string(models.WarmupHealthWatch)}); err != nil {
		t.Fatalf("CarryStanding watch: %v", err)
	}
	h, err := f.warmup.GetParticipantHealthForAccount(ctx, f.sender)
	if err != nil || h == nil {
		t.Fatalf("participant: %v %v", h, err)
	}
	if h.HealthState != models.WarmupHealthQuarantined || h.BlockedUntil == nil || h.BlockedAt == nil {
		t.Fatalf("carried standing = %+v", h)
	}
}

func TestLivePoolLinkListStandingReportsTheHeldStanding(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx := context.Background()
	repo := NewPoolLinkRepository(f.pool)
	inst := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: f.org, Name: "standing"}
	if err := repo.CreateInstance(ctx, inst, "standing-"+inst.ID.String()); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	remote := uuid.New()
	if err := repo.EnrollMailbox(ctx, &models.PoolLinkMailbox{InstanceID: inst.ID, RemoteID: remote, EmailAccountID: f.sender}); err != nil {
		t.Fatalf("EnrollMailbox: %v", err)
	}
	read := func() *models.WarmupHealthInfo {
		t.Helper()
		list, err := repo.ListStanding(ctx, inst.ID)
		if err != nil || len(list) != 1 || list[0].RemoteID != remote || list[0].Health == nil {
			t.Fatalf("ListStanding = %+v, %v", list, err)
		}
		return list[0].Health
	}

	if h := read(); h.State != string(models.WarmupHealthHealthy) {
		t.Fatalf("no standing reads %q, want healthy", h.State)
	}

	if err := f.warmup.MoveToPool(ctx, models.WarmupPoolFreeID, f.sender, "sender_receiver"); err != nil {
		t.Fatalf("MoveToPool: %v", err)
	}
	until := time.Now().Add(7 * 24 * time.Hour)
	if _, err := f.warmup.UpdateParticipantHealth(ctx, f.sender, models.WarmupHealthQuarantined, &until, "complaints", 70); err != nil {
		t.Fatalf("UpdateParticipantHealth: %v", err)
	}
	if h := read(); h.State != string(models.WarmupHealthQuarantined) || h.PoolType != "free" {
		t.Fatalf("pool standing = %+v", h)
	}

	// Leaving the pool does not lift a hold the address still carries.
	if err := f.warmup.LeaveAllPools(ctx, f.sender); err != nil {
		t.Fatalf("LeaveAllPools: %v", err)
	}
	if h := read(); h.State != string(models.WarmupHealthQuarantined) || h.BlockedUntil == nil {
		t.Fatalf("ledger standing = %+v", h)
	}
}

func TestLiveCloudLinkStandingEdgeCases(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx := context.Background()
	links := NewCloudLinkRepository(f.pool, nil)
	if err := f.warmup.MoveToPool(ctx, models.WarmupPoolFreeID, f.sender, "sender_receiver"); err != nil {
		t.Fatal(err)
	}
	if _, err := links.Enroll(ctx, f.sender, f.sender, liveCloudLink(t, f), false); err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	// An initial write lands once; after that a change is the sync's.
	if _, err := links.SetStanding(ctx, f.sender, &models.WarmupHealthInfo{State: "watch"}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := links.SetStanding(ctx, f.sender, &models.WarmupHealthInfo{State: "healthy"}, true); err != nil {
		t.Fatal(err)
	}
	if state, _, _ := f.warmup.GetHealthState(ctx, f.sender); state != models.WarmupHealthWatch {
		t.Fatalf("an initial write replaced a recorded standing: %s", state)
	}

	// A review-required cloud block outranks a dated local one of the same state.
	soon := time.Now().Add(24 * time.Hour)
	if _, err := f.warmup.UpdateParticipantHealth(ctx, f.sender, models.WarmupHealthBlocked, &soon, "local", 80); err != nil {
		t.Fatal(err)
	}
	if _, err := links.SetStanding(ctx, f.sender, &models.WarmupHealthInfo{State: "blocked"}, false); err != nil {
		t.Fatal(err)
	}
	if state, until, _ := f.warmup.GetHealthState(ctx, f.sender); state != models.WarmupHealthBlocked || until != nil {
		t.Fatalf("tie resolved to %s until %v, want the review-required block", state, until)
	}

	// Carrying keeps the later end at equal severity, and carries a review block.
	later := time.Now().Add(10 * 24 * time.Hour)
	if _, err := f.pool.Exec(ctx, `UPDATE warmup_pool_participants SET health_state = 'quarantined', blocked_until = $2 WHERE email_account_id = $1`, f.sender, soon); err != nil {
		t.Fatal(err)
	}
	if err := links.CarryStanding(ctx, f.sender, &models.WarmupHealthInfo{State: "quarantined", BlockedUntil: &later}); err != nil {
		t.Fatal(err)
	}
	h, _ := f.warmup.GetParticipantHealthForAccount(ctx, f.sender)
	if h == nil || h.BlockedUntil == nil || h.BlockedUntil.Before(later.Add(-time.Second)) {
		t.Fatalf("equal-severity carry kept %+v, want the later end", h)
	}
	if err := links.CarryStanding(ctx, f.sender, &models.WarmupHealthInfo{State: "blocked"}); err != nil {
		t.Fatal(err)
	}
	h, _ = f.warmup.GetParticipantHealthForAccount(ctx, f.sender)
	if h == nil || h.HealthState != models.WarmupHealthBlocked || h.BlockedUntil != nil {
		t.Fatalf("review block carried as %+v", h)
	}
}

func TestLiveCloudLinkStandingExpiredHoldDoesNotMaskALiveOne(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx := context.Background()
	links := NewCloudLinkRepository(f.pool, nil)
	if err := f.warmup.MoveToPool(ctx, models.WarmupPoolFreeID, f.sender, "sender_receiver"); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if _, err := f.pool.Exec(ctx, `UPDATE warmup_pool_participants SET health_state = 'quarantined', blocked_until = $2 WHERE email_account_id = $1`, f.sender, past); err != nil {
		t.Fatal(err)
	}
	if _, err := links.Enroll(ctx, f.sender, f.sender, liveCloudLink(t, f), false); err != nil {
		t.Fatal(err)
	}
	if _, err := links.SetStanding(ctx, f.sender, &models.WarmupHealthInfo{State: "throttled"}, true); err != nil {
		t.Fatal(err)
	}
	if state, _, _ := f.warmup.GetHealthState(ctx, f.sender); state != models.WarmupHealthThrottled {
		t.Fatalf("an ended quarantine masked the live throttle: %s", state)
	}

	// A throttle past its term does not mask a live watch either.
	if _, err := f.pool.Exec(ctx, `UPDATE warmup_pool_participants SET health_state = 'throttled', blocked_until = $2 WHERE email_account_id = $1`, f.sender, past); err != nil {
		t.Fatal(err)
	}
	if _, err := links.SetStanding(ctx, f.sender, &models.WarmupHealthInfo{State: "watch"}, false); err != nil {
		t.Fatal(err)
	}
	if state, _, _ := f.warmup.GetHealthState(ctx, f.sender); state != models.WarmupHealthWatch {
		t.Fatalf("an ended throttle masked the live watch: %s", state)
	}
}

func TestLiveCloudWorkspaceDisconnectDoesNotHoldOrBrokerAnotherWorkspace(t *testing.T) {
	f, other := newPoolLinkFixture(t), newPoolLinkFixture(t)
	ctx := context.Background()
	a, b := liveCloudLink(t, f), liveCloudLink(t, other)
	links := NewCloudLinkRepository(f.pool, nil)
	for _, row := range []struct{ account, instance uuid.UUID }{{f.sender, a}, {other.sender, b}} {
		if _, err := links.Enroll(ctx, row.account, row.account, row.instance, true); err != nil {
			t.Fatal(err)
		}
		if _, err := links.SetStanding(ctx, row.account, &models.WarmupHealthInfo{State: "healthy"}, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := links.SetDisconnectPending(ctx, a); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		account uuid.UUID
		state   models.WarmupHealthState
		allowed bool
	}{{f.sender, models.WarmupHealthBlocked, false}, {other.sender, models.WarmupHealthHealthy, true}} {
		state, _, err := f.warmup.GetHealthState(ctx, row.account)
		if err != nil || state != row.state {
			t.Fatalf("workspace standing = %s, %v; want %s", state, err, row.state)
		}
		allowed, err := links.(CloudManagedConsentRepository).CanBrokerManagedToken(ctx, row.account)
		if err != nil || allowed != row.allowed {
			t.Fatalf("workspace token authority = %v, %v; want %v", allowed, err, row.allowed)
		}
	}
	if err := links.BeginEnrollment(ctx, other.sender, other.sender, a); err == nil {
		t.Fatal("enrollment moved a mailbox to another workspace's instance")
	}
	if _, err := links.Enroll(ctx, other.sender, other.sender, a, true); err == nil {
		t.Fatal("confirmation moved a mailbox to another workspace's instance")
	}
	if err := links.BeginEnrollment(ctx, f.recipient, f.recipient, b); err == nil {
		t.Fatal("new enrollment used another workspace's instance")
	}
	if _, err := links.Enroll(ctx, f.recipient, f.recipient, b, true); err == nil {
		t.Fatal("new confirmation used another workspace's instance")
	}
	if m, err := links.GetByAccount(ctx, f.recipient); err != nil || m != nil {
		t.Fatalf("foreign workspace enrollment persisted: %+v, %v", m, err)
	}
	c := &models.CloudManagedConsent{OrganizationID: f.org, UserID: &f.user, InstanceID: &b}
	allowed, err := links.(CloudManagedConsentRepository).ManagedConsentAuthorized(ctx, c)
	if err != nil || allowed {
		t.Fatalf("foreign workspace instance authorized = %v, %v", allowed, err)
	}
}

func liveCloudLink(t *testing.T, f *poolLinkFixture) uuid.UUID {
	t.Helper()
	var existing uuid.UUID
	if err := f.pool.QueryRow(context.Background(), `SELECT instance_id FROM cloud_link WHERE organization_id = $1`, f.org).Scan(&existing); err == nil {
		return existing
	}
	id := uuid.New()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO cloud_link (cloud_url, instance_id, token, organization_id) VALUES ('https://example.test', $1, 'fixture', $2)`, id, f.org); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM cloud_link WHERE instance_id = $1`, id) })
	return id
}

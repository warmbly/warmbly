package repository

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveWarmupTimeWindowValidatesEffectivePatchAndPreservesHolds(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newWarmupUsageFixture(t, pool)
	r := NewEmailRepostory(handle, nil)
	ptr := func(v string) *string { return &v }
	for _, tc := range []struct {
		name, end string
		patch     models.UpdateEmail
		invalid   bool
	}{
		{"valid pair", "20:00", models.UpdateEmail{WarmupStartTime: ptr("09:00"), WarmupEndTime: ptr("17:00")}, false},
		{"malformed time", "20:00", models.UpdateEmail{WarmupStartTime: ptr("25:00")}, true},
		{"reversed pair", "20:00", models.UpdateEmail{WarmupStartTime: ptr("08:00"), WarmupEndTime: ptr("07:00"), Name: ptr("Changed")}, true},
		{"equal pair", "20:00", models.UpdateEmail{WarmupStartTime: ptr("08:00"), WarmupEndTime: ptr("08:00")}, true},
		{"partial end", "20:00", models.UpdateEmail{WarmupEndTime: ptr("07:00")}, true},
		{"partial start", "20:00", models.UpdateEmail{WarmupStartTime: ptr("21:00")}, true},
		{"valid partial end", "20:00", models.UpdateEmail{WarmupEndTime: ptr("19:00")}, false},
		{"explicit repair", "07:00", models.UpdateEmail{WarmupEndTime: ptr("19:00")}, false},
		{"unrelated legacy patch", "07:00", models.UpdateEmail{Name: ptr("Changed")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.exec(`UPDATE email_accounts SET warmup_start_time='08:00',warmup_end_time=$2,name='Before',send_recovery_hold=true,send_recovery_reason='unknown' WHERE id=$1`, f.account, tc.end)
			_, xerr := r.Update(t.Context(), f.org.String(), f.account.String(), &tc.patch)
			if tc.invalid {
				if xerr == nil || xerr.Code != errx.BadRequest {
					t.Fatalf("invalid window not rejected: %v", xerr)
				}
			} else if xerr != nil {
				t.Fatal(xerr)
			}
			var start, end, name, reason string
			var held bool
			if err := pool.QueryRow(t.Context(), `SELECT warmup_start_time::text,warmup_end_time::text,name,send_recovery_hold,send_recovery_reason FROM email_accounts WHERE id=$1`, f.account).Scan(&start, &end, &name, &held, &reason); err != nil {
				t.Fatal(err)
			}
			if !held || reason != "unknown" {
				t.Fatal("configuration patch released unknown send protection")
			}
			if tc.invalid && (models.ClockHHMM(start) != "08:00" || models.ClockHHMM(end) != tc.end || name != "Before") {
				t.Fatal("rejected patch changed persisted settings")
			}
			if !tc.invalid && tc.patch.WarmupEndTime != nil && models.ClockHHMM(end) != *tc.patch.WarmupEndTime {
				t.Fatal("explicit hours correction was not saved")
			}
			if tc.patch.WarmupStartTime == nil && tc.patch.WarmupEndTime == nil && (models.ClockHHMM(end) != tc.end || models.ClockHHMM(start) != "08:00") {
				t.Fatal("unrelated update changed legacy invalid hours")
			}
		})
	}
	_, xerr := r.Update(t.Context(), uuid.NewString(), f.account.String(), &models.UpdateEmail{WarmupEndTime: ptr("19:00")})
	if xerr == nil || xerr.Code != errx.NotFound {
		t.Fatal("time patch crossed workspace ownership")
	}
}

func TestLiveWarmupTimeWindowSerializesCrossingPartialPatches(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newWarmupUsageFixture(t, pool)
	f.exec(`UPDATE email_accounts SET warmup_start_time='08:00',warmup_end_time='20:00' WHERE id=$1`, f.account)
	r := NewEmailRepostory(handle, nil)
	start, end := "18:00", "17:00"
	results := make(chan *errx.Error, 2)
	begin := make(chan struct{})
	var wg sync.WaitGroup
	for _, patch := range []models.UpdateEmail{{WarmupStartTime: &start}, {WarmupEndTime: &end}} {
		wg.Go(func() {
			<-begin
			_, xerr := r.Update(t.Context(), f.org.String(), f.account.String(), &patch)
			results <- xerr
		})
	}
	close(begin)
	wg.Wait()
	close(results)
	accepted, rejected := 0, 0
	for xerr := range results {
		if xerr == nil {
			accepted++
		} else if xerr.Code == errx.BadRequest {
			rejected++
		} else {
			t.Fatal(xerr)
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("crossing patches accepted=%d rejected=%d", accepted, rejected)
	}
	var valid bool
	if err := pool.QueryRow(t.Context(), `SELECT warmup_start_time<warmup_end_time FROM email_accounts WHERE id=$1`, f.account).Scan(&valid); err != nil || !valid {
		t.Fatalf("invalid concurrent window persisted: valid=%v err=%v", valid, err)
	}
}

func TestLiveWarmupInvalidWindowRemainsVisibleUntilCorrected(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newWarmupUsageFixture(t, pool)
	r := NewMonitoringRepository(pool)
	now := time.Now()
	before := measuredMonitoring(t, collectMonitoring(t, r, "send_safety", now), "warmup_invalid_window")
	f.exec(`UPDATE email_accounts SET warmup=NOW(),warmup_start_time='08:00',warmup_end_time='07:00',test_mode='diagnostic',test_send_enabled=true WHERE id=$1`, f.account)
	bad := measuredMonitoring(t, collectMonitoring(t, r, "send_safety", now), "warmup_invalid_window")
	if *bad.Count != *before.Count+1 || bad.Condition != "safety_hold" || *bad.AffectedMailboxes != *before.AffectedMailboxes+1 || *bad.AffectedOrganizations != *before.AffectedOrganizations+1 || bad.EvidenceAt != nil || bad.LatestEvidenceAt != nil {
		t.Fatal("invalid configured hours lost their scoped protection classification", bad)
	}
	for _, update := range []string{"test_send_enabled=false", "test_send_enabled=true,warmup_paused_at=NOW()", "warmup_paused_at=NULL,warmup_end_time='19:00'"} {
		f.exec(`UPDATE email_accounts SET `+update+` WHERE id=$1`, f.account)
		got := measuredMonitoring(t, collectMonitoring(t, r, "send_safety", now), "warmup_invalid_window")
		if *got.Count != *before.Count {
			t.Fatal("stopped or corrected mailbox remained an invalid-window hold", got)
		}
	}
}

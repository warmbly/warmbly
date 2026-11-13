package repository

import (
	"testing"
	"time"
)

func TestLiveRecordDailyTargetSharesTheSendCountersRow(t *testing.T) {
	f := newCampaignAdmissionFixture(t, 100)
	r := NewWarmupRepository(f.pool)
	at := time.Date(2026, 11, 13, 23, 30, 0, 0, time.FixedZone("UTC-5", -5*3600))
	if err := r.IncrementDailyCount(t.Context(), f.sender, at); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordDailyTarget(t.Context(), f.sender, at, 16); err != nil {
		t.Fatal(err)
	}
	if err := r.IncrementDailyCount(t.Context(), f.sender, at); err != nil {
		t.Fatal(err)
	}
	var rows, sent, target int
	var day string
	if err := f.pool.QueryRow(t.Context(), `SELECT count(*) OVER (),date::text,emails_sent,target_volume FROM warmup_statistics WHERE email_account_id=$1`, f.sender).Scan(&rows, &day, &sent, &target); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || day != "2026-11-14" || sent != 2 || target != 16 {
		t.Fatalf("rows=%d day=%s sent=%d target=%d", rows, day, sent, target)
	}
	if err := r.RecordDailyTarget(t.Context(), f.sender, at, 17); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(t.Context(), `SELECT emails_sent,target_volume FROM warmup_statistics WHERE email_account_id=$1`, f.sender).Scan(&sent, &target); err != nil || sent != 2 || target != 17 {
		t.Fatalf("updated target lost counts: sent=%d target=%d err=%v", sent, target, err)
	}
}

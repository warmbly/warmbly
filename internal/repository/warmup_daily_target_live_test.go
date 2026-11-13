package repository

import (
	"testing"
	"time"
)

func TestLiveRecordDailyTargetKeepsOnePlanPerCalendarDate(t *testing.T) {
	f := newCampaignAdmissionFixture(t, 100)
	r := NewWarmupRepository(f.pool)
	// One local day in UTC-5 spans two UTC dates; its sends land on both rows.
	zone := time.FixedZone("UTC-5", -5*3600)
	morning := time.Date(2026, 11, 13, 9, 0, 0, 0, zone)
	night := time.Date(2026, 11, 13, 20, 30, 0, 0, zone)
	for _, at := range []time.Time{morning, night} {
		if err := r.IncrementDailyCount(t.Context(), f.sender, at); err != nil {
			t.Fatal(err)
		}
		if err := r.RecordDailyTarget(t.Context(), f.sender, at, 16); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.RecordDailyTarget(t.Context(), f.sender, time.Date(2026, 11, 14, 9, 0, 0, 0, zone), 17); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := f.pool.QueryRow(t.Context(), `SELECT string_agg(date::text||':'||emails_sent||'/'||target_volume,',' ORDER BY date) FROM warmup_statistics WHERE email_account_id=$1`, f.sender).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if want := "2026-11-13:1/16,2026-11-14:1/17"; got != want {
		t.Fatalf("rows = %s, want %s", got, want)
	}
}

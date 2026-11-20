package instancesettings

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFullCheckIntervalScalesWithTheAllowance(t *testing.T) {
	s := DefaultSync()
	s.Normalize()
	ten := 10
	zero := 0
	for _, tc := range []struct {
		name      string
		used      int
		allowance *int
		want      time.Duration
	}{
		{"no mailboxes", 0, &ten, 15 * time.Minute},
		{"a quarter", 2, &[]int{8}[0], 15 * time.Minute},
		{"a third", 1, &[]int{3}[0], 20 * time.Minute},
		{"half", 5, &ten, 30 * time.Minute},
		{"at the allowance", 10, &ten, 60 * time.Minute},
		{"twice the allowance", 20, &ten, 60 * time.Minute},
		{"unlimited", 500, nil, 15 * time.Minute},
		{"mailboxes kept on a zero allowance", 3, &zero, 60 * time.Minute},
		{"zero allowance and no mailboxes", 0, &zero, 15 * time.Minute},
	} {
		if got := s.FullCheckInterval(tc.used, tc.allowance); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFullCheckBoundsNormalize(t *testing.T) {
	for _, tc := range []struct {
		name               string
		min, max           int
		wantMin, wantMax   int
		ratioHalfInMinutes time.Duration
	}{
		{"defaults for a document from before the setting", 0, 0, 15, 60, 30},
		{"below the floor", 1, 2, 5, 5, 5},
		{"above the ceiling", 500, 900, 180, 180, 180},
		{"maximum under the minimum is raised to it", 40, 20, 40, 40, 40},
		{"operator values", 10, 120, 10, 120, 60},
	} {
		s := DefaultSync()
		s.FullCheckMinMinutes, s.FullCheckMaxMinutes = tc.min, tc.max
		s.Normalize()
		if s.FullCheckMinMinutes != tc.wantMin || s.FullCheckMaxMinutes != tc.wantMax {
			t.Errorf("%s: %d-%d, want %d-%d", tc.name, s.FullCheckMinMinutes, s.FullCheckMaxMinutes, tc.wantMin, tc.wantMax)
		}
		if got := s.FullCheckInterval(1, &[]int{2}[0]); got != tc.ratioHalfInMinutes*time.Minute {
			t.Errorf("%s: half the allowance gives %v", tc.name, got)
		}
	}
}

func TestPatchFullCheckBounds(t *testing.T) {
	var p Patch
	if err := json.Unmarshal([]byte(`{"sync":{"full_check_min_minutes":20,"full_check_max_minutes":90}}`), &p); err != nil {
		t.Fatal(err)
	}
	doc := p.Apply(Defaults())
	doc.Normalize()
	if doc.Sync.FullCheckMinMinutes != 20 || doc.Sync.FullCheckMaxMinutes != 90 {
		t.Fatalf("patched bounds %d-%d", doc.Sync.FullCheckMinMinutes, doc.Sync.FullCheckMaxMinutes)
	}
	if doc.Sync.DailyMessagesPerMailbox != DefaultSync().DailyMessagesPerMailbox {
		t.Fatal("a patch of the full check bounds moved another sync setting")
	}
}

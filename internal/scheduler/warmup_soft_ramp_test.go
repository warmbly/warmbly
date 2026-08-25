package scheduler

import "testing"

func TestSoftRampAdjust(t *testing.T) {
	if got := softRampAdjust(40, 1, 100); got != 30 {
		t.Fatalf("softRampAdjust() = %d, want 30", got)
	}
	if got := softRampAdjust(40, 0, 100); got != 40 {
		t.Fatalf("clean softRampAdjust() = %d, want 40", got)
	}
	if got := softRampAdjust(2, 1, 2); got < 1 {
		t.Fatalf("softRampAdjust() lost safety floor: %d", got)
	}
}

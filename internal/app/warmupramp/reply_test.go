package warmupramp

import (
	"math"
	"math/rand"
	"testing"
)

func TestReplyDue(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		target, sent, replied int
		want                  bool
	}{
		{"replies off", 0, 40, 0, false},
		{"a fresh mailbox's first send is a new thread", 30, 0, 0, false},
		{"a reply that lands closer to target is due", 30, 1, 0, true},
		{"behind", 30, 19, 2, true},
		{"one more reply would overshoot by more than it closes", 30, 10, 3, false},
		{"just under target", 30, 11, 3, true},
		{"ahead", 30, 9, 6, false},
		{"a full target always replies", 100, 10, 10, true},
		{"over 100 reads as 100", 150, 1, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReplyDue(tc.target, tc.sent, tc.replied); got != tc.want {
				t.Fatalf("ReplyDue(%d,%d,%d)=%v, want %v", tc.target, tc.sent, tc.replied, got, tc.want)
			}
		})
	}
}

func TestReplyChance(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		target, sent, replied int
		want                  float64
	}{
		{"replies off", 0, 40, 0, 0},
		{"a high target replies on a fresh mailbox", 80, 0, 0, 1},
		{"due is repaid at half again the target", 30, 100, 0, 0.45},
		{"just under target still repays at half again", 30, 11, 3, 0.45},
		{"ahead does not reply", 30, 100, 31, 0},
		{"one more reply would overshoot", 30, 10, 3, 0},
		{"never above certain", 80, 100, 0, 1},
		{"a high target stops when ahead", 80, 100, 100, 0},
		{"over 100 reads as 100", 150, 10, 10, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReplyChance(tc.target, tc.sent, tc.replied); math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("ReplyChance(%d,%d,%d)=%v, want %v", tc.target, tc.sent, tc.replied, got, tc.want)
			}
		})
	}
}

// Over the same seven-day window the scheduler reads, the share settles on the
// setting whether arrivals answer most of the owed mail or none of it, and
// whether a new thread always finds a recipient or mostly finds none (a pool
// whose inboxes are at their daily caps refuses new threads but not replies).
// A window with no replies in it is repaid without any day turning into a burst.
func TestReplyShareConvergesWithoutBursts(t *testing.T) {
	const attemptsPerDay, days = 30, 60
	rng := rand.New(rand.NewSource(7))
	for _, target := range []int{10, 30, 60, 100} {
		for _, arrivalBinds := range []float64{0, 0.5, 0.9} {
			for _, newThreadReach := range []float64{1, 0.5, 0.2} {
				sent, replied := make([]int, days+7), make([]int, days+7)
				for i := range 7 {
					sent[i] = attemptsPerDay
				}
				for d := 7; d < days+7; d++ {
					for range attemptsPerDay {
						s, r := 0, 0
						for k := d - 6; k <= d; k++ {
							s, r = s+sent[k], r+replied[k]
						}
						arrival := ReplyDue(target, s, r) && rng.Float64() < arrivalBinds*float64(target)/100
						switch {
						case arrival || rng.Float64() < ReplyChance(target, s, r):
							replied[d]++
							sent[d]++
						case rng.Float64() < newThreadReach:
							sent[d]++
						}
					}
					if day := float64(replied[d]) / attemptsPerDay * 100; target < 100 && day > min(2*float64(target)+12, 100) {
						t.Errorf("target %d%%, arrival binds %.0f%%, reach %.0f%%: day %d replied on %.0f%% of its attempts", target, arrivalBinds*100, newThreadReach*100, d, day)
					}
				}
				s, r := 0, 0
				for k := days; k < days+7; k++ {
					s, r = s+sent[k], r+replied[k]
				}
				if share := float64(r) / float64(s) * 100; math.Abs(share-float64(target)) > 3 {
					t.Errorf("target %d%%, arrival binds %.0f%%, reach %.0f%%: settled at %.1f%%", target, arrivalBinds*100, newThreadReach*100, share)
				}
			}
		}
	}
}

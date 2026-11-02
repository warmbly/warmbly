package warmupramp

import (
	"math"
	"math/rand"
	"testing"
)

func TestReplyDeficit(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		target, sent, replied int
		want                  float64
	}{
		{"replies off", 0, 40, 0, 0},
		{"fresh mailbox owes a share of its first send", 30, 0, 0, 0.3},
		{"behind", 30, 19, 2, 4},
		{"on target", 50, 9, 5, 0},
		{"ahead", 30, 9, 6, -3},
		{"over 100 reads as 100", 150, 1, 0, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReplyDeficit(tc.target, tc.sent, tc.replied); math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("ReplyDeficit(%d,%d,%d)=%v, want %v", tc.target, tc.sent, tc.replied, got, tc.want)
			}
		})
	}
}

func TestReplyDue(t *testing.T) {
	if ReplyDue(0, 0) || ReplyDue(-1, 0) {
		t.Fatal("a mailbox at or ahead of its share was made to reply")
	}
	if !ReplyDue(1, 0.99) || !ReplyDue(2.5, 0.99) {
		t.Fatal("a mailbox a whole reply behind was left a new thread")
	}
	if !ReplyDue(0.4, 0.39) || ReplyDue(0.4, 0.4) {
		t.Fatal("a fractional deficit is not a chance of that size")
	}
}

// The share converges on the setting whether arrivals answer most of the
// owed mail or, as at a high send rate, almost none of it.
func TestReplyShareConvergesOnTarget(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for _, target := range []int{0, 10, 30, 60, 100} {
		for _, arrivalBinds := range []float64{0, 0.1, 0.9} {
			sent, replied := 0, 0
			for range 2000 {
				deficit := ReplyDeficit(target, sent, replied)
				arrival := deficit > 0 && rng.Float64() < arrivalBinds*float64(target)/100
				if arrival || ReplyDue(deficit, rng.Float64()) {
					replied++
				}
				sent++
			}
			share := float64(replied) / float64(sent) * 100
			if math.Abs(share-float64(target)) > 0.5 {
				t.Errorf("target %d%% with arrival binds %.0f%%: share %.2f%%", target, arrivalBinds*100, share)
			}
		}
	}
}

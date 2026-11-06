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

func TestReplyChance(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		target, sent, replied int
		want                  float64
	}{
		{"replies off", 0, 40, 0, 0},
		{"fresh mailbox replies at its target", 30, 0, 0, 0.3},
		{"on target", 30, 10, 3, 0.3},
		{"behind is repaid at up to half again the target", 30, 100, 0, 0.45},
		{"slightly behind leans toward replies", 30, 100, 20, 0.4},
		{"ahead still answers at half the target", 30, 100, 80, 0.15},
		{"never above certain", 80, 100, 0, 1},
		{"a high target still slows when ahead", 80, 100, 100, 0.6},
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
// setting whether arrivals answer most of the owed mail or none of it, and a
// window with no replies in it is repaid without any day turning into a burst.
func TestReplyShareConvergesWithoutBursts(t *testing.T) {
	const perDay, days = 30, 60
	rng := rand.New(rand.NewSource(7))
	for _, target := range []int{10, 30, 60, 100} {
		for _, arrivalBinds := range []float64{0, 0.5, 0.9} {
			sent, replied := make([]int, days+7), make([]int, days+7)
			for i := range 7 {
				sent[i] = perDay
			}
			tail := 0.0
			for d := 7; d < days+7; d++ {
				for range perDay {
					s, r := 0, 0
					for k := d - 6; k <= d; k++ {
						s, r = s+sent[k], r+replied[k]
					}
					arrival := ReplyDeficit(target, s, r) > 0 && rng.Float64() < arrivalBinds*float64(target)/100
					if arrival || rng.Float64() < ReplyChance(target, s, r) {
						replied[d]++
					}
					sent[d]++
				}
				day := float64(replied[d]) / perDay * 100
				if limit := min(2*float64(target)+12, 100); target < 100 && day > limit {
					t.Errorf("target %d%% with arrival binds %.0f%%: day %d replied %.0f%% of its sends", target, arrivalBinds*100, d, day)
				}
				if d >= days {
					tail += day / 7
				}
			}
			if math.Abs(tail-float64(target)) > 5 {
				t.Errorf("target %d%% with arrival binds %.0f%%: settled at %.1f%%", target, arrivalBinds*100, tail)
			}
		}
	}
}

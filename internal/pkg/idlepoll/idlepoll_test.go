package idlepoll

import (
	"testing"
	"time"
)

func TestPaceDrainsBusyQueuesAndSlowsOnEmptyOnes(t *testing.T) {
	p := Pace{Min: 2 * time.Second, Max: 15 * time.Second}
	steps := []struct {
		found int
		full  bool
		want  time.Duration
	}{
		{0, false, 2 * time.Second},
		{0, false, 4 * time.Second},
		{0, false, 8 * time.Second},
		{0, false, 15 * time.Second},
		{0, false, 15 * time.Second},
		{25, true, 0},
		{3, false, 2 * time.Second},
		{0, false, 4 * time.Second},
	}
	for i, s := range steps {
		if got := p.Next(s.found, s.full); got != s.want {
			t.Fatalf("step %d: found %d full %v waited %v, want %v", i, s.found, s.full, got, s.want)
		}
	}
}

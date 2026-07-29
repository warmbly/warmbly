package warmupcontent

import "testing"

func TestAdaptiveThreadTarget(t *testing.T) {
	tests := []struct {
		name       string
		sends      int
		share      int
		floor      int
		wantTarget int
	}{
		{name: "idle library keeps safety floor", sends: 0, share: 70, floor: 0, wantTarget: 200},
		{name: "configured floor wins", sends: 700, share: 70, floor: 300, wantTarget: 300},
		{name: "busy segment grows from demand", sends: 70000, share: 70, floor: 200, wantTarget: 350},
		{name: "extreme volume stays bounded", sends: 10000000, share: 100, floor: 200, wantTarget: 5000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AdaptiveThreadTarget(tt.sends, tt.share, tt.floor); got != tt.wantTarget {
				t.Fatalf("AdaptiveThreadTarget(%d, %d, %d) = %d, want %d", tt.sends, tt.share, tt.floor, got, tt.wantTarget)
			}
		})
	}
}

package warmupcontent

import "testing"

func TestAdaptiveThreadTarget(t *testing.T) {
	tests := []struct {
		name       string
		sends      int
		floor      int
		wantTarget int
	}{
		{name: "idle library keeps safety floor", sends: 0, floor: 0, wantTarget: 200},
		{name: "configured floor wins", sends: 700, floor: 300, wantTarget: 300},
		{name: "valid smaller floor survives", sends: 0, floor: 12, wantTarget: 12},
		{name: "busiest mailbox drives generation demand", sends: 10500, floor: 200, wantTarget: 1500},
		{name: "busy segment grows from demand", sends: 70000, floor: 200, wantTarget: 5000},
		{name: "extreme volume stays bounded", sends: 10000000, floor: 200, wantTarget: 5000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AdaptiveThreadTarget(tt.sends, tt.floor); got != tt.wantTarget {
				t.Fatalf("AdaptiveThreadTarget(%d, %d) = %d, want %d", tt.sends, tt.floor, got, tt.wantTarget)
			}
		})
	}
}

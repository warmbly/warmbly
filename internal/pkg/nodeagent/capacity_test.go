package nodeagent

import (
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func usage(residentMB, limitMB int) models.NodeUsage {
	return models.NodeUsage{ResidentMB: &residentMB, MemoryLimitMB: &limitMB}
}

func TestMemoryCapacityFollowsMeasuredMailboxCost(t *testing.T) {
	held := 0
	c := &MemoryCapacity{Mailboxes: func() int { return held }}

	// First beat, nothing loaded: the 100 MB process is the baseline and the default cost applies.
	if got := c.Target(usage(100, 4000)); got != (4000*0.7-100)/defaultMailboxMB {
		t.Fatalf("empty worker planned %v", got)
	}
	// 1,000 mailboxes measured at 2 MB each halve the plan.
	held = 1000
	if got := c.Target(usage(2100, 4000)); got != (4000*0.7-100)/2 {
		t.Fatalf("measured worker planned %v", got)
	}
	// A bigger machine with the same mailboxes plans for more.
	if got := c.Target(usage(2100, 16000)); got <= (4000*0.7-100)/2 {
		t.Fatalf("a larger machine did not plan for more, got %v", got)
	}
}

func TestMemoryCapacitySmoothsAndBounds(t *testing.T) {
	held := 1000
	c := &MemoryCapacity{Mailboxes: func() int { return held }}
	c.Target(usage(100, 4000))
	steady := c.Target(usage(1100, 4000))
	if spike := c.Target(usage(5100, 4000)); spike < steady/5 {
		t.Fatalf("one heavy beat swung the plan from %v to %v", steady, spike)
	}
	if got := c.Target(usage(100, 100)); got != capacityFloor {
		t.Fatalf("a machine with no room planned %v, want the floor", got)
	}
	if got := (&MemoryCapacity{}).Target(models.NodeUsage{}); got != 0 {
		t.Fatalf("a beat without memory must leave the stored target, got %v", got)
	}
}

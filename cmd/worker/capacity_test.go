package main

import "testing"

func TestWorkerCapacityTarget(t *testing.T) {
	t.Setenv("WARMBLY_WORKER_CAPACITY", "250")
	if got := workerCapacityTarget(); got != 250 {
		t.Fatalf("configured capacity = %.0f, want 250", got)
	}

	t.Setenv("WARMBLY_WORKER_CAPACITY", "")
	if got := workerCapacityTarget(); got != 100 {
		t.Fatalf("default capacity = %.0f, want 100", got)
	}

	t.Setenv("WARMBLY_WORKER_CAPACITY", "invalid")
	if got := workerCapacityTarget(); got != 100 {
		t.Fatalf("invalid capacity = %.0f, want safe default 100", got)
	}

	for _, invalid := range []string{"NaN", "+Inf", "100000000"} {
		t.Run(invalid, func(t *testing.T) {
			t.Setenv("WARMBLY_WORKER_CAPACITY", invalid)
			if got := workerCapacityTarget(); got != 100 {
				t.Fatalf("capacity %q = %v, want safe default 100", invalid, got)
			}
		})
	}
}

func TestWorkerCapacityAutoSizesFromMemory(t *testing.T) {
	t.Setenv("WARMBLY_WORKER_CAPACITY", "auto")
	if workerCapacityFromMemory(func() int { return 0 }) == nil {
		t.Fatal("auto did not size the target from memory")
	}
	if got := workerCapacityTarget(); got != defaultWorkerCapacityTarget {
		t.Fatalf("auto's first beat before a memory reading falls back to %v, got %v", defaultWorkerCapacityTarget, got)
	}
	t.Setenv("WARMBLY_WORKER_CAPACITY", "500")
	if workerCapacityFromMemory(func() int { return 0 }) != nil {
		t.Fatal("a fixed target was replaced by the memory sizing")
	}
}

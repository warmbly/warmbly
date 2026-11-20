package leader

import (
	"context"
	"testing"
	"time"
)

func TestRunWithNoLoopsReturnsWithoutConnecting(t *testing.T) {
	e := New(nil, "empty")
	done := make(chan struct{})
	go func() { e.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("an elector with nothing to run held on")
	}
}

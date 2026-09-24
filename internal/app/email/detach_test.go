package email

import (
	"context"
	"testing"
	"time"
)

func TestDetachOutlivesTheCallerButNotItsDeadline(t *testing.T) {
	caller, leave := context.WithCancel(context.Background())
	ctx, cancel := detach(caller, time.Minute)
	defer cancel()
	leave()
	if ctx.Err() != nil {
		t.Fatal("a refresh cancelled the connect")
	}
	if d, ok := ctx.Deadline(); !ok || time.Until(d) > time.Minute {
		t.Fatalf("deadline = %v, %v", d, ok)
	}

	short, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	ctx2, cancel2 := detach(short, time.Hour)
	defer cancel2()
	if d, _ := ctx2.Deadline(); time.Until(d) > time.Second {
		t.Fatalf("the caller's earlier deadline was dropped: %v", d)
	}
}

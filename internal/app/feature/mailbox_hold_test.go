package feature

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// stubMailboxHold holds the mailboxes in held; err fails every read.
type stubMailboxHold struct {
	stubHold
	held map[uuid.UUID]bool
	err  error
}

func (h stubMailboxHold) MailboxWarmupHeld(_ context.Context, _, mailbox uuid.UUID) (bool, error) {
	return h.held[mailbox], h.err
}

// A mailbox its partner left off (or warmup only for campaigns) warms only as its enablement allows,
// through the same gate every warmup decision reads.
func TestCanWarmMailboxReadsThePartnerEnablement(t *testing.T) {
	ctx, org := context.Background(), uuid.New()
	off, on := uuid.New(), uuid.New()
	g := &featureGateService{selfHost: true}
	if ok, xerr := CanWarmMailbox(ctx, g, org, off); !ok || xerr != nil {
		t.Fatalf("unmanaged mailbox: ok=%v err=%v", ok, xerr)
	}
	WithPartnerHold(g, stubMailboxHold{held: map[uuid.UUID]bool{off: true}})
	if ok, xerr := CanWarmMailbox(ctx, g, org, off); ok || xerr != nil {
		t.Fatalf("mailbox left off: ok=%v err=%v, want a definite no", ok, xerr)
	}
	if ok, xerr := CanWarmMailbox(ctx, g, org, on); !ok || xerr != nil {
		t.Fatalf("enabled mailbox: ok=%v err=%v", ok, xerr)
	}
	// The workspace hold wins over a mailbox's own enablement.
	WithPartnerHold(g, stubMailboxHold{stubHold: stubHold{paused: true}, held: map[uuid.UUID]bool{}})
	if ok, _ := CanWarmMailbox(ctx, g, org, on); ok {
		t.Fatal("a paused workspace's enabled mailbox warmed")
	}
	// An unreadable enablement is unknown, never a yes.
	WithPartnerHold(g, stubMailboxHold{err: errors.New("db down")})
	if ok, xerr := CanWarmMailbox(ctx, g, org, on); ok || xerr == nil {
		t.Fatalf("unreadable enablement: ok=%v err=%v", ok, xerr)
	}
	if held, xerr := MailboxWarmupHeld(ctx, g, org, on); !held || xerr == nil {
		t.Fatalf("unreadable enablement alone: held=%v err=%v", held, xerr)
	}
}

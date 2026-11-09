package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// The tag is read and written only inside the mailbox's organization, and the send path sees it:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveWarmupFilterTag -v
func TestLiveWarmupFilterTagIsScopedAndReadBySends(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	other := newUniboxFolderFixture(t, handle.Pool)
	tags := NewWarmupFilterTagRepository(handle.Pool)
	warmup := NewWarmupRepository(handle.Pool).(*warmupRepository)
	ctx := context.Background()

	if tag, found, err := tags.Get(ctx, f.org, f.mailbox); err != nil || !found || tag != nil {
		t.Fatalf("a new mailbox's tag = %v, found %v, err %v; want off", tag, found, err)
	}
	if got, err := warmup.RecipientWarmupFilterTag(ctx, f.mailbox); err != nil || got != "" {
		t.Fatalf("send path saw %q (%v) for a mailbox with no tag", got, err)
	}

	code := "k7q2-mx94-ab3d"
	if found, err := tags.Set(ctx, f.org, f.mailbox, &code); err != nil || !found {
		t.Fatalf("Set: found %v, err %v", found, err)
	}
	if got, err := warmup.RecipientWarmupFilterTag(ctx, f.mailbox); err != nil || got != code {
		t.Fatalf("send path saw %q (%v), want %q", got, err, code)
	}

	// Another workspace can neither read nor change it.
	if _, found, err := tags.Get(ctx, other.org, f.mailbox); err != nil || found {
		t.Fatalf("foreign read: found %v, err %v", found, err)
	}
	if found, err := tags.Set(ctx, other.org, f.mailbox, nil); err != nil || found {
		t.Fatalf("foreign write: found %v, err %v", found, err)
	}
	if got, _ := warmup.RecipientWarmupFilterTag(ctx, f.mailbox); got != code {
		t.Fatal("a foreign write changed the tag")
	}

	if found, err := tags.Set(ctx, f.org, f.mailbox, nil); err != nil || !found {
		t.Fatalf("clear: found %v, err %v", found, err)
	}
	if got, _ := warmup.RecipientWarmupFilterTag(ctx, f.mailbox); got != "" {
		t.Fatalf("a cleared tag still reached sends: %q", got)
	}
	if got, err := warmup.RecipientWarmupFilterTag(ctx, uuid.New()); err != nil || got != "" {
		t.Fatalf("unknown mailbox = %q, %v", got, err)
	}

	// A failed read is an error, so the send retries instead of going out without an opted-in tag.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := warmup.RecipientWarmupFilterTag(cancelled, f.mailbox); err == nil {
		t.Fatal("a failed read reported no tag and no error")
	}
}

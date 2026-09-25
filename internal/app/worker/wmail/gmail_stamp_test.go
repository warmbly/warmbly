package wmail

import (
	"context"
	"errors"
	"testing"
)

type fakeStamp struct {
	id  string
	err error
}

func (f fakeStamp) SentMessageID(context.Context, string) (string, error) { return f.id, f.err }

// Gmail can replace the Message-ID we mint, and replies to the send quote the
// stamped one, so that is what the result reports.
func TestGmailSentMessageIDReportsTheStamp(t *testing.T) {
	ctx := context.Background()
	minted := "<3f1c@gmail.com>"
	if got := gmailSentMessageID(ctx, fakeStamp{id: "<CABx@mail.gmail.com>"}, "t", "18f", minted); got != "<CABx@mail.gmail.com>" {
		t.Fatalf("stamped id not reported: %q", got)
	}
	if got := gmailSentMessageID(ctx, fakeStamp{err: errors.New("boom")}, "t", "18f", minted); got != minted {
		t.Fatalf("a failed read must fall back to the minted id, got %q", got)
	}
	if got := gmailSentMessageID(ctx, fakeStamp{}, "t", "18f", minted); got != minted {
		t.Fatalf("an empty header must fall back to the minted id, got %q", got)
	}
	if got := gmailSentMessageID(ctx, fakeStamp{id: "<x@y>"}, "t", "", minted); got != minted {
		t.Fatalf("no Gmail id means nothing to read, got %q", got)
	}
}

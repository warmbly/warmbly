package aitools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
)

type countingEmails struct {
	stubEmails
	deleted int
}

func (c *countingEmails) Delete(context.Context, string, string) *errx.Error {
	c.deleted++
	return nil
}

func TestDisconnectToolHonoursTheMailboxLock(t *testing.T) {
	locked := uuid.New()
	lock := func(_ context.Context, _, accountID uuid.UUID) *errx.Error {
		if accountID == locked {
			return errx.NewWithIdentifier(errx.Forbidden, "purchased_mailbox", "Cancel it under Subscription instead.")
		}
		return nil
	}
	emails := &countingEmails{}
	d := Deps{Emails: emails, MailboxLock: &lock}
	inv := Invocation{OrgID: uuid.New(), UserID: uuid.New()}

	args, _ := json.Marshal(map[string]string{"email_account_id": locked.String()})
	if _, err := d.disconnectMailbox(context.Background(), inv, args); err == nil || emails.deleted != 0 {
		t.Fatalf("a locked mailbox was removed: err=%v deleted=%d", err, emails.deleted)
	}

	args, _ = json.Marshal(map[string]string{"email_account_id": uuid.NewString()})
	if _, err := d.disconnectMailbox(context.Background(), inv, args); err != nil || emails.deleted != 1 {
		t.Fatalf("an unlocked mailbox was not removed: err=%v deleted=%d", err, emails.deleted)
	}
}

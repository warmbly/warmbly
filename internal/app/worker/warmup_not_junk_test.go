package worker

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/models"
)

// callLog records the warmup actions an IMAP connection is asked for, in order.
type callLog struct {
	wmail.ImapConn
	calls []string
}

func (c *callLog) MarkNotJunk(_ context.Context, box string, uid uint32) error {
	c.calls = append(c.calls, fmt.Sprintf("not_junk %s %d", box, uid))
	return nil
}

func (c *callLog) MoveToFolder(_ context.Context, src, dst string, uid uint32) (bool, error) {
	c.calls = append(c.calls, fmt.Sprintf("move %s>%s %d", src, dst, uid))
	return true, nil
}

func (c *callLog) RemoveFromSpam(_ context.Context, src, inbox string, uid uint32) error {
	c.calls = append(c.calls, fmt.Sprintf("rescue %s>%s %d", src, inbox, uid))
	return nil
}

func runImap(t *testing.T, arrived string, placement string, actions ...string) []string {
	t.Helper()
	conn := &callLog{}
	mail := &wmail.WMail{SmtpImapData: &wmail.SmtpImapData{
		ImapClient: conn,
		Mailboxes: []*models.Mailbox{
			{Name: "INBOX", UIDValidity: 1},
			{Name: "Junk", Attrs: []string{"\\Junk"}, UIDValidity: 2},
		},
	}}
	validity := uint32(1)
	if arrived == "Junk" {
		validity = 2
	}
	action := models.WarmupEmailAction{
		EmailID: uuid.New(), UID: 41, MailboxFolder: arrived, MailboxUIDValidity: validity,
		Placement: placement, TargetFolder: "Warmbly", Actions: actions,
	}
	if err := (&WorkerService{}).runImapWarmupActions(context.Background(), mail, action); err != nil {
		t.Fatalf("run: %v", err)
	}
	return conn.calls
}

// The not-junk keywords go on while the message is still in Junk: once either
// move lands, the UID they would be stored against is gone.
func TestImapWarmupMarksNotJunkBeforeLeavingJunk(t *testing.T) {
	t.Run("filed out of junk", func(t *testing.T) {
		got := runImap(t, "Junk", models.WarmupPlacementFolder, models.WarmupActionFile, models.WarmupActionRescueFromSpam)
		want := []string{"not_junk Junk 41", "move Junk>Warmbly 41"}
		if !slices.Equal(got, want) {
			t.Errorf("calls = %v, want %v", got, want)
		}
	})
	t.Run("rescued to the inbox", func(t *testing.T) {
		got := runImap(t, "Junk", models.WarmupPlacementInbox, models.WarmupActionRescueFromSpam)
		want := []string{"not_junk Junk 41", "rescue Junk>INBOX 41"}
		if !slices.Equal(got, want) {
			t.Errorf("calls = %v, want %v", got, want)
		}
	})
	t.Run("mail that reached the inbox is left unmarked", func(t *testing.T) {
		got := runImap(t, "INBOX", models.WarmupPlacementFolder, models.WarmupActionFile, models.WarmupActionRescueFromSpam)
		want := []string{"move INBOX>Warmbly 41"}
		if !slices.Equal(got, want) {
			t.Errorf("calls = %v, want %v", got, want)
		}
	})
}

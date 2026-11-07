package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// A campaign that rotates follow-ups sends each step from another mailbox, and
// every threaded step still references the whole conversation so the
// recipient's client can keep it together.
//
//	WARMBLY_TEST_DB=... go test ./internal/tasks/ -run LiveRotatedFollowUps -v
func TestLiveRotatedFollowUpsThreadAcrossMailboxes(t *testing.T) {
	handle := liveCampaignDB(t)
	sender := &recordingSender{}
	svc := liveCampaignService(t, handle, sender)
	f := newCampaignSendFixture(t, handle.Pool)
	ctx := context.Background()
	f.otherMailbox(t, handle.Pool)
	if _, err := handle.Pool.Exec(ctx, `UPDATE campaigns SET follow_up_sender = 'rotate' WHERE id = $1`, f.campaign); err != nil {
		t.Fatalf("rotate follow-ups: %v", err)
	}
	second := f.addFollowUp(t, handle.Pool, "", true)
	third := uuid.New()
	if _, err := handle.Pool.Exec(ctx, `INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
	        body_plain, body_html, wait_after, position, kind, thread_reply)
	     VALUES ($1, $2, $3, 'Step 3', '', 'Last', '<p>Last</p>', 0, 2, 'email', true)`,
		third, f.campaign, f.org); err != nil {
		t.Fatalf("third step: %v", err)
	}
	conditions, err := json.Marshal(models.BranchConditions{
		Branches: []models.Branch{{BranchID: uuid.New().String(), TargetSequenceID: &third}},
	})
	if err != nil {
		t.Fatalf("marshal conditions: %v", err)
	}
	if _, err := handle.Pool.Exec(ctx, `UPDATE sequences SET conditions = $2 WHERE id = $1`, second, conditions); err != nil {
		t.Fatalf("connect steps: %v", err)
	}

	ids := make([]string, 3)
	for i := range ids {
		ids[i] = fmt.Sprintf("<step%d.%s%smail.test>", i+1, f.contact.String()[:8], "@")
		f.tick(t, svc)
		f.confirmSend(t, handle.Pool, ids[i], "")
	}
	if sender.count() != 3 {
		t.Fatalf("%d sends, want all three steps", sender.count())
	}
	one, two, three := sender.message(t, 0), sender.message(t, 1), sender.message(t, 2)
	if one.From == two.From || two.From == three.From {
		t.Fatalf("senders %s, %s, %s: each follow-up should come from another mailbox", one.From, two.From, three.From)
	}
	if three.InReplyTo != ids[1] {
		t.Errorf("in_reply_to = %q, want the previous step %q", three.InReplyTo, ids[1])
	}
	want := []string{trimAngle(ids[0]), trimAngle(ids[1])}
	if fmt.Sprint(three.References) != fmt.Sprint(want) {
		t.Errorf("references = %v, want the whole conversation %v", three.References, want)
	}
	if two.Subject != "Hi" || three.Subject != "Hi" {
		t.Errorf("subjects %q, %q, want the conversation's", two.Subject, three.Subject)
	}

	var reassigned int
	if err := handle.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM campaign_logs WHERE campaign_id = $1 AND event_type = 'sender_reassigned'`,
		f.campaign).Scan(&reassigned); err != nil {
		t.Fatalf("logs: %v", err)
	}
	if reassigned != 0 {
		t.Errorf("%d sender_reassigned lines for a campaign that rotates on purpose", reassigned)
	}
}

func trimAngle(id string) string { return id[1 : len(id)-1] }

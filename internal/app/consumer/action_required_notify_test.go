package jobs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

type capturedOrgNotice struct {
	orgID    uuid.UUID
	perm     models.OrganizationPermission
	message  uuid.UUID
	category models.NotificationCategory
	title    string
	body     string
	link     string
	groupKey string
}

type captureOrgNotifier struct{ got chan capturedOrgNotice }

func (c *captureOrgNotifier) NotifyOrg(context.Context, uuid.UUID, models.OrganizationPermission, uuid.UUID, models.NotificationCategory, string, string, string, map[string]any, string) {
}

func (c *captureOrgNotifier) NotifyOrgAboutMessage(_ context.Context, orgID uuid.UUID, perm models.OrganizationPermission, message uuid.UUID, category models.NotificationCategory, title, body, link string, _ map[string]any, groupKey string) {
	c.got <- capturedOrgNotice{orgID, perm, message, category, title, body, link, groupKey}
}

// The notice goes to members who both keep mailboxes running and can open the
// message, is tied to the message, and never carries the sender's subject.
func TestNotifyActionRequiredReachesMailboxManagers(t *testing.T) {
	n := &captureOrgNotifier{got: make(chan capturedOrgNotice, 1)}
	s := &JobsService{Notifier: n}
	orgID := uuid.New()
	msg := &models.EmailMessageStoreData{
		ID:       uuid.New(),
		EmailID:  uuid.New(),
		ThreadID: "thread/1",
		Subject:  "Your account is suspended, verify at evil.example",
	}

	s.notifyActionRequired(orgID, "sales@acme.test", msg)

	var got capturedOrgNotice
	select {
	case got = <-n.got:
	case <-time.After(2 * time.Second):
		t.Fatal("no notification raised")
	}
	if got.orgID != orgID || got.message != msg.ID || got.category != models.NotifInboxActionRequired {
		t.Fatalf("notice = %+v", got)
	}
	if got.perm != models.PermManageEmails|models.PermAccessUnibox {
		t.Fatalf("perm = %b, want manage mailboxes and use the inbox", got.perm)
	}
	if got.title != "Message to review in sales@acme.test" || got.link != "/app/unibox/all/thread%2F1" || got.groupKey == "" {
		t.Fatalf("notice = %+v", got)
	}
	if strings.Contains(got.title+got.body, "evil.example") {
		t.Fatalf("the sender's subject reached the notification: %+v", got)
	}
	if !strings.Contains(got.body, "unverified") || !strings.Contains(got.body, "not a Warmbly connection check") {
		t.Fatalf("the notification must distinguish an incoming claim from Warmbly's own diagnosis: %+v", got)
	}
}

func TestNotifyActionRequiredWithoutNotifierIsQuiet(t *testing.T) {
	s := &JobsService{}
	s.notifyActionRequired(uuid.New(), "", &models.EmailMessageStoreData{ID: uuid.New()})
}

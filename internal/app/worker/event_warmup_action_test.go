package worker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type warmupActionAuthorityStub struct {
	repository.SyncContextRepository
	err error
}

func (s *warmupActionAuthorityStub) PermittedWarmupActions(context.Context, uuid.UUID, uuid.UUID, []string) ([]string, error) {
	return nil, s.err
}

func TestWarmupActionAuthorityDenialDoesNotTouchMailbox(t *testing.T) {
	w := &WorkerService{ID: uuid.NewString(), SyncContextRepository: &warmupActionAuthorityStub{}}
	for _, action := range []string{models.WarmupActionFile, models.WarmupActionDelete, models.WarmupActionVerifyRemoval} {
		if err := w.HandleWarmupAction(t.Context(), models.WarmupEmailAction{EmailID: uuid.New(), Actions: []string{action}, FilingID: uuid.NewString()}); err != nil {
			t.Fatalf("denied %s action must finish without loading a mailbox: %v", action, err)
		}
	}
}

func TestWarmupActionAuthorityFailureMustRetry(t *testing.T) {
	failure := errors.New("authority unavailable")
	w := &WorkerService{ID: uuid.NewString(), SyncContextRepository: &warmupActionAuthorityStub{err: failure}}
	err := w.HandleWarmupAction(t.Context(), models.WarmupEmailAction{EmailID: uuid.New(), Actions: []string{models.WarmupActionDelete}})
	if !errors.Is(err, failure) {
		t.Fatalf("authority failure must remain retryable: %v", err)
	}
}

func TestWarmupFilingFallsBackForOlderEvents(t *testing.T) {
	// An event published before the placement fields existed has to behave the
	// way every mailbox did then: the default folder.
	placement, folder := warmupFiling(models.WarmupEmailAction{})
	if placement != models.WarmupPlacementFolder || folder != config.WarmupFolderDefault {
		t.Fatalf("empty action = (%q, %q), want (%q, %q)", placement, folder, models.WarmupPlacementFolder, config.WarmupFolderDefault)
	}

	placement, folder = warmupFiling(models.WarmupEmailAction{Placement: "elsewhere", TargetFolder: "  "})
	if placement != models.WarmupPlacementFolder || folder != config.WarmupFolderDefault {
		t.Fatalf("unknown placement = (%q, %q), want the defaults", placement, folder)
	}

	placement, folder = warmupFiling(models.WarmupEmailAction{Placement: models.WarmupPlacementArchive, TargetFolder: "Reputation"})
	if placement != models.WarmupPlacementArchive || folder != "Reputation" {
		t.Fatalf("explicit action = (%q, %q), want (archive, Reputation)", placement, folder)
	}
}

// searchStub answers FindUIDByMessageID from a folder→uid table and records
// which folders were asked, so the search order is asserted rather than assumed.
type searchStub struct {
	found map[string]uint32
	fail  map[string]bool
	asked []string
}

func (s *searchStub) FindUIDByMessageID(_ context.Context, mailbox, _ string) (uint32, error) {
	s.asked = append(s.asked, mailbox)
	if s.fail[mailbox] {
		return 0, errors.New("select refused")
	}
	return s.found[mailbox], nil
}

func warmupAction() models.WarmupEmailAction {
	return models.WarmupEmailAction{
		EmailID:      uuid.New(),
		UID:          41,
		RFCMessageID: "<a@b.test>",
	}
}

func TestLocateWarmupMessage(t *testing.T) {
	w := &WorkerService{}
	junk := &models.Mailbox{Name: "Junk", Attrs: []string{"\\Junk"}}
	inboxName := "INBOX"

	t.Run("the filing leg acts where the mail arrived", func(t *testing.T) {
		stub := &searchStub{}
		box, uid, _ := w.locateWarmupMessage(context.Background(), stub, warmupAction(), junk, true, "Warmbly", inboxName, "Sent")
		if box != "Junk" || uid != 41 {
			t.Fatalf("got (%q, %d), want (Junk, 41)", box, uid)
		}
		if len(stub.asked) != 0 {
			t.Fatalf("the filing leg should not search: asked %v", stub.asked)
		}
	})

	t.Run("the delayed leg follows the message into the destination", func(t *testing.T) {
		// This is the case the delayed leg used to get wrong: the UID it carries
		// belongs to the arrival folder, which no longer has the message.
		stub := &searchStub{found: map[string]uint32{"Warmbly": 7}}
		box, uid, _ := w.locateWarmupMessage(context.Background(), stub, warmupAction(), junk, false, "Warmbly", inboxName, "Sent")
		if box != "Warmbly" || uid != 7 {
			t.Fatalf("got (%q, %d), want (Warmbly, 7)", box, uid)
		}
		if len(stub.asked) == 0 || stub.asked[0] != "Warmbly" {
			t.Fatalf("destination should be searched first, asked %v", stub.asked)
		}
	})

	t.Run("a rescued message is followed into the inbox", func(t *testing.T) {
		// Nothing is filed for the inbox placement, so the only move that
		// happened was the rescue out of Junk.
		stub := &searchStub{found: map[string]uint32{"INBOX": 12}}
		box, uid, _ := w.locateWarmupMessage(context.Background(), stub, warmupAction(), junk, false, "", inboxName, "Sent")
		if box != "INBOX" || uid != 12 {
			t.Fatalf("got (%q, %d), want (INBOX, 12)", box, uid)
		}
	})

	t.Run("a message nobody moved keeps its arrival folder and UID", func(t *testing.T) {
		stub := &searchStub{}
		box, uid, _ := w.locateWarmupMessage(context.Background(), stub, warmupAction(), junk, false, "Warmbly", inboxName, "Sent")
		if box != "Junk" || uid != 41 {
			t.Fatalf("got (%q, %d), want the arrival folder and UID", box, uid)
		}
	})

	t.Run("a folder that refuses a search does not end the hunt", func(t *testing.T) {
		stub := &searchStub{fail: map[string]bool{"Warmbly": true}, found: map[string]uint32{"INBOX": 3}}
		box, uid, _ := w.locateWarmupMessage(context.Background(), stub, warmupAction(), junk, false, "Warmbly", inboxName, "Sent")
		if box != "INBOX" || uid != 3 {
			t.Fatalf("got (%q, %d), want (INBOX, 3)", box, uid)
		}
	})

	t.Run("no source folder and nothing found is nothing to act on", func(t *testing.T) {
		stub := &searchStub{}
		box, uid, _ := w.locateWarmupMessage(context.Background(), stub, warmupAction(), nil, false, "Warmbly", inboxName, "Sent")
		if box != "" || uid != 0 {
			t.Fatalf("got (%q, %d), want an empty answer", box, uid)
		}
		if got := strings.Join(stub.asked, ","); got != "Warmbly,INBOX,Sent" {
			t.Fatalf("searched %q, want the destination, the inbox and Sent in that order", got)
		}
	})

	t.Run("our own sent copy is found in Sent", func(t *testing.T) {
		// The filing leg for a sent copy, on a worker whose folder list does not
		// yet carry the UIDVALIDITY the event was published with.
		stub := &searchStub{found: map[string]uint32{"Sent": 19}}
		box, uid, _ := w.locateWarmupMessage(context.Background(), stub, warmupAction(), nil, true, "Warmbly", inboxName, "Sent")
		if box != "Sent" || uid != 19 {
			t.Fatalf("got (%q, %d), want (Sent, 19)", box, uid)
		}
	})

	t.Run("a folder named twice is searched once", func(t *testing.T) {
		// The inbox placement leaves dst empty and the mail arrived in the inbox.
		stub := &searchStub{}
		inbox := &models.Mailbox{Name: inboxName, Attrs: []string{"\\Inbox"}}
		w.locateWarmupMessage(context.Background(), stub, warmupAction(), inbox, false, "", inboxName, "Sent")
		if got := strings.Join(stub.asked, ","); got != "INBOX,Sent" {
			t.Fatalf("searched %q, want each folder once", got)
		}
	})

	t.Run("an event with no Message-ID cannot be relocated", func(t *testing.T) {
		stub := &searchStub{found: map[string]uint32{"Warmbly": 7}}
		action := warmupAction()
		action.RFCMessageID = ""
		box, uid, _ := w.locateWarmupMessage(context.Background(), stub, action, junk, false, "Warmbly", inboxName, "Sent")
		if box != "Junk" || uid != 41 {
			t.Fatalf("got (%q, %d), want the arrival folder and UID", box, uid)
		}
		if len(stub.asked) != 0 {
			t.Fatalf("nothing to search by: asked %v", stub.asked)
		}
	})
}

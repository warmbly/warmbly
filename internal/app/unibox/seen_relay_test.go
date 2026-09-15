package unibox

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Only the two methods the relay uses are implemented; the embedded interface
// satisfies the rest and panics if the relay ever reaches for one.
type fakeSeenRepo struct {
	repository.UniboxRepository
	targets []models.SeenRelayTarget
	asked   [][]uuid.UUID
}

func (f *fakeSeenRepo) SeenRelayTargets(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]models.SeenRelayTarget, error) {
	f.asked = append(f.asked, ids)
	return f.targets, nil
}

type fakeSeenPublisher struct {
	events.Publisher
	sent   []*models.MessageSeenAction
	toWork []uuid.UUID
}

func (f *fakeSeenPublisher) PublishMessageSeen(_ context.Context, workerID uuid.UUID, action *models.MessageSeenAction) error {
	f.sent = append(f.sent, action)
	f.toWork = append(f.toWork, workerID)
	return nil
}

func TestRelaySeenGroupsByMailbox(t *testing.T) {
	boxA, boxB := uuid.New(), uuid.New()
	workerA, workerB := uuid.New(), uuid.New()
	repo := &fakeSeenRepo{targets: []models.SeenRelayTarget{
		{EmailID: boxA, WorkerID: workerA, Seen: true, Ref: models.MessageSeenRef{ProviderID: "a1"}},
		{EmailID: boxA, WorkerID: workerA, Seen: true, Ref: models.MessageSeenRef{ProviderID: "a2"}},
		{EmailID: boxB, WorkerID: workerB, Seen: true, Ref: models.MessageSeenRef{UID: 7, Folder: "INBOX"}},
	}}
	pub := &fakeSeenPublisher{}
	s := &uniboxService{uniboxRepository: repo, publisher: pub}

	s.publishSeenRelay(context.Background(), uuid.New(), []uuid.UUID{uuid.New(), uuid.New(), uuid.New()})

	if len(pub.sent) != 2 {
		t.Fatalf("expected one event per mailbox, got %d", len(pub.sent))
	}
	// A mailbox is what a worker holds, so each event must go to its own.
	for i, act := range pub.sent {
		switch act.EmailID {
		case boxA:
			if pub.toWork[i] != workerA || len(act.Messages) != 2 {
				t.Errorf("mailbox A: worker=%v messages=%d", pub.toWork[i], len(act.Messages))
			}
		case boxB:
			if pub.toWork[i] != workerB || len(act.Messages) != 1 {
				t.Errorf("mailbox B: worker=%v messages=%d", pub.toWork[i], len(act.Messages))
			}
		default:
			t.Errorf("unexpected mailbox %v", act.EmailID)
		}
		if !act.Seen {
			t.Error("the requested state did not travel")
		}
	}
}

// "Mark all as read" on a busy folder is one press over thousands of
// messages; the bus must not see one enormous event.
func TestRelaySeenChunks(t *testing.T) {
	box, worker := uuid.New(), uuid.New()
	targets := make([]models.SeenRelayTarget, 0, models.SeenRelayChunk+100)
	for i := 0; i < models.SeenRelayChunk+100; i++ {
		targets = append(targets, models.SeenRelayTarget{EmailID: box, WorkerID: worker, Ref: models.MessageSeenRef{ProviderID: "m"}})
	}
	pub := &fakeSeenPublisher{}
	s := &uniboxService{uniboxRepository: &fakeSeenRepo{targets: targets}, publisher: pub}

	s.publishSeenRelay(context.Background(), uuid.New(), []uuid.UUID{uuid.New()})

	if len(pub.sent) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(pub.sent))
	}
	if len(pub.sent[0].Messages) != models.SeenRelayChunk || len(pub.sent[1].Messages) != 100 {
		t.Errorf("chunk sizes %d and %d", len(pub.sent[0].Messages), len(pub.sent[1].Messages))
	}
	if pub.sent[0].Seen || pub.sent[1].Seen {
		t.Error("marking unread must travel as unread")
	}
}

// Nothing changed means nothing to tell the provider, and the repository is
// not even asked.
func TestRelaySeenSkipsWhenNothingChanged(t *testing.T) {
	repo := &fakeSeenRepo{}
	pub := &fakeSeenPublisher{}
	s := &uniboxService{uniboxRepository: repo, publisher: pub}

	s.relaySeen(context.Background(), uuid.New(), nil)

	if len(pub.sent) != 0 || len(repo.asked) != 0 {
		t.Errorf("relayed %d events after %d lookups for no change", len(pub.sent), len(repo.asked))
	}
}

// An install with no worker bus wired still has a working unibox.
func TestRelaySeenWithoutAPublisher(t *testing.T) {
	s := &uniboxService{uniboxRepository: &fakeSeenRepo{}}
	s.relaySeen(context.Background(), uuid.New(), []uuid.UUID{uuid.New()})
}

// A relay batch carries one state, so rows that disagree (a concurrent toggle
// landed between the write and the lookup) have to split rather than be sent
// under whichever state was asked for.
func TestRelaySeenSplitsOnStoredState(t *testing.T) {
	box, worker := uuid.New(), uuid.New()
	repo := &fakeSeenRepo{targets: []models.SeenRelayTarget{
		{EmailID: box, WorkerID: worker, Seen: true, Ref: models.MessageSeenRef{ProviderID: "read"}},
		{EmailID: box, WorkerID: worker, Seen: false, Ref: models.MessageSeenRef{ProviderID: "unread"}},
	}}
	pub := &fakeSeenPublisher{}
	s := &uniboxService{uniboxRepository: repo, publisher: pub}

	s.publishSeenRelay(context.Background(), uuid.New(), []uuid.UUID{uuid.New(), uuid.New()})

	if len(pub.sent) != 2 {
		t.Fatalf("expected one event per state, got %d", len(pub.sent))
	}
	for _, act := range pub.sent {
		if len(act.Messages) != 1 {
			t.Fatalf("states were merged into one batch: %+v", act)
		}
		want := "unread"
		if act.Seen {
			want = "read"
		}
		if act.Messages[0].ProviderID != want {
			t.Errorf("message %q relayed with seen=%v", act.Messages[0].ProviderID, act.Seen)
		}
	}
}

package email

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type graphCheckpointRepo struct {
	links map[string]string
	err   error
}

type graphLoaderState struct {
	repository.EmailSyncStateRepository
	state *models.SyncState
	err   error
}

func (s *graphLoaderState) Get(context.Context, uuid.UUID) (*models.SyncState, error) {
	return s.state, s.err
}

func (r *graphCheckpointRepo) Get(context.Context, uuid.UUID, uuid.UUID) (map[string]string, error) {
	return r.links, r.err
}
func (r *graphCheckpointRepo) Put(context.Context, uuid.UUID, uuid.UUID, string, string) error {
	return nil
}

func TestGraphCheckpointLoadFailureDoesNotPublishCursorlessWorker(t *testing.T) {
	org, worker := uuid.New(), uuid.New()
	acc := &models.Email{ID: uuid.New(), UserID: uuid.NewString(), Email: "box@outlook.test", Provider: string(models.InboxProviderOutlook), Status: "active", OrganizationID: &org, WorkerID: &worker}
	repo := &stubReauthRepo{account: acc}
	lookupErr := errors.New("checkpoint read failed")
	checkpoints := &graphCheckpointRepo{links: map[string]string{"inbox": "saved-delta"}, err: lookupErr}
	pub := &countingPublisher{}
	s := &emailService{emailRepository: repo, workerAssignment: &countingAssignment{}, publisher: pub, graphDelta: checkpoints}
	state := &graphLoaderState{state: &models.SyncState{BackfillStatus: models.SyncBackfillComplete}}
	s.syncState = state
	if err := s.LoadAccountOntoWorker(t.Context(), acc.ID); !errors.Is(err, lookupErr) || pub.added != 0 {
		t.Fatalf("err=%v published=%d", err, pub.added)
	}
	checkpoints.err = nil
	state.err = lookupErr
	if err := s.LoadAccountOntoWorker(t.Context(), acc.ID); !errors.Is(err, lookupErr) || pub.added != 0 {
		t.Fatalf("recovery state failure published unsafe worker: err=%v published=%d", err, pub.added)
	}
	state.err = nil
	payload, err := s.buildAddWorkerEmail(t.Context(), acc)
	if err != nil || payload.Graph.DeltaLinks["inbox"] != "saved-delta" || payload.Sync.State.BackfillStatus != models.SyncBackfillComplete {
		t.Fatalf("reload did not recover saved checkpoint: %v %v", payload, err)
	}
	if err := s.LoadAccountOntoWorker(t.Context(), acc.ID); err != nil || pub.added != 1 {
		t.Fatalf("retry err=%v published=%d", err, pub.added)
	}
	checkpoints.links = nil
	payload, err = s.buildAddWorkerEmail(t.Context(), acc)
	if err != nil || len(payload.Graph.DeltaLinks) != 0 {
		t.Fatalf("genuinely missing checkpoint: %v %v", payload, err)
	}
}

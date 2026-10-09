package jobs

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// seenSyncRepo is the unibox as these handlers see it: one stored message and
// whatever the handler asked to write back.
type seenSyncRepo struct {
	repository.UniboxRepository
	stored  *models.EmailMessageStoreData
	updates []repository.UpdateUniboxEntry
}

func (r *seenSyncRepo) GetByID(context.Context, uuid.UUID, uuid.UUID) (*models.EmailMessageStoreData, error) {
	return r.stored, nil
}

func (r *seenSyncRepo) GetForSync(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*models.EmailMessageStoreData, error) {
	return r.stored, nil
}

func (r *seenSyncRepo) UpdateEntry(_ context.Context, _, _, _ uuid.UUID, e *repository.UpdateUniboxEntry) error {
	r.updates = append(r.updates, *e)
	return nil
}

func (r *seenSyncRepo) seenWritten(t *testing.T) bool {
	t.Helper()
	for i := len(r.updates) - 1; i >= 0; i-- {
		if r.updates[i].Seen != nil {
			return *r.updates[i].Seen
		}
	}
	t.Fatal("no read-state change was written")
	return false
}

func seenSyncService(stored *models.EmailMessageStoreData) (*JobsService, *seenSyncRepo) {
	repo := &seenSyncRepo{stored: stored}
	return &JobsService{UniboxRepository: repo, EmailRepository: warmupInboxEmailRepo{}}, repo
}

// Gmail and Graph report read state as a flag transition. Reading a message in
// the customer's own client has to clear it here too, or the unibox can only
// ever be marked read from inside Warmbly.
func TestFlagsAddMarksTheMessageRead(t *testing.T) {
	s, repo := seenSyncService(&models.EmailMessageStoreData{Flags: []string{}, Seen: false})
	if err := s.HandleFlagsAdd(context.Background(), &models.JobEventFlags{
		UserID: uuid.New(), EmailID: uuid.New(), ID: uuid.New(),
		Flags: []string{models.FlagSeen},
	}); err != nil {
		t.Fatal(err)
	}
	if !repo.seenWritten(t) {
		t.Error("gaining \\Seen at the provider left the message unread in the unibox")
	}
}

func TestFlagsRemoveMarksTheMessageUnread(t *testing.T) {
	s, repo := seenSyncService(&models.EmailMessageStoreData{Flags: []string{models.FlagSeen}, Seen: true})
	if err := s.HandleFlagsRemove(context.Background(), &models.JobEventFlags{
		UserID: uuid.New(), EmailID: uuid.New(), ID: uuid.New(),
		Flags: []string{models.FlagSeen},
	}); err != nil {
		t.Fatal(err)
	}
	if repo.seenWritten(t) {
		t.Error("losing \\Seen at the provider left the message read in the unibox")
	}
}

// The IMAP flag scan relays the message's whole flag set, so read state is
// adopted from it in both directions.
func TestUpdateEmailAdoptsTheProvidersReadState(t *testing.T) {
	for _, tc := range []struct {
		name  string
		was   bool
		flags []string
		want  bool
	}{
		{"read at the provider", false, []string{models.FlagSeen}, true},
		{"back to unread at the provider", true, []string{"\\Flagged"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo := seenSyncService(&models.EmailMessageStoreData{Flags: []string{}, Seen: tc.was})
			if err := s.HandleUpdateEmail(context.Background(), &models.JobEventEmailUpdate{
				UserID: uuid.New(), EmailID: uuid.New(), ID: uuid.New(),
				Flags: tc.flags,
			}); err != nil {
				t.Fatal(err)
			}
			if got := repo.seenWritten(t); got != tc.want {
				t.Errorf("stored seen = %v, want %v", got, tc.want)
			}
		})
	}
}

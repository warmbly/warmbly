package wmail

import (
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// The checkpoint row is keyed (user_id, email_id) with a foreign key to users,
// so an event carrying zero UUIDs is rejected by Postgres and the mailbox never
// gets a checkpoint at all.
func TestNewHistoryIDCarriesRowKey(t *testing.T) {
	userID := uuid.New()
	emailID := uuid.New()

	var got *models.JobEventHistoryIDUpdate
	w := &WMail{
		UserID: userID,
		ID:     emailID,
		onEvent: func(jobType models.JobEventType, body any) error {
			if jobType != models.JobEventTypeHistoryIDUpdate {
				t.Errorf("published %v, want %v", jobType, models.JobEventTypeHistoryIDUpdate)
			}
			got = body.(*models.JobEventHistoryIDUpdate)
			return nil
		},
	}

	if err := w.NewHistoryID(65207); err != nil {
		t.Fatalf("NewHistoryID: %v", err)
	}

	if got == nil {
		t.Fatal("no event was published")
	}
	if got.UserID != userID {
		t.Errorf("UserID = %v, want %v", got.UserID, userID)
	}
	if got.EmailID != emailID {
		t.Errorf("EmailID = %v, want %v", got.EmailID, emailID)
	}
	if got.HistoryID != 65207 {
		t.Errorf("HistoryID = %d, want 65207", got.HistoryID)
	}
	if got.UserID == uuid.Nil || got.EmailID == uuid.Nil {
		t.Error("event carries a nil UUID, which violates the users foreign key")
	}
}

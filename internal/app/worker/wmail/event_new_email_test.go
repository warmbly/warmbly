package wmail

import (
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestOnNewEmailEventWrapsUserID(t *testing.T) {
	userID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	msg := &models.EmailMessageStoreData{ID: uuid.New(), EmailID: uuid.New()}
	var gotType models.JobEventType
	var gotBody any
	mail := &WMail{
		UserID: userID,
		onEvent: func(jobType models.JobEventType, body any) error {
			gotType = jobType
			gotBody = body
			return nil
		},
	}

	if err := mail.onNewEmailEvent(msg); err != nil {
		t.Fatalf("onNewEmailEvent returned error: %v", err)
	}
	if gotType != models.JobEventTypeNewEmail {
		t.Fatalf("event type = %v, want %v", gotType, models.JobEventTypeNewEmail)
	}
	wrapped, ok := gotBody.(*models.JobEventNewEmail)
	if !ok {
		t.Fatalf("body type = %T, want *models.JobEventNewEmail", gotBody)
	}
	if wrapped.UserID != userID {
		t.Fatalf("wrapped user_id = %s, want %s", wrapped.UserID, userID)
	}
	if wrapped.Message != msg {
		t.Fatalf("wrapped message pointer changed")
	}
}

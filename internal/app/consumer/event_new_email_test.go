package jobs

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestNormalizeNewEmailEventAcceptsWrappedPayload(t *testing.T) {
	userID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	msg := &models.EmailMessageStoreData{ID: uuid.New(), EmailID: uuid.New()}
	svc := &JobsService{}

	got, err := svc.normalizeNewEmailEvent(context.Background(), map[string]any{
		"user_id": userID.String(),
		"message": msg,
	})
	if err != nil {
		t.Fatalf("normalizeNewEmailEvent returned error: %v", err)
	}
	if got.UserID != userID {
		t.Fatalf("user_id = %s, want %s", got.UserID, userID)
	}
	if got.Message == nil || got.Message.ID != msg.ID || got.Message.EmailID != msg.EmailID {
		t.Fatalf("message did not round-trip: %#v", got.Message)
	}
}

func TestHandleNewEmailRejectsMissingMessage(t *testing.T) {
	svc := &JobsService{}
	if err := svc.HandleNewEmail(context.Background(), &models.JobEventNewEmail{}); err == nil {
		t.Fatal("HandleNewEmail accepted a nil message")
	}
}

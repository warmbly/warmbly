package wmail

import (
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestWorkerEventKeyIsUniquePerEvent(t *testing.T) {
	emailID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	first := workerEventKey(emailID, models.JobEventTypeGraphDeltaUpdate)
	second := workerEventKey(emailID, models.JobEventTypeNewEmail)
	third := workerEventKey(emailID, models.JobEventTypeGraphDeltaUpdate)

	if first == second {
		t.Fatalf("different event types for the same mailbox reused a NATS de-dupe key: %q", first)
	}
	if first == third {
		t.Fatalf("repeated events for the same mailbox reused a NATS de-dupe key: %q", first)
	}
}

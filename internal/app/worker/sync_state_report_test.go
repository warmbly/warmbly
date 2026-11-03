package worker

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/models"
)

type syncReportBus struct {
	eventbus.EventBus
	payload []byte
}

func (b *syncReportBus) Publish(_ context.Context, _, _ string, payload []byte) error {
	b.payload = payload
	return nil
}

func TestSyncStateReportsPublishingWorker(t *testing.T) {
	wid, other := uuid.New(), uuid.New()
	bus := &syncReportBus{}
	w := &WorkerService{ID: wid.String(), Bus: bus, Codec: codec.NewJSON()}
	report := &models.JobEventSyncState{WorkerID: &other, EmailID: uuid.New()}
	if err := w.Produce(models.JobEventTypeSyncState, report.EmailID.String(), report); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Body models.JobEventSyncState `json:"body"`
	}
	if err := json.Unmarshal(bus.payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.Body.WorkerID == nil || *got.Body.WorkerID != wid || got.Body.EmailID != report.EmailID || *report.WorkerID != other {
		t.Fatalf("publishing identity or source report changed: %+v", got.Body)
	}
	bus.payload = nil
	w.ID = ""
	if err := w.Produce(models.JobEventTypeSyncState, report.EmailID.String(), report); err == nil || bus.payload != nil {
		t.Fatal("report published without a worker identity")
	}
}

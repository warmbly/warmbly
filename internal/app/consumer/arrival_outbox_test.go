package jobs

import (
	"context"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func TestDurableArrivalRequiresRegisteredHandler(t *testing.T) {
	s := &JobsService{}
	if err := s.deliverArrivalEvent(context.Background(), models.JobEventTypeNewEmail, &models.JobEventNewEmail{}); err == nil {
		t.Fatal("unregistered durable event dropped")
	}
	called := false
	s.InitEvents()
	Register(s, models.JobEventTypeNewEmail, func(context.Context, *models.JobEventNewEmail) error { called = true; return nil })
	if err := s.deliverArrivalEvent(context.Background(), models.JobEventTypeNewEmail, &models.JobEventNewEmail{}); err != nil || !called {
		t.Fatalf("called=%v err=%v", called, err)
	}
}

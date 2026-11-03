package tasks

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/advanced"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// casCampaigns closes a campaign once, as the active -> completed CAS does.
type casCampaigns struct {
	repository.CampaignRepository
	mu     sync.Mutex
	status string
}

func (c *casCampaigns) MarkCompleted(context.Context, uuid.UUID) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status != "active" {
		return false, nil
	}
	c.status = "completed"
	return true, nil
}

type completedEvents struct {
	advanced.Service
	mu     sync.Mutex
	events []models.WebhookEventType
}

func (e *completedEvents) EmitCampaignEvent(_ context.Context, _ uuid.UUID, eventType models.WebhookEventType, _ map[string]any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, eventType)
}

// The task path and the reconciler can both find a campaign finished; only
// the one that closed it announces campaign.completed.
func TestSecondCompleterEmitsNothing(t *testing.T) {
	repo := &casCampaigns{status: "active"}
	events := &completedEvents{}
	s := &tasksService{campaignRepo: repo, advanced: events}
	org := uuid.New()
	campaign := &models.Campaign{ID: uuid.New(), OrganizationID: &org, Name: "Q3 outreach"}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.completeCampaign(context.Background(), campaign, "all_sent"); err != nil {
				t.Errorf("complete: %v", err)
			}
		}()
	}
	wg.Wait()

	if len(events.events) != 1 || events.events[0] != models.WebhookEventCampaignCompleted {
		t.Fatalf("want exactly one campaign.completed, got %v", events.events)
	}
	if err := s.completeCampaign(context.Background(), campaign, "end_date"); err != nil {
		t.Fatal(err)
	}
	if len(events.events) != 1 {
		t.Fatalf("closing a completed campaign announced again: %v", events.events)
	}
}

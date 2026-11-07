package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type runRepo struct {
	repository.IntegrationRepository
	automationLookups []string
}

func (r *runRepo) MatchingDispatchTargets(context.Context, uuid.UUID, string) ([]repository.DispatchTarget, error) {
	return nil, nil
}

func (r *runRepo) ListEnabledAutomationsForEvent(_ context.Context, _ uuid.UUID, eventType string) ([]models.Automation, error) {
	r.automationLookups = append(r.automationLookups, eventType)
	return nil, nil
}

func (r *runRepo) CreateAutomationRun(_ context.Context, run *models.AutomationRun) error {
	run.ID = uuid.New()
	return nil
}

func (r *runRepo) FinishAutomationRun(context.Context, uuid.UUID, string, string, []models.AutomationNodeResult) error {
	return nil
}

type recordedEvent struct {
	eventType models.WebhookEventType
	data      map[string]any
}

type eventLog struct{ events []recordedEvent }

func (l *eventLog) Dispatch(_ context.Context, _ uuid.UUID, eventType models.WebhookEventType, data any) (uuid.UUID, error) {
	m, _ := data.(map[string]any)
	l.events = append(l.events, recordedEvent{eventType, m})
	return uuid.New(), nil
}

// A finished run is announced, and the announcement never starts a flow.
func TestAutomationRunIsAnnouncedAndNeverTriggersAutomations(t *testing.T) {
	repo := &runRepo{}
	events := &eventLog{}
	s := &service{repo: repo}
	s.SetEvents(events)

	a := models.Automation{ID: uuid.New(), OrganizationID: uuid.New(), Name: "Reply ping"}
	s.executeAutomationGraph(context.Background(), a, string(models.WebhookEventCampaignReplyReceived), map[string]any{
		"contact_email": "lead@example.com",
	})
	if len(events.events) != 1 || events.events[0].eventType != models.WebhookEventAutomationRun {
		t.Fatalf("want one automation.run, got %+v", events.events)
	}
	got := events.events[0].data
	if got["automation_id"] != a.ID.String() || got["status"] != "success" ||
		got["trigger_event"] != string(models.WebhookEventCampaignReplyReceived) || got["contact_email"] != "lead@example.com" {
		t.Fatalf("unexpected automation.run payload: %v", got)
	}

	s.Dispatch(context.Background(), a.OrganizationID, models.WebhookEventAutomationRun, got)
	if len(repo.automationLookups) != 0 {
		t.Fatalf("automation.run looked up automations to run: %v", repo.automationLookups)
	}
	s.Dispatch(context.Background(), a.OrganizationID, models.WebhookEventCampaignReplyReceived, map[string]any{})
	if len(repo.automationLookups) != 1 {
		t.Fatalf("other events must still run automations, lookups: %v", repo.automationLookups)
	}
}

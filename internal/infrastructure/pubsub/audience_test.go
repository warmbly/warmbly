package pubsub

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type audienceBus struct {
	events []map[string]any
	attrs  []map[string]string
}

func (b *audienceBus) Publish(_ context.Context, _ string, event interface{}, attrs map[string]string) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	b.events = append(b.events, fields)
	b.attrs = append(b.attrs, attrs)
	return nil
}

type audienceStub struct {
	org     uuid.UUID
	allowed bool
	err     error
	orgErr  error
}

func (a *audienceStub) ResourceOrganization(context.Context, string, uuid.UUID) (uuid.UUID, error) {
	return a.org, a.orgErr
}

func (a *audienceStub) CanAddressUser(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return a.allowed, a.err
}

func TestOwnerEventAudience(t *testing.T) {
	org, user, campaign := uuid.New(), uuid.New(), uuid.New()
	for _, state := range []string{"unrestricted", "restricted", "revoked", "error", "missing", "resource error", "wrong org"} {
		t.Run(state, func(t *testing.T) {
			resolver := &audienceStub{org: org, allowed: true}
			var audience AudienceResolver = resolver
			switch state {
			case "restricted", "revoked":
				resolver.allowed = false
			case "error":
				resolver.err = errors.New("unavailable")
			case "missing":
				audience = nil
			case "resource error":
				resolver.orgErr = errors.New("deleted resource")
			case "wrong org":
				resolver.org = uuid.New()
			}
			bus := &audienceBus{}
			p := NewStreamingPublisher(bus, audience)
			p.PublishCampaignEvent(context.Background(), &CampaignEvent{BaseEvent: BaseEvent{UserID: user.String(), EventType: EventCampaignUpdated}, CampaignID: campaign.String(), OrgID: org.String()})
			if state == "wrong org" || state == "error" || state == "missing" || state == "resource error" {
				if len(bus.events) != 0 {
					t.Fatal("unresolved or mismatched resource audience was published")
				}
				return
			}
			if len(bus.events) != 1 || bus.events[0]["org_id"] != org.String() {
				t.Fatalf("expected authorized organization route: %+v", bus.events)
			}
			wantUser := state == "unrestricted"
			if (bus.events[0]["user_id"] == user.String()) != wantUser || (bus.attrs[0]["user_id"] == user.String()) != wantUser {
				t.Fatalf("owner shortcut differs from membership: event=%+v attrs=%+v", bus.events, bus.attrs)
			}
		})
	}
}

func TestResourceOnlyOwnerEvents(t *testing.T) {
	org, user, mailbox, task := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	resolver := &audienceStub{org: org}
	bus := &audienceBus{}
	p := NewStreamingPublisher(bus, resolver)
	p.PublishEmailError(context.Background(), user.String(), mailbox, uuid.Nil, "private", "private")
	p.PublishTaskStatus(context.Background(), user.String(), task, EventTaskCompleted, "private", nil)
	p.PublishWarmupStats(context.Background(), user.String(), mailbox, nil)
	if len(bus.events) != 3 {
		t.Fatalf("resource resolution lost events: %+v", bus.events)
	}
	for _, event := range bus.events {
		if event["org_id"] != org.String() || event["user_id"] != nil {
			t.Fatalf("restricted owner received user route: %+v", event)
		}
	}
	resolver.orgErr = errors.New("unavailable")
	p.PublishEmailError(context.Background(), user.String(), mailbox, uuid.Nil, "private", "private")
	NewStreamingPublisher(bus).PublishEmailError(context.Background(), user.String(), mailbox, uuid.Nil, "private", "private")
	if len(bus.events) != 3 {
		t.Fatal("unresolved events must fail closed")
	}
}

func TestPersonalAndWorkspaceNotificationAudience(t *testing.T) {
	ctx := context.Background()
	user, org := uuid.New(), uuid.New()
	bus := &audienceBus{}
	resolver := &audienceStub{org: org, allowed: true}
	p := NewStreamingPublisher(bus, resolver)
	p.PublishNotificationCreated(ctx, user.String(), uuid.NewString(), "campaign_completed", "private", "", &org)
	if len(bus.events) != 1 || bus.events[0]["org_id"] != nil || bus.events[0]["user_id"] != user.String() {
		t.Fatalf("recipient notice must never broadcast to workspace: %+v", bus.events)
	}
	resolver.allowed = false
	p.PublishNotificationCreated(ctx, user.String(), uuid.NewString(), "campaign_completed", "private", "", &org)
	NewStreamingPublisher(bus).PublishNotificationCreated(ctx, user.String(), uuid.NewString(), "campaign_completed", "private", "", &org)
	NewStreamingPublisher(bus).PublishNotificationCreated(ctx, user.String(), uuid.NewString(), "campaign_completed", "private", "")
	if len(bus.events) != 1 {
		t.Fatal("restricted/unresolved notice published")
	}
	NewStreamingPublisher(bus).PublishNotificationCreated(ctx, user.String(), uuid.NewString(), "security_new_signin", "sign-in", "")
	NewStreamingPublisher(bus).PublishSessionsRevoked(ctx, user)
	if len(bus.events) != 3 || bus.events[1]["user_id"] != user.String() || bus.events[2]["user_id"] != user.String() {
		t.Fatalf("personal events must survive without resolver: %+v", bus.events)
	}
}

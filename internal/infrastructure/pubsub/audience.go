package pubsub

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

type AudienceResolver interface {
	ResourceOrganization(ctx context.Context, kind string, id uuid.UUID) (uuid.UUID, error)
	CanAddressUser(ctx context.Context, orgID, userID uuid.UUID) (bool, error)
}

// publish authorizes owner shortcuts without widening the event's original audience.
func (p *StreamingPublisher) publish(ctx context.Context, topic string, event any, attrs map[string]string) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return nil
	}
	value := func(key string) string {
		var v string
		_ = json.Unmarshal(fields[key], &v)
		return v
	}
	user := value("user_id")
	if user == "" {
		user = attrs["user_id"]
	}
	if user == "" || user == uuid.Nil.String() {
		return p.client.Publish(ctx, topic, event, attrs)
	}
	org := value("org_id")
	if org == "" {
		org = value("organization_id")
	}
	if org == "" {
		org = attrs["org_id"]
	}
	userOnly := org == "" || topic == TopicUserEvents || value("event_type") == string(EventContactsReload)
	if org == "" {
		org = attrs["organization_id"]
	}
	personal := org == "" && (value("event_type") == string(EventSessionsRevoked) ||
		(value("event_type") == string(EventNotificationCreated) && value("category") == "security_new_signin"))
	if personal {
		return p.client.Publish(ctx, topic, event, attrs)
	}
	if p.audience == nil {
		return nil
	}
	orgID, parseErr := uuid.Parse(org)
	resolved := parseErr == nil && orgID != uuid.Nil
	seen := make(map[string]bool)
	for _, resource := range []struct{ key, kind string }{
		{"campaign_id", "campaign"}, {"email_account_id", "mailbox"}, {"account_id", "mailbox"},
		{"email_id", "mailbox"}, {"task_id", "task"}, {"booking_id", "booking"}, {"contact_id", "contact"},
	} {
		id := value(resource.key)
		if id == "" {
			id = attrs[resource.key]
		}
		if id == "" || id == uuid.Nil.String() {
			continue
		}
		resourceID, err := uuid.Parse(id)
		if err != nil {
			return nil
		}
		key := resource.kind + ":" + resourceID.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		resourceOrg, err := p.audience.ResourceOrganization(ctx, resource.kind, resourceID)
		if err != nil || resourceOrg == uuid.Nil {
			return nil
		}
		if resolved && orgID != resourceOrg {
			return nil
		}
		orgID, resolved = resourceOrg, true
	}
	userID, err := uuid.Parse(user)
	allowed := false
	if err == nil && resolved {
		allowed, err = p.audience.CanAddressUser(ctx, orgID, userID)
		if err != nil {
			return nil
		}
	}
	if !resolved {
		return errors.New("user event requires organization or resource context")
	}
	if userOnly || value("event_type") == string(EventNotificationCreated) {
		if !allowed {
			return nil
		}
		delete(fields, "org_id")
		delete(fields, "organization_id")
		fields["user_id"], _ = json.Marshal(userID.String())
		fields["authorization_org_id"], _ = json.Marshal(orgID.String())
		delete(attrs, "org_id")
		delete(attrs, "organization_id")
		return p.client.Publish(ctx, topic, fields, attrs)
	}
	if !allowed {
		delete(fields, "user_id")
		delete(attrs, "user_id")
	}
	fields["org_id"], _ = json.Marshal(orgID.String())
	attrs["org_id"] = orgID.String()
	return p.client.Publish(ctx, topic, fields, attrs)
}

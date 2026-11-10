package models

import (
	"encoding/json"
	"testing"
)

func TestPositiveRepliesNotifyByDefault(t *testing.T) {
	p := DefaultNotificationPreferences()
	if !p.CategoryPref(NotifInboundPositiveReply).Enabled {
		t.Fatal("positive replies must notify by default")
	}
	if p.CategoryPref(NotifInboundReply).Enabled || p.CategoryPref(NotifInboundOOO).Enabled {
		t.Fatal("other inbound categories must stay off by default")
	}
}

// Preferences saved before the category existed carry no key for it.
func TestStoredPreferencesWithoutPositiveReplyKeepTheDefault(t *testing.T) {
	p := DefaultNotificationPreferences()
	stored := `{"inbound_reply":{"enabled":false,"channels":{"in_app":true}},"email_digest_minutes":60}`
	if err := json.Unmarshal([]byte(stored), &p); err != nil {
		t.Fatal(err)
	}
	if !p.InboundPositiveReply.Enabled || !p.InboundPositiveReply.Channels.InApp {
		t.Fatalf("got %+v", p.InboundPositiveReply)
	}
}

func TestDeliveryCategory(t *testing.T) {
	p := DefaultNotificationPreferences()
	if got := p.DeliveryCategory(NotifInboundPositiveReply); got != NotifInboundPositiveReply {
		t.Fatalf("every reply off: got %s", got)
	}
	p.InboundReply.Enabled = true
	if got := p.DeliveryCategory(NotifInboundPositiveReply); got != NotifInboundReply {
		t.Fatalf("every reply on: got %s", got)
	}
	if got := p.DeliveryCategory(NotifInboundOOO); got != NotifInboundOOO {
		t.Fatalf("other categories are untouched: got %s", got)
	}
}

package models

import (
	"testing"
	"time"
)

func TestEventAllowedByScopes(t *testing.T) {
	// An app with only READ_CONTACTS receives contact.* but not campaign.*.
	scopes := APIPermReadContacts
	if !EventAllowedByScopes(WebhookEventContactCreated, scopes) {
		t.Error("contact.created should be allowed with READ_CONTACTS")
	}
	if EventAllowedByScopes(WebhookEventCampaignReplyReceived, scopes) {
		t.Error("campaign.* must NOT be allowed without READ_CAMPAIGNS")
	}
	if EventAllowedByScopes(WebhookEventCRMDealCreated, scopes) {
		t.Error("crm.* must NOT be allowed without READ_CRM")
	}
	// With both scopes, both pass.
	both := APIPermReadContacts | APIPermReadCampaigns
	if !EventAllowedByScopes(WebhookEventCampaignReplyReceived, both) {
		t.Error("campaign.* should be allowed with READ_CAMPAIGNS")
	}
}

func TestAppSubscribedEventTypes(t *testing.T) {
	// Explicit wanted list is filtered by scope: campaign event dropped without
	// READ_CAMPAIGNS, contact event kept.
	wanted := []string{string(WebhookEventContactCreated), string(WebhookEventCampaignReplyReceived)}
	got := AppSubscribedEventTypes(wanted, APIPermReadContacts)
	if len(got) != 1 || got[0] != string(WebhookEventContactCreated) {
		t.Errorf("expected only contact.created, got %v", got)
	}
	// Empty wanted = all non-firehose scope-allowed events; with only READ_CONTACTS
	// it should include contact.* but never campaign.* or any firehose event.
	all := AppSubscribedEventTypes(nil, APIPermReadContacts)
	hasContact, hasCampaign, hasFirehose := false, false, false
	for _, e := range all {
		switch {
		case e == string(WebhookEventContactCreated):
			hasContact = true
		case e == string(WebhookEventCampaignReplyReceived):
			hasCampaign = true
		case IsFirehoseEvent(WebhookEventType(e)):
			hasFirehose = true
		}
	}
	if !hasContact {
		t.Error("empty filter with READ_CONTACTS should include contact.created")
	}
	if hasCampaign {
		t.Error("empty filter with READ_CONTACTS must not include campaign.*")
	}
	if hasFirehose {
		t.Error("empty filter must never include firehose events")
	}
}

func verifiedEndpoint(eventTypes []string) *WebhookEndpoint {
	now := time.Now()
	return &WebhookEndpoint{Enabled: true, VerifiedAt: &now, EventTypes: eventTypes}
}

func TestSubscribes_RequiresEnabledAndVerified(t *testing.T) {
	now := time.Now()
	// Unverified endpoint never receives events, even with an explicit subscription.
	unverified := &WebhookEndpoint{Enabled: true, EventTypes: []string{string(WebhookEventCampaignReplyReceived)}}
	if unverified.Subscribes(WebhookEventCampaignReplyReceived) {
		t.Error("an unverified endpoint must not receive events")
	}
	// Disabled endpoint never receives events.
	disabled := &WebhookEndpoint{Enabled: false, VerifiedAt: &now}
	if disabled.Subscribes(WebhookEventCampaignReplyReceived) {
		t.Error("a disabled endpoint must not receive events")
	}
}

func TestSubscribes_FirehoseOptIn(t *testing.T) {
	all := verifiedEndpoint(nil) // empty filter = all NON-firehose
	if !all.Subscribes(WebhookEventCampaignReplyReceived) {
		t.Error("empty filter should receive normal events")
	}
	if all.Subscribes(WebhookEventCampaignEmailOpened) {
		t.Error("empty filter must NOT receive firehose events")
	}

	optedIn := verifiedEndpoint([]string{string(WebhookEventCampaignEmailOpened)})
	if !optedIn.Subscribes(WebhookEventCampaignEmailOpened) {
		t.Error("explicit subscription should receive a firehose event")
	}
	if optedIn.Subscribes(WebhookEventCampaignReplyReceived) {
		t.Error("an explicit filter should not match unlisted events")
	}
}

func TestWebhookEventForAudit(t *testing.T) {
	cases := []struct {
		entity AuditEntityType
		action AuditAction
		want   WebhookEventType
		ok     bool
	}{
		{AuditEntityCampaign, AuditActionCreate, WebhookEventCampaignCreated, true},
		{AuditEntityCampaign, AuditActionStart, WebhookEventCampaignStarted, true},
		{AuditEntityContact, AuditActionDelete, WebhookEventContactDeleted, true},
		{AuditEntityCRMDeal, AuditActionUpdate, WebhookEventCRMDealUpdated, true},
		{AuditEntitySettings, AuditActionUpdate, WebhookEventSettingsUpdated, true},
		// Operator-internal + self-referential entities must NOT bridge.
		{AuditEntityWorker, AuditActionCreate, "", false},
		{AuditEntityWebhook, AuditActionCreate, "", false},
		{AuditEntityAPIKey, AuditActionRotate, "", false},
		{AuditEntityEmailAccount, AuditActionConnect, "", false}, // has a dedicated emit
	}
	for _, c := range cases {
		got, ok := WebhookEventForAudit(c.entity, c.action)
		if ok != c.ok || got != c.want {
			t.Errorf("WebhookEventForAudit(%s,%s) = (%q,%v), want (%q,%v)", c.entity, c.action, got, ok, c.want, c.ok)
		}
	}
}

func TestWebhookEventCatalog_CoversFirehoseFlag(t *testing.T) {
	if len(WebhookEventCatalog) == 0 {
		t.Fatal("catalog must not be empty")
	}
	byType := map[WebhookEventType]WebhookEventDescriptor{}
	for _, d := range WebhookEventCatalog {
		byType[d.Type] = d
		if !IsValidWebhookEventType(string(d.Type)) {
			t.Errorf("catalog event %q is not a valid event type", d.Type)
		}
	}
	if !byType[WebhookEventCampaignEmailOpened].Firehose {
		t.Error("campaign.email_opened should be flagged firehose in the catalog")
	}
	if byType[WebhookEventCampaignReplyReceived].Firehose {
		t.Error("campaign.reply_received should NOT be firehose")
	}
}

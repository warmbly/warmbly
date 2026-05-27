package models

import "testing"

func TestClassifyProvider(t *testing.T) {
	tests := []struct {
		domain string
		want   WarmupProvider
	}{
		{"gmail.com", ProviderGoogle},
		{"googlemail.com", ProviderGoogle},
		{"outlook.com", ProviderMicrosoft},
		{"hotmail.com", ProviderMicrosoft},
		{"yahoo.com", ProviderYahoo},
		{"icloud.com", ProviderApple},
		{"proton.me", ProviderProton},
		{"zoho.com", ProviderZoho},
		{"someweirddomain.io", ProviderCustom},
		{"", ProviderCustom},
		{"GMAIL.COM", ProviderGoogle},
	}
	for _, tt := range tests {
		if got := ClassifyProvider(tt.domain); got != tt.want {
			t.Errorf("ClassifyProvider(%q) = %v, want %v", tt.domain, got, tt.want)
		}
	}
}

func TestWarmupRoutingRule_Matches_ProviderMatch(t *testing.T) {
	rule := &WarmupRoutingRule{
		Enabled:             true,
		SenderMatchType:     WarmupMatchProvider,
		SenderMatchValue:    "google",
		RecipientMatchType:  WarmupMatchProvider,
		RecipientMatchValue: "google",
		Weight:              2.5,
	}
	if !rule.Matches("alice@gmail.com", "bob@googlemail.com") {
		t.Error("expected match for two Google addresses")
	}
	if rule.Matches("alice@gmail.com", "bob@outlook.com") {
		t.Error("expected no match for Google sender → Microsoft recipient")
	}
}

func TestWarmupRoutingRule_Matches_DomainAny(t *testing.T) {
	rule := &WarmupRoutingRule{
		Enabled:             true,
		SenderMatchType:     WarmupMatchAny,
		RecipientMatchType:  WarmupMatchDomain,
		RecipientMatchValue: "acme.com",
		Weight:              3.0,
	}
	if !rule.Matches("anyone@anything.io", "lead@acme.com") {
		t.Error("expected wildcard sender + domain recipient to match")
	}
	if rule.Matches("anyone@anything.io", "lead@other.com") {
		t.Error("recipient-domain mismatch should not match")
	}
}

func TestWarmupRoutingRule_Matches_Disabled(t *testing.T) {
	rule := &WarmupRoutingRule{
		Enabled:            false,
		SenderMatchType:    WarmupMatchAny,
		RecipientMatchType: WarmupMatchAny,
		Weight:             10,
	}
	if rule.Matches("a@b.com", "c@d.com") {
		t.Error("disabled rules must not match")
	}
}

func TestEmailDomain(t *testing.T) {
	if EmailDomain("Alice@Example.COM") != "example.com" {
		t.Error("domain should be lowercased")
	}
	if EmailDomain("noatsign") != "" {
		t.Error("expected empty domain for malformed address")
	}
}

func TestEmailTLD(t *testing.T) {
	if got := EmailTLD("mail.gmail.com"); got != "com" {
		t.Errorf("expected com, got %q", got)
	}
	if got := EmailTLD("plain"); got != "" {
		t.Errorf("expected empty TLD, got %q", got)
	}
}

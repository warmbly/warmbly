package models

import "testing"

func TestFollowUpSenderValuesAndRotation(t *testing.T) {
	for _, tc := range []struct {
		in             FollowUpSender
		valid, rotates bool
	}{
		{FollowUpSameMailbox, true, false},
		{FollowUpSameDomain, true, true},
		{FollowUpRotate, true, true},
		// A value a newer release wrote keeps the pinned behaviour here.
		{"round_robin", false, false},
		{"", false, false},
	} {
		if tc.in.Valid() != tc.valid || tc.in.Rotates() != tc.rotates {
			t.Errorf("%q: valid=%v rotates=%v, want %v %v", tc.in, tc.in.Valid(), tc.in.Rotates(), tc.valid, tc.rotates)
		}
	}
}

func TestSenderDomainFollowsTheSendAsAddress(t *testing.T) {
	plain := Email{Email: "ana" + "@Sales.Example.test"}
	if got := plain.SenderDomain(); got != "sales.example.test" {
		t.Errorf("domain = %q, want the mailbox's lower-cased", got)
	}
	alias := Email{Email: "ana" + "@example.test", SendAsEmail: "hello" + "@brand.test"}
	if got := alias.SenderDomain(); got != "brand.test" {
		t.Errorf("domain = %q, want the send-as alias's", got)
	}
	if got := (&Email{}).SenderDomain(); got != "" {
		t.Errorf("domain = %q, want none", got)
	}
}

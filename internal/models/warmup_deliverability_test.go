package models

import "testing"

func TestClassifyWarmupLanding(t *testing.T) {
	cases := []struct {
		name   string
		folder string
		flags  []string
		want   string
	}{
		{"plain inbox", FolderInbox, nil, WarmupLandedInbox},
		{"gmail spam label", "", []string{"SPAM"}, WarmupLandedSpam},
		{"graph junk", FolderSpam, []string{"\\Junk"}, WarmupLandedSpam},
		{"imap junk folder without keyword", FolderSpam, []string{"\\Seen"}, WarmupLandedSpam},
		{"gmail promotions", FolderInbox, []string{"CATEGORY_PROMOTIONS"}, WarmupLandedTabs},
		{"gmail updates", "", []string{"CATEGORY_UPDATES"}, WarmupLandedTabs},
		{"gmail primary", "", []string{"CATEGORY_PERSONAL"}, WarmupLandedInbox},
		{"spam outranks a tab", "", []string{"CATEGORY_PROMOTIONS", "SPAM"}, WarmupLandedSpam},
	}
	for _, c := range cases {
		if got := ClassifyWarmupLanding(c.folder, c.flags); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestWarmupRecipientGroup(t *testing.T) {
	cases := []struct{ host, provider, want string }{
		{"google_workspace", "smtp_imap", WarmupRecipientGoogle},
		{"microsoft365", "smtp_imap", WarmupRecipientMicrosoft},
		{"outlook", "outlook", WarmupRecipientMicrosoft},
		{"aol", "smtp_imap", WarmupRecipientYahoo},
		{"", "gmail", WarmupRecipientGoogle},
		{"", "outlook", WarmupRecipientMicrosoft},
		{"", "smtp_imap", WarmupRecipientOther},
		{"zoho", "gmail", WarmupRecipientOther},
	}
	for _, c := range cases {
		if got := WarmupRecipientGroup(c.host, c.provider); got != c.want {
			t.Errorf("(%q, %q): got %q, want %q", c.host, c.provider, got, c.want)
		}
	}
}

func TestNewWarmupPlacementRate(t *testing.T) {
	if r := NewWarmupPlacementRate(0, 0, 0); r.Band != WarmupPlacementBandNone || r.InboxRate != nil {
		t.Fatalf("empty window: %+v", r)
	}
	if r := NewWarmupPlacementRate(12, 0, 0); r.Band != WarmupPlacementBandCollecting || r.InboxRate != nil {
		t.Fatalf("below the floor must withhold the rate: %+v", r)
	}
	r := NewWarmupPlacementRate(15, 3, 2)
	if r.InboxRate == nil || *r.InboxRate != 90 || r.Band != WarmupPlacementBandGood {
		t.Fatalf("tabs count as inbox: %+v", r)
	}
	if r := NewWarmupPlacementRate(17, 0, 3); r.Band != WarmupPlacementBandFair {
		t.Fatalf("85%% is fair: %+v", r)
	}
	if r := NewWarmupPlacementRate(15, 0, 5); r.Band != WarmupPlacementBandPoor {
		t.Fatalf("75%% is poor: %+v", r)
	}
}

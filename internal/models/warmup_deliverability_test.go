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
		{"gmail updates without inbox", "", []string{"CATEGORY_UPDATES"}, WarmupLandedUnknown},
		{"gmail primary without inbox", "", []string{"CATEGORY_PERSONAL"}, WarmupLandedUnknown},
		{"mixed case inbox", " InBoX ", nil, WarmupLandedInbox},
		{"mixed case spam", "SPAM", nil, WarmupLandedSpam},
		{"missing folder", "", nil, WarmupLandedUnknown},
		{"custom folder", "Invoices", nil, WarmupLandedCustom},
		{"archive with category", FolderArchive, []string{"CATEGORY_PROMOTIONS"}, WarmupLandedArchive},
		{"sent copy", FolderSent, nil, WarmupLandedUnknown},
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

// The headline is taken at the major providers only, and the other hosts ride
// beside it however many deliveries they have.
func TestWarmupPlacementWindowRate(t *testing.T) {
	w := WarmupPlacementWindow{
		Major: WarmupPlacementTally{Inbox: 18, Tabs: 2},
		All:   WarmupPlacementTally{Inbox: 28, Tabs: 2, Spam: 10},
	}
	r := w.Rate()
	if r.Scope != WarmupPlacementScopeMajor || r.Delivered != 20 || r.InboxRate == nil || *r.InboxRate != 100 {
		t.Fatalf("major rate = %+v, want 20 delivered at 100%%", r)
	}
	if r.OtherDelivered != 20 || r.OtherInboxRate == nil || *r.OtherInboxRate != 50 {
		t.Fatalf("other hosts = %d at %v, want 20 at 50%%", r.OtherDelivered, r.OtherInboxRate)
	}

	// Small hosts alone never produce a headline, however many there are.
	only := WarmupPlacementWindow{All: WarmupPlacementTally{Inbox: 15, Spam: 5}}.Rate()
	if only.InboxRate != nil || only.Band != WarmupPlacementBandNone || only.Delivered != 0 || only.OtherDelivered != 20 {
		t.Fatalf("small-host-only rate = %+v, want no headline and 20 beside it", only)
	}
	thin := WarmupPlacementWindow{Major: WarmupPlacementTally{Inbox: 3}, All: WarmupPlacementTally{Inbox: 120, Spam: 33}}.Rate()
	if thin.InboxRate != nil || thin.Band != WarmupPlacementBandCollecting || thin.Delivered != 3 || thin.OtherDelivered != 150 {
		t.Fatalf("thin major rate = %+v, want 3 of the major sample collecting", thin)
	}
}

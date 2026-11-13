package models

import "testing"

// Acquisition values come off a query string a stranger controls, so the
// normalizer is the boundary: anything that reaches the database has been
// reduced to a path, a bare host, or a clamped scalar.
func TestOrgAcquisitionNormalize(t *testing.T) {
	tests := []struct {
		name string
		in   OrgAcquisition
		want OrgAcquisition
	}{
		{
			name: "a full landing URL is reduced to its path",
			in:   OrgAcquisition{LandingPath: "https://warmbly.com/pricing?utm_source=x&secret=y"},
			want: OrgAcquisition{LandingPath: "/pricing"},
		},
		{
			name: "a referrer URL is reduced to a bare lowercase host",
			in:   OrgAcquisition{ReferrerHost: "HTTPS://News.YCombinator.com/item?id=1"},
			want: OrgAcquisition{ReferrerHost: "news.ycombinator.com"},
		},
		{
			name: "a bare host with a port keeps only the host",
			in:   OrgAcquisition{ReferrerHost: "example.com:8443/path"},
			want: OrgAcquisition{ReferrerHost: "example.com"},
		},
		{
			name: "a relative landing path is kept as-is",
			in:   OrgAcquisition{LandingPath: "  /guides/warmup  "},
			want: OrgAcquisition{LandingPath: "/guides/warmup"},
		},
		{
			name: "a landing value that is neither a URL nor a path is dropped",
			in:   OrgAcquisition{LandingPath: "pricing"},
			want: OrgAcquisition{},
		},
		{
			name: "control characters are stripped from UTM values",
			in:   OrgAcquisition{UTMSource: "news\nletter\t"},
			want: OrgAcquisition{UTMSource: "newsletter"},
		},
		{
			// url.Parse reads this as a relative path with an empty Hostname,
			// so the reduction runs on the raw string and the result has to be
			// checked rather than trusted.
			name: "a query string on a bare referrer host is not stored",
			in:   OrgAcquisition{ReferrerHost: "example.com?email=alice@example.com"},
			want: OrgAcquisition{ReferrerHost: "example.com"},
		},
		{
			name: "a fragment on a bare referrer host is not stored",
			in:   OrgAcquisition{ReferrerHost: "example.com#alice@example.com"},
			want: OrgAcquisition{ReferrerHost: "example.com"},
		},
		{
			name: "userinfo in a bare referrer host is not stored",
			in:   OrgAcquisition{ReferrerHost: "alice@example.com"},
			want: OrgAcquisition{ReferrerHost: ""},
		},
		{
			name: "a referrer that is not a hostname is dropped",
			in:   OrgAcquisition{ReferrerHost: "not a host"},
			want: OrgAcquisition{},
		},
		{
			// These arrive on a query string anybody can write, and an address
			// here would be stored on the org and sent as an analytics property.
			name: "an email address in a UTM value is redacted",
			in:   OrgAcquisition{UTMSource: "alice@example.com", UTMCampaign: "launch alice@example.com now"},
			want: OrgAcquisition{UTMSource: "[redacted]", UTMCampaign: "launch [redacted] now"},
		},
		{
			name: "an email in a landing path is redacted too",
			in:   OrgAcquisition{LandingPath: "/invite/bob@example.com"},
			want: OrgAcquisition{LandingPath: "/invite/[redacted]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.Normalize(); got != tt.want {
				t.Fatalf("Normalize() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// An oversized value must be clamped rather than rejected: a truncated campaign
// name is still useful, and a failed signup over a long link is not acceptable.
func TestOrgAcquisitionClampsLongValues(t *testing.T) {
	long := make([]byte, acquisitionFieldMax*3)
	for i := range long {
		long[i] = 'a'
	}
	got := OrgAcquisition{UTMCampaign: string(long)}.Normalize()
	if len(got.UTMCampaign) != acquisitionFieldMax {
		t.Fatalf("clamped length = %d, want %d", len(got.UTMCampaign), acquisitionFieldMax)
	}
}

// Empty is what decides whether a row is written at all, so a direct signup
// must report empty and any single field must not.
func TestOrgAcquisitionEmpty(t *testing.T) {
	if !(OrgAcquisition{}).Empty() {
		t.Fatal("a signup that carried nothing should be Empty")
	}
	if (OrgAcquisition{UTMSource: "newsletter"}).Empty() {
		t.Fatal("a signup with a utm_source should not be Empty")
	}
}

// The partner click rides the signup but never lands in the channel row.
func TestOrgAcquisitionPartnerClick(t *testing.T) {
	acq := OrgAcquisition{PartnerClickID: " cl1ck_ID-9 "}.Normalize()
	if acq.PartnerClickID != "cl1ck_ID-9" || acq.Empty() {
		t.Fatalf("a valid click must survive and keep the signup non-empty: %+v", acq)
	}
	if !acq.Channel().Empty() {
		t.Fatal("Channel must drop the click, leaving nothing to store as acquisition")
	}
	for _, bad := range []string{`x"><b>`, "a b", "owner@example.com", string(make([]byte, 65))} {
		if got := (OrgAcquisition{PartnerClickID: bad}).Normalize().PartnerClickID; got != "" {
			t.Fatalf("click %q kept as %q", bad, got)
		}
	}
}

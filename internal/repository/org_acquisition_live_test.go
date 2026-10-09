package repository

import (
	"context"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

// Acquisition round-trips through signup and admin queries against a real schema.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveOrgAcquisition -v

func TestLiveOrgAcquisitionRoundTrips(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := context.Background()
	f := newAdminFixture(t, pool)
	repo := NewOrganizationRepository(pool)

	// A workspace with no row reads as no acquisition, which is the normal
	// case for a direct signup.
	got, err := repo.GetOrganizationAcquisition(ctx, f.org)
	if err != nil {
		t.Fatalf("GetOrganizationAcquisition before any write: %v", err)
	}
	if got != nil {
		t.Fatalf("an org that carried nothing reads %+v, want nil", got)
	}

	acq := &models.OrgAcquisition{
		OrganizationID: f.org,
		LandingPath:    "https://warmbly.com/pricing?utm_source=x",
		ReferrerHost:   "HTTPS://News.YCombinator.com/item?id=1",
		UTMSource:      "newsletter",
		UTMMedium:      "email",
		UTMCampaign:    "launch",
	}
	if err := repo.RecordOrganizationAcquisition(ctx, acq); err != nil {
		t.Fatalf("RecordOrganizationAcquisition: %v", err)
	}

	got, err = repo.GetOrganizationAcquisition(ctx, f.org)
	if err != nil {
		t.Fatalf("GetOrganizationAcquisition: %v", err)
	}
	if got == nil {
		t.Fatal("the recorded acquisition did not read back")
	}
	// Stored normalized: a full URL becomes a path, a referrer becomes a host.
	if got.LandingPath != "/pricing" {
		t.Errorf("landing_path = %q, want %q", got.LandingPath, "/pricing")
	}
	if got.ReferrerHost != "news.ycombinator.com" {
		t.Errorf("referrer_host = %q, want %q", got.ReferrerHost, "news.ycombinator.com")
	}
	if got.UTMSource != "newsletter" || got.UTMMedium != "email" || got.UTMCampaign != "launch" {
		t.Errorf("utm fields = %q/%q/%q", got.UTMSource, got.UTMMedium, got.UTMCampaign)
	}
	// Absent fields stay NULL rather than becoming "".
	if got.UTMTerm != "" || got.UTMContent != "" {
		t.Errorf("absent utm fields read %q/%q, want empty", got.UTMTerm, got.UTMContent)
	}

	// The record is the account's origin, not a mutable setting: a second
	// signup link must not rewrite where the workspace actually came from.
	if err := repo.RecordOrganizationAcquisition(ctx, &models.OrgAcquisition{
		OrganizationID: f.org,
		UTMSource:      "paid-ads",
	}); err != nil {
		t.Fatalf("second RecordOrganizationAcquisition: %v", err)
	}
	got, err = repo.GetOrganizationAcquisition(ctx, f.org)
	if err != nil {
		t.Fatalf("GetOrganizationAcquisition after the second write: %v", err)
	}
	if got.UTMSource != "newsletter" {
		t.Fatalf("utm_source = %q after a second write; the first signup must win", got.UTMSource)
	}

	// A signup that carried nothing writes nothing at all.
	if err := repo.RecordOrganizationAcquisition(ctx, &models.OrgAcquisition{OrganizationID: f.org}); err != nil {
		t.Fatalf("recording an empty acquisition should be a no-op, got: %v", err)
	}
}

// The admin list joins the table and filters on it. A wrong join or a column
// out of order in the projection would break the scan for every row, acquisition
// or not.
func TestLiveOrgAcquisitionSurfacesInTheAdminList(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := context.Background()
	f := newAdminFixture(t, pool)
	repo := NewOrganizationRepository(pool)

	find := func(t *testing.T, search *models.AdminOrgSearch) *models.AdminOrgListItem {
		t.Helper()
		search.Query = f.tag
		search.Limit = 10
		res, err := repo.SearchOrganizationsForAdmin(ctx, search)
		if err != nil {
			t.Fatalf("SearchOrganizationsForAdmin: %v", err)
		}
		for i := range res.Data {
			if res.Data[i].ID == f.org {
				return &res.Data[i]
			}
		}
		return nil
	}

	// Before any acquisition row: the org still lists, with empty channel.
	item := find(t, &models.AdminOrgSearch{})
	if item == nil {
		t.Fatal("the fixture org did not appear in the admin list")
	}
	if item.UTMSource != nil {
		t.Errorf("utm_source = %v for an org with no acquisition row, want nil", *item.UTMSource)
	}
	if item.ReferrerHost != nil {
		t.Errorf("referrer_host = %v for an org with no acquisition row, want nil", *item.ReferrerHost)
	}
	if find(t, &models.AdminOrgSearch{NoAcquisition: true}) == nil {
		t.Error("an org with no acquisition row should match no_acquisition")
	}
	if find(t, &models.AdminOrgSearch{HasAcquisition: true}) != nil {
		t.Error("an org with no acquisition row should not match has_acquisition")
	}

	if err := repo.RecordOrganizationAcquisition(ctx, &models.OrgAcquisition{
		OrganizationID: f.org,
		UTMSource:      "newsletter",
		UTMMedium:      "email",
		ReferrerHost:   "newsletter.example",
		LandingPath:    "/pricing",
	}); err != nil {
		t.Fatalf("RecordOrganizationAcquisition: %v", err)
	}

	item = find(t, &models.AdminOrgSearch{})
	if item == nil {
		t.Fatal("the fixture org disappeared from the admin list after the join had a row")
	}
	if item.UTMSource == nil || *item.UTMSource != "newsletter" {
		t.Errorf("utm_source = %v, want newsletter", item.UTMSource)
	}
	if item.UTMMedium == nil || *item.UTMMedium != "email" {
		t.Errorf("utm_medium = %v, want email", item.UTMMedium)
	}
	if item.LandingPath == nil || *item.LandingPath != "/pricing" {
		t.Errorf("landing_path = %v, want /pricing", item.LandingPath)
	}
	if item.ReferrerHost == nil || *item.ReferrerHost != "newsletter.example" {
		t.Errorf("referrer_host = %v, want newsletter.example", item.ReferrerHost)
	}
	detail, err := repo.GetOrganizationAdminDetail(ctx, f.org)
	if err != nil {
		t.Fatalf("GetOrganizationAdminDetail: %v", err)
	}
	if detail.ReferrerHost == nil || *detail.ReferrerHost != "newsletter.example" {
		t.Errorf("detail referrer_host = %v, want newsletter.example", detail.ReferrerHost)
	}
	if find(t, &models.AdminOrgSearch{ReferrerHost: "newsletter.example"}) == nil {
		t.Error("filtering by the saved referrer did not find the org")
	}
	if find(t, &models.AdminOrgSearch{ReferrerHost: "www.google.com"}) != nil {
		t.Error("filtering by another referrer still found the org")
	}
	if find(t, &models.AdminOrgSearch{ReferrerHost: "newsletter.example' OR TRUE --"}) != nil {
		t.Error("referrer filter should treat SQL syntax as a literal host")
	}
	// The join must not duplicate the row.
	if item.MemberCount != 1 {
		t.Errorf("member_count = %d, want 1: the acquisition join should not fan out rows", item.MemberCount)
	}

	// And the channel filters select on it.
	if find(t, &models.AdminOrgSearch{UTMSource: "newsletter"}) == nil {
		t.Error("filtering by utm_source=newsletter did not find the org")
	}
	if find(t, &models.AdminOrgSearch{UTMSource: "paid-ads"}) != nil {
		t.Error("filtering by a different utm_source still found the org")
	}
	if find(t, &models.AdminOrgSearch{UTMMedium: "email"}) == nil {
		t.Error("filtering by utm_medium=email did not find the org")
	}
	if find(t, &models.AdminOrgSearch{HasAcquisition: true}) == nil {
		t.Error("has_acquisition did not find an org that has a row")
	}
	if find(t, &models.AdminOrgSearch{NoAcquisition: true}) != nil {
		t.Error("no_acquisition found an org that has a row")
	}

	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			`DELETE FROM organization_acquisition WHERE organization_id = $1`, f.org); err != nil {
			t.Errorf("cleanup acquisition: %v", err)
		}
	})
}

func TestLiveOrgAcquisitionReferrerOnly(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := context.Background()
	f := newAdminFixture(t, pool)
	repo := NewOrganizationRepository(pool)
	if err := repo.RecordOrganizationAcquisition(ctx, &models.OrgAcquisition{
		OrganizationID: f.org,
		ReferrerHost:   "www.google.com",
	}); err != nil {
		t.Fatal(err)
	}
	result, err := repo.SearchOrganizationsForAdmin(ctx, &models.AdminOrgSearch{
		Query: f.tag, ReferrerHost: "www.google.com", HasAcquisition: true, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Data) != 1 {
		t.Fatalf("referrer-only filter matched %d rows, want 1", len(result.Data))
	}
	item := result.Data[0]
	if item.ReferrerHost == nil || *item.ReferrerHost != "www.google.com" || item.UTMSource != nil {
		t.Fatalf("referrer-only attribution = %+v", item)
	}
}

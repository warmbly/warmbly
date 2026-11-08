package repository

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/geo"
)

// Run against a migrated scratch database:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveContactLocation -v

// sweepLocations answers every pending contact the way the location sweep does.
func sweepLocations(t *testing.T, repo ContactRepository) []uuid.UUID {
	t.Helper()
	ctx := context.Background()
	pending, err := repo.ListLocationPending(ctx, 100000)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	results := make([]ContactLocationResult, len(pending))
	for i, p := range pending {
		loc := geo.Resolve(geo.Input{Fields: p.CustomFields, Phone: p.Phone, Email: p.Email})
		results[i] = ContactLocationResult{ID: p.ID, Fingerprint: p.Fingerprint, Timezone: loc.Timezone, Country: loc.Country, Source: string(loc.Source)}
	}
	orgs, err := repo.SetContactLocations(ctx, results)
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	return orgs
}

func locationPending(t *testing.T, repo ContactRepository, id uuid.UUID) bool {
	t.Helper()
	pending, err := repo.ListLocationPending(context.Background(), 100000)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	return slices.ContainsFunc(pending, func(p ContactLocationPending) bool { return p.ID == id })
}

func TestLiveContactLocationSweepFeedsSegments(t *testing.T) {
	f, segments := newSegmentFixture(t)
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 369)
	repo := NewContactRepostory(handle)
	ctx := context.Background()

	tag := uuid.New().String()[:6]
	add := func(local, custom, phone string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if _, err := pool.Exec(ctx, `
			INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields)
			VALUES ($1, $2, $3, $4, 'Geo', 'Live', '', $5, $6::jsonb)`,
			id, f.owner, f.org, local+"-"+tag+"@geo.example", phone, custom); err != nil {
			t.Fatalf("insert %s: %v", local, err)
		}
		return id
	}
	texas := add("tx", `{"country":"United States","state":"TX","city":"Austin"}`, "")
	berlin := add("de", `{"Country":"Germany"}`, "")
	add("jp", `{}`, "+81 3-1234-5678")
	// A nested value must not stop the row from being read.
	add("odd", `{"location":"Denver, CO","meta":{"x":1}}`, "")
	// A bare offset places the clock but no country.
	add("offset", `{"timezone":"UTC+3"}`, "")

	if orgs := sweepLocations(t, repo); !slices.Contains(orgs, f.org) {
		t.Fatalf("sweep did not report org %s as changed: %v", f.org, orgs)
	}
	var source string
	if err := pool.QueryRow(ctx, `SELECT location_source FROM contacts WHERE id = $1`, texas).Scan(&source); err != nil || source != "region" {
		t.Fatalf("texas source = %q, %v; want region", source, err)
	}

	// The fixture's own four contacts carry no location.
	for _, tc := range []struct {
		name string
		cond models.SegmentCondition
		want int
	}{
		{"north america", models.SegmentCondition{Field: "world_region", Operator: "in", Values: []string{"north_america"}}, 2},
		{"emea or apac", models.SegmentCondition{Field: "world_region", Operator: "in", Values: []string{"emea", "apac"}}, 2},
		{"country", models.SegmentCondition{Field: "country", Operator: "in", Values: []string{"DE"}}, 1},
		{"timezone", models.SegmentCondition{Field: "timezone", Operator: "in", Values: []string{"America/Chicago", "America/Denver"}}, 2},
		{"location known", models.SegmentCondition{Field: "location_known", Operator: "is_true"}, 5},
		{"no known region", models.SegmentCondition{Field: "world_region", Operator: "in", Values: []string{"none"}}, 5},
		{"every region or none covers everyone", models.SegmentCondition{Field: "world_region", Operator: "in", Values: []string{"north_america", "latin_america", "emea", "apac", "none"}}, 9},
		{"not in a region", models.SegmentCondition{Field: "world_region", Operator: "not_in", Values: []string{"none"}}, 4},
		{"location unknown", models.SegmentCondition{Field: "location_known", Operator: "is_false"}, 4},
	} {
		if got := segCount(t, segments, f.org, models.SegmentMatchAll, tc.cond); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}

	// Nothing changed, so a second sweep reports nothing.
	if orgs := sweepLocations(t, repo); slices.Contains(orgs, f.org) {
		t.Fatalf("an unchanged sweep reported org %s", f.org)
	}

	// An edit makes the contact pending again, and an answer computed for the
	// old inputs must not land over it.
	if locationPending(t, repo, berlin) {
		t.Fatal("berlin pending before any edit")
	}
	if _, err := pool.Exec(ctx, `UPDATE contacts SET custom_fields = '{"Country":"Japan"}' WHERE id = $1`, berlin); err != nil {
		t.Fatal(err)
	}
	stale := ContactLocationResult{ID: berlin, Fingerprint: "stale", Timezone: "Europe/Berlin", Country: "DE", Source: "country"}
	if _, err := repo.SetContactLocations(ctx, []ContactLocationResult{stale}); err != nil {
		t.Fatal(err)
	}
	if !locationPending(t, repo, berlin) {
		t.Fatal("edited contact is not pending")
	}
	sweepLocations(t, repo)
	if got := segCount(t, segments, f.org, models.SegmentMatchAll, models.SegmentCondition{Field: "country", Operator: "in", Values: []string{"JP"}}); got != 2 {
		t.Errorf("JP after edit: got %d, want 2", got)
	}
}

// A recipient-local campaign asks routing for the leads whose day is open
// first; every other campaign's order is exactly what it was.
func TestLiveContactLocationOpenClocksOrderRoutingAndCountLeads(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 369)
	f := newRoutedPairsFixture(t, pool, 4)
	ctx := context.Background()
	// Lead 0 is in Tokyo, 1 in Berlin, 2 in Chicago, 3 has no location.
	for i, custom := range []string{`{"country":"Japan"}`, `{"country":"Germany"}`, `{"state":"Illinois"}`, `{}`} {
		if _, err := pool.Exec(ctx, `UPDATE contacts SET custom_fields = $2::jsonb WHERE id = $1`, f.leads[i], custom); err != nil {
			t.Fatal(err)
		}
	}
	sweepLocations(t, NewContactRepostory(handle))

	repo := NewCampaignProgressRepository(pool)
	order := func(open *OpenClocks) []uuid.UUID {
		t.Helper()
		pairs, _, _, err := repo.FindRoutedPairs(ctx, f.campaign, "created_at", "asc", "", false, false, nil, open, 10)
		if err != nil {
			t.Fatalf("find: %v", err)
		}
		out := make([]uuid.UUID, len(pairs))
		for i, p := range pairs {
			out[i] = p.ContactID
		}
		return out
	}
	if got := order(nil); !slices.Equal(got, f.leads) {
		t.Fatalf("no open clocks reordered routing: %v, want %v", got, f.leads)
	}
	// Berlin's and Chicago's day is open, the campaign's own is not.
	got := order(&OpenClocks{Zones: []string{"Europe/Berlin", "America/Chicago"}})
	want := []uuid.UUID{f.leads[1], f.leads[2], f.leads[0], f.leads[3]}
	if !slices.Equal(got, want) {
		t.Fatalf("open clocks order = %v, want %v", got, want)
	}
	// An unknown lead is on the campaign's clock, so it is open when that is.
	got = order(&OpenClocks{Zones: []string{}, Unknown: true})
	if got[0] != f.leads[3] {
		t.Fatalf("an open campaign clock did not lift the unplaced lead: %v", got)
	}

	tzs, err := NewCampaignRepostory(handle).LeadTimezones(ctx, f.org, f.campaign)
	if err != nil {
		t.Fatal(err)
	}
	if tzs.Total != 4 || tzs.Unknown != 1 || tzs.Resolving != 0 || len(tzs.Zones) != 3 {
		t.Fatalf("lead timezones = %+v, want 4 leads, 1 unknown, 3 zones", tzs)
	}
	// Another workspace reads nothing of this campaign.
	other, err := NewCampaignRepostory(handle).LeadTimezones(ctx, uuid.New(), f.campaign)
	if err != nil || other.Total != 0 {
		t.Fatalf("lead timezones leaked across workspaces: %+v, %v", other, err)
	}
}

// A world region this build does not know matches nobody, in either direction.
func TestLiveContactLocationUnknownRegionMatchesNobody(t *testing.T) {
	f, segments := newSegmentFixture(t)
	for _, op := range []string{"in", "not_in"} {
		cond := models.SegmentCondition{Field: "world_region", Operator: op, Values: []string{"atlantis"}}
		n, xerr := segments.Count(context.Background(), f.org, nil, models.SegmentMatchAll, []models.SegmentCondition{cond})
		if xerr != nil || n != 0 {
			t.Fatalf("%s an unknown region matched %d (%v), want 0", op, n, xerr)
		}
	}
}

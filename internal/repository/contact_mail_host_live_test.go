package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// Run against a migrated scratch database:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveContactMailHost -v

func TestLiveContactMailHostSweepWritesAndSearchReads(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 219)
	f := newSharedOrgFixture(t, pool)
	repo := NewContactRepostory(handle)
	ctx := context.Background()

	add := func(email string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if _, err := pool.Exec(ctx, `
			INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, updated_at, created_at)
			VALUES ($1, $2, $3, $4, 'Live', 'Host', '', '', '{}'::jsonb, NOW(), NOW())`,
			id, f.owner, f.org, email); err != nil {
			t.Fatalf("insert %s: %v", email, err)
		}
		return id
	}
	workspace := add("ana@acme-i695.example")
	flaky := add("bo@flaky-i695.example")
	moved := add("cy@moved-i695.example")

	pending, err := repo.ListMailHostPending(ctx, 100000)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	seen := map[uuid.UUID]bool{}
	for _, p := range pending {
		seen[p.ID] = true
	}
	for _, id := range []uuid.UUID{f.contact, workspace, flaky, moved} {
		if !seen[id] {
			t.Fatalf("contact %s is not pending", id)
		}
	}

	// The address changed after it was read, so its answer must not land.
	if _, err := pool.Exec(ctx, `UPDATE contacts SET email = 'cy@elsewhere-i695.example' WHERE id = $1`, moved); err != nil {
		t.Fatal(err)
	}
	orgs, err := repo.SetContactMailHosts(ctx, []ContactMailHostResult{
		{ID: workspace, Email: "ana@acme-i695.example", MailHost: "google_workspace", ESP: "gmail"},
		{ID: flaky, Email: "bo@flaky-i695.example", Transient: true},
		{ID: moved, Email: "cy@moved-i695.example", MailHost: "yahoo", ESP: "other"},
	})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if len(orgs) != 1 || orgs[0] != f.org {
		t.Fatalf("changed orgs = %v, want [%s]", orgs, f.org)
	}

	type row struct {
		host, esp   string
		resolved    bool
		retryInHour bool
	}
	read := func(id uuid.UUID) row {
		t.Helper()
		var r row
		if err := pool.QueryRow(ctx, `
			SELECT mail_host, esp_provider, esp_resolved_at IS NOT NULL,
			       COALESCE(esp_resolved_at < NOW() - make_interval(days => 7) + make_interval(mins => 61), false)
			FROM contacts WHERE id = $1`, id).Scan(&r.host, &r.esp, &r.resolved, &r.retryInHour); err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := read(workspace); r.host != "google_workspace" || r.esp != "gmail" || !r.resolved {
		t.Fatalf("workspace contact = %+v", r)
	}
	if r := read(flaky); r.host != "" || !r.resolved || !r.retryInHour {
		t.Fatalf("transient contact = %+v, want unresolved host retried within the hour", r)
	}
	if r := read(moved); r.host != "" || r.resolved {
		t.Fatalf("moved contact = %+v, want untouched", r)
	}

	pending, err = repo.ListMailHostPending(ctx, 100000)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pending {
		if p.ID == workspace || p.ID == flaky {
			t.Fatalf("contact %s is pending again straight after its check", p.ID)
		}
	}

	res, xerr := repo.Search(ctx, f.org.String(), nil, nil, models.SearchContacts{MailHosts: []string{"google_workspace"}}, 25)
	if xerr != nil {
		t.Fatalf("filter search: %v", xerr)
	}
	if len(res.Data) != 1 || res.Data[0].ID != workspace || res.Data[0].MailHost != "google_workspace" || res.Data[0].ESPProvider != "gmail" {
		t.Fatalf("filter by provider returned %+v", res.Data)
	}
	res, xerr = repo.Search(ctx, f.org.String(), nil, nil, models.SearchContacts{MailHosts: []string{""}}, 25)
	if xerr != nil {
		t.Fatalf("undetected search: %v", xerr)
	}
	if len(res.Data) != 3 {
		t.Fatalf("not-detected filter returned %d contacts, want 3", len(res.Data))
	}

	if _, err := pool.Exec(ctx, `INSERT INTO campaign_leads (campaign_id, contact_id) SELECT $1, id FROM contacts WHERE organization_id = $2 ON CONFLICT DO NOTHING`, f.campaign, f.org); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	counts, xerr := repo.CampaignLeadCounts(ctx, f.org.String(), f.campaign.String())
	if xerr != nil {
		t.Fatalf("lead counts: %v", xerr)
	}
	// The failed lookup was checked, so it counts with other; two were never reached.
	if p := counts.Providers; p.Google != 1 || p.Microsoft != 0 || p.Other != 1 || p.Undetected != 2 {
		t.Fatalf("lead counts by provider = %+v, want 1 Google, 1 other and 2 undetected", p)
	}

	for _, reverse := range []bool{true, false} {
		res, xerr = repo.Search(ctx, f.org.String(), nil, nil, models.SearchContacts{SortBy: "mail_host", Reverse: reverse}, 25)
		if xerr != nil {
			t.Fatalf("sort (reverse=%v): %v", reverse, xerr)
		}
		if len(res.Data) != 4 {
			t.Fatalf("sort (reverse=%v) returned %d contacts, want 4", reverse, len(res.Data))
		}
		// Undetected contacts sit after every known host ascending, before it descending.
		want := 0
		if !reverse {
			want = 3
		}
		if res.Data[want].ID != workspace {
			t.Fatalf("sort (reverse=%v): detected contact at %d, want %d", reverse, indexOf(res.Data, workspace), want)
		}
	}
}

func indexOf(cs []models.Contact, id uuid.UUID) int {
	for i, c := range cs {
		if c.ID == id {
			return i
		}
	}
	return -1
}

package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// The lead finder's writes merge what a source says into what is known, and
// every read and write stays inside the organization.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveLeadFinder -v
func TestLiveLeadFinder(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 363)
	ctx := context.Background()
	repo := NewLeadFinderRepository(pool)

	user := uuid.New()
	orgA, orgB := uuid.New(), uuid.New()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	exec(`INSERT INTO users (id, email, first_name, last_name, password_hash) VALUES ($1, $2, 'Lead', 'Finder', 'x')`,
		user, "lf-"+user.String()[:8]+"@test.local")
	for _, org := range []uuid.UUID{orgA, orgB} {
		exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Lead Finder', $2, $3)`, org, "lf-"+org.String()[:8], user)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = ANY($1)`, []uuid.UUID{orgA, orgB})
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user)
	})

	rating := 4.6
	reviews := 120
	search := &models.LeadSearch{OrganizationID: orgA, CreatedByUserID: &user, Name: "Dentists", Provider: models.LeadProviderOSM,
		Area: models.LeadArea{Kind: models.LeadAreaBBox, Bounds: models.LeadBounds{South: 47, West: 19, North: 48, East: 20}}}
	if err := repo.CreateSearch(ctx, search); err != nil {
		t.Fatal(err)
	}
	first := []models.Lead{
		{Provider: models.LeadProviderOSM, ExternalID: "node/1", Name: "Bright Smiles", Category: "Dentist", Latitude: 47.5, Longitude: 19.05,
			Phone: "+36 1 555 0100", Website: "https://brightsmiles.hu", Domain: "brightsmiles.hu", Rating: &rating, ReviewCount: &reviews,
			Socials: map[string]string{"facebook": "https://facebook.com/bright"}},
		{Provider: models.LeadProviderOSM, ExternalID: "node/2", Name: "Corner Dental", Category: "Dentist", Latitude: 47.6, Longitude: 19.1},
	}
	ids, err := repo.UpsertLeads(ctx, orgA, &search.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("ids = %v", ids)
	}

	// A later answer without the phone keeps the phone, and adds what is new.
	again := []models.Lead{{Provider: models.LeadProviderOSM, ExternalID: "node/1", Name: "Bright Smiles Dental", Latitude: 47.5, Longitude: 19.05,
		Socials: map[string]string{"instagram": "https://instagram.com/bright"}}}
	if _, err := repo.UpsertLeads(ctx, orgA, nil, again); err != nil {
		t.Fatal(err)
	}
	l, err := repo.GetLead(ctx, orgA, ids[0])
	if err != nil || l == nil {
		t.Fatalf("get: %v %v", l, err)
	}
	if l.Name != "Bright Smiles Dental" || l.Phone != "+36 1 555 0100" || l.Website == "" || l.Rating == nil || len(l.Socials) != 2 {
		t.Fatalf("merge lost or ignored a field: %+v", l)
	}
	if l.Score != 15+15+10+10+10 {
		t.Fatalf("score = %d", l.Score)
	}
	if got, err := repo.GetLead(ctx, orgB, ids[0]); err != nil || got != nil {
		t.Fatalf("another organization read the lead: %v %v", got, err)
	}

	// Enrichment stores the primary address and rescores.
	l.Email = "info@brightsmiles.hu"
	l.Emails = []models.LeadEmail{{Email: "info@brightsmiles.hu", Source: "website", DomainMatch: true, Status: "valid"}}
	l.EnrichmentStatus = models.LeadEnrichmentDone
	if err := repo.SaveEnrichment(ctx, l); err != nil {
		t.Fatal(err)
	}
	if l, _ = repo.GetLead(ctx, orgA, ids[0]); l.Score != 100 || l.EnrichedAt == nil {
		t.Fatalf("after enrichment: score %d enriched %v", l.Score, l.EnrichedAt)
	}

	yes := true
	minRating := 4.0
	got, total, err := repo.ListLeads(ctx, orgA, &models.LeadFilter{SearchID: &search.ID, HasEmail: &yes, MinRating: &minRating}, models.LeadSortScore, 10, 0)
	if err != nil || total != 1 || len(got) != 1 || got[0].ID != ids[0] {
		t.Fatalf("filtered list = %d %v %v", total, got, err)
	}
	if _, total, _ = repo.ListLeads(ctx, orgA, &models.LeadFilter{Q: "corner"}, models.LeadSortName, 10, 0); total != 1 {
		t.Fatalf("text filter total = %d", total)
	}
	if _, total, err = repo.ListLeads(ctx, orgB, nil, models.LeadSortRecent, 10, 0); err != nil || total != 0 {
		t.Fatalf("another organization lists %d leads: %v", total, err)
	}

	// Tags converge, and a patch from another organization changes nothing.
	saved := models.LeadStatusSaved
	if n, err := repo.PatchLeads(ctx, orgA, ids, &models.LeadPatch{Status: &saved, AddTags: []string{"hot", "hot", "q4"}}); err != nil || n != 2 {
		t.Fatalf("patch = %d %v", n, err)
	}
	if _, err := repo.PatchLeads(ctx, orgA, ids, &models.LeadPatch{AddTags: []string{"q4"}, RemoveTags: []string{"hot"}}); err != nil {
		t.Fatal(err)
	}
	if n, err := repo.PatchLeads(ctx, orgB, ids, &models.LeadPatch{Status: &saved}); err != nil || n != 0 {
		t.Fatalf("another organization patched the leads: %d %v", n, err)
	}
	if l, _ = repo.GetLead(ctx, orgA, ids[1]); len(l.Tags) != 1 || l.Tags[0] != "q4" || l.Status != models.LeadStatusSaved {
		t.Fatalf("tags %v status %s", l.Tags, l.Status)
	}

	// A selection never reaches past the organization.
	sel, err := repo.ResolveSelection(ctx, orgB, &models.LeadSelection{LeadIDs: ids}, 100)
	if err != nil || len(sel) != 0 {
		t.Fatalf("other org resolved %v %v", sel, err)
	}
	if sel, _ = repo.ResolveSelection(ctx, orgA, &models.LeadSelection{All: true, Filter: &models.LeadFilter{Tag: "q4"}}, 100); len(sel) != 2 {
		t.Fatalf("all matching = %v", sel)
	}

	// A contact from another organization is never linked.
	contactB := uuid.New()
	exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, updated_at, created_at)
		VALUES ($1, $2, $3, 'x@other.test', '', '', '', '', '{}', NOW(), NOW())`, contactB, user, orgB)
	if err := repo.LinkContacts(ctx, orgA, map[uuid.UUID]uuid.UUID{ids[0]: contactB}); err != nil {
		t.Fatal(err)
	}
	if l, _ = repo.GetLead(ctx, orgA, ids[0]); l.ContactID != nil {
		t.Fatal("linked a contact from another organization")
	}

	// People are matched by name and stay inside the organization; a website
	// read never replaces a typed-in address, and a published one beats a guess.
	at, dom := "@", "brightsmiles.hu"
	people := []models.LeadPerson{
		{Name: "Jane Doe", NameKey: "jane doe", FirstName: "Jane", LastName: "Doe", Title: "Owner", Seniority: models.LeadSeniorityOwner,
			Email: "jane" + at + dom, EmailSource: models.LeadEmailManual, Source: models.LeadPersonManual},
		{Name: "Ravi Patel", NameKey: "ravi patel", FirstName: "Ravi", LastName: "Patel", Seniority: models.LeadSeniorityStaff,
			Email: "ravi" + at + dom, EmailSource: models.LeadEmailPattern, EmailStatus: models.LeadPersonStatusGuessed, EmailConfidence: 25,
			Source: models.LeadPersonPage},
	}
	stored, err := repo.SavePeople(ctx, orgA, ids[0], people)
	if err != nil || len(stored) != 2 {
		t.Fatalf("save people = %v %v", stored, err)
	}
	if _, err := repo.SavePeople(ctx, orgB, ids[0], people); !errors.Is(err, ErrLeadNotFound) {
		t.Fatalf("another organization saved people onto the lead: %v", err)
	}
	reread := []models.LeadPerson{
		{Name: "Jane Doe", NameKey: "jane doe", FirstName: "Jane", LastName: "Doe", Title: "Founder", Seniority: models.LeadSeniorityOwner,
			Email: "j.doe" + at + dom, EmailSource: models.LeadEmailWebsite, EmailConfidence: 80, Source: models.LeadPersonPage},
		{Name: "Ravi Patel", NameKey: "ravi patel", FirstName: "Ravi", LastName: "Patel", Title: "Associate", Seniority: models.LeadSeniorityStaff,
			Email: "ravi.patel" + at + dom, EmailSource: models.LeadEmailWebsite, EmailConfidence: 80, Source: models.LeadPersonPage},
	}
	if _, err := repo.SavePeople(ctx, orgA, ids[0], reread); err != nil {
		t.Fatal(err)
	}
	byLead, err := repo.PeopleForLeads(ctx, orgA, ids)
	if err != nil || len(byLead[ids[0]]) != 2 {
		t.Fatalf("people = %v %v", byLead, err)
	}
	jane, ravi := byLead[ids[0]][0], byLead[ids[0]][1]
	if jane.Name != "Jane Doe" || jane.Email != "jane"+at+dom || jane.Title != "Owner" {
		t.Fatalf("a typed-in person was overwritten: %+v", jane)
	}
	if ravi.Email != "ravi.patel"+at+dom || ravi.EmailSource != models.LeadEmailWebsite || ravi.Title != "Associate" {
		t.Fatalf("a guess did not give way to a published address: %+v", ravi)
	}
	if other, err := repo.PeopleForLeads(ctx, orgB, ids); err != nil || len(other) != 0 {
		t.Fatalf("another organization read the people: %v %v", other, err)
	}
	if l, _ = repo.GetLead(ctx, orgA, ids[0]); l.PeopleCount != 2 {
		t.Fatalf("people_count = %d", l.PeopleCount)
	}
	if _, total, _ = repo.ListLeads(ctx, orgA, &models.LeadFilter{HasPeople: &yes}, models.LeadSortRecent, 10, 0); total != 1 {
		t.Fatalf("has_people total = %d", total)
	}
	ravi.Name, ravi.NameKey, ravi.FirstName, ravi.LastName = "Jane Doe", "jane doe", "Jane", "Doe"
	if _, err := repo.SavePeople(ctx, orgA, ids[0], []models.LeadPerson{ravi}); !errors.Is(err, ErrLeadPersonExists) {
		t.Fatalf("renaming onto someone already there = %v", err)
	}
	if err := repo.LinkPersonContacts(ctx, orgA, map[uuid.UUID]uuid.UUID{jane.ID: contactB}); err != nil {
		t.Fatal(err)
	}
	if p, _ := repo.GetPerson(ctx, orgA, ids[0], jane.ID); p == nil || p.ContactID != nil {
		t.Fatal("linked a person to a contact from another organization")
	}
	if ok, err := repo.DeletePerson(ctx, orgB, ids[0], jane.ID); err != nil || ok {
		t.Fatalf("another organization removed a person: %v %v", ok, err)
	}
	if ok, err := repo.DeletePerson(ctx, orgA, ids[0], jane.ID); !ok || err != nil {
		t.Fatalf("delete person = %v %v", ok, err)
	}

	if err := repo.RecordSearchPage(ctx, search); err != nil || search.ResultCount != 2 {
		t.Fatalf("search count = %d %v", search.ResultCount, err)
	}
	// A run keeps what it cost in micro-dollars through progress, finish and a read.
	run := &models.LeadEnrichmentRun{OrganizationID: orgA, RequestedByUserID: &user, LeadIDs: ids, Verify: true, Total: len(ids)}
	if err := repo.CreateEnrichmentRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if claimed, err := repo.ClaimEnrichmentRun(ctx, run.ID); err != nil || claimed == nil {
		t.Fatalf("claim = %v %v", claimed, err)
	}
	run.Status, run.Processed, run.ChargedMicro = "running", 1, 20_000
	if err := repo.UpdateEnrichmentProgress(ctx, run); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetEnrichmentRun(ctx, orgA, run.ID); err != nil || got == nil || got.ChargedMicro != 20_000 || got.ChargedUSD != 0.02 {
		t.Fatalf("progress charged = %+v %v", got, err)
	}
	run.Status, run.Processed, run.ChargedMicro = "done", 2, 40_000
	if err := repo.FinishEnrichmentRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetEnrichmentRun(ctx, orgA, run.ID); err != nil || got == nil || got.ChargedMicro != 40_000 || got.ChargedUSD != 0.04 {
		t.Fatalf("finished charged = %+v %v", got, err)
	}
	if got, err := repo.GetEnrichmentRun(ctx, orgB, run.ID); err != nil || got != nil {
		t.Fatalf("another organization read the run: %v %v", got, err)
	}

	if n, err := repo.DeleteLeads(ctx, orgB, ids); err != nil || n != 0 {
		t.Fatalf("another organization deleted the leads: %d %v", n, err)
	}
}

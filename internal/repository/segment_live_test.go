package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// Issue #266: segments compile to SQL over the real schema. These prove each
// field family, the manual overrides and nested segments against Postgres.
// The shared fixture already holds one contact (Pied Piper), so org-wide
// counts are one higher than the three contacts seeded here.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveSegment -v

type segmentFixture struct {
	*sharedOrgFixture
	category uuid.UUID
	alice    uuid.UUID // Acme, VP, in campaign, opened, categorised
	bob      uuid.UUID // Globex, custom title Engineer, unsubscribed
	carol    uuid.UUID // Acme, no activity
}

func newSegmentFixture(t *testing.T) (*segmentFixture, SegmentRepository) {
	t.Helper()
	handle, pool := liveContactDB(t)
	base := newSharedOrgFixture(t, pool)
	f := &segmentFixture{sharedOrgFixture: base, category: uuid.New(), alice: uuid.New(), bob: uuid.New(), carol: uuid.New()}
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	tag := uuid.New().String()[:6]
	contact := func(id uuid.UUID, first, company string, custom string, subscribed bool) {
		exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, subscribed)
		      VALUES ($1, $2, $3, $4, $5, 'Seg', $6, '', $7::jsonb, $8)`,
			id, f.owner, f.org, first+"-"+tag+"@"+company+".test", first, company, custom, subscribed)
	}
	contact(f.alice, "alice", "acme", `{"title":"VP Sales"}`, true)
	contact(f.bob, "bob", "globex", `{"title":"Engineer"}`, false)
	contact(f.carol, "carol", "acme", `{}`, true)

	exec(`INSERT INTO categories (id, organization_id, user_id, title, color, position) VALUES ($1, $2, $3, 'Hot', '#ff0000', 0)`, f.category, f.org, f.owner)
	exec(`INSERT INTO contact_categories (contact_id, category_id) VALUES ($1, $2)`, f.alice, f.category)
	exec(`INSERT INTO campaign_leads (campaign_id, contact_id) VALUES ($1, $2)`, f.campaign, f.alice)
	seq := uuid.New()
	exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_plain, body_html) VALUES ($1, $2, $3, 'Email 1', 'Hi', 'Body', 'Body')`, seq, f.campaign, f.org)
	exec(`INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at, opened_at, opened_machine)
	      VALUES ($1, $2, $3, NOW() - interval '2 days', NOW() - interval '1 day', false)`, f.campaign, f.alice, seq)

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM segments WHERE organization_id = $1`, f.org},
			{`DELETE FROM campaign_contact_progress WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM sequences WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM contact_categories WHERE category_id = $1`, f.category},
			{`DELETE FROM categories WHERE id = $1`, f.category},
		} {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})
	return f, NewSegmentRepository(handle)
}

func segCount(t *testing.T, repo SegmentRepository, org uuid.UUID, match models.SegmentMatch, conds ...models.SegmentCondition) int {
	t.Helper()
	if err := models.ValidateSegmentConditions(conds, nil); err != nil {
		t.Fatalf("validate: %v", err)
	}
	n, xerr := repo.Count(context.Background(), org, nil, match, conds)
	if xerr != nil {
		t.Fatalf("count: %v", xerr)
	}
	return n
}

func TestLiveSegmentConditionsMatchEachFamily(t *testing.T) {
	f, repo := newSegmentFixture(t)
	cases := []struct {
		name string
		want int
		cond models.SegmentCondition
	}{
		{"company equals", 2, models.SegmentCondition{Field: "company", Operator: "equals", Value: "ACME"}},
		{"company contains escapes wildcards", 0, models.SegmentCondition{Field: "company", Operator: "contains", Value: "%"}},
		{"email domain", 1, models.SegmentCondition{Field: "email_domain", Operator: "equals", Value: "globex.test"}},
		{"custom field", 1, models.SegmentCondition{Field: "custom.title", Operator: "starts_with", Value: "vp"}},
		{"custom field empty", 2, models.SegmentCondition{Field: "custom.title", Operator: "is_empty"}},
		{"subscribed false", 1, models.SegmentCondition{Field: "subscribed", Operator: "is_false"}},
		{"category in", 1, models.SegmentCondition{Field: "category", Operator: "in", Values: []string{f.category.String()}}},
		{"category none", 3, models.SegmentCondition{Field: "category", Operator: "is_empty"}},
		{"campaign in", 1, models.SegmentCondition{Field: "campaign", Operator: "in", Values: []string{f.campaign.String()}}},
		{"campaign not in", 3, models.SegmentCondition{Field: "campaign", Operator: "not_in", Values: []string{f.campaign.String()}}},
		{"campaign count", 3, models.SegmentCondition{Field: "campaign_count", Operator: "equals", Value: "0"}},
		{"emails opened", 1, models.SegmentCondition{Field: "emails_opened", Operator: "gte", Value: "1"}},
		{"last opened within", 1, models.SegmentCondition{Field: "last_opened_at", Operator: "within_days", Value: "7"}},
		{"last opened not within", 3, models.SegmentCondition{Field: "last_opened_at", Operator: "not_within_days", Value: "7"}},
		{"last replied empty", 4, models.SegmentCondition{Field: "last_replied_at", Operator: "is_empty"}},
		{"created after yesterday", 4, models.SegmentCondition{Field: "created_at", Operator: "after", Value: "2000-01-01"}},
		{"source enum", 4, models.SegmentCondition{Field: "source", Operator: "in", Values: []string{"unknown"}}},
		{"suppressed", 0, models.SegmentCondition{Field: "suppressed", Operator: "is_true"}},
	}
	for _, tc := range cases {
		if got := segCount(t, repo, f.org, models.SegmentMatchAll, tc.cond); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestLiveSegmentMatchAnyAndAll(t *testing.T) {
	f, repo := newSegmentFixture(t)
	acme := models.SegmentCondition{Field: "company", Operator: "equals", Value: "acme"}
	opened := models.SegmentCondition{Field: "emails_opened", Operator: "gte", Value: "1"}
	if got := segCount(t, repo, f.org, models.SegmentMatchAll, acme, opened); got != 1 {
		t.Errorf("all: got %d, want 1", got)
	}
	unsub := models.SegmentCondition{Field: "subscribed", Operator: "is_false"}
	if got := segCount(t, repo, f.org, models.SegmentMatchAny, opened, unsub); got != 2 {
		t.Errorf("any: got %d, want 2", got)
	}
}

func TestLiveSegmentOverridesNestingAndCampaign(t *testing.T) {
	f, repo := newSegmentFixture(t)
	ctx := context.Background()
	acme, xerr := repo.Create(ctx, f.org, &f.owner, &models.Segment{
		Name: "Acme", Color: "#0284c7", Match: models.SegmentMatchAll,
		Conditions: []models.SegmentCondition{{Field: "company", Operator: "equals", Value: "acme"}},
	})
	if xerr != nil {
		t.Fatalf("create: %v", xerr)
	}
	if acme.ContactCount != 2 {
		t.Fatalf("acme count = %d, want 2", acme.ContactCount)
	}

	// Exclude carol, include bob: membership is (conditions OR include) AND NOT exclude.
	if _, xerr := repo.SetMembers(ctx, f.org, acme.ID, []uuid.UUID{f.carol}, models.SegmentMemberExclude); xerr != nil {
		t.Fatalf("exclude: %v", xerr)
	}
	if _, xerr := repo.SetMembers(ctx, f.org, acme.ID, []uuid.UUID{f.bob}, models.SegmentMemberInclude); xerr != nil {
		t.Fatalf("include: %v", xerr)
	}
	got, xerr := repo.Get(ctx, f.org, acme.ID)
	if xerr != nil {
		t.Fatalf("get: %v", xerr)
	}
	if got.ContactCount != 2 || got.IncludedCount != 1 || got.ExcludedCount != 1 {
		t.Fatalf("after overrides: count=%d included=%d excluded=%d", got.ContactCount, got.IncludedCount, got.ExcludedCount)
	}
	modes, xerr := repo.MemberModes(ctx, acme.ID, []uuid.UUID{f.alice, f.bob, f.carol})
	if xerr != nil {
		t.Fatalf("modes: %v", xerr)
	}
	if modes[f.bob] != models.SegmentMemberInclude || modes[f.carol] != models.SegmentMemberExclude || modes[f.alice] != "" {
		t.Fatalf("modes = %v", modes)
	}

	// The contact-side view reports membership and the override per segment,
	// and the overrides listing shows both pinned contacts.
	forBob, xerr := repo.SegmentsForContact(ctx, f.org, f.bob)
	if xerr != nil {
		t.Fatalf("segments for contact: %v", xerr)
	}
	if len(forBob) != 1 || !forBob[0].Member || forBob[0].Mode != models.SegmentMemberInclude {
		t.Fatalf("segments for bob = %+v", forBob)
	}
	forCarol, _ := repo.SegmentsForContact(ctx, f.org, f.carol)
	if len(forCarol) != 1 || forCarol[0].Member || forCarol[0].Mode != models.SegmentMemberExclude {
		t.Fatalf("segments for carol = %+v", forCarol)
	}
	overrides, xerr := repo.ListOverrides(ctx, f.org, acme.ID)
	if xerr != nil || len(overrides) != 2 {
		t.Fatalf("overrides = %+v, %v", overrides, xerr)
	}

	// A segment built on another one sees its overrides.
	nested, xerr := repo.Create(ctx, f.org, &f.owner, &models.Segment{
		Name: "Acme not opened", Color: "#0284c7", Match: models.SegmentMatchAll,
		Conditions: []models.SegmentCondition{
			{Field: "segment", Operator: "in", Values: []string{acme.ID.String()}},
			{Field: "emails_opened", Operator: "equals", Value: "0"},
		},
	})
	if xerr != nil {
		t.Fatalf("create nested: %v", xerr)
	}
	if nested.ContactCount != 1 { // bob (included), since carol is excluded and alice opened
		t.Fatalf("nested count = %d, want 1", nested.ContactCount)
	}
	names, xerr := repo.ReferencedBy(ctx, f.org, acme.ID)
	if xerr != nil || len(names) != 1 || names[0] != "Acme not opened" {
		t.Fatalf("referenced by = %v, %v", names, xerr)
	}

	// The contacts search honours segment_ids.
	handle, _ := liveContactDB(t)
	contacts := NewContactRepostory(handle)
	res, xerr := contacts.Search(ctx, f.org.String(), nil, nil, models.SearchContacts{SegmentIDs: []string{acme.ID.String()}}, 50)
	if xerr != nil {
		t.Fatalf("search: %v", xerr)
	}
	if len(res.Data) != 2 {
		t.Fatalf("search by segment = %d contacts, want 2", len(res.Data))
	}

	// Enrolling the segment adds only the members not already leads.
	out, xerr := repo.AddToCampaign(ctx, f.org, f.mate.String(), acme.ID, f.other)
	if xerr != nil {
		t.Fatalf("add to campaign: %v", xerr)
	}
	if out.Added != 2 || out.Members != 2 {
		t.Fatalf("add to campaign = %+v", out)
	}
	out, xerr = repo.AddToCampaign(ctx, f.org, f.mate.String(), acme.ID, f.other)
	if xerr != nil || out.Added != 0 {
		t.Fatalf("second add = %+v, %v", out, xerr)
	}

	// Duplicate names collide, and a referenced segment cannot be deleted
	// (the service refuses; the repository reports the reference).
	if _, xerr := repo.Create(ctx, f.org, nil, &models.Segment{Name: "ACME", Color: "#0284c7", Match: models.SegmentMatchAll}); xerr == nil {
		t.Fatalf("duplicate name accepted")
	}
	if xerr := repo.Delete(ctx, f.org, nested.ID); xerr != nil {
		t.Fatalf("delete nested: %v", xerr)
	}
	if got, _ := repo.Get(ctx, f.org, acme.ID); got == nil || got.ContactCount != 2 {
		t.Fatalf("acme after nested delete: %+v", got)
	}
}

// Issue #277: segments linked to a campaign are a live audience source. These
// prove the link CRUD, the incremental enrolment sync, the sweep scan and the
// delete guard against the real schema.
func TestLiveSegmentCampaignLinks(t *testing.T) {
	f, repo := newSegmentFixture(t)
	ctx := context.Background()

	acme, xerr := repo.Create(ctx, f.org, &f.owner, &models.Segment{
		Name: "Acme live", Color: "#0284c7", Match: models.SegmentMatchAll,
		Conditions: []models.SegmentCondition{{Field: "company", Operator: "equals", Value: "acme"}},
	})
	if xerr != nil {
		t.Fatalf("create: %v", xerr)
	}

	// Linking replaces the set; a segment the org does not own is refused.
	if xerr := repo.SetForCampaign(ctx, f.org, f.other, []uuid.UUID{acme.ID}); xerr != nil {
		t.Fatalf("set: %v", xerr)
	}
	if xerr := repo.SetForCampaign(ctx, f.org, f.other, []uuid.UUID{uuid.New()}); xerr == nil {
		t.Fatalf("unknown segment accepted")
	}
	links, xerr := repo.ListForCampaign(ctx, f.org, f.other)
	if xerr != nil || len(links) != 1 || links[0].SegmentID != acme.ID || links[0].ContactCount != 2 || links[0].LeadCount != 0 {
		t.Fatalf("links = %+v, %v", links, xerr)
	}

	// Replacing the set enrols current members in the same transaction; a
	// second pass adds nothing.
	added, status, xerr := repo.ReplaceForCampaign(ctx, f.org, f.other, []uuid.UUID{acme.ID})
	if xerr != nil || added != 2 || status == "" {
		t.Fatalf("replace = %d, %q, %v", added, status, xerr)
	}
	if links, _ = repo.ListForCampaign(ctx, f.org, f.other); len(links) != 1 || links[0].LeadCount != 2 || links[0].HeldOutCount != 0 {
		t.Fatalf("links after replace = %+v", links)
	}
	if added, _ = repo.SyncCampaignSegments(ctx, f.org, f.other); added != 0 {
		t.Fatalf("second sync = %d, want 0", added)
	}

	// A contact pinned into the segment is picked up by the next sync.
	if _, xerr := repo.SetMembers(ctx, f.org, acme.ID, []uuid.UUID{f.bob}, models.SegmentMemberInclude); xerr != nil {
		t.Fatalf("include: %v", xerr)
	}
	if added, _ = repo.SyncCampaignSegments(ctx, f.org, f.other); added != 1 {
		t.Fatalf("post-include sync = %d, want 1", added)
	}

	// A hand-removed lead stays removed: the sync skips the pair until a
	// manual add clears the record, and the explicit one-shot enrol overrides
	// and clears it too.
	handle, _ := liveContactDB(t)
	contacts := NewContactRepostory(handle)
	removeBob := &models.BulkEditContactsData{ContactSelection: models.ContactSelection{Contacts: []string{f.bob.String()}}, RemoveCampaigns: []string{f.other.String()}}
	if _, xerr := contacts.BulkUpdate(ctx, f.owner.String(), f.org, removeBob); xerr != nil {
		t.Fatalf("remove: %v", xerr)
	}
	if added, _ = repo.SyncCampaignSegments(ctx, f.org, f.other); added != 0 {
		t.Fatalf("sync after manual removal = %d, want 0", added)
	}
	// The link reports the split the Leads tab explains: three members, two
	// of them leads, one held out by the removal.
	links, xerr = repo.ListForCampaign(ctx, f.org, f.other)
	if xerr != nil || len(links) != 1 || links[0].ContactCount != 3 || links[0].LeadCount != 2 || links[0].HeldOutCount != 1 {
		t.Fatalf("links after removal = %+v, %v", links, xerr)
	}
	if _, xerr := contacts.BulkUpdate(ctx, f.owner.String(), f.org, &models.BulkEditContactsData{
		ContactSelection: models.ContactSelection{Contacts: []string{f.bob.String()}}, AddCampaigns: []string{f.other.String()},
	}); xerr != nil {
		t.Fatalf("re-add: %v", xerr)
	}
	var removals int
	if err := handle.QueryRow(ctx, `SELECT COUNT(*) FROM campaign_lead_removals WHERE campaign_id = $1`, f.other).Scan(&removals); err != nil || removals != 0 {
		t.Fatalf("removals after manual re-add = %d, %v", removals, err)
	}
	if links, _ = repo.ListForCampaign(ctx, f.org, f.other); len(links) != 1 || links[0].LeadCount != 3 || links[0].HeldOutCount != 0 {
		t.Fatalf("links after manual re-add = %+v", links)
	}
	if _, xerr := contacts.BulkUpdate(ctx, f.owner.String(), f.org, removeBob); xerr != nil {
		t.Fatalf("remove again: %v", xerr)
	}
	out, xerr := repo.AddToCampaign(ctx, f.org, f.owner.String(), acme.ID, f.other)
	if xerr != nil || out.Added != 1 {
		t.Fatalf("one-shot after removal = %+v, %v", out, xerr)
	}
	if err := handle.QueryRow(ctx, `SELECT COUNT(*) FROM campaign_lead_removals WHERE campaign_id = $1`, f.other).Scan(&removals); err != nil || removals != 0 {
		t.Fatalf("removals after one-shot = %d, %v", removals, err)
	}

	// The sweep and the targeted lookup both see the linked campaign, and the
	// delete guard reports it by name.
	linked, xerr := repo.LinkedCampaigns(ctx, &f.org)
	if xerr != nil || len(linked) != 1 || linked[0].CampaignID != f.other || linked[0].OrganizationID != f.org {
		t.Fatalf("linked = %+v, %v", linked, xerr)
	}
	byseg, xerr := repo.LinkedCampaignsForSegments(ctx, f.org, []uuid.UUID{acme.ID})
	if xerr != nil || len(byseg) != 1 || byseg[0].CampaignID != f.other {
		t.Fatalf("by segment = %+v, %v", byseg, xerr)
	}
	names, xerr := repo.CampaignsUsingSegment(ctx, f.org, acme.ID)
	if xerr != nil || len(names) != 1 || names[0] != "RevOps outreach" {
		t.Fatalf("using = %v, %v", names, xerr)
	}

	// Unlinking everything keeps the enrolled leads (alice, carol, bob).
	if xerr := repo.SetForCampaign(ctx, f.org, f.other, []uuid.UUID{}); xerr != nil {
		t.Fatalf("unlink: %v", xerr)
	}
	if links, _ = repo.ListForCampaign(ctx, f.org, f.other); len(links) != 0 {
		t.Fatalf("links after unlink = %+v", links)
	}
	inOther := models.SegmentCondition{Field: "campaign", Operator: "in", Values: []string{f.other.String()}}
	if got := segCount(t, repo, f.org, models.SegmentMatchAll, inOther); got != 3 {
		t.Errorf("leads after unlink = %d, want 3", got)
	}
}

// Issue #285: a contact created inside a segment joins it, and the include
// override is written in the same transaction as the contact. A segment that
// vanishes between the service's existence check and this insert must roll the
// whole create back rather than answer with a membership that never happened.
func TestLiveContactAddPinsSegments(t *testing.T) {
	f, repo := newSegmentFixture(t)
	ctx := context.Background()
	handle, pool := liveContactDB(t)
	contacts := NewContactRepostory(handle)

	acme, xerr := repo.Create(ctx, f.org, &f.owner, &models.Segment{
		Name: "Pin target", Color: "#0284c7", Match: models.SegmentMatchAll,
		Conditions: []models.SegmentCondition{{Field: "company", Operator: "equals", Value: "acme"}},
	})
	if xerr != nil {
		t.Fatalf("create segment: %v", xerr)
	}

	// A company the conditions do not match, so membership can only come from
	// the pin. The second contact names no segment and must stay out.
	tag := uuid.New().String()[:6]
	made, xerr := contacts.Add(ctx, f.owner.String(), f.org, []models.AddContact{
		{Email: "pinned-" + tag + "@initech.test", FirstName: "Pinned", Company: "initech", Segments: []string{acme.ID.String(), acme.ID.String()}},
		{Email: "loose-" + tag + "@initech.test", FirstName: "Loose", Company: "initech"},
	})
	if xerr != nil || len(made) != 2 {
		t.Fatalf("add: %+v, %v", made, xerr)
	}
	modes, xerr := repo.MemberModes(ctx, acme.ID, []uuid.UUID{made[0].ID, made[1].ID})
	if xerr != nil {
		t.Fatalf("modes: %v", xerr)
	}
	if modes[made[0].ID] != models.SegmentMemberInclude {
		t.Errorf("pinned contact mode = %q, want include", modes[made[0].ID])
	}
	if _, ok := modes[made[1].ID]; ok {
		t.Errorf("contact that named no segment was pinned")
	}

	// A segment id that is not in the organization (the state the race leaves
	// behind) fails the create instead of dropping the membership silently.
	gone := uuid.New().String()
	email := "vanished-" + tag + "@initech.test"
	if _, xerr := contacts.Add(ctx, f.owner.String(), f.org, []models.AddContact{
		{Email: email, FirstName: "Vanished", Company: "initech", Segments: []string{gone}},
	}); xerr == nil {
		t.Fatalf("create with a vanished segment succeeded")
	}
	var stranded int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM contacts WHERE organization_id = $1 AND email = $2`, f.org, email).Scan(&stranded); err != nil {
		t.Fatalf("count: %v", err)
	}
	if stranded != 0 {
		t.Errorf("rolled-back create left %d contacts behind", stranded)
	}
}

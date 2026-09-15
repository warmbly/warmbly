package repository

import (
	"context"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// Regression cover for issue #510: swapping a campaign's linked segment left
// the campaign holding both audiences. Detaching a link only ever stopped
// future enrolment, so the leads the wrong segment brought stayed and got
// emailed alongside the right ones.
//
// Run against the dev stack:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveCampaignAudience -v

type audienceFixture struct {
	*sharedOrgFixture
	pool *pgxpool.Pool
	catA uuid.UUID
	catB uuid.UUID
	// onlyA leaves with segment A. bothAB stays because segment B keeps it.
	// sentA stays because the campaign already wrote to it. manualA stays
	// because a person added it. onlyB arrives with segment B.
	onlyA   uuid.UUID
	bothAB  uuid.UUID
	sentA   uuid.UUID
	manualA uuid.UUID
	onlyB   uuid.UUID
	// tag makes the fixture's addresses unique per run; the re-import test
	// needs one of them back.
	tag string
}

func newAudienceFixture(t *testing.T) (*audienceFixture, SegmentRepository, ContactRepository) {
	t.Helper()
	handle, pool := liveContactDB(t)
	base := newSharedOrgFixture(t, pool)
	f := &audienceFixture{
		sharedOrgFixture: base, pool: pool,
		catA: uuid.New(), catB: uuid.New(),
		onlyA: uuid.New(), bothAB: uuid.New(), sentA: uuid.New(), manualA: uuid.New(), onlyB: uuid.New(),
	}
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	exec(`INSERT INTO categories (id, organization_id, user_id, title, color, position) VALUES ($1, $2, $3, 'Alpha', '#0284c7', 0), ($4, $2, $3, 'Beta', '#16a34a', 1)`,
		f.catA, f.org, f.owner, f.catB)

	tag := uuid.New().String()[:6]
	f.tag = tag
	for _, c := range []struct {
		id   uuid.UUID
		name string
		cats []uuid.UUID
	}{
		{f.onlyA, "onlya", []uuid.UUID{f.catA}},
		{f.bothAB, "bothab", []uuid.UUID{f.catA, f.catB}},
		{f.sentA, "senta", []uuid.UUID{f.catA}},
		{f.manualA, "manuala", []uuid.UUID{f.catA}},
		{f.onlyB, "onlyb", []uuid.UUID{f.catB}},
	} {
		exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields)
		      VALUES ($1, $2, $3, $4, $5, 'Aud', '', '', '{}'::jsonb)`,
			c.id, f.owner, f.org, c.name+"-"+tag+"@i510.test", c.name)
		for _, cat := range c.cats {
			exec(`INSERT INTO contact_categories (contact_id, category_id) VALUES ($1, $2)`, c.id, cat)
		}
	}
	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM segments WHERE organization_id = $1`, f.org},
			{`DELETE FROM campaign_contact_progress WHERE campaign_id IN (SELECT id FROM campaigns WHERE organization_id = $1)`, f.org},
			{`DELETE FROM sequences WHERE organization_id = $1`, f.org},
			{`DELETE FROM campaign_lead_removals WHERE campaign_id IN (SELECT id FROM campaigns WHERE organization_id = $1)`, f.org},
			{`DELETE FROM contact_categories WHERE category_id = ANY($1)`, []uuid.UUID{f.catA, f.catB}},
			{`DELETE FROM categories WHERE organization_id = $1`, f.org},
		} {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})
	return f, NewSegmentRepository(handle), NewContactRepostory(handle)
}

// categorySegment saves a segment matching one category.
func categorySegment(t *testing.T, repo SegmentRepository, org uuid.UUID, owner uuid.UUID, name string, cat uuid.UUID) uuid.UUID {
	t.Helper()
	seg, xerr := repo.Create(context.Background(), org, &owner, &models.Segment{
		Name: name, Color: "#0284c7", Match: models.SegmentMatchAll,
		Conditions: []models.SegmentCondition{{Field: "category", Operator: "in", Values: []string{cat.String()}}},
	})
	if xerr != nil {
		t.Fatalf("create segment %s: %v", name, xerr)
	}
	return seg.ID
}

// leadNames is the campaign's leads by first name, sorted, which is what the
// Leads tab shows.
func (f *audienceFixture) leadNames(t *testing.T) []string {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `
		SELECT c.first_name FROM campaign_leads cl
		JOIN contacts c ON c.id = cl.contact_id
		WHERE cl.campaign_id = $1`, f.campaign)
	if err != nil {
		t.Fatalf("leads: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("leads: %v", err)
	}
	sort.Strings(out)
	return out
}

func (f *audienceFixture) leadSource(t *testing.T, contact uuid.UUID) string {
	t.Helper()
	var src string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT source FROM campaign_leads WHERE campaign_id = $1 AND contact_id = $2`, f.campaign, contact).Scan(&src); err != nil {
		t.Fatalf("lead source: %v", err)
	}
	return src
}

func eq(t *testing.T, got, want []string, what string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", what, got, want)
		}
	}
}

func TestLiveCampaignAudienceFollowsTheLinkedSegments(t *testing.T) {
	f, segments, contacts := newAudienceFixture(t)
	ctx := context.Background()
	segA := categorySegment(t, segments, f.org, f.owner, "Alpha list", f.catA)
	segB := categorySegment(t, segments, f.org, f.owner, "Beta list", f.catB)

	// Link A. Every member of A becomes a lead.
	added, change, _, xerr := segments.ReplaceForCampaign(ctx, f.org, f.campaign, []uuid.UUID{segA})
	if xerr != nil || added != 4 {
		t.Fatalf("link A = %d, %+v, %v", added, change, xerr)
	}
	eq(t, f.leadNames(t), []string{"bothab", "manuala", "onlya", "senta"}, "leads after linking A")

	// One of them is claimed by hand, which is what stops the detachment from
	// withdrawing it later.
	if _, xerr := contacts.BulkUpdate(ctx, f.owner.String(), f.org, &models.BulkEditContactsData{
		ContactSelection: models.ContactSelection{Contacts: []string{f.manualA.String()}},
		AddCampaigns:     []string{f.campaign.String()},
	}); xerr != nil {
		t.Fatalf("hand add: %v", xerr)
	}
	if got := f.leadSource(t, f.manualA); got != "manual" {
		t.Fatalf("hand-added lead source = %q, want manual", got)
	}
	if got := f.leadSource(t, f.onlyA); got != "segment" {
		t.Fatalf("segment-enrolled lead source = %q, want segment", got)
	}

	// And one has already been written to.
	seq := uuid.New()
	if _, err := f.pool.Exec(ctx, `INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_plain, body_html)
	      VALUES ($1, $2, $3, 'Email 1', 'Hi', 'Body', 'Body')`, seq, f.campaign, f.org); err != nil {
		t.Fatalf("sequence: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, dispatched_at)
	      VALUES ($1, $2, $3, NOW())`, f.campaign, f.sentA, seq); err != nil {
		t.Fatalf("progress: %v", err)
	}

	// The swap: A out, B in. This is the issue.
	added, change, _, xerr = segments.ReplaceForCampaign(ctx, f.org, f.campaign, []uuid.UUID{segB})
	if xerr != nil {
		t.Fatalf("swap: %v", xerr)
	}
	if added != 1 {
		t.Fatalf("swap added %d leads, want 1 (onlyb)", added)
	}
	if change.Withdrawn != 1 {
		t.Fatalf("swap withdrew %d leads, want 1 (onlya)", change.Withdrawn)
	}
	if change.Contacted != 1 {
		t.Fatalf("swap reported %d already-contacted leads, want 1 (senta)", change.Contacted)
	}
	eq(t, f.leadNames(t), []string{"bothab", "manuala", "onlyb", "senta"}, "leads after the swap")

	// The removal is not a hand-made one, so re-linking A brings its audience
	// back rather than treating it as held out.
	var removals int
	if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM campaign_lead_removals WHERE campaign_id = $1`, f.campaign).Scan(&removals); err != nil {
		t.Fatalf("removals: %v", err)
	}
	if removals != 0 {
		t.Fatalf("withdrawal recorded %d hand-removals, want 0", removals)
	}
	added, change, _, xerr = segments.ReplaceForCampaign(ctx, f.org, f.campaign, []uuid.UUID{segA, segB})
	if xerr != nil || added != 1 || change.Withdrawn != 0 {
		t.Fatalf("re-link A = %d added, %+v, %v", added, change, xerr)
	}
	eq(t, f.leadNames(t), []string{"bothab", "manuala", "onlya", "onlyb", "senta"}, "leads after re-linking A")
}

func TestLiveCampaignAudienceEnrolmentStaysAdditiveWithinALink(t *testing.T) {
	f, segments, _ := newAudienceFixture(t)
	ctx := context.Background()
	segA := categorySegment(t, segments, f.org, f.owner, "Alpha list", f.catA)
	segB := categorySegment(t, segments, f.org, f.owner, "Beta list", f.catB)

	if _, _, _, xerr := segments.ReplaceForCampaign(ctx, f.org, f.campaign, []uuid.UUID{segA, segB}); xerr != nil {
		t.Fatalf("link: %v", xerr)
	}
	eq(t, f.leadNames(t), []string{"bothab", "manuala", "onlya", "onlyb", "senta"}, "leads after linking both")

	// A contact who LEAVES a segment that is still linked keeps their lead row
	// and their progress: only detaching the link withdraws an audience.
	if _, err := f.pool.Exec(ctx, `DELETE FROM contact_categories WHERE contact_id = $1 AND category_id = $2`, f.onlyA, f.catA); err != nil {
		t.Fatalf("leave segment: %v", err)
	}
	added, change, _, xerr := segments.ReplaceForCampaign(ctx, f.org, f.campaign, []uuid.UUID{segA, segB})
	if xerr != nil || added != 0 || change.Withdrawn != 0 {
		t.Fatalf("re-save of the same set = %d added, %+v, %v", added, change, xerr)
	}
	eq(t, f.leadNames(t), []string{"bothab", "manuala", "onlya", "onlyb", "senta"}, "leads after a member left a linked segment")

	// Detaching the segment they already left does not withdraw them either:
	// the scope is the detached segment's CURRENT members.
	added, change, _, xerr = segments.ReplaceForCampaign(ctx, f.org, f.campaign, []uuid.UUID{segB})
	if xerr != nil || added != 0 {
		t.Fatalf("detach A = %d added, %v", added, xerr)
	}
	// manuala and senta are current members of A and nothing else keeps them.
	// onlya is not withdrawn: it left A before the detachment, so it is not
	// part of the audience being taken back, and bothab is kept by B.
	if change.Withdrawn != 2 {
		t.Fatalf("detach A withdrew %d, want 2 (manuala and senta)", change.Withdrawn)
	}
	eq(t, f.leadNames(t), []string{"bothab", "onlya", "onlyb"}, "leads after detaching A")
}

func TestLiveCampaignAudienceDetachingEverythingEmptiesIt(t *testing.T) {
	f, segments, _ := newAudienceFixture(t)
	ctx := context.Background()
	segA := categorySegment(t, segments, f.org, f.owner, "Alpha list", f.catA)

	if _, _, _, xerr := segments.ReplaceForCampaign(ctx, f.org, f.campaign, []uuid.UUID{segA}); xerr != nil {
		t.Fatalf("link: %v", xerr)
	}
	added, change, _, xerr := segments.ReplaceForCampaign(ctx, f.org, f.campaign, []uuid.UUID{})
	if xerr != nil || added != 0 || change.Withdrawn != 4 || change.Contacted != 0 {
		t.Fatalf("detach all = %d added, %+v, %v", added, change, xerr)
	}
	if got := f.leadNames(t); len(got) != 0 {
		t.Fatalf("leads after detaching every segment = %v, want none", got)
	}
}

// A contact re-imported into the campaign is a person choosing that lead, so
// detaching the segment that first enrolled them leaves them alone. The upsert
// in Add is the only path that can promote an existing segment lead, since a
// contact edit only ever adds campaigns the contact is not already in.
func TestLiveCampaignAudienceAReimportClaimsASegmentLead(t *testing.T) {
	f, segments, contacts := newAudienceFixture(t)
	ctx := context.Background()
	segA := categorySegment(t, segments, f.org, f.owner, "Alpha list", f.catA)

	if _, _, _, xerr := segments.ReplaceForCampaign(ctx, f.org, f.campaign, []uuid.UUID{segA}); xerr != nil {
		t.Fatalf("link: %v", xerr)
	}
	if got := f.leadSource(t, f.onlyA); got != "segment" {
		t.Fatalf("lead source before the re-import = %q, want segment", got)
	}

	if _, xerr := contacts.Add(ctx, f.owner.String(), f.org, []models.AddContact{{
		FirstName: "onlya", Email: "onlya-" + f.tag + "@i510.test",
		Campaigns: []string{f.campaign.String()},
	}}); xerr != nil {
		t.Fatalf("re-import: %v", xerr)
	}
	if got := f.leadSource(t, f.onlyA); got != "manual" {
		t.Fatalf("lead source after the re-import = %q, want manual", got)
	}

	// The one-shot "add to campaign" is the same kind of act, so it claims the
	// leads the link had enrolled even though every one of them is already in.
	out, xerr := segments.AddToCampaign(ctx, f.org, f.owner.String(), segA, f.campaign)
	if xerr != nil || out.Added != 0 {
		t.Fatalf("one-shot enrol = %+v, %v", out, xerr)
	}
	for _, id := range []uuid.UUID{f.bothAB, f.sentA, f.manualA} {
		if got := f.leadSource(t, id); got != "manual" {
			t.Fatalf("lead source after the one-shot enrol = %q, want manual", got)
		}
	}

	_, change, _, xerr := segments.ReplaceForCampaign(ctx, f.org, f.campaign, []uuid.UUID{})
	if xerr != nil {
		t.Fatalf("detach: %v", xerr)
	}
	if change.Withdrawn != 0 {
		t.Fatalf("detach withdrew %d, want 0: every lead is claimed by hand", change.Withdrawn)
	}
	eq(t, f.leadNames(t), []string{"bothab", "manuala", "onlya", "senta"}, "leads after detaching the only segment")
}

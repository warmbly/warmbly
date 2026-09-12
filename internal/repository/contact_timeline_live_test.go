package repository

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

// Live cover for the contact timeline's lifecycle events and first-touch
// source attribution (issue #255): creation, campaign and category membership
// changes are written inside the same transactions as the links, and read
// back through ListTimeline with the names resolved at write time.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveContactTimeline -v

func countTimeline(events []models.ContactTimelineEvent, typ models.ContactTimelineEventType, match func(models.ContactTimelineEvent) bool) int {
	n := 0
	for _, e := range events {
		if e.Type == typ && (match == nil || match(e)) {
			n++
		}
	}
	return n
}

func TestLiveContactTimelineLifecycleEvents(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()
	repo := NewContactRepostory(handle)

	category := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO categories (id, organization_id, user_id, title, color, position) VALUES ($1, $2, $3, 'Warm lead', '#ff8800', 0)`,
		category, f.org, f.owner); err != nil {
		t.Fatalf("category: %v", err)
	}
	t.Cleanup(func() {
		// Registered after the fixture, so it runs before the fixture drops
		// the contacts and users the category hangs off.
		_, _ = pool.Exec(context.Background(), `DELETE FROM contact_categories WHERE category_id = $1`, category)
		_, _ = pool.Exec(context.Background(), `DELETE FROM categories WHERE id = $1`, category)
	})

	email := "i255-" + uuid.New().String()[:8] + "@test.local"
	created, xerr := repo.Add(ctx, f.owner.String(), f.org, []models.AddContact{{
		FirstName: "Dana", LastName: "Ruiz", Email: email,
		Campaigns:  []string{f.campaign.String()},
		Categories: []string{category.String()},
		Source:     models.ContactSourceManual,
	}})
	if xerr != nil || len(created) != 1 {
		t.Fatalf("add: %v (%d created)", xerr, len(created))
	}
	id := created[0].ID

	timeline := func() []models.ContactTimelineEvent {
		t.Helper()
		res, xerr := repo.ListTimeline(ctx, f.owner, &f.org, id, 50, nil)
		if xerr != nil {
			t.Fatalf("timeline: %v", xerr)
		}
		return res.Data
	}
	byCampaign := func(name string) func(models.ContactTimelineEvent) bool {
		return func(e models.ContactTimelineEvent) bool { return e.CampaignName != nil && *e.CampaignName == name }
	}
	byCategory := func(e models.ContactTimelineEvent) bool {
		return e.CategoryTitle != nil && *e.CategoryTitle == "Warm lead" && e.CategoryID != nil && *e.CategoryID == category
	}

	ev := timeline()
	if n := countTimeline(ev, models.TimelineContactCreated, func(e models.ContactTimelineEvent) bool {
		return e.Source != nil && *e.Source == "manual" && e.UserID != nil && *e.UserID == f.owner
	}); n != 1 {
		t.Fatalf("want one manual contact_created event by the owner, got %d in %+v", n, ev)
	}
	if n := countTimeline(ev, models.TimelineCampaignAdded, byCampaign("Agency partnerships")); n != 1 {
		t.Fatalf("want campaign_added for Agency partnerships, got %d", n)
	}
	if n := countTimeline(ev, models.TimelineCategoryAdded, byCategory); n != 1 {
		t.Fatalf("want category_added for Warm lead, got %d", n)
	}

	// Re-adding the same address is an upsert, not a creation: no second
	// contact_created, and the original source survives the import's claim.
	if _, xerr := repo.Add(ctx, f.owner.String(), f.org, []models.AddContact{{
		Email: email, Company: "Pied Piper", Source: models.ContactSourceImport, SourceDetail: "leads.csv",
	}}); xerr != nil {
		t.Fatalf("re-add: %v", xerr)
	}
	if n := countTimeline(timeline(), models.TimelineContactCreated, nil); n != 1 {
		t.Fatalf("an upsert must not log a second creation, got %d", n)
	}
	detail, xerr := repo.GetDetail(ctx, f.owner, &f.org, id)
	if xerr != nil {
		t.Fatalf("detail: %v", xerr)
	}
	if detail.Source != models.ContactSourceManual || detail.FirstSeenAt.IsZero() {
		t.Fatalf("want first-touch source manual with a first-seen time, got %q at %v", detail.Source, detail.FirstSeenAt)
	}

	// Single-contact update: swap campaigns, drop the category.
	if _, xerr := repo.Update(ctx, f.owner.String(), id.String(), f.org, &models.UpdateContact{
		Campaigns:        []string{f.other.String()},
		RemoveCategories: []string{category.String()},
	}); xerr != nil {
		t.Fatalf("update: %v", xerr)
	}
	ev = timeline()
	if n := countTimeline(ev, models.TimelineCampaignRemoved, byCampaign("Agency partnerships")); n != 1 {
		t.Fatalf("want campaign_removed for Agency partnerships, got %d", n)
	}
	if n := countTimeline(ev, models.TimelineCampaignAdded, byCampaign("RevOps outreach")); n != 1 {
		t.Fatalf("want campaign_added for RevOps outreach, got %d", n)
	}
	if n := countTimeline(ev, models.TimelineCategoryRemoved, byCategory); n != 1 {
		t.Fatalf("want category_removed for Warm lead, got %d", n)
	}

	// Bulk edit: the other direction for both link kinds.
	if _, xerr := repo.BulkUpdate(ctx, f.owner.String(), f.org, &models.BulkEditContactsData{
		ContactSelection: models.ContactSelection{Contacts: []string{id.String()}},
		RemoveCampaigns:  []string{f.other.String()},
		AddCategories:    []string{category.String()},
	}); xerr != nil {
		t.Fatalf("bulk update: %v", xerr)
	}
	ev = timeline()
	if n := countTimeline(ev, models.TimelineCampaignRemoved, byCampaign("RevOps outreach")); n != 1 {
		t.Fatalf("want campaign_removed for RevOps outreach, got %d", n)
	}
	if n := countTimeline(ev, models.TimelineCategoryAdded, byCategory); n != 2 {
		t.Fatalf("want a second category_added for Warm lead, got %d", n)
	}
	// A no-op bulk edit (already linked) writes nothing.
	if _, xerr := repo.BulkUpdate(ctx, f.owner.String(), f.org, &models.BulkEditContactsData{
		ContactSelection: models.ContactSelection{Contacts: []string{id.String()}}, AddCategories: []string{category.String()},
	}); xerr != nil {
		t.Fatalf("bulk no-op: %v", xerr)
	}
	if n := countTimeline(timeline(), models.TimelineCategoryAdded, byCategory); n != 2 {
		t.Fatalf("an already-linked category must not log again, got %d", n)
	}

	// Newest first, and the whole feed is in order.
	for i := 1; i < len(ev); i++ {
		if ev[i].At.After(ev[i-1].At) {
			t.Fatalf("timeline out of order at %d: %s after %s", i, ev[i].At, ev[i-1].At)
		}
	}
}

// A contact created from a campaign's Leads tab is attributed to that campaign
// by name, resolved server-side.
func TestLiveContactSourceCampaignResolvesName(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()
	repo := NewContactRepostory(handle)

	created, xerr := repo.Add(ctx, f.mate.String(), f.org, []models.AddContact{{
		Email:     "i255-" + uuid.New().String()[:8] + "@test.local",
		Campaigns: []string{f.campaign.String()},
		Source:    models.ContactSourceCampaign,
	}})
	if xerr != nil || len(created) != 1 {
		t.Fatalf("add: %v", xerr)
	}
	detail, xerr := repo.GetDetail(ctx, f.mate, &f.org, created[0].ID)
	if xerr != nil {
		t.Fatalf("detail: %v", xerr)
	}
	if detail.Source != models.ContactSourceCampaign || detail.SourceDetail != "Agency partnerships" {
		t.Fatalf("want source campaign/Agency partnerships, got %q/%q", detail.Source, detail.SourceDetail)
	}
	if _, xerr := repo.Add(ctx, f.mate.String(), f.org, []models.AddContact{{
		Email: "i255-bad-" + uuid.New().String()[:8] + "@test.local", Source: "website",
	}}); xerr == nil {
		t.Fatal("an unknown source must be refused, not stored")
	}
}

// Live cover for the timeline's cursor (issue #305): events that share a
// timestamp, within one source and across sources, must land on one side of
// a page boundary or the other, never be skipped and never repeat, and a page
// filled by a single source must still report that more follow.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveContactTimelinePages -v
func TestLiveContactTimelinePagesOnTiesWithoutGapsOrRepeats(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()
	repo := NewContactRepostory(handle)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	at := time.Date(2026, 6, 9, 11, 42, 0, 250000000, time.UTC)
	step := uuid.New()
	exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_plain, body_html) VALUES ($1, $2, $3, 'Intro', 'Quick question', '', '')`, step, f.campaign, f.org)
	// Three stamps on one lead at the same instant: sent, a summary open
	// (nothing in email_opens stands for it) and a reply.
	exec(`INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at, opened_at, replied_at) VALUES ($1, $2, $3, $4, $4, $4)`,
		f.campaign, f.contact, step, at)
	for i := 0; i < 3; i++ {
		exec(`INSERT INTO contact_notes (contact_id, organization_id, user_id, content, created_at) VALUES ($1, $2, $3, $4, $5)`,
			f.contact, f.org, f.owner, "note "+strconv.Itoa(i), at)
	}
	exec(`INSERT INTO contact_notes (contact_id, organization_id, user_id, content, created_at) VALUES ($1, $2, $3, 'older', $4)`,
		f.contact, f.org, f.owner, at.Add(-time.Hour))
	for i := 0; i < 2; i++ {
		exec(`INSERT INTO contact_activities (contact_id, organization_id, user_id, activity_type, metadata, created_at) VALUES ($1, $2, $3, 'campaign_added', '{"campaign_name":"Agency partnerships"}', $4)`,
			f.contact, f.org, f.owner, at)
	}
	exec(`INSERT INTO contact_activities (contact_id, organization_id, user_id, activity_type, metadata, created_at) VALUES ($1, $2, $3, 'contact_created', '{"source":"manual"}', $4)`,
		f.contact, f.org, f.owner, at.Add(time.Hour))
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM contact_activities WHERE contact_id = $1`, f.contact)
		_, _ = pool.Exec(c, `DELETE FROM contact_notes WHERE contact_id = $1`, f.contact)
		_, _ = pool.Exec(c, `DELETE FROM campaign_contact_progress WHERE contact_id = $1`, f.contact)
		_, _ = pool.Exec(c, `DELETE FROM sequences WHERE id = $1`, step)
	})
	const total = 10 // 3 stamps + 4 notes + 3 activities

	// Walk the feed three at a time: a page of three can be filled by the
	// notes at `at` alone, and eight events share that instant.
	var all []models.ContactTimelineEvent
	var cursor *models.ContactTimelineKey
	for page := 0; ; page++ {
		res, xerr := repo.ListTimeline(ctx, f.owner, &f.org, f.contact, 3, cursor)
		if xerr != nil {
			t.Fatalf("page %d: %v", page, xerr)
		}
		if res.HasMore != res.Pagination.HasMore {
			t.Fatalf("page %d: has_more %v disagrees with pagination.has_more %v", page, res.HasMore, res.Pagination.HasMore)
		}
		all = append(all, res.Data...)
		if !res.HasMore {
			if res.Pagination.NextCursor != nil {
				t.Fatalf("last page must carry no cursor, got %q", *res.Pagination.NextCursor)
			}
			break
		}
		if res.Pagination.NextCursor == nil || len(res.Data) != 3 {
			t.Fatalf("page %d: has_more with %d events and cursor %v", page, len(res.Data), res.Pagination.NextCursor)
		}
		cAt, cSource, cID, xerr := paging.DecodeMergedCursor(*res.Pagination.NextCursor)
		if xerr != nil {
			t.Fatalf("page %d: cursor does not decode: %v", page, xerr)
		}
		if last := res.Data[2].Key; !cAt.Equal(last.At) || models.ContactTimelineSource(cSource) != last.Source || cID != last.ID {
			t.Fatalf("page %d: cursor %v/%d/%s is not the last event's key %+v", page, cAt, cSource, cID, last)
		}
		cursor = &models.ContactTimelineKey{At: cAt, Source: models.ContactTimelineSource(cSource), ID: cID}
		if page > total {
			t.Fatal("the walk never ends")
		}
	}
	if len(all) != total {
		t.Fatalf("want %d events across the pages, got %d: %+v", total, len(all), all)
	}
	seen := map[models.ContactTimelineKey]bool{}
	for i, e := range all {
		if seen[e.Key] {
			t.Fatalf("event %d repeated across pages: %+v", i, e.Key)
		}
		seen[e.Key] = true
		if i > 0 && !e.Key.Before(all[i-1].Key) {
			t.Fatalf("feed out of order at %d: %+v then %+v", i, all[i-1].Key, e.Key)
		}
	}
	for typ, want := range map[models.ContactTimelineEventType]int{
		models.TimelineEmailSent: 1, models.TimelineEmailOpened: 1, models.TimelineEmailReplied: 1,
		models.TimelineNote: 4, models.TimelineCampaignAdded: 2, models.TimelineContactCreated: 1,
	} {
		if n := countTimeline(all, typ, nil); n != want {
			t.Fatalf("want %d %s events, got %d", want, typ, n)
		}
	}

	// The legacy bare timestamp still means "strictly older than": rank zero
	// sits below every source, so nothing at that instant qualifies.
	res, xerr := repo.ListTimeline(ctx, f.owner, &f.org, f.contact, 50, &models.ContactTimelineKey{At: at})
	if xerr != nil {
		t.Fatalf("before: %v", xerr)
	}
	if len(res.Data) != 1 || res.Data[0].Type != models.TimelineNote || res.HasMore {
		t.Fatalf("want only the older note before %s, got %+v (has_more %v)", at, res.Data, res.HasMore)
	}
}

package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// Regression cover for issue #436: the folder / tag / category registries were
// keyed on the user who created them, so a tag the workspace owner made was
// invisible to every teammate — the mailbox list still carried the tag ids, but
// /auth/me listed only the caller's own rows, so the chips rendered as nothing.
//
// Labels are organization assets, so the creator has left these signatures
// entirely: every read and write below is keyed on the workspace, and each one
// is checked against a label a DIFFERENT member made, plus a real label
// belonging to another workspace that must never be reachable.
//
// Run against the dev stack:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveGroup -v

func TestLiveGroupRegistriesAreOrganizationWide(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()

	// A second organization, owned by the same person, whose tag must never
	// show up in the first one: a user may belong to several workspaces, which
	// is the other half of what a per-user registry got wrong.
	foreignOrg := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Issue 436 other', $2, $3)`,
		foreignOrg, "i436-"+foreignOrg.String()[:8], f.owner); err != nil {
		t.Fatalf("foreign org: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tags WHERE organization_id = ANY($1)`, []uuid.UUID{f.org, foreignOrg})
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, foreignOrg)
	})

	tags := NewGroupRepostory(handle, models.Tags)

	// The OWNER creates the tag, exactly as the reporter did.
	created, xerr := tags.Create(ctx, f.org, f.owner, &models.GroupCreate{Title: "VIP senders", Color: "#a855f7"})
	if xerr != nil {
		t.Fatalf("create: %v", xerr)
	}
	foreign, xerr := tags.Create(ctx, foreignOrg, f.owner, &models.GroupCreate{Title: "Other workspace", Color: "#ef4444"})
	if xerr != nil {
		t.Fatalf("create foreign: %v", xerr)
	}

	// The TEAMMATE lists them. This is what /auth/me serves.
	listed, xerr := tags.List(ctx, f.org)
	if xerr != nil {
		t.Fatalf("list: %v", xerr)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("teammate sees %d tags (%+v), want only %s", len(listed), listed, created.ID)
	}
	if listed[0].Title != "VIP senders" {
		t.Errorf("title = %q, want %q", listed[0].Title, "VIP senders")
	}

	// ...and the other workspace's tag stays in the other workspace.
	other, xerr := tags.List(ctx, foreignOrg)
	if xerr != nil {
		t.Fatalf("list foreign: %v", xerr)
	}
	if len(other) != 1 || other[0].ID != foreign.ID {
		t.Fatalf("foreign workspace sees %+v, want only %s", other, foreign.ID)
	}

	// The teammate can rename it: a tag is not the creator's private property.
	title := "VIP"
	if _, xerr := tags.Update(ctx, f.org, created.ID, &models.GroupUpdate{Title: &title}); xerr != nil {
		t.Fatalf("teammate update: %v", xerr)
	}

	// Another organization cannot, even naming the right id.
	if _, xerr := tags.Update(ctx, foreignOrg, created.ID, &models.GroupUpdate{Title: &title}); xerr == nil {
		t.Error("a tag was editable from another workspace")
	}
	if xerr := tags.Delete(ctx, foreignOrg, created.ID); xerr == nil {
		t.Error("a tag was deletable from another workspace")
	}
}

// A mailbox is a workspace asset and so are its tags, so the member who did not
// connect the mailbox must still be able to tag it and read the tag back.
func TestLiveMailboxTagsAreWritableByAnyMember(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()

	mailbox := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, warmup_tag)
		VALUES ($1, $2, $3, $4, 'Owner mailbox', '', '', 'smtp_imap', '')`,
		mailbox, f.owner, f.org, "i436-"+mailbox.String()[:8]+"@test.local"); err != nil {
		t.Fatalf("mailbox: %v", err)
	}
	foreignOrg := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Issue 436 other', $2, $3)`,
		foreignOrg, "i436-"+foreignOrg.String()[:8], f.owner); err != nil {
		t.Fatalf("foreign org: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM email_tags WHERE email_id = $1`, mailbox)
		_, _ = pool.Exec(c, `DELETE FROM email_accounts WHERE id = $1`, mailbox)
		_, _ = pool.Exec(c, `DELETE FROM tags WHERE organization_id = ANY($1)`, []uuid.UUID{f.org, foreignOrg})
		_, _ = pool.Exec(c, `DELETE FROM organizations WHERE id = $1`, foreignOrg)
	})

	tags := NewGroupRepostory(handle, models.Tags)
	tag, xerr := tags.Create(ctx, f.org, f.owner, &models.GroupCreate{Title: "Client A", Color: "#38bdf8"})
	if xerr != nil {
		t.Fatalf("create tag: %v", xerr)
	}
	foreignTag, xerr := tags.Create(ctx, foreignOrg, f.owner, &models.GroupCreate{Title: "Client B", Color: "#f59e0b"})
	if xerr != nil {
		t.Fatalf("create foreign tag: %v", xerr)
	}

	emails := NewEmailRepostory(handle, nil)

	// The teammate bulk-tags the owner's mailbox.
	owned, xerr := emails.BulkUpdateTags(ctx, f.org.String(), []uuid.UUID{mailbox}, []uuid.UUID{tag.ID}, nil)
	if xerr != nil {
		t.Fatalf("bulk tag: %v", xerr)
	}
	if owned != 1 {
		t.Fatalf("bulk tag reported %d mailboxes, want 1", owned)
	}

	got, xerr := emails.Get(ctx, f.org.String(), mailbox.String())
	if xerr != nil {
		t.Fatalf("get mailbox: %v", xerr)
	}
	if len(got.Tags) != 1 || got.Tags[0] != tag.ID.String() {
		t.Fatalf("mailbox tags = %v, want [%s]", got.Tags, tag.ID)
	}

	// A real tag belonging to ANOTHER workspace is dropped rather than linked:
	// the row exists, so the foreign key alone would have let it through.
	if _, xerr := emails.BulkUpdateTags(ctx, f.org.String(), []uuid.UUID{mailbox}, []uuid.UUID{foreignTag.ID}, nil); xerr != nil {
		t.Fatalf("bulk tag foreign: %v", xerr)
	}
	got, xerr = emails.Get(ctx, f.org.String(), mailbox.String())
	if xerr != nil {
		t.Fatalf("get mailbox again: %v", xerr)
	}
	if len(got.Tags) != 1 || got.Tags[0] != tag.ID.String() {
		t.Fatalf("mailbox tags = %v after a foreign id, want only [%s]", got.Tags, tag.ID)
	}

	// Same rule on the single-mailbox PATCH, which replaces the whole set.
	updated, xerr := emails.Update(ctx, f.org.String(), mailbox.String(), &models.UpdateEmail{
		Tags: []string{tag.ID.String(), foreignTag.ID.String()},
	})
	if xerr != nil {
		t.Fatalf("update mailbox: %v", xerr)
	}
	if len(updated.Tags) != 1 || updated.Tags[0] != tag.ID.String() {
		t.Fatalf("update returned tags %v, want only [%s]", updated.Tags, tag.ID)
	}
}

// Conversation labels ride the same registry and the same rule: the inbox is
// read per organization, so a thread one member files must read as filed to the
// next, and the label rail must count it.
func TestLiveConversationLabelsAreOrganizationWide(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()

	mailbox, message := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, warmup_tag)
		VALUES ($1, $2, $3, $4, 'Owner mailbox', '', '', 'smtp_imap', '')`,
		mailbox, f.owner, f.org, "i436l-"+mailbox.String()[:8]+"@test.local"); err != nil {
		t.Fatalf("mailbox: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO unibox_emails (id, user_id, email_id, thread_id, subject, folder, seen)
		VALUES ($1, $2, $3, 'i436-thread', 'Re: pricing', 'inbox', false)`,
		message, f.owner, mailbox); err != nil {
		t.Fatalf("message: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM unibox_thread_labels WHERE organization_id = $1`, f.org)
		_, _ = pool.Exec(c, `DELETE FROM unibox_emails WHERE email_id = $1`, mailbox)
		_, _ = pool.Exec(c, `DELETE FROM email_accounts WHERE id = $1`, mailbox)
		_, _ = pool.Exec(c, `DELETE FROM categories WHERE organization_id = $1`, f.org)
	})

	cats := NewGroupRepostory(handle, models.Categories)
	cat, xerr := cats.Create(ctx, f.org, f.owner, &models.GroupCreate{Title: "Interested", Color: "#10b981"})
	if xerr != nil {
		t.Fatalf("create category: %v", xerr)
	}

	// A real category in another workspace. Both label writes are handed it
	// alongside a legitimate id: the foreign key alone would let it through, so
	// only the organization check keeps it out.
	foreignOrg := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Issue 436 label other', $2, $3)`,
		foreignOrg, "i436l-"+foreignOrg.String()[:8], f.owner); err != nil {
		t.Fatalf("foreign org: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM categories WHERE organization_id = $1`, foreignOrg)
		_, _ = pool.Exec(c, `DELETE FROM organizations WHERE id = $1`, foreignOrg)
	})
	foreignCat, xerr := cats.Create(ctx, foreignOrg, f.owner, &models.GroupCreate{Title: "Theirs", Color: "#ef4444"})
	if xerr != nil {
		t.Fatalf("create foreign category: %v", xerr)
	}

	unibox := NewUniboxRepository(handle)

	// The OWNER labels the thread.
	set, err := unibox.SetThreadLabels(ctx, f.org, f.owner, "i436-thread", []uuid.UUID{cat.ID, foreignCat.ID})
	if err != nil {
		t.Fatalf("set labels: %v", err)
	}
	if len(set) != 1 || set[0].ID != cat.ID {
		t.Fatalf("set returned %+v, want only this workspace's [%s]", set, cat.ID)
	}

	// The TEAMMATE reads it back, filters on it, and sees it counted.
	labels, err := unibox.ListThreadLabels(ctx, f.org, "i436-thread")
	if err != nil {
		t.Fatalf("list labels: %v", err)
	}
	if len(labels) != 1 || labels[0].ID != cat.ID {
		t.Fatalf("teammate sees labels %+v, want [%s]", labels, cat.ID)
	}

	res, err := unibox.Search(ctx, f.org, &models.MailSearchParams{CategoryIDs: []uuid.UUID{cat.ID}, PageSize: 50})
	if err != nil {
		t.Fatalf("search by label: %v", err)
	}
	if len(res.Data) != 1 || res.Data[0].ID != message {
		t.Fatalf("label filter returned %d rows, want the labelled thread", len(res.Data))
	}

	// The automation "label email" action adds without clobbering, and drops a
	// category another workspace owns rather than borrowing it. The foreign id
	// is a real row, so the foreign key alone would have let it through.
	second, xerr := cats.Create(ctx, f.org, f.mate, &models.GroupCreate{Title: "Follow up", Color: "#f59e0b"})
	if xerr != nil {
		t.Fatalf("create second category: %v", xerr)
	}
	if err := unibox.AddThreadLabels(ctx, f.org, "i436-thread", []uuid.UUID{second.ID, foreignCat.ID}); err != nil {
		t.Fatalf("add labels: %v", err)
	}
	labels, err = unibox.ListThreadLabels(ctx, f.org, "i436-thread")
	if err != nil {
		t.Fatalf("list labels after add: %v", err)
	}
	if len(labels) != 2 {
		t.Fatalf("additive labelling left %+v, want both categories and nothing else", labels)
	}

	overview, err := unibox.Overview(ctx, f.org)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	var found bool
	for _, c := range overview.Categories {
		if c.ID == cat.ID {
			found = true
			if c.Total != 1 {
				t.Errorf("label rail total = %d, want 1", c.Total)
			}
		}
	}
	if !found {
		t.Fatalf("the label rail does not list %s: %+v", cat.ID, overview.Categories)
	}
}

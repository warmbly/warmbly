package agencyhome

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type fakeAgencies struct {
	member bool
	usage  *models.AgencyUsage
}

func (f *fakeAgencies) Get(_ context.Context, agencyID, _ uuid.UUID) (*models.Agency, *errx.Error) {
	if !f.member {
		return nil, errx.NewWithIdentifier(errx.NotFound, "agency_not_found", "no")
	}
	return &models.Agency{ID: agencyID}, nil
}

func (f *fakeAgencies) Usage(context.Context, uuid.UUID, uuid.UUID) (*models.AgencyUsage, *errx.Error) {
	if f.usage == nil {
		return &models.AgencyUsage{Unlimited: true}, nil
	}
	return f.usage, nil
}

type fakeRepo struct {
	clients []models.AgencyClientMembership
	threads []models.AgencyInboxThread

	calls    map[string]int
	campaign []uuid.UUID
	mailbox  []uuid.UUID
	replies  []uuid.UUID
	advisor  []uuid.UUID
	inbox    *models.AgencyInboxQuery
}

func (f *fakeRepo) hit(name string) {
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[name]++
}

func (f *fakeRepo) ClientMemberships(context.Context, uuid.UUID, uuid.UUID) ([]models.AgencyClientMembership, error) {
	f.hit("memberships")
	return f.clients, nil
}

func (f *fakeRepo) CampaignStats(_ context.Context, ids []uuid.UUID, _ time.Time) (map[uuid.UUID]*models.AgencyClientCampaigns, error) {
	f.hit("campaigns")
	f.campaign = ids
	out := map[uuid.UUID]*models.AgencyClientCampaigns{}
	for _, id := range ids {
		out[id] = &models.AgencyClientCampaigns{Active: 2, SentToday: 10, Sent7d: 100, Bounced7d: 12, BounceRate7d: 0.12}
	}
	return out, nil
}

func (f *fakeRepo) MailboxStats(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]*models.AgencyClientMailboxes, error) {
	f.hit("mailboxes")
	f.mailbox = ids
	out := map[uuid.UUID]*models.AgencyClientMailboxes{}
	for _, id := range ids {
		out[id] = &models.AgencyClientMailboxes{Total: 3, Disconnected: 1}
	}
	return out, nil
}

func (f *fakeRepo) RepliesWaiting(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]int64, error) {
	f.hit("replies")
	f.replies = ids
	out := map[uuid.UUID]int64{}
	for _, id := range ids {
		out[id] = 4
	}
	return out, nil
}

func (f *fakeRepo) AdvisorCounts(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]*models.AgencyClientAdvisor, error) {
	f.hit("advisor")
	f.advisor = ids
	return map[uuid.UUID]*models.AgencyClientAdvisor{}, nil
}

func (f *fakeRepo) InboxThreads(_ context.Context, q models.AgencyInboxQuery) ([]models.AgencyInboxThread, error) {
	f.hit("inbox")
	f.inbox = &q
	if len(f.threads) > q.Limit+1 {
		return f.threads[:q.Limit+1], nil
	}
	return f.threads, nil
}

type fakeScopes struct{ mailboxes map[uuid.UUID][]uuid.UUID }

func (f fakeScopes) ResolveMemberScope(_ context.Context, orgID, _ uuid.UUID) (*models.ResourceScope, *errx.Error) {
	return &models.ResourceScope{Mailboxes: f.mailboxes[orgID]}, nil
}

type fakeUnibox struct{ thread *models.MailSearchResult }

func (f fakeUnibox) GetByThreadWithin(context.Context, uuid.UUID, uuid.UUID, string, string, string, []uuid.UUID) (*models.MailSearchResult, *errx.Error) {
	return f.thread, nil
}

func (f fakeUnibox) Peek(_ context.Context, _ uuid.UUID, id uuid.UUID) (*models.EmailMessage, *errx.Error) {
	return &models.EmailMessage{ID: id, EmailID: uuid.Nil}, nil
}

type fakeGate struct{ refused map[uuid.UUID]bool }

func (f fakeGate) CanUseUnibox(_ context.Context, orgID uuid.UUID) (bool, *errx.Error) {
	return !f.refused[orgID], nil
}

func role(r models.Role) *string { s := string(r); return &s }

// Four clients: one the caller runs, one where they only read the inbox, one they are not in, one restricted.
type fixture struct {
	full, inboxOnly, outsider, restricted uuid.UUID
	grant                                 uuid.UUID
	repo                                  *fakeRepo
	svc                                   *Service
}

func newFixture(member bool) *fixture {
	f := &fixture{full: uuid.New(), inboxOnly: uuid.New(), outsider: uuid.New(), restricted: uuid.New(), grant: uuid.New()}
	f.repo = &fakeRepo{clients: []models.AgencyClientMembership{
		{OrganizationID: f.full, Name: "Acme", Role: role(models.RoleAdmin), Permissions: models.RolePermissions[models.RoleAdmin], AccessScope: models.AccessScopeWorkspace},
		{OrganizationID: f.inboxOnly, Name: "Beta", Role: role(models.RoleViewer), Permissions: models.PermAccessUnibox, AccessScope: models.AccessScopeWorkspace},
		{OrganizationID: f.outsider, Name: "Cobalt"},
		{OrganizationID: f.restricted, Name: "Delta", Role: role(models.RoleViewer), Permissions: models.RestrictedReadPermissions, AccessScope: models.AccessScopeRestricted},
	}}
	f.svc = NewService(&fakeAgencies{member: member}, f.repo,
		fakeScopes{mailboxes: map[uuid.UUID][]uuid.UUID{f.restricted: {f.grant}}}, fakeUnibox{}, fakeGate{})
	return f
}

func TestNonMemberIsRefusedBeforeAnyRead(t *testing.T) {
	f := newFixture(false)
	ctx := context.Background()
	if _, xerr := f.svc.Overview(ctx, uuid.New(), uuid.New()); xerr == nil || xerr.Identifier != "agency_not_found" {
		t.Fatalf("overview: want agency_not_found, got %v", xerr)
	}
	if _, xerr := f.svc.Inbox(ctx, uuid.New(), uuid.New(), InboxParams{}); xerr == nil || xerr.Identifier != "agency_not_found" {
		t.Fatalf("inbox: want agency_not_found, got %v", xerr)
	}
	if _, xerr := f.svc.Thread(ctx, uuid.New(), uuid.New(), f.full, "t1", ""); xerr == nil || xerr.Identifier != "agency_not_found" {
		t.Fatalf("thread: want agency_not_found, got %v", xerr)
	}
	if len(f.repo.calls) != 0 {
		t.Fatalf("a non-member reached the repository: %v", f.repo.calls)
	}
}

func TestOverviewQueriesOncePerMetricWithPermittedClientsOnly(t *testing.T) {
	f := newFixture(true)
	o, xerr := f.svc.Overview(context.Background(), uuid.New(), uuid.New())
	if xerr != nil {
		t.Fatal(xerr)
	}
	for _, name := range []string{"campaigns", "mailboxes", "replies", "advisor"} {
		if f.repo.calls[name] != 1 {
			t.Fatalf("%s queried %d times, want once for every client together", name, f.repo.calls[name])
		}
	}
	if !slices.Equal(f.repo.campaign, []uuid.UUID{f.full}) || !slices.Equal(f.repo.mailbox, []uuid.UUID{f.full}) {
		t.Fatalf("campaign metrics read %v / %v, want only the client with view_campaigns", f.repo.campaign, f.repo.mailbox)
	}
	if !slices.Equal(f.repo.replies, []uuid.UUID{f.full, f.inboxOnly}) {
		t.Fatalf("replies read %v, want the two clients with access_unibox across the workspace", f.repo.replies)
	}
	if !slices.Equal(f.repo.advisor, []uuid.UUID{f.full}) {
		t.Fatalf("advisor read %v", f.repo.advisor)
	}

	by := map[uuid.UUID]models.AgencyOverviewClient{}
	for _, c := range o.Clients {
		by[c.OrganizationID] = c
	}
	if c := by[f.full]; c.Campaigns == nil || c.Mailboxes == nil || c.Inbox == nil || c.Advisor == nil || c.Access != models.AgencyClientAccessFull {
		t.Fatalf("full client missing sections: %+v", c)
	}
	if c := by[f.inboxOnly]; c.Campaigns != nil || c.Mailboxes != nil || c.Advisor != nil || c.Inbox == nil {
		t.Fatalf("inbox-only client shows more than its permission: %+v", c)
	}
	for _, id := range []uuid.UUID{f.outsider, f.restricted} {
		if c := by[id]; c.Campaigns != nil || c.Mailboxes != nil || c.Inbox != nil || c.Advisor != nil {
			t.Fatalf("client %s shows metrics without a whole-workspace membership: %+v", c.Name, c)
		}
	}
	if by[f.outsider].Access != models.AgencyClientAccessNone || by[f.restricted].Access != models.AgencyClientAccessRestricted {
		t.Fatalf("access labels wrong: %s %s", by[f.outsider].Access, by[f.restricted].Access)
	}
	if o.Totals.RepliesWaiting != 8 || o.Totals.SentToday != 10 {
		t.Fatalf("totals add only visible sections: %+v", o.Totals)
	}
}

func TestOverviewIsCachedBriefly(t *testing.T) {
	f := newFixture(true)
	now := time.Now()
	f.svc.now = func() time.Time { return now }
	agencyID, userID := uuid.New(), uuid.New()
	for range 3 {
		if _, xerr := f.svc.Overview(context.Background(), agencyID, userID); xerr != nil {
			t.Fatal(xerr)
		}
	}
	if f.repo.calls["campaigns"] != 1 {
		t.Fatalf("campaigns queried %d times within the cache window", f.repo.calls["campaigns"])
	}
	now = now.Add(overviewTTL)
	if _, xerr := f.svc.Overview(context.Background(), agencyID, userID); xerr != nil {
		t.Fatal(xerr)
	}
	if f.repo.calls["campaigns"] != 2 {
		t.Fatal("the cache outlived its window")
	}
	// Staff access is checked on every call, cached or not.
	if f.repo.calls["memberships"] != 4 {
		t.Fatalf("memberships read %d times, want every call", f.repo.calls["memberships"])
	}
}

func TestInboxScopeComesFromMemberships(t *testing.T) {
	f := newFixture(true)
	if _, xerr := f.svc.Inbox(context.Background(), uuid.New(), uuid.New(), InboxParams{}); xerr != nil {
		t.Fatal(xerr)
	}
	q := f.repo.inbox
	if !slices.Equal(q.OrgIDs, []uuid.UUID{f.full, f.inboxOnly}) {
		t.Fatalf("whole workspaces %v", q.OrgIDs)
	}
	if !slices.Equal(q.ScopedOrgIDs, []uuid.UUID{f.restricted}) || !slices.Equal(q.ScopedMailboxes, []uuid.UUID{f.grant}) {
		t.Fatalf("restricted scope %v %v", q.ScopedOrgIDs, q.ScopedMailboxes)
	}

	// A filter naming a workspace outside the agency or the caller's reach selects nothing from it.
	foreign := uuid.New()
	if _, xerr := f.svc.Inbox(context.Background(), uuid.New(), uuid.New(), InboxParams{Clients: []uuid.UUID{foreign, f.outsider, f.inboxOnly}}); xerr != nil {
		t.Fatal(xerr)
	}
	q = f.repo.inbox
	if !slices.Equal(q.OrgIDs, []uuid.UUID{f.inboxOnly}) || len(q.ScopedOrgIDs) != 0 {
		t.Fatalf("filtered scope %v %v", q.OrgIDs, q.ScopedOrgIDs)
	}
}

func TestInboxSkipsClientsWithoutUniboxPlan(t *testing.T) {
	f := newFixture(true)
	f.svc.gate = fakeGate{refused: map[uuid.UUID]bool{f.inboxOnly: true}}
	if _, xerr := f.svc.Inbox(context.Background(), uuid.New(), uuid.New(), InboxParams{}); xerr != nil {
		t.Fatal(xerr)
	}
	if !slices.Equal(f.repo.inbox.OrgIDs, []uuid.UUID{f.full}) {
		t.Fatalf("got %v", f.repo.inbox.OrgIDs)
	}
}

func TestInboxCursorPagination(t *testing.T) {
	f := newFixture(true)
	base := time.Date(2026, 11, 8, 12, 0, 0, 123456000, time.UTC)
	for i := range 5 {
		f.repo.threads = append(f.repo.threads, models.AgencyInboxThread{ID: uuid.New(), InternalDate: base.Add(-time.Duration(i) * time.Minute)})
	}
	page, xerr := f.svc.Inbox(context.Background(), uuid.New(), uuid.New(), InboxParams{Limit: "2"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if len(page.Data) != 2 || !page.Pagination.HasMore || page.Pagination.NextCursor == nil {
		t.Fatalf("first page %+v", page.Pagination)
	}
	if f.repo.inbox.Limit != 2 || !f.repo.inbox.BeforeAt.IsZero() {
		t.Fatalf("first query %+v", f.repo.inbox)
	}
	if _, xerr := f.svc.Inbox(context.Background(), uuid.New(), uuid.New(), InboxParams{Limit: "2", Cursor: *page.Pagination.NextCursor}); xerr != nil {
		t.Fatal(xerr)
	}
	last := page.Data[1]
	if !f.repo.inbox.BeforeAt.Equal(last.InternalDate) || f.repo.inbox.BeforeID != last.ID {
		t.Fatalf("cursor resumed at %v/%s, want the last row %v/%s", f.repo.inbox.BeforeAt, f.repo.inbox.BeforeID, last.InternalDate, last.ID)
	}

	f.repo.threads = f.repo.threads[:2]
	page, _ = f.svc.Inbox(context.Background(), uuid.New(), uuid.New(), InboxParams{Limit: "2"})
	if page.Pagination.HasMore || page.Pagination.NextCursor != nil {
		t.Fatal("a final page still offers a next cursor")
	}
}

func TestInboxRejectsBadCursorAndLimit(t *testing.T) {
	f := newFixture(true)
	ctx := context.Background()
	if _, xerr := f.svc.Inbox(ctx, uuid.New(), uuid.New(), InboxParams{Cursor: "nope"}); xerr == nil || xerr.Identifier != "invalid_cursor" {
		t.Fatalf("want invalid_cursor, got %v", xerr)
	}
	for _, l := range []string{"0", "101", "x"} {
		if _, xerr := f.svc.Inbox(ctx, uuid.New(), uuid.New(), InboxParams{Limit: l}); xerr == nil || xerr.Identifier != "invalid_limit" {
			t.Fatalf("limit %q: want invalid_limit, got %v", l, xerr)
		}
	}
}

func TestThreadRefusesOutsideClientsAndPermissions(t *testing.T) {
	f := newFixture(true)
	f.svc.unibox = fakeUnibox{thread: &models.MailSearchResult{Data: []models.EmailMessageStoreDataPreview{
		{ID: uuid.New(), EmailID: f.grant}, {ID: uuid.New(), EmailID: uuid.New()},
	}}}
	ctx := context.Background()
	if _, xerr := f.svc.Thread(ctx, uuid.New(), uuid.New(), uuid.New(), "t", ""); xerr == nil || xerr.Identifier != "agency_client_not_found" {
		t.Fatalf("foreign workspace: %v", xerr)
	}
	if _, xerr := f.svc.Thread(ctx, uuid.New(), uuid.New(), f.outsider, "t", ""); xerr == nil || xerr.Identifier != "agency_client_inbox_forbidden" {
		t.Fatalf("client the caller is not in: %v", xerr)
	}
	res, xerr := f.svc.Thread(ctx, uuid.New(), uuid.New(), f.restricted, "t", "")
	if xerr != nil {
		t.Fatal(xerr)
	}
	if len(res.Data) != 1 || res.Data[0].EmailID != f.grant {
		t.Fatalf("restricted reader saw mailboxes outside the grant: %+v", res.Data)
	}
	if _, xerr := f.svc.Message(ctx, uuid.New(), uuid.New(), f.restricted, uuid.New()); xerr == nil {
		t.Fatal("a message outside the grant was readable")
	}
}

func TestAttentionPutsCriticalFirst(t *testing.T) {
	limit := 100
	id := uuid.New()
	usage := &models.AgencyUsage{Total: 1000, Used: 950, Remaining: 50, Projected: 1200}
	got := attention(usage, []models.AgencyOverviewClient{{
		OrganizationID: id, Name: "Acme", PoolUsed: 95, MonthlySendLimit: &limit,
		Campaigns: &models.AgencyClientCampaigns{Sent7d: 100, Bounced7d: 6, BounceRate7d: 0.06, PausedByBilling: 1},
		Mailboxes: &models.AgencyClientMailboxes{Disconnected: 1, Quarantined: 1},
	}})
	var kinds []models.AgencyAttentionKind
	for _, a := range got {
		kinds = append(kinds, a.Kind)
	}
	want := []models.AgencyAttentionKind{
		models.AttentionMailboxDisconnected, models.AttentionMailboxHeld, models.AttentionCampaignsPausedBilling,
		models.AttentionPoolLow, models.AttentionBounceSpike, models.AttentionClientCapNear,
	}
	if !slices.Equal(kinds, want) {
		t.Fatalf("got %v, want %v", kinds, want)
	}
	if got[0].OrganizationID == nil || *got[0].OrganizationID != id || got[3].OrganizationID != nil {
		t.Fatal("client items name their workspace and the pool item names none")
	}
	if a := attention(&models.AgencyUsage{Unlimited: true}, nil); len(a) != 0 {
		t.Fatalf("an unlimited pool raised %v", a)
	}
}

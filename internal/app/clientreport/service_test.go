package clientreport

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/analytics"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// memRepo keeps reports in memory with the same tenant and token rules as the SQL.
type memRepo struct {
	repository.ClientReportRepository
	reports map[uuid.UUID]*memReport
	// owned is every resource id per organization.
	owned map[uuid.UUID]map[uuid.UUID]bool
	// folderCampaigns is what each folder holds.
	folderCampaigns map[uuid.UUID][]uuid.UUID
}

type memReport struct {
	rep     models.ClientReport
	hash    []byte
	expires time.Time
}

func newMemRepo() *memRepo {
	return &memRepo{reports: map[uuid.UUID]*memReport{}, owned: map[uuid.UUID]map[uuid.UUID]bool{}, folderCampaigns: map[uuid.UUID][]uuid.UUID{}}
}

func (m *memRepo) own(org uuid.UUID, ids ...uuid.UUID) {
	if m.owned[org] == nil {
		m.owned[org] = map[uuid.UUID]bool{}
	}
	for _, id := range ids {
		m.owned[org][id] = true
	}
}

func (m *memRepo) Count(_ context.Context, org uuid.UUID) (int, error) {
	n := 0
	for _, r := range m.reports {
		if r.rep.OrganizationID == org {
			n++
		}
	}
	return n, nil
}

func (m *memRepo) Create(_ context.Context, r *models.ClientReport) error {
	r.ID = uuid.New()
	m.reports[r.ID] = &memReport{rep: *r}
	return nil
}

func (m *memRepo) Get(_ context.Context, org, id uuid.UUID) (*models.ClientReport, error) {
	r, ok := m.reports[id]
	if !ok || r.rep.OrganizationID != org {
		return nil, nil
	}
	out := r.rep
	return &out, nil
}

func (m *memRepo) Update(_ context.Context, r *models.ClientReport) (bool, error) {
	cur, ok := m.reports[r.ID]
	if !ok || cur.rep.OrganizationID != r.OrganizationID {
		return false, nil
	}
	cur.rep = *r
	return true, nil
}

func (m *memRepo) OwnedCounts(_ context.Context, org uuid.UUID, a *models.MemberAccess) (int, int, int, error) {
	count := func(ids []uuid.UUID) int {
		n := 0
		for _, id := range ids {
			if m.owned[org][id] {
				n++
			}
		}
		return n
	}
	return count(a.FolderIDs), count(a.CampaignIDs), count(a.EmailAccountIDs), nil
}

func (m *memRepo) ResolveScope(_ context.Context, org uuid.UUID, a *models.MemberAccess) (*models.ResourceScope, error) {
	scope := &models.ResourceScope{}
	seen := map[uuid.UUID]bool{}
	add := func(id uuid.UUID) {
		if m.owned[org][id] && !seen[id] {
			seen[id] = true
			scope.Campaigns = append(scope.Campaigns, id)
		}
	}
	for _, id := range a.CampaignIDs {
		add(id)
	}
	for _, f := range a.FolderIDs {
		if !m.owned[org][f] {
			continue
		}
		for _, id := range m.folderCampaigns[f] {
			add(id)
		}
	}
	for _, id := range a.EmailAccountIDs {
		if m.owned[org][id] {
			scope.Mailboxes = append(scope.Mailboxes, id)
		}
	}
	return scope, nil
}

func (m *memRepo) SetLink(_ context.Context, org, id uuid.UUID, hash []byte, hint string, expires time.Time) (bool, error) {
	r, ok := m.reports[id]
	if !ok || r.rep.OrganizationID != org {
		return false, nil
	}
	r.hash, r.expires = hash, expires
	r.rep.Link = &models.ClientReportLink{Hint: hint, CreatedAt: time.Now(), ExpiresAt: &expires}
	return true, nil
}

func (m *memRepo) RevokeLinks(_ context.Context, org, id uuid.UUID) (bool, error) {
	r, ok := m.reports[id]
	if !ok || r.rep.OrganizationID != org {
		return false, nil
	}
	r.hash, r.rep.Link = nil, nil
	return true, nil
}

func (m *memRepo) ResolveToken(_ context.Context, hash []byte, now time.Time) (*repository.ClientReportToken, error) {
	for _, r := range m.reports {
		if r.hash != nil && bytes.Equal(r.hash, hash) && now.Before(r.expires) {
			out := r.rep
			return &repository.ClientReportToken{Report: &out}, nil
		}
	}
	return nil, nil
}

// spyAnalytics records the query each report view asks for.
type spyAnalytics struct {
	analytics.AnalyticsService
	org     uuid.UUID
	queries []models.ClientReportQuery
}

func (s *spyAnalytics) GetClientReportAnalytics(_ context.Context, org uuid.UUID, q models.ClientReportQuery) (*models.ClientReportAnalytics, *errx.Error) {
	s.org = org
	s.queries = append(s.queries, q)
	return &models.ClientReportAnalytics{Health: &models.AccountHealthSummary{}, Warmup: &models.WarmupSummary{}}, nil
}

type fixedHost string

func (h fixedHost) PortalHost(context.Context, uuid.UUID) string { return string(h) }

func newTestService(repo *memRepo, spy *spyAnalytics, now time.Time) *service {
	s := New(Deps{Repo: repo, Analytics: spy, Hosts: fixedHost("portal.agency.example.com"), LinkKey: EmailLinkKey(strings.Repeat("k", 32))}).(*service)
	s.now = func() time.Time { return now }
	return s
}

func strPtr(s string) *string { return &s }

func TestReportReadsOnlyItsScopeInsideItsOrganization(t *testing.T) {
	repo, spy := newMemRepo(), &spyAnalytics{}
	s := newTestService(repo, spy, time.Now())
	org, other := uuid.New(), uuid.New()
	inScope, sibling, foreign := uuid.New(), uuid.New(), uuid.New()
	folder, folderCampaign := uuid.New(), uuid.New()
	mailbox := uuid.New()
	repo.own(org, inScope, sibling, folder, folderCampaign, mailbox)
	repo.own(other, foreign)
	repo.folderCampaigns[folder] = []uuid.UUID{folderCampaign}

	// A campaign of another workspace is refused at write time.
	_, xerr := s.Create(context.Background(), org, uuid.New(), &models.ClientReportInput{Name: strPtr("Acme"),
		Access: &models.MemberAccess{Scope: models.AccessScopeRestricted, CampaignIDs: []uuid.UUID{inScope, foreign}}})
	if xerr == nil || xerr.ResponseCode() != CodeInvalidScope {
		t.Fatalf("foreign campaign accepted: %v", xerr)
	}

	rep, xerr := s.Create(context.Background(), org, uuid.New(), &models.ClientReportInput{Name: strPtr("Acme"),
		Access: &models.MemberAccess{Scope: models.AccessScopeRestricted, CampaignIDs: []uuid.UUID{inScope},
			FolderIDs: []uuid.UUID{folder}, EmailAccountIDs: []uuid.UUID{mailbox}}})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if _, xerr := s.Preview(context.Background(), org, rep.ID, ""); xerr != nil {
		t.Fatal(xerr)
	}
	q := spy.queries[len(spy.queries)-1]
	if spy.org != org {
		t.Fatalf("read organization %s, want %s", spy.org, org)
	}
	if q.Campaigns == nil {
		t.Fatal("a restricted report read the whole workspace")
	}
	got := map[uuid.UUID]bool{}
	for _, id := range q.Campaigns.CampaignIDs {
		got[id] = true
	}
	if len(got) != 2 || !got[inScope] || !got[folderCampaign] || got[sibling] || got[foreign] {
		t.Fatalf("campaign scope = %v, want only %s and %s", q.Campaigns.CampaignIDs, inScope, folderCampaign)
	}
	if len(q.Mailboxes) != 1 || q.Mailboxes[0] != mailbox {
		t.Fatalf("mailbox scope = %v", q.Mailboxes)
	}

	// Another workspace cannot read, preview or link the report.
	if _, xerr := s.Preview(context.Background(), other, rep.ID, ""); xerr == nil || xerr.ResponseCode() != CodeNotFound {
		t.Fatalf("another workspace previewed the report: %v", xerr)
	}
	if _, xerr := s.CreateLink(context.Background(), other, rep.ID, nil); xerr == nil {
		t.Fatal("another workspace minted a link")
	}
}

func TestEmptiedScopeReadsNothingRatherThanEverything(t *testing.T) {
	repo, spy := newMemRepo(), &spyAnalytics{}
	s := newTestService(repo, spy, time.Now())
	org, campaign := uuid.New(), uuid.New()
	repo.own(org, campaign)
	rep, xerr := s.Create(context.Background(), org, uuid.Nil, &models.ClientReportInput{Name: strPtr("Acme"),
		Access: &models.MemberAccess{Scope: models.AccessScopeRestricted, CampaignIDs: []uuid.UUID{campaign}}})
	if xerr != nil {
		t.Fatal(xerr)
	}
	// The campaign moves out of the workspace's reach (deleted or moved away) after the report was saved.
	delete(repo.owned[org], campaign)
	if _, xerr := s.Preview(context.Background(), org, rep.ID, ""); xerr != nil {
		t.Fatal(xerr)
	}
	q := spy.queries[len(spy.queries)-1]
	if q.Campaigns == nil || len(q.Campaigns.CampaignIDs) != 0 {
		t.Fatalf("campaign scope = %+v, want an empty set", q.Campaigns)
	}
	if q.Mailboxes == nil || len(q.Mailboxes) != 0 {
		t.Fatalf("mailbox scope = %v, want an empty, non-nil set", q.Mailboxes)
	}

	if _, xerr := s.Create(context.Background(), org, uuid.Nil, &models.ClientReportInput{Name: strPtr("Empty"),
		Access: &models.MemberAccess{Scope: models.AccessScopeRestricted}}); xerr == nil || xerr.ResponseCode() != CodeInvalidScope {
		t.Fatalf("a restricted report with no grants was accepted: %v", xerr)
	}
}

func TestWorkspaceReportReadsTheWorkspace(t *testing.T) {
	repo, spy := newMemRepo(), &spyAnalytics{}
	s := newTestService(repo, spy, time.Now())
	org := uuid.New()
	rep, xerr := s.Create(context.Background(), org, uuid.Nil, &models.ClientReportInput{Name: strPtr("Everything")})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if _, xerr := s.Preview(context.Background(), org, rep.ID, models.ClientReportLast7Days); xerr != nil {
		t.Fatal(xerr)
	}
	q := spy.queries[len(spy.queries)-1]
	if q.Campaigns != nil || q.Mailboxes != nil {
		t.Fatalf("workspace report narrowed to %+v / %v", q.Campaigns, q.Mailboxes)
	}
}

func TestShareLinkLifecycle(t *testing.T) {
	repo, spy := newMemRepo(), &spyAnalytics{}
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	s := newTestService(repo, spy, now)
	org := uuid.New()
	rep, xerr := s.Create(context.Background(), org, uuid.Nil, &models.ClientReportInput{Name: strPtr("Acme")})
	if xerr != nil {
		t.Fatal(xerr)
	}

	minted, xerr := s.CreateLink(context.Background(), org, rep.ID, nil)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if !validTokenShape(minted.Token) || !strings.HasSuffix(minted.URL, "/r/"+minted.Token) || !strings.HasPrefix(minted.URL, "https://portal.agency.example.com/") {
		t.Fatalf("minted %q at %q", minted.Token, minted.URL)
	}
	if stored := repo.reports[rep.ID].hash; bytes.Contains(stored, []byte(minted.Token)) || !bytes.Equal(stored, hashToken(minted.Token)) {
		t.Fatal("the stored value is not the token's sha256")
	}
	if minted.Link.Hint != minted.Token[:hintLength] {
		t.Fatalf("hint %q", minted.Link.Hint)
	}
	if want := now.Add(models.ClientReportLinkDefaultDays * 24 * time.Hour); minted.Link.ExpiresAt == nil || !minted.Link.ExpiresAt.Equal(want) {
		t.Fatalf("expires %v, want %v", minted.Link.ExpiresAt, want)
	}
	pub, xerr := s.Public(context.Background(), minted.Token, "")
	if xerr != nil || pub == nil || pub.OrganizationID != org || pub.View.Name != "Acme" {
		t.Fatalf("the minted link did not open its report: %+v %v", pub, xerr)
	}

	// Rotating replaces the link: the old token opens nothing.
	rotated, xerr := s.CreateLink(context.Background(), org, rep.ID, nil)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if rotated.Token == minted.Token {
		t.Fatal("rotation reused the token")
	}
	if pub, _ := s.Public(context.Background(), minted.Token, ""); pub != nil {
		t.Fatal("the rotated-out token still opens the report")
	}
	if pub, _ := s.Public(context.Background(), rotated.Token, ""); pub == nil {
		t.Fatal("the new token does not open the report")
	}

	// Expiry closes it.
	s.now = func() time.Time { return now.Add(models.ClientReportLinkDefaultDays*24*time.Hour + time.Second) }
	if pub, _ := s.Public(context.Background(), rotated.Token, ""); pub != nil {
		t.Fatal("an expired link still opens the report")
	}
	s.now = func() time.Time { return now }

	// Revoking closes it.
	if _, xerr := s.RevokeLinks(context.Background(), org, rep.ID); xerr != nil {
		t.Fatal(xerr)
	}
	if pub, _ := s.Public(context.Background(), rotated.Token, ""); pub != nil {
		t.Fatal("a revoked link still opens the report")
	}

	// Malformed tokens never reach the store, and a bad lifetime is refused.
	for _, bad := range []string{"", "short", strings.Repeat("a", 44), strings.Repeat("*", 43)} {
		if pub, xerr := s.Public(context.Background(), bad, ""); pub != nil || xerr != nil {
			t.Fatalf("token %q: %+v %v", bad, pub, xerr)
		}
	}
	zero := 0
	if _, xerr := s.CreateLink(context.Background(), org, rep.ID, &models.CreateClientReportLinkRequest{ExpiresInDays: &zero}); xerr == nil {
		t.Fatal("a zero-day link was accepted")
	}
}

func TestEmailTokenIsStableAndKeyed(t *testing.T) {
	id := uuid.New()
	a, b := EmailLinkKey(strings.Repeat("a", 32)), EmailLinkKey(strings.Repeat("b", 32))
	if emailToken(a, id) != emailToken(a, id) {
		t.Fatal("the same delivery produced two links")
	}
	if emailToken(a, id) == emailToken(b, id) || emailToken(a, id) == emailToken(a, uuid.New()) {
		t.Fatal("delivery links collide across keys or deliveries")
	}
	if !validTokenShape(emailToken(a, id)) {
		t.Fatal("a delivery link does not have a link token's shape")
	}
}

func TestViewLeavesOutWhatIsOff(t *testing.T) {
	positive := 3
	rep := &models.ClientReport{Name: "Acme", Sections: models.ClientReportSections{Overview: true, Campaigns: true, Replies: true}}
	a := &models.ClientReportAnalytics{
		Overall:   models.DashboardOverallStats{TotalEmailsSent: 10, TotalReplies: 2},
		Positive:  positive,
		Campaigns: []models.ClientReportCampaign{{Name: "C", Sent: 10, Positive: &positive}},
		Replies:   []models.ClientReportReply{{Email: "lead@example.com", Snippet: "private words"}},
		Health:    &models.AccountHealthSummary{TotalAccounts: 2},
	}
	from := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 11, 8, 15, 0, 0, 0, time.UTC)
	v := buildView(rep, models.ClientReportLast7Days, from, to, to, a)
	if v.Overview == nil || v.Overview.Positive != nil || v.Campaigns[0].Positive != nil {
		t.Fatal("positive replies shown while the section is off")
	}
	if v.Replies[0].Snippet != "" {
		t.Fatal("a reply snippet shown while snippets are off")
	}
	if v.Mailboxes != nil || v.Daily != nil {
		t.Fatal("an off section was rendered")
	}
	rep.Sections.DailyChart = true
	v = buildView(rep, models.ClientReportLast7Days, from, to, to, a)
	if len(v.Daily) != 7 || v.Daily[0].Date != "2026-11-02" || v.Daily[6].Date != "2026-11-08" {
		t.Fatalf("daily series = %+v", v.Daily)
	}
}

// A report link lives on the portal service: the shared portal address without an agency domain, and nowhere without PORTAL_DOMAIN.
func TestShareLinksAreBuiltOnThePortalAddressOnly(t *testing.T) {
	t.Setenv("FORMS_DOMAIN", "forms.example.com")
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	s := newTestService(newMemRepo(), &spyAnalytics{}, now)
	s.Hosts = nil
	org := uuid.New()
	rep, xerr := s.Create(context.Background(), org, uuid.Nil, &models.ClientReportInput{Name: strPtr("Acme")})
	if xerr != nil {
		t.Fatal(xerr)
	}

	t.Setenv("PORTAL_DOMAIN", "")
	if _, xerr := s.CreateLink(context.Background(), org, rep.ID, nil); xerr == nil || xerr.ResponseCode() != CodeLinksUnavailable {
		t.Fatalf("a link was minted without a portal service: %v", xerr)
	}
	t.Setenv("PORTAL_DOMAIN", "portal.example.com")
	minted, xerr := s.CreateLink(context.Background(), org, rep.ID, nil)
	if xerr != nil || minted.URL != "https://portal.example.com/r/"+minted.Token {
		t.Fatalf("shared portal link = %+v, %v", minted, xerr)
	}
}

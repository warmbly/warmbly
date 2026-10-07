package sendingdomain

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// fakeCloud is Warmbly Cloud as the linked instance sees it.
type fakeCloud struct {
	releasedInstances []uuid.UUID
	offer             *models.PoolLinkRedirectOffer
	rows              map[string]*models.DomainRedirect
	down              bool
	unlinked          bool
	deletes           []string
	putErr            *errx.Error
}

func newFakeCloud() *fakeCloud {
	return &fakeCloud{offer: &models.PoolLinkRedirectOffer{Available: true, Host: "t.warmbly.cloud", Limit: 200}, rows: map[string]*models.DomainRedirect{}}
}

func (f *fakeCloud) fail() *errx.Error {
	if f.unlinked {
		return errx.NewWithIdentifier(errx.Conflict, "cloud_link_not_connected", "not connected")
	}
	if f.down {
		return errx.NewWithIdentifier(errx.ServiceUnavailable, "cloud_link_unreachable", "unreachable")
	}
	return nil
}

func (f *fakeCloud) RedirectOffer(context.Context, uuid.UUID) (*models.PoolLinkRedirectOffer, bool) {
	if f.unlinked {
		return nil, false
	}
	if f.down {
		return nil, true
	}
	return f.offer, true
}
func (f *fakeCloud) PutRedirect(_ context.Context, _ uuid.UUID, domain string, in models.DomainRedirectRequest) (*models.DomainRedirect, *errx.Error) {
	if xerr := f.fail(); xerr != nil {
		return nil, xerr
	}
	if f.putErr != nil {
		return nil, f.putErr
	}
	if old, ok := f.rows[domain]; ok {
		old.TargetURL, old.IncludeWWW = in.TargetURL, *in.IncludeWWW
		return old, nil
	}
	id := uuid.Nil
	r := &models.DomainRedirect{CloudLinkInstanceID: &id, Domain: domain, TargetURL: in.TargetURL, IncludeWWW: *in.IncludeWWW, ServeHost: "t.warmbly.cloud", CreatedAt: time.Now(),
		Records: []models.DNSRecord{{Purpose: "root", Type: "A", Name: domain, Value: "198.51.100.7"}}, LastError: "The TXT record is not there yet."}
	f.rows[domain] = r
	return r, nil
}
func (f *fakeCloud) ListRedirects(context.Context) ([]models.DomainRedirect, *errx.Error) {
	if xerr := f.fail(); xerr != nil {
		return nil, xerr
	}
	var out []models.DomainRedirect
	for _, r := range f.rows {
		out = append(out, *r)
	}
	return out, nil
}
func (f *fakeCloud) GetRedirect(_ context.Context, _ uuid.UUID, domain string) (*models.DomainRedirect, *errx.Error) {
	if xerr := f.fail(); xerr != nil {
		return nil, xerr
	}
	if r, ok := f.rows[domain]; ok {
		return r, nil
	}
	return nil, errx.NewWithIdentifier(errx.NotFound, ErrIDRemoteNotFound, "none")
}
func (f *fakeCloud) VerifyRedirect(ctx context.Context, orgID uuid.UUID, domain string) (*models.DomainRedirect, *errx.Error) {
	return f.GetRedirect(ctx, orgID, domain)
}
func (f *fakeCloud) DeleteRedirect(_ context.Context, _ uuid.UUID, domain string) *errx.Error {
	if xerr := f.fail(); xerr != nil {
		return xerr
	}
	f.deletes = append(f.deletes, domain)
	delete(f.rows, domain)
	return nil
}

func TestCloudServedRedirectMirrorsCloud(t *testing.T) {
	s, repo := newTest(baseDNS())
	cloud := newFakeCloud()
	ctx := context.Background()
	org, user := uuid.New(), uuid.New()

	// Unlinked, Cloud is not a choice.
	if _, xerr := s.SetRedirect(ctx, org, user, "acme.io", RedirectInput{TargetURL: "acme.com", ServedBy: models.RedirectServedByCloud}); xerr == nil || xerr.Identifier != ErrIDCloudUnavailable {
		t.Fatalf("served from Cloud with no link: %v", xerr)
	}
	s.WireCloud(cloud)

	r, xerr := s.SetRedirect(ctx, org, user, "acme.io", RedirectInput{TargetURL: "acme.com", ServedBy: models.RedirectServedByCloud})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if r.ServedBy != models.RedirectServedByCloud || r.ServeHost != "t.warmbly.cloud" || len(r.Records) != 1 || r.Records[0].Value != "198.51.100.7" || r.Verified {
		t.Fatalf("the row does not show Cloud's records: %+v", r)
	}
	if reach := s.reach.(*fakeReach); reach.probes != 0 {
		t.Fatal("this instance probed a domain Cloud serves")
	}

	// Cloud verifying (and finding it reachable) is what makes the row live here.
	cloud.rows["acme.io"].Verified, cloud.rows["acme.io"].LastError = true, ""
	cloud.rows["acme.io"].Reach = &models.RedirectReach{Status: models.RedirectReachOK}
	if r, _ = s.VerifyRedirect(ctx, org, "acme.io"); !r.Verified || r.Reach == nil || r.Reach.Status != models.RedirectReachOK {
		t.Fatalf("Cloud's verdict was not mirrored: %+v", r)
	}

	// Cloud being unreachable changes nothing; the link ending stops the row.
	cloud.down = true
	if r, xerr = s.check(ctx, repo.rows[r.ID], false); xerr != nil || !r.Verified {
		t.Fatalf("an unreachable Cloud took the redirect down: %v %+v", xerr, r)
	}
	cloud.down = false

	// A row Cloud lost is put back rather than left dark.
	delete(cloud.rows, "acme.io")
	if _, xerr = s.check(ctx, repo.rows[r.ID], false); xerr != nil || cloud.rows["acme.io"] == nil {
		t.Fatalf("a lost row was not put back on Cloud: %v", xerr)
	}

	// Moving back releases Cloud and serves it here.
	if r, xerr = s.SetRedirect(ctx, org, user, "acme.io", RedirectInput{TargetURL: "acme.com", ServedBy: models.RedirectServedByInstance}); xerr != nil {
		t.Fatal(xerr)
	}
	if r.ServedBy != models.RedirectServedByInstance || len(cloud.deletes) != 1 || r.ServeHost != "track.warmbly.test" {
		t.Fatalf("moving back = %+v, Cloud deletes %v", r, cloud.deletes)
	}

	// Removal reaches Cloud before the row goes, and refuses when Cloud cannot be told.
	if _, xerr = s.SetRedirect(ctx, org, user, "acme.io", RedirectInput{TargetURL: "acme.com", ServedBy: models.RedirectServedByCloud}); xerr != nil {
		t.Fatal(xerr)
	}
	cloud.down = true
	if xerr = s.DeleteRedirect(ctx, org, "acme.io"); xerr == nil || xerr.Identifier != ErrIDCloudUnreachable {
		t.Fatalf("removed while Cloud still serves it: %v", xerr)
	}
	cloud.down = false
	if xerr = s.DeleteRedirect(ctx, org, "acme.io"); xerr != nil || cloud.rows["acme.io"] != nil || len(repo.rows) != 0 {
		t.Fatalf("removal = %v, Cloud rows %v, local rows %d", xerr, cloud.rows, len(repo.rows))
	}
}

func TestCloudServedRedirectStopsWhenTheLinkEnds(t *testing.T) {
	s, repo := newTest(baseDNS())
	cloud := newFakeCloud()
	s.WireCloud(cloud)
	ctx := context.Background()
	org := uuid.New()
	if _, xerr := s.SetRedirect(ctx, org, uuid.New(), "acme.io", RedirectInput{TargetURL: "acme.com", ServedBy: models.RedirectServedByCloud}); xerr != nil {
		t.Fatal(xerr)
	}
	cloud.rows["acme.io"].Verified = true
	r, _ := s.VerifyRedirect(ctx, org, "acme.io")
	if !r.Verified {
		t.Fatal("not live")
	}
	s.MarkCloudUnlinked(ctx, uuid.Nil)
	if row := repo.rows[r.ID]; row.Verified || row.LastError != unlinkedMessage {
		t.Fatalf("still live after the link ended: %+v", row)
	}
	cloud.unlinked = true
	if r, _ = s.check(ctx, repo.rows[r.ID], false); r.Verified {
		t.Fatal("a sweep brought an unlinked redirect back")
	}
}

func TestAnotherWorkspaceCannotTakeACloudServedDomain(t *testing.T) {
	s, _ := newTest(baseDNS())
	s.WireCloud(newFakeCloud())
	ctx := context.Background()
	if _, xerr := s.SetRedirect(ctx, uuid.New(), uuid.New(), "acme.io", RedirectInput{TargetURL: "acme.com", ServedBy: models.RedirectServedByCloud}); xerr != nil {
		t.Fatal(xerr)
	}
	if _, xerr := s.SetRedirect(ctx, uuid.New(), uuid.New(), "acme.io", RedirectInput{TargetURL: "evil.example", ServedBy: models.RedirectServedByCloud}); xerr == nil || xerr.Identifier != ErrIDTaken {
		t.Fatalf("a second workspace pointed Cloud's row elsewhere: %v", xerr)
	}
}

func TestReachIsCheckedOnlyOnceDNSVerifies(t *testing.T) {
	dns := baseDNS()
	s, _ := newTest(dns)
	reach := &fakeReach{verdict: models.RedirectReach{Status: models.RedirectReachNotReaching, Hint: models.RedirectHintNotRouted, Detail: "404"}}
	s.reach = reach
	ctx := context.Background()
	org := uuid.New()
	r, _ := s.SetRedirect(ctx, org, uuid.New(), "acme.io", RedirectInput{TargetURL: "acme.com"})
	if reach.probes != 0 || r.Reach != nil {
		t.Fatalf("probed a domain whose DNS is not in place: %+v", r.Reach)
	}
	dns.ips["acme.io"] = []string{"203.0.113.10"}
	dns.txt["_warmbly.acme.io"] = []string{s.proof.Value(org, "acme.io")}
	r, _ = s.VerifyRedirect(ctx, org, "acme.io")
	if !r.Verified || r.Reach == nil || r.Reach.Hint != models.RedirectHintNotRouted {
		t.Fatalf("verified row carries no reach verdict: %+v", r)
	}
}

func TestLinkedRedirectsProveOwnershipOnCloud(t *testing.T) {
	dns := baseDNS()
	s, repo := newTest(dns)
	ctx := context.Background()
	creator := uuid.New()
	inst := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: uuid.New(), CreatedBy: &creator, RemoteOrganizationID: &creator}

	// No mailbox on Cloud is needed: the instance's workspace sends from there.
	r, xerr := s.LinkedSet(ctx, inst, "frost.io", models.DomainRedirectRequest{TargetURL: "frost.se"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if r.Verified {
		t.Fatal("verified on the instance's word")
	}
	dns.ips["frost.io"] = []string{"203.0.113.10"}
	dns.txt["_warmbly.frost.io"] = []string{s.proof.Value(inst.OrganizationID, "frost.io")}
	if r, _ = s.LinkedVerify(ctx, inst, "frost.io"); !r.Verified {
		t.Fatalf("not verified with both records: %s", r.LastError)
	}

	// The row is the link's: Cloud's own dashboard neither lists nor changes it.
	if list, _ := repo.List(ctx, inst.OrganizationID); len(list) != 0 {
		t.Fatalf("the workspace lists the link's row: %+v", list)
	}
	s.mailboxes = &fakeMailboxes{counts: map[string]int{"frost.io": 1}}
	if _, xerr := s.SetRedirect(ctx, inst.OrganizationID, creator, "frost.io", RedirectInput{TargetURL: "evil.example"}); xerr == nil || xerr.Identifier != ErrIDLinked {
		t.Fatalf("the dashboard changed the link's row: %v", xerr)
	}

	// Another instance cannot read or remove it.
	other := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: inst.OrganizationID}
	if _, xerr := s.LinkedGet(ctx, other, "frost.io"); xerr == nil || xerr.Identifier != ErrIDRemoteNotFound {
		t.Fatalf("another instance read it: %v", xerr)
	}
	if xerr := s.LinkedDelete(ctx, other, "frost.io"); xerr == nil {
		t.Fatal("another instance removed it")
	}
	if _, xerr := s.LinkedSet(ctx, other, "frost.io", models.DomainRedirectRequest{TargetURL: "evil.example"}); xerr == nil || xerr.Identifier != ErrIDTaken {
		t.Fatalf("another instance took it: %v", xerr)
	}
	if xerr := s.LinkedDelete(ctx, inst, "frost.io"); xerr != nil {
		t.Fatal(xerr)
	}

	for _, bad := range []string{"gmail.com", "localhost", "a b.io"} {
		if _, xerr := s.LinkedSet(ctx, inst, bad, models.DomainRedirectRequest{TargetURL: "x.com"}); xerr == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if offer := s.LinkedOffer(ctx, inst.ID); !offer.Available || offer.Host != "track.warmbly.test" || offer.Limit != config.PoolLinkRedirectLimit {
		t.Fatalf("offer = %+v", offer)
	}
}

type countingAuditor struct{ calls []map[string]string }

func (a *countingAuditor) LogAction(_ context.Context, _, _ uuid.UUID, _ models.AuditAction, _ models.AuditEntityType, _ *uuid.UUID, _, _ string, changes, _ map[string]string) {
	a.calls = append(a.calls, changes)
}

func TestTeammatesHearWhenARedirectStartsOrStopsReachingVisitors(t *testing.T) {
	dns := baseDNS()
	s, repo := newTest(dns)
	audit := &countingAuditor{}
	s.WireAuditor(audit)
	reach := &fakeReach{verdict: models.RedirectReach{Status: models.RedirectReachOK}}
	s.reach = reach
	ctx := context.Background()
	org, user := uuid.New(), uuid.New()
	r, _ := s.SetRedirect(ctx, org, user, "acme.io", RedirectInput{TargetURL: "acme.com"})
	repo.rows[r.ID].CreatedBy = &user
	dns.ips["acme.io"] = []string{"203.0.113.10"}
	dns.txt["_warmbly.acme.io"] = []string{s.proof.Value(org, "acme.io")}

	steps := []struct {
		verdict models.RedirectReachStatus
		audits  int
	}{
		{models.RedirectReachOK, 1},          // went live
		{models.RedirectReachUnreachable, 1}, // could not confirm: not news
		{models.RedirectReachNotReaching, 2}, // stopped reaching visitors
		{models.RedirectReachHTTPSError, 2},  // still not reaching
		{models.RedirectReachOK, 3},          // reaches them again
	}
	for _, st := range steps {
		reach.verdict = models.RedirectReach{Status: st.verdict}
		if _, xerr := s.VerifyRedirect(ctx, org, "acme.io"); xerr != nil {
			t.Fatal(xerr)
		}
		if len(audit.calls) != st.audits {
			t.Fatalf("after %s: %d audit entries, want %d (%v)", st.verdict, len(audit.calls), st.audits, audit.calls)
		}
	}
}

func TestASettlingMissOutlastingTheCacheIsAnAnswer(t *testing.T) {
	dns := baseDNS()
	s, repo := newTest(dns)
	reach := &fakeReach{verdict: models.RedirectReach{Status: models.RedirectReachUnreachable, Hint: models.RedirectHintSettling, Detail: "settling"}}
	s.reach = reach
	ctx := context.Background()
	org := uuid.New()
	dns.ips["acme.io"] = []string{"203.0.113.10"}
	dns.txt["_warmbly.acme.io"] = []string{s.proof.Value(org, "acme.io")}
	r, _ := s.SetRedirect(ctx, org, uuid.New(), "acme.io", RedirectInput{TargetURL: "acme.com"})
	if r.Reach == nil || r.Reach.Hint != models.RedirectHintSettling {
		t.Fatalf("a miss right after verifying is not settling: %+v", r.Reach)
	}
	old := time.Now().Add(-10 * time.Minute)
	repo.rows[r.ID].VerifiedAt = &old
	if r, _ = s.VerifyRedirect(ctx, org, "acme.io"); r.Reach == nil || r.Reach.Status != models.RedirectReachNotReaching {
		t.Fatalf("a miss long after verifying is still settling: %+v", r.Reach)
	}
}

func TestAnUnreachableCloudIsNotAMissingLink(t *testing.T) {
	s, _ := newTest(baseDNS())
	cloud := newFakeCloud()
	s.WireCloud(cloud)
	cloud.down = true
	_, xerr := s.SetRedirect(context.Background(), uuid.New(), uuid.New(), "acme.io", RedirectInput{TargetURL: "acme.com", ServedBy: models.RedirectServedByCloud})
	if xerr == nil || xerr.Identifier != ErrIDCloudUnreachable {
		t.Fatalf("a linked instance with Cloud down was told to connect: %v", xerr)
	}
}

func TestMovingAVerifiedRedirectVerifiesItAgainOnTheNewServer(t *testing.T) {
	dns := baseDNS()
	s, _ := newTest(dns)
	cloud := newFakeCloud()
	s.WireCloud(cloud)
	ctx := context.Background()
	org := uuid.New()
	if _, xerr := s.SetRedirect(ctx, org, uuid.New(), "acme.io", RedirectInput{TargetURL: "acme.com", ServedBy: models.RedirectServedByCloud}); xerr != nil {
		t.Fatal(xerr)
	}
	cloud.rows["acme.io"].Verified = true
	if r, _ := s.VerifyRedirect(ctx, org, "acme.io"); !r.Verified {
		t.Fatal("not live on Cloud")
	}
	// DNS here cannot be read at all; Cloud's verdict must not stand in for this instance's.
	dns.fail = true
	r, xerr := s.SetRedirect(ctx, org, uuid.New(), "acme.io", RedirectInput{TargetURL: "acme.com", ServedBy: models.RedirectServedByInstance})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if r.Verified {
		t.Fatal("a redirect moved here kept Cloud's verification")
	}
}

func TestBulkRedirectsGoToCloudWithoutATrackingHostHere(t *testing.T) {
	s, _ := newTest(baseDNS())
	s.target = func() string { return "" }
	s.WireCloud(newFakeCloud())
	ctx := context.Background()
	rows, xerr := s.BulkSetup(ctx, uuid.New(), uuid.New(), BulkInput{Domains: []string{"acme.io"}, RedirectURL: "acme.com", ServedBy: models.RedirectServedByCloud})
	if xerr != nil || len(rows) != 1 || rows[0].Redirect == nil || rows[0].Redirect.Via != "cloud" || rows[0].Redirect.Error != "" {
		t.Fatalf("a Cloud redirect needed a tracking host here: %v %+v", xerr, rows)
	}
	if _, xerr := s.BulkSetup(ctx, uuid.New(), uuid.New(), BulkInput{Domains: []string{"acme.io"}, TrackingLabel: "link"}); xerr == nil || xerr.Identifier != ErrIDNoTracking {
		t.Fatalf("tracking without a tracking host = %v", xerr)
	}
}

func TestARevokedLinkNeverReachesTheDashboardAsA401(t *testing.T) {
	s, _ := newTest(baseDNS())
	cloud := newFakeCloud()
	cloud.putErr = errx.NewWithIdentifier(errx.Unauthorized, "cloud_link_remote", "Warmbly Cloud answered 401")
	s.WireCloud(cloud)
	_, xerr := s.SetRedirect(context.Background(), uuid.New(), uuid.New(), "acme.io", RedirectInput{TargetURL: "acme.com", ServedBy: models.RedirectServedByCloud})
	if xerr == nil || xerr.Code == errx.Unauthorized || xerr.Identifier != ErrIDCloudUnavailable {
		t.Fatalf("a revoked link answered %v", xerr)
	}
}

func TestCloudRefusingALostRowStopsIt(t *testing.T) {
	s, repo := newTest(baseDNS())
	cloud := newFakeCloud()
	s.WireCloud(cloud)
	ctx := context.Background()
	org := uuid.New()
	r, _ := s.SetRedirect(ctx, org, uuid.New(), "acme.io", RedirectInput{TargetURL: "acme.com", ServedBy: models.RedirectServedByCloud})
	cloud.rows["acme.io"].Verified = true
	r, _ = s.VerifyRedirect(ctx, org, "acme.io")
	delete(cloud.rows, "acme.io")
	cloud.putErr = errx.NewWithIdentifier(errx.Conflict, ErrIDLimit, "at the limit")
	if r, _ = s.check(ctx, repo.rows[r.ID], false); r.Verified || r.LastError != "at the limit" {
		t.Fatalf("a row Cloud refused to take back still reads live: %+v", r)
	}
}

func TestARefusedNewLinkedRowDoesNotStayBehind(t *testing.T) {
	dns := baseDNS()
	s, repo := newTest(dns)
	ctx := context.Background()
	inst := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: uuid.New()}
	inst.RemoteOrganizationID = &inst.OrganizationID
	// Another workspace already serves the domain verified, so the new row cannot verify.
	other := &models.DomainRedirect{ID: uuid.New(), OrganizationID: uuid.New(), Domain: "frost.io", TargetURL: "https://x.com", Verified: true, ServedBy: models.RedirectServedByInstance}
	repo.rows[other.ID] = other
	dns.ips["frost.io"] = []string{"203.0.113.10"}
	dns.txt["_warmbly.frost.io"] = []string{s.proof.Value(inst.OrganizationID, "frost.io")}
	if _, xerr := s.LinkedSet(ctx, inst, "frost.io", models.DomainRedirectRequest{TargetURL: "frost.se"}); xerr == nil || xerr.Identifier != ErrIDTaken {
		t.Fatalf("refusal = %v", xerr)
	}
	if n, _ := repo.CountLinked(ctx, inst.ID); n != 0 {
		t.Fatalf("a refused row stayed on Cloud, counted against the instance: %d", n)
	}
}

func TestAWorkspaceSaveCannotLandOnALinkedRow(t *testing.T) {
	s, repo := newTest(baseDNS())
	org := uuid.New()
	inst := uuid.New()
	// The row appears between the ownership read and the write.
	s.redirects = &raceRedirects{memRedirects: repo, plant: &models.DomainRedirect{ID: uuid.New(), OrganizationID: org, Domain: "acme.io",
		TargetURL: "https://frost.se", ServedBy: models.RedirectServedByInstance, LinkedInstanceID: &inst}}
	if _, xerr := s.SetRedirect(context.Background(), org, uuid.New(), "acme.io", RedirectInput{TargetURL: "evil.example"}); xerr == nil || xerr.Identifier != ErrIDLinked {
		t.Fatalf("a racing save = %v", xerr)
	}
}

// raceRedirects plants a row right after the service's ownership read.
type raceRedirects struct {
	*memRedirects
	plant *models.DomainRedirect
}

func (r *raceRedirects) Get(ctx context.Context, org uuid.UUID, domain string) (*models.DomainRedirect, error) {
	got, err := r.memRedirects.Get(ctx, org, domain)
	if r.plant != nil {
		r.rows[r.plant.ID], r.plant = r.plant, nil
	}
	return got, err
}

func TestACloudOutageOnSaveLeavesTheRedirectForTheSweep(t *testing.T) {
	s, repo := newTest(baseDNS())
	cloud := newFakeCloud()
	s.WireCloud(cloud)
	ctx := context.Background()
	org := uuid.New()
	cloud.putErr = errx.NewWithIdentifier(errx.ServiceUnavailable, "cloud_link_unreachable", "timeout")
	r, xerr := s.SetRedirect(ctx, org, uuid.New(), "acme.io", RedirectInput{TargetURL: "acme.com", ServedBy: models.RedirectServedByCloud})
	if xerr != nil || r.ServedBy != models.RedirectServedByCloud || r.Verified || r.LastError != pendingCloudMessage {
		t.Fatalf("an outage on save = %v %+v", xerr, r)
	}
	cloud.putErr = nil
	if _, xerr = s.check(ctx, repo.rows[r.ID], false); xerr != nil || cloud.rows["acme.io"] == nil {
		t.Fatalf("the sweep did not send the pending redirect: %v", xerr)
	}
}

func TestACloudRefusalOnSaveRestoresTheRedirect(t *testing.T) {
	dns := baseDNS()
	s, repo := newTest(dns)
	cloud := newFakeCloud()
	s.WireCloud(cloud)
	ctx := context.Background()
	org, user := uuid.New(), uuid.New()
	cloud.putErr = errx.NewWithIdentifier(errx.Conflict, ErrIDLimit, "at the limit")
	if _, xerr := s.SetRedirect(ctx, org, user, "acme.io", RedirectInput{TargetURL: "acme.com", ServedBy: models.RedirectServedByCloud}); xerr == nil || xerr.Identifier != ErrIDLimit {
		t.Fatalf("refusal = %v", xerr)
	}
	if len(repo.rows) != 0 {
		t.Fatal("a new redirect Cloud refused was kept")
	}
	// An existing redirect served here goes back to exactly that, and re-verifies.
	cloud.putErr = nil
	dns.ips["acme.io"] = []string{"203.0.113.10"}
	dns.txt["_warmbly.acme.io"] = []string{s.proof.Value(org, "acme.io")}
	if r, _ := s.SetRedirect(ctx, org, user, "acme.io", RedirectInput{TargetURL: "acme.com"}); !r.Verified {
		t.Fatal("not live here")
	}
	cloud.putErr = errx.NewWithIdentifier(errx.Conflict, ErrIDLimit, "at the limit")
	_, _ = s.SetRedirect(ctx, org, user, "acme.io", RedirectInput{TargetURL: "acme.com", ServedBy: models.RedirectServedByCloud})
	got, _ := repo.Get(ctx, org, "acme.io")
	if got == nil || got.ServedBy != models.RedirectServedByInstance || !got.Verified {
		t.Fatalf("a refused move did not put the redirect back: %+v", got)
	}
}

func TestTheSweepReleasesCloudRedirectsNothingHereHas(t *testing.T) {
	s, _ := newTest(baseDNS())
	cloud := newFakeCloud()
	s.WireCloud(cloud)
	ctx := context.Background()
	id := uuid.Nil
	old := &models.DomainRedirect{CloudLinkInstanceID: &id, Domain: "gone.io", CreatedAt: time.Now().Add(-time.Hour)}
	young := &models.DomainRedirect{Domain: "saving.io", CreatedAt: time.Now()}
	cloud.rows["gone.io"], cloud.rows["saving.io"] = old, young
	if _, xerr := s.SetRedirect(ctx, uuid.New(), uuid.New(), "acme.io", RedirectInput{TargetURL: "acme.com", ServedBy: models.RedirectServedByCloud}); xerr != nil {
		t.Fatal(xerr)
	}
	cloud.rows["acme.io"].CreatedAt = time.Now().Add(-time.Hour)
	s.reconcileCloud(ctx)
	if cloud.rows["gone.io"] != nil || cloud.rows["saving.io"] == nil || cloud.rows["acme.io"] == nil {
		t.Fatalf("reconcile kept or released the wrong rows: %v", cloud.deletes)
	}
}

func TestCloudHoldingAnOlderTargetIsSentTheCurrentOne(t *testing.T) {
	s, repo := newTest(baseDNS())
	cloud := newFakeCloud()
	s.WireCloud(cloud)
	ctx := context.Background()
	r, _ := s.SetRedirect(ctx, uuid.New(), uuid.New(), "acme.io", RedirectInput{TargetURL: "acme.com", ServedBy: models.RedirectServedByCloud})
	cloud.rows["acme.io"].TargetURL = "https://old.example"
	if _, xerr := s.check(ctx, repo.rows[r.ID], false); xerr != nil || cloud.rows["acme.io"].TargetURL != "https://acme.com" {
		t.Fatalf("Cloud kept the old target: %v %s", xerr, cloud.rows["acme.io"].TargetURL)
	}
}

func TestOnlyADefiniteRefusalStopsACloudRedirect(t *testing.T) {
	s, repo := newTest(baseDNS())
	cloud := newFakeCloud()
	s.WireCloud(cloud)
	ctx := context.Background()
	org := uuid.New()
	r, _ := s.SetRedirect(ctx, org, uuid.New(), "acme.io", RedirectInput{TargetURL: "acme.com", ServedBy: models.RedirectServedByCloud})
	cloud.rows["acme.io"].Verified = true
	r, _ = s.VerifyRedirect(ctx, org, "acme.io")
	delete(cloud.rows, "acme.io")
	cloud.putErr = errx.NewWithIdentifier(errx.Conflict, ErrIDCloudUnavailable, "deploying")
	if r, _ = s.check(ctx, repo.rows[r.ID], false); !r.Verified {
		t.Fatal("a passing Cloud answer took a live redirect down")
	}
}

func TestARefusedUpdateOfALinkedRowPutsItBack(t *testing.T) {
	dns := baseDNS()
	s, repo := newTest(dns)
	ctx := context.Background()
	inst := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: uuid.New()}
	inst.RemoteOrganizationID = &inst.OrganizationID
	if _, xerr := s.LinkedSet(ctx, inst, "frost.io", models.DomainRedirectRequest{TargetURL: "frost.se"}); xerr != nil {
		t.Fatal(xerr)
	}
	// Another workspace verifies the domain first, so this row's next check is refused.
	other := &models.DomainRedirect{ID: uuid.New(), OrganizationID: uuid.New(), Domain: "frost.io", TargetURL: "https://x.com", Verified: true, ServedBy: models.RedirectServedByInstance}
	repo.rows[other.ID] = other
	dns.ips["frost.io"] = []string{"203.0.113.10"}
	dns.txt["_warmbly.frost.io"] = []string{s.proof.Value(inst.OrganizationID, "frost.io")}
	if _, xerr := s.LinkedSet(ctx, inst, "frost.io", models.DomainRedirectRequest{TargetURL: "evil.example"}); xerr == nil {
		t.Fatal("the update was not refused")
	}
	got, _ := repo.GetLinked(ctx, inst.ID, "frost.io")
	if got == nil || got.TargetURL != "https://frost.se" {
		t.Fatalf("a refused update kept its target: %+v", got)
	}
}

func (f *fakeCloud) ReleaseRedirect(ctx context.Context, instanceID uuid.UUID, domain string) *errx.Error {
	f.releasedInstances = append(f.releasedInstances, instanceID)
	return f.DeleteRedirect(ctx, instanceID, domain)
}

func TestLegacyCloudRedirectsCanBeUpdatedButNotCreated(t *testing.T) {
	s, _ := newTest(baseDNS())
	inst := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: uuid.New()}
	in := models.DomainRedirectRequest{TargetURL: "frost.se"}
	if _, xerr := s.LinkedSet(context.Background(), inst, "frost.io", in); xerr == nil || xerr.Identifier != "pool_link_workspace_required" {
		t.Fatalf("new legacy redirect = %v", xerr)
	}
	inst.RemoteOrganizationID = &inst.OrganizationID
	if _, xerr := s.LinkedSet(context.Background(), inst, "frost.io", in); xerr != nil {
		t.Fatal(xerr)
	}
	inst.RemoteOrganizationID = nil
	if _, xerr := s.LinkedSet(context.Background(), inst, "frost.io", in); xerr != nil {
		t.Fatalf("existing legacy redirect stopped working: %v", xerr)
	}
}

func TestSweepReleasesOnlyThePreviousLinksRedirect(t *testing.T) {
	s, repo := newTest(baseDNS())
	cloud := newFakeCloud()
	s.WireCloud(cloud)
	oldID, currentID := uuid.New(), uuid.New()
	domain := "moved.io"
	repo.rows[uuid.New()] = &models.DomainRedirect{Domain: domain, ServedBy: models.RedirectServedByCloud, CloudLinkInstanceID: &currentID}
	cloud.rows[domain] = &models.DomainRedirect{Domain: domain, CloudLinkInstanceID: &oldID, CreatedAt: time.Now().Add(-time.Hour)}
	s.reconcileCloud(context.Background())
	if len(cloud.releasedInstances) != 1 || cloud.releasedInstances[0] != oldID {
		t.Fatal("sweep did not use original link")
	}
}

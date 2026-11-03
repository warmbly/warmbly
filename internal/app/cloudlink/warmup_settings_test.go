package cloudlink

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// fakeCloud applies warmup settings the way the cloud does: zero fields are skipped
// and times come back as stored, with seconds.
type fakeCloud struct {
	t        *testing.T
	remote   uuid.UUID
	settings models.PoolLinkWarmupSettings
	refuse   map[string]bool
	omit     bool
	patches  []models.PoolLinkMailboxPatch
	posts    []models.PoolLinkEnrollRequest
}

func (c *fakeCloud) apply(w *models.PoolLinkWarmupSettings) {
	if w == nil {
		return
	}
	set := func(name string, v int, dst *int) {
		if v > 0 && !c.refuse[name] {
			*dst = v
		}
	}
	set("base", w.Base, &c.settings.Base)
	set("max", w.Max, &c.settings.Max)
	set("increase", w.Increase, &c.settings.Increase)
	set("reply_rate", w.ReplyRate, &c.settings.ReplyRate)
	set("days", w.Days, &c.settings.Days)
	if w.StartTime != "" {
		c.settings.StartTime = w.StartTime + ":00"
	}
	if w.EndTime != "" {
		c.settings.EndTime = w.EndTime + ":00"
	}
	if w.Timezone != "" {
		c.settings.Timezone = w.Timezone
	}
}

func (c *fakeCloud) state() models.PoolLinkMailboxState {
	st := models.PoolLinkMailboxState{RemoteID: c.remote, Status: "active"}
	if !c.omit {
		st.Settings = c.settings
	}
	return st
}

func (c *fakeCloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	list := "/v1/pool-link/instance/mailboxes"
	switch {
	case r.Method == http.MethodGet && r.URL.Path == list:
		_ = json.NewEncoder(w).Encode([]models.PoolLinkMailboxState{c.state()})
	case r.Method == http.MethodPost && r.URL.Path == list:
		var req models.PoolLinkEnrollRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		c.posts = append(c.posts, req)
		c.apply(&req.Warmup)
		_ = json.NewEncoder(w).Encode(c.state())
	case r.Method == http.MethodPatch && r.URL.Path == list+"/"+c.remote.String():
		var patch models.PoolLinkMailboxPatch
		_ = json.NewDecoder(r.Body).Decode(&patch)
		c.patches = append(c.patches, patch)
		c.apply(patch.Warmup)
		_ = json.NewEncoder(w).Encode(c.state())
	default:
		c.t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

type mirrorRepo struct {
	*enrollmentFaultRepo
	mirrored []map[uuid.UUID]models.PoolLinkWarmupSettings
}

func (r *mirrorRepo) MirrorCloudWarmupSettings(_ context.Context, _ time.Time, settings map[uuid.UUID]models.PoolLinkWarmupSettings) ([]uuid.UUID, error) {
	r.mirrored = append(r.mirrored, settings)
	var ids []uuid.UUID
	for id := range settings {
		ids = append(ids, id)
	}
	return ids, nil
}

func (r *mirrorRepo) last(id uuid.UUID) (models.PoolLinkWarmupSettings, bool) {
	if len(r.mirrored) == 0 {
		return models.PoolLinkWarmupSettings{}, false
	}
	w, ok := r.mirrored[len(r.mirrored)-1][id]
	return w, ok
}

func cloudDefaults() models.PoolLinkWarmupSettings {
	return models.PoolLinkWarmupSettings{Base: 10, Max: 40, Increase: 1, ReplyRate: 30, StartTime: "08:00:00", EndTime: "20:00:00", Timezone: "Asia/Calcutta"}
}

func newSettingsFixture(t *testing.T, account *models.Email, mailbox *models.CloudLinkMailbox) (*service, *mirrorRepo, *fakeCloud) {
	t.Helper()
	t.Setenv("APP_ENV", "dev")
	cloud := &fakeCloud{t: t, remote: account.ID, settings: cloudDefaults()}
	srv := httptest.NewServer(cloud)
	t.Cleanup(srv.Close)
	instance := uuid.New()
	if mailbox != nil {
		mailbox.InstanceID = instance
	}
	repo := &mirrorRepo{enrollmentFaultRepo: &enrollmentFaultRepo{stubLinkRepo: &stubLinkRepo{
		link:    &models.CloudLink{InstanceID: instance, CloudURL: srv.URL, Token: "fixture", OrganizationID: account.OrganizationID},
		mailbox: mailbox,
	}}}
	s := NewService(repo, enrollmentEmails{stubEmails{account: account}}, nil).(*service)
	return s, repo, cloud
}

func enrolledAccount() (*models.Email, *models.CloudLinkMailbox) {
	org, id := uuid.New(), uuid.New()
	acc := &models.Email{ID: id, OrganizationID: &org, Status: "active", Provider: "smtp_imap"}
	return acc, &models.CloudLinkMailbox{EmailAccountID: id, RemoteID: id, EnrollmentState: "active"}
}

func TestRelaySendsOnlyTheRequestedFieldsAndMirrorsCloudsAnswer(t *testing.T) {
	acc, mailbox := enrolledAccount()
	s, repo, cloud := newSettingsFixture(t, acc, mailbox)

	got, xerr := s.RelayWarmupSettings(context.Background(), *acc.OrganizationID, acc.ID,
		models.PoolLinkWarmupSettings{Base: 15, Days: models.WarmupEveryDay}, "")
	if xerr != nil {
		t.Fatal(xerr)
	}
	if len(cloud.patches) != 1 || *cloud.patches[0].Warmup != (models.PoolLinkWarmupSettings{Base: 15, Days: models.WarmupEveryDay}) || cloud.patches[0].Lifecycle != "" {
		t.Fatalf("patch = %+v, want only the requested fields", cloud.patches)
	}
	mirrored, ok := repo.last(acc.ID)
	if !ok || mirrored.Base != 15 || mirrored.Max != 40 || mirrored.StartTime != "08:00:00" {
		t.Fatalf("mirrored %+v, want the cloud's whole answer", mirrored)
	}
	if got.Base != 15 || got.Days != models.WarmupEveryDay || got.StartTime != "08:00" || got.EndTime != "20:00" {
		t.Fatalf("returned %+v, want the cloud's values in local form", got)
	}
}

func TestRelayNamesWhatTheCloudDidNotApplyAndStillMirrorsIt(t *testing.T) {
	acc, mailbox := enrolledAccount()
	s, repo, cloud := newSettingsFixture(t, acc, mailbox)
	cloud.refuse = map[string]bool{"increase": true}

	_, xerr := s.RelayWarmupSettings(context.Background(), *acc.OrganizationID, acc.ID,
		models.PoolLinkWarmupSettings{Base: 12, Increase: 3}, "")
	if xerr == nil || xerr.Identifier != "cloud_warmup_not_applied" {
		t.Fatalf("error = %v, want cloud_warmup_not_applied", xerr)
	}
	if mirrored, ok := repo.last(acc.ID); !ok || mirrored.Base != 12 || mirrored.Increase != 1 {
		t.Fatalf("mirrored %+v, want the cloud's actual values", mirrored)
	}
}

func TestRelayFailsClosedWithoutReportedSettings(t *testing.T) {
	acc, mailbox := enrolledAccount()
	s, repo, cloud := newSettingsFixture(t, acc, mailbox)
	cloud.omit = true
	if _, xerr := s.RelayWarmupSettings(context.Background(), *acc.OrganizationID, acc.ID, models.PoolLinkWarmupSettings{Base: 12}, ""); xerr == nil {
		t.Fatal("an answer without settings was taken as applied")
	}
	if len(repo.mirrored) != 0 {
		t.Fatal("mirrored settings the cloud never reported")
	}
}

func TestRelayToAnUnreachableCloudChangesNothing(t *testing.T) {
	acc, mailbox := enrolledAccount()
	s, repo, _ := newSettingsFixture(t, acc, mailbox)
	repo.link.CloudURL = "http://127.0.0.1:1"
	_, xerr := s.RelayWarmupSettings(context.Background(), *acc.OrganizationID, acc.ID, models.PoolLinkWarmupSettings{Base: 12}, "")
	if xerr == nil || xerr.Identifier != "cloud_link_unreachable" {
		t.Fatalf("error = %v, want cloud_link_unreachable", xerr)
	}
	if len(repo.mirrored) != 0 {
		t.Fatal("mirrored after a failed relay")
	}
}

func TestRelayRefusesOtherWorkspacesAndPendingRemovals(t *testing.T) {
	acc, mailbox := enrolledAccount()
	s, _, cloud := newSettingsFixture(t, acc, mailbox)
	if _, xerr := s.RelayWarmupSettings(context.Background(), uuid.New(), acc.ID, models.PoolLinkWarmupSettings{Base: 12}, ""); xerr == nil {
		t.Fatal("relayed a mailbox from another workspace")
	}
	mailbox.EnrollmentState = "pending_remove"
	if _, xerr := s.RelayWarmupSettings(context.Background(), *acc.OrganizationID, acc.ID, models.PoolLinkWarmupSettings{Base: 12}, ""); xerr == nil {
		t.Fatal("relayed a mailbox that is leaving the cloud")
	}
	if len(cloud.patches) != 0 {
		t.Fatalf("%d patches reached the cloud", len(cloud.patches))
	}
}

func TestRelayVerifiesPauseState(t *testing.T) {
	acc, mailbox := enrolledAccount()
	s, _, cloud := newSettingsFixture(t, acc, mailbox)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := cloud.state()
		st.Warmup = &models.WarmupStatusInfo{Paused: false}
		_ = json.NewEncoder(w).Encode(st)
	}))
	defer srv.Close()
	s.repo.(*mirrorRepo).link.CloudURL = srv.URL
	if _, xerr := s.RelayWarmupSettings(context.Background(), *acc.OrganizationID, acc.ID, models.PoolLinkWarmupSettings{}, "pause"); xerr == nil || xerr.Identifier != "cloud_warmup_not_applied" {
		t.Fatalf("error = %v, want a pause the cloud did not take reported", xerr)
	}
}

func TestEnrollProposesTheLocalRampOnlyTheFirstTime(t *testing.T) {
	acc, _ := enrolledAccount()
	acc.WarmupBase, acc.WarmupMax, acc.WarmupIncrease, acc.WarmupReplyRate = 12, 30, 2, 25
	acc.WarmupStartTime, acc.WarmupEndTime, acc.Timezone = "09:00:00", "18:00:00", "Europe/Berlin"
	s, repo, cloud := newSettingsFixture(t, acc, nil)

	if _, xerr := s.Enroll(context.Background(), *acc.OrganizationID, acc.ID); xerr != nil {
		t.Fatal(xerr)
	}
	want := models.PoolLinkWarmupSettings{Base: 12, Max: 30, Increase: 2, ReplyRate: 25, StartTime: "09:00", EndTime: "18:00", Days: models.WarmupEveryDay, Timezone: "Europe/Berlin"}
	if len(cloud.posts) != 1 || cloud.posts[0].Warmup != want {
		t.Fatalf("first enrollment sent %+v, want %+v", cloud.posts, want)
	}
	if mirrored, ok := repo.last(acc.ID); !ok || mirrored.Base != 12 {
		t.Fatalf("enrollment did not mirror the cloud's answer: %+v", repo.mirrored)
	}

	// The cloud now holds the ramp; a credential refresh must not send ours back over it.
	cloud.settings.Base = 20
	if xerr := s.RefreshCredentials(context.Background(), *acc.OrganizationID, acc.ID); xerr != nil {
		t.Fatal(xerr)
	}
	if len(cloud.posts) != 2 || cloud.posts[1].Warmup != (models.PoolLinkWarmupSettings{Timezone: "Europe/Berlin"}) {
		t.Fatalf("re-enrollment sent %+v, want the timezone only", cloud.posts[1].Warmup)
	}
	if cloud.settings.Base != 20 {
		t.Fatalf("re-enrollment overwrote the cloud's ramp: base %d", cloud.settings.Base)
	}
}

func TestSyncWarmupSettingsMirrorsAndPushesTheLocalTimezone(t *testing.T) {
	acc, mailbox := enrolledAccount()
	acc.OrgTimezone = "Asia/Calcutta"
	s, repo, cloud := newSettingsFixture(t, acc, mailbox)
	cloud.settings.Timezone = "UTC"
	var announced []uuid.UUID
	s.OnSettingsChange(func(_ context.Context, ids []uuid.UUID) { announced = append(announced, ids...) })

	changed, xerr := s.SyncWarmupSettings(context.Background())
	if xerr != nil {
		t.Fatal(xerr)
	}
	if !slices.Equal(changed, []uuid.UUID{acc.ID}) || !slices.Equal(announced, []uuid.UUID{acc.ID}) {
		t.Fatalf("changed %v announced %v, want the mailbox once", changed, announced)
	}
	if len(cloud.patches) != 1 || cloud.patches[0].Warmup == nil || *cloud.patches[0].Warmup != (models.PoolLinkWarmupSettings{Timezone: "Asia/Calcutta"}) {
		t.Fatalf("patches = %+v, want the local timezone pushed", cloud.patches)
	}
	if _, xerr := s.SyncWarmupSettings(context.Background()); xerr != nil {
		t.Fatal(xerr)
	}
	if len(cloud.patches) != 1 {
		t.Fatalf("pushed the timezone again after the cloud took it: %d patches", len(cloud.patches))
	}

	mailbox.EnrollmentState = "pending_remove"
	repo.mirrored = nil
	if _, xerr := s.SyncWarmupSettings(context.Background()); xerr != nil {
		t.Fatal(xerr)
	}
	if len(repo.mirrored) != 0 {
		t.Fatal("mirrored a mailbox that is leaving the cloud")
	}
}

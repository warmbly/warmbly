package confenge

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/confenge/dispatch"
	"github.com/warmbly/warmbly/internal/models"
)

func allowConfengeSendingForTest(t *testing.T) {
	t.Helper()
	t.Setenv(EnvKillSwitchPath, filepath.Join(t.TempDir(), "not-engaged"))
}

func TestGateCampaignEmailKillSwitchBlocksStaleEnrollment(t *testing.T) {
	t.Setenv(EnvKillSwitchPath, filepath.Join(t.TempDir(), "engaged"))
	if err := EngageKillSwitch(); err != nil {
		t.Fatal(err)
	}
	svc := &service{cfg: Config{Enabled: true}}
	result := svc.GateCampaignEmail(context.Background(), uuid.New(), DefaultCampaignName, "lead@example.com", uuid.New(), uuid.New(), uuid.New())
	if result.Kind != GateDeferred || result.Reason != ReasonSendingOff {
		t.Fatalf("kill switch must defer before transport: %+v", result)
	}
}

func TestPermanentSuppressOnlyHardBlock(t *testing.T) {
	cases := []struct {
		kind GateKind
		want bool
	}{
		{GateProceed, false},
		{GateDeferred, false},
		{GateAlready, false},
		{GateHardBlock, true},
		{GateTransient, false},
		{GateBypass, false},
	}
	for _, tc := range cases {
		r := CampaignGateResult{Kind: tc.kind}
		if got := r.PermanentSuppress(); got != tc.want {
			t.Fatalf("kind=%d PermanentSuppress=%v want %v", tc.kind, got, tc.want)
		}
	}
}

func TestGateCampaignEmailHardBlockDNC(t *testing.T) {
	allowConfengeSendingForTest(t)
	org := uuid.New()
	accID := uuid.New()
	candID := uuid.New()
	repo := newMemRepoWithSettings()
	acc := &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "12345678000199",
		DoNotContact: false, Blocked: false,
	}
	_, _ = repo.UpsertAccount(context.Background(), acc)
	cand := &models.OutreachContactCandidate{
		ID: candID, OrganizationID: org, AccountID: accID,
		Email: "dnc@example.com", DoNotContact: true,
	}
	_, _ = repo.UpsertCandidate(context.Background(), cand)

	svc := &service{cfg: Config{Enabled: true}, repo: repo}
	clock := &dispatch.FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	cfg := dispatch.DefaultConfig()
	cfg.WindowStart, cfg.WindowEnd, cfg.Timezone, cfg.MinGap = "00:00", "23:59", "UTC", 0
	cfg.BusinessDaysOnly = false
	svc.governor = dispatch.NewGovernor(cfg, dispatch.NewMemoryStore(), clock)

	r := svc.GateCampaignEmail(context.Background(), org, DefaultCampaignName, "dnc@example.com",
		uuid.New(), uuid.New(), uuid.New())
	if r.Kind != GateHardBlock || r.Reason != ReasonDNCOrBounce {
		t.Fatalf("got kind=%d reason=%s want HardBlock dnc_or_bounce", r.Kind, r.Reason)
	}
	if r.Err != nil {
		t.Fatalf("HardBlock must not set Err: %v", r.Err)
	}
	if !r.PermanentSuppress() {
		t.Fatal("HardBlock must PermanentSuppress")
	}
}

func TestGateCampaignEmailHardBlockAccountDNC(t *testing.T) {
	allowConfengeSendingForTest(t)
	org := uuid.New()
	accID := uuid.New()
	repo := newMemRepoWithSettings()
	acc := &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "12345678000188",
		DoNotContact: true, Blocked: true,
	}
	_, _ = repo.UpsertAccount(context.Background(), acc)
	cand := &models.OutreachContactCandidate{
		ID: uuid.New(), OrganizationID: org, AccountID: accID,
		Email: "ok@example.com", DoNotContact: false,
	}
	_, _ = repo.UpsertCandidate(context.Background(), cand)

	svc := &service{cfg: Config{Enabled: true}, repo: repo}
	clock := &dispatch.FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	cfg := dispatch.DefaultConfig()
	cfg.WindowStart, cfg.WindowEnd, cfg.Timezone, cfg.MinGap = "00:00", "23:59", "UTC", 0
	cfg.BusinessDaysOnly = false
	svc.governor = dispatch.NewGovernor(cfg, dispatch.NewMemoryStore(), clock)

	r := svc.GateCampaignEmail(context.Background(), org, DefaultCampaignName, "ok@example.com",
		uuid.New(), uuid.New(), uuid.New())
	if r.Kind != GateHardBlock || r.Reason != ReasonAccountDNC {
		t.Fatalf("got kind=%d reason=%s", r.Kind, r.Reason)
	}
	if r.PermanentSuppress() != true {
		t.Fatal("account DNC must permanent suppress")
	}
}

func TestGateCampaignEmailTransientOnGovernorError(t *testing.T) {
	allowConfengeSendingForTest(t)
	// errStore fails TryReserveAtomic (simulates DB blip).
	store := &errStore{MemoryStore: dispatch.NewMemoryStore(), failReserve: true}
	clock := &dispatch.FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	cfg := dispatch.DefaultConfig()
	cfg.WindowStart, cfg.WindowEnd, cfg.Timezone, cfg.MinGap = "00:00", "23:59", "UTC", 0
	cfg.BusinessDaysOnly = false
	gov := dispatch.NewGovernor(cfg, store, clock)

	repo := newMemRepoWithSettings()
	org := uuid.New()
	accID := uuid.New()
	_, _ = repo.UpsertAccount(context.Background(), &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "12345678000177",
	})
	candidateID := uuid.New()
	_, _ = repo.UpsertCandidate(context.Background(), &models.OutreachContactCandidate{ID: candidateID, OrganizationID: org, AccountID: accID, Email: "lead@example.com"})
	campaignID, contactID := bindTransportableEnrollment(t, repo.memRepo, org, accID, candidateID, "lead@example.com")

	svc := &service{cfg: Config{Enabled: true}, repo: repo, governor: gov}
	r := svc.GateCampaignEmail(context.Background(), org, DefaultCampaignName, "lead@example.com",
		campaignID, contactID, uuid.New())
	if r.Kind != GateTransient {
		t.Fatalf("governor error must be Transient, got kind=%d reason=%s err=%v", r.Kind, r.Reason, r.Err)
	}
	if r.PermanentSuppress() {
		t.Fatal("Transient must never PermanentSuppress")
	}
	if r.Err == nil {
		t.Fatal("Transient should carry Err")
	}
}

func TestGateCampaignEmailDeferredCap(t *testing.T) {
	allowConfengeSendingForTest(t)
	store := dispatch.NewMemoryStore()
	clock := &dispatch.FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	cfg := dispatch.DefaultConfig()
	cfg.WindowStart, cfg.WindowEnd, cfg.Timezone, cfg.MinGap = "00:00", "23:59", "UTC", 0
	cfg.BusinessDaysOnly = false
	cfg.SendsPerHour = 1
	gov := dispatch.NewGovernor(cfg, store, clock)

	repo := newMemRepoWithSettings()
	org := uuid.New()
	accID := uuid.New()
	_, _ = repo.UpsertAccount(context.Background(), &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "12345678000166",
	})
	candidateID := uuid.New()
	_, _ = repo.UpsertCandidate(context.Background(), &models.OutreachContactCandidate{ID: candidateID, OrganizationID: org, AccountID: accID, Email: "lead@example.com"})
	campaignID, contactID := bindTransportableEnrollment(t, repo.memRepo, org, accID, candidateID, "lead@example.com")
	svc := &service{cfg: Config{Enabled: true}, repo: repo, governor: gov}

	// First send consumes the only slot.
	r1 := svc.GateCampaignEmail(context.Background(), org, DefaultCampaignName, "lead@example.com",
		campaignID, contactID, uuid.New())
	if r1.Kind != GateProceed {
		t.Fatalf("first: kind=%d", r1.Kind)
	}
	_ = svc.governor.Commit(context.Background(), r1.ReservationID)

	// Second is deferred (cap), not hard-block.
	r2 := svc.GateCampaignEmail(context.Background(), org, DefaultCampaignName, "lead@example.com",
		campaignID, contactID, uuid.New())
	if r2.Kind != GateDeferred {
		t.Fatalf("second: kind=%d reason=%s want Deferred", r2.Kind, r2.Reason)
	}
	if r2.PermanentSuppress() {
		t.Fatal("Deferred must not permanent suppress")
	}
}

func bindTransportableEnrollment(t *testing.T, repo *memRepo, orgID, accountID, candidateID uuid.UUID, email string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	account, _ := repo.GetAccount(ctx, orgID, accountID)
	contact, _ := repo.GetCandidate(ctx, orgID, candidateID)
	if account == nil || contact == nil {
		t.Fatal("account and candidate are required")
	}
	importID := uuid.New()
	now := time.Now().UTC()
	account.LastImportRunID = &importID
	account.SourceRunID = "current-run"
	account.MessageContextHash = "context-" + accountID.String()
	account.TargetFitClass = TargetFitConfirmed
	account.TargetFitVersion = "v1"
	account.TargetFitSourceWatermark = "watermark"
	account.TargetFitObservedAt = &now
	account.TargetFitFresh = true
	account.TargetFitEligible = true
	account.EmailSendReady = true
	_, _ = repo.UpsertAccount(ctx, account)
	contact.LastImportRunID = &importID
	contact.EmailSendReady = true
	contact.OwnershipStatus = "COMPANY_OWNED"
	contact.VerificationStatus = models.OutreachVerifyOfficialSource
	_, _ = repo.UpsertCandidate(ctx, contact)
	campaignID, enrollmentContactID := uuid.New(), uuid.New()
	draft := &models.OutreachDraft{
		OrganizationID: orgID, AccountID: accountID, ContactCandidateID: &candidateID,
		RecipientEmail: email, CampaignID: &campaignID, EnrollmentContactID: &enrollmentContactID,
		Status: models.OutreachDraftEnrolled,
	}
	if err := repo.UpsertDraft(ctx, draft); err != nil {
		t.Fatal(err)
	}
	approver := uuid.New()
	touchpoint := &models.OutreachTouchpoint{
		OrganizationID: orgID, AccountID: accountID, ContactCandidateID: &candidateID,
		DraftID: &draft.ID, Ordinal: 1, Channel: models.OutreachChannelEmail,
		State: models.TouchpointQueued, Recipient: email, Subject: "Approved subject",
		BodyText: "Approved body with enough detail", ContentHash: "hash", ApprovedContentHash: "hash",
		ApprovedBy: &approver, AuthorizationMode: AuthorizationModeHumanTouchpoint,
		GeneratedContextHash: account.MessageContextHash,
	}
	if err := repo.InsertTouchpoint(ctx, touchpoint); err != nil {
		t.Fatal(err)
	}
	return campaignID, enrollmentContactID
}

func TestGateCampaignEmailBlocksPartialAuthoritativeFeed(t *testing.T) {
	allowConfengeSendingForTest(t)
	repo := newMemRepoWithSettings()
	orgID, accountID, candidateID := uuid.New(), uuid.New(), uuid.New()
	_, _ = repo.UpsertAccount(context.Background(), &models.OutreachAccount{ID: accountID, OrganizationID: orgID, CNPJ14: "12345678000155"})
	_, _ = repo.UpsertCandidate(context.Background(), &models.OutreachContactCandidate{ID: candidateID, OrganizationID: orgID, AccountID: accountID, Email: "lead@example.com"})
	campaignID, contactID := bindTransportableEnrollment(t, repo.memRepo, orgID, accountID, candidateID, "lead@example.com")
	now := time.Now().UTC()
	repo.feedSync = map[uuid.UUID]*models.OutreachFeedSyncState{orgID: {
		OrganizationID: orgID, LastStatus: "partial", LastRunID: "current-run",
		LastSnapshotHash: "snapshot", SourceGeneratedAt: &now,
	}}
	dispatchCfg := dispatch.DefaultConfig()
	dispatchCfg.WindowStart, dispatchCfg.WindowEnd, dispatchCfg.Timezone, dispatchCfg.BusinessDaysOnly = "00:00", "23:59", "UTC", false
	svc := &service{
		cfg: Config{Enabled: true, FeedSyncEnabled: true}, repo: repo,
		governor: dispatch.NewGovernor(dispatchCfg, dispatch.NewMemoryStore(), nil),
	}
	result := svc.GateCampaignEmail(context.Background(), orgID, DefaultCampaignName, "lead@example.com", campaignID, contactID, uuid.New())
	if result.Kind != GateCommercialBlock || result.Reason != "authoritative_feed_invalid" {
		t.Fatalf("partial authoritative feed must block transport: %+v", result)
	}
}

func TestGateCampaignEmailBypassNonConfenge(t *testing.T) {
	svc := &service{cfg: Config{Enabled: true}}
	r := svc.GateCampaignEmail(context.Background(), uuid.New(), "Regular campaign", "a@b.com",
		uuid.New(), uuid.New(), uuid.New())
	if r.Kind != GateBypass {
		t.Fatalf("kind=%d", r.Kind)
	}
	if r.PermanentSuppress() {
		t.Fatal("Bypass must not suppress")
	}
}

func TestGateCampaignEmailRenamedConfiguredCampaignCannotBypass(t *testing.T) {
	allowConfengeSendingForTest(t)
	orgID, campaignID := uuid.New(), uuid.New()
	repo := newMemRepoWithSettings()
	if err := repo.UpsertOrgSettings(context.Background(), &models.OutreachOrgSettings{OrganizationID: orgID, CampaignID: &campaignID}); err != nil {
		t.Fatal(err)
	}
	svc := &service{cfg: Config{Enabled: true}, repo: repo}
	result := svc.GateCampaignEmail(context.Background(), orgID, "Renamed campaign", "lead@example.com", campaignID, uuid.New(), uuid.New())
	if result.Kind == GateBypass || result.Kind == GateProceed {
		t.Fatalf("configured CONFENGE campaign must fail closed without an exact approved touchpoint: %+v", result)
	}
}

// TestGateCampaignEmailConfengeNilGovernorFailClosed: CONFENGE + nil governor => zero send.
func TestGateCampaignEmailConfengeNilGovernorFailClosed(t *testing.T) {
	allowConfengeSendingForTest(t)
	svc := &service{cfg: Config{Enabled: true}, governor: nil}
	r := svc.GateCampaignEmail(context.Background(), uuid.New(), DefaultCampaignName, "lead@example.com",
		uuid.New(), uuid.New(), uuid.New())
	if r.Kind != GateTransient {
		t.Fatalf("CONFENGE+nil governor must be Transient (fail-closed), got kind=%d reason=%s", r.Kind, r.Reason)
	}
	if r.Reason != ReasonNoGovernor {
		t.Fatalf("reason want %s got %s", ReasonNoGovernor, r.Reason)
	}
	if r.PermanentSuppress() {
		t.Fatal("must not permanent suppress")
	}
	// Non-CONFENGE still bypasses with nil governor.
	r2 := svc.GateCampaignEmail(context.Background(), uuid.New(), "Other campaign", "a@b.com",
		uuid.New(), uuid.New(), uuid.New())
	if r2.Kind != GateBypass {
		t.Fatalf("non-CONFENGE+nil want Bypass, got %d", r2.Kind)
	}
}

// TestGateCampaignEmailConfengeDisabledFailClosed: enabled=false still fail-closed for CONFENGE name.
func TestGateCampaignEmailConfengeDisabledFailClosed(t *testing.T) {
	allowConfengeSendingForTest(t)
	svc := &service{cfg: Config{Enabled: false}, governor: nil}
	r := svc.GateCampaignEmail(context.Background(), uuid.New(), "CONFENGE cold", "x@y.com",
		uuid.New(), uuid.New(), uuid.New())
	if r.Kind != GateTransient {
		t.Fatalf("got kind=%d want Transient", r.Kind)
	}
}

// errStore embeds MemoryStore and can fail TryReserveAtomic.
type errStore struct {
	*dispatch.MemoryStore
	failReserve bool
}

func (e *errStore) TryReserveAtomic(ctx context.Context, in dispatch.AtomicReserveInput) (dispatch.AtomicReserveOutput, error) {
	if e.failReserve {
		return dispatch.AtomicReserveOutput{}, errors.New("simulated store failure")
	}
	return e.MemoryStore.TryReserveAtomic(ctx, in)
}

package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// retentionWarmupRepo answers the one receipt lookup the removal and flag
// handlers make.
type retentionWarmupRepo struct {
	repository.WarmupRepository
	rec  *repository.WarmupReceived
	held *[]repository.WarmupSpamMove
}

func (r retentionWarmupRepo) GetWarmupReceived(context.Context, uuid.UUID, uuid.UUID) (*repository.WarmupReceived, error) {
	return r.rec, nil
}

func (r retentionWarmupRepo) RecordWarmupSpamMove(_ context.Context, m repository.WarmupSpamMove) (bool, error) {
	*r.held = append(*r.held, m)
	return true, nil
}

// retentionWarmupService records which strikes the handlers asked for.
type retentionWarmupService struct {
	warmupapp.Service
	strikes []string
	held    []repository.WarmupSpamMove
	fail    bool
}

func (s *retentionWarmupService) RecordTampering(_ context.Context, _ uuid.UUID, _, kind string) (*models.WarmupParticipantHealth, *errx.Error) {
	if s.fail {
		return nil, errx.InternalError()
	}
	s.strikes = append(s.strikes, kind)
	return nil, nil
}

func (s *retentionWarmupService) WithdrawTampering(_ context.Context, _ uuid.UUID, _, kind string) (*models.WarmupParticipantHealth, *errx.Error) {
	s.strikes = append(s.strikes, "withdraw:"+kind)
	return nil, nil
}

func (s *retentionWarmupService) ApplySpamReport(context.Context, uuid.UUID, uuid.UUID, string, string) (*models.WarmupParticipantHealth, *errx.Error) {
	s.strikes = append(s.strikes, "spam_report")
	return nil, nil
}

func retentionService(rec *repository.WarmupReceived) (*JobsService, *retentionWarmupService) {
	svc := &retentionWarmupService{}
	return &JobsService{
		WarmupRepo:      retentionWarmupRepo{rec: rec, held: &svc.held},
		WarmupService:   svc,
		EmailRepository: warmupInboxEmailRepo{},
	}, svc
}

func receivedAgo(age time.Duration) *repository.WarmupReceived {
	return &repository.WarmupReceived{
		EmailAccountID: uuid.New(), InternalID: uuid.New(),
		MessageID: "<warmup@example.test>", SenderAccountID: uuid.New(),
		CreatedAt: time.Now().Add(-age),
	}
}

func TestWarmupDeletionCounts(t *testing.T) {
	now := time.Now()
	window := time.Duration(config.WarmupDeletionStrikeHours) * time.Hour
	retired := now.Add(-time.Hour)
	cases := []struct {
		name string
		rec  *repository.WarmupReceived
		want bool
	}{
		{"no receipt is not warmup", nil, false},
		{"fresh is a strike", &repository.WarmupReceived{CreatedAt: now.Add(-time.Hour)}, true},
		{"just inside the window is a strike", &repository.WarmupReceived{CreatedAt: now.Add(-window + time.Minute)}, true},
		{"past the window is housekeeping", &repository.WarmupReceived{CreatedAt: now.Add(-window - time.Minute)}, false},
		{"weeks later is housekeeping", &repository.WarmupReceived{CreatedAt: now.AddDate(0, 0, -31)}, false},
		{"retired by the platform is never a strike, even fresh", &repository.WarmupReceived{CreatedAt: now.Add(-time.Hour), RetiredAt: &retired}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := warmupDeletionCounts(tc.rec, now); got != tc.want {
				t.Fatalf("warmupDeletionCounts() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A removal of warmup mail is judged only while the message is fresh, and then
// never on the removal itself: the worker is asked where the message went.
// Later it is the mailbox owner tidying, Gmail purging its Trash, a server
// retention rule, or the platform's own retention, none of which is harm.
func TestRemoveEmailChecksOnlyFreshWarmupMail(t *testing.T) {
	cases := []struct {
		name   string
		rec    *repository.WarmupReceived
		checks int
	}{
		{"removed an hour after arrival", receivedAgo(time.Hour), 1},
		{"removed a week after arrival", receivedAgo(7 * 24 * time.Hour), 0},
		{"not warmup at all", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, svc := retentionService(tc.rec)
			pub := &backfillPublisher{}
			worker := uuid.New()
			s.Publisher = pub
			s.EmailRepository = &backfillEmailRepo{account: &models.Email{WorkerID: &worker}}
			if err := s.HandleRemoveEmail(context.Background(), &models.JobEventRemoveEmail{
				UserID: uuid.New(), EmailID: uuid.New(), ID: uuid.New(),
			}); err != nil {
				t.Fatal(err)
			}
			if len(svc.strikes) != 0 {
				t.Fatalf("a removal was charged before the mailbox was searched: %v", svc.strikes)
			}
			if len(pub.actions) != tc.checks {
				t.Fatalf("published %d checks, want %d", len(pub.actions), tc.checks)
			}
			if tc.checks == 1 {
				a := pub.actions[0]
				if len(a.Actions) != 1 || a.Actions[0] != models.WarmupActionVerifyRemoval ||
					a.RFCMessageID != tc.rec.MessageID || a.Recheck || pub.workers[0] != worker {
					t.Fatalf("check carried %+v to %v", a, pub.workers[0])
				}
			}
		})
	}
}

// A mailbox with no worker to search it is not charged.
func TestRemoveEmailWithNowhereToCheckChargesNothing(t *testing.T) {
	s, svc := retentionService(receivedAgo(time.Hour))
	pub := &backfillPublisher{}
	s.Publisher = pub
	if err := s.HandleRemoveEmail(context.Background(), &models.JobEventRemoveEmail{
		UserID: uuid.New(), EmailID: uuid.New(), ID: uuid.New(),
	}); err != nil {
		t.Fatal(err)
	}
	if len(svc.strikes) != 0 || len(pub.actions) != 0 {
		t.Fatalf("strikes %v, checks %d; want neither", svc.strikes, len(pub.actions))
	}
}

// The strike follows where the worker found the message. A fresh check
// records a strike for the trash or nowhere; a recheck never adds one, it
// confirms the recorded strike or withdraws it when the message is still
// there or retention removed it since.
func TestRemovalCheckedJudgesOnWhereTheMessageIs(t *testing.T) {
	cases := []struct {
		name     string
		outcome  string
		recheck  bool
		retired  bool
		want     []string
		verified int
	}{
		{"moved to another folder", models.WarmupRemovalPresent, false, false, []string{"withdraw:deletion"}, 0},
		{"in the trash", models.WarmupRemovalTrashed, false, false, nil, 0},
		{"gone for good", models.WarmupRemovalGone, false, false, nil, 0},
		{"a fresh search that cannot tell charges nothing", models.WarmupRemovalUnknown, false, false, nil, 0},
		{"recheck: still in the mailbox", models.WarmupRemovalPresent, true, false, []string{"withdraw:deletion"}, 0},
		{"recheck: in the trash confirms without a new strike", models.WarmupRemovalTrashed, true, false, nil, 1},
		{"recheck: gone after retention retired it", models.WarmupRemovalGone, true, true, []string{"withdraw:deletion"}, 0},
		{"recheck: cannot tell leaves it and stops asking", models.WarmupRemovalUnknown, true, false, nil, 1},
		{"an answer this consumer does not know", "sideways", false, false, nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, svc := retentionService(receivedAgo(time.Hour))
			repo := &verifiedRepo{retired: tc.retired}
			s.WarmupRepo = repo
			if err := s.HandleWarmupRemovalChecked(context.Background(), &models.JobEventWarmupRemovalChecked{
				UserID: uuid.New(), EmailID: uuid.New(), RFCMessageID: "<m@example.test>", Outcome: tc.outcome, Recheck: tc.recheck,
			}); err != nil {
				t.Fatal(err)
			}
			if len(svc.strikes) != len(tc.want) {
				t.Fatalf("strikes = %v, want %v", svc.strikes, tc.want)
			}
			for i := range tc.want {
				if svc.strikes[i] != tc.want[i] {
					t.Fatalf("strikes = %v, want %v", svc.strikes, tc.want)
				}
			}
			if repo.verified != tc.verified {
				t.Fatalf("confirmed %d strikes, want %d", repo.verified, tc.verified)
			}
		})
	}
}

func TestRemovalCheckedDoesNotChargeRecipientWithdrawal(t *testing.T) {
	s, svc := retentionService(nil)
	svc.fail = true
	if err := s.HandleWarmupRemovalChecked(context.Background(), &models.JobEventWarmupRemovalChecked{
		UserID: uuid.New(), EmailID: uuid.New(), RFCMessageID: "<m@example.test>", Outcome: models.WarmupRemovalTrashed,
	}); err != nil || len(svc.strikes) != 0 {
		t.Fatal("recipient withdrawal was penalized", err, svc.strikes)
	}
}

// verifiedRepo counts the strikes a search confirmed.
type verifiedRepo struct {
	repository.WarmupRepository
	retired  bool
	verified int
}

func (r *verifiedRepo) WarmupReceiptRetired(context.Context, uuid.UUID, string) (bool, error) {
	return r.retired, nil
}

func (r *verifiedRepo) MarkTamperingVerified(context.Context, uuid.UUID, string, string) error {
	r.verified++
	return nil
}

// unverifiedRepo serves one listing of strikes recorded before removals were
// checked and records which searches were asked for.
type unverifiedRepo struct {
	repository.WarmupRepository
	rows      []repository.WarmupTamperingToVerify
	requested []string
}

func (r *unverifiedRepo) ListUnverifiedDeletions(context.Context, time.Time, time.Duration, int) ([]repository.WarmupTamperingToVerify, error) {
	return r.rows, nil
}

func (r *unverifiedRepo) MarkTamperingVerifyRequested(_ context.Context, _ uuid.UUID, messageID string) error {
	r.requested = append(r.requested, messageID)
	return nil
}

// Every old strike is searched for, and marked asked only once its search is
// on the bus, so a publish that fails is offered again next pass.
func TestRecheckTamperingSearchesEachOldStrike(t *testing.T) {
	rows := []repository.WarmupTamperingToVerify{
		{EmailAccountID: uuid.New(), UserID: uuid.New(), WorkerID: uuid.New(), MessageID: "<a@example.test>"},
		{EmailAccountID: uuid.New(), UserID: uuid.New(), WorkerID: uuid.New(), MessageID: "<b@example.test>"},
	}
	repo := &unverifiedRepo{rows: rows}
	pub := &backfillPublisher{}
	s := &JobsService{WarmupRepo: repo, Publisher: pub}
	if err := s.recheckTamperingBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(pub.actions) != 2 || len(repo.requested) != 2 {
		t.Fatalf("published %d, marked %d; want 2 and 2", len(pub.actions), len(repo.requested))
	}
	if pub.actions[1].RFCMessageID != rows[1].MessageID || pub.workers[1] != rows[1].WorkerID ||
		pub.actions[1].Actions[0] != models.WarmupActionVerifyRemoval || !pub.actions[1].Recheck {
		t.Fatalf("checks carried %+v to %v", pub.actions, pub.workers)
	}

	failing := &unverifiedRepo{rows: rows}
	s = &JobsService{WarmupRepo: failing, Publisher: &backfillPublisher{err: errors.New("bus down")}}
	if err := s.recheckTamperingBatch(context.Background()); err == nil {
		t.Fatal("a failed publish should be reported")
	}
	if len(failing.requested) != 0 {
		t.Fatalf("marked %v asked without a search on the bus", failing.requested)
	}
}

// Recipient filing never earns a strike; later spam moves retain sender attribution.
func TestFlagsAddPreservesSenderAttributionWithoutPenalizingRecipientFiling(t *testing.T) {
	landedSpam := receivedAgo(time.Second)
	landedSpam.LandedSpam = true
	cases := []struct {
		name  string
		rec   *repository.WarmupReceived
		flags []string
		want  []string
		held  int
	}{
		{"trashed an hour after arrival", receivedAgo(time.Hour), []string{"TRASH"}, nil, 0},
		{"trashed a month after arrival", receivedAgo(30 * 24 * time.Hour), []string{"TRASH"}, nil, 0},
		{"moved to spam a month after arrival is held", receivedAgo(30 * 24 * time.Hour), []string{"SPAM"}, nil, 1},
		{"spam label on mail that arrived in spam", landedSpam, []string{"SPAM"}, nil, 0},
		{"read is not a strike", receivedAgo(time.Hour), []string{models.FlagSeen}, nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, svc := retentionService(tc.rec)
			if err := s.HandleFlagsAdd(context.Background(), &models.JobEventFlags{
				UserID: uuid.New(), EmailID: uuid.New(), ID: uuid.New(), Flags: tc.flags,
			}); err != nil {
				t.Fatal(err)
			}
			if len(svc.strikes) != len(tc.want) {
				t.Fatalf("strikes = %v, want %v", svc.strikes, tc.want)
			}
			if len(svc.held) != tc.held {
				t.Fatalf("held %d moves, want %d", len(svc.held), tc.held)
			}
			for i := range tc.want {
				if svc.strikes[i] != tc.want[i] {
					t.Fatalf("strikes = %v, want %v", svc.strikes, tc.want)
				}
			}
		})
	}
}

// retentionPublisher captures the delete actions the sweep publishes.
type retentionPublisher struct {
	backfillPublisher
}

// retentionRepo serves one listing of each kind and records what was retired.
type retentionRepo struct {
	repository.WarmupRepository
	received []repository.WarmupMailToRetire
	sent     []repository.WarmupMailToRetire
	retired  []uuid.UUID
	pruned   *time.Time
}

func (r *retentionRepo) ListWarmupMailToRetire(context.Context, int, int) ([]repository.WarmupMailToRetire, error) {
	return r.received, nil
}

func (r *retentionRepo) ListWarmupSentCopiesToRetire(context.Context, int, int) ([]repository.WarmupMailToRetire, error) {
	return r.sent, nil
}

func (r *retentionRepo) RetireWarmupReceived(_ context.Context, _, internalID uuid.UUID) error {
	r.retired = append(r.retired, internalID)
	return nil
}

func (r *retentionRepo) RetireWarmupSentCopy(_ context.Context, token uuid.UUID) error {
	r.retired = append(r.retired, token)
	return nil
}

func (r *retentionRepo) PruneWarmupEventsBefore(_ context.Context, before time.Time) (int64, error) {
	r.pruned = &before
	return 0, nil
}

// The sweep publishes one delete per row, with every key the worker can find
// the message by, and retires the row only once the action is on the bus.
func TestRetireWarmupMailBatchPublishesAndRetires(t *testing.T) {
	worker := uuid.New()
	received := repository.WarmupMailToRetire{
		UserID: uuid.New(), EmailAccountID: uuid.New(), WorkerID: worker,
		InternalID: uuid.New(), MessageID: "<in@example.test>", ProviderKey: "gmail-1",
		Placement: models.WarmupPlacementFolder, Folder: "Reputation",
	}
	sent := repository.WarmupMailToRetire{
		UserID: uuid.New(), EmailAccountID: uuid.New(), WorkerID: worker,
		Token: uuid.New(), MessageID: "<out@example.test>",
	}
	repo := &retentionRepo{received: []repository.WarmupMailToRetire{received}, sent: []repository.WarmupMailToRetire{sent}}
	pub := &retentionPublisher{}
	s := &JobsService{WarmupRepo: repo, Publisher: pub}

	done, err := s.retireWarmupMailBatch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !done {
		t.Fatal("a short listing should complete the pass")
	}
	if len(pub.actions) != 2 {
		t.Fatalf("published %d actions, want 2", len(pub.actions))
	}
	in := pub.actions[0]
	if in.Actions[0] != models.WarmupActionDelete || in.GmailID != "gmail-1" || in.RFCMessageID != received.MessageID ||
		in.InternalID != received.InternalID.String() || in.TargetFolder != "Reputation" {
		t.Fatalf("received delete carried %+v", in)
	}
	out := pub.actions[1]
	if out.InternalID != "" || out.RFCMessageID != sent.MessageID || out.TargetFolder != config.WarmupFolderDefault {
		t.Fatalf("sent-copy delete carried %+v", out)
	}
	if len(repo.retired) != 2 || repo.retired[0] != received.InternalID || repo.retired[1] != sent.Token {
		t.Fatalf("retired %v, want the receipt then the token", repo.retired)
	}
}

func TestPruneWarmupEventsUsesTheInstanceWindow(t *testing.T) {
	repo := &retentionRepo{}
	s := &JobsService{WarmupRepo: repo, Publisher: &retentionPublisher{}}
	if err := s.pruneWarmupEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repo.pruned == nil {
		t.Fatal("nothing was pruned")
	}
	want := time.Now().AddDate(0, 0, -config.WarmupEventRetentionDaysDefault)
	if d := repo.pruned.Sub(want); d < -time.Minute || d > time.Minute {
		t.Fatalf("pruned before %v, want about %v", repo.pruned, want)
	}
}

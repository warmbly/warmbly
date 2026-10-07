package jobs

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func spamMoveAfter(sinceArrival time.Duration) repository.WarmupSpamMove {
	observed := time.Date(2026, 9, 28, 14, 18, 36, 0, time.UTC)
	return repository.WarmupSpamMove{
		EmailAccountID: uuid.New(), MessageID: "<warmup@example.test>", SenderAccountID: uuid.New(),
		ReceivedAt: observed.Add(-sinceArrival), ObservedAt: observed, Verdict: repository.SpamMovePending,
	}
}

func TestAttributeSpamMove(t *testing.T) {
	quick := time.Duration(config.WarmupSpamMoveQuickMinutes)*time.Minute - time.Second
	later := 6 * time.Hour
	pattern := config.WarmupSpamMovePatternSenders
	cases := []struct {
		name  string
		since time.Duration
		ev    repository.WarmupSpamMoveEvidence
		want  string
	}{
		{"the filter catching up with nobody there", quick, repository.WarmupSpamMoveEvidence{OwnerActiveRecently: true}, repository.SpamMoveProvider},
		{"the owner at the mailbox right after arrival", quick, repository.WarmupSpamMoveEvidence{OwnerActiveNear: true, OwnerActiveRecently: true}, repository.SpamMoveOwner},
		{"the owner at the mailbox later", later, repository.WarmupSpamMoveEvidence{OwnerActiveNear: true, OwnerActiveRecently: true}, repository.SpamMoveOwner},
		{"other workspaces junked the same sender", later, repository.WarmupSpamMoveEvidence{OwnerActiveNear: true, OwnerActiveRecently: true, CorrelatedElsewhere: 1}, repository.SpamMoveProvider},
		{"a mailbox nobody uses", later, repository.WarmupSpamMoveEvidence{PatternSenders: pattern + 5}, repository.SpamMoveUnattributed},
		{"one unexplained move in a used mailbox", later, repository.WarmupSpamMoveEvidence{OwnerActiveRecently: true, PatternSenders: 1}, repository.SpamMoveUnattributed},
		{"a used mailbox junking many senders nobody else does", later, repository.WarmupSpamMoveEvidence{OwnerActiveRecently: true, PatternSenders: pattern}, repository.SpamMoveOwner},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, signals := attributeSpamMove(spamMoveAfter(tc.since), tc.ev)
			if got != tc.want {
				t.Fatalf("verdict = %s (%v), want %s", got, signals, tc.want)
			}
			if len(signals) == 0 {
				t.Fatal("a verdict carries no evidence for an operator to read")
			}
		})
	}
}

// attributionRepo serves settled moves and their evidence, and records what was decided.
type attributionRepo struct {
	repository.WarmupRepository
	moves      []repository.WarmupSpamMove
	evidence   repository.WarmupSpamMoveEvidence
	correlated []repository.WarmupSpamMove
	claimLost  bool
	log        *[]string
}

func (r attributionRepo) ClaimWarmupSpamMove(context.Context, uuid.UUID, string, time.Duration) (bool, error) {
	return !r.claimLost, nil
}

func (r attributionRepo) FixWarmupSpamMoveVerdict(_ context.Context, _ uuid.UUID, _, verdict string, _ []string) (bool, error) {
	*r.log = append(*r.log, "fix:"+verdict)
	return true, nil
}

func (r attributionRepo) CompleteWarmupSpamMove(context.Context, uuid.UUID, string) error {
	*r.log = append(*r.log, "complete")
	return nil
}

func (r attributionRepo) ListSettledWarmupSpamMoves(context.Context, time.Time, int) ([]repository.WarmupSpamMove, error) {
	return r.moves, nil
}

func (r attributionRepo) WarmupSpamMoveEvidence(context.Context, repository.WarmupSpamMove) (repository.WarmupSpamMoveEvidence, error) {
	return r.evidence, nil
}

func (r attributionRepo) CorrelatedOwnerSpamMoves(context.Context, uuid.UUID, uuid.UUID, time.Time) ([]repository.WarmupSpamMove, error) {
	return r.correlated, nil
}

func (r attributionRepo) ReattributeOwnerSpamMoves(context.Context, uuid.UUID, uuid.UUID, time.Time) error {
	*r.log = append(*r.log, "reattribute")
	return nil
}

// attributionService records the effects a verdict has, in order.
type attributionService struct {
	warmupapp.Service
	senderFails bool
	log         *[]string
}

func (s attributionService) ApplySpamReport(_ context.Context, _, _ uuid.UUID, _, reportType string) (*models.WarmupParticipantHealth, *errx.Error) {
	if s.senderFails {
		return nil, errx.InternalError()
	}
	*s.log = append(*s.log, "sender:"+reportType)
	return nil, nil
}

func (s attributionService) RecordSpamPlacement(context.Context, uuid.UUID, uuid.UUID, string, string, string, string) (*models.WarmupParticipantHealth, *errx.Error) {
	if s.senderFails {
		return nil, errx.InternalError()
	}
	*s.log = append(*s.log, "sender:spam_placement")
	return nil, nil
}

func (s attributionService) RecordTampering(_ context.Context, _ uuid.UUID, _, kind string) (*models.WarmupParticipantHealth, *errx.Error) {
	*s.log = append(*s.log, "strike:"+kind)
	return nil, nil
}

func (s attributionService) WithdrawTampering(_ context.Context, _ uuid.UUID, _, kind string) (*models.WarmupParticipantHealth, *errx.Error) {
	*s.log = append(*s.log, "withdraw:"+kind)
	return nil, nil
}

// Only an owner verdict charges the recipient; the rest read as placement
// against the sender. The verdict is fixed before any effect and the move is
// completed after all of them, so a failure part way re-applies the same
// verdict, and a correlation takes back the owner verdicts it explains first.
func TestAttributeSpamMovesAppliesTheVerdict(t *testing.T) {
	owner := repository.WarmupSpamMoveEvidence{OwnerActiveNear: true, OwnerActiveRecently: true}
	fixedProvider := spamMoveAfter(6 * time.Hour)
	fixedProvider.Verdict, fixedProvider.Signals = repository.SpamMoveProvider, []string{spamMoveOnArrival}
	cases := []struct {
		name        string
		move        repository.WarmupSpamMove
		ev          repository.WarmupSpamMoveEvidence
		correlated  int
		claimLost   bool
		senderFails bool
		wantErr     bool
		want        []string
	}{
		{name: "owner", move: spamMoveAfter(6 * time.Hour), ev: owner,
			want: []string{"fix:owner", "sender:user_complaint", "complete"}},
		{name: "provider on arrival", move: spamMoveAfter(time.Minute),
			want: []string{"fix:provider", "sender:spam_placement", "complete"}},
		{name: "unattributed", move: spamMoveAfter(6 * time.Hour), ev: repository.WarmupSpamMoveEvidence{OwnerActiveRecently: true, PatternSenders: 1},
			want: []string{"fix:unattributed", "sender:spam_placement", "complete"}},
		{name: "correlated withdraws earlier owner verdicts", move: spamMoveAfter(6 * time.Hour), ev: repository.WarmupSpamMoveEvidence{CorrelatedElsewhere: 2}, correlated: 2,
			want: []string{"fix:provider", "sender:spam_placement", "withdraw:spam_flag", "withdraw:spam_flag", "reattribute", "complete"}},
		{name: "a move another consumer holds is left alone", move: spamMoveAfter(6 * time.Hour), ev: owner, claimLost: true,
			want: nil},
		{name: "a fixed verdict is re-applied, not decided again", move: fixedProvider, ev: owner,
			want: []string{"sender:spam_placement", "complete"}},
		{name: "a failed sender write leaves the move to retry", move: spamMoveAfter(6 * time.Hour), ev: owner, senderFails: true, wantErr: true,
			want: []string{"fix:owner"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var log []string
			repo := attributionRepo{moves: []repository.WarmupSpamMove{tc.move}, evidence: tc.ev, claimLost: tc.claimLost, log: &log}
			for range tc.correlated {
				repo.correlated = append(repo.correlated, spamMoveAfter(6*time.Hour))
			}
			s := &JobsService{WarmupRepo: repo, WarmupService: attributionService{log: &log, senderFails: tc.senderFails}, EmailRepository: warmupInboxEmailRepo{}}
			err := s.attributeSpamMoves(context.Background(), time.Now())
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
			if !slices.Equal(log, tc.want) {
				t.Fatalf("effects = %v, want %v", log, tc.want)
			}
		})
	}
}

// activityRepo is a mailbox with no warmup receipt that records owner activity.
type activityRepo struct {
	repository.WarmupRepository
	noted *int
}

func (activityRepo) GetWarmupReceived(context.Context, uuid.UUID, uuid.UUID) (*repository.WarmupReceived, error) {
	return nil, nil
}

func (r activityRepo) RecordOwnerActivity(context.Context, uuid.UUID, time.Time) error {
	*r.noted++
	return nil
}

// Owner activity is a change the provider reports that our store did not
// already hold: a read made in Warmbly and echoed back is not the owner at the
// mailbox, and a label on mail that only just arrived may be a filter.
func TestOwnerActivityIsOnlyTheProvidersOwnChange(t *testing.T) {
	old := time.Now().Add(-time.Hour)
	cases := []struct {
		name   string
		stored models.EmailMessageStoreData
		add    bool
		flags  []string
		want   int
	}{
		{"read at the provider", models.EmailMessageStoreData{Flags: []string{}, InternalDate: old}, true, []string{models.FlagSeen}, 1},
		{"a read made in Warmbly echoed back", models.EmailMessageStoreData{Flags: []string{models.FlagSeen}, Seen: true, InternalDate: old}, true, []string{models.FlagSeen}, 0},
		{"read by a filter on arrival", models.EmailMessageStoreData{Flags: []string{}, InternalDate: time.Now()}, true, []string{models.FlagSeen}, 0},
		{"starred at the provider", models.EmailMessageStoreData{Flags: []string{}, Seen: true, InternalDate: old}, true, []string{models.FlagFlagged}, 1},
		{"marked unread at the provider", models.EmailMessageStoreData{Flags: []string{models.FlagSeen}, Seen: true, InternalDate: old}, false, []string{models.FlagSeen}, 1},
		{"a label no person sets", models.EmailMessageStoreData{Flags: []string{}, Seen: true, InternalDate: old}, true, []string{`\Important`}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stored := tc.stored
			s, _ := seenSyncService(&stored)
			noted := 0
			s.WarmupRepo = activityRepo{noted: &noted}
			ev := &models.JobEventFlags{UserID: uuid.New(), EmailID: uuid.New(), ID: uuid.New(), Flags: tc.flags}
			var err error
			if tc.add {
				err = s.HandleFlagsAdd(context.Background(), ev)
			} else {
				err = s.HandleFlagsRemove(context.Background(), ev)
			}
			if err != nil {
				t.Fatal(err)
			}
			if noted != tc.want {
				t.Fatalf("noted %d owner activities, want %d", noted, tc.want)
			}
		})
	}
}

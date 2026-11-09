package tasks

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type directedTaskRepo struct {
	repository.TaskRepository

	target uuid.UUID
}

func (r *directedTaskRepo) GetWarmupTask(context.Context, uuid.UUID) (*repository.WarmupTask, error) {
	return &repository.WarmupTask{TargetAccountID: &r.target}, nil
}

type directedEmailRepo struct{ repository.EmailRepository }

func (directedEmailRepo) GetByID(_ context.Context, id uuid.UUID) (*models.Email, *errx.Error) {
	return &models.Email{ID: id}, nil
}

// addressedEmailRepo answers with a real address, which the routing rules read.
type addressedEmailRepo struct {
	repository.EmailRepository

	email string
}

func (r addressedEmailRepo) GetByID(_ context.Context, id uuid.UUID) (*models.Email, *errx.Error) {
	return &models.Email{ID: id, Email: r.email}, nil
}

// pinnedGate answers the pool-pinned gate the way the real one does: a row is
// only found in the pool it is in.
type pinnedGate struct {
	warmupapp.Service

	poolOf map[uuid.UUID]string
	asked  []string
}

func (g *pinnedGate) CanParticipate(_ context.Context, id uuid.UUID, poolType string) (bool, string, *errx.Error) {
	g.asked = append(g.asked, poolType)
	if g.poolOf[id] != poolType {
		return false, "not_in_pool", nil
	}
	return true, "", nil
}

// standingRepo serves the sender's own pool row; the rest of the interface panics.
type standingRepo struct {
	repository.WarmupRepository

	state models.WarmupHealthState
}

func (r standingRepo) GetParticipantHealth(_ context.Context, _ uuid.UUID, pool string) (*models.WarmupParticipantHealth, error) {
	return &models.WarmupParticipantHealth{PoolType: pool, HealthState: r.state}, nil
}

type riskRepo struct {
	repository.OrgRiskRepository

	state models.OrgRiskState
	err   error
}

func (r riskRepo) GetOrgRiskStates(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]models.OrgRiskState, error) {
	if r.err != nil {
		return nil, r.err
	}
	out := map[uuid.UUID]models.OrgRiskState{}
	for _, id := range ids {
		out[id] = r.state
	}
	return out, nil
}

// A reply-back target borrowed from the other tier is not in the sender's
// pool; gating it only there discarded every such reply (#495).
func TestDirectedWarmupPartnerReplyAcrossTiers(t *testing.T) {
	org := uuid.New()
	cases := []struct {
		name       string
		senderPool string
		targetPool string
		risk       repository.OrgRiskRepository
		want       bool
		asked      []string
		standing   models.WarmupHealthState
	}{
		{"a paid mailbox answers a borrowed free partner", "premium", "free", riskRepo{state: models.OrgRiskTrusted}, true, []string{"premium", "free"}, models.WarmupHealthHealthy},
		{"a free mailbox answers the paid one that borrowed it", "free", "premium", riskRepo{state: models.OrgRiskTrusted}, true, []string{"free", "premium"}, models.WarmupHealthHealthy},
		{"a free mailbox on watch leaves a paying inbox unanswered", "free", "premium", riskRepo{state: models.OrgRiskTrusted}, false, []string{"free"}, models.WarmupHealthWatch},
		{"a restricted workspace may not answer into a paying inbox", "free", "premium", riskRepo{state: models.OrgRiskRestricted}, false, []string{"free"}, models.WarmupHealthHealthy},
		{"an unreadable standing fails closed", "free", "premium", riskRepo{err: errors.New("db down")}, false, []string{"free"}, models.WarmupHealthHealthy},
		{"no risk repository at all fails closed", "free", "premium", nil, false, []string{"free"}, models.WarmupHealthHealthy},
		{"a same-tier target is gated once", "premium", "premium", riskRepo{state: models.OrgRiskTrusted}, true, []string{"premium"}, models.WarmupHealthHealthy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := uuid.New()
			gate := &pinnedGate{poolOf: map[uuid.UUID]string{target: tc.targetPool}}
			s := &tasksService{
				taskRepo:     &directedTaskRepo{target: target},
				emailRepo:    directedEmailRepo{},
				warmupRepo:   standingRepo{state: tc.standing},
				warmupHealth: gate,
				orgRiskRepo:  tc.risk,
			}
			sender := &Email{ID: uuid.New(), OrganizationID: &org, WarmupPoolType: tc.senderPool}
			partner := s.directedWarmupPartner(context.Background(), uuid.New(), sender, tc.senderPool)
			if (partner != nil) != tc.want {
				t.Fatalf("partner = %v, want present=%v", partner, tc.want)
			}
			if len(gate.asked) != len(tc.asked) {
				t.Fatalf("gated in pools %v, want %v", gate.asked, tc.asked)
			}
			for i := range tc.asked {
				if gate.asked[i] != tc.asked[i] {
					t.Fatalf("gated in pools %v, want %v", gate.asked, tc.asked)
				}
			}
		})
	}
}

// withdrawnGate answers like the real gate for a pool member whose receiving was stopped.
type withdrawnGate struct {
	warmupapp.Service

	asked []string
}

func (g *withdrawnGate) CanParticipate(_ context.Context, _ uuid.UUID, poolType string) (bool, string, *errx.Error) {
	g.asked = append(g.asked, poolType)
	return false, "pool_authority_unavailable", nil
}

// A reply-back into a thread never reaches a mailbox that stopped receiving, in either tier.
func TestDirectedWarmupPartnerRefusesStoppedRecipient(t *testing.T) {
	org := uuid.New()
	target := uuid.New()
	gate := &withdrawnGate{}
	s := &tasksService{
		taskRepo:     &directedTaskRepo{target: target},
		emailRepo:    directedEmailRepo{},
		warmupRepo:   standingRepo{state: models.WarmupHealthHealthy},
		warmupHealth: gate,
		orgRiskRepo:  riskRepo{state: models.OrgRiskTrusted},
	}
	sender := &Email{ID: uuid.New(), OrganizationID: &org, WarmupPoolType: "premium"}
	if partner := s.directedWarmupPartner(context.Background(), uuid.New(), sender, "premium"); partner != nil {
		t.Fatalf("reply-back addressed a stopped mailbox: %v", partner.ID)
	}
	if len(gate.asked) != 1 {
		t.Fatalf("gated in pools %v, want only the sender's: a withdrawn recipient is not a cross-tier candidate", gate.asked)
	}

	off, diagnostic := models.TestParticipationOff, models.TestParticipationDiagnostic
	sending := &Email{ID: uuid.New(), TestMode: &diagnostic, TestSendEnabled: true, TestReceiveEnabled: true}
	for _, recipient := range []*Email{
		{ID: target, TestMode: &off},
		{ID: target, TestMode: &diagnostic, TestSendEnabled: true},
	} {
		// No repository is wired: the pair is refused on the recipient's own consent first.
		if err := (&tasksService{}).validateWarmupPair(context.Background(), sending, recipient, "premium", true); err == nil {
			t.Fatalf("dispatch admitted a recipient with receiving withdrawn (mode %v)", *recipient.TestMode)
		}
	}
}

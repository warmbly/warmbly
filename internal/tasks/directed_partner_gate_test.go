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
	}{
		{"a paid mailbox answers a borrowed free partner", "premium", "free", riskRepo{state: models.OrgRiskTrusted}, true, []string{"premium", "free"}},
		{"a free mailbox answers the paid one that borrowed it", "free", "premium", riskRepo{state: models.OrgRiskTrusted}, true, []string{"free", "premium"}},
		{"a restricted workspace may not answer into a paying inbox", "free", "premium", riskRepo{state: models.OrgRiskRestricted}, false, []string{"free"}},
		{"an unreadable standing fails closed", "free", "premium", riskRepo{err: errors.New("db down")}, false, []string{"free"}},
		{"no risk repository at all fails closed", "free", "premium", nil, false, []string{"free"}},
		{"a same-tier target is gated once", "premium", "premium", riskRepo{state: models.OrgRiskTrusted}, true, []string{"premium"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := uuid.New()
			gate := &pinnedGate{poolOf: map[uuid.UUID]string{target: tc.targetPool}}
			s := &tasksService{
				taskRepo:     &directedTaskRepo{target: target},
				emailRepo:    directedEmailRepo{},
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

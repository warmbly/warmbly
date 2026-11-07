// Package nodescope binds a worker node's internal calls to what it is assigned.
package nodescope

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// scopeTTL is how long a node's loaded assignment is reused before it is read again.
	scopeTTL = 30 * time.Second
	// grantTTL keeps an organization in a worker's scope after the control plane hands it unassigned work (a credential check).
	grantTTL = 10 * time.Minute
)

// ReleaseGrace is how long a mailbox stays in its previous worker's scope after a move.
const ReleaseGrace = time.Duration(config.WorkerReleaseGraceMinutes) * time.Minute

// scope is one node's loaded assignment.
type scope struct {
	role      models.NodeRole
	mailboxes map[uuid.UUID]uuid.UUID // mailbox -> user
	orgs      map[uuid.UUID]struct{}
	keys      map[string]struct{}
	loadedAt  time.Time
}

// exempt is a consumer, which serves every organization; a worker or a node with no row is bound.
func (sc *scope) exempt() bool { return sc.role == models.NodeRoleConsumer }

type entry struct {
	mu    sync.Mutex
	scope *scope
}

// Service answers whether a node may act on what a call names.
type Service struct {
	repo   repository.NodeScopeRepository
	grants redis.Cmdable
	nodes  sync.Map // uuid.UUID -> *entry
	now    func() time.Time
}

// New builds the service; grants may be nil, which grants nothing beyond assignment.
func New(repo repository.NodeScopeRepository, grants redis.Cmdable) *Service {
	return &Service{repo: repo, grants: grants, now: time.Now}
}

func grantKey(worker, org uuid.UUID) string {
	return "node_scope_grant:" + worker.String() + ":" + org.String()
}

// Grant puts org in worker's scope for a while; called before the control plane hands that worker unassigned work for it.
func Grant(ctx context.Context, rdb redis.Cmdable, worker, org uuid.UUID) error {
	if rdb == nil || worker == uuid.Nil || org == uuid.Nil {
		return nil
	}
	return rdb.Set(ctx, grantKey(worker, org), "1", grantTTL).Err()
}

func (s *Service) granted(ctx context.Context, worker, org uuid.UUID) (bool, error) {
	if s.grants == nil {
		return false, nil
	}
	n, err := s.grants.Exists(ctx, grantKey(worker, org)).Result()
	return n > 0, err
}

func (s *Service) load(ctx context.Context, node uuid.UUID) (*scope, error) {
	role, found, err := s.repo.NodeRole(ctx, node)
	if err != nil {
		return nil, err
	}
	sc := &scope{role: role, mailboxes: map[uuid.UUID]uuid.UUID{}, orgs: map[uuid.UUID]struct{}{}, keys: map[string]struct{}{}, loadedAt: s.now()}
	if !found || role != models.NodeRoleWorker {
		return sc, nil
	}
	boxes, err := s.repo.WorkerMailboxes(ctx, node, ReleaseGrace)
	if err != nil {
		return nil, err
	}
	orgIDs := make([]uuid.UUID, 0, len(boxes))
	for _, b := range boxes {
		sc.mailboxes[b.ID] = b.UserID
		if b.OrganizationID != nil {
			if _, seen := sc.orgs[*b.OrganizationID]; !seen {
				sc.orgs[*b.OrganizationID] = struct{}{}
				orgIDs = append(orgIDs, *b.OrganizationID)
			}
		}
	}
	keys, err := s.repo.OrganizationDataKeys(ctx, orgIDs)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		sc.keys[k] = struct{}{}
	}
	return sc, nil
}

// check answers from the node's cached scope, and on a miss asks the database directly with point, so an assignment made since the last load is seen at once.
func (s *Service) check(ctx context.Context, node uuid.UUID, cached func(*scope) bool, point func(context.Context) (bool, error)) (bool, error) {
	v, _ := s.nodes.LoadOrStore(node, &entry{})
	e := v.(*entry)
	e.mu.Lock()
	if e.scope == nil || s.now().Sub(e.scope.loadedAt) >= scopeTTL {
		sc, err := s.load(ctx, node)
		if err != nil {
			e.mu.Unlock()
			return false, err
		}
		e.scope = sc
	}
	sc := e.scope
	e.mu.Unlock()
	if sc.exempt() || cached(sc) {
		return true, nil
	}
	if point == nil {
		return false, nil
	}
	ok, err := point(ctx)
	if ok && err == nil {
		// The cached scope is behind; the next call reloads it.
		e.mu.Lock()
		if e.scope == sc {
			e.scope = nil
		}
		e.mu.Unlock()
	}
	return ok, err
}

// Worker reports whether a call naming workerID may come from node.
func (s *Service) Worker(ctx context.Context, node, workerID uuid.UUID) (bool, error) {
	return s.check(ctx, node, func(*scope) bool { return node == workerID }, nil)
}

// Mailbox reports whether node runs mailbox (for user, when user is set), now or within the release grace.
func (s *Service) Mailbox(ctx context.Context, node, user, mailbox uuid.UUID) (bool, error) {
	return s.check(ctx, node, func(sc *scope) bool {
		owner, held := sc.mailboxes[mailbox]
		return held && (user == uuid.Nil || owner == user)
	}, func(ctx context.Context) (bool, error) {
		if mailbox == uuid.Nil {
			return false, nil
		}
		return s.repo.WorkerHoldsMailbox(ctx, node, user, mailbox, ReleaseGrace)
	})
}

// Organization reports whether node runs a mailbox of org, or was granted it for unassigned work.
func (s *Service) Organization(ctx context.Context, node, org uuid.UUID) (bool, error) {
	return s.check(ctx, node, func(sc *scope) bool {
		_, held := sc.orgs[org]
		return held
	}, func(ctx context.Context) (bool, error) {
		return s.holdsOrganization(ctx, node, org)
	})
}

func (s *Service) holdsOrganization(ctx context.Context, node, org uuid.UUID) (bool, error) {
	if org == uuid.Nil {
		return false, nil
	}
	if ok, err := s.granted(ctx, node, org); err != nil || ok {
		return ok, err
	}
	return s.repo.WorkerHoldsOrganization(ctx, node, org, ReleaseGrace)
}

// DataKey reports whether the sealed data key belongs to an organization in node's scope.
func (s *Service) DataKey(ctx context.Context, node uuid.UUID, sealed string) (bool, error) {
	return s.check(ctx, node, func(sc *scope) bool {
		_, held := sc.keys[sealed]
		return held
	}, func(ctx context.Context) (bool, error) {
		org, found, err := s.repo.DataKeyOrganization(ctx, sealed)
		if err != nil || !found {
			return false, err
		}
		return s.holdsOrganization(ctx, node, org)
	})
}

// BlobKey reports whether a key that names a mailbox or campaign names one in node's scope; keys naming neither pass.
func (s *Service) BlobKey(ctx context.Context, node uuid.UUID, key string) (bool, error) {
	parts := strings.Split(key, "/")
	switch parts[0] {
	case "users":
		// users/<user>/emails/<mailbox>/...
		if len(parts) < 5 || parts[2] != "emails" {
			return s.workerless(ctx, node)
		}
		user, uerr := uuid.Parse(parts[1])
		mailbox, merr := uuid.Parse(parts[3])
		if uerr != nil || merr != nil {
			return s.workerless(ctx, node)
		}
		return s.Mailbox(ctx, node, user, mailbox)
	case "attachments":
		// attachments/<campaign>/<name>
		if len(parts) < 3 {
			return s.workerless(ctx, node)
		}
		campaign, err := uuid.Parse(parts[1])
		if err != nil {
			return s.workerless(ctx, node)
		}
		org, found, err := s.repo.CampaignOrganization(ctx, campaign)
		if err != nil {
			return false, err
		}
		if !found {
			return s.workerless(ctx, node)
		}
		return s.Organization(ctx, node, org)
	}
	return true, nil
}

// workerless allows a non-worker node and refuses a worker.
func (s *Service) workerless(ctx context.Context, node uuid.UUID) (bool, error) {
	return s.check(ctx, node, func(*scope) bool { return false }, nil)
}

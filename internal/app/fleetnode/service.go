// Package fleetnode is the control-plane half of the pull-based fleet.
//
// A node joins by running one command with the instance join token, then
// heartbeats forever. It is never reached into: everything the control plane
// wants a node to do comes back in the heartbeat reply, which today is exactly
// one instruction — what version to be running.
package fleetnode

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
	"github.com/warmbly/warmbly/internal/repository"
)

var (
	// ErrNoJoinToken means no token has ever been issued, so nothing may join.
	// Deliberately distinct from a wrong token: an instance with no token is a
	// setup problem, not an attack.
	ErrNoJoinToken = errors.New("no join token has been issued for this instance")
	ErrBadToken    = errors.New("join token is not valid")
	ErrBadRole     = errors.New("unknown node role")
	// ErrRoleChanged is a node claiming an id that is already registered under
	// the other role. Silently accepting it would leave a worker's mailboxes
	// assigned to a machine that has stopped doing worker work.
	ErrRoleChanged = errors.New("that node id is already registered under a different role")
)

type Service struct {
	nodes    repository.FleetNodeRepository
	workers  repository.WorkerRepository
	settings repository.FleetSettingsRepository

	// variant is appended to every version this service hands a node, because
	// a version names an image and some builds of an image are not
	// interchangeable. See imageVariant.
	variant string
}

func New(
	nodes repository.FleetNodeRepository,
	workers repository.WorkerRepository,
	settings repository.FleetSettingsRepository,
) *Service {
	return &Service{nodes: nodes, workers: workers, settings: settings, variant: imageVariant()}
}

// imageVariant is the tag suffix a node must add to reach an image that can
// talk to this instance's event bus.
//
// The default images are CGO-free and carry no librdkafka, so a node running
// one cannot speak Kafka at all: it would take EVENTBUS_PROVIDER=kafka from
// its rendered env and fail at boot. The Kafka builds are published under the
// same image name with a "-kafka" tag suffix, so the fix is to name that tag,
// and the control plane is the only side that knows which bus it runs.
//
// FLEET_IMAGE_VARIANT overrides it for anyone publishing their own images
// under a different convention. Set and empty means "no suffix", which is how
// an operator whose own Kafka build is tagged plainly opts out.
func imageVariant() string {
	if v, ok := os.LookupEnv("FLEET_IMAGE_VARIANT"); ok {
		return v
	}
	if strings.EqualFold(os.Getenv("EVENTBUS_PROVIDER"), "kafka") {
		return "-kafka"
	}
	return ""
}

// withVariant appends the image variant to a resolved version. An empty
// version stays empty: "no opinion" must never become a bare "-kafka", which
// the node would dutifully try to pull.
func (s *Service) withVariant(version string) string {
	if version == "" || s.variant == "" || strings.HasSuffix(version, s.variant) {
		return version
	}
	return version + s.variant
}

// IssueJoinToken mints a new instance join token, stores only its hash, and
// returns the plaintext. The caller must show it once: it cannot be recovered.
//
// Issuing replaces any previous token, which is also how you revoke one.
// Existing nodes are unaffected — they are already enrolled, and the token
// only gates joining.
func (s *Service) IssueJoinToken(ctx context.Context) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	if err := s.settings.SetJoinTokenHash(ctx, crypt.SHA256(token)); err != nil {
		return "", err
	}
	return token, nil
}

// VerifyJoinToken checks a presented token in constant time.
func (s *Service) VerifyJoinToken(ctx context.Context, token string) error {
	want, err := s.settings.GetJoinTokenHash(ctx)
	if err != nil {
		return err
	}
	if want == "" {
		return ErrNoJoinToken
	}
	got := crypt.SHA256(strings.TrimSpace(token))
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return ErrBadToken
	}
	return nil
}

// Heartbeat records a beat and answers with what the node should be running.
//
// Registration is the first heartbeat: there is no separate create step, so a
// node rebuilt from scratch simply reappears under the same id. A node that
// declares itself a worker also gets its placement row, because placement
// reads `workers` and a node with no row there would be invisible to it.
func (s *Service) Heartbeat(ctx context.Context, beat models.NodeHeartbeat) (*models.NodeHeartbeatReply, error) {
	if !beat.Role.Valid() {
		return nil, ErrBadRole
	}
	if beat.NodeID == uuid.Nil {
		return nil, errors.New("node_id required")
	}

	// A node may not change what it does under the same id. Checked before the
	// upsert so the row is never half-migrated between roles.
	if existing, err := s.nodes.Get(ctx, beat.NodeID); err == nil && existing != nil && existing.Role != beat.Role {
		return nil, ErrRoleChanged
	}

	if beat.Stopping {
		// The farewell beat. Go inactive at once rather than staying selectable
		// until the beat ages out; placement must not hand work to a process
		// that has already gone.
		if err := s.nodes.Deactivate(ctx, beat.NodeID); err != nil {
			return nil, err
		}
		return &models.NodeHeartbeatReply{LivenessSeconds: int(models.NodeLivenessWindow.Seconds())}, nil
	}

	if err := s.nodes.UpsertOnHeartbeat(ctx, beat); err != nil {
		return nil, err
	}
	if beat.Role == models.NodeRoleWorker && s.workers != nil {
		if err := s.workers.EnsureWorkerRow(ctx, beat.NodeID); err != nil {
			return nil, err
		}
	}

	reply := &models.NodeHeartbeatReply{
		LivenessSeconds: int(models.NodeLivenessWindow.Seconds()),
	}
	reply.DesiredVersion = s.desiredVersion(ctx, beat.NodeID)
	return reply, nil
}

// desiredVersion resolves what this node should run: its own pin if it has
// one, otherwise the fleet-wide resolved release.
//
// Any failure answers "" — no opinion. A node that cannot be told what to run
// must keep running what it has, because the alternative is a control-plane
// hiccup rolling the whole fleet.
func (s *Service) desiredVersion(ctx context.Context, nodeID uuid.UUID) string {
	if node, err := s.nodes.Get(ctx, nodeID); err == nil && node != nil && node.PinnedVersion != "" {
		return s.withVariant(node.PinnedVersion)
	}
	state, err := s.settings.GetRelease(ctx)
	if err != nil {
		return ""
	}
	return s.withVariant(state.DesiredVersion())
}

// DefaultJoinTag is what a machine joining an instance with no resolved
// release is told to run. It has to be a tag this project actually publishes:
// `latest` is not one, and a node sent there fails on the image pull before it
// ever heartbeats. The floating release tag is `prod`.
const DefaultJoinTag = "prod"

// JoinVersion is what a machine joining right now should start on.
//
// It differs from the heartbeat's answer in exactly one way. To a node that is
// already running something, an unresolved release means "no opinion" and must
// stay empty, because the alternative is a control-plane hiccup rolling the
// fleet. A joining node has nothing to keep running, so it has to be told a
// tag, and the fallback is the published floating one.
func (s *Service) JoinVersion(ctx context.Context, nodeID uuid.UUID) string {
	if v := s.desiredVersion(ctx, nodeID); v != "" {
		return v
	}
	return s.withVariant(DefaultJoinTag)
}

// List returns the fleet, with each node's resolved target attached so a
// caller can see at a glance which machines are behind.
func (s *Service) List(ctx context.Context, role models.NodeRole) ([]models.FleetNode, error) {
	nodes, err := s.nodes.List(ctx, role)
	if err != nil {
		return nil, err
	}
	state, err := s.settings.GetRelease(ctx)
	if err != nil {
		return nil, err
	}
	fleetTarget := s.withVariant(state.DesiredVersion())
	for i := range nodes {
		if nodes[i].PinnedVersion != "" {
			nodes[i].DesiredVersion = s.withVariant(nodes[i].PinnedVersion)
			continue
		}
		nodes[i].DesiredVersion = fleetTarget
	}
	return nodes, nil
}

// Get returns one node with its resolved target attached.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*models.FleetNode, error) {
	node, err := s.nodes.Get(ctx, id)
	if err != nil || node == nil {
		return nil, err
	}
	if node.PinnedVersion != "" {
		node.DesiredVersion = s.withVariant(node.PinnedVersion)
		return node, nil
	}
	state, err := s.settings.GetRelease(ctx)
	if err != nil {
		return nil, err
	}
	node.DesiredVersion = s.withVariant(state.DesiredVersion())
	return node, nil
}

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
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
	"github.com/warmbly/warmbly/internal/repository"
	buildversion "github.com/warmbly/warmbly/internal/version"
)

var (
	// ErrNoJoinToken means no token has ever been issued, so nothing may join.
	// Deliberately distinct from a wrong token: an instance with no token is a
	// setup problem, not an attack.
	ErrNoJoinToken = errors.New("no join token has been issued for this instance")
	ErrBadToken    = errors.New("join token is not valid")
	// ErrJoinTokenExpired is the right token past its expiry.
	ErrJoinTokenExpired = errors.New("join token has expired")
	// ErrJoinTokenTTL is a requested lifetime above MaxJoinTokenTTL.
	ErrJoinTokenTTL = errors.New("a join token can be valid for at most 30 days")
	ErrBadRole      = errors.New("unknown node role")
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

	// now is the clock rollouts are judged by; nil means time.Now.
	now func() time.Time
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
	readBoth, _ := strconv.ParseBool(os.Getenv("EVENTBUS_READ_BOTH"))
	readBothCodecs, _ := strconv.ParseBool(os.Getenv("CODEC_READ_BOTH"))
	if strings.EqualFold(os.Getenv("EVENTBUS_PROVIDER"), "kafka") || readBoth || readBothCodecs {
		return "-kafka"
	}
	return ""
}

// withVariant appends the image variant to a resolved version. An empty
// version stays empty: "no opinion" must never become a bare "-kafka", which
// the node would dutifully try to pull.
func (s *Service) withVariant(version string) string {
	if version == "" || s.CheckTarget(version) != nil {
		return ""
	}
	return buildversion.FleetImageTag(strings.TrimSuffix(version, s.variant)) + s.variant
}

func (s *Service) CheckTarget(target string) error {
	if s.variant != "" {
		target = strings.TrimSuffix(target, s.variant)
	}
	return buildversion.CheckFleetTarget(buildversion.String(), target)
}

const (
	// DefaultJoinTokenTTL is how long a join token works when none is asked for.
	DefaultJoinTokenTTL = 7 * 24 * time.Hour
	// MaxJoinTokenTTL bounds how long any join token may work.
	MaxJoinTokenTTL = 30 * 24 * time.Hour
)

// IssueJoinToken mints a new instance join token, stores only its hash and
// expiry, and returns the plaintext. The caller must show it once: it cannot
// be recovered.
//
// The token joins any number of machines until it expires. Issuing replaces
// any previous token, which is also how you revoke one. Existing nodes are
// unaffected: they are already enrolled, and the token only gates joining.
func (s *Service) IssueJoinToken(ctx context.Context, ttl time.Duration) (string, time.Time, error) {
	if ttl <= 0 {
		ttl = DefaultJoinTokenTTL
	}
	if ttl > MaxJoinTokenTTL {
		return "", time.Time{}, ErrJoinTokenTTL
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expiresAt := time.Now().Add(ttl).UTC().Truncate(time.Second)
	if err := s.settings.SetJoinToken(ctx, crypt.SHA256(token), expiresAt); err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

// VerifyJoinToken checks a presented token in constant time, then its expiry.
// A token stored without an expiry is refused as expired.
func (s *Service) VerifyJoinToken(ctx context.Context, token string) error {
	want, expiresAt, err := s.settings.GetJoinToken(ctx)
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
	if expiresAt == nil || !time.Now().Before(*expiresAt) {
		return ErrJoinTokenExpired
	}
	return nil
}

// ErrNoNodeCredential is a node that holds no credential of its own yet; joining again issues one.
var ErrNoNodeCredential = errors.New("this node holds no node credential; join it again to receive one")

// ErrBadNodeCredential is a credential that was not issued to the node it names.
var ErrBadNodeCredential = errors.New("node credential does not match the node")

// IssueNodeCredential mints the node's own credential and stores only its hash, replacing any earlier one.
func (s *Service) IssueNodeCredential(ctx context.Context, id uuid.UUID) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	secret := base64.RawURLEncoding.EncodeToString(raw)
	if err := s.nodes.SetCredentialHash(ctx, id, crypt.SHA256(secret)); err != nil {
		return "", err
	}
	return secret, nil
}

// VerifyNodeCredential checks, in constant time, that secret is the credential issued to node id.
func (s *Service) VerifyNodeCredential(ctx context.Context, id uuid.UUID, secret string) error {
	want, err := s.nodes.CredentialHash(ctx, id)
	if err != nil {
		return err
	}
	if want == "" {
		return ErrNoNodeCredential
	}
	if subtle.ConstantTimeCompare([]byte(crypt.SHA256(strings.TrimSpace(secret))), []byte(want)) != 1 {
		return ErrBadNodeCredential
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
	reply.DesiredVersion, reply.DesiredDigest = s.desired(ctx, beat.NodeID)
	return reply, nil
}

// UpdateOnly answers a beat from a node still on INTERNAL_API_TOKEN with its
// version and records nothing: the node is not marked live and nothing it
// reports is stored. Only an enrolled node is told a version.
func (s *Service) UpdateOnly(ctx context.Context, beat models.NodeHeartbeat) (*models.NodeHeartbeatReply, error) {
	if !beat.Role.Valid() {
		return nil, ErrBadRole
	}
	if beat.NodeID == uuid.Nil {
		return nil, errors.New("node_id required")
	}
	reply := &models.NodeHeartbeatReply{LivenessSeconds: int(models.NodeLivenessWindow.Seconds())}
	if beat.Stopping {
		return reply, nil
	}
	if node, err := s.nodes.Get(ctx, beat.NodeID); err != nil || node == nil || node.Role != beat.Role {
		return reply, nil
	}
	reply.DesiredVersion = s.desiredVersion(ctx, beat.NodeID)
	return reply, nil
}

// desiredVersion resolves what this node should run: its own pin if it has
// one, otherwise the fleet-wide resolved release, or a staged rollout's target
// once the node has been admitted to it.
//
// Any failure answers "" — no opinion. A node that cannot be told what to run
// must keep running what it has, because the alternative is a control-plane
// hiccup rolling the whole fleet.
func (s *Service) desiredVersion(ctx context.Context, nodeID uuid.UUID) string {
	v, _ := s.desired(ctx, nodeID)
	return v
}

func (s *Service) desired(ctx context.Context, nodeID uuid.UUID) (version, digest string) {
	node, err := s.nodes.Get(ctx, nodeID)
	if err == nil && node != nil && node.PinnedVersion != "" {
		return s.withVariant(node.PinnedVersion), ""
	}
	state, err := s.settings.GetRelease(ctx)
	if err != nil {
		return "", ""
	}
	role := models.NodeRole("")
	if node != nil {
		role = node.Role
	}
	return s.resolve(state, nodeID, role)
}

// resolve answers what an unpinned node runs under state. A halted rollout
// answers Tag, which is From, so admitted nodes roll themselves back.
func (s *Service) resolve(state *models.FleetReleaseState, nodeID uuid.UUID, role models.NodeRole) (version, digest string) {
	if state == nil {
		return "", ""
	}
	if r := state.Rollout; r.Running() && r.IsAdmitted(nodeID) {
		version = s.withVariant(r.Target)
		if version != "" && role != "" {
			digest = validDigest(r.Digests[string(role)+s.variant])
		}
		return version, digest
	}
	return s.withVariant(state.DesiredVersion()), ""
}

func validDigest(d string) string {
	if len(d) != len("sha256:")+64 || !strings.HasPrefix(d, "sha256:") {
		return ""
	}
	for _, c := range d[len("sha256:"):] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return ""
		}
	}
	return d
}

// JoinVersion falls back to the backend's exact release, never a floating tag.
func (s *Service) JoinVersion(ctx context.Context, nodeID uuid.UUID) string {
	if v := s.desiredVersion(ctx, nodeID); v != "" {
		return v
	}
	return s.withVariant(buildversion.FleetImageTag(buildversion.String()))
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
	for i := range nodes {
		if nodes[i].PinnedVersion != "" {
			nodes[i].DesiredVersion = s.withVariant(nodes[i].PinnedVersion)
			continue
		}
		nodes[i].DesiredVersion, _ = s.resolve(state, nodes[i].ID, nodes[i].Role)
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
	node.DesiredVersion, _ = s.resolve(state, node.ID, node.Role)
	return node, nil
}

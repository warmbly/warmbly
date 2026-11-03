package fleetnode

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// os_Unsetenv clears a variable for the duration of the test; t.Setenv already
// restores whatever was there.
func os_Unsetenv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}

// Only the two reads the version resolution makes are implemented; anything
// else would panic, which is the point.
type stubNodes struct {
	repository.FleetNodeRepository
	node *models.FleetNode
}

func (s stubNodes) Get(context.Context, uuid.UUID) (*models.FleetNode, error) {
	return s.node, nil
}

func (s stubNodes) List(context.Context, models.NodeRole) ([]models.FleetNode, error) {
	return []models.FleetNode{*s.node}, nil
}

type stubSettings struct {
	repository.FleetSettingsRepository
	release string
}

func (s stubSettings) GetRelease(context.Context) (*models.FleetReleaseState, error) {
	return &models.FleetReleaseState{Tag: s.release}, nil
}

func TestImageVariant(t *testing.T) {
	cases := []struct {
		name     string
		bus      string
		override string
		hasOver  bool
		want     string
	}{
		{name: "nats gets no suffix", bus: "nats", want: ""},
		{name: "unset bus gets no suffix", want: ""},
		{name: "kafka gets the kafka suffix", bus: "kafka", want: "-kafka"},
		{name: "kafka is matched case insensitively", bus: "KAFKA", want: "-kafka"},
		{name: "an explicit override wins", bus: "kafka", override: "-confluent", hasOver: true, want: "-confluent"},
		// Set and empty is how an operator whose own Kafka build is tagged
		// plainly opts out; it must not fall through to the default.
		{name: "an explicit empty override disables it", bus: "kafka", override: "", hasOver: true, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("EVENTBUS_PROVIDER", tc.bus)
			if tc.hasOver {
				t.Setenv("FLEET_IMAGE_VARIANT", tc.override)
			} else {
				os_Unsetenv(t, "FLEET_IMAGE_VARIANT")
			}
			if got := imageVariant(); got != tc.want {
				t.Fatalf("imageVariant() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWithVariant(t *testing.T) {
	t.Setenv("WARMBLY_VERSION", "v1.2.3")
	s := &Service{variant: "-kafka"}
	cases := []struct{ in, want string }{
		{"v0.4.5", "v0.4.5-kafka"},
		// "No opinion" must stay no opinion. A bare "-kafka" is a tag the node
		// would dutifully try to pull.
		{"", ""},
		// Idempotent, so a pin an operator already wrote with the suffix does
		// not become v0.4.5-kafka-kafka.
		{"v0.4.5-kafka", "v0.4.5-kafka"},
	}
	for _, tc := range cases {
		if got := s.withVariant(tc.in); got != tc.want {
			t.Errorf("withVariant(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	plain := &Service{variant: ""}
	if got := plain.withVariant("v0.4.5"); got != "v0.4.5" {
		t.Errorf("no variant should pass the version through, got %q", got)
	}
}

func TestMigrationRetainsKafkaWorkerImages(t *testing.T) {
	os_Unsetenv(t, "FLEET_IMAGE_VARIANT")
	t.Setenv("EVENTBUS_PROVIDER", "nats")
	for _, setting := range []string{"EVENTBUS_READ_BOTH", "CODEC_READ_BOTH"} {
		t.Setenv("EVENTBUS_READ_BOTH", "false")
		t.Setenv("CODEC_READ_BOTH", "false")
		t.Setenv(setting, "true")
		if got := imageVariant(); got != "-kafka" {
			t.Fatalf("%s selected image without Kafka support: %q", setting, got)
		}
	}
}

// A pin is the sharp edge: an operator canarying a node types "v0.4.4", and
// that has to reach the machine as the image the machine can actually run.
func TestDesiredVersionAppliesVariantToPinsAndFleetTarget(t *testing.T) {
	t.Setenv("WARMBLY_VERSION", "v1.2.3")
	id := uuid.New()
	settings := stubSettings{release: "v0.4.5"}

	pinned := &Service{
		nodes:    stubNodes{node: &models.FleetNode{ID: id, PinnedVersion: "v0.4.4"}},
		settings: settings,
		variant:  "-kafka",
	}
	if got := pinned.desiredVersion(context.Background(), id); got != "v0.4.4-kafka" {
		t.Errorf("pinned node: got %q, want v0.4.4-kafka", got)
	}

	unpinned := &Service{
		nodes:    stubNodes{node: &models.FleetNode{ID: id}},
		settings: settings,
		variant:  "-kafka",
	}
	if got := unpinned.desiredVersion(context.Background(), id); got != "v0.4.5-kafka" {
		t.Errorf("unpinned node: got %q, want v0.4.5-kafka", got)
	}
}

// A joining node has nothing installed, so "no opinion" is not an answer it
// can act on. It used to fall through to join.sh's `latest`, which this
// project has never published, so the machine died on the image pull.
func TestJoinVersionFallsBackToAPublishedTag(t *testing.T) {
	t.Setenv("WARMBLY_VERSION", "v1.2.3")
	id := uuid.New()

	unresolved := &Service{
		nodes:    stubNodes{node: &models.FleetNode{ID: id}},
		settings: stubSettings{release: ""},
		variant:  "-kafka",
	}
	if got := unresolved.JoinVersion(context.Background(), id); got != "v1.2.3-kafka" {
		t.Errorf("unresolved release: got %q, want v1.2.3-kafka", got)
	}

	plain := &Service{
		nodes:    stubNodes{node: &models.FleetNode{ID: id}},
		settings: stubSettings{release: ""},
	}
	if got := plain.JoinVersion(context.Background(), id); got != "v1.2.3" {
		t.Errorf("unresolved release, no variant: got %q, want v1.2.3", got)
	}

	// A resolved release still wins; the fallback is only for having nothing.
	resolved := &Service{
		nodes:    stubNodes{node: &models.FleetNode{ID: id}},
		settings: stubSettings{release: "v0.4.5"},
		variant:  "-kafka",
	}
	if got := resolved.JoinVersion(context.Background(), id); got != "v0.4.5-kafka" {
		t.Errorf("resolved release: got %q, want v0.4.5-kafka", got)
	}

	// And a pin still wins over both.
	pinned := &Service{
		nodes:    stubNodes{node: &models.FleetNode{ID: id, PinnedVersion: "v0.4.4"}},
		settings: stubSettings{release: "v0.4.5"},
		variant:  "-kafka",
	}
	if got := pinned.JoinVersion(context.Background(), id); got != "v0.4.4-kafka" {
		t.Errorf("pinned node: got %q, want v0.4.4-kafka", got)
	}
}

// The heartbeat must NOT gain the fallback: a node that is already running
// something has to be left alone when the control plane cannot resolve a
// release, or one hiccup rolls the whole fleet.
func TestHeartbeatKeepsNoOpinionEmpty(t *testing.T) {
	id := uuid.New()
	s := &Service{
		nodes:    stubNodes{node: &models.FleetNode{ID: id}},
		settings: stubSettings{release: ""},
		variant:  "-kafka",
	}
	if got := s.desiredVersion(context.Background(), id); got != "" {
		t.Errorf("unresolved release must stay empty on the heartbeat, got %q", got)
	}
}

func TestStoredTargetsCannotUpgradeAheadOfBackend(t *testing.T) {
	t.Setenv("WARMBLY_VERSION", "v0.6.31")
	id := uuid.New()
	for _, pin := range []string{"", "v0.6.32", "v0.6.32-kafka"} {
		s := &Service{
			nodes:    stubNodes{node: &models.FleetNode{ID: id, PinnedVersion: pin}},
			settings: stubSettings{release: "v0.6.32"}, variant: "-kafka",
		}
		if got := s.desiredVersion(context.Background(), id); got != "" {
			t.Fatalf("pin %q instructs incompatible upgrade %q", pin, got)
		}
		if got := s.JoinVersion(context.Background(), id); got != "v0.6.31-kafka" {
			t.Fatalf("join must use backend release, got %q", got)
		}
	}
}

func TestUnstampedBackendCannotChooseFloatingJoinImage(t *testing.T) {
	t.Setenv("WARMBLY_VERSION", "")
	id := uuid.New()
	s := &Service{nodes: stubNodes{node: &models.FleetNode{ID: id}}, settings: stubSettings{}}
	if got := s.JoinVersion(context.Background(), id); got != "" {
		t.Fatalf("unknown backend must not choose image %q", got)
	}
}

func TestMainBuildJoinsItsPublishedSHAImage(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	t.Setenv("WARMBLY_VERSION", "dev-"+sha)
	id := uuid.New()
	s := &Service{nodes: stubNodes{node: &models.FleetNode{ID: id}}, settings: stubSettings{}, variant: "-kafka"}
	if got := s.JoinVersion(context.Background(), id); got != sha+"-kafka" {
		t.Fatalf("main build must use published SHA tag, got %q", got)
	}
	for _, pin := range []string{"dev-" + sha, "dev-" + sha + "-kafka", sha + "-kafka"} {
		s.nodes = stubNodes{node: &models.FleetNode{ID: id, PinnedVersion: pin}}
		if got := s.desiredVersion(context.Background(), id); got != sha+"-kafka" {
			t.Fatalf("pin %q must use published SHA tag, got %q", pin, got)
		}
	}
}

// List and Get feed the admin panel's "is this machine behind" column. A node
// reports the WARMBLY_VERSION the join script wrote, which already carries the
// suffix, so the target it is compared against has to carry it too or every
// Kafka node reads as permanently out of date.
func TestListAndGetAttachTheVariant(t *testing.T) {
	t.Setenv("WARMBLY_VERSION", "v1.2.3")
	id := uuid.New()
	s := &Service{
		nodes:    stubNodes{node: &models.FleetNode{ID: id}},
		settings: stubSettings{release: "v0.4.5"},
		variant:  "-kafka",
	}
	nodes, err := s.List(context.Background(), models.NodeRoleWorker)
	if err != nil {
		t.Fatal(err)
	}
	if nodes[0].DesiredVersion != "v0.4.5-kafka" {
		t.Errorf("List: got %q, want v0.4.5-kafka", nodes[0].DesiredVersion)
	}
	node, err := s.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if node.DesiredVersion != "v0.4.5-kafka" {
		t.Errorf("Get: got %q, want v0.4.5-kafka", node.DesiredVersion)
	}
}

// An update-only beat must write nothing: stubNodes panics on any write.
func TestUpdateOnlyTellsTheVersionAndRecordsNothing(t *testing.T) {
	t.Setenv("WARMBLY_VERSION", "v1.2.3")
	id := uuid.New()
	s := &Service{
		nodes:    stubNodes{node: &models.FleetNode{ID: id, Role: models.NodeRoleWorker}},
		settings: stubSettings{release: "v1.2.3"},
		variant:  "-kafka",
	}
	ctx := context.Background()
	reply, err := s.UpdateOnly(ctx, models.NodeHeartbeat{NodeID: id, Role: models.NodeRoleWorker, Booted: true})
	if err != nil || reply.DesiredVersion != "v1.2.3-kafka" || reply.LivenessSeconds == 0 {
		t.Fatalf("enrolled node: reply=%+v err=%v", reply, err)
	}
	if reply, _ := s.UpdateOnly(ctx, models.NodeHeartbeat{NodeID: id, Role: models.NodeRoleWorker, Stopping: true}); reply.DesiredVersion != "" {
		t.Errorf("a farewell beat is told nothing, got %q", reply.DesiredVersion)
	}
	if reply, _ := s.UpdateOnly(ctx, models.NodeHeartbeat{NodeID: id, Role: models.NodeRoleConsumer}); reply.DesiredVersion != "" {
		t.Errorf("a beat claiming another role is told nothing, got %q", reply.DesiredVersion)
	}
	unknown := &Service{nodes: stubNodes{}, settings: stubSettings{release: "v1.2.3"}}
	if reply, _ := unknown.UpdateOnly(ctx, models.NodeHeartbeat{NodeID: uuid.New(), Role: models.NodeRoleWorker}); reply.DesiredVersion != "" {
		t.Errorf("an unenrolled node is told nothing, got %q", reply.DesiredVersion)
	}
	if _, err := s.UpdateOnly(ctx, models.NodeHeartbeat{Role: models.NodeRoleWorker}); err == nil {
		t.Error("a beat without a node id is refused")
	}
}

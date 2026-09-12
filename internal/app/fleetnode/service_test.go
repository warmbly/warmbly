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

// A pin is the sharp edge: an operator canarying a node types "v0.4.4", and
// that has to reach the machine as the image the machine can actually run.
func TestDesiredVersionAppliesVariantToPinsAndFleetTarget(t *testing.T) {
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

// List and Get feed the admin panel's "is this machine behind" column. A node
// reports the WARMBLY_VERSION the join script wrote, which already carries the
// suffix, so the target it is compared against has to carry it too or every
// Kafka node reads as permanently out of date.
func TestListAndGetAttachTheVariant(t *testing.T) {
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

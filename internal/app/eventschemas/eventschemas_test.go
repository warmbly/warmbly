package eventschemas

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iskorotkov/avro/v2"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/models"
)

var update = flag.Bool("update", false, "record the current schemas as the snapshot (make schemas)")

// snapshotPath is the recorded schema for topic, as the registry last accepted it.
func snapshotPath(topic string) string {
	name := topic
	if topic == WorkerCommands {
		name = "worker-commands"
	}
	return filepath.Join("testdata", name+".avsc")
}

func document(t *testing.T, s avro.Schema) []byte {
	t.Helper()
	doc, err := models.SchemaDocument(s)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, doc, "", "  "); err != nil {
		t.Fatal(err)
	}
	return append(out.Bytes(), '\n')
}

// TestPublishedSchemasStayCompatible is the registry's check, run before merge:
// a reader on the new schema has to decode what the recorded one wrote.
func TestPublishedSchemasStayCompatible(t *testing.T) {
	for topic, schema := range Published() {
		t.Run(topic, func(t *testing.T) {
			path := snapshotPath(topic)
			current := document(t, schema)
			recorded, err := os.ReadFile(path)
			if err == nil {
				old, perr := avro.Parse(string(recorded))
				if perr != nil {
					t.Fatalf("%s does not parse: %v", path, perr)
				}
				if cerr := avro.NewSchemaCompatibility().Compatible(schema, old); cerr != nil {
					t.Fatalf("the %s schema is not BACKWARD compatible with %s, so the registry would refuse it and every publish on the topic would stop: %v", topic, path, cerr)
				}
			} else if !os.IsNotExist(err) || !*update {
				t.Fatalf("no recorded schema for %s; run make schemas: %v", topic, err)
			}
			if *update {
				if err := os.WriteFile(path, current, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			if !bytes.Equal(current, recorded) {
				t.Fatalf("the %s schema changed compatibly; run make schemas to record it in %s", topic, path)
			}
		})
	}
}

type registry struct {
	codec.Codec
	err        error
	registered map[string]avro.Schema
}

func (r *registry) RegisterSchemas(_ context.Context, s map[string]avro.Schema) error {
	r.registered = s
	return r.err
}

func TestGate(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, running, tag string
		err                error
		held               bool
	}{
		{"the control plane runs the release", "v1.2.3", "v1.2.3", nil, false},
		{"the kafka image is the same release", "v1.2.3-kafka", "v1.2.3", nil, false},
		{"the control plane is behind", "v1.2.3", "v1.2.4", nil, true},
		{"a dev build still registers", "dev-abc", "v1.2.4", nil, false},
		{"the registry refuses", "v1.2.3", "v1.2.3", errors.New("409"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &registry{err: tc.err}
			err := Gate(r, tc.running)(ctx, tc.tag)
			if (err != nil) != tc.held {
				t.Fatalf("held = %v, want %v", err, tc.held)
			}
			if !tc.held && len(r.registered) != len(Published()) {
				t.Fatalf("registered %d schemas, want %d", len(r.registered), len(Published()))
			}
		})
	}

	if err := Gate(codec.NewJSON(), "v1.2.3")(ctx, "v9.9.9"); err != nil {
		t.Fatalf("a codec with no registry held the fleet: %v", err)
	}
}

func TestWorkerCommandsNamesThePerNodeTopics(t *testing.T) {
	if !strings.HasPrefix(kafka.GetWorkerTopic("node"), strings.TrimSuffix(WorkerCommands, "*")) {
		t.Fatalf("WorkerCommands %q does not match w.<node-id>", WorkerCommands)
	}
}

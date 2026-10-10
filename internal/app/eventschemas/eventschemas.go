// Package eventschemas registers a release's bus schemas from the control
// plane, which rolls out first, and holds the fleet off a release the schema
// registry refused, so a refusal never reaches a worker mid-send.
package eventschemas

import (
	"context"
	"fmt"
	"strings"

	"github.com/iskorotkov/avro/v2"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/models"
)

// WorkerCommands names every per-node command topic (w.<node-id>).
const WorkerCommands = "w.*"

// Published is every schema this build publishes, by topic.
func Published() map[string]avro.Schema {
	return map[string]avro.Schema{
		kafka.TopicWorkerEvents:  models.JobEvent{}.Schema(),
		WorkerCommands:           models.WorkerEvent{}.Schema(),
		events.TopicEmailEvents:  events.EmailSentEvent{}.Schema(),
		events.TopicWarmupEvents: events.WarmupEmailSentEvent{}.Schema(),
	}
}

// Register registers every published schema when c resolves against a
// registry. A codec without one has nothing to refuse.
func Register(ctx context.Context, c codec.Codec) error {
	r, ok := c.(codec.SchemaRegistrar)
	if !ok {
		return nil
	}
	return r.RegisterSchemas(ctx, Published())
}

// Gate answers whether the fleet may move to tag. On a registry, the control
// plane has to be running that release and its schemas have to register;
// running is this binary's stamped version, and a dev build is not held.
func Gate(c codec.Codec, running string) func(ctx context.Context, tag string) error {
	return func(ctx context.Context, tag string) error {
		if _, ok := c.(codec.SchemaRegistrar); !ok {
			return nil
		}
		if release(running) && release(tag) && base(running) != base(tag) {
			return fmt.Errorf("the control plane runs %s, so the fleet waits for it to run %s and register its schemas", running, tag)
		}
		if err := Register(ctx, c); err != nil {
			return fmt.Errorf("schema registry refused this release's event schemas: %w", err)
		}
		return nil
	}
}

// release is a tagged version rather than a dev or unstamped build.
func release(v string) bool {
	return len(v) > 1 && v[0] == 'v' && v[1] >= '0' && v[1] <= '9'
}

// base drops the image variant, so v1.2.3-kafka and v1.2.3 are one release.
func base(v string) string {
	return strings.TrimSuffix(v, "-kafka")
}

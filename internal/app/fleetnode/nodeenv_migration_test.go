package fleetnode

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestMigrationSettingsReachBothFleetRoles(t *testing.T) {
	settings := map[string]string{
		"EVENTBUS_READ_BOTH": "true", "EVENTBUS_HOLD_READS": "nats", "CODEC_READ_BOTH": "true",
		"NATS_STREAM_NAME": "warmbly", "NATS_SUBJECT_PREFIX": "warmbly", "NATS_REPLICAS": "3",
		"NATS_DISCARD_NEW": "true", "NATS_TLS_CA_B64": "fixture-ca", "NATS_CREDS_B64": "fixture-creds",
	}
	for key, value := range settings {
		t.Setenv(key, value)
	}
	for _, role := range []models.NodeRole{models.NodeRoleWorker, models.NodeRoleConsumer} {
		env := RenderEnv(uuid.New(), role, "us-east-1")
		for key, value := range settings {
			if !strings.Contains("\n"+env, "\n"+key+"="+value+"\n") {
				t.Fatalf("%s missing %s", role, key)
			}
		}
	}
}

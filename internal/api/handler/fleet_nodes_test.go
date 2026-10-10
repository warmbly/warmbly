package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/fleetnode"
	"github.com/warmbly/warmbly/internal/app/nodelogs"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
	"github.com/warmbly/warmbly/internal/repository"
)

type joinNodes struct {
	repository.FleetNodeRepository
	upserts int
	nodeID  uuid.UUID
	role    models.NodeRole
}

func (n *joinNodes) Get(_ context.Context, id uuid.UUID) (*models.FleetNode, error) {
	if n.nodeID == id && id != uuid.Nil {
		return &models.FleetNode{ID: id, Role: n.role}, nil
	}
	return nil, nil
}

func (n *joinNodes) UpsertOnHeartbeat(_ context.Context, beat models.NodeHeartbeat) error {
	n.upserts++
	n.nodeID = beat.NodeID
	n.role = beat.Role
	return nil
}

type joinSettings struct {
	repository.FleetSettingsRepository
}

type joinEvidence struct {
	*nodelogs.Service
	node uuid.UUID
}

func (e *joinEvidence) Enroll(_ context.Context, id uuid.UUID) (string, error) {
	e.node = id
	return strings.Repeat("l", 43), nil
}

func (joinSettings) GetJoinToken(context.Context) (string, *time.Time, error) {
	expiresAt := time.Now().Add(time.Hour)
	return crypt.SHA256("join-test-token"), &expiresAt, nil
}

func (joinSettings) GetRelease(context.Context) (*models.FleetReleaseState, error) {
	return &models.FleetReleaseState{}, nil
}

func TestFleetJoinResolvesImageBeforeRegisteringNode(t *testing.T) {
	for _, version := range []string{"dev", "v0.6.33"} {
		t.Run(version, func(t *testing.T) {
			t.Setenv("WARMBLY_VERSION", version)
			t.Setenv("FLEET_IMAGE_VARIANT", "")
			nodes := &joinNodes{}
			evidence := &joinEvidence{}
			h := &Handler{FleetNodes: fleetnode.New(nodes, nil, joinSettings{}), NodeLogs: evidence}
			for i := range 2 {
				response := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(response)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/fleet/join", strings.NewReader(`{"token":"join-test-token","role":"consumer"}`))
				c.Request.Header.Set("Content-Type", "application/json")
				h.FleetJoin(c)
				if version == "dev" {
					if response.Code != http.StatusInternalServerError || nodes.upserts != 0 {
						t.Fatalf("failed join registered a node: status=%d, upserts=%d", response.Code, nodes.upserts)
					}
				} else if response.Code != http.StatusOK || nodes.upserts != i+1 {
					t.Fatalf("successful join was not registered: status=%d, upserts=%d", response.Code, nodes.upserts)
				} else if evidence.node == uuid.Nil {
					t.Fatal("successful join did not enroll node evidence")
				}
			}
		})
	}
}

func TestFleetJoinFailsClosedWithoutEvidenceStore(t *testing.T) {
	t.Setenv("WARMBLY_VERSION", "v0.6.33")
	t.Setenv("FLEET_IMAGE_VARIANT", "")
	nodes := &joinNodes{}
	h := &Handler{FleetNodes: fleetnode.New(nodes, nil, joinSettings{})}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/fleet/join", strings.NewReader(`{"token":"join-test-token","role":"consumer"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.FleetJoin(c)
	if w.Code != http.StatusServiceUnavailable || nodes.upserts != 0 {
		t.Fatalf("missing evidence dependency registered a node: status=%d, upserts=%d", w.Code, nodes.upserts)
	}
}

func TestFleetJoinReenrollsOriginalNodeIdentityWithEvidenceCredential(t *testing.T) {
	t.Setenv("WARMBLY_VERSION", "v0.6.44")
	t.Setenv("FLEET_IMAGE_VARIANT", "")
	id := uuid.New()
	nodes, evidence := &joinNodes{}, &joinEvidence{}
	h := &Handler{FleetNodes: fleetnode.New(nodes, nil, joinSettings{}), NodeLogs: evidence}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/fleet/join", strings.NewReader(`{"token":"join-test-token","role":"worker","node_id":"`+id.String()+`"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.FleetJoin(c)
	if w.Code != http.StatusOK || nodes.nodeID != id || evidence.node != id || nodes.upserts != 1 {
		t.Fatalf("re-enrollment changed identity or omitted evidence enrollment: status=%d", w.Code)
	}
	var reply fleetJoinResponse
	if json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.NodeID != id {
		t.Fatal("re-enrollment response replaced the original identity")
	}
	raw, err := base64.StdEncoding.DecodeString(reply.EnvB64)
	if err != nil {
		t.Fatal(err)
	}
	env := envLines(t, string(raw))
	if env["WARMBLY_NODE_ID"] != id.String() || env["NODE_LOG_TOKEN"] != strings.Repeat("l", 43) {
		t.Fatal("protected environment did not bind the evidence credential to the existing node")
	}
}

type retryJoinEvidence struct {
	joinEvidence
	calls int
}

func (e *retryJoinEvidence) Enroll(ctx context.Context, id uuid.UUID) (string, error) {
	e.calls++
	if e.calls == 1 {
		return "", errors.New("fixture enrollment unavailable")
	}
	return e.joinEvidence.Enroll(ctx, id)
}

func TestFleetJoinRetryKeepsIdentityAfterEnrollmentFailureAndLostReply(t *testing.T) {
	t.Setenv("WARMBLY_VERSION", "v0.6.44")
	t.Setenv("FLEET_IMAGE_VARIANT", "")
	id := uuid.New()
	nodes, evidence := &joinNodes{}, &retryJoinEvidence{}
	h := &Handler{FleetNodes: fleetnode.New(nodes, nil, joinSettings{}), NodeLogs: evidence}
	for i, role := range []string{"worker", "worker", "worker", "consumer"} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/fleet/join", strings.NewReader(`{"token":"join-test-token","role":"`+role+`","node_id":"`+id.String()+`"}`))
		c.Request.Header.Set("Content-Type", "application/json")
		h.FleetJoin(c)
		if i == 0 {
			if w.Code != http.StatusServiceUnavailable || nodes.nodeID != id || nodes.upserts != 1 {
				t.Fatal("failed enrollment did not retain the supplied identity")
			}
			continue
		}
		if i == 3 {
			if w.Code == http.StatusOK || evidence.calls != 3 || nodes.upserts != 3 {
				t.Fatal("role change mutated identity or rotated evidence authority before validation")
			}
			continue
		}
		var reply fleetJoinResponse
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.NodeID != id || evidence.node != id || nodes.nodeID != id {
			t.Fatal("retry after failed/lost reply changed node identity")
		}
		// The first successful response is deliberately unused, as if its acknowledgement was lost.
	}
}

func TestHeartbeatAddressRequiresNodePublicIPv4(t *testing.T) {
	tests := []struct {
		reported string
		want     string
	}{
		{"1.1.1.1", "1.1.1.1"},
		{"::ffff:1.1.1.1", "1.1.1.1"},
		{"198.51.100.8", ""},
		{"100.64.2.3", ""},
		{"172.17.0.2", ""},
		{"2001:4860:4860::8888", ""},
		{"", ""},
	}
	for _, tc := range tests {
		if got := heartbeatAddress(tc.reported); got != tc.want {
			t.Errorf("heartbeatAddress(%q) = %q, want %q", tc.reported, got, tc.want)
		}
	}
}

// envLines turns a rendered node env into a lookup, so assertions name a
// setting rather than a line number.
func envLines(t *testing.T, rendered string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, line := range strings.Split(rendered, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("line is not KEY=value: %q", line)
		}
		out[k] = v
	}
	return out
}

// setInstanceEnv puts the process in the shape of an AWS-backed control plane.
func setInstanceEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ENCRYPTED_KEYS_BACKEND_URL", "https://api.example.com/")
	t.Setenv("INTERNAL_API_TOKEN", "tok")
	t.Setenv("KMS_PROVIDER", "aws")
	t.Setenv("KMS_AWS_KEY_ID", "alias/warmbly")
	t.Setenv("BLOB_PROVIDER", "s3")
	t.Setenv("BLOB_BUCKET", "warmbly-blobs")
	t.Setenv("AWS_REGION", "eu-central-1")
	t.Setenv("CREDENTIALS_ENCRYPTION_KEY", "deadbeef")
	t.Setenv("NATS_URL", "tls://bus.example.com:4222")
	t.Setenv("REDIS", "rediss://bus.example.com:6380")
}

// A node reports its own crashes, and the only place it can learn where to send
// them is the env the join endpoint renders. Both backends travel: an instance
// on PostHog and an instance on Sentry each get a fleet that reports.
func TestRenderNodeEnvCarriesErrorTracking(t *testing.T) {
	t.Setenv("POSTHOG_KEY", "phc_example")
	t.Setenv("POSTHOG_HOST", "https://eu.i.posthog.com")
	t.Setenv("POSTHOG_ERROR_TRACKING", "false")
	t.Setenv("SENTRY_DSN", "https://k@example.invalid/1")

	env := envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleWorker, ""))

	for key, want := range map[string]string{
		"POSTHOG_KEY":            "phc_example",
		"POSTHOG_HOST":           "https://eu.i.posthog.com",
		"POSTHOG_ERROR_TRACKING": "false",
		"SENTRY_DSN":             "https://k@example.invalid/1",
	} {
		if env[key] != want {
			t.Errorf("%s = %q, want %q", key, env[key], want)
		}
	}
}

// A node has no cloud credential and no way to be given one, so a provider
// that needs a credential has to arrive translated. Shipping "aws" here is how
// a joined node ends up unable to open a single mailbox.
func TestRenderNodeEnvBrokersCredentialProviders(t *testing.T) {
	setInstanceEnv(t)
	env := envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleWorker, "eu-central"))

	if env["KMS_PROVIDER"] != "brokered" {
		t.Errorf("KMS_PROVIDER = %q, want brokered", env["KMS_PROVIDER"])
	}
	if env["BLOB_PROVIDER"] != "brokered" {
		t.Errorf("BLOB_PROVIDER = %q, want brokered", env["BLOB_PROVIDER"])
	}
	if _, ok := env["AWS_ACCESS_KEY_ID"]; ok {
		t.Error("a node was handed an AWS credential")
	}
}

// A provider that needs no credential works on a node as it stands, and
// translating it would break a local install for nothing.
func TestRenderNodeEnvPassesThroughLocalProviders(t *testing.T) {
	setInstanceEnv(t)
	t.Setenv("KMS_PROVIDER", "local")
	t.Setenv("BLOB_PROVIDER", "filesystem")
	t.Setenv("BLOB_FS_ROOT", "/data/blobs")

	env := envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleWorker, ""))
	if env["KMS_PROVIDER"] != "local" {
		t.Errorf("KMS_PROVIDER = %q, want local", env["KMS_PROVIDER"])
	}
	if env["BLOB_PROVIDER"] != "filesystem" {
		t.Errorf("BLOB_PROVIDER = %q, want filesystem", env["BLOB_PROVIDER"])
	}
	if env["BLOB_FS_ROOT"] != "/data/blobs" {
		t.Errorf("BLOB_FS_ROOT = %q", env["BLOB_FS_ROOT"])
	}
}

// The regression this file exists for: every name sent has to be one the
// node's own code reads. S3_BUCKET and KMS_KEY_ID were read by nothing, so a
// node fell back to the default bucket and the default key alias in silence.
func TestRenderNodeEnvSendsNamesTheNodeReads(t *testing.T) {
	setInstanceEnv(t)
	t.Setenv("S3_BUCKET", "should-not-travel")
	t.Setenv("KMS_KEY_ID", "should-not-travel")

	env := envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleWorker, ""))
	for _, dead := range []string{"S3_BUCKET", "KMS_KEY_ID"} {
		if _, ok := env[dead]; ok {
			t.Errorf("%s is still sent; nothing reads it", dead)
		}
	}
	if env["BLOB_BUCKET"] != "warmbly-blobs" {
		t.Errorf("BLOB_BUCKET = %q, want warmbly-blobs", env["BLOB_BUCKET"])
	}
	if env["KMS_AWS_KEY_ID"] != "alias/warmbly" {
		t.Errorf("KMS_AWS_KEY_ID = %q, want alias/warmbly", env["KMS_AWS_KEY_ID"])
	}
}

// A worker reaches relational data through the internal API and nothing else.
// The DSN must not travel even when the control plane has one to send.
func TestRenderNodeEnvWithholdsDSNFromWorker(t *testing.T) {
	setInstanceEnv(t)
	t.Setenv("PRIMARY_DB", "postgres://u:p@db/warmbly")

	env := envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleWorker, ""))
	if _, ok := env["PRIMARY_DB"]; ok {
		t.Fatal("a worker was handed a database DSN")
	}
	if env["ENCRYPTED_KEYS_PROVIDER"] != "http" {
		t.Errorf("ENCRYPTED_KEYS_PROVIDER = %q, want http", env["ENCRYPTED_KEYS_PROVIDER"])
	}
}

// A consumer is control plane: it opens Postgres itself and cannot boot
// without the DSN, which is why joining one used to produce a node that died
// on its first start.
func TestRenderNodeEnvGivesConsumerTheDSN(t *testing.T) {
	setInstanceEnv(t)
	t.Setenv("PRIMARY_DB", "postgres://u:p@db/warmbly")

	id := uuid.New()
	env := envLines(t, renderNodeEnv(id, models.NodeRoleConsumer, ""))
	if env["PRIMARY_DB"] != "postgres://u:p@db/warmbly" {
		t.Errorf("PRIMARY_DB = %q", env["PRIMARY_DB"])
	}
	if env["ENCRYPTED_KEYS_PROVIDER"] != "postgres" {
		t.Errorf("ENCRYPTED_KEYS_PROVIDER = %q, want postgres", env["ENCRYPTED_KEYS_PROVIDER"])
	}
	// Only a worker claims a placement identity.
	if _, ok := env["WORKER_ID"]; ok {
		t.Error("a consumer was given a WORKER_ID")
	}
	if env["WARMBLY_NODE_ID"] != id.String() {
		t.Errorf("WARMBLY_NODE_ID = %q, want %s", env["WARMBLY_NODE_ID"], id)
	}
}

// An instance holding its DSN in SSM has none to send. The node still has to
// be able to fetch keys, so it falls back to the HTTP path rather than being
// left with a provider it cannot satisfy.
func TestRenderNodeEnvConsumerWithoutDSNFallsBackToHTTP(t *testing.T) {
	setInstanceEnv(t)
	t.Setenv("PRIMARY_DB", "")

	env := envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleConsumer, ""))
	if _, ok := env["PRIMARY_DB"]; ok {
		t.Error("PRIMARY_DB was sent as an empty value")
	}
	if env["ENCRYPTED_KEYS_PROVIDER"] != "http" {
		t.Errorf("ENCRYPTED_KEYS_PROVIDER = %q, want http", env["ENCRYPTED_KEYS_PROVIDER"])
	}
}

// The worker's identity and the node row have to be the same machine.
func TestRenderNodeEnvWorkerIdentity(t *testing.T) {
	setInstanceEnv(t)
	id := uuid.New()
	env := envLines(t, renderNodeEnv(id, models.NodeRoleWorker, "eu-central"))

	if env["WORKER_ID"] != id.String() {
		t.Errorf("WORKER_ID = %q, want %s", env["WORKER_ID"], id)
	}
	if env["WARMBLY_NODE_REGION"] != "eu-central" {
		t.Errorf("WARMBLY_NODE_REGION = %q", env["WARMBLY_NODE_REGION"])
	}
	// The trailing slash on the instance URL must not survive into a base URL
	// the node concatenates paths onto.
	if env["ENCRYPTED_KEYS_BACKEND_URL"] != "https://api.example.com" {
		t.Errorf("ENCRYPTED_KEYS_BACKEND_URL = %q", env["ENCRYPTED_KEYS_BACKEND_URL"])
	}
}

// A consumer refreshes integration tokens and drains the CRM outbox, so it
// needs the integration client credentials; a worker never sees them.
func TestRenderNodeEnvIntegrationCredentialsReachConsumerOnly(t *testing.T) {
	setInstanceEnv(t)
	t.Setenv("HUBSPOT_OAUTH_CLIENT_ID", "hs-id")
	t.Setenv("HUBSPOT_OAUTH_CLIENT_SECRET", "hs-secret")

	consumer := envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleConsumer, ""))
	if consumer["HUBSPOT_OAUTH_CLIENT_SECRET"] != "hs-secret" || consumer["HUBSPOT_OAUTH_CLIENT_ID"] != "hs-id" {
		t.Errorf("consumer missing HubSpot credentials: %v", consumer)
	}
	worker := envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleWorker, ""))
	if _, ok := worker["HUBSPOT_OAUTH_CLIENT_SECRET"]; ok {
		t.Error("a worker was handed an integration client secret")
	}
}

// A node calls only node routes, so with a separate node token it carries that
// alone and never the token the tracking and forms services hold.
func TestRenderNodeEnvSendsOnlyTheNodeToken(t *testing.T) {
	setInstanceEnv(t)
	t.Setenv("NODE_BROKER_TOKEN", "nodetok")

	env := envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleWorker, ""))
	if env["NODE_BROKER_TOKEN"] != "nodetok" || env["ENCRYPTED_KEYS_WORKER_TOKEN"] != "nodetok" {
		t.Errorf("node token not sent: %q / %q", env["NODE_BROKER_TOKEN"], env["ENCRYPTED_KEYS_WORKER_TOKEN"])
	}
	if _, ok := env["INTERNAL_API_TOKEN"]; ok {
		t.Error("a node was handed the edge services' internal token")
	}

	t.Setenv("NODE_BROKER_TOKEN", "")
	env = envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleWorker, ""))
	if env["ENCRYPTED_KEYS_WORKER_TOKEN"] != "tok" || env["INTERNAL_API_TOKEN"] != "tok" {
		t.Error("a single-token instance must keep sending the shared token")
	}
}

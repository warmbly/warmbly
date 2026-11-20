package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
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
	hash    string
}

func (n *joinNodes) SetCredentialHash(_ context.Context, id uuid.UUID, hash string) error {
	if id == n.nodeID {
		n.hash = hash
	}
	return nil
}

func (n *joinNodes) CredentialHash(_ context.Context, id uuid.UUID) (string, error) {
	if id == n.nodeID {
		return n.hash, nil
	}
	return "", nil
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
	if env["NODE_CREDENTIAL"] == "" || nodes.hash != crypt.SHA256(env["NODE_CREDENTIAL"]) || env["NODE_CREDENTIAL"] == env["NODE_LOG_TOKEN"] {
		t.Fatal("join must issue the node its own credential and store only its hash")
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

// A node cannot tell a hosted instance from a self-host, so it is handed the
// mail-host policy the control plane resolved for itself.
func TestRenderNodeEnvResolvesPrivateMailHostPolicy(t *testing.T) {
	setInstanceEnv(t)
	for _, tc := range []struct {
		name, mode, appEnv, flag, want string
	}{
		{"hosted production refuses", "cloud", "prod", "true", "false"},
		{"self-host allows", "self_hosted", "prod", "", "true"},
		{"self-host opted out", "self_hosted", "prod", "false", "false"},
		{"development allows", "cloud", "dev", "", "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DEPLOYMENT_MODE", tc.mode)
			t.Setenv("APP_ENV", tc.appEnv)
			t.Setenv("MAIL_ALLOW_PRIVATE_HOSTS", tc.flag)
			env := envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleWorker, ""))
			if env["MAIL_ALLOW_PRIVATE_HOSTS"] != tc.want {
				t.Errorf("MAIL_ALLOW_PRIVATE_HOSTS = %q, want %q", env["MAIL_ALLOW_PRIVATE_HOSTS"], tc.want)
			}
		})
	}
}

// A provider that needs no credential works on a node as it stands. Local KMS
// is the exception for a worker: the master key stays on the control plane and
// the worker opens keys through the broker, while a consumer keeps it.
func TestRenderNodeEnvPassesThroughLocalProviders(t *testing.T) {
	setInstanceEnv(t)
	t.Setenv("KMS_PROVIDER", "local")
	t.Setenv("KMS_LOCAL_MASTER_KEY", "bWFzdGVyLWtleQ==")
	t.Setenv("BLOB_PROVIDER", "filesystem")
	t.Setenv("BLOB_FS_ROOT", "/data/blobs")

	env := envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleWorker, ""))
	if env["KMS_PROVIDER"] != "brokered" {
		t.Errorf("worker KMS_PROVIDER = %q, want brokered", env["KMS_PROVIDER"])
	}
	if _, ok := env["KMS_LOCAL_MASTER_KEY"]; ok {
		t.Error("a worker was handed the KMS master key")
	}
	if env["BLOB_PROVIDER"] != "filesystem" {
		t.Errorf("BLOB_PROVIDER = %q, want filesystem", env["BLOB_PROVIDER"])
	}
	if env["BLOB_FS_ROOT"] != "/data/blobs" {
		t.Errorf("BLOB_FS_ROOT = %q", env["BLOB_FS_ROOT"])
	}

	consumer := envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleConsumer, ""))
	if consumer["KMS_PROVIDER"] != "local" || consumer["KMS_LOCAL_MASTER_KEY"] != "bWFzdGVyLWtleQ==" {
		t.Errorf("consumer KMS = %q with key %v, want local with its key", consumer["KMS_PROVIDER"], consumer["KMS_LOCAL_MASTER_KEY"] != "")
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

// A worker reaches relational data through the internal API and nothing else,
// and opens only workspace-sealed material. Neither the DSN nor the instance
// credential keys travel even when the control plane has them to send.
func TestRenderNodeEnvWithholdsDSNFromWorker(t *testing.T) {
	setInstanceEnv(t)
	t.Setenv("PRIMARY_DB", "postgres://u:p@db/warmbly")
	t.Setenv("CREDENTIALS_ENCRYPTION_READ_KEYS", `["`+strings.Repeat("b", 64)+`"]`)
	t.Setenv("KMS_LOCAL_MASTER_KEY", "bWFzdGVyLWtleQ==")

	env := envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleWorker, ""))
	for _, key := range []string{"PRIMARY_DB", "CREDENTIALS_ENCRYPTION_KEY", "CREDENTIALS_ENCRYPTION_READ_KEYS", "KMS_LOCAL_MASTER_KEY"} {
		if _, ok := env[key]; ok {
			t.Errorf("a worker was handed %s", key)
		}
	}
	if env["ENCRYPTED_KEYS_PROVIDER"] != "http" {
		t.Errorf("ENCRYPTED_KEYS_PROVIDER = %q, want http", env["ENCRYPTED_KEYS_PROVIDER"])
	}

	consumer := envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleConsumer, ""))
	if consumer["CREDENTIALS_ENCRYPTION_KEY"] != "deadbeef" || consumer["CREDENTIALS_ENCRYPTION_READ_KEYS"] == "" {
		t.Error("a consumer seals and opens stored credentials, so it keeps the instance credential keys")
	}
}

// A node resolves hosted or self-hosted from what the control plane resolved, so
// a hosted node ignores the self-host-only opt-outs whatever node.local.env says.
func TestRenderNodeEnvResolvesDeploymentMode(t *testing.T) {
	setInstanceEnv(t)
	for _, tc := range []struct{ mode, billing, want string }{
		{"cloud", "", "cloud"},
		{"self_hosted", "", "self_hosted"},
		{"", "none", "self_hosted"},
		{"", "stripe", "cloud"},
	} {
		t.Setenv("DEPLOYMENT_MODE", tc.mode)
		t.Setenv("BILLING_PROVIDER", tc.billing)
		env := envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleWorker, ""))
		if env["DEPLOYMENT_MODE"] != tc.want {
			t.Errorf("mode=%q billing=%q: DEPLOYMENT_MODE = %q, want %q", tc.mode, tc.billing, env["DEPLOYMENT_MODE"], tc.want)
		}
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

// A rotation reaches nodes by pulling: the env endpoint and the revision in
// the heartbeat reply must describe the same file, or the updater loops.
func TestFleetNodeEnvMatchesHeartbeatRevision(t *testing.T) {
	t.Setenv("WARMBLY_VERSION", "v0.6.44")
	t.Setenv("FLEET_IMAGE_VARIANT", "")
	t.Setenv("CREDENTIALS_ENCRYPTION_READ_KEYS", `["`+strings.Repeat("a", 64)+`"]`)
	id := uuid.New()
	nodes := &joinNodes{nodeID: id, role: models.NodeRoleWorker}
	h := &Handler{FleetNodes: fleetnode.New(nodes, nil, joinSettings{})}

	get := func(query string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/internal/fleet/node-env?"+query, nil)
		h.InternalFleetNodeEnv(c)
		return w
	}
	w := get("node_id=" + id.String() + "&role=worker")
	if w.Code != http.StatusOK {
		t.Fatalf("enrolled node was refused its env: %d", w.Code)
	}
	env := envLines(t, w.Body.String())
	if env["WARMBLY_NODE_ID"] != id.String() {
		t.Fatal("node env must carry the node identity")
	}
	if _, ok := env["CREDENTIALS_ENCRYPTION_READ_KEYS"]; ok {
		t.Fatal("a worker env must never carry the instance credential read keys")
	}
	if _, ok := env["PRIMARY_DB"]; ok {
		t.Fatal("a worker env must never carry a database DSN")
	}
	rev := w.Header().Get("X-Warmbly-Env-Revision")
	if rev == "" || rev != fleetnode.EnvRevision(w.Body.String()) {
		t.Fatal("the revision header must name the body it came with")
	}

	for _, q := range []string{"node_id=" + id.String() + "&role=consumer", "node_id=" + uuid.NewString() + "&role=worker", "node_id=x&role=worker"} {
		if code := get(q).Code; code == http.StatusOK {
			t.Fatalf("%s must be refused", q)
		}
	}

	hw := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(hw)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/internal/fleet/heartbeat",
		strings.NewReader(`{"node_id":"`+id.String()+`","role":"worker","env_refresh":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.FleetHeartbeat(c)
	var reply models.NodeHeartbeatReply
	if err := json.Unmarshal(hw.Body.Bytes(), &reply); err != nil || reply.EnvRevision != rev {
		t.Fatalf("heartbeat revision %q must match the env endpoint's %q", reply.EnvRevision, rev)
	}
}

// fleetOf is a registry of several enrolled nodes, each with its own credential hash.
type fleetOf struct {
	repository.FleetNodeRepository
	roles  map[uuid.UUID]models.NodeRole
	hashes map[uuid.UUID]string
}

func (f *fleetOf) Get(_ context.Context, id uuid.UUID) (*models.FleetNode, error) {
	if role, ok := f.roles[id]; ok {
		return &models.FleetNode{ID: id, Role: role}, nil
	}
	return nil, nil
}

func (f *fleetOf) SetCredentialHash(_ context.Context, id uuid.UUID, hash string) error {
	f.hashes[id] = hash
	return nil
}

func (f *fleetOf) CredentialHash(_ context.Context, id uuid.UUID) (string, error) {
	return f.hashes[id], nil
}

// A node's env is handed only to that node: a worker's credential never opens a
// consumer's env, and a worker never receives a database DSN.
func TestFleetNodeEnvIsHandedOnlyToItsOwnNode(t *testing.T) {
	setInstanceEnv(t)
	t.Setenv("PRIMARY_DB", "postgres://u:p@db/warmbly")
	t.Setenv("DEPLOYMENT_MODE", "self_hosted")
	var logs bytes.Buffer
	prev := zlog.Logger
	zlog.Logger = zerolog.New(&logs)
	defer func() { zlog.Logger = prev }()

	worker, otherWorker, consumer := uuid.New(), uuid.New(), uuid.New()
	fleet := &fleetOf{
		roles:  map[uuid.UUID]models.NodeRole{worker: models.NodeRoleWorker, otherWorker: models.NodeRoleWorker, consumer: models.NodeRoleConsumer},
		hashes: map[uuid.UUID]string{},
	}
	svc := fleetnode.New(fleet, nil, joinSettings{})
	h := &Handler{FleetNodes: svc}
	creds := map[uuid.UUID]string{}
	for id := range fleet.roles {
		secret, err := svc.IssueNodeCredential(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		creds[id] = secret
	}

	get := func(id uuid.UUID, role models.NodeRole, credential string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/internal/fleet/node-env?node_id="+id.String()+"&role="+string(role), nil)
		if credential != "" {
			c.Request.Header.Set(nodeCredentialHeader, credential)
		}
		h.InternalFleetNodeEnv(c)
		return w
	}

	for _, tc := range []struct {
		name       string
		id         uuid.UUID
		role       models.NodeRole
		credential string
		code       int
	}{
		{"a worker presenting its credential for a consumer", consumer, models.NodeRoleConsumer, creds[worker], http.StatusUnauthorized},
		{"a worker presenting its credential for another worker", otherWorker, models.NodeRoleWorker, creds[worker], http.StatusUnauthorized},
		{"a consumer env with no credential", consumer, models.NodeRoleConsumer, "", http.StatusForbidden},
		{"a forged credential", consumer, models.NodeRoleConsumer, strings.Repeat("x", 43), http.StatusUnauthorized},
		{"a node asking under the other role", consumer, models.NodeRoleWorker, creds[consumer], http.StatusNotFound},
	} {
		w := get(tc.id, tc.role, tc.credential)
		if w.Code != tc.code || strings.Contains(w.Body.String(), "PRIMARY_DB") {
			t.Fatalf("%s: status %d, want %d (body carried a DSN: %v)", tc.name, w.Code, tc.code, strings.Contains(w.Body.String(), "PRIMARY_DB"))
		}
	}

	w := get(consumer, models.NodeRoleConsumer, creds[consumer])
	if w.Code != http.StatusOK || envLines(t, w.Body.String())["PRIMARY_DB"] != "postgres://u:p@db/warmbly" {
		t.Fatalf("a consumer presenting its own credential: status %d", w.Code)
	}
	for _, credential := range []string{creds[worker], ""} {
		w = get(worker, models.NodeRoleWorker, credential)
		env := envLines(t, w.Body.String())
		if w.Code != http.StatusOK || env["WARMBLY_NODE_ID"] != worker.String() {
			t.Fatalf("a worker fetching its own env (credential=%v): status %d", credential != "", w.Code)
		}
		for _, key := range []string{"PRIMARY_DB", "CREDENTIALS_ENCRYPTION_KEY"} {
			if _, ok := env[key]; ok {
				t.Fatalf("a worker env carried %s", key)
			}
		}
	}

	out := logs.String()
	if strings.Count(out, `"operation":"fleet.node_env"`) != 8 || strings.Count(out, `"allowed":false`) != 5 {
		t.Fatalf("every hand-out and refusal must be logged once: %s", out)
	}
	for _, leaked := range []string{"postgres://", creds[worker], creds[consumer], "deadbeef"} {
		if strings.Contains(out, leaked) {
			t.Fatal("the access log carried a secret")
		}
	}
}

// A cleartext control-plane address is reported on hosted production; a self-host on a LAN is not.
func TestNodeBackendURLIsReportedWhenCleartextOnHostedProduction(t *testing.T) {
	for _, tc := range []struct {
		mode, appEnv, url string
		ok                bool
	}{
		{"cloud", "prod", "https://api.example.com", true},
		{"cloud", "prod", "http://api.example.com", false},
		{"cloud", "prod", "http://localhost:8080", true},
		{"cloud", "prod", "http://[::1]:8080", true},
		{"cloud", "dev", "http://api.example.com", true},
		{"self_hosted", "prod", "http://10.0.0.5:8080", true},
	} {
		t.Setenv("DEPLOYMENT_MODE", tc.mode)
		t.Setenv("APP_ENV", tc.appEnv)
		t.Setenv("ENCRYPTED_KEYS_BACKEND_URL", tc.url)
		if err := fleetnode.CheckBackendURL(); (err == nil) != tc.ok {
			t.Errorf("%s/%s %s: err = %v, want ok=%v", tc.mode, tc.appEnv, tc.url, err, tc.ok)
		}
	}

}

// testCAPEM is a PEM certificate spanning several lines, the shape an env file cannot hold.
func testCAPEM(t *testing.T) string {
	t.Helper()
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	var b strings.Builder
	if err := pem.Encode(&b, &pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// Without NODE_BACKEND_INTERNAL_URL a node's env is what it always was.
func TestRenderNodeEnvOmitsPrivateBackendWhenUnset(t *testing.T) {
	setInstanceEnv(t)
	env := envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleWorker, ""))
	for _, key := range []string{"WARMBLY_BACKEND_INTERNAL_URL", "WARMBLY_BACKEND_INTERNAL_CA_B64"} {
		if _, ok := env[key]; ok {
			t.Errorf("%s rendered with no private backend configured", key)
		}
	}
}

// The private address is offered beside the public one, never in its place, so a node that cannot read it keeps working.
func TestRenderNodeEnvOffersPrivateBackendBesideThePublicOne(t *testing.T) {
	setInstanceEnv(t)
	ca := testCAPEM(t)
	t.Setenv("NODE_BACKEND_INTERNAL_URL", "https://backend.warmbly.internal:8443/")
	t.Setenv("NODE_BACKEND_INTERNAL_CA", ca)

	for _, role := range []models.NodeRole{models.NodeRoleWorker, models.NodeRoleConsumer} {
		env := envLines(t, renderNodeEnv(uuid.New(), role, ""))
		if env["WARMBLY_BACKEND_URL"] != "https://api.example.com" || env["ENCRYPTED_KEYS_BACKEND_URL"] != "https://api.example.com" {
			t.Errorf("%s: the public address moved: %q / %q", role, env["WARMBLY_BACKEND_URL"], env["ENCRYPTED_KEYS_BACKEND_URL"])
		}
		if env["WARMBLY_BACKEND_INTERNAL_URL"] != "https://backend.warmbly.internal:8443" {
			t.Errorf("%s: WARMBLY_BACKEND_INTERNAL_URL = %q", role, env["WARMBLY_BACKEND_INTERNAL_URL"])
		}
		got, err := base64.StdEncoding.DecodeString(env["WARMBLY_BACKEND_INTERNAL_CA_B64"])
		if err != nil || strings.TrimSpace(string(got)) != strings.TrimSpace(ca) {
			t.Errorf("%s: the CA did not survive the env file (err %v)", role, err)
		}
	}
	if err := fleetnode.CheckBackendURL(); err != nil {
		t.Errorf("CheckBackendURL = %v", err)
	}
}

// A cleartext private address is not handed out on hosted production, and the join log says why.
func TestRenderNodeEnvRefusesCleartextPrivateBackendOnHostedProduction(t *testing.T) {
	setInstanceEnv(t)
	t.Setenv("DEPLOYMENT_MODE", "cloud")
	t.Setenv("APP_ENV", "prod")
	t.Setenv("NODE_BACKEND_INTERNAL_URL", "http://backend.warmbly.internal:8080")

	env := envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleWorker, ""))
	if _, ok := env["WARMBLY_BACKEND_INTERNAL_URL"]; ok {
		t.Error("a cleartext private address reached a node on hosted production")
	}
	if err := fleetnode.CheckBackendURL(); !errors.Is(err, fleetnode.ErrInternalBackendUnusable) {
		t.Errorf("CheckBackendURL = %v, want ErrInternalBackendUnusable", err)
	}

	t.Setenv("DEPLOYMENT_MODE", "self_hosted")
	env = envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleWorker, ""))
	if env["WARMBLY_BACKEND_INTERNAL_URL"] != "http://backend.warmbly.internal:8080" {
		t.Error("a self-host's private-network address was withheld")
	}
}

// A CA that is not PEM withholds the private address rather than handing nodes one they cannot verify.
func TestRenderNodeEnvWithholdsPrivateBackendWithBadCA(t *testing.T) {
	setInstanceEnv(t)
	t.Setenv("NODE_BACKEND_INTERNAL_URL", "https://backend.warmbly.internal:8443")
	t.Setenv("NODE_BACKEND_INTERNAL_CA", "not a certificate")
	env := envLines(t, renderNodeEnv(uuid.New(), models.NodeRoleWorker, ""))
	if _, ok := env["WARMBLY_BACKEND_INTERNAL_URL"]; ok {
		t.Error("a private address went out with a CA nobody can parse")
	}
	if err := fleetnode.CheckBackendURL(); !errors.Is(err, fleetnode.ErrInternalBackendUnusable) {
		t.Errorf("CheckBackendURL = %v, want ErrInternalBackendUnusable", err)
	}
}

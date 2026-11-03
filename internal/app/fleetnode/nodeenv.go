package fleetnode

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

// NodeEnvKeys are the settings a node needs to do its job, in the order they
// are written. Every one is read from the backend's own environment, so a node
// is configured with exactly the infrastructure the control plane uses and
// there is nothing to keep in sync by hand.
//
// Every name here must be one the node's own code reads. Two of them were not
// (S3_BUCKET and KMS_KEY_ID, against a storage layer reading BLOB_BUCKET and a
// KMS factory reading KMS_AWS_KEY_ID), which sent an AWS-backed node to the
// default bucket and the default key alias with nothing logged.
//
// KMS_PROVIDER and BLOB_PROVIDER are absent because they are not copied but
// translated; see nodeProviders. The AWS-shaped settings above stay, so an
// operator who deliberately overrides a node back to a direct provider in
// node.local.env only has to add the credential.
//
// Deliberately excluded: PRIMARY_DB for a worker, and anything else that would
// give one direct database access. A worker reaches relational data through
// the internal API and nothing else. RenderEnv sends the DSN to a consumer,
// which is control plane and updates relational state itself.
var NodeEnvKeys = []string{
	"APP_ENV",
	"EVENTBUS_PROVIDER",
	"EVENTBUS_READ_BOTH",
	"EVENTBUS_HOLD_READS",
	"NATS_URL",
	// Base64, not a path: a node has no file to point at, and the env file
	// docker reads cannot hold a multi-line value.
	"NATS_CREDS_B64",
	"NATS_MAX_BYTES",
	"NATS_STREAM_NAME",
	"NATS_SUBJECT_PREFIX",
	"NATS_REPLICAS",
	"NATS_DISCARD_NEW",
	"NATS_TLS_CA_B64",
	"KAFKA_BOOTSTRAP_SERVERS",
	"KAFKA_SASL_USERNAME",
	"KAFKA_SASL_PASSWORD",
	"SCHEMA_REGISTRY_URL",
	"SCHEMA_REGISTRY_KEY",
	"SCHEMA_REGISTRY_SECRET",
	"CODEC_PROVIDER",
	"CODEC_READ_BOTH",
	"REDIS",
	"KMS_AWS_KEY_ID",
	"BLOB_FS_ROOT",
	"BLOB_BUCKET",
	"AWS_REGION",
	"AWS_ENDPOINT_URL_S3",
	"BOX_GOOGLE_CLIENT_ID",
	"BOX_GOOGLE_CLIENT_SECRET",
	"BOX_OUTLOOK_CLIENT_ID",
	"BOX_OUTLOOK_CLIENT_SECRET",
	"POSTHOG_KEY",
	"POSTHOG_HOST",
	"POSTHOG_ERROR_TRACKING",
	"SENTRY_DSN",
	"IMAP_IDLE",
	"SYNC_PUSH_FALLBACK_INTERVAL",
}

// consumerEnvKeys reach a consumer and never a worker. A consumer runs the
// integration actions, refreshes their OAuth tokens and drains the CRM outbox,
// so it reads the integration client credentials (integration.NewOAuthManager),
// APP_URL (the "Open in Warmbly" link written to the CRM) and the backend's
// public URL (where it registers Pipedrive's webhooks).
//
// The instance credential key and the local KMS master key are consumer-only
// too: a consumer seals and opens stored credentials and may mint a
// workspace's first data key, while a worker opens only what reaches it sealed
// under a workspace key, through the decrypt broker.
var consumerEnvKeys = []string{
	"KMS_LOCAL_MASTER_KEY",
	"CREDENTIALS_ENCRYPTION_KEY",
	// Earlier keys stay readable on consumers while a rotation reseals.
	"CREDENTIALS_ENCRYPTION_READ_KEYS",
	"APP_URL",
	"BACKEND_PUBLIC_URL",
	"API_PUBLIC_URL",
	"HUBSPOT_OAUTH_CLIENT_ID",
	"HUBSPOT_OAUTH_CLIENT_SECRET",
	"SLACK_OAUTH_CLIENT_ID",
	"SLACK_OAUTH_CLIENT_SECRET",
	"GOOGLE_SHEETS_OAUTH_CLIENT_ID",
	"GOOGLE_SHEETS_OAUTH_CLIENT_SECRET",
	"PIPEDRIVE_OAUTH_CLIENT_ID",
	"PIPEDRIVE_OAUTH_CLIENT_SECRET",
	"SALESFORCE_OAUTH_CLIENT_ID",
	"SALESFORCE_OAUTH_CLIENT_SECRET",
}

// nodeProviders translates the control plane's own crypto and blob providers
// into the ones a node should run.
//
// A node has no cloud credentials and no way to be handed any: node.env is
// regenerated from this instance's environment on every join, and shipping an
// IAM key to every machine in the fleet is exactly the thing worth avoiding. So
// a provider that needs a credential becomes its brokered form, which
// authenticates with the internal API token the node already holds and asks
// this instance to do the one privileged operation. Filesystem blobs need no
// credential and pass through unchanged.
//
// Local KMS is brokered for a worker too, so the master key that wraps every
// workspace key stays on the control plane. A consumer keeps it: a consumer
// may mint a workspace's first data key, which the broker never does.
//
// Brokering costs one HTTPS call to the control plane per DEK open (Redis
// caches the result) and per blob operation. The bytes still go straight
// between the node and the object store.
func nodeProviders(role models.NodeRole) (kmsProvider, blobProvider string) {
	kmsProvider = config.KMSProvider()
	switch {
	case kmsProvider == "aws" || kmsProvider == "aws-kms":
		kmsProvider = "brokered"
	case kmsProvider == "local" && role == models.NodeRoleWorker:
		kmsProvider = "brokered"
	}
	blobProvider = config.BlobProvider()
	if blobProvider == "s3" {
		blobProvider = "brokered"
	}
	return kmsProvider, blobProvider
}

// BackendURL is the control-plane address a node is told to call back on.
func BackendURL() string {
	backend := strings.TrimRight(os.Getenv("ENCRYPTED_KEYS_BACKEND_URL"), "/")
	if backend == "" {
		backend = strings.TrimRight(os.Getenv("APP_INTERNAL_URL"), "/")
	}
	return backend
}

// ErrBackendNotHTTPS names a cleartext control-plane address handed to nodes on hosted production.
var ErrBackendNotHTTPS = errors.New("hosted production hands nodes a cleartext control-plane address; keep ENCRYPTED_KEYS_BACKEND_URL on a private network or set it to an https:// URL")

// CheckBackendURL reports a non-https, non-loopback control-plane address on hosted production.
func CheckBackendURL() error {
	backend := BackendURL()
	if backend == "" || !config.HostedProduction() {
		return nil
	}
	u, err := url.Parse(backend)
	if err != nil {
		return ErrBackendNotHTTPS
	}
	if strings.EqualFold(u.Scheme, "https") || loopbackHost(u.Hostname()) {
		return nil
	}
	return ErrBackendNotHTTPS
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// RenderEnv builds the env file a node writes to disk on join.
func RenderEnv(nodeID uuid.UUID, role models.NodeRole, region string) string {
	var b strings.Builder
	b.WriteString("# Written by `warmbly join`. Regenerate by joining again.\n")
	fmt.Fprintf(&b, "WARMBLY_NODE_ID=%s\n", nodeID)
	fmt.Fprintf(&b, "WARMBLY_NODE_ROLE=%s\n", role)
	if region != "" {
		fmt.Fprintf(&b, "WARMBLY_NODE_REGION=%s\n", region)
	}
	// A worker resolves its own identity from WORKER_ID; keeping the two equal
	// means the placement row and the node row are the same machine.
	if role == models.NodeRoleWorker {
		fmt.Fprintf(&b, "WORKER_ID=%s\n", nodeID)
	}

	backend := BackendURL()
	// A consumer is control plane: it opens Postgres itself, so it gets the DSN
	// a worker is deliberately never given, and reads keys straight from the
	// table rather than back through the API it sits behind. An instance whose
	// DSN lives in SSM rather than the environment has nothing to send, so the
	// node falls back to the worker's HTTP key path and join.sh tells the
	// operator to supply PRIMARY_DB in node.local.env.
	dsn := os.Getenv("PRIMARY_DB")
	keysProvider := "http"
	if role == models.NodeRoleConsumer && dsn != "" {
		keysProvider = "postgres"
		fmt.Fprintf(&b, "PRIMARY_DB=%s\n", dsn)
	}

	fmt.Fprintf(&b, "WARMBLY_BACKEND_URL=%s\n", backend)
	fmt.Fprintf(&b, "ENCRYPTED_KEYS_PROVIDER=%s\n", keysProvider)
	fmt.Fprintf(&b, "ENCRYPTED_KEYS_BACKEND_URL=%s\n", backend)
	// A node calls only the node-only internal routes. With a separate
	// NODE_BROKER_TOKEN it is sent that alone and never the edge services' token.
	if v := os.Getenv("NODE_BROKER_TOKEN"); v != "" {
		fmt.Fprintf(&b, "ENCRYPTED_KEYS_WORKER_TOKEN=%s\n", v)
		fmt.Fprintf(&b, "NODE_BROKER_TOKEN=%s\n", v)
	} else {
		fmt.Fprintf(&b, "ENCRYPTED_KEYS_WORKER_TOKEN=%s\n", os.Getenv("INTERNAL_API_TOKEN"))
		fmt.Fprintf(&b, "INTERNAL_API_TOKEN=%s\n", os.Getenv("INTERNAL_API_TOKEN"))
	}

	kmsProvider, blobProvider := nodeProviders(role)
	fmt.Fprintf(&b, "KMS_PROVIDER=%s\n", kmsProvider)
	fmt.Fprintf(&b, "BLOB_PROVIDER=%s\n", blobProvider)
	// Resolved here: a node cannot tell a hosted instance from a self-host on its own.
	mode := "cloud"
	if config.SelfHosted() {
		mode = "self_hosted"
	}
	fmt.Fprintf(&b, "DEPLOYMENT_MODE=%s\n", mode)
	fmt.Fprintf(&b, "MAIL_ALLOW_PRIVATE_HOSTS=%t\n", config.PrivateMailHostsAllowed())
	fmt.Fprintf(&b, "MAIL_TLS_INSECURE=%t\n", config.MailTLSInsecureAllowed())

	keys := NodeEnvKeys
	if role == models.NodeRoleConsumer {
		keys = append(append([]string{}, NodeEnvKeys...), consumerEnvKeys...)
	}
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			fmt.Fprintf(&b, "%s=%s\n", k, v)
		}
	}
	return b.String()
}

// EnvRevision names one rendered env, so a node can tell its file is stale.
func EnvRevision(env string) string {
	sum := sha256.Sum256([]byte(env))
	return hex.EncodeToString(sum[:16])
}

// NodeEnvState is whether one live node runs the env the backend renders now.
type NodeEnvState struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Applied string `json:"applied,omitempty"`
	Tracked bool   `json:"tracked"`
	Current bool   `json:"current"`
}

// EnvStatus is the fleet's answer to "has every node picked up the current env".
type EnvStatus struct {
	AllCurrent bool           `json:"all_current"`
	Nodes      []NodeEnvState `json:"nodes"`
}

// EnvStatusOf compares each live node's applied revision with the env this process renders for it.
func EnvStatusOf(nodes []models.FleetNode) EnvStatus {
	out := EnvStatus{AllCurrent: true, Nodes: []NodeEnvState{}}
	for _, n := range nodes {
		if !n.Live() {
			continue
		}
		want := EnvRevision(RenderEnv(n.ID, n.Role, n.Region))
		row := NodeEnvState{ID: n.ID.String(), Name: n.Name, Role: string(n.Role), Applied: n.EnvRevision, Tracked: n.EnvRevision != "", Current: n.EnvRevision == want}
		out.AllCurrent = out.AllCurrent && row.Current
		out.Nodes = append(out.Nodes, row)
	}
	return out
}

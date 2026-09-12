package handler

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/fleetnode"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// The fleet is pull-based. A node joins with the instance token, gets the
// config it needs, then heartbeats forever; the reply to that heartbeat is the
// only channel by which the control plane tells it to do anything.
//
//	POST /api/v1/fleet/join                 join token   -> node id + env file
//	POST /api/v1/internal/fleet/heartbeat   internal     -> desired version
//
// Nothing here reaches into a machine, which is why onboarding needs no
// keypair, no inbound port and no cloud account.

type fleetJoinRequest struct {
	Token string `json:"token" binding:"required"`
	Role  string `json:"role"  binding:"required"`
	// NodeID lets a machine keep its identity across a rebuild. Omitted on a
	// first join, in which case the control plane assigns one.
	NodeID  string `json:"node_id,omitempty"`
	Name    string `json:"name,omitempty"`
	Region  string `json:"region,omitempty"`
	Address string `json:"address,omitempty"`
}

type fleetJoinResponse struct {
	NodeID uuid.UUID `json:"node_id"`
	Role   string    `json:"role"`
	// EnvB64 is the complete environment file the node should write, base64
	// encoded. Rendered from the backend's own configuration, so a node always
	// gets exactly the infrastructure the control plane is using.
	//
	// Base64 rather than a raw JSON string because the join script is POSIX sh
	// with no JSON parser: pulling a multi-line value containing quotes and
	// backslashes back out with sed is guesswork, and got it wrong. One
	// `base64 -d` is exact.
	EnvB64 string `json:"env_b64"`
	// DesiredVersion is what to run right now, so the first start is already
	// on the right version instead of starting stale and updating a beat later.
	DesiredVersion string `json:"desired_version,omitempty"`
	// HeartbeatSeconds is how often to beat. Derived from the server's liveness
	// window so the two can never drift apart.
	HeartbeatSeconds int `json:"heartbeat_seconds"`
}

// FleetJoin enrols a node. It is the one endpoint reachable with the join
// token rather than an operator session, because the machine running it has no
// credentials yet — that is the whole point.
func (h *Handler) FleetJoin(c *gin.Context) {
	if h.FleetNodes == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "fleet enrolment is not available on this instance"))
		return
	}
	var req fleetJoinRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	ctx := c.Request.Context()
	if err := h.FleetNodes.VerifyJoinToken(ctx, req.Token); err != nil {
		switch err {
		case fleetnode.ErrNoJoinToken:
			// Distinct from a wrong token on purpose: this is a setup problem,
			// and telling the operator so saves a long hunt.
			errx.JSON(c, errx.New(errx.BadRequest, "this instance has no join token yet; issue one from Fleet settings or with `warmblyctl fleet join-token`"))
		default:
			errx.JSON(c, errx.New(errx.Unauthorized, "join token is not valid"))
		}
		return
	}

	role := models.NodeRole(strings.TrimSpace(req.Role))
	if !role.Valid() {
		errx.JSON(c, errx.New(errx.BadRequest, "role must be worker or consumer"))
		return
	}

	nodeID := uuid.New()
	if req.NodeID != "" {
		parsed, err := uuid.Parse(req.NodeID)
		if err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "node_id is not a valid uuid"))
			return
		}
		nodeID = parsed
	}

	address := strings.TrimSpace(req.Address)
	if address == "" {
		address = c.ClientIP()
	}

	// Registering here rather than waiting for the first beat means the node
	// shows up in the dashboard the moment it joins, even if it then fails to
	// start. A join that silently produces nothing visible is the worst
	// possible onboarding experience.
	beat := models.NodeHeartbeat{
		NodeID:  nodeID,
		Role:    role,
		Name:    req.Name,
		Region:  req.Region,
		Address: address,
	}
	reply, err := h.FleetNodes.Heartbeat(ctx, beat)
	if err != nil {
		if errors.Is(err, fleetnode.ErrRoleChanged) {
			errx.JSON(c, errx.New(errx.BadRequest,
				"that machine is already enrolled as the other role. Remove the node first, which releases anything assigned to it, then join again."))
			return
		}
		errx.JSON(c, errx.New(errx.Internal, "enrol node: "+err.Error()))
		return
	}

	c.JSON(http.StatusOK, fleetJoinResponse{
		NodeID:           nodeID,
		Role:             string(role),
		EnvB64:           base64.StdEncoding.EncodeToString([]byte(renderNodeEnv(nodeID, role, req.Region))),
		DesiredVersion:   reply.DesiredVersion,
		HeartbeatSeconds: nodeHeartbeatSeconds(reply.LivenessSeconds),
	})
}

// FleetHeartbeat records a beat and answers with the version the node should
// be running. Authenticated with INTERNAL_API_TOKEN, the same shared secret
// the node already needs to read encrypted keys.
func (h *Handler) FleetHeartbeat(c *gin.Context) {
	if h.FleetNodes == nil {
		c.Status(http.StatusNoContent)
		return
	}
	var beat models.NodeHeartbeat
	if err := c.ShouldBindJSON(&beat); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "decode body"})
		return
	}
	reply, err := h.FleetNodes.Heartbeat(c.Request.Context(), beat)
	if err != nil {
		switch {
		case errors.Is(err, fleetnode.ErrBadRole):
			c.JSON(http.StatusBadRequest, gin.H{"error": "role must be worker or consumer"})
		case errors.Is(err, fleetnode.ErrRoleChanged):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	// A worker that just booted holds no mailboxes: they live in memory only.
	// Reload them now instead of leaving it to the reconciler's next pass,
	// during which every send to it would fail with "not found".
	if beat.Booted && beat.Role == models.NodeRoleWorker && h.EmailService != nil {
		// Off the request: the reload publishes one ADD_EMAIL per mailbox.
		go func(id uuid.UUID) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			h.EmailService.ReloadWorkerAccounts(ctx, id)
		}(beat.NodeID)
	}

	c.JSON(http.StatusOK, reply)
}

// nodeHeartbeatSeconds derives the beat interval from the server's liveness
// window: beat three times per window, so two lost beats still do not look
// like a dead machine.
func nodeHeartbeatSeconds(livenessSeconds int) int {
	if livenessSeconds <= 0 {
		return 90
	}
	n := livenessSeconds / 3
	if n < 15 {
		return 15
	}
	return n
}

// nodeEnvKeys are the settings a node needs to do its job, in the order they
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
// the internal API and nothing else. renderNodeEnv sends the DSN to a consumer,
// which is control plane and updates relational state itself.
var nodeEnvKeys = []string{
	"APP_ENV",
	"EVENTBUS_PROVIDER",
	"NATS_URL",
	// Base64, not a path: a node has no file to point at, and the env file
	// docker reads cannot hold a multi-line value.
	"NATS_CREDS_B64",
	"NATS_MAX_BYTES",
	"KAFKA_BOOTSTRAP_SERVERS",
	"KAFKA_SASL_USERNAME",
	"KAFKA_SASL_PASSWORD",
	"SCHEMA_REGISTRY_URL",
	"SCHEMA_REGISTRY_KEY",
	"SCHEMA_REGISTRY_SECRET",
	"CODEC_PROVIDER",
	"REDIS",
	"KMS_LOCAL_MASTER_KEY",
	"KMS_AWS_KEY_ID",
	"CREDENTIALS_ENCRYPTION_KEY",
	"BLOB_FS_ROOT",
	"BLOB_BUCKET",
	"AWS_REGION",
	"AWS_ENDPOINT_URL_S3",
	"BOX_GOOGLE_CLIENT_ID",
	"BOX_GOOGLE_CLIENT_SECRET",
	"BOX_OUTLOOK_CLIENT_ID",
	"BOX_OUTLOOK_CLIENT_SECRET",
	"MAIL_TLS_INSECURE",
	"POSTHOG_KEY",
	"POSTHOG_HOST",
	"POSTHOG_ERROR_TRACKING",
	"SENTRY_DSN",
}

// nodeProviders translates the control plane's own crypto and blob providers
// into the ones a node should run.
//
// A node has no cloud credentials and no way to be handed any: node.env is
// regenerated from this instance's environment on every join, and shipping an
// IAM key to every machine in the fleet is exactly the thing worth avoiding. So
// a provider that needs a credential becomes its brokered form, which
// authenticates with the internal API token the node already holds and asks
// this instance to do the one privileged operation. A provider that needs no
// credential (local KMS, filesystem blobs) passes through unchanged.
//
// Brokering costs one HTTPS call to the control plane per DEK open (Redis
// caches the result) and per blob operation. The bytes still go straight
// between the node and the object store.
func nodeProviders() (kmsProvider, blobProvider string) {
	kmsProvider = config.KMSProvider()
	if kmsProvider == "aws" || kmsProvider == "aws-kms" {
		kmsProvider = "brokered"
	}
	blobProvider = config.BlobProvider()
	if blobProvider == "s3" {
		blobProvider = "brokered"
	}
	return kmsProvider, blobProvider
}

// renderNodeEnv builds the env file a node writes to disk on join.
func renderNodeEnv(nodeID uuid.UUID, role models.NodeRole, region string) string {
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

	backend := strings.TrimRight(os.Getenv("ENCRYPTED_KEYS_BACKEND_URL"), "/")
	if backend == "" {
		backend = strings.TrimRight(os.Getenv("APP_INTERNAL_URL"), "/")
	}
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
	fmt.Fprintf(&b, "ENCRYPTED_KEYS_WORKER_TOKEN=%s\n", os.Getenv("INTERNAL_API_TOKEN"))
	fmt.Fprintf(&b, "INTERNAL_API_TOKEN=%s\n", os.Getenv("INTERNAL_API_TOKEN"))

	kmsProvider, blobProvider := nodeProviders()
	fmt.Fprintf(&b, "KMS_PROVIDER=%s\n", kmsProvider)
	fmt.Fprintf(&b, "BLOB_PROVIDER=%s\n", blobProvider)

	// The credential for the two endpoints that open a key and sign a blob
	// operation. Sent only when the instance issues a separate one; otherwise
	// the node falls back to the internal token it already has.
	if v := os.Getenv("NODE_BROKER_TOKEN"); v != "" {
		fmt.Fprintf(&b, "NODE_BROKER_TOKEN=%s\n", v)
	}

	for _, k := range nodeEnvKeys {
		if v := os.Getenv(k); v != "" {
			fmt.Fprintf(&b, "%s=%s\n", k, v)
		}
	}
	return b.String()
}

// ---- admin ----

// AdminFleetNodes lists the fleet: every worker and consumer, what version
// each is on, what it should be on, and what it is using.
func (h *Handler) AdminFleetNodes(c *gin.Context) {
	if h.FleetNodes == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "fleet enrolment is not available on this instance"))
		return
	}
	role := models.NodeRole(c.Query("role"))
	if role != "" && !role.Valid() {
		errx.JSON(c, errx.New(errx.BadRequest, "role must be worker or consumer"))
		return
	}
	nodes, err := h.FleetNodes.List(c.Request.Context(), role)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Role < nodes[j].Role })
	c.JSON(http.StatusOK, gin.H{"data": nodes})
}

// AdminFleetIssueJoinToken mints a new instance join token and returns it
// once. Issuing replaces the previous one, which is also how it is revoked.
func (h *Handler) AdminFleetIssueJoinToken(c *gin.Context) {
	if h.FleetNodes == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "fleet enrolment is not available on this instance"))
		return
	}
	token, err := h.FleetNodes.IssueJoinToken(c.Request.Context())
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, "fleet_join_token_issued", models.AuditEntityWorker, nil, nil)
	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"note":  "Shown once. Issuing a new token revokes this one; nodes already enrolled are unaffected.",
	})
}

// ---- node tags ----

type setTagsBody struct {
	Tags []string `json:"tags"`
}

// AdminListWorkerTags returns every tag in use, for the filter menu.
func (h *Handler) AdminListWorkerTags(c *gin.Context) {
	tags, err := h.WorkerRepo.ListAllWorkerTags(c.Request.Context())
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tags})
}

// AdminSetWorkerTags replaces a node's operator-applied tags.
func (h *Handler) AdminSetWorkerTags(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	var body setTagsBody
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	// Normalise: trim, lowercase, dedupe, drop empties. The DB constraint
	// rejects garbage too; this just avoids 500s on the happy path.
	seen := map[string]struct{}{}
	tags := make([]string, 0, len(body.Tags))
	for _, t := range body.Tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		tags = append(tags, t)
	}
	if err := h.WorkerRepo.SetWorkerTags(c.Request.Context(), id, tags); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
		return
	}
	h.audit(c, models.AuditActionUpdate, models.AuditEntityWorker, &id, map[string]string{
		"tags": strings.Join(tags, ","),
	})
	c.JSON(http.StatusOK, gin.H{"ok": true, "tags": tags})
}

// ---- isolated egress ----

type reserveWorkerBody struct {
	OrganizationID string `json:"organization_id" binding:"required"`
	SubscriptionID string `json:"subscription_id" binding:"required"`
}

// AdminFleetReserveWorker reserves a worker for one organization, so its
// mailboxes always sign in from an address no other tenant uses.
//
// It only writes the binding. Mailboxes already on the worker are not evicted
// here: the rotation loop moves other tenants off on its own schedule, and
// forcing them now would re-authenticate every one of them at once for no
// deliverability gain.
func (h *Handler) AdminFleetReserveWorker(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	if h.WorkerRepo == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "worker placement is not available on this instance"))
		return
	}
	var body reserveWorkerBody
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	orgID, err := uuid.Parse(body.OrganizationID)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid organization_id"))
		return
	}
	subID, err := uuid.Parse(body.SubscriptionID)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid subscription_id"))
		return
	}

	ctx := c.Request.Context()
	w, err := h.WorkerRepo.GetWorkerDetail(ctx, id)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	if w == nil {
		errx.JSON(c, errx.New(errx.NotFound, "worker not found"))
		return
	}

	created, err := h.WorkerRepo.CreateDedicatedAssignmentIfNotExists(ctx, &models.DedicatedWorkerAssignment{
		ID:             uuid.New(),
		WorkerID:       id,
		OrganizationID: orgID,
		SubscriptionID: subID,
		AssignedAt:     time.Now(),
	})
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "create reservation: "+err.Error()))
		return
	}

	h.audit(c, "fleet_reserve_worker", models.AuditEntityWorker, &id, map[string]string{
		"organization_id": orgID.String(),
		"subscription_id": subID.String(),
		"new_reservation": boolStr(created),
	})
	c.JSON(http.StatusOK, gin.H{"ok": true, "worker_id": id, "new_reservation": created})
}

// AdminFleetDeleteNode forgets a node.
//
// Its worker row cascades and its mailboxes are released, so they are re-placed
// on a live worker within a rotation pass rather than stranded. It does not
// stop anything on the machine: a node whose process is still running will
// re-join on its next heartbeat, which is deliberate — removing a row should
// not be a way to lose track of a machine that is still sending.
func (h *Handler) AdminFleetDeleteNode(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	if h.FleetNodeRepo == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "fleet enrolment is not available on this instance"))
		return
	}
	if err := h.FleetNodeRepo.Delete(c.Request.Context(), id); err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, models.AuditActionDelete, models.AuditEntityWorker, &id, nil)
	c.JSON(http.StatusOK, gin.H{
		"ok": true,
		"note": "Any mailboxes it carried will be re-placed within a few minutes. " +
			"Stop the service on that machine too, or it will re-join on its next heartbeat.",
	})
}

type patchNodeBody struct {
	Name  *string `json:"name,omitempty"`
	Notes *string `json:"notes,omitempty"`
	// PinnedVersion holds this node at a version. An empty string clears the
	// pin and returns it to the fleet target.
	PinnedVersion *string `json:"pinned_version,omitempty"`
}

// AdminFleetPatchNode applies the only things an operator sets on a node: what
// it is called, a note, and whether it is held at a version.
func (h *Handler) AdminFleetPatchNode(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	if h.FleetNodeRepo == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "fleet enrolment is not available on this instance"))
		return
	}
	var body patchNodeBody
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	ctx := c.Request.Context()
	changed := map[string]string{}
	if body.Name != nil {
		if err := h.FleetNodeRepo.SetName(ctx, id, *body.Name); err != nil {
			errx.JSON(c, errx.New(errx.Internal, err.Error()))
			return
		}
		changed["name"] = *body.Name
	}
	if body.Notes != nil {
		if err := h.FleetNodeRepo.SetNotes(ctx, id, *body.Notes); err != nil {
			errx.JSON(c, errx.New(errx.Internal, err.Error()))
			return
		}
		changed["notes"] = *body.Notes
	}
	if body.PinnedVersion != nil {
		if err := h.FleetNodeRepo.SetPinnedVersion(ctx, id, *body.PinnedVersion); err != nil {
			errx.JSON(c, errx.New(errx.Internal, err.Error()))
			return
		}
		changed["pinned_version"] = *body.PinnedVersion
	}
	if len(changed) == 0 {
		errx.JSON(c, errx.New(errx.BadRequest, "nothing to change"))
		return
	}

	h.audit(c, models.AuditActionUpdate, models.AuditEntityWorker, &id, changed)
	node, err := h.FleetNodes.Get(ctx, id)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	c.JSON(http.StatusOK, node)
}

// AdminFleetRelease reports, and optionally sets, the version the fleet should
// converge on.
func (h *Handler) AdminFleetRelease(c *gin.Context) {
	if h.FleetSettingsRepo == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "fleet enrolment is not available on this instance"))
		return
	}
	state, err := h.FleetSettingsRepo.GetRelease(c.Request.Context())
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	if state == nil {
		state = &models.FleetReleaseState{Channel: models.FleetChannelStable}
	}
	c.JSON(http.StatusOK, state)
}

type setReleaseBody struct {
	Channel string `json:"channel,omitempty"`
	Tag     string `json:"tag,omitempty"`
}

// AdminFleetSetRelease moves the whole fleet. Setting a tag pins the channel
// too, so the next release check does not immediately undo a deliberate
// rollback.
func (h *Handler) AdminFleetSetRelease(c *gin.Context) {
	if h.FleetSettingsRepo == nil {
		errx.JSON(c, errx.New(errx.NotImplemented, "fleet enrolment is not available on this instance"))
		return
	}
	var body setReleaseBody
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	ctx := c.Request.Context()
	state, err := h.FleetSettingsRepo.GetRelease(ctx)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	if state == nil {
		state = &models.FleetReleaseState{}
	}

	if body.Tag != "" {
		state.Tag = strings.TrimSpace(body.Tag)
		state.Channel = models.FleetChannelPinned
		state.ResolvedAt = time.Now()
		state.Source = "admin"
	} else if body.Channel != "" {
		switch body.Channel {
		case models.FleetChannelStable, models.FleetChannelDev, models.FleetChannelPinned:
			state.Channel = body.Channel
		default:
			errx.JSON(c, errx.New(errx.BadRequest, "channel must be stable, dev or pinned"))
			return
		}
	} else {
		errx.JSON(c, errx.New(errx.BadRequest, "supply a channel or a tag"))
		return
	}

	if err := h.FleetSettingsRepo.SetRelease(ctx, state); err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, "fleet_release_set", models.AuditEntityWorker, nil, map[string]string{
		"channel": state.Channel,
		"tag":     state.Tag,
	})
	c.JSON(http.StatusOK, state)
}

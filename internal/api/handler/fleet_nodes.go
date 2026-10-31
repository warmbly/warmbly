package handler

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/rs/zerolog/log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/fleetnode"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/nodeauth"
	"github.com/warmbly/warmbly/internal/pkg/publicip"
)

// The fleet is pull-based. A node joins with the instance token, gets the
// config it needs, then heartbeats forever; the reply to that heartbeat is the
// only channel by which the control plane tells it to do anything.
//
//	POST /api/v1/fleet/join                 join token   -> node id + env file
//	POST /api/v1/internal/fleet/heartbeat   internal     -> desired version
//	GET  /api/v1/internal/fleet/node-env    node broker + that node's credential -> current env file
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
		errx.JSON(c, errx.InvalidBody(err))
		return
	}

	ctx := c.Request.Context()
	if err := h.FleetNodes.VerifyJoinToken(ctx, req.Token); err != nil {
		switch err {
		case fleetnode.ErrNoJoinToken:
			// Distinct from a wrong token on purpose: this is a setup problem,
			// and telling the operator so saves a long hunt.
			errx.JSON(c, errx.New(errx.BadRequest, "this instance has no join token yet; issue one from Fleet settings or with `warmblyctl fleet join-token`"))
		case fleetnode.ErrJoinTokenExpired:
			errx.JSON(c, errx.New(errx.Unauthorized, "join token has expired; issue a new one from Fleet settings or with `warmblyctl fleet join-token`, then run the join command again"))
		default:
			errx.JSON(c, errx.New(errx.Unauthorized, "join token is not valid; it may have been replaced by a newer one. Issue a new one from Fleet settings or with `warmblyctl fleet join-token`, then run the join command again"))
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

	address := heartbeatAddress(req.Address)
	// A cleartext address inside a private network is common, so this warns rather than refusing a join.
	if err := fleetnode.CheckBackendURL(); err != nil {
		log.Warn().Str("node_id", nodeID.String()).Msg(err.Error())
	}
	joinVersion := h.FleetNodes.JoinVersion(ctx, nodeID)
	if joinVersion == "" {
		errx.JSON(c, errx.New(errx.Internal, "cannot resolve a backend-compatible node image; configure WARMBLY_VERSION for unstamped builds"))
		return
	}

	if h.NodeLogs == nil {
		errx.JSON(c, errx.ErrServiceDown)
		return
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

	logToken, logErr := h.NodeLogs.Enroll(ctx, nodeID)
	if logErr != nil {
		errx.JSON(c, errx.ErrServiceDown)
		return
	}
	// The node's own credential: its env is handed only to a caller presenting it.
	credential, credErr := h.FleetNodes.IssueNodeCredential(ctx, nodeID)
	if credErr != nil {
		errx.JSON(c, errx.ErrServiceDown)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, fleetJoinResponse{
		NodeID: nodeID,
		Role:   string(role),
		EnvB64: base64.StdEncoding.EncodeToString([]byte(renderNodeEnv(nodeID, role, req.Region) +
			"\nNODE_LOG_TOKEN=" + logToken + "\nNODE_CREDENTIAL=" + credential + "\n")),
		// Not reply.DesiredVersion: an empty answer is "no opinion" to a node
		// that is already running something, but this one has nothing to run.
		DesiredVersion:   joinVersion,
		HeartbeatSeconds: nodeHeartbeatSeconds(reply.LivenessSeconds),
	})
}

// FleetHeartbeat records a beat and answers with the version the node should
// be running. Authenticated with the node token (NODE_BROKER_TOKEN, falling
// back to INTERNAL_API_TOKEN), the one the node reads encrypted keys with.
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
	if middleware.IsNodeUpdateOnly(c) {
		h.fleetUpdateOnlyHeartbeat(c, beat)
		return
	}
	if caller, ok := middleware.AuthenticatedNodeID(c); ok {
		if caller != beat.NodeID {
			c.JSON(http.StatusForbidden, gin.H{"error": "a node may heartbeat only as itself"})
			return
		}
	} else if _, seen := fleetTokenNodes.LoadOrStore(beat.NodeID, struct{}{}); !seen {
		log.Warn().Str("node_id", beat.NodeID.String()).Msg("fleet: node heartbeats without its own node credential; it will be refused once NODE_REQUIRE_NODE_CREDENTIAL is on. Update it, or join it again if it predates node credentials")
	}
	beat.Address = heartbeatAddress(beat.Address)
	reply, err := h.FleetNodes.Heartbeat(c.Request.Context(), beat)
	if err != nil {
		switch {
		case errors.Is(err, fleetnode.ErrBadRole):
			c.JSON(http.StatusBadRequest, gin.H{"error": "role must be worker or consumer"})
		case errors.Is(err, fleetnode.ErrRoleChanged):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			// The heartbeat is answered to a machine, not a person, and the
			// caller authenticates with a node credential rather than an
			// operator session. The detail goes to the log where an operator
			// reads it; the node only needs to know to retry.
			errs.CaptureException(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "heartbeat could not be recorded"})
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

	if beat.EnvRefresh {
		if _, rev, ok := h.nodeEnvFor(c.Request.Context(), beat.NodeID, beat.Role); ok {
			reply.EnvRevision = rev
		}
	}
	c.JSON(http.StatusOK, reply)
}

// nodeEnvFor renders the env an enrolled node of role should have, with its revision.
func (h *Handler) nodeEnvFor(ctx context.Context, id uuid.UUID, role models.NodeRole) (string, string, bool) {
	node, err := h.FleetNodes.Get(ctx, id)
	if err != nil || node == nil || node.Role != role {
		return "", "", false
	}
	env := renderNodeEnv(node.ID, node.Role, node.Region)
	return env, fleetnode.EnvRevision(env), true
}

// nodeCredentialHeader carries the credential join issued to one node, next to the fleet bearer.
const nodeCredentialHeader = nodeauth.CredentialHeader

// InternalFleetNodeEnv hands an enrolled node the env file join would write
// today, so a rotated value reaches it by pulling instead of rejoining. The
// caller proves which node it is with that node's own credential; an updater
// from before node credentials is answered only for a worker, whose env holds
// nothing a fleet token holder lacks.
func (h *Handler) InternalFleetNodeEnv(c *gin.Context) {
	const op = "fleet.node_env"
	c.Header("Cache-Control", "no-store")
	if h.FleetNodes == nil {
		c.Status(http.StatusNotFound)
		return
	}
	id, err := uuid.Parse(c.Query("node_id"))
	if err != nil {
		logBrokerAccess(c, op, "", false, "node_id is not a uuid")
		c.JSON(http.StatusBadRequest, gin.H{"error": "node_id must be a uuid"})
		return
	}
	role := models.NodeRole(c.Query("role"))
	if !role.Valid() {
		logBrokerAccess(c, op, id.String(), false, "unknown role")
		c.JSON(http.StatusBadRequest, gin.H{"error": "role must be worker or consumer"})
		return
	}
	if err := fleetnode.CheckBackendURL(); err != nil {
		log.Warn().Str("node_id", id.String()).Msg(err.Error())
	}
	ctx := c.Request.Context()
	reason := "node credential"
	if caller, ok := middleware.AuthenticatedNodeID(c); ok {
		if caller != id {
			logBrokerAccess(c, op, id.String(), false, "node asked for another node's env")
			c.JSON(http.StatusForbidden, gin.H{"error": "a node may fetch only its own env"})
			return
		}
	} else if credential := c.GetHeader(nodeCredentialHeader); credential != "" {
		switch err := h.FleetNodes.VerifyNodeCredential(ctx, id, credential); {
		case errors.Is(err, fleetnode.ErrNoNodeCredential), errors.Is(err, fleetnode.ErrBadNodeCredential):
			logBrokerAccess(c, op, id.String(), false, err.Error())
			c.JSON(http.StatusUnauthorized, gin.H{"error": "node credential does not match this node; join this machine again"})
			return
		case err != nil:
			logBrokerAccess(c, op, id.String(), false, "credential lookup failed")
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "node credential could not be checked"})
			return
		}
	} else if role != models.NodeRoleWorker {
		logBrokerAccess(c, op, id.String(), false, "consumer env requires the node credential")
		c.JSON(http.StatusForbidden, gin.H{"error": "a consumer's env is handed only to the node itself; join this machine again so its updater presents the node credential"})
		return
	} else {
		reason = "worker updater without a node credential"
	}
	env, rev, ok := h.nodeEnvFor(ctx, id, role)
	if !ok {
		logBrokerAccess(c, op, id.String(), false, "no enrolled node with that id and role")
		c.JSON(http.StatusNotFound, gin.H{"error": "no enrolled node with that id and role"})
		return
	}
	logBrokerAccess(c, op, id.String(), true, reason)
	c.Header("X-Warmbly-Env-Revision", rev)
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(env))
}

// legacyTokenNodes remembers which nodes were already warned about, so the log says it once per process.
var legacyTokenNodes sync.Map

// fleetTokenNodes remembers which nodes were already warned for beating on the fleet token alone.
var fleetTokenNodes sync.Map

// fleetUpdateOnlyHeartbeat answers a node still on INTERNAL_API_TOKEN with its version only.
func (h *Handler) fleetUpdateOnlyHeartbeat(c *gin.Context, beat models.NodeHeartbeat) {
	reply, err := h.FleetNodes.UpdateOnly(c.Request.Context(), beat)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, seen := legacyTokenNodes.LoadOrStore(beat.NodeID, struct{}{}); !seen {
		log.Warn().Str("node_id", beat.NodeID.String()).Str("desired_version", reply.DesiredVersion).Msg("fleet: node heartbeats with INTERNAL_API_TOKEN; it is told its version but not recorded as live. A node that stays on this token after updating needs to join again to receive NODE_BROKER_TOKEN")
	}
	c.JSON(http.StatusOK, reply)
}

// A backend may see a reverse proxy rather than the worker's mail egress address.
func heartbeatAddress(reported string) string {
	if normalized, ok := publicip.IPv4(reported); ok {
		return normalized
	}
	return ""
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

// renderNodeEnv builds the env file a node writes to disk on join.
func renderNodeEnv(nodeID uuid.UUID, role models.NodeRole, region string) string {
	return fleetnode.RenderEnv(nodeID, role, region)
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
	token, expiresAt, err := h.FleetNodes.IssueJoinToken(c.Request.Context(), fleetnode.DefaultJoinTokenTTL)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
		return
	}
	h.audit(c, "fleet_join_token_issued", models.AuditEntityWorker, nil, map[string]string{"expires_at": expiresAt.Format(time.RFC3339)})
	c.JSON(http.StatusOK, gin.H{
		"token":      token,
		"expires_at": expiresAt,
		"note":       "Shown once. It joins any number of machines until it expires; issuing a new token revokes this one. Nodes already enrolled are unaffected.",
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
		errx.JSON(c, errx.InvalidBody(err))
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
		errx.JSON(c, errx.New(errx.Internal, err.Error()))
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
		errx.JSON(c, errx.InvalidBody(err))
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
	if h.NodeLogs == nil {
		errx.JSON(c, errx.ErrServiceDown)
		return
	}
	if err := h.NodeLogs.Revoke(c.Request.Context(), id); err != nil {
		errx.JSON(c, errx.ErrServiceDown)
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
		errx.JSON(c, errx.InvalidBody(err))
		return
	}

	ctx := c.Request.Context()
	changed := map[string]string{}
	if body.PinnedVersion != nil && *body.PinnedVersion != "" && h.FleetNodes != nil {
		if err := h.FleetNodes.CheckTarget(*body.PinnedVersion); err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
			return
		}
	}
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
		errx.JSON(c, errx.InvalidBody(err))
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
		if h.FleetNodes != nil {
			if err := h.FleetNodes.CheckTarget(strings.TrimSpace(body.Tag)); err != nil {
				errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
				return
			}
		}
		state.Tag = strings.TrimSpace(body.Tag)
		// An explicit tag moves everyone at once, replacing any staged rollout.
		state.Rollout = nil
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

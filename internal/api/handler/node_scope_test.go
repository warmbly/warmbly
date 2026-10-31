package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/fleetnode"
	"github.com/warmbly/warmbly/internal/app/nodescope"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/nodeauth"
	"github.com/warmbly/warmbly/internal/repository"
)

// scopeRepo assigns one mailbox of one organization to one worker.
type scopeRepo struct {
	repository.NodeScopeRepository
	roles   map[uuid.UUID]models.NodeRole
	worker  uuid.UUID
	mailbox repository.NodeMailbox
}

func (r *scopeRepo) NodeRole(_ context.Context, id uuid.UUID) (models.NodeRole, bool, error) {
	role, ok := r.roles[id]
	return role, ok, nil
}

func (r *scopeRepo) WorkerMailboxes(_ context.Context, w uuid.UUID, _ time.Duration) ([]repository.NodeMailbox, error) {
	if w == r.worker {
		return []repository.NodeMailbox{r.mailbox}, nil
	}
	return nil, nil
}

func (r *scopeRepo) WorkerHoldsMailbox(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Duration) (bool, error) {
	return false, nil
}

func (r *scopeRepo) WorkerHoldsOrganization(context.Context, uuid.UUID, uuid.UUID, time.Duration) (bool, error) {
	return false, nil
}

func (r *scopeRepo) OrganizationDataKeys(context.Context, []uuid.UUID) ([]string, error) {
	return nil, nil
}

type stubKeys struct{}

func (stubKeys) Put(context.Context, uuid.UUID, string) error   { return nil }
func (stubKeys) Get(context.Context, uuid.UUID) (string, error) { return "sealed", nil }
func (stubKeys) Name() string                                   { return "stub" }

type stubSyncState struct {
	repository.EmailSyncStateRepository
}

func (s stubSyncState) IsOwnConversation(context.Context, uuid.UUID, uuid.UUID, []string, string) (bool, error) {
	return true, nil
}

func TestWorkerNodeCallsAreBoundToItsAssignment(t *testing.T) {
	setInstanceEnv(t)
	t.Setenv("NODE_BROKER_TOKEN", "fleet")
	t.Setenv("NODE_REQUIRE_NODE_CREDENTIAL", "")
	var logs bytes.Buffer
	prev := zlog.Logger
	zlog.Logger = zerolog.New(&logs)
	defer func() { zlog.Logger = prev }()

	worker, otherWorker, consumer := uuid.New(), uuid.New(), uuid.New()
	user, org, otherOrg, mailbox := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	roles := map[uuid.UUID]models.NodeRole{worker: models.NodeRoleWorker, otherWorker: models.NodeRoleWorker, consumer: models.NodeRoleConsumer}
	fleet := &fleetOf{roles: roles, hashes: map[uuid.UUID]string{}}
	svc := fleetnode.New(fleet, nil, joinSettings{})
	creds := map[uuid.UUID]string{}
	for id := range roles {
		secret, err := svc.IssueNodeCredential(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		creds[id] = secret
	}
	h := &Handler{
		FleetNodes:     svc,
		NodeScope:      nodescope.New(&scopeRepo{roles: roles, worker: worker, mailbox: repository.NodeMailbox{ID: mailbox, UserID: user, OrganizationID: &org}}, nil),
		EncryptedKeys:  stubKeys{},
		EmailSyncState: stubSyncState{},
	}
	m := &middleware.Handler{NodeCredentials: svc}
	r := gin.New()
	r.GET("/api/v1/internal/dek/:orgID", m.NodeAuthMiddleware(), h.InternalGetDEK)
	r.GET("/api/v1/internal/worker/config", m.NodeAuthMiddleware(), h.InternalWorkerConfig)
	r.GET("/api/v1/internal/sync/own-conversation", m.NodeAuthMiddleware(), h.InternalSyncOwnConversation)

	call := func(node uuid.UUID, path string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if node == uuid.Nil {
			req.Header.Set("Authorization", "Bearer fleet")
		} else {
			req.Header.Set(nodeauth.IDHeader, node.String())
			req.Header.Set(nodeauth.CredentialHeader, creds[node])
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	sync := func(u, box uuid.UUID) string {
		return "/api/v1/internal/sync/own-conversation?user_id=" + u.String() + "&email_id=" + box.String() + "&message_ids=a"
	}

	for _, tc := range []struct {
		name string
		node uuid.UUID
		path string
		code int
	}{
		{"worker: assigned organization's key", worker, "/api/v1/internal/dek/" + org.String(), http.StatusOK},
		{"worker: another organization's key", worker, "/api/v1/internal/dek/" + otherOrg.String(), http.StatusNotFound},
		{"worker: its own config", worker, "/api/v1/internal/worker/config?worker_id=" + worker.String(), http.StatusOK},
		{"worker: another worker's config", worker, "/api/v1/internal/worker/config?worker_id=" + otherWorker.String(), http.StatusNotFound},
		{"worker: assigned mailbox sync", worker, sync(user, mailbox), http.StatusOK},
		{"worker: unassigned mailbox sync", worker, sync(user, uuid.New()), http.StatusNotFound},
		{"other worker: that mailbox", otherWorker, sync(user, mailbox), http.StatusNotFound},
		{"consumer: any organization", consumer, "/api/v1/internal/dek/" + otherOrg.String(), http.StatusOK},
		{"consumer: any mailbox", consumer, sync(user, uuid.New()), http.StatusOK},
		{"fleet token: any organization", uuid.Nil, "/api/v1/internal/dek/" + otherOrg.String(), http.StatusOK},
		{"fleet token: any worker", uuid.Nil, "/api/v1/internal/worker/config?worker_id=" + otherWorker.String(), http.StatusOK},
	} {
		if got := call(tc.node, tc.path); got != tc.code {
			t.Fatalf("%s: %d, want %d", tc.name, got, tc.code)
		}
	}
	call(worker, "/api/v1/internal/dek/"+otherOrg.String())

	out := logs.String()
	if n := strings.Count(out, `"route":"dek.get"`); n != 1 {
		t.Fatalf("a refusal is logged once per node and route, got %d: %s", n, out)
	}
	if !strings.Contains(out, `"node_id":"`+worker.String()+`"`) {
		t.Fatalf("the refusal names the node: %s", out)
	}
	for _, secret := range creds {
		if strings.Contains(out, secret) {
			t.Fatal("the log carried a node credential")
		}
	}
}

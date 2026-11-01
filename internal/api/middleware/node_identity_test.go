package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/fleetnode"
	"github.com/warmbly/warmbly/internal/pkg/nodeauth"
	"github.com/warmbly/warmbly/internal/repository"
)

// credentialRegistry holds credential hashes the way fleet_nodes does; a removed node has no row.
type credentialRegistry struct {
	repository.FleetNodeRepository
	hashes map[uuid.UUID]string
	err    error
}

func (r *credentialRegistry) SetCredentialHash(_ context.Context, id uuid.UUID, hash string) error {
	r.hashes[id] = hash
	return nil
}

func (r *credentialRegistry) CredentialHash(_ context.Context, id uuid.UUID) (string, error) {
	return r.hashes[id], r.err
}

type nodeCall struct {
	id, credential, bearer string
}

type nodeOutcome struct {
	code   int
	nodeID uuid.UUID
	named  bool
}

// nodeRouteOutcome sends one call through each node middleware and fails unless all three agree.
func nodeRouteOutcome(t *testing.T, h *Handler, call nodeCall) nodeOutcome {
	t.Helper()
	brokerTokenOnce = sync.Once{}
	brokerToken = nil
	mws := map[string]gin.HandlerFunc{
		"broker":    h.NodeBrokerAuthMiddleware(),
		"node":      h.NodeAuthMiddleware(),
		"heartbeat": h.NodeHeartbeatAuthMiddleware(),
	}
	var first *nodeOutcome
	for name, mw := range mws {
		var got nodeOutcome
		r := gin.New()
		r.POST("/n", mw, func(c *gin.Context) {
			got.nodeID, got.named = AuthenticatedNodeID(c)
			c.Status(http.StatusOK)
		})
		req := httptest.NewRequest(http.MethodPost, "/n", nil)
		if call.bearer != "" {
			req.Header.Set("Authorization", "Bearer "+call.bearer)
		}
		if call.id != "" {
			req.Header.Set(nodeauth.IDHeader, call.id)
		}
		if call.credential != "" {
			req.Header.Set(nodeauth.CredentialHeader, call.credential)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		got.code = w.Code
		if first == nil {
			first = &got
		} else if *first != got {
			t.Fatalf("%s middleware answered %+v, the others %+v", name, got, *first)
		}
	}
	return *first
}

func TestNodeRoutesAuthenticateTheNode(t *testing.T) {
	t.Setenv("INTERNAL_API_TOKEN", "edge")
	t.Setenv("NODE_BROKER_TOKEN", "fleet")
	t.Setenv("NODE_ACCEPT_INTERNAL_TOKEN", "")

	reg := &credentialRegistry{hashes: map[uuid.UUID]string{}}
	svc := fleetnode.New(reg, nil, nil)
	node, other, removed := uuid.New(), uuid.New(), uuid.New()
	secret, err := svc.IssueNodeCredential(t.Context(), node)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IssueNodeCredential(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	removedSecret, err := svc.IssueNodeCredential(t.Context(), removed)
	if err != nil {
		t.Fatal(err)
	}
	delete(reg.hashes, removed)
	h := &Handler{NodeCredentials: svc}

	for _, required := range []string{"", "true"} {
		t.Run("required="+required, func(t *testing.T) {
			t.Setenv("NODE_REQUIRE_NODE_CREDENTIAL", required)
			strict := required == "true"
			fleetOnly := http.StatusOK
			if strict {
				fleetOnly = http.StatusUnauthorized
			}
			for _, tc := range []struct {
				name  string
				call  nodeCall
				code  int
				named bool
			}{
				{"own credential", nodeCall{node.String(), secret, ""}, http.StatusOK, true},
				{"own credential next to the fleet token", nodeCall{node.String(), secret, "fleet"}, http.StatusOK, true},
				{"fleet token alone", nodeCall{"", "", "fleet"}, fleetOnly, false},
				{"another node's id", nodeCall{other.String(), secret, ""}, http.StatusUnauthorized, false},
				{"wrong credential", nodeCall{node.String(), "forged", ""}, http.StatusUnauthorized, false},
				{"removed node", nodeCall{removed.String(), removedSecret, ""}, http.StatusUnauthorized, false},
				{"unknown node", nodeCall{uuid.NewString(), secret, ""}, http.StatusUnauthorized, false},
				{"id that is not a uuid", nodeCall{"node-1", secret, ""}, http.StatusUnauthorized, false},
				{"credential without an id", nodeCall{"", secret, ""}, http.StatusUnauthorized, false},
				{"nothing", nodeCall{}, http.StatusUnauthorized, false},
				{"wrong credential with the fleet token", nodeCall{node.String(), "forged", "fleet"}, fleetOnly, false},
			} {
				got := nodeRouteOutcome(t, h, tc.call)
				if got.code != tc.code || got.named != tc.named || (tc.named && got.nodeID != node) {
					t.Fatalf("%s: got %+v, want %d named=%v", tc.name, got, tc.code, tc.named)
				}
			}
		})
	}
}

func TestNodeCredentialLookupFailure(t *testing.T) {
	t.Setenv("NODE_BROKER_TOKEN", "fleet")
	reg := &credentialRegistry{hashes: map[uuid.UUID]string{}}
	svc := fleetnode.New(reg, nil, nil)
	node := uuid.New()
	secret, err := svc.IssueNodeCredential(t.Context(), node)
	if err != nil {
		t.Fatal(err)
	}
	reg.err = errors.New("database unavailable")
	h := &Handler{NodeCredentials: svc}

	t.Setenv("NODE_REQUIRE_NODE_CREDENTIAL", "true")
	if got := nodeRouteOutcome(t, h, nodeCall{node.String(), secret, "fleet"}); got.code != http.StatusServiceUnavailable {
		t.Fatalf("required: a failed lookup answered %d, want 503", got.code)
	}
	t.Setenv("NODE_REQUIRE_NODE_CREDENTIAL", "false")
	if got := nodeRouteOutcome(t, h, nodeCall{node.String(), secret, "fleet"}); got.code != http.StatusOK || got.named {
		t.Fatalf("not required: a failed lookup must fall back to the fleet token, got %+v", got)
	}
}

func TestNodeCredentialRequiredWithoutRegistryRefusesEverything(t *testing.T) {
	t.Setenv("NODE_BROKER_TOKEN", "fleet")
	t.Setenv("NODE_REQUIRE_NODE_CREDENTIAL", "true")
	got := nodeRouteOutcome(t, &Handler{}, nodeCall{uuid.NewString(), "secret", "fleet"})
	if got.code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", got.code)
	}
}

// Package nodeauth is how a fleet node names itself on the node-only internal routes.
package nodeauth

import (
	"net/http"
	"os"
	"strings"
)

const (
	// IDHeader names the node making the call.
	IDHeader = "X-Warmbly-Node-Id"
	// CredentialHeader carries the credential join issued to that node.
	CredentialHeader = "X-Warmbly-Node-Credential"
)

// Identity is the node id and credential this process presents.
type Identity struct {
	ID         string
	Credential string
}

// FromEnv reads the identity join wrote to node.env: WARMBLY_NODE_ID (else WORKER_ID) and NODE_CREDENTIAL.
func FromEnv() Identity {
	id := strings.TrimSpace(os.Getenv("WARMBLY_NODE_ID"))
	if id == "" {
		id = strings.TrimSpace(os.Getenv("WORKER_ID"))
	}
	return Identity{ID: id, Credential: strings.TrimSpace(os.Getenv("NODE_CREDENTIAL"))}
}

// Complete reports whether both halves are present.
func (i Identity) Complete() bool {
	return i.ID != "" && i.Credential != ""
}

// Authorize sets the fleet bearer and, when this process holds one, its own node identity.
func Authorize(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if id := FromEnv(); id.Complete() {
		req.Header.Set(IDHeader, id.ID)
		req.Header.Set(CredentialHeader, id.Credential)
	}
}

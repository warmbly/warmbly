package provisioning

import (
	"context"

	"github.com/google/uuid"
)

// Installer is the SSH-driven side of provisioning: given a freshly-created
// server with a known SSH key, copy the worker binary + env, configure the
// OS to bind each Primary IP, and start one systemd unit per IP.
//
// The real implementation lives in internal/app/worker_orchestrator (which
// already does single-IP installs over SSH). The provisioning state machine
// depends only on this small interface so it stays testable and so we can
// later swap to cloud-init / Ansible / whatever without touching it.
type Installer interface {
	// Install runs scripts/install-worker.sh on the target server. ips
	// is the comma-separated list of attached Primary IPs. workerEnv is
	// the rendered content of /etc/warmbly/worker.env.
	Install(ctx context.Context, req InstallRequest) (*InstallResult, error)
}

// InstallRequest is what Install takes.
type InstallRequest struct {
	Host        string      // public IP or hostname the SSH session connects to
	SSHPort     int         // default 22
	SSHUser     string      // default root
	SSHKeyPEM   []byte      // private key bytes (PEM-encoded ed25519)
	IPs         []string    // dotted-quad strings, will be passed to --ips
	WorkerEnv   string      // rendered /etc/warmbly/worker.env contents
	ImageTag    string      // e.g. ghcr.io/warmbly/worker:v1.2.3
	ExpectedIDs []uuid.UUID // workers that should heartbeat after install
}

// InstallResult tells the state machine what's running on the box.
type InstallResult struct {
	InstalledWorkerIDs []uuid.UUID // typically the same as ExpectedIDs
	Logs               string      // captured installer output, optional
}

// StubInstaller is a no-op that records its inputs. Useful for tests and
// for environments without SSH access.
type StubInstaller struct {
	Calls []InstallRequest
	Err   error
}

func (s *StubInstaller) Install(_ context.Context, req InstallRequest) (*InstallResult, error) {
	s.Calls = append(s.Calls, req)
	if s.Err != nil {
		return nil, s.Err
	}
	return &InstallResult{InstalledWorkerIDs: req.ExpectedIDs}, nil
}

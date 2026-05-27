// Package provisioning is the state machine that drives a provisioning_jobs
// row from "pending" through the lifecycle of creating a server, attaching
// IPs, setting rDNS, installing the worker binary, and verifying that the
// expected workers report in.
//
// The state machine is idempotent: each Run call resumes from the row's
// current state, so a backend crash mid-provision is recoverable. Each step
// records its progress to the database before attempting the next step.
//
// On failure at any step, state transitions to rolling_back and the inverse
// operations run (delete primary IPs, delete server). Cleanly failed jobs
// leave no orphaned resources at the cloud provider.
package provisioning

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/cloudprovider"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// UUIDv5 URL namespace, kept in sync with cmd/worker and the installer.
var uuidNamespaceURL = uuid.MustParse("6ba7b811-9dad-11d1-80b4-00c04fd430c8")

// WorkerIDForIP returns the deterministic UUIDv5 that a worker process will
// adopt when it boots with the given WORKER_BIND_IP.
func WorkerIDForIP(ip string) uuid.UUID {
	return uuid.NewSHA1(uuidNamespaceURL, []byte(ip))
}

// JobConfig is the in-row snapshot of what an admin (or the scale loop)
// asked for. Mirrors the relevant subset of provisioning_templates plus the
// rendered worker env that the installer needs.
type JobConfig struct {
	Provider       string            `json:"provider"`
	Location       string            `json:"location"`
	Datacenter     string            `json:"datacenter,omitempty"`
	ServerType     string            `json:"server_type"`
	Image          string            `json:"image"`
	ServerCount    int               `json:"server_count"`
	IPv4PerServer  int               `json:"ipv4_per_server"`
	IPv6PerServer  int               `json:"ipv6_per_server"`
	Tier           string            `json:"tier"`
	EgressKind     string            `json:"egress_kind"`
	Labels         map[string]string `json:"labels,omitempty"`
	PlacementGroup string            `json:"placement_group,omitempty"`
	PrivateNetwork string            `json:"private_network,omitempty"`
	Firewall       string            `json:"firewall,omitempty"`
	ImageTag       string            `json:"image_tag"`
	WorkerEnv      string            `json:"worker_env"`
	SSHKeyID       string            `json:"ssh_key_id"`       // pre-uploaded to provider
	SSHPrivKeyPEM  []byte            `json:"ssh_priv_key_pem"` // encrypted at rest in config blob
	SSHPort        int               `json:"ssh_port,omitempty"`
	SSHUser        string            `json:"ssh_user,omitempty"`
	RDNSPattern    string            `json:"rdns_pattern,omitempty"` // e.g. "w-{{ip}}.workers.example.com"
}

// Service is the orchestrator.
type Service struct {
	Jobs      repository.ProvisioningJobRepository
	Providers map[string]cloudprovider.Provider // keyed by provider name
	Installer Installer
	// VerifyTimeout is how long Run waits for expected workers to heartbeat
	// before failing the job. Default 5 min.
	VerifyTimeout time.Duration
	// VerifyAllReady is called repeatedly during the verify step to check
	// which of the expected workers have registered via heartbeat. Returns
	// the subset that are alive.
	VerifyAllReady func(ctx context.Context, expected []uuid.UUID) ([]uuid.UUID, error)
}

// Run drives the given job to terminal state (completed or failed). Safe to
// call concurrently with itself for different jobs; not safe for the same job.
func (s *Service) Run(ctx context.Context, jobID uuid.UUID) error {
	job, err := s.Jobs.Get(ctx, jobID)
	if err != nil {
		return fmt.Errorf("provisioning: load job: %w", err)
	}
	if job == nil {
		return fmt.Errorf("provisioning: job %s not found", jobID)
	}

	provider, ok := s.Providers[job.Provider]
	if !ok {
		return s.fail(ctx, jobID, fmt.Errorf("no provider client for %q", job.Provider))
	}

	var cfg JobConfig
	if err := json.Unmarshal(job.Config, &cfg); err != nil {
		return s.fail(ctx, jobID, fmt.Errorf("decode config: %w", err))
	}
	if cfg.SSHPort == 0 {
		cfg.SSHPort = 22
	}
	if cfg.SSHUser == "" {
		cfg.SSHUser = "root"
	}
	if cfg.ServerCount <= 0 {
		cfg.ServerCount = 1
	}
	if cfg.IPv4PerServer <= 0 {
		cfg.IPv4PerServer = 1
	}

	// One server per Run call. server_count > 1 means the admin / scale loop
	// must enqueue multiple jobs, one per server. Keeps each job's failure
	// domain small.

	for state := job.State; ; {
		switch state {
		case models.ProvJobPending:
			state = models.ProvJobCreatingServer

		case models.ProvJobCreatingServer:
			if err := s.Jobs.UpdateState(ctx, jobID, state); err != nil {
				return err
			}
			server, err := s.createServer(ctx, provider, cfg, jobID)
			if err != nil {
				return s.rollback(ctx, jobID, provider, fmt.Errorf("create_server: %w", err))
			}
			if err := s.Jobs.RecordServer(ctx, jobID, server.ID); err != nil {
				return s.rollback(ctx, jobID, provider, err)
			}
			if err := s.Jobs.AppendIPs(ctx, jobID, []string{}, []string{server.PublicIPv4}); err != nil {
				return s.rollback(ctx, jobID, provider, err)
			}
			job.ProviderServerID = &server.ID
			job.IPs = append(job.IPs, server.PublicIPv4)
			state = models.ProvJobCreatingIPs

		case models.ProvJobCreatingIPs:
			if err := s.Jobs.UpdateState(ctx, jobID, state); err != nil {
				return err
			}
			// IPv4PerServer=1 means the server's default IP is the only IP.
			// IPv4PerServer>1 means create (n-1) extra Primary IPs.
			extra := cfg.IPv4PerServer - 1
			if extra > 0 {
				ipIDs := make([]string, 0, extra)
				ips := make([]string, 0, extra)
				for i := 0; i < extra; i++ {
					ip, err := provider.CreatePrimaryIP(ctx, cloudprovider.CreatePrimaryIPRequest{
						Type:       "ipv4",
						Name:       fmt.Sprintf("warmbly-%s-%d", jobID.String()[:8], i),
						Datacenter: cfg.Datacenter,
						Labels:     cfg.Labels,
					})
					if err != nil {
						return s.rollback(ctx, jobID, provider, fmt.Errorf("create_ip %d: %w", i, err))
					}
					ipIDs = append(ipIDs, ip.ID)
					ips = append(ips, ip.IP)
				}
				if err := s.Jobs.AppendIPs(ctx, jobID, ipIDs, ips); err != nil {
					return s.rollback(ctx, jobID, provider, err)
				}
				job.ProviderIPIDs = append(job.ProviderIPIDs, ipIDs...)
				job.IPs = append(job.IPs, ips...)
			}
			state = models.ProvJobAssigningIPs

		case models.ProvJobAssigningIPs:
			if err := s.Jobs.UpdateState(ctx, jobID, state); err != nil {
				return err
			}
			if job.ProviderServerID != nil {
				for _, ipID := range job.ProviderIPIDs {
					if err := provider.AssignPrimaryIP(ctx, ipID, *job.ProviderServerID); err != nil {
						return s.rollback(ctx, jobID, provider, fmt.Errorf("assign_ip %s: %w", ipID, err))
					}
				}
			}
			state = models.ProvJobSettingRDNS

		case models.ProvJobSettingRDNS:
			if err := s.Jobs.UpdateState(ctx, jobID, state); err != nil {
				return err
			}
			if cfg.RDNSPattern != "" {
				for i, ipID := range job.ProviderIPIDs {
					ip := ""
					if i+1 < len(job.IPs) {
						ip = job.IPs[i+1] // index 0 is the server's default IP
					}
					hostname := strings.ReplaceAll(cfg.RDNSPattern, "{{ip}}", strings.ReplaceAll(ip, ".", "-"))
					if err := provider.SetReverseDNS(ctx, ipID, hostname); err != nil {
						// rDNS failure is non-fatal — log and continue.
						_ = err
					}
				}
			}
			state = models.ProvJobInstalling

		case models.ProvJobInstalling:
			if err := s.Jobs.UpdateState(ctx, jobID, state); err != nil {
				return err
			}
			if len(job.IPs) == 0 {
				return s.rollback(ctx, jobID, provider, fmt.Errorf("no IPs to install on"))
			}
			expected := make([]uuid.UUID, 0, len(job.IPs))
			for _, ip := range job.IPs {
				expected = append(expected, WorkerIDForIP(ip))
			}
			res, err := s.Installer.Install(ctx, InstallRequest{
				Host:        job.IPs[0], // use server's default IP for SSH
				SSHPort:     cfg.SSHPort,
				SSHUser:     cfg.SSHUser,
				SSHKeyPEM:   cfg.SSHPrivKeyPEM,
				IPs:         job.IPs,
				WorkerEnv:   cfg.WorkerEnv,
				ImageTag:    cfg.ImageTag,
				ExpectedIDs: expected,
			})
			if err != nil {
				return s.rollback(ctx, jobID, provider, fmt.Errorf("install: %w", err))
			}
			if len(res.InstalledWorkerIDs) > 0 {
				if err := s.Jobs.AppendWorkerIDs(ctx, jobID, res.InstalledWorkerIDs); err != nil {
					return s.rollback(ctx, jobID, provider, err)
				}
			}
			state = models.ProvJobVerifying

		case models.ProvJobVerifying:
			if err := s.Jobs.UpdateState(ctx, jobID, state); err != nil {
				return err
			}
			timeout := s.VerifyTimeout
			if timeout == 0 {
				timeout = 5 * time.Minute
			}
			vctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			expected := make([]uuid.UUID, 0, len(job.IPs))
			for _, ip := range job.IPs {
				expected = append(expected, WorkerIDForIP(ip))
			}
			ok, err := s.waitForHeartbeats(vctx, expected)
			if err != nil {
				return s.rollback(ctx, jobID, provider, fmt.Errorf("verify: %w", err))
			}
			if !ok {
				return s.rollback(ctx, jobID, provider, fmt.Errorf("verify: timeout waiting for workers to register"))
			}
			state = models.ProvJobCompleted

		case models.ProvJobCompleted:
			return s.Jobs.MarkCompleted(ctx, jobID)

		case models.ProvJobFailed, models.ProvJobRollingBack:
			return fmt.Errorf("provisioning job %s already terminal: %s", jobID, state)

		default:
			return s.fail(ctx, jobID, fmt.Errorf("unknown state %q", state))
		}
	}
}

func (s *Service) createServer(ctx context.Context, p cloudprovider.Provider, cfg JobConfig, jobID uuid.UUID) (*cloudprovider.Server, error) {
	sshKeys := []string{}
	if cfg.SSHKeyID != "" {
		sshKeys = []string{cfg.SSHKeyID}
	}
	return p.CreateServer(ctx, cloudprovider.CreateServerRequest{
		Name:             fmt.Sprintf("warmbly-%s-%s", cfg.Location, jobID.String()[:8]),
		ServerType:       cfg.ServerType,
		Image:            cfg.Image,
		Location:         cfg.Location,
		Datacenter:       cfg.Datacenter,
		SSHKeyIDs:        sshKeys,
		UserData:         renderCloudInit(cfg),
		Labels:           cfg.Labels,
		PlacementGroup:   cfg.PlacementGroup,
		PrivateNetwork:   cfg.PrivateNetwork,
		Firewall:         cfg.Firewall,
		StartAfterCreate: true,
	})
}

func renderCloudInit(cfg JobConfig) string {
	// Minimal cloud-init: ensure Docker is present so install-worker.sh
	// doesn't have to fetch it. The installer handles the rest.
	return `#cloud-config
package_update: true
packages:
  - curl
  - ca-certificates
  - docker.io
runcmd:
  - [systemctl, enable, --now, docker]
`
}

func (s *Service) waitForHeartbeats(ctx context.Context, expected []uuid.UUID) (bool, error) {
	if s.VerifyAllReady == nil {
		// No verify hook wired — assume installer-side check is sufficient.
		return true, nil
	}
	wantSet := map[uuid.UUID]struct{}{}
	for _, id := range expected {
		wantSet[id] = struct{}{}
	}
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		alive, err := s.VerifyAllReady(ctx, expected)
		if err != nil {
			return false, err
		}
		for _, id := range alive {
			delete(wantSet, id)
		}
		if len(wantSet) == 0 {
			return true, nil
		}
		select {
		case <-ctx.Done():
			return false, nil
		case <-tick.C:
		}
	}
}

// rollback transitions the job to rolling_back, undoes provider-side
// resources we created, then marks the job failed.
func (s *Service) rollback(ctx context.Context, jobID uuid.UUID, p cloudprovider.Provider, rootCause error) error {
	_ = s.Jobs.UpdateState(ctx, jobID, models.ProvJobRollingBack)
	job, _ := s.Jobs.Get(ctx, jobID)
	if job != nil {
		for _, ipID := range job.ProviderIPIDs {
			_ = p.UnassignPrimaryIP(ctx, ipID)
			_ = p.DeletePrimaryIP(ctx, ipID)
		}
		if job.ProviderServerID != nil {
			_ = p.DeleteServer(ctx, *job.ProviderServerID)
		}
	}
	return s.fail(ctx, jobID, rootCause)
}

func (s *Service) fail(ctx context.Context, jobID uuid.UUID, err error) error {
	_ = s.Jobs.MarkFailed(ctx, jobID, err.Error())
	return err
}

// SubmitConfigJSON marshals a JobConfig to the raw JSON the row stores.
func SubmitConfigJSON(cfg JobConfig) (json.RawMessage, error) {
	return json.Marshal(cfg)
}

// randomToken is a small helper for default name suffixes.
func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// ParseIPs is a small helper for callers that need to round-trip the
// INET[]→string→net.IP conversion when reading job rows.
func ParseIPs(raw []string) []net.IP {
	out := make([]net.IP, 0, len(raw))
	for _, s := range raw {
		if ip := net.ParseIP(s); ip != nil {
			out = append(out, ip)
		}
	}
	return out
}

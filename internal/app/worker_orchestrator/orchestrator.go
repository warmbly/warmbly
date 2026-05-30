// Package worker_orchestrator drives the lifecycle of remote worker VPSes
// over SSH.
//
// The control plane stores per-worker connection info and an ed25519 private
// key (encrypted via the platform cipher service) in Postgres. Operations
// (Install, Restart, Update, Uninstall, Status, TailLogs, RotateKeys,
// TestConnection) open a fresh SSH session, run the relevant command, and
// update the workers row.
//
// The install payload is the project's scripts/install-worker.sh — uploaded
// to /tmp on the target and executed with the worker's env config baked in.
//
// Identity model: the worker's UUID is the workers.id row. The VPS's
// hostname is set to that UUID via `docker run --hostname` inside the
// installer, so cmd/worker reads its identity from os.Hostname().

package worker_orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"golang.org/x/crypto/ssh"
)

// platformCipherUser is the UUID under which the cipher service stores the
// DEK that encrypts platform-level secrets (worker SSH keys). The zero UUID
// is not used by any real user, so it cleanly partitions platform secrets
// from user secrets in DynamoDB.
var platformCipherUser = uuid.Nil

// WorkerEnvConfig is the set of env vars the worker container needs at run
// time. The orchestrator writes these into /etc/warmbly/worker.env on the
// target. Provide credentials that scope only what the worker can reach.
type WorkerEnvConfig struct {
	AppEnv      string // "dev" or "prod"
	WorkerImage string // e.g. ghcr.io/warmbly/worker:latest

	KafkaBootstrap    string
	KafkaSASLUsername string
	KafkaSASLPassword string

	SchemaRegistryURL    string
	SchemaRegistryKey    string
	SchemaRegistrySecret string

	RedisURL string

	AWSRegion          string
	AWSAccessKeyID     string
	AWSSecretAccessKey string

	EncryptedKeysBackendURL  string
	EncryptedKeysWorkerToken string

	EventBusProvider string
	NATSURL          string
	CodecProvider    string
}

type Orchestrator struct {
	repo            repository.WorkerRepository
	credentialsRepo repository.CredentialsRepository
	cipher          cipher.CipherService
	defaultEnv      WorkerEnvConfig // fallback when worker has no profile assigned
	installerPath   string          // absolute path to scripts/install-worker.sh
}

func New(
	repo repository.WorkerRepository,
	credentialsRepo repository.CredentialsRepository,
	cipherSvc cipher.CipherService,
	defaultEnv WorkerEnvConfig,
	installerPath string,
) *Orchestrator {
	return &Orchestrator{
		repo:            repo,
		credentialsRepo: credentialsRepo,
		cipher:          cipherSvc,
		defaultEnv:      defaultEnv,
		installerPath:   installerPath,
	}
}

// EncryptSecret is a small public helper for handlers writing new platform
// secrets (AWS keys, Kafka passwords, etc.) into the credentials tables.
// Uses the same platform-identity DEK as worker SSH keys.
func (o *Orchestrator) EncryptSecret(ctx context.Context, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	return o.encryptPrivateKey(ctx, plaintext)
}

// public ops

// TestConnection opens an SSH session and runs `true`. Useful for the
// dashboard "test" button after the admin pastes the pubkey into the VPS.
func (o *Orchestrator) TestConnection(ctx context.Context, workerID uuid.UUID) error {
	return o.withSession(ctx, workerID, func(d *dialer) error {
		_, err := d.Run(ctx, "true")
		return err
	})
}

// Install uploads the installer + env file, then runs the installer to bring
// the worker container up.
func (o *Orchestrator) Install(ctx context.Context, workerID uuid.UUID) error {
	_ = o.repo.UpdateInstallState(ctx, workerID, models.WorkerInstallStateProvisioning, "")
	err := o.withSession(ctx, workerID, func(d *dialer) error {
		envContent, image, err := o.renderEnvFile(ctx, workerID)
		if err != nil {
			return err
		}
		if err := d.Upload(ctx, "/tmp/warmbly-worker.env", envContent, "0600"); err != nil {
			return fmt.Errorf("upload env: %w", err)
		}

		installerBytes, err := os.ReadFile(o.installerPath)
		if err != nil {
			return fmt.Errorf("read installer: %w", err)
		}
		if err := d.Upload(ctx, "/tmp/install-worker.sh", string(installerBytes), "0755"); err != nil {
			return fmt.Errorf("upload installer: %w", err)
		}

		cmd := fmt.Sprintf(
			"sudo /tmp/install-worker.sh --non-interactive --worker-id %s --image %s --env-file /tmp/warmbly-worker.env",
			workerID.String(), shellQuote(image),
		)
		out, err := d.Run(ctx, cmd)
		if err != nil {
			return fmt.Errorf("installer failed: %w\n%s", err, tail(out, 40))
		}
		return nil
	})
	if err != nil {
		_ = o.repo.UpdateInstallState(ctx, workerID, models.WorkerInstallStateError, err.Error())
		return err
	}
	_ = o.repo.MarkConfigApplied(ctx, workerID, time.Now())
	return o.repo.UpdateInstallState(ctx, workerID, models.WorkerInstallStateInstalled, "")
}

func (o *Orchestrator) InstallerScript() ([]byte, error) {
	return os.ReadFile(o.installerPath)
}

// RenderEnrollmentEnv returns a complete dotenv payload for the one-command
// enrollment flow. The token exchange authenticates the caller; this method
// only renders the config the installer writes to disk.
func (o *Orchestrator) RenderEnrollmentEnv(ctx context.Context, workerID uuid.UUID) (string, string, error) {
	envContent, image, err := o.renderEnvFile(ctx, workerID)
	if err != nil {
		return "", "", err
	}
	var b strings.Builder
	b.WriteString("# Warmbly worker enrollment config\n")
	b.WriteString("WORKER_ID=")
	b.WriteString(workerID.String())
	b.WriteString("\n")
	b.WriteString("WARMBLY_WORKER_IMAGE=")
	b.WriteString(image)
	b.WriteString("\n")
	b.WriteString(envContent)
	return b.String(), image, nil
}

// ApplyConfig re-writes /etc/warmbly/worker.env from the worker's current
// profile + AWS creds and restarts the service. Cheaper than Install — does
// not touch Docker or the installer script. Use after a credential change.
func (o *Orchestrator) ApplyConfig(ctx context.Context, workerID uuid.UUID) error {
	err := o.withSession(ctx, workerID, func(d *dialer) error {
		envContent, _, err := o.renderEnvFile(ctx, workerID)
		if err != nil {
			return err
		}
		// install-worker.sh writes to /etc/warmbly/worker.env at install time;
		// we replace that file directly here so it's atomic relative to a
		// restart.
		if err := d.Upload(ctx, "/etc/warmbly/worker.env", envContent, "0600"); err != nil {
			return fmt.Errorf("upload env: %w", err)
		}
		out, err := d.Run(ctx, "sudo systemctl restart warmbly-worker.service")
		if err != nil {
			return fmt.Errorf("restart: %w: %s", err, tail(out, 20))
		}
		return nil
	})
	if err != nil {
		return err
	}
	return o.repo.MarkConfigApplied(ctx, workerID, time.Now())
}

func (o *Orchestrator) Restart(ctx context.Context, workerID uuid.UUID) error {
	return o.withSession(ctx, workerID, func(d *dialer) error {
		out, err := d.Run(ctx, "sudo systemctl restart warmbly-worker.service")
		if err != nil {
			return fmt.Errorf("restart: %w: %s", err, tail(out, 20))
		}
		return nil
	})
}

// Update pulls a new image and restarts the worker. Resolves the target image
// from the worker's profile (if one is assigned); otherwise from the
// orchestrator's default. The installer rewrites the systemd unit with the
// new image so subsequent restarts pick it up.
func (o *Orchestrator) Update(ctx context.Context, workerID uuid.UUID) error {
	_, image, err := o.renderEnvFile(ctx, workerID)
	if err != nil {
		return err
	}
	return o.UpdateToImage(ctx, workerID, image)
}

// UpdateToImage rolls a worker to a specific image tag. Used by the release
// service when an auto-update channel resolves a new tag.
func (o *Orchestrator) UpdateToImage(ctx context.Context, workerID uuid.UUID, image string) error {
	if image == "" {
		return errors.New("no image specified")
	}
	err := o.withSession(ctx, workerID, func(d *dialer) error {
		// Make sure the installer is present — it lands in /tmp during Install
		// but a backend redeploy can blow it away if /tmp is cleaned.
		installerBytes, ierr := os.ReadFile(o.installerPath)
		if ierr != nil {
			return fmt.Errorf("read installer: %w", ierr)
		}
		if err := d.Upload(ctx, "/tmp/install-worker.sh", string(installerBytes), "0755"); err != nil {
			return fmt.Errorf("upload installer: %w", err)
		}
		cmd := fmt.Sprintf("sudo /tmp/install-worker.sh --update --image %s", shellQuote(image))
		out, err := d.Run(ctx, cmd)
		if err != nil {
			return fmt.Errorf("update: %w: %s", err, tail(out, 30))
		}
		return nil
	})
	if err != nil {
		return err
	}
	tag := imageTag(image)
	_ = o.repo.MarkImageVersion(ctx, workerID, tag)
	return nil
}

// imageTag extracts the human-readable tag part of an image reference. Used
// for the workers.image_version column.
//
//	ghcr.io/foo/worker:v1.2.3 → "v1.2.3"
//	ghcr.io/foo/worker        → "latest"
//	ghcr.io/foo/worker@sha256:abc → "abc[:12]"
func imageTag(image string) string {
	if i := strings.LastIndex(image, "@sha256:"); i >= 0 {
		d := image[i+len("@sha256:"):]
		if len(d) > 12 {
			d = d[:12]
		}
		return "sha256:" + d
	}
	if i := strings.LastIndex(image, ":"); i >= 0 && !strings.Contains(image[i:], "/") {
		return image[i+1:]
	}
	return "latest"
}

func (o *Orchestrator) Uninstall(ctx context.Context, workerID uuid.UUID) error {
	_ = o.repo.UpdateInstallState(ctx, workerID, models.WorkerInstallStateUninstalling, "")
	err := o.withSession(ctx, workerID, func(d *dialer) error {
		out, err := d.Run(ctx, "sudo /tmp/install-worker.sh --uninstall || true")
		_ = out
		return err
	})
	if err != nil {
		_ = o.repo.UpdateInstallState(ctx, workerID, models.WorkerInstallStateError, err.Error())
		return err
	}
	return o.repo.UpdateInstallState(ctx, workerID, models.WorkerInstallStateUninstalled, "")
}

// Status snapshot from the target. Cheap, on-demand.
type StatusResult struct {
	ServiceActive  bool   `json:"service_active"`
	ContainerUp    bool   `json:"container_up"`
	ContainerImage string `json:"container_image"`
	Uptime         string `json:"uptime"`
	Raw            string `json:"raw"`
}

func (o *Orchestrator) Status(ctx context.Context, workerID uuid.UUID) (*StatusResult, error) {
	var result *StatusResult
	err := o.withSession(ctx, workerID, func(d *dialer) error {
		out, _ := d.Run(ctx,
			"systemctl is-active warmbly-worker.service 2>/dev/null; "+
				"echo '---'; "+
				"docker inspect --format='{{.State.Status}} {{.Config.Image}} {{.State.StartedAt}}' warmbly-worker 2>/dev/null; "+
				"echo '---'; "+
				"uptime",
		)
		result = parseStatus(out)
		return nil
	})
	return result, err
}

// SystemUpdate runs OS package upgrades on the worker VPS. Detects the
// distro and dispatches to apt / dnf / yum / pacman / apk. Returns the
// combined output (often long) so the dashboard can render it verbatim.
//
// Also checks whether a reboot is required afterward so the admin can be
// prompted; reboots are never automatic.
type SystemUpdateResult struct {
	Output         string `json:"output"`
	RebootRequired bool   `json:"reboot_required"`
}

func (o *Orchestrator) SystemUpdate(ctx context.Context, workerID uuid.UUID) (*SystemUpdateResult, error) {
	var result *SystemUpdateResult
	err := o.withSession(ctx, workerID, func(d *dialer) error {
		// One big shell script: detect package manager, run upgrade, then
		// check for reboot-required markers.
		script := `set -e
detect() {
  if command -v apt-get >/dev/null 2>&1; then echo apt
  elif command -v dnf >/dev/null 2>&1; then echo dnf
  elif command -v yum >/dev/null 2>&1; then echo yum
  elif command -v pacman >/dev/null 2>&1; then echo pacman
  elif command -v apk >/dev/null 2>&1; then echo apk
  else echo unknown; fi
}
PM="$(detect)"
echo "== package manager: $PM"
case "$PM" in
  apt)
    sudo DEBIAN_FRONTEND=noninteractive apt-get update -y
    sudo DEBIAN_FRONTEND=noninteractive apt-get upgrade -y -o Dpkg::Options::="--force-confold"
    sudo DEBIAN_FRONTEND=noninteractive apt-get autoremove -y
    ;;
  dnf|yum)
    sudo "$PM" upgrade -y
    sudo "$PM" autoremove -y || true
    ;;
  pacman)
    sudo pacman -Syu --noconfirm
    ;;
  apk)
    sudo apk update
    sudo apk upgrade
    ;;
  *)
    echo "unsupported package manager"; exit 1
    ;;
esac
echo "== reboot check"
REBOOT=0
[ -f /var/run/reboot-required ] && REBOOT=1
command -v needs-restarting >/dev/null 2>&1 && needs-restarting -r >/dev/null 2>&1 || true
# kernel mismatch heuristic for everyone else
RUN_KERNEL="$(uname -r)"
NEW_KERNEL="$(ls -1 /lib/modules 2>/dev/null | sort -V | tail -1 || echo "$RUN_KERNEL")"
[ "$RUN_KERNEL" != "$NEW_KERNEL" ] && REBOOT=1
echo "==REBOOT_REQUIRED:$REBOOT"
`
		out, err := d.Run(ctx, script)
		result = &SystemUpdateResult{
			Output:         out,
			RebootRequired: strings.Contains(out, "==REBOOT_REQUIRED:1"),
		}
		return err
	})
	return result, err
}

// RebootWorker requests an OS reboot. Worker comes back online once the VPS
// restarts and the systemd unit auto-starts.
func (o *Orchestrator) RebootWorker(ctx context.Context, workerID uuid.UUID) error {
	return o.withSession(ctx, workerID, func(d *dialer) error {
		// nohup + delay so the SSH session can close cleanly before reboot
		_, _ = d.Run(ctx, "sudo sh -c 'nohup shutdown -r +1 >/dev/null 2>&1 &'")
		return nil
	})
}

func (o *Orchestrator) TailLogs(ctx context.Context, workerID uuid.UUID, lines int) (string, error) {
	if lines <= 0 || lines > 1000 {
		lines = 200
	}
	var logs string
	err := o.withSession(ctx, workerID, func(d *dialer) error {
		out, err := d.Run(ctx, fmt.Sprintf("sudo journalctl -u warmbly-worker -n %d --no-pager", lines))
		logs = out
		return err
	})
	return logs, err
}

// RotateKeys generates a new keypair, installs it on the target via the old
// connection, then persists the new keypair. The old key remains in
// authorized_keys until the admin removes it manually — we never auto-delete
// keys we didn't put there.
func (o *Orchestrator) RotateKeys(ctx context.Context, workerID uuid.UUID) (newPublicKey string, err error) {
	newPub, newPriv, err := GenerateKeypair()
	if err != nil {
		return "", err
	}
	encPriv, err := o.encryptPrivateKey(ctx, newPriv)
	if err != nil {
		return "", err
	}

	err = o.withSession(ctx, workerID, func(d *dialer) error {
		// Append the new pubkey to ~/.ssh/authorized_keys idempotently.
		shellCmd := fmt.Sprintf(
			"mkdir -p ~/.ssh && chmod 700 ~/.ssh && "+
				"touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys && "+
				"grep -qxF %s ~/.ssh/authorized_keys || echo %s >> ~/.ssh/authorized_keys",
			shellQuote(strings.TrimSpace(newPub)), shellQuote(strings.TrimSpace(newPub)),
		)
		_, err := d.Run(ctx, shellCmd)
		return err
	})
	if err != nil {
		return "", err
	}

	if err := o.repo.RotateSSHKey(ctx, workerID, newPub, encPriv); err != nil {
		return "", err
	}
	return newPub, nil
}

// helpers

// withSession decrypts the worker's private key, dials, runs `fn`, pins the
// host fingerprint on first connect.
func (o *Orchestrator) withSession(ctx context.Context, workerID uuid.UUID, fn func(*dialer) error) error {
	creds, err := o.repo.GetWorkerSSHCredentials(ctx, workerID)
	if err != nil {
		return err
	}
	if creds == nil {
		return errors.New("worker not found")
	}
	if creds.SSHHost == "" {
		return errors.New("worker has no SSH host configured")
	}

	signer, err := o.loadSigner(ctx, creds.SSHPrivateKeyEncrypted)
	if err != nil {
		return fmt.Errorf("load private key: %w", err)
	}

	d, err := dial(ctx, dialOptions{
		host:                creds.SSHHost,
		port:                creds.SSHPort,
		user:                creds.SSHUser,
		signer:              signer,
		expectedFingerprint: creds.SSHHostFingerprint,
		timeout:             20 * time.Second,
	})
	if err != nil {
		return err
	}
	defer d.Close()

	if creds.SSHHostFingerprint == "" && d.observedFingerprint != "" {
		_ = o.repo.UpdateHostFingerprint(ctx, workerID, d.observedFingerprint)
	}

	return fn(d)
}

func (o *Orchestrator) loadSigner(ctx context.Context, encryptedPrivateKey string) (ssh.Signer, error) {
	if encryptedPrivateKey == "" {
		return nil, errors.New("no private key stored")
	}
	c, err := o.cipher.Cipher(ctx, platformCipherUser)
	if err != nil {
		return nil, fmt.Errorf("cipher: %w", err)
	}
	pem, err := c.Decrypt(ctx, encryptedPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	signer, err := ParsePrivateKey([]byte(pem))
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	return signer, nil
}

func (o *Orchestrator) encryptPrivateKey(ctx context.Context, pem string) (string, error) {
	c, err := o.cipher.Cipher(ctx, platformCipherUser)
	if err != nil {
		return "", err
	}
	return c.Encrypt(ctx, pem)
}

// EncryptPrivateKey is the entry point used by the admin service when first
// creating a worker (before any orchestrator op has run).
func (o *Orchestrator) EncryptPrivateKey(ctx context.Context, pem string) (string, error) {
	return o.encryptPrivateKey(ctx, pem)
}

// renderEnvFile produces the contents of /etc/warmbly/worker.env for this
// worker. Resolution order:
//  1. If the worker has a profile assigned, fetch the profile + linked AWS
//     credentials, decrypt each secret with the cipher service, and use
//     those values.
//  2. Otherwise fall back to the orchestrator's defaultEnv (the backend's
//     own process env). This lets dev/sim work without setting up profiles.
//
// Also returns the image to run, since profiles can pin their own.
func (o *Orchestrator) renderEnvFile(ctx context.Context, workerID uuid.UUID) (envContent string, image string, err error) {
	w, err := o.repo.GetWorkerDetail(ctx, workerID)
	if err != nil {
		return "", "", err
	}
	if w == nil {
		return "", "", errors.New("worker not found")
	}

	env := o.defaultEnv
	image = o.defaultEnv.WorkerImage

	if w.ProfileID != nil {
		pe, err := o.credentialsRepo.GetProfileEncrypted(ctx, *w.ProfileID)
		if err != nil {
			return "", "", fmt.Errorf("load profile: %w", err)
		}
		if pe == nil {
			return "", "", fmt.Errorf("profile %s not found", w.ProfileID)
		}
		c, err := o.cipher.Cipher(ctx, platformCipherUser)
		if err != nil {
			return "", "", fmt.Errorf("cipher: %w", err)
		}

		decrypt := func(s string) (string, error) {
			if s == "" {
				return "", nil
			}
			return c.Decrypt(ctx, s)
		}

		env.AppEnv = pe.Profile.AppEnv
		env.WorkerImage = pe.Profile.WorkerImage
		image = pe.Profile.WorkerImage

		env.KafkaBootstrap = pe.Profile.KafkaBootstrapServers
		env.KafkaSASLUsername = pe.Profile.KafkaSASLUsername
		if env.KafkaSASLPassword, err = decrypt(pe.KafkaSASLPasswordEncrypted); err != nil {
			return "", "", fmt.Errorf("decrypt kafka pw: %w", err)
		}

		env.SchemaRegistryURL = pe.Profile.SchemaRegistryURL
		env.SchemaRegistryKey = pe.Profile.SchemaRegistryKey
		if env.SchemaRegistrySecret, err = decrypt(pe.SchemaRegistrySecretEncrypted); err != nil {
			return "", "", fmt.Errorf("decrypt schema secret: %w", err)
		}

		if env.RedisURL, err = decrypt(pe.RedisURLEncrypted); err != nil {
			return "", "", fmt.Errorf("decrypt redis url: %w", err)
		}

		// AWS credentials live in their own row.
		if pe.Profile.AWSCredentialID != nil {
			aws, err := o.credentialsRepo.GetAWSCreds(ctx, *pe.Profile.AWSCredentialID)
			if err != nil {
				return "", "", fmt.Errorf("load aws creds: %w", err)
			}
			if aws != nil {
				env.AWSRegion = aws.Region
				env.AWSAccessKeyID = aws.AccessKeyID
				if env.AWSSecretAccessKey, err = decrypt(aws.SecretAccessKeyEncrypted); err != nil {
					return "", "", fmt.Errorf("decrypt aws secret: %w", err)
				}
			}
		}
	}

	if image == "" {
		image = "ghcr.io/warmbly/worker:latest"
	}

	var b strings.Builder
	write := func(k, v string) {
		if v == "" {
			return
		}
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(v)
		b.WriteString("\n")
	}
	write("APP_ENV", env.AppEnv)
	write("AWS_CONFIG_ENABLED", "false")
	write("AWS_REGION", env.AWSRegion)
	write("AWS_ACCESS_KEY_ID", env.AWSAccessKeyID)
	write("AWS_SECRET_ACCESS_KEY", env.AWSSecretAccessKey)
	write("ENCRYPTED_KEYS_PROVIDER", "http")
	write("ENCRYPTED_KEYS_BACKEND_URL", env.EncryptedKeysBackendURL)
	write("ENCRYPTED_KEYS_WORKER_TOKEN", env.EncryptedKeysWorkerToken)
	write("KAFKA_BOOTSTRAP_SERVERS", env.KafkaBootstrap)
	write("KAFKA_SASL_USERNAME", env.KafkaSASLUsername)
	write("KAFKA_SASL_PASSWORD", env.KafkaSASLPassword)
	write("SCHEMA_REGISTRY_URL", env.SchemaRegistryURL)
	write("SCHEMA_REGISTRY_KEY", env.SchemaRegistryKey)
	write("SCHEMA_REGISTRY_SECRET", env.SchemaRegistrySecret)
	write("REDIS", env.RedisURL)
	write("EVENTBUS_PROVIDER", env.EventBusProvider)
	write("NATS_URL", env.NATSURL)
	write("CODEC_PROVIDER", env.CodecProvider)
	return b.String(), image, nil
}

func parseStatus(raw string) *StatusResult {
	r := &StatusResult{Raw: raw}
	parts := strings.Split(raw, "---")
	if len(parts) >= 1 {
		r.ServiceActive = strings.TrimSpace(parts[0]) == "active"
	}
	if len(parts) >= 2 {
		fields := strings.Fields(parts[1])
		if len(fields) >= 1 {
			r.ContainerUp = fields[0] == "running"
		}
		if len(fields) >= 2 {
			r.ContainerImage = fields[1]
		}
	}
	if len(parts) >= 3 {
		r.Uptime = strings.TrimSpace(parts[2])
	}
	return r
}

func tail(s string, lines int) string {
	parts := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(parts) <= lines {
		return s
	}
	return strings.Join(parts[len(parts)-lines:], "\n")
}

package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/models"
)

// CreateWorkerInput is the payload for inserting a new SSH-managed worker.
// SSHPrivateKeyEncrypted must already be ciphertext from the cipher service.
type CreateWorkerInput struct {
	ID                     uuid.UUID
	Name                   string
	Notes                  string
	IPAddr                 string
	WorkerType             models.WorkerType
	FreeTier               bool
	SSHHost                string
	SSHPort                int
	SSHUser                string
	SSHPublicKey           string
	SSHPrivateKeyEncrypted string
	EnrollmentTokenHash    string
	EnrollmentTokenExpires *time.Time
}

func (r *workerRepository) CreateWorker(ctx context.Context, in CreateWorkerInput) error {
	id := in.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO workers (
			id, name, notes, ip_addr,
			worker_type, free_tier, active,
			ssh_host, ssh_port, ssh_user,
			ssh_public_key, ssh_private_key_encrypted,
			install_state,
			enrollment_token_hash, enrollment_token_expires_at
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, false,
			$7, $8, $9,
			$10, $11,
			'pending',
			NULLIF($12, ''), $13
		)
	`,
		id, in.Name, in.Notes, in.IPAddr,
		in.WorkerType, in.FreeTier,
		in.SSHHost, in.SSHPort, in.SSHUser,
		in.SSHPublicKey, in.SSHPrivateKeyEncrypted,
		in.EnrollmentTokenHash, in.EnrollmentTokenExpires,
	)
	return err
}

const workerDetailColumns = `
	id, name, COALESCE(notes,''), ip_addr,
	active, free_tier, worker_type, account_count,
	COALESCE(ssh_host,''), ssh_port, ssh_user,
	COALESCE(ssh_public_key,''), COALESCE(ssh_host_fingerprint,''),
	install_state, last_seen_at, COALESCE(last_error,''),
	profile_id, config_applied_at, COALESCE(image_version,''),
	risk_pool,
	created_at, updated_at
`

func scanWorkerDetail(row pgx.Row) (*models.Worker, error) {
	var w models.Worker
	err := row.Scan(
		&w.ID, &w.Name, &w.Notes, &w.IPAddr,
		&w.Active, &w.FreeTier, &w.WorkerType, &w.AccountCount,
		&w.SSHHost, &w.SSHPort, &w.SSHUser,
		&w.SSHPublicKey, &w.SSHHostFingerprint,
		&w.InstallState, &w.LastSeenAt, &w.LastError,
		&w.ProfileID, &w.ConfigAppliedAt, &w.ImageVersion,
		&w.RiskPool,
		&w.CreatedAt, &w.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *workerRepository) GetWorkerDetail(ctx context.Context, id uuid.UUID) (*models.Worker, error) {
	return scanWorkerDetail(r.db.QueryRow(ctx, `
		SELECT `+workerDetailColumns+`
		FROM workers
		WHERE id = $1
	`, id))
}

func (r *workerRepository) ListWorkersDetail(ctx context.Context) ([]models.Worker, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+workerDetailColumns+`
		FROM workers
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.Worker, 0)
	for rows.Next() {
		w, err := scanWorkerDetail(rows)
		if err != nil {
			return nil, err
		}
		if w != nil {
			out = append(out, *w)
		}
	}
	return out, rows.Err()
}

// GetWorkerSSHCredentials returns the encrypted private key with the rest of
// the connection info. Orchestrator-only — never expose to API responses.
func (r *workerRepository) GetWorkerSSHCredentials(ctx context.Context, id uuid.UUID) (*models.WorkerSSHCredentials, error) {
	var c models.WorkerSSHCredentials
	err := r.db.QueryRow(ctx, `
		SELECT id,
		       COALESCE(ssh_host, ''), ssh_port, ssh_user,
		       COALESCE(ssh_public_key, ''),
		       COALESCE(ssh_private_key_encrypted, ''),
		       COALESCE(ssh_host_fingerprint, '')
		FROM workers
		WHERE id = $1
	`, id).Scan(
		&c.WorkerID,
		&c.SSHHost, &c.SSHPort, &c.SSHUser,
		&c.SSHPublicKey, &c.SSHPrivateKeyEncrypted,
		&c.SSHHostFingerprint,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *workerRepository) UpdateInstallState(ctx context.Context, id uuid.UUID, state models.WorkerInstallState, lastError string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE workers
		SET install_state = $2,
		    last_error = NULLIF($3, ''),
		    active = CASE WHEN $2 = 'installed' THEN true
		                  WHEN $2 IN ('uninstalled', 'error') THEN false
		                  ELSE active END,
		    updated_at = NOW()
		WHERE id = $1
	`, id, state, lastError)
	return err
}

func (r *workerRepository) UpdateLastSeen(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE workers SET last_seen_at = $2 WHERE id = $1
	`, id, at)
	return err
}

func (r *workerRepository) UpdateHostFingerprint(ctx context.Context, id uuid.UUID, fingerprint string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE workers SET ssh_host_fingerprint = $2, updated_at = NOW() WHERE id = $1
	`, id, fingerprint)
	return err
}

func (r *workerRepository) RotateSSHKey(ctx context.Context, id uuid.UUID, publicKey, privateKeyEncrypted string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE workers
		SET ssh_public_key = $2,
		    ssh_private_key_encrypted = $3,
		    updated_at = NOW()
		WHERE id = $1
	`, id, publicKey, privateKeyEncrypted)
	return err
}

func (r *workerRepository) DeleteWorker(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM workers WHERE id = $1`, id)
	return err
}

func (r *workerRepository) ConsumeEnrollmentToken(ctx context.Context, tokenHash string) (*models.Worker, error) {
	row := r.db.QueryRow(ctx, `
		UPDATE workers
		SET enrollment_token_hash = NULL,
		    enrollment_token_expires_at = NULL,
		    updated_at = NOW()
		WHERE enrollment_token_hash = $1
		  AND enrollment_token_expires_at > NOW()
		RETURNING `+workerDetailColumns+`
	`, tokenHash)
	return scanWorkerDetail(row)
}

func (r *workerRepository) AssignWorkerProfile(ctx context.Context, workerID uuid.UUID, profileID *uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE workers SET profile_id = $2, updated_at = NOW() WHERE id = $1
	`, workerID, profileID)
	return err
}

func (r *workerRepository) MarkConfigApplied(ctx context.Context, workerID uuid.UUID, at time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE workers SET config_applied_at = $2 WHERE id = $1
	`, workerID, at)
	return err
}

func (r *workerRepository) MarkImageVersion(ctx context.Context, workerID uuid.UUID, version string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE workers SET image_version = $2, updated_at = NOW() WHERE id = $1
	`, workerID, version)
	return err
}

func (r *workerRepository) ListWorkersByProfile(ctx context.Context, profileID uuid.UUID) ([]models.Worker, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+workerDetailColumns+`
		FROM workers
		WHERE profile_id = $1
		ORDER BY created_at DESC
	`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.Worker, 0)
	for rows.Next() {
		w, err := scanWorkerDetail(rows)
		if err != nil {
			return nil, err
		}
		if w != nil {
			out = append(out, *w)
		}
	}
	return out, rows.Err()
}

func (r *workerRepository) RecordEnrolledIP(ctx context.Context, id uuid.UUID, ip string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE workers SET ip_addr = $2, ssh_host = COALESCE(NULLIF(ssh_host,''), $2), updated_at = NOW()
		WHERE id = $1
	`, id, ip)
	return err
}

package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// AWS credentials

type CredentialsRepository interface {
	// AWS credentials
	CreateAWSCreds(ctx context.Context, in CreateAWSCredsInput) (uuid.UUID, error)
	UpdateAWSCreds(ctx context.Context, id uuid.UUID, in UpdateAWSCredsInput) error
	GetAWSCreds(ctx context.Context, id uuid.UUID) (*models.AWSCredentials, error)
	ListAWSCreds(ctx context.Context) ([]models.AWSCredentials, error)
	DeleteAWSCreds(ctx context.Context, id uuid.UUID) error
	// Plain decrypted helpers for the orchestrator — never expose over API.
	GetAWSCredsEncryptedSecret(ctx context.Context, id uuid.UUID) (string, error)

	// Worker profiles
	CreateProfile(ctx context.Context, in CreateProfileInput) (uuid.UUID, error)
	UpdateProfile(ctx context.Context, id uuid.UUID, in UpdateProfileInput) error
	GetProfile(ctx context.Context, id uuid.UUID) (*models.WorkerProfile, error)
	GetProfileEncrypted(ctx context.Context, id uuid.UUID) (*ProfileEncrypted, error)
	ListProfiles(ctx context.Context) ([]models.WorkerProfile, error)
	DeleteProfile(ctx context.Context, id uuid.UUID) error

	// Release channel management
	UpdateProfileRelease(ctx context.Context, id uuid.UUID, channel models.ReleaseChannel, autoUpdate bool) error
	ListProfilesByChannel(ctx context.Context, channel models.ReleaseChannel) ([]models.WorkerProfile, error)
	RecordResolvedTag(ctx context.Context, id uuid.UUID, image, tag string) error
}

type CreateAWSCredsInput struct {
	Name                     string
	Description              string
	Region                   string
	AccessKeyID              string
	SecretAccessKeyEncrypted string
}

type UpdateAWSCredsInput struct {
	Name        string
	Description string
	Region      string
	AccessKeyID string
	// When empty, the existing encrypted secret is kept; otherwise replaced.
	SecretAccessKeyEncrypted string
}

type CreateProfileInput struct {
	Name                          string
	Description                   string
	AppEnv                        string
	WorkerImage                   string
	KafkaBootstrap                string
	KafkaSASLUsername             string
	KafkaSASLPasswordEncrypted    string
	SchemaRegistryURL             string
	SchemaRegistryKey             string
	SchemaRegistrySecretEncrypted string
	RedisURLEncrypted             string
	AWSCredentialID               *uuid.UUID
}

// UpdateProfileInput mirrors create. Empty-string encrypted fields mean
// "leave the stored value as-is" (so the UI can submit a partial update
// without re-typing all secrets).
type UpdateProfileInput = CreateProfileInput

// ProfileEncrypted exposes the still-encrypted secret material to the
// orchestrator, which is the only caller that has cipher access.
type ProfileEncrypted struct {
	Profile                       *models.WorkerProfile
	KafkaSASLPasswordEncrypted    string
	SchemaRegistrySecretEncrypted string
	RedisURLEncrypted             string
}

type credentialsRepository struct {
	db *pgxpool.Pool
}

func NewCredentialsRepository(db *pgxpool.Pool) CredentialsRepository {
	return &credentialsRepository{db: db}
}

// AWS creds impl

func (r *credentialsRepository) CreateAWSCreds(ctx context.Context, in CreateAWSCredsInput) (uuid.UUID, error) {
	id := uuid.New()
	_, err := r.db.Exec(ctx, `
		INSERT INTO aws_credentials (id, name, description, region, access_key_id, secret_access_key_encrypted)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, id, in.Name, in.Description, in.Region, in.AccessKeyID, in.SecretAccessKeyEncrypted)
	return id, err
}

func (r *credentialsRepository) UpdateAWSCreds(ctx context.Context, id uuid.UUID, in UpdateAWSCredsInput) error {
	if in.SecretAccessKeyEncrypted == "" {
		_, err := r.db.Exec(ctx, `
			UPDATE aws_credentials
			SET name=$2, description=$3, region=$4, access_key_id=$5, updated_at=NOW()
			WHERE id=$1
		`, id, in.Name, in.Description, in.Region, in.AccessKeyID)
		return err
	}
	_, err := r.db.Exec(ctx, `
		UPDATE aws_credentials
		SET name=$2, description=$3, region=$4, access_key_id=$5,
		    secret_access_key_encrypted=$6, updated_at=NOW()
		WHERE id=$1
	`, id, in.Name, in.Description, in.Region, in.AccessKeyID, in.SecretAccessKeyEncrypted)
	return err
}

func (r *credentialsRepository) GetAWSCreds(ctx context.Context, id uuid.UUID) (*models.AWSCredentials, error) {
	var c models.AWSCredentials
	var secret string
	err := r.db.QueryRow(ctx, `
		SELECT id, name, description, region, access_key_id, secret_access_key_encrypted, created_at, updated_at
		FROM aws_credentials WHERE id=$1
	`, id).Scan(&c.ID, &c.Name, &c.Description, &c.Region, &c.AccessKeyID, &secret, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.SecretAccessKeyEncrypted = secret
	c.HasSecret = secret != ""
	return &c, nil
}

func (r *credentialsRepository) ListAWSCreds(ctx context.Context) ([]models.AWSCredentials, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, name, description, region, access_key_id, secret_access_key_encrypted, created_at, updated_at
		FROM aws_credentials ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.AWSCredentials, 0)
	for rows.Next() {
		var c models.AWSCredentials
		var secret string
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &c.Region, &c.AccessKeyID, &secret, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.HasSecret = secret != ""
		// Strip ciphertext from list responses to avoid accidentally serializing.
		c.SecretAccessKeyEncrypted = ""
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *credentialsRepository) DeleteAWSCreds(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM aws_credentials WHERE id=$1`, id)
	return err
}

func (r *credentialsRepository) GetAWSCredsEncryptedSecret(ctx context.Context, id uuid.UUID) (string, error) {
	var s string
	err := r.db.QueryRow(ctx, `SELECT secret_access_key_encrypted FROM aws_credentials WHERE id=$1`, id).Scan(&s)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return s, err
}

// worker profiles impl

func (r *credentialsRepository) CreateProfile(ctx context.Context, in CreateProfileInput) (uuid.UUID, error) {
	id := uuid.New()
	_, err := r.db.Exec(ctx, `
		INSERT INTO worker_profiles (
			id, name, description,
			app_env, worker_image,
			kafka_bootstrap_servers, kafka_sasl_username, kafka_sasl_password_encrypted,
			schema_registry_url, schema_registry_key, schema_registry_secret_encrypted,
			redis_url_encrypted, aws_credential_id
		) VALUES (
			$1, $2, $3,
			$4, $5,
			$6, $7, $8,
			$9, $10, $11,
			$12, $13
		)
	`,
		id, in.Name, in.Description,
		in.AppEnv, in.WorkerImage,
		in.KafkaBootstrap, in.KafkaSASLUsername, in.KafkaSASLPasswordEncrypted,
		in.SchemaRegistryURL, in.SchemaRegistryKey, in.SchemaRegistrySecretEncrypted,
		in.RedisURLEncrypted, in.AWSCredentialID,
	)
	return id, err
}

func (r *credentialsRepository) UpdateProfile(ctx context.Context, id uuid.UUID, in UpdateProfileInput) error {
	// COALESCE-style: empty encrypted fields keep existing values.
	_, err := r.db.Exec(ctx, `
		UPDATE worker_profiles SET
			name=$2, description=$3,
			app_env=$4, worker_image=$5,
			kafka_bootstrap_servers=$6, kafka_sasl_username=$7,
			kafka_sasl_password_encrypted = CASE WHEN $8 = '' THEN kafka_sasl_password_encrypted ELSE $8 END,
			schema_registry_url=$9, schema_registry_key=$10,
			schema_registry_secret_encrypted = CASE WHEN $11 = '' THEN schema_registry_secret_encrypted ELSE $11 END,
			redis_url_encrypted = CASE WHEN $12 = '' THEN redis_url_encrypted ELSE $12 END,
			aws_credential_id=$13,
			updated_at=NOW()
		WHERE id=$1
	`,
		id, in.Name, in.Description,
		in.AppEnv, in.WorkerImage,
		in.KafkaBootstrap, in.KafkaSASLUsername, in.KafkaSASLPasswordEncrypted,
		in.SchemaRegistryURL, in.SchemaRegistryKey, in.SchemaRegistrySecretEncrypted,
		in.RedisURLEncrypted, in.AWSCredentialID,
	)
	return err
}

func (r *credentialsRepository) scanProfile(row pgx.Row) (*models.WorkerProfile, string, string, string, error) {
	var p models.WorkerProfile
	var kafkaSecret, schemaSecret, redisURL string
	err := row.Scan(
		&p.ID, &p.Name, &p.Description,
		&p.AppEnv, &p.WorkerImage,
		&p.KafkaBootstrapServers, &p.KafkaSASLUsername, &kafkaSecret,
		&p.SchemaRegistryURL, &p.SchemaRegistryKey, &schemaSecret,
		&redisURL, &p.AWSCredentialID,
		&p.ReleaseChannel, &p.AutoUpdate, &p.ResolvedImageTag, &p.LastReleaseCheckAt,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, "", "", "", err
	}
	p.HasKafkaPassword = kafkaSecret != ""
	p.HasSchemaSecret = schemaSecret != ""
	p.HasRedisURL = redisURL != ""
	return &p, kafkaSecret, schemaSecret, redisURL, nil
}

const profileColumns = `
	id, name, description,
	app_env, worker_image,
	kafka_bootstrap_servers, kafka_sasl_username, kafka_sasl_password_encrypted,
	schema_registry_url, schema_registry_key, schema_registry_secret_encrypted,
	redis_url_encrypted, aws_credential_id,
	release_channel, auto_update, resolved_image_tag, last_release_check_at,
	created_at, updated_at
`

func (r *credentialsRepository) GetProfile(ctx context.Context, id uuid.UUID) (*models.WorkerProfile, error) {
	p, _, _, _, err := r.scanProfile(r.db.QueryRow(ctx, `SELECT `+profileColumns+` FROM worker_profiles WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (r *credentialsRepository) GetProfileEncrypted(ctx context.Context, id uuid.UUID) (*ProfileEncrypted, error) {
	p, kafkaEnc, schemaEnc, redisEnc, err := r.scanProfile(
		r.db.QueryRow(ctx, `SELECT `+profileColumns+` FROM worker_profiles WHERE id=$1`, id),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ProfileEncrypted{
		Profile:                       p,
		KafkaSASLPasswordEncrypted:    kafkaEnc,
		SchemaRegistrySecretEncrypted: schemaEnc,
		RedisURLEncrypted:             redisEnc,
	}, nil
}

func (r *credentialsRepository) ListProfiles(ctx context.Context) ([]models.WorkerProfile, error) {
	rows, err := r.db.Query(ctx, `SELECT `+profileColumns+` FROM worker_profiles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.WorkerProfile, 0)
	for rows.Next() {
		p, _, _, _, err := r.scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (r *credentialsRepository) DeleteProfile(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM worker_profiles WHERE id=$1`, id)
	return err
}

func (r *credentialsRepository) UpdateProfileRelease(ctx context.Context, id uuid.UUID, channel models.ReleaseChannel, autoUpdate bool) error {
	_, err := r.db.Exec(ctx, `
		UPDATE worker_profiles
		SET release_channel = $2, auto_update = $3, updated_at = NOW()
		WHERE id = $1
	`, id, channel, autoUpdate)
	return err
}

func (r *credentialsRepository) ListProfilesByChannel(ctx context.Context, channel models.ReleaseChannel) ([]models.WorkerProfile, error) {
	rows, err := r.db.Query(ctx, `SELECT `+profileColumns+` FROM worker_profiles WHERE release_channel = $1`, channel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.WorkerProfile, 0)
	for rows.Next() {
		p, _, _, _, err := r.scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// RecordResolvedTag updates the profile's worker_image and resolved_image_tag
// after the release poller resolves a channel to a concrete tag. Bumps
// updated_at so the UI's "stale config" indicator fires for assigned workers
// that haven't rolled yet.
func (r *credentialsRepository) RecordResolvedTag(ctx context.Context, id uuid.UUID, image, tag string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE worker_profiles
		SET worker_image = $2,
		    resolved_image_tag = $3,
		    last_release_check_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
	`, id, image, tag)
	return err
}

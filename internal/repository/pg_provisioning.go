package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// ----- cloud_credentials -----

type CloudCredential struct {
	ID             uuid.UUID
	Provider       string
	Name           string
	EncryptedToken string
	LastUsedAt     *time.Time
	LastTestAt     *time.Time
	LastTestOK     *bool
	LastTestError  *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type CloudCredentialRepository interface {
	List(ctx context.Context) ([]CloudCredential, error)
	Get(ctx context.Context, id uuid.UUID) (*CloudCredential, error)
	GetByProvider(ctx context.Context, provider string) (*CloudCredential, error)
	Create(ctx context.Context, c *CloudCredential) error
	UpdateTestResult(ctx context.Context, id uuid.UUID, ok bool, errMsg string) error
	TouchLastUsed(ctx context.Context, id uuid.UUID) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type cloudCredentialRepository struct{ db *db.DB }

func NewCloudCredentialRepository(d *db.DB) CloudCredentialRepository {
	return &cloudCredentialRepository{db: d}
}

const cloudCredCols = `id, provider, name, encrypted_token, last_used_at, last_test_at,
                       last_test_ok, last_test_error, created_at, updated_at`

func scanCloudCred(row pgx.Row) (*CloudCredential, error) {
	var c CloudCredential
	if err := row.Scan(&c.ID, &c.Provider, &c.Name, &c.EncryptedToken,
		&c.LastUsedAt, &c.LastTestAt, &c.LastTestOK, &c.LastTestError,
		&c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *cloudCredentialRepository) List(ctx context.Context) ([]CloudCredential, error) {
	rows, err := r.db.Query(ctx, `SELECT `+cloudCredCols+` FROM cloud_credentials ORDER BY provider, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CloudCredential
	for rows.Next() {
		c, err := scanCloudCred(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (r *cloudCredentialRepository) Get(ctx context.Context, id uuid.UUID) (*CloudCredential, error) {
	row := r.db.QueryRow(ctx, `SELECT `+cloudCredCols+` FROM cloud_credentials WHERE id = $1`, id)
	c, err := scanCloudCred(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

func (r *cloudCredentialRepository) GetByProvider(ctx context.Context, provider string) (*CloudCredential, error) {
	row := r.db.QueryRow(ctx,
		`SELECT `+cloudCredCols+` FROM cloud_credentials
		 WHERE provider = $1 ORDER BY created_at DESC LIMIT 1`, provider)
	c, err := scanCloudCred(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

func (r *cloudCredentialRepository) Create(ctx context.Context, c *CloudCredential) error {
	const q = `
		INSERT INTO cloud_credentials (provider, name, encrypted_token)
		VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at
	`
	return r.db.QueryRow(ctx, q, c.Provider, c.Name, c.EncryptedToken).
		Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
}

func (r *cloudCredentialRepository) UpdateTestResult(ctx context.Context, id uuid.UUID, ok bool, errMsg string) error {
	const q = `
		UPDATE cloud_credentials
		SET last_test_at = now(), last_test_ok = $2, last_test_error = $3, updated_at = now()
		WHERE id = $1
	`
	var errPtr *string
	if errMsg != "" {
		errPtr = &errMsg
	}
	_, err := r.db.Exec(ctx, q, id, ok, errPtr)
	return err
}

func (r *cloudCredentialRepository) TouchLastUsed(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE cloud_credentials SET last_used_at = now() WHERE id = $1`, id)
	return err
}

func (r *cloudCredentialRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM cloud_credentials WHERE id = $1`, id)
	return err
}

// ----- provisioning_templates -----

type ProvisioningTemplate struct {
	ID              uuid.UUID         `json:"id"`
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Provider        string            `json:"provider"`
	Location        string            `json:"location"`
	Datacenter      string            `json:"datacenter,omitempty"`
	ServerType      string            `json:"server_type"`
	Image           string            `json:"image"`
	ServerCount     int               `json:"server_count"`
	IPv4PerServer   int               `json:"ipv4_per_server"`
	IPv6PerServer   int               `json:"ipv6_per_server"`
	WorkerProfileID *uuid.UUID        `json:"worker_profile_id,omitempty"`
	Tier            string            `json:"tier"`
	EgressKind      string            `json:"egress_kind"`
	Labels          map[string]string `json:"labels"`
	PlacementGroup  string            `json:"placement_group,omitempty"`
	PrivateNetwork  string            `json:"private_network,omitempty"`
	Firewall        string            `json:"firewall,omitempty"`
	IsAutoTemplate  bool              `json:"is_auto_template"`
	EstMonthlyCost  *float64          `json:"est_monthly_cost,omitempty"`
	EstCostCurrency string            `json:"est_cost_currency,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

type ProvisioningTemplateRepository interface {
	List(ctx context.Context) ([]ProvisioningTemplate, error)
	Get(ctx context.Context, id uuid.UUID) (*ProvisioningTemplate, error)
	GetAutoForTier(ctx context.Context, tier string) (*ProvisioningTemplate, error)
	Create(ctx context.Context, t *ProvisioningTemplate) error
	Update(ctx context.Context, t *ProvisioningTemplate) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type provisioningTemplateRepository struct{ db *db.DB }

func NewProvisioningTemplateRepository(d *db.DB) ProvisioningTemplateRepository {
	return &provisioningTemplateRepository{db: d}
}

const tplCols = `id, name, description, provider, location, datacenter, server_type,
                 image, server_count, ipv4_per_server, ipv6_per_server, worker_profile_id,
                 tier, egress_kind, labels, placement_group, private_network, firewall,
                 is_auto_template, est_monthly_cost, est_cost_currency, created_at, updated_at`

func scanTpl(row pgx.Row) (*ProvisioningTemplate, error) {
	var t ProvisioningTemplate
	var desc, dc, pg, pn, fw, ccur *string
	var labels []byte
	if err := row.Scan(
		&t.ID, &t.Name, &desc, &t.Provider, &t.Location, &dc, &t.ServerType,
		&t.Image, &t.ServerCount, &t.IPv4PerServer, &t.IPv6PerServer, &t.WorkerProfileID,
		&t.Tier, &t.EgressKind, &labels, &pg, &pn, &fw,
		&t.IsAutoTemplate, &t.EstMonthlyCost, &ccur, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	t.Labels = map[string]string{}
	if len(labels) > 0 {
		_ = json.Unmarshal(labels, &t.Labels)
	}
	if desc != nil {
		t.Description = *desc
	}
	if dc != nil {
		t.Datacenter = *dc
	}
	if pg != nil {
		t.PlacementGroup = *pg
	}
	if pn != nil {
		t.PrivateNetwork = *pn
	}
	if fw != nil {
		t.Firewall = *fw
	}
	if ccur != nil {
		t.EstCostCurrency = *ccur
	}
	return &t, nil
}

func (r *provisioningTemplateRepository) List(ctx context.Context) ([]ProvisioningTemplate, error) {
	rows, err := r.db.Query(ctx, `SELECT `+tplCols+` FROM provisioning_templates ORDER BY tier, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProvisioningTemplate
	for rows.Next() {
		t, err := scanTpl(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (r *provisioningTemplateRepository) Get(ctx context.Context, id uuid.UUID) (*ProvisioningTemplate, error) {
	row := r.db.QueryRow(ctx, `SELECT `+tplCols+` FROM provisioning_templates WHERE id = $1`, id)
	t, err := scanTpl(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func (r *provisioningTemplateRepository) GetAutoForTier(ctx context.Context, tier string) (*ProvisioningTemplate, error) {
	row := r.db.QueryRow(ctx,
		`SELECT `+tplCols+` FROM provisioning_templates WHERE tier = $1 AND is_auto_template LIMIT 1`, tier)
	t, err := scanTpl(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func (r *provisioningTemplateRepository) Create(ctx context.Context, t *ProvisioningTemplate) error {
	labels, _ := json.Marshal(t.Labels)
	const q = `
		INSERT INTO provisioning_templates
		  (name, description, provider, location, datacenter, server_type, image,
		   server_count, ipv4_per_server, ipv6_per_server, worker_profile_id, tier,
		   egress_kind, labels, placement_group, private_network, firewall,
		   is_auto_template, est_monthly_cost, est_cost_currency)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
		RETURNING id, created_at, updated_at
	`
	return r.db.QueryRow(ctx, q,
		t.Name, nullIfEmpty(t.Description), t.Provider, t.Location, nullIfEmpty(t.Datacenter),
		t.ServerType, t.Image, t.ServerCount, t.IPv4PerServer, t.IPv6PerServer,
		t.WorkerProfileID, t.Tier, t.EgressKind, labels, nullIfEmpty(t.PlacementGroup),
		nullIfEmpty(t.PrivateNetwork), nullIfEmpty(t.Firewall),
		t.IsAutoTemplate, t.EstMonthlyCost, nullIfEmpty(t.EstCostCurrency)).
		Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
}

func (r *provisioningTemplateRepository) Update(ctx context.Context, t *ProvisioningTemplate) error {
	labels, _ := json.Marshal(t.Labels)
	const q = `
		UPDATE provisioning_templates SET
		  name=$2, description=$3, provider=$4, location=$5, datacenter=$6,
		  server_type=$7, image=$8, server_count=$9, ipv4_per_server=$10,
		  ipv6_per_server=$11, worker_profile_id=$12, tier=$13, egress_kind=$14,
		  labels=$15, placement_group=$16, private_network=$17, firewall=$18,
		  is_auto_template=$19, est_monthly_cost=$20, est_cost_currency=$21,
		  updated_at=now()
		WHERE id=$1
	`
	tag, err := r.db.Exec(ctx, q,
		t.ID, t.Name, nullIfEmpty(t.Description), t.Provider, t.Location, nullIfEmpty(t.Datacenter),
		t.ServerType, t.Image, t.ServerCount, t.IPv4PerServer, t.IPv6PerServer,
		t.WorkerProfileID, t.Tier, t.EgressKind, labels, nullIfEmpty(t.PlacementGroup),
		nullIfEmpty(t.PrivateNetwork), nullIfEmpty(t.Firewall),
		t.IsAutoTemplate, t.EstMonthlyCost, nullIfEmpty(t.EstCostCurrency))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("provisioning_templates: id %s not found", t.ID)
	}
	return nil
}

func (r *provisioningTemplateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM provisioning_templates WHERE id = $1`, id)
	return err
}

// ----- provisioning_jobs -----

type ProvisioningJob struct {
	ID               uuid.UUID
	State            models.ProvisioningJobState
	TriggeredBy      string
	Provider         string
	CredentialID     *uuid.UUID
	TemplateID       *uuid.UUID
	Config           json.RawMessage
	ProviderServerID *string
	ProviderIPIDs    []string
	IPs              []string // INET[] as strings
	WorkerIDs        []uuid.UUID
	EstMonthlyCost   *float64
	CostCurrency     string
	Error            *string
	Attempts         int
	LastStepAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CompletedAt      *time.Time
}

type ProvisioningJobRepository interface {
	List(ctx context.Context, limit int) ([]ProvisioningJob, error)
	ListInFlight(ctx context.Context) ([]ProvisioningJob, error)
	Get(ctx context.Context, id uuid.UUID) (*ProvisioningJob, error)
	Create(ctx context.Context, j *ProvisioningJob) error
	UpdateState(ctx context.Context, id uuid.UUID, state models.ProvisioningJobState) error
	RecordServer(ctx context.Context, id uuid.UUID, providerServerID string) error
	AppendIPs(ctx context.Context, id uuid.UUID, ipIDs []string, ips []string) error
	AppendWorkerIDs(ctx context.Context, id uuid.UUID, workerIDs []uuid.UUID) error
	MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error
	MarkCompleted(ctx context.Context, id uuid.UUID) error
}

type provisioningJobRepository struct{ db *db.DB }

func NewProvisioningJobRepository(d *db.DB) ProvisioningJobRepository {
	return &provisioningJobRepository{db: d}
}

const jobCols = `id, state, triggered_by, provider, credential_id, template_id, config,
                 provider_server_id, provider_ip_ids, ips, worker_ids, est_monthly_cost,
                 cost_currency, error, attempts, last_step_at, created_at, updated_at, completed_at`

func scanJob(row pgx.Row) (*ProvisioningJob, error) {
	var j ProvisioningJob
	var ips []string
	var workerIDs []uuid.UUID
	var ipIDs []string
	var ccur *string
	if err := row.Scan(
		&j.ID, &j.State, &j.TriggeredBy, &j.Provider, &j.CredentialID, &j.TemplateID,
		&j.Config, &j.ProviderServerID, &ipIDs, &ips, &workerIDs, &j.EstMonthlyCost,
		&ccur, &j.Error, &j.Attempts, &j.LastStepAt, &j.CreatedAt, &j.UpdatedAt, &j.CompletedAt); err != nil {
		return nil, err
	}
	j.ProviderIPIDs = ipIDs
	j.IPs = ips
	j.WorkerIDs = workerIDs
	if ccur != nil {
		j.CostCurrency = *ccur
	}
	return &j, nil
}

func (r *provisioningJobRepository) List(ctx context.Context, limit int) ([]ProvisioningJob, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.Query(ctx,
		`SELECT `+jobCols+` FROM provisioning_jobs ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProvisioningJob
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (r *provisioningJobRepository) ListInFlight(ctx context.Context) ([]ProvisioningJob, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+jobCols+` FROM provisioning_jobs
		 WHERE state NOT IN ('completed','failed') ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProvisioningJob
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (r *provisioningJobRepository) Get(ctx context.Context, id uuid.UUID) (*ProvisioningJob, error) {
	row := r.db.QueryRow(ctx, `SELECT `+jobCols+` FROM provisioning_jobs WHERE id = $1`, id)
	j, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

func (r *provisioningJobRepository) Create(ctx context.Context, j *ProvisioningJob) error {
	if len(j.Config) == 0 {
		j.Config = json.RawMessage(`{}`)
	}
	const q = `
		INSERT INTO provisioning_jobs
		  (state, triggered_by, provider, credential_id, template_id, config,
		   est_monthly_cost, cost_currency)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, created_at, updated_at
	`
	state := j.State
	if state == "" {
		state = models.ProvJobPending
	}
	ccur := j.CostCurrency
	if ccur == "" {
		ccur = "EUR"
	}
	return r.db.QueryRow(ctx, q, string(state), j.TriggeredBy, j.Provider, j.CredentialID,
		j.TemplateID, []byte(j.Config), j.EstMonthlyCost, ccur).
		Scan(&j.ID, &j.CreatedAt, &j.UpdatedAt)
}

func (r *provisioningJobRepository) UpdateState(ctx context.Context, id uuid.UUID, state models.ProvisioningJobState) error {
	_, err := r.db.Exec(ctx,
		`UPDATE provisioning_jobs
		 SET state=$2, last_step_at=now(), updated_at=now(), attempts=attempts+1
		 WHERE id=$1`, id, string(state))
	return err
}

func (r *provisioningJobRepository) RecordServer(ctx context.Context, id uuid.UUID, providerServerID string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE provisioning_jobs SET provider_server_id=$2, updated_at=now() WHERE id=$1`,
		id, providerServerID)
	return err
}

func (r *provisioningJobRepository) AppendIPs(ctx context.Context, id uuid.UUID, ipIDs []string, ips []string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE provisioning_jobs
		 SET provider_ip_ids = provider_ip_ids || $2::text[],
		     ips = ips || $3::inet[],
		     updated_at = now()
		 WHERE id=$1`, id, ipIDs, ips)
	return err
}

func (r *provisioningJobRepository) AppendWorkerIDs(ctx context.Context, id uuid.UUID, workerIDs []uuid.UUID) error {
	_, err := r.db.Exec(ctx,
		`UPDATE provisioning_jobs
		 SET worker_ids = worker_ids || $2::uuid[], updated_at = now()
		 WHERE id=$1`, id, workerIDs)
	return err
}

func (r *provisioningJobRepository) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE provisioning_jobs
		 SET state='failed', error=$2, completed_at=now(), updated_at=now()
		 WHERE id=$1`, id, errMsg)
	return err
}

func (r *provisioningJobRepository) MarkCompleted(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx,
		`UPDATE provisioning_jobs
		 SET state='completed', completed_at=now(), updated_at=now()
		 WHERE id=$1`, id)
	return err
}

// ----- provisioning_policy -----

type ProvisioningPolicy struct {
	Provider        string
	Enabled         bool
	AutoProvision   bool
	MaxPerDay       int
	MaxPerMonth     int
	MonthlyBudget   *float64
	BudgetCurrency  string
	CooldownMinutes int
	UpdatedAt       time.Time
}

type ProvisioningPolicyRepository interface {
	Get(ctx context.Context, provider string) (*ProvisioningPolicy, error)
	List(ctx context.Context) ([]ProvisioningPolicy, error)
	Update(ctx context.Context, p *ProvisioningPolicy) error
}

type provisioningPolicyRepository struct{ db *db.DB }

func NewProvisioningPolicyRepository(d *db.DB) ProvisioningPolicyRepository {
	return &provisioningPolicyRepository{db: d}
}

const polCols = `provider, enabled, auto_provision, max_per_day, max_per_month,
                 monthly_budget, budget_currency, cooldown_min, updated_at`

func scanPol(row pgx.Row) (*ProvisioningPolicy, error) {
	var p ProvisioningPolicy
	var bcur *string
	if err := row.Scan(&p.Provider, &p.Enabled, &p.AutoProvision, &p.MaxPerDay, &p.MaxPerMonth,
		&p.MonthlyBudget, &bcur, &p.CooldownMinutes, &p.UpdatedAt); err != nil {
		return nil, err
	}
	if bcur != nil {
		p.BudgetCurrency = *bcur
	}
	return &p, nil
}

func (r *provisioningPolicyRepository) Get(ctx context.Context, provider string) (*ProvisioningPolicy, error) {
	row := r.db.QueryRow(ctx, `SELECT `+polCols+` FROM provisioning_policy WHERE provider = $1`, provider)
	p, err := scanPol(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (r *provisioningPolicyRepository) List(ctx context.Context) ([]ProvisioningPolicy, error) {
	rows, err := r.db.Query(ctx, `SELECT `+polCols+` FROM provisioning_policy ORDER BY provider`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProvisioningPolicy
	for rows.Next() {
		p, err := scanPol(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (r *provisioningPolicyRepository) Update(ctx context.Context, p *ProvisioningPolicy) error {
	_, err := r.db.Exec(ctx, `
		UPDATE provisioning_policy SET
		  enabled=$2, auto_provision=$3, max_per_day=$4, max_per_month=$5,
		  monthly_budget=$6, budget_currency=$7, cooldown_min=$8, updated_at=now()
		WHERE provider=$1`,
		p.Provider, p.Enabled, p.AutoProvision, p.MaxPerDay, p.MaxPerMonth,
		p.MonthlyBudget, nullIfEmpty(p.BudgetCurrency), p.CooldownMinutes)
	return err
}

// ----- helpers -----

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

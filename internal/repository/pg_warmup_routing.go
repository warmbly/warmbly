package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// WarmupRoutingRepository persists customer-defined warmup routing rules.
type WarmupRoutingRepository interface {
	Create(ctx context.Context, rule *models.WarmupRoutingRule) error
	Update(ctx context.Context, rule *models.WarmupRoutingRule) error
	Delete(ctx context.Context, organizationID, ruleID uuid.UUID) error
	GetByID(ctx context.Context, organizationID, ruleID uuid.UUID) (*models.WarmupRoutingRule, error)
	ListForOrganization(ctx context.Context, organizationID uuid.UUID) ([]models.WarmupRoutingRule, error)
}

type warmupRoutingRepository struct {
	db *pgxpool.Pool
}

func NewWarmupRoutingRepository(db *pgxpool.Pool) WarmupRoutingRepository {
	return &warmupRoutingRepository{db: db}
}

func (r *warmupRoutingRepository) Create(ctx context.Context, rule *models.WarmupRoutingRule) error {
	if rule.ID == uuid.Nil {
		rule.ID = uuid.New()
	}
	rule.CreatedAt = time.Now().UTC()
	rule.UpdatedAt = rule.CreatedAt

	query := `
		INSERT INTO warmup_routing_rules (
			id, organization_id, name, priority,
			sender_match_type, sender_match_value,
			recipient_match_type, recipient_match_value,
			weight, enabled, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
	`
	_, err := r.db.Exec(ctx, query,
		rule.ID, rule.OrganizationID, rule.Name, rule.Priority,
		string(rule.SenderMatchType), rule.SenderMatchValue,
		string(rule.RecipientMatchType), rule.RecipientMatchValue,
		rule.Weight, rule.Enabled, rule.CreatedAt,
	)
	return err
}

func (r *warmupRoutingRepository) Update(ctx context.Context, rule *models.WarmupRoutingRule) error {
	rule.UpdatedAt = time.Now().UTC()

	query := `
		UPDATE warmup_routing_rules
		SET name = $1,
		    priority = $2,
		    sender_match_type = $3,
		    sender_match_value = $4,
		    recipient_match_type = $5,
		    recipient_match_value = $6,
		    weight = $7,
		    enabled = $8,
		    updated_at = $9
		WHERE id = $10 AND organization_id = $11
	`
	cmd, err := r.db.Exec(ctx, query,
		rule.Name, rule.Priority,
		string(rule.SenderMatchType), rule.SenderMatchValue,
		string(rule.RecipientMatchType), rule.RecipientMatchValue,
		rule.Weight, rule.Enabled, rule.UpdatedAt,
		rule.ID, rule.OrganizationID,
	)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return errors.New("warmup routing rule not found")
	}
	return nil
}

func (r *warmupRoutingRepository) Delete(ctx context.Context, organizationID, ruleID uuid.UUID) error {
	cmd, err := r.db.Exec(ctx,
		`DELETE FROM warmup_routing_rules WHERE id = $1 AND organization_id = $2`,
		ruleID, organizationID,
	)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return errors.New("warmup routing rule not found")
	}
	return nil
}

func (r *warmupRoutingRepository) GetByID(ctx context.Context, organizationID, ruleID uuid.UUID) (*models.WarmupRoutingRule, error) {
	query := `
		SELECT id, organization_id, name, priority,
		       sender_match_type, sender_match_value,
		       recipient_match_type, recipient_match_value,
		       weight, enabled, created_at, updated_at
		FROM warmup_routing_rules
		WHERE id = $1 AND organization_id = $2
	`
	rule := &models.WarmupRoutingRule{}
	var senderType, recipientType string
	if err := r.db.QueryRow(ctx, query, ruleID, organizationID).Scan(
		&rule.ID, &rule.OrganizationID, &rule.Name, &rule.Priority,
		&senderType, &rule.SenderMatchValue,
		&recipientType, &rule.RecipientMatchValue,
		&rule.Weight, &rule.Enabled, &rule.CreatedAt, &rule.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	rule.SenderMatchType = models.WarmupRoutingMatchType(senderType)
	rule.RecipientMatchType = models.WarmupRoutingMatchType(recipientType)
	return rule, nil
}

func (r *warmupRoutingRepository) ListForOrganization(ctx context.Context, organizationID uuid.UUID) ([]models.WarmupRoutingRule, error) {
	query := `
		SELECT id, organization_id, name, priority,
		       sender_match_type, sender_match_value,
		       recipient_match_type, recipient_match_value,
		       weight, enabled, created_at, updated_at
		FROM warmup_routing_rules
		WHERE organization_id = $1
		ORDER BY priority ASC, created_at ASC
	`
	rows, err := r.db.Query(ctx, query, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.WarmupRoutingRule
	for rows.Next() {
		var rule models.WarmupRoutingRule
		var senderType, recipientType string
		if err := rows.Scan(
			&rule.ID, &rule.OrganizationID, &rule.Name, &rule.Priority,
			&senderType, &rule.SenderMatchValue,
			&recipientType, &rule.RecipientMatchValue,
			&rule.Weight, &rule.Enabled, &rule.CreatedAt, &rule.UpdatedAt,
		); err != nil {
			return nil, err
		}
		rule.SenderMatchType = models.WarmupRoutingMatchType(senderType)
		rule.RecipientMatchType = models.WarmupRoutingMatchType(recipientType)
		out = append(out, rule)
	}
	return out, rows.Err()
}

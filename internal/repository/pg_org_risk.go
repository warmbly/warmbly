package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

type OrgRiskFinding struct {
	OrganizationID uuid.UUID
	Key            string
	Score          int
	Reason         string
	Evidence       map[string]any
}

type OrgRiskRepository interface {
	Get(ctx context.Context, organizationID uuid.UUID) (*models.OrganizationRisk, error)
	RecordSignal(ctx context.Context, organizationID uuid.UUID, key string, signal models.OrganizationRiskSignal) (*models.OrganizationRisk, *models.OrganizationRisk, error)
	ListCorrelationFindings(ctx context.Context) ([]OrgRiskFinding, error)
}

type orgRiskRepository struct {
	db *pgxpool.Pool
}

func NewOrgRiskRepository(db *pgxpool.Pool) OrgRiskRepository {
	return &orgRiskRepository{db: db}
}

func (r *orgRiskRepository) Get(ctx context.Context, organizationID uuid.UUID) (*models.OrganizationRisk, error) {
	var risk models.OrganizationRisk
	err := r.db.QueryRow(ctx, `
		SELECT id, risk_state, risk_score, risk_reason, risk_signals, risk_evaluated_at
		FROM organizations
		WHERE id = $1
	`, organizationID).Scan(
		&risk.OrganizationID,
		&risk.State,
		&risk.Score,
		&risk.Reason,
		&risk.Signals,
		&risk.EvaluatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return &risk, err
}

func (r *orgRiskRepository) RecordSignal(ctx context.Context, organizationID uuid.UUID, key string, signal models.OrganizationRiskSignal) (*models.OrganizationRisk, *models.OrganizationRisk, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	before := &models.OrganizationRisk{OrganizationID: organizationID}
	var raw []byte
	err = tx.QueryRow(ctx, `
		SELECT risk_state, risk_score, risk_reason, risk_signals, risk_evaluated_at
		FROM organizations
		WHERE id = $1
		FOR UPDATE
	`, organizationID).Scan(&before.State, &before.Score, &before.Reason, &raw, &before.EvaluatedAt)
	if err != nil {
		return nil, nil, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &before.Signals); err != nil {
			return nil, nil, fmt.Errorf("decode organization risk signals: %w", err)
		}
	}
	if before.Signals == nil {
		before.Signals = map[string]models.OrganizationRiskSignal{}
	}

	signal.Score = max(0, min(signal.Score, 100))
	if signal.ObservedAt.IsZero() {
		signal.ObservedAt = time.Now().UTC()
	}
	after := &models.OrganizationRisk{
		OrganizationID: organizationID,
		Signals:        make(map[string]models.OrganizationRiskSignal, len(before.Signals)+1),
	}
	for name, current := range before.Signals {
		after.Signals[name] = current
	}
	after.Signals[key] = signal
	for _, current := range after.Signals {
		after.Score += current.Score
	}
	after.Score = min(after.Score, 100)
	after.State = models.OrganizationRiskStateForScore(after.Score)
	after.Reason = signal.Reason
	evaluatedAt := time.Now().UTC()
	after.EvaluatedAt = &evaluatedAt

	encoded, err := json.Marshal(after.Signals)
	if err != nil {
		return nil, nil, err
	}
	_, err = tx.Exec(ctx, `
		UPDATE organizations
		SET risk_state = $2,
		    risk_score = $3,
		    risk_reason = $4,
		    risk_signals = $5,
		    risk_evaluated_at = $6,
		    updated_at = now()
		WHERE id = $1
	`, organizationID, after.State, after.Score, after.Reason, encoded, evaluatedAt)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return before, after, nil
}

func (r *orgRiskRepository) ListCorrelationFindings(ctx context.Context) ([]OrgRiskFinding, error) {
	findings := make([]OrgRiskFinding, 0)
	queries := []struct {
		key    string
		score  int
		reason string
		query  string
	}{
		{
			key:    "shared_signup_ip",
			score:  25,
			reason: "Multiple organizations share a signup IP address",
			query: `
				SELECT o.id, host(u.signup_ip), count(*) OVER (PARTITION BY u.signup_ip)
				FROM organizations o
				JOIN users u ON u.id = o.owner_user_id
				WHERE u.signup_ip IS NOT NULL
				  AND u.created_at >= now() - interval '30 days'
				  AND (SELECT count(DISTINCT o2.id) FROM organizations o2 JOIN users u2 ON u2.id = o2.owner_user_id WHERE u2.signup_ip = u.signup_ip) >= 3
			`,
		},
		{
			key:    "shared_signup_asn",
			score:  15,
			reason: "Signup velocity is concentrated in one network",
			query: `
				SELECT o.id, u.signup_asn::text, count(*) OVER (PARTITION BY u.signup_asn)
				FROM organizations o
				JOIN users u ON u.id = o.owner_user_id
				WHERE u.signup_asn IS NOT NULL
				  AND u.created_at >= now() - interval '7 days'
				  AND (SELECT count(DISTINCT o2.id) FROM organizations o2 JOIN users u2 ON u2.id = o2.owner_user_id WHERE u2.signup_asn = u.signup_asn AND u2.created_at >= now() - interval '7 days') >= 5
			`,
		},
		{
			key:    "shared_payment_fingerprint",
			score:  35,
			reason: "Multiple organizations share a payment fingerprint",
			query: `
				SELECT s.organization_id, s.payment_fingerprint, count(*) OVER (PARTITION BY s.payment_fingerprint)
				FROM subscriptions s
				WHERE s.organization_id IS NOT NULL
				  AND s.payment_fingerprint <> ''
				  AND (SELECT count(DISTINCT s2.organization_id) FROM subscriptions s2 WHERE s2.payment_fingerprint = s.payment_fingerprint) >= 2
			`,
		},
		{
			key:    "snowshoe_domains",
			score:  30,
			reason: "Many fresh domains are sending just below mailbox caps",
			query: `
				WITH recent AS (
					SELECT ea.organization_id,
					       split_part(lower(ea.email), '@', 2) AS domain,
					       count(t.id) AS sends
					FROM email_accounts ea
					LEFT JOIN tasks t ON t.email_account_id = ea.id
					  AND t.task_type = 'campaign'
					  AND t.status = 'completed'
					  AND t.completed_at >= now() - interval '7 days'
					WHERE ea.organization_id IS NOT NULL
					  AND ea.created_at >= now() - interval '30 days'
					GROUP BY ea.organization_id, domain
				), flagged AS (
					SELECT organization_id, count(*) AS domains, sum(sends) AS sends
					FROM recent
					WHERE sends BETWEEN 5 AND 49
					GROUP BY organization_id
					HAVING count(*) >= 5
				)
				SELECT organization_id, domains::text, sends FROM flagged
			`,
		},
	}

	for _, q := range queries {
		rows, err := r.db.Query(ctx, q.query)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var orgID uuid.UUID
			var value string
			var count int
			if err := rows.Scan(&orgID, &value, &count); err != nil {
				rows.Close()
				return nil, err
			}
			findings = append(findings, OrgRiskFinding{
				OrganizationID: orgID,
				Key:            q.key,
				Score:          q.score,
				Reason:         q.reason,
				Evidence:       map[string]any{"value": value, "cluster_size": count},
			})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return findings, nil
}

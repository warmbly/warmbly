package seed

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedWorkers(ctx context.Context, pool *pgxpool.Pool, r *Result) error {
	type worker struct {
		id       uuid.UUID
		name     string
		ipAddr   string
		notes    string
		wType    string
		freeTier bool
	}
	workers := []worker{
		{id: WorkerFreeID, name: "worker-free-1", ipAddr: "10.0.0.11", wType: "shared", freeTier: true, notes: "Free-tier shared worker"},
		{id: WorkerSharedID, name: "worker-shared-1", ipAddr: "10.0.0.12", wType: "shared", freeTier: false, notes: "Premium shared worker"},
		{id: WorkerDedicatedID, name: "worker-dedicated-1", ipAddr: "10.0.0.13", wType: "dedicated", freeTier: false, notes: "Reserved for enterprise org"},
	}

	for _, w := range workers {
		_, err := pool.Exec(ctx, `
			INSERT INTO workers (id, name, notes, ip_addr, active, worker_type, account_count, free_tier, created_at, updated_at)
			VALUES ($1,$2,$3,$4,TRUE,$5,0,$6,$7,$7)
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				notes = EXCLUDED.notes,
				ip_addr = EXCLUDED.ip_addr,
				active = TRUE,
				worker_type = EXCLUDED.worker_type,
				free_tier = EXCLUDED.free_tier,
				updated_at = NOW()
		`, w.id, w.name, w.notes, w.ipAddr, w.wType, w.freeTier, time.Now())
		if err != nil {
			return err
		}
		tier := "premium"
		if w.freeTier {
			tier = "free"
		}
		r.Workers = append(r.Workers, SeededWorker{Name: w.name, Tier: tier, Type: w.wType, ID: w.id.String()})
	}
	return nil
}

func seedWorkerAssignments(ctx context.Context, pool *pgxpool.Pool, _ *Result) error {
	// Acme (Pro plan) gets the dedicated worker. Use a stable assignment ID so
	// re-running the seed updates rather than duplicates.
	assignmentID := uuid.MustParse("00000000-0000-0000-0000-000000000280")
	_, err := pool.Exec(ctx, `
		INSERT INTO dedicated_worker_assignments (id, worker_id, user_id, subscription_id, assigned_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (id) DO NOTHING
	`, assignmentID, WorkerDedicatedID, UserOwnerID, SubAcmeID)
	return err
}

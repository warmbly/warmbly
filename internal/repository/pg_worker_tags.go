package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// GetWorkerTags returns the sorted set of tags on a single worker.
func (r *workerRepository) GetWorkerTags(ctx context.Context, workerID uuid.UUID) ([]string, error) {
	rows, err := r.db.Query(ctx, `SELECT tag FROM worker_tags WHERE worker_id = $1 ORDER BY tag`, workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetWorkerTags replaces the entire tag set for a worker. Done in a
// single transaction so the row briefly seeing only a subset of the new
// tags is impossible. Callers are expected to lowercase + sanitise tags
// before calling; the CHECK constraint on the column will reject anything
// invalid.
func (r *workerRepository) SetWorkerTags(ctx context.Context, workerID uuid.UUID, tags []string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM worker_tags WHERE worker_id = $1`, workerID); err != nil {
		return err
	}
	for _, t := range tags {
		if _, err := tx.Exec(ctx, `
			INSERT INTO worker_tags (worker_id, tag) VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, workerID, t); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ListAllWorkerTags returns every distinct tag in use across the fleet,
// sorted. Used by the UI for tag autocomplete.
func (r *workerRepository) ListAllWorkerTags(ctx context.Context) ([]string, error) {
	rows, err := r.db.Query(ctx, `SELECT DISTINCT tag FROM worker_tags ORDER BY tag`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// HydrateWorkerTags fills in the Tags field on a slice of workers in a
// single round-trip. Used after ListWorkersDetail so the list page can
// render tags without N+1 queries.
func (r *workerRepository) HydrateWorkerTags(ctx context.Context, workers []*models.Worker) error {
	if len(workers) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(workers))
	idx := make(map[uuid.UUID]int, len(workers))
	for i, w := range workers {
		ids[i] = w.ID
		idx[w.ID] = i
		w.Tags = nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT worker_id, tag FROM worker_tags
		WHERE worker_id = ANY($1)
		ORDER BY worker_id, tag
	`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var wid uuid.UUID
		var t string
		if err := rows.Scan(&wid, &t); err != nil {
			return err
		}
		if i, ok := idx[wid]; ok {
			workers[i].Tags = append(workers[i].Tags, t)
		}
	}
	return rows.Err()
}

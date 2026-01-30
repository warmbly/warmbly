package repository

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

type ServersRepository interface {
	GetWorkers(ctx context.Context) ([]models.Worker, *errx.Error)
	UpdateWorker(ctx context.Context, id string, data *models.UpdateWorker) (*models.Worker, *errx.Error)
	DeleteWorker(ctx context.Context, id string) *errx.Error
}

type serversRepository struct {
	DB *db.DB
}

func NewServersRepostory(db *db.DB) CampaignRepository {
	return &campaignRepository{
		DB: db,
	}
}

func (r *serversRepository) GetWorkers(ctx context.Context) ([]models.Worker, *errx.Error) {
	query := `
		SELECT id, ip_addr,
		 active, created_at, updated_at
		FROM workers
		ORDER BY created_at DESC
	`
	rows, err := r.DB.Query(ctx, query)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	var workers []models.Worker

	for rows.Next() {
		var w models.Worker
		if err := rows.Scan(
			&w.ID, &w.IPAddr,
			&w.Active, &w.CreatedAt, &w.UpdatedAt,
		); err != nil {
			sentry.CaptureException(err)
			return nil, errx.InternalError()
		}

		workers = append(workers, w)
	}

	return workers, nil
}

func (r *serversRepository) UpdateWorker(ctx context.Context, id string, data *models.UpdateWorker) (*models.Worker, *errx.Error) {
	setClauses := []string{}
	args := []any{id}
	argPos := 2
	if data.IPAddr != nil {
		ip := net.ParseIP(*data.IPAddr)
		if ip == nil {
			return nil, errx.ErrIPAddr
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "ip_addr", argPos))
		args = append(args, *data.IPAddr)
		argPos++
	}
	if data.Active != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "active", argPos))
		args = append(args, *data.Active)
		argPos++
	}

	setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "updated_at", argPos))
	args = append(args, time.Now())
	argPos++

	query := fmt.Sprintf(`
		UPDATE workers
		SET %s
		WHERE id = $1
		RETURNING id, ip_addr, active, updated_at, created_at
	`, strings.Join(setClauses, ", "))

	var w models.Worker

	if err := r.DB.QueryRow(
		ctx,
		query,
		args...,
	).Scan(&w.ID, &w.IPAddr, &w.Active, &w.UpdatedAt, &w.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	return &w, nil
}

func (r *serversRepository) DeleteWorker(ctx context.Context, id string) *errx.Error {
	query := `
		DELETE FROM workers
		WHERE id = $1
	`

	params := []any{
		id,
	}
	cmd, err := r.DB.Exec(
		ctx,
		query,
		params...,
	)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}
	if cmd.RowsAffected() == 0 {
		return errx.ErrNotEnough
	}

	return nil
}

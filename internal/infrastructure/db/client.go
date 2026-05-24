package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	*pgxpool.Pool
}

const (
	// MaxConns sizing rationale: 4 was catastrophically low — a single
	// leaked tx in a request handler is enough to deadlock the entire
	// backend (e.g. /auth/refresh blocks waiting for a connection, the
	// frontend treats the resulting timeout as session expiry, user is
	// kicked at the 10-minute refresh boundary). 25 leaves headroom even
	// under bursty admin pages while staying well under postgres'
	// default max_connections=100.
	defaultMaxConns          = int32(25)
	defaultMinConns          = int32(2)
	defaultMaxConnLifetime   = time.Hour
	defaultMaxConnIdleTime   = time.Minute * 30
	defaultHealthCheckPeriod = time.Minute
	defaultConnectTimeout    = time.Second * 5

	// Postgres idle-in-transaction safety net. If a code path forgets
	// `defer tx.Rollback(ctx)`, the server will abort the leaked tx
	// after this many milliseconds (5 min) instead of holding the
	// connection forever. Belt-and-suspenders against the bug class
	// that caused the 10-minute logout.
	idleInTxnTimeoutMs = "300000"
	// Statement-level safety net for query runaway. 60s should be more
	// than enough for any user-facing query; admin reports that need
	// longer can override with SET LOCAL statement_timeout.
	statementTimeoutMs = "60000"
)

func New(ctx context.Context, endpoint string) (*DB, error) {
	dbConfig, err := pgxpool.ParseConfig(endpoint)
	if err != nil {
		return nil, err
	}

	dbConfig.MaxConns = defaultMaxConns
	dbConfig.MinConns = defaultMinConns
	dbConfig.MaxConnLifetime = defaultMaxConnLifetime
	dbConfig.MaxConnIdleTime = defaultMaxConnIdleTime
	dbConfig.HealthCheckPeriod = defaultHealthCheckPeriod
	dbConfig.ConnConfig.ConnectTimeout = defaultConnectTimeout

	if dbConfig.ConnConfig.RuntimeParams == nil {
		dbConfig.ConnConfig.RuntimeParams = map[string]string{}
	}
	dbConfig.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = idleInTxnTimeoutMs
	dbConfig.ConnConfig.RuntimeParams["statement_timeout"] = statementTimeoutMs

	conn, err := pgxpool.NewWithConfig(ctx, dbConfig)
	if err != nil {
		return nil, err
	}

	return &DB{
		Pool: conn,
	}, nil
}

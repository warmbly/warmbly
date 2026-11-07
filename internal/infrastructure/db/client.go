package db

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

type DB struct {
	*pgxpool.Pool
	directConfig *pgx.ConnConfig
	pooled       bool

	sessionsOnce sync.Once
	sessions     *pgxpool.Pool
	sessionsErr  error
}

// sessionLockConns bounds the direct connections one process holds for session-scoped locks.
const sessionLockConns = 4

// SessionLocks is a small pool of direct connections for locks held while pooled work runs,
// so a held lock never occupies one of the pooler's server connections. It is nil without a pooler.
func (d *DB) SessionLocks() (*pgxpool.Pool, error) {
	if !d.pooled || d.directConfig == nil {
		return nil, nil
	}
	d.sessionsOnce.Do(func() {
		cfg, err := pgxpool.ParseConfig("")
		if err != nil {
			d.sessionsErr = err
			return
		}
		cfg.ConnConfig = d.directConfig.Copy()
		cfg.MaxConns, cfg.MinConns = sessionLockConns, 0
		cfg.MaxConnIdleTime = defaultMaxConnIdleTime
		d.sessions, d.sessionsErr = pgxpool.NewWithConfig(context.Background(), cfg)
	})
	return d.sessions, d.sessionsErr
}

// Close closes the session-lock pool with the main one.
func (d *DB) Close() {
	d.sessionsOnce.Do(func() {})
	if d.sessions != nil {
		d.sessions.Close()
	}
	d.Pool.Close()
}

const (
	// MaxConns sizing rationale: 4 was catastrophically low — a single
	// leaked tx in a request handler is enough to deadlock the entire
	// backend (e.g. /auth/refresh blocks waiting for a connection, the
	// frontend treats the resulting timeout as session expiry, user is
	// kicked at the 10-minute refresh boundary). 25 leaves headroom even
	// under bursty admin pages. It is an upper bound and not a target:
	// the clamp below lowers it to what the server can actually serve,
	// and observed concurrency is a small fraction of either.
	defaultMaxConns = int32(25)
	defaultMinConns = int32(2)
	// How long a connection may sit unused before the pool gives it back.
	// Thirty minutes meant nothing was ever trimmed, so the pool kept every
	// connection it had ever needed at once; on a 76-connection server two
	// processes held 65 between them while exactly one was doing work. A
	// minute keeps a connection across a burst and returns it before the next
	// service needs the slot.
	defaultMaxConnLifetime   = time.Hour
	defaultMaxConnIdleTime   = time.Minute
	defaultHealthCheckPeriod = time.Minute
	defaultConnectTimeout    = time.Second * 5

	// How many pooled processes one database is assumed to carry: backend and
	// consumer, each counted twice because a rolling deploy runs the outgoing
	// and incoming container at once. This is a per-process best effort, not a
	// fleet-wide allocation: nothing here knows how many clients the database
	// really has, so a deployment with more than four sets DB_MAX_CONNS itself.
	// Used only to lower a pool the server cannot honour, never to raise one.
	serverShareDivisor = int32(4)
	// Below this the share is too small to serve a process comfortably, and
	// the operator is told. It is a warning threshold and nothing else: raising
	// the pool to meet it would hand out capacity the server does not have,
	// which is the exhaustion this exists to prevent.
	lowCapacityWarnBelow = int32(10)

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
	return NewWithDirect(ctx, endpoint, os.Getenv("PRIMARY_DB_DIRECT"))
}

// NewWithDirect keeps session-scoped operations separate from transaction pooling.
func NewWithDirect(ctx context.Context, endpoint, directEndpoint string) (*DB, error) {
	dbConfig, directConfig, pooled, err := connectionConfigs(endpoint, directEndpoint)
	if err != nil {
		return nil, err
	}

	// The pool ceiling is per process, and the database's is for the whole
	// fleet: every backend replica, every consumer and every warmblyctl run
	// takes its own share. Exhausting max_connections shows up as "remaining
	// connection slots are reserved", which no amount of retrying fixes, so
	// the size has to be tunable without a rebuild. A DSN that names
	// pool_max_conns keeps it; DB_MAX_CONNS overrides both.
	dbConfig.MaxConns = poolCeiling(endpoint, dbConfig.MaxConns)
	dbConfig.MinConns = defaultMinConns
	if n := envInt32("DB_MIN_CONNS"); n >= 0 && os.Getenv("DB_MIN_CONNS") != "" {
		dbConfig.MinConns = n
	}
	if dbConfig.MinConns > dbConfig.MaxConns {
		dbConfig.MinConns = dbConfig.MaxConns
	}

	// A constant cannot know the server's ceiling, and the same 25 that is
	// generous on a large instance is fatal on a small one: backend plus
	// consumer, doubled by a deploy overlap, asked for more than the server
	// had and every job in that second failed with "remaining connection
	// slots are reserved". Ask the server instead. The probe costs one
	// connection at boot and can only lower the pool.
	if share := connectionShare(ctx, dbConfig.ConnConfig, pooled); share > 0 && share < dbConfig.MaxConns {
		log.Warn().
			Int32("configured_max_conns", dbConfig.MaxConns).
			Int32("clamped_max_conns", share).
			Msg("db: pool clamped to this process's share of the server's max_connections; raise the server's limit or set DB_MAX_CONNS to size it yourself")
		dbConfig.MaxConns = share
		if dbConfig.MinConns > dbConfig.MaxConns {
			dbConfig.MinConns = dbConfig.MaxConns
		}
	}
	dbConfig.MaxConnLifetime = defaultMaxConnLifetime
	dbConfig.MaxConnIdleTime = defaultMaxConnIdleTime
	dbConfig.HealthCheckPeriod = defaultHealthCheckPeriod
	dbConfig.ConnConfig.ConnectTimeout = defaultConnectTimeout

	// statement_timeout does not cover the wait for a connection; this reports that wait.
	if dbConfig.ConnConfig.Tracer == nil {
		dbConfig.ConnConfig.Tracer = &acquireTracer{}
	}

	conn, err := pgxpool.NewWithConfig(ctx, dbConfig)
	if err != nil {
		return nil, err
	}
	if pooled {
		probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if err := conn.Ping(probeCtx); err != nil {
			conn.Close()
			return nil, errors.New("transaction-pooled database connection failed; verify connectivity, role timeouts and pooler configuration")
		}
	}

	return &DB{
		Pool:         conn,
		directConfig: directConfig,
		pooled:       pooled,
	}, nil
}

// ConnectSession opens a direct connection whose close releases session locks.
func (d *DB) ConnectSession(ctx context.Context) (*pgx.Conn, error) {
	cfg := d.directConfig
	if cfg == nil {
		cfg = d.Config().ConnConfig
	}
	return pgx.ConnectConfig(ctx, cfg.Copy())
}

func connectionShare(ctx context.Context, cfg *pgx.ConnConfig, pooled bool) int32 {
	if pooled {
		return 0
	}
	return serverConnectionShare(ctx, cfg)
}

// poolCeiling is the pool size to ask for before the server is consulted.
//
// The default here was unreachable for as long as it has existed. ParseConfig
// fills MaxConns with pgxpool's own default, max(4, NumCPU), so it is never
// zero or less and the `<= 0` guard that was meant to apply `defaultMaxConns`
// never fired once. On Railway's 48-core container hosts that quietly made the
// real ceiling 48 per process, not the 25 the constant and the docs both
// promised, and two of those against a 76-connection server is where the
// exhaustion came from. A machine's core count says nothing about what the
// database can serve, so it is honoured only when the DSN asks for it by name.
func poolCeiling(endpoint string, parsed int32) int32 {
	ceiling := parsed
	if !strings.Contains(endpoint, "pool_max_conns") || ceiling <= 0 {
		ceiling = defaultMaxConns
	}
	if n := envInt32("DB_MAX_CONNS"); n > 0 {
		ceiling = n
	}
	return ceiling
}

// processShare is one process's cut of the connections the server leaves
// unreserved. It never returns more than that cut: rounding it up to something
// comfortable would hand out capacity the server does not have, which is the
// exhaustion this exists to prevent. One is the floor only because a pool of
// zero cannot serve anything.
func processShare(usable int32) int32 {
	share := usable / serverShareDivisor
	if share < 1 {
		return 1
	}
	return share
}

// serverConnectionShare reports the largest pool this process may take from
// the server, or 0 when the server cannot be asked. Boot must not depend on
// the answer: a probe that fails leaves the configured size in place.
func serverConnectionShare(ctx context.Context, cfg *pgx.ConnConfig) int32 {
	probeCtx, cancel := context.WithTimeout(ctx, defaultConnectTimeout)
	defer cancel()

	conn, err := pgx.ConnectConfig(probeCtx, cfg)
	if err != nil {
		log.Debug().Err(err).Msg("db: could not probe max_connections; keeping the configured pool size")
		return 0
	}
	defer func() { _ = conn.Close(context.WithoutCancel(probeCtx)) }()

	// Both reservations are subtracted because neither is ours to spend: the
	// superuser slots are what the error names, and RDS keeps its own on top.
	// current_setting's second argument suppresses the error for the RDS-only
	// name so the same query works on stock Postgres.
	const q = `SELECT current_setting('max_connections')::int
		- current_setting('superuser_reserved_connections')::int
		- COALESCE(NULLIF(current_setting('rds.rds_superuser_reserved_connections', true), '')::int, 0)`
	var usable int32
	if err := conn.QueryRow(probeCtx, q).Scan(&usable); err != nil {
		log.Debug().Err(err).Msg("db: could not read max_connections; keeping the configured pool size")
		return 0
	}
	if usable <= 0 {
		return 0
	}

	share := processShare(usable)
	if share < lowCapacityWarnBelow {
		log.Warn().
			Int32("usable_server_connections", usable).
			Int32("process_share", share).
			Msg("db: the server has too few connections for the fleet; services will contend for slots until its max_connections is raised")
	}
	return share
}

// envInt32 reads a positive pool bound from the environment. Anything unset or
// unparseable returns -1 so the caller keeps its own default rather than
// sizing the pool from a typo.
func envInt32(name string) int32 {
	raw := os.Getenv(name)
	if raw == "" {
		return -1
	}
	n, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || n < 0 {
		return -1
	}
	return int32(n)
}

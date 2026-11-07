// Package leader runs loops on one process of a role at a time. The leader
// holds a session advisory lock on a direct connection and keeps it until the
// process stops or the connection breaks, so loops that keep state in memory
// stay on the same replica instead of moving every tick.
package leader

import (
	"context"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"
)

// lockClass is the advisory-lock namespace; 266 is the cloud-link reconciliation lock.
const lockClass = 267

const (
	defaultRetry = 30 * time.Second
	defaultPing  = 10 * time.Second
	dialTimeout  = 10 * time.Second
)

// Sessioner opens a connection whose close releases its session locks; *db.DB implements it.
type Sessioner interface {
	ConnectSession(ctx context.Context) (*pgx.Conn, error)
}

type loop struct {
	name string
	fn   func(ctx context.Context)
}

// Elector runs the loops registered with Go while this process holds the role's lock.
type Elector struct {
	db      Sessioner
	role    string
	key     int32
	retry   time.Duration
	ping    time.Duration
	mu      sync.Mutex
	loops   []loop
	running bool
	leading atomic.Bool
}

// New returns an elector for role, one lock per role (backend, consumer).
func New(db Sessioner, role string) *Elector {
	h := fnv.New32a()
	_, _ = h.Write([]byte(role))
	return &Elector{db: db, role: role, key: int32(h.Sum32() & 0x7fffffff), retry: defaultRetry, ping: defaultPing}
}

// Go registers a loop to run while leading. fn must return when its ctx ends;
// it is started again whenever this process wins the lock again.
func (e *Elector) Go(name string, fn func(ctx context.Context)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running {
		panic("leader: Go after Run")
	}
	e.loops = append(e.loops, loop{name: name, fn: fn})
}

// Leading reports whether this process holds the lock now.
func (e *Elector) Leading() bool { return e.leading.Load() }

// Run competes for the lock until ctx ends, running the registered loops while it holds it.
func (e *Elector) Run(ctx context.Context) {
	e.mu.Lock()
	e.running = true
	loops := append([]loop(nil), e.loops...)
	e.mu.Unlock()
	if len(loops) == 0 {
		return
	}
	for {
		if conn := e.acquire(ctx); conn != nil {
			e.lead(ctx, conn, loops)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(e.retry):
		}
	}
}

// acquire returns the lock-holding connection, or nil when another process leads.
func (e *Elector) acquire(ctx context.Context) *pgx.Conn {
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	conn, err := e.db.ConnectSession(dialCtx)
	if err != nil {
		if ctx.Err() == nil {
			log.Warn().Err(err).Str("role", e.role).Msg("leader: could not open the lock connection")
		}
		return nil
	}
	var won bool
	if err := conn.QueryRow(dialCtx, `SELECT pg_try_advisory_lock($1, $2)`, lockClass, e.key).Scan(&won); err != nil || !won {
		if err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Str("role", e.role).Msg("leader: lock attempt failed")
		}
		closeConn(ctx, conn)
		return nil
	}
	return conn
}

// lead runs the loops until ctx ends or the lock connection stops answering.
func (e *Elector) lead(ctx context.Context, conn *pgx.Conn, loops []loop) {
	leadCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	e.leading.Store(true)
	log.Info().Str("role", e.role).Int("loops", len(loops)).Msg("leader: this process now runs the single-replica loops")
	for _, l := range loops {
		wg.Add(1)
		go func(l loop) {
			defer wg.Done()
			l.fn(leadCtx)
		}(l)
	}

	ticker := time.NewTicker(e.ping)
	defer ticker.Stop()
	for leadCtx.Err() == nil {
		select {
		case <-leadCtx.Done():
		case <-ticker.C:
			pingCtx, done := context.WithTimeout(leadCtx, e.ping)
			_, err := conn.Exec(pingCtx, `SELECT 1`)
			done()
			if err != nil && ctx.Err() == nil {
				log.Warn().Err(err).Str("role", e.role).Msg("leader: lock connection lost; stopping the single-replica loops")
				cancel()
			}
		}
	}
	cancel()
	wg.Wait()
	e.leading.Store(false)
	closeConn(ctx, conn)
}

func closeConn(ctx context.Context, conn *pgx.Conn) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = conn.Close(closeCtx)
}

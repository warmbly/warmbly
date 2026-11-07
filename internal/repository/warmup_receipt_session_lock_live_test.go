package repository

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

func receiptPool(t *testing.T, ctx context.Context, conns int32) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(os.Getenv("WARMBLY_TEST_DB"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns, cfg.MinConns = conns, 0
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// One pooled connection stands in for a pooler whose server connections are all taken.
func TestLiveWarmupReceiptSessionLocksLeaveThePooledConnectionsToAcceptance(t *testing.T) {
	f, _ := lineageFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	pool := receiptPool(t, ctx, 1)
	r := NewWarmupRepository(pool).(*warmupRepository)
	r.UseSessionLocks(receiptPool(t, ctx, 4))
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for range 16 {
		token := uuid.New()
		if _, err := f.pool.Exec(ctx, `INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id)VALUES($1,$2,$3,$4)`, token, f.task, f.sender, f.recipient); err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := r.ProcessWarmupReceipt(ctx, token, f.recipient, WarmupReceiptProof{SenderAddress: f.senderTo}, func(current *models.WarmupToken) error {
				var value int
				if err := pool.QueryRow(ctx, `SELECT 1`).Scan(&value); err != nil {
					return err
				}
				accepted.Add(1)
				return r.ConsumeWarmupToken(ctx, current.Token)
			})
			if err != nil || !ok {
				t.Errorf("receipt: ok=%v err=%v", ok, err)
			}
		}()
	}
	wg.Wait()
	if n := accepted.Load(); n != 16 {
		t.Fatalf("accepted %d of 16 receipts", n)
	}
	var unlocked bool
	if err := f.pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory')`).Scan(&unlocked); err != nil || !unlocked {
		t.Fatalf("advisory locks left behind: err=%v", err)
	}
}

// Old consumers lock receipts in a transaction and new ones on a session; during a rollout each must exclude the other.
func TestLiveWarmupReceiptSessionAndTransactionLocksExcludeEachOther(t *testing.T) {
	f, _ := lineageFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	token := uuid.New()
	if _, err := f.pool.Exec(ctx, `INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id)VALUES($1,$2,$3,$4)`, token, f.task, f.sender, f.recipient); err != nil {
		t.Fatal(err)
	}
	pool := receiptPool(t, ctx, 4)
	session := NewWarmupRepository(pool).(*warmupRepository)
	session.UseSessionLocks(receiptPool(t, ctx, 2))
	transactional := NewWarmupRepository(pool).(*warmupRepository)
	proof := WarmupReceiptProof{SenderAddress: f.senderTo}
	never := func(*models.WarmupToken) error { t.Error("accepted while the other path held the lock"); return nil }

	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('warmup_receipt_' || $1::text))`, token); err != nil {
		t.Fatal(err)
	}
	if _, err := session.ProcessWarmupReceipt(ctx, token, f.recipient, proof, never); !errors.Is(err, ErrWarmupReceiptBusy) {
		t.Fatalf("session path under a transaction lock: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := session.ProcessWarmupReceipt(ctx, token, f.recipient, proof, func(*models.WarmupToken) error {
			close(held)
			<-release
			return nil
		})
		done <- err
	}()
	<-held
	if _, err := transactional.ProcessWarmupReceipt(ctx, token, f.recipient, proof, never); !errors.Is(err, ErrWarmupReceiptBusy) {
		t.Fatalf("transaction path under a session lock: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

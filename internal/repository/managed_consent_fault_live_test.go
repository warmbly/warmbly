package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveManagedOperationClaimSerializesAcrossReplicasWithOnePoolConnection(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx := context.Background()
	cfg := f.pool.Config()
	cfg.MaxConns = 1
	cfg.MinConns = 0
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	r := NewPoolLinkRepository(pool)
	mr := r.(PoolLinkManagedRepository)
	i := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: f.org}
	if err := r.CreateInstance(ctx, i, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	remote := uuid.New()
	op := &models.PoolLinkManagedOperation{OrganizationID: f.org, InstanceID: &i.ID, RemoteID: &remote, Kind: "oauth", Provider: models.InboxProviderGoogle, PlannedAccountID: &f.sender, ExpiresAt: time.Now().Add(time.Minute)}
	if err := mr.CreateManagedOperation(ctx, op); err != nil {
		t.Fatal(err)
	}
	var claimed atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- mr.WithManagedOperationLock(ctx, i.ID, func() error {
				ok, err := mr.ClaimManagedOperation(ctx, i.ID, remote)
				if ok {
					claimed.Add(1)
				}
				return err
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if claimed.Load() != 1 {
		t.Fatalf("provider exchange admitted %d times", claimed.Load())
	}
}

func TestLiveManagedHistoryDowngradeRefusesPendingCompletedAndRevokedHistory(t *testing.T) {
	f := newPoolLinkFixture(t)
	ctx := context.Background()
	r := NewPoolLinkRepository(f.pool)
	mr := r.(PoolLinkManagedRepository)
	i := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: f.org}
	if err := r.CreateInstance(ctx, i, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET provider='gmail',auth_method='oauth' WHERE id=$1`, f.sender); err != nil {
		t.Fatal(err)
	}
	remote := uuid.New()
	op := &models.PoolLinkManagedOperation{OrganizationID: f.org, InstanceID: &i.ID, RemoteID: &remote, Kind: "adopt", Provider: models.InboxProviderGoogle, PlannedAccountID: &f.sender, ExpiresAt: time.Now().Add(time.Minute)}
	if err := mr.CreateManagedOperation(ctx, op); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob("../infrastructure/db/migrations/*_cloud_trust_reconciliation.down.sql")
	if err != nil || len(paths) != 1 {
		t.Fatalf("migration paths=%v,err=%v", paths, err)
	}
	sql, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"pending", "completed", "revoked"} {
		if state == "completed" {
			if err := mr.CompleteManagedOperation(ctx, i.ID, remote, f.sender); err != nil {
				t.Fatal(err)
			}
		}
		if state == "revoked" {
			if err := mr.RevokeManagedOperations(ctx, i.ID, &remote); err != nil {
				t.Fatal(err)
			}
		}
		tx, err := f.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, string(sql))
		_ = tx.Rollback(ctx)
		if err == nil || !strings.Contains(err.Error(), "Managed consent history") {
			t.Fatalf("%s: unsafe downgrade=%v", state, err)
		}
		got, err := mr.GetManagedOperation(ctx, i.ID, remote)
		if err != nil || got == nil || got.State != state {
			t.Fatalf("%s: guard lost history=%+v,err=%v", state, got, err)
		}
	}
}

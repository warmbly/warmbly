package repository

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAdminPlatformOverviewReturnsSendCountError(t *testing.T) {
	pool, err := pgxpool.New(t.Context(), "postgres://test:test@127.0.0.1:1/test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	got, err := (&adminRepository{db: pool}).GetPlatformOverview(t.Context())
	if err == nil || got != nil {
		t.Fatalf("closed database: got %+v, %v; want an error, not zero counters", got, err)
	}
}

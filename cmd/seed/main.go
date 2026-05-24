package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/seed"
)

// seed is a standalone command that fills the development database with a
// rich, internally-consistent dataset so every product surface can be tested
// end-to-end without manually clicking through onboarding.
//
// It is idempotent: re-running against an already-seeded database is a no-op
// (or in-place upsert) for every entity, so the command is safe to run on
// every docker compose up.
func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	cfg, err := config.NewConfig(ctx)
	if err != nil {
		log.Fatalf("seed: config: %v", err)
	}

	endpoint, err := cfg.LoadPrimaryDBEndpoint(ctx)
	if err != nil {
		log.Fatalf("seed: load PRIMARY_DB: %v", err)
	}

	log.Println("seed: running migrations")
	if err := db.RunMigrations(endpoint); err != nil {
		log.Fatalf("seed: migrations: %v", err)
	}

	pg, err := db.New(ctx, endpoint)
	if err != nil {
		log.Fatalf("seed: connect db: %v", err)
	}
	defer pg.Close()

	log.Println("seed: writing data")
	result, err := seed.Run(ctx, pg.Pool)
	if err != nil {
		log.Fatalf("seed: %v", err)
	}

	result.Print(os.Stdout)
	log.Println("seed: done")
}

// Sandbox: a fully working local demo environment.
//
// Seeds a paid showcase org ("Sunrise Labs") whose mailboxes really send
// (SMTP -> mailpit) and really sync (IMAP <- dovecot), then runs a simulator
// that plays the internet: routing captured mail into recipient inboxes,
// opening tracking pixels, clicking tracked links, and replying as the seeded
// contacts. The platform side (scheduler, worker, consumer, tracking,
// realtime) is all production code; run it with `make infra` + `make run` +
// `make tracking` (+ `make realtime` + `make web` for the live dashboard).
//
//	make sandbox              # seed + simulate (foreground)
//	go run ./cmd/sandbox -seed-only
//	go run ./cmd/sandbox -simulate-only
//
// Documented at docs.warmbly.com/development/sandbox/.
package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/sandbox"
)

func main() {
	seedOnly := flag.Bool("seed-only", false, "seed the sandbox org and exit")
	simulateOnly := flag.Bool("simulate-only", false, "skip seeding, run only the simulator")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := sandbox.FromEnv()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	if !*simulateOnly {
		// Migrations first so the sandbox works on a fresh database.
		if err := db.RunMigrations(cfg.DatabaseURL); err != nil {
			log.Fatalf("migrations: %v", err)
		}
		seedCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
		if err := sandbox.Seed(seedCtx, pool, cfg); err != nil {
			cancel()
			log.Fatalf("seed: %v", err)
		}
		cancel()
	}
	if *seedOnly {
		return
	}

	if err := sandbox.Simulate(ctx, pool, cfg); err != nil {
		log.Fatalf("simulate: %v", err)
	}
}

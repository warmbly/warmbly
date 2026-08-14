// Command confenge is the local operator CLI for CONFENGE workspace bootstrap,
// preflight, import, and kill-switch control. It does not reimplement commercial
// intelligence; it wires existing confenge services and env configuration.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/app/confenge"
	"github.com/warmbly/warmbly/internal/app/confenge/dispatch"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/seed"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "preflight":
		os.Exit(cmdPreflight(os.Args[2:]))
	case "bootstrap":
		os.Exit(cmdBootstrap(os.Args[2:]))
	case "import":
		os.Exit(cmdImport(os.Args[2:]))
	case "reconcile-target-fit":
		os.Exit(cmdReconcileTargetFit(os.Args[2:]))
	case "stop-sending":
		os.Exit(cmdStopSending())
	case "resume-sending":
		os.Exit(cmdResumeSending())
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `confenge — local CONFENGE operator CLI

Usage:
  confenge preflight [--org-id UUID]
  confenge bootstrap [--org-id UUID]
  confenge import --feed PATH|file://... [--dry-run] [--org-id UUID]
  confenge reconcile-target-fit [--dry-run] [--org-id UUID]
  confenge stop-sending
  confenge resume-sending

Env: PRIMARY_DB, CONFENGE_*, REDIS, NATS_URL (see .env.confenge.example).
`)
}

func loadEnvFile(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
}

func maybeLoadDotEnv() {
	for _, p := range []string{".env.confenge", ".env"} {
		if _, err := os.Stat(p); err == nil {
			loadEnvFile(p)
		}
	}
}

func parseOrg(s string) uuid.UUID {
	if id, err := uuid.Parse(strings.TrimSpace(s)); err == nil {
		return id
	}
	return seed.DevOrgID
}

func cmdPreflight(args []string) int {
	maybeLoadDotEnv()
	fs := flag.NewFlagSet("preflight", flag.ExitOnError)
	orgStr := fs.String("org-id", seed.DevOrgID.String(), "organization UUID for mailbox probe")
	_ = fs.Parse(args)
	cfg := confenge.LoadConfig()
	rep := confenge.RunPreflight(cfg, confenge.DefaultPreflightDeps(parseOrg(*orgStr)))
	fmt.Print(confenge.FormatPreflight(rep))
	if !rep.OK {
		return 1
	}
	return 0
}

func cmdStopSending() int {
	maybeLoadDotEnv()
	if err := confenge.EngageKillSwitch(); err != nil {
		fmt.Fprintf(os.Stderr, "stop-sending: %v\n", err)
		return 1
	}
	fmt.Printf("Kill switch engaged: %s\n", confenge.KillSwitchPath())
	fmt.Println("Enroll/send paths refuse outbound until confenge resume-sending.")
	return 0
}

func cmdResumeSending() int {
	maybeLoadDotEnv()
	if err := confenge.ReleaseKillSwitch(); err != nil {
		fmt.Fprintf(os.Stderr, "resume-sending: %v\n", err)
		return 1
	}
	fmt.Println("Kill switch released. CONFENGE_SENDING_PAUSED env still applies if set.")
	return 0
}

func cmdBootstrap(args []string) int {
	maybeLoadDotEnv()
	fs := flag.NewFlagSet("bootstrap", flag.ExitOnError)
	orgStr := fs.String("org-id", seed.DevOrgID.String(), "organization UUID")
	_ = fs.Parse(args)
	cfg := confenge.LoadConfig()
	if !cfg.Enabled {
		fmt.Fprintln(os.Stderr, "CONFENGE_OUTREACH_ENABLED is false; set it true in .env.confenge")
		return 1
	}
	dsn := primaryDSN()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db: %v\n", err)
		return 1
	}
	defer pool.Close()

	orgID := parseOrg(*orgStr)
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM organizations WHERE id=$1)`, orgID).Scan(&exists); err != nil {
		fmt.Fprintf(os.Stderr, "org lookup: %v\n", err)
		return 1
	}
	if !exists {
		fmt.Fprintf(os.Stderr, "organization %s not found. Run make confenge-local (seed) first.\n", orgID)
		return 1
	}

	repo := repository.NewOutreachRepository(pool)
	settings, err := repo.GetOrgSettings(ctx, orgID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "settings: %v\n", err)
		return 1
	}
	if settings == nil {
		s := &models.OutreachOrgSettings{
			OrganizationID: orgID,
			CampaignName:   confenge.DefaultCampaignName,
		}
		if err := repo.UpsertOrgSettings(ctx, s); err != nil {
			fmt.Fprintf(os.Stderr, "upsert settings: %v\n", err)
			return 1
		}
		fmt.Println("Created outreach_org_settings for org", orgID)
	} else {
		fmt.Println("outreach_org_settings already present for org", orgID)
	}

	var mailboxCount int
	_ = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM email_accounts
		WHERE organization_id=$1 AND status='active'
	`, orgID).Scan(&mailboxCount)

	fmt.Println("CONFENGE workspace bootstrap summary")
	fmt.Println("  org_id:           ", orgID)
	fmt.Println("  outreach_enabled: ", cfg.Enabled)
	fmt.Println("  human_approval:   ", cfg.RequireHumanApproval)
	fmt.Println("  auto_send:        ", cfg.AutoSendEnabled)
	fmt.Println("  governor_hourly:  ", dispatch.LoadConfig().SendsPerHour)
	fmt.Println("  campaign_daily:   ", cfg.DefaultDailyLimit)
	fmt.Println("  feed_url:         ", cfg.FeedURL)
	fmt.Println("  active_mailboxes: ", mailboxCount)
	fmt.Println("  kill_switch:      ", !cfg.SendingAllowed())
	fmt.Println()
	fmt.Println("Próximo passo: make confenge-import FEED=internal/app/confenge/testdata/demo_3_companies.json")
	fmt.Println("Abra o painel dedicado em http://localhost:5173 (sem login)")
	return 0
}

func cmdImport(args []string) int {
	maybeLoadDotEnv()
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	feed := fs.String("feed", "", "path or file:// URI to confenge.outreach.v1 JSON")
	dry := fs.Bool("dry-run", false, "validate and count only")
	orgStr := fs.String("org-id", seed.DevOrgID.String(), "organization UUID")
	_ = fs.Parse(args)
	if strings.TrimSpace(*feed) == "" {
		// fall back to env feed
		*feed = strings.TrimSpace(os.Getenv(confenge.EnvFeedURL))
	}
	if strings.TrimSpace(*feed) == "" {
		fmt.Fprintln(os.Stderr, "usage: confenge import --feed PATH [--dry-run]")
		return 2
	}
	cfg := confenge.LoadConfig()
	if !cfg.Enabled {
		// allow local import tool even if flag was forgotten, but warn
		cfg.Enabled = true
		fmt.Fprintln(os.Stderr, "warning: enabling CONFENGE for this import process only")
	}
	uri := *feed
	if !strings.Contains(uri, "://") {
		abs, err := filepath.Abs(uri)
		if err != nil {
			fmt.Fprintf(os.Stderr, "feed path: %v\n", err)
			return 1
		}
		uri = "file://" + abs
	}

	dsn := primaryDSN()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db: %v\n", err)
		return 1
	}
	defer pool.Close()

	orgID := parseOrg(*orgStr)
	repo := repository.NewOutreachRepository(pool)
	svc := confenge.NewService(cfg, repo, nil)
	userID := seed.DevUserID
	run, xerr := svc.ImportFromURI(ctx, orgID, &userID, uri, confenge.ImportOptions{DryRun: *dry})
	if xerr != nil {
		fmt.Fprintf(os.Stderr, "import: %s\n", xerr.Error())
		return 1
	}
	fmt.Print(confenge.FormatImportSummary(run))
	return 0
}

func cmdReconcileTargetFit(args []string) int {
	maybeLoadDotEnv()
	fs := flag.NewFlagSet("reconcile-target-fit", flag.ExitOnError)
	dry := fs.Bool("dry-run", false, "report changes without writing")
	orgStr := fs.String("org-id", seed.DevOrgID.String(), "organization UUID")
	_ = fs.Parse(args)
	cfg := confenge.LoadConfig()
	cfg.Enabled = true
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, primaryDSN())
	if err != nil {
		fmt.Fprintf(os.Stderr, "db: %v\n", err)
		return 1
	}
	defer pool.Close()
	svc := confenge.NewService(cfg, repository.NewOutreachRepository(pool), nil)
	report, xerr := svc.ReconcileTargetFit(ctx, parseOrg(*orgStr), *dry)
	if xerr != nil {
		fmt.Fprintf(os.Stderr, "reconcile target-fit: %s\n", xerr.Error())
		return 1
	}
	b, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(b))
	return 0
}

func primaryDSN() string {
	dsn := strings.TrimSpace(os.Getenv("PRIMARY_DB"))
	if dsn == "" {
		return "postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable"
	}
	return dsn
}

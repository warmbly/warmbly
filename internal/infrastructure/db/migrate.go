package db

import (
	"embed"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/warmbly/warmbly/internal/observability/errs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrateConnectWait bounds how long boot waits for the server to accept the
// migration connection, e.g. while a rolling deploy's outgoing container still
// holds its slots.
const migrateConnectWait = 2 * time.Minute

// RunMigrations runs all pending database migrations.
// It uses embedded SQL files and is safe to call on every startup.
// Returns nil if migrations succeed or if there are no changes to apply.
func RunMigrations(databaseURL string) error {
	source, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("failed to create migration source: %w", err)
	}

	m, err := openMigrate(source, databaseURL)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", errs.Sensitive(err, "failed to open the migration connection"+connectReason(err)))
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		// A dropped connection names the database's address; a failed migration does not.
		var op *net.OpError
		if errors.As(err, &op) {
			err = errs.Sensitive(err, "lost the migration connection"+connectReason(err))
		}
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

func openMigrate(sourceDrv source.Driver, databaseURL string) (*migrate.Migrate, error) {
	delay := 2 * time.Second
	deadline := time.Now().Add(migrateConnectWait)
	for {
		m, err := migrate.NewWithSourceInstance("iofs", sourceDrv, databaseURL)
		if err == nil || !serverBusy(err) || time.Now().Add(delay).After(deadline) {
			return m, err
		}
		log.Printf("database refused the migration connection (%v); retrying in %s", err, delay)
		time.Sleep(delay)
		delay = min(delay*2, 15*time.Second)
	}
}

// serverBusy reports a refusal that clears on its own: no free connection
// slot (53300) or a server starting up or shutting down (57P03).
// connectReason names why the connection failed without the URL or address the driver's text carries.
func connectReason(err error) string {
	var pe interface{ SQLState() string }
	if errors.As(err, &pe) {
		return " (SQLSTATE " + pe.SQLState() + ")"
	}
	// A DNS error names the host and the resolver, so it is reduced to its kind.
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return ": dns lookup failed"
	}
	var sys *os.SyscallError
	if errors.As(err, &sys) {
		return ": " + sys.Error()
	}
	var op *net.OpError
	if errors.As(err, &op) {
		return ": " + op.Op + " failed"
	}
	return ""
}

func serverBusy(err error) bool {
	var pe interface{ SQLState() string }
	if !errors.As(err, &pe) {
		return false
	}
	switch pe.SQLState() {
	case "53300", "57P03":
		return true
	}
	return false
}

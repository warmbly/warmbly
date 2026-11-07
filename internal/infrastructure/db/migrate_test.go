package db

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Two migrations sharing a version make iofs.New fail, which takes the backend
// down at boot before it serves anything. Neither PR that introduces the
// collision is red on its own, so the assertion lives here where every Go CI
// run has to pass it.
func TestEmbeddedMigrationsLoad(t *testing.T) {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("embedded migrations do not load, the backend will exit at boot: %v", err)
	}
	defer src.Close()

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}

	ups := map[string]bool{}
	downs := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case strings.HasSuffix(name, ".up.sql"):
			ups[strings.TrimSuffix(name, ".up.sql")] = true
		case strings.HasSuffix(name, ".down.sql"):
			downs[strings.TrimSuffix(name, ".down.sql")] = true
		default:
			t.Errorf("%s is neither an .up.sql nor a .down.sql migration", name)
		}
	}

	for stem := range ups {
		if !downs[stem] {
			t.Errorf("%s.up.sql has no matching .down.sql, so a rollback stops there", stem)
		}
	}
	for stem := range downs {
		if !ups[stem] {
			t.Errorf("%s.down.sql has no matching .up.sql", stem)
		}
	}
}

type sqlStateErr string

func (e sqlStateErr) Error() string    { return "pq: " + string(e) }
func (e sqlStateErr) SQLState() string { return string(e) }

func TestServerBusy(t *testing.T) {
	cases := map[error]bool{
		fmt.Errorf("failed to open database: %w", sqlStateErr("53300")): true,
		sqlStateErr("57P03"):             true,
		sqlStateErr("28P01"):             false,
		sqlStateErr("42P01"):             false,
		errors.New("connection refused"): false,
	}
	for err, want := range cases {
		if got := serverBusy(err); got != want {
			t.Errorf("serverBusy(%v) = %v, want %v", err, got, want)
		}
	}
}

// The reason given for a failed migration connection names no host, address or resolver.
func TestConnectReasonLeavesOutHostsAndAddresses(t *testing.T) {
	dns := &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "db.internal.example", Server: "10.0.0.2:53", IsNotFound: true}}
	refused := &net.OpError{Op: "dial", Net: "tcp", Addr: &net.TCPAddr{IP: net.ParseIP("10.9.8.7"), Port: 5432}, Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("open: %w", dns), ": dns lookup failed"},
		{fmt.Errorf("open: %w", refused), ": connect: connection refused"},
	} {
		got := connectReason(tc.err)
		if got != tc.want {
			t.Errorf("connectReason(%v) = %q, want %q", tc.err, got, tc.want)
		}
		for _, leaked := range []string{"db.internal.example", "10.0.0.2", "10.9.8.7"} {
			if strings.Contains(got, leaked) {
				t.Errorf("reason %q carries %q", got, leaked)
			}
		}
	}
}

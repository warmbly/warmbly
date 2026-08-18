// warmblyctl is the operator CLI for a Warmbly instance. It answers "what
// state is this install in" and "how do I get back in" by talking to the
// database directly, so it keeps working when signing in does not.
//
// Authorization is container or host access, the same trust model as Sentry's
// createuser, Gitea's `admin user create` and authentik's `ak changepassword`.
// It must never grow an HTTP surface.
//
// It reads PRIMARY_DB and REDIS from the environment, which is why running it
// inside the backend container is the documented path: the environment there is
// already correct.
//
//	docker compose -p warmbly exec backend warmblyctl status
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	args := os.Args[1:]
	if len(args) == 0 {
		usage(os.Stderr)
		os.Exit(2)
	}

	if err := dispatch(ctx, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		// A failing check has already printed itself; repeating it as an error
		// line would bury the findings under the tool's own noise.
		if errors.Is(err, errChecksFailed) {
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func dispatch(ctx context.Context, args []string) error {
	switch args[0] {
	case "help", "-h", "--help":
		usage(os.Stdout)
		return nil
	case "status":
		return runStatus(ctx, args[1:])
	case "setup-link":
		return runSetupLink(ctx, args[1:])
	case "hash-password":
		return runHashPassword(ctx, args[1:])
	case "user":
		return runUser(ctx, args[1:])
	case "org":
		return runOrg(ctx, args[1:])
	}
	return fmt.Errorf("unknown command %q. Run `warmblyctl --help` for the full list.", args[0])
}

// command is one help entry: what it does and one invocation worth copying.
// The example is the whole command line, because the piped ones do not survive
// having a `docker compose exec` prefix bolted on.
type command struct {
	name    string
	summary string
	example string
}

const composeExec = "docker compose -p warmbly exec backend warmblyctl "

var commands = []command{
	{"status", "Show accounts, admins, registration and mail state, how to get in, and what is wrong", composeExec + "status"},
	{"setup-link", "Print a fresh single-use link that claims an instance with no accounts", composeExec + "setup-link"},
	{"user create", "Create a user, an organization and a trial, optionally a platform admin", composeExec + "user create --email you@example.com --admin"},
	{"user list", "List accounts; --admin answers whether any platform admin survives", composeExec + "user list --admin"},
	{"user reset-password", "Print a one-time reset link, or set the password from stdin", composeExec + "user reset-password --email you@example.com"},
	{"user grant-admin", "Give an account a platform admin role", composeExec + "user grant-admin --email you@example.com --role super"},
	{"user revoke-admin", "Take platform admin away from an account", composeExec + "user revoke-admin --email old@example.com"},
	{"user disable-2fa", "Clear an account's TOTP enrollment after a lost authenticator", composeExec + "user disable-2fa --email you@example.com"},
	{"hash-password", "Print an argon2 hash for WARMBLY_BOOTSTRAP_PASSWORD_HASH", "printf '%s' 'your-password' | docker compose -p warmbly exec -T backend warmblyctl hash-password"},
	{"org list", "List the workspaces on this instance with their id, owner, and size", composeExec + "org list"},
	{"org export", "Write a whole workspace to a portable archive file", composeExec + "org export --org you@example.com --out /tmp/workspace.warmbly.zip"},
	{"org import", "Apply an archive to a workspace on this instance", composeExec + "org import --org you@example.com --file /tmp/workspace.warmbly.zip"},
}

func usage(w *os.File) {
	fmt.Fprint(w, `warmblyctl is the operator CLI for this Warmbly instance. It reads and writes
the database directly, so it works when the sign-in page does not.

Usage:
  warmblyctl <command> [flags]

Commands:
`)
	for _, c := range commands {
		fmt.Fprintf(w, "  %-20s %s\n", c.name, c.summary)
	}

	fmt.Fprint(w, "\nExamples:\n")
	for _, c := range commands {
		fmt.Fprintf(w, "  %s\n", c.example)
	}

	_, _ = io.WriteString(w, `
Anything piped into a container needs exec -T, which is what turns the TTY off.
Anything that prompts needs a TTY, so run it without -T.

Environment:
  PRIMARY_DB   Postgres connection string. Every command except hash-password needs it.
  REDIS        Redis URL. setup-link needs it, and reset-password needs it to mint a link.
  APP_URL      Base URL every printed link is built from.

  org export and org import additionally read the instance's crypto settings,
  because a workspace's sealed values have to be opened on the way out and
  re-sealed on the way in: KMS_PROVIDER (with KMS_LOCAL_MASTER_KEY or the AWS
  settings) and CREDENTIALS_ENCRYPTION_KEY. Run them inside the backend
  container, where those are already set.

Exit status:
  status exits non-zero when a check is at error severity, so it works as a
  post-deploy gate. Use status --quiet from cron: it prints the checks alone,
  and nothing at all when none fire. status --json always exits 0, because it
  doubles as a liveness probe; read .summary.error there instead.

Run `+"`warmblyctl <command> --help`"+` for one command's flags.
`)
}

// newFlagSet gives every subcommand the same help shape: its flags, then one
// example, because a flag list alone never answers "what do I type". The text
// comes from the commands table so there is one place to keep it honest.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	c := lookupCommand(name)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s.\n\nUsage:\n  warmblyctl %s [flags]\n\nFlags:\n", c.summary, name)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExample:\n  %s\n", c.example)
	}
	return fs
}

func lookupCommand(name string) command {
	for _, c := range commands {
		if c.name == name {
			return c
		}
	}
	return command{name: name, summary: "warmblyctl " + name}
}

// noExtraArgs rejects stray positional arguments so a typo like
// `user create you@example.com` fails loudly instead of creating nothing.
func noExtraArgs(fs *flag.FlagSet) error {
	if fs.NArg() == 0 {
		return nil
	}
	return fmt.Errorf("unexpected argument %q. Every value goes in a flag, for example --email you@example.com.", fs.Arg(0))
}

// warn writes an operational note that is not a failure.
func warn(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "Warning: "+format+"\n", args...)
}

// indent renders a block of next steps under a heading.
func printSteps(heading string, steps []string) {
	if len(steps) == 0 {
		return
	}
	fmt.Printf("\n%s\n", heading)
	for _, s := range steps {
		for _, line := range strings.Split(s, "\n") {
			fmt.Printf("  %s\n", line)
		}
	}
}

// warmbly is the command line interface to Warmbly.
//
// It is the customer's CLI, not the operator's: it signs in as a person, holds
// one credential per host in ~/.config/warmbly, and speaks only the public
// REST API, so it drives the hosted service and any self-hosted instance the
// caller can reach. Everything it can do is bounded by the scopes the sign-in
// approved, and it never serves HTTP.
//
// The other CLI, warmblyctl, is the operator's: it talks to Postgres directly,
// runs inside the backend container, and exists for recovery and accounts. If
// you are asking "what is wrong with this install", that is the one you want.
//
//	warmbly auth login
//	warmbly campaign list
//	warmbly api "/campaigns?limit=10"
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/warmbly/warmbly/internal/cli/api"
	"github.com/warmbly/warmbly/internal/cli/config"
	"github.com/warmbly/warmbly/internal/cli/iostreams"
)

// errCancelled is a user saying no at a prompt. It exits 1 with no error line,
// because the person already knows what happened.
var errCancelled = errors.New("cancelled")

// errSilent lets a command print its own failure and still exit non-zero.
var errSilent = errors.New("")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	f := NewFactory()
	root := newRootCmd(f)
	root.SetArgs(expandAliases(f, os.Args[1:]))

	err := root.ExecuteContext(ctx)

	// After the command, never before: the reminder is not worth delaying a
	// result for, and it must not appear instead of an error.
	if err == nil {
		nudgeAboutUpdates(ctx, f)
	}

	if err != nil {
		os.Exit(reportError(f.IO, err))
	}
}

// reportError turns whatever came back into one line a person can act on, and
// the exit code a script can branch on:
//
//	1  the command failed
//	2  usage was wrong
//	4  not signed in, or the credential was rejected
func reportError(io *iostreams.IOStreams, err error) int {
	if errors.Is(err, errCancelled) {
		fmt.Fprintln(io.ErrOut, io.Gray("Cancelled."))
		return 1
	}
	if errors.Is(err, errSilent) {
		return 1
	}

	var noToken *config.ErrNoToken
	if errors.As(err, &noToken) {
		fmt.Fprintf(io.ErrOut, "%s %s\n", io.Cross(), noToken.Error())
		return 4
	}

	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		fmt.Fprintf(io.ErrOut, "%s %s\n", io.Cross(), apiErr.Error())
		switch {
		case apiErr.IsUnauthorized():
			fmt.Fprintln(io.ErrOut, io.Gray("The credential was rejected. Run `warmbly auth status` to see which one was used, or `warmbly auth login` to replace it."))
			return 4
		case apiErr.Status == 403:
			fmt.Fprintln(io.ErrOut, io.Gray("The key is missing a scope for this call. `warmbly auth refresh` re-runs the sign-in and can ask for more."))
			return 4
		}
		return 1
	}

	var noTTY *iostreams.ErrNoTTY
	if errors.As(err, &noTTY) {
		fmt.Fprintf(io.ErrOut, "%s %s\n", io.Cross(), noTTY.Error())
		return 2
	}

	fmt.Fprintf(io.ErrOut, "%s %s\n", io.Cross(), err.Error())
	return 1
}

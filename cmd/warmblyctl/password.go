package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

// One reader for the whole process: a fresh bufio.Reader per prompt would eat
// the second password out of its buffer.
var stdin = bufio.NewReader(os.Stdin)

// errNoTerminal is the refusal that keeps a passwordless account from existing.
var errNoTerminal = errors.New("stdin is not a terminal, so there is nowhere to prompt for a password, and an account nobody can sign in to is worse than no account.\nPipe the password in instead:\n  printf '%s' 'your-password' | docker compose -p warmbly exec -T backend warmblyctl ... --password-stdin")

// readPassword takes the password from stdin when --password-stdin is set, and
// otherwise prompts twice on a terminal. A non-TTY without --password-stdin is
// refused rather than silently creating an account nobody can sign in to.
func readPassword(ctx context.Context, fromStdin bool, what string) (string, error) {
	if fromStdin {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("reading the password from stdin: %w", err)
		}
		password := strings.TrimRight(string(data), "\r\n")
		if password == "" {
			return "", errors.New("--password-stdin was set but stdin was empty. Pipe the password in:\n  printf '%s' 'your-password' | docker compose -p warmbly exec -T backend warmblyctl ... --password-stdin")
		}
		return password, nil
	}

	if !stdinIsTerminal(ctx) {
		return "", errNoTerminal
	}

	first, err := promptSecret(ctx, what+": ")
	if err != nil {
		return "", err
	}
	second, err := promptSecret(ctx, "Repeat "+strings.ToLower(what)+": ")
	if err != nil {
		return "", err
	}
	if first != second {
		return "", errors.New("the two passwords do not match, so nothing was changed. Run the command again.")
	}
	return first, nil
}

// validatePassword uses the same rule the dashboard enforces, so the scripted
// route is never weaker than the interactive one.
func validatePassword(password string) error {
	if crypt.ValidatePassword(password) {
		return nil
	}
	return errors.New("that password is not accepted: it must be between 8 and 128 characters. Nothing was changed.")
}

func promptSecret(ctx context.Context, label string) (string, error) {
	fmt.Fprint(os.Stderr, label)

	restore, err := disableEcho(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nWarning: terminal echo could not be turned off, so the password will be visible as you type.")
		fmt.Fprint(os.Stderr, label)
	} else {
		defer restore()
	}

	line, rerr := stdin.ReadString('\n')
	fmt.Fprintln(os.Stderr)
	if rerr != nil && line == "" {
		// Nothing to read means nobody is typing, whatever the terminal test said.
		if errors.Is(rerr, io.EOF) {
			return "", errNoTerminal
		}
		return "", fmt.Errorf("reading the password: %w", rerr)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// stdinIsTerminal reports whether a person is on the other end. The
// character-device test alone answers yes for /dev/null, so stty confirms it:
// docker compose exec allocates a TTY unless -T is passed.
func stdinIsTerminal(ctx context.Context) bool {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	if _, err := exec.LookPath("stty"); err != nil {
		// No stty to ask. The prompt itself fails closed on EOF.
		return true
	}
	return stty(ctx, "-g") == nil
}

// disableEcho shells out to stty (present in the alpine runtime image via
// busybox) because turning echo off in pure Go needs per-platform ioctls.
func disableEcho(ctx context.Context) (func(), error) {
	if _, err := exec.LookPath("stty"); err != nil {
		return nil, err
	}
	if err := stty(ctx, "-echo"); err != nil {
		return nil, err
	}
	return func() { _ = stty(ctx, "echo") }, nil
}

func stty(ctx context.Context, arg string) error {
	cmd := exec.CommandContext(ctx, "stty", arg)
	cmd.Stdin = os.Stdin
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

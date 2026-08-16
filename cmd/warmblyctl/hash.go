package main

import (
	"context"
	"fmt"
	"os"

	"github.com/warmbly/warmbly/internal/pkg/argon2"
)

func runHashPassword(ctx context.Context, args []string) error {
	fs := newFlagSet("hash-password")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}

	// Prompts when a terminal is attached, reads the pipe when one is not, so
	// the same command works by hand and in a provisioning script.
	password, err := readPassword(ctx, !stdinIsTerminal(ctx), "Password to hash")
	if err != nil {
		return err
	}
	if err := validatePassword(password); err != nil {
		return err
	}

	hash, herr := argon2.Hash(password)
	if herr != nil {
		return fmt.Errorf("hashing the password: %w", herr)
	}

	// The hash alone goes to stdout so the command can be piped or captured.
	fmt.Println(hash)

	fmt.Fprint(os.Stderr, `
Set it before the first start of an instance with no accounts:
  WARMBLY_BOOTSTRAP_EMAIL=you@example.com
  WARMBLY_BOOTSTRAP_PASSWORD_HASH='<the line above>'

Quote it. The hash contains $ characters, which docker compose reads as
interpolation in docker-compose.yml (write them as $$ there) and a shell reads
inside double quotes.
`)
	return nil
}

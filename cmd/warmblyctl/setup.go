package main

import (
	"context"
	"fmt"

	"github.com/warmbly/warmbly/internal/app/bootstrap"
)

func runSetupLink(ctx context.Context, args []string) error {
	fs := newFlagSet("setup-link")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}

	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.close()

	accounts, cerr := c.users.CountUsers(ctx)
	if cerr != nil {
		return fmt.Errorf("counting accounts: %w", cerr)
	}
	if accounts > 0 {
		return fmt.Errorf("this instance already has %d account(s), so no setup link is issued: a second owner must never be mintable without an existing account.\nCreate the account you need instead:\n  warmblyctl user create --email you@example.com --admin\nOr recover the one you had:\n  warmblyctl user reset-password --email you@example.com", accounts)
	}

	// The token lives in Redis, so this is the one command that cannot degrade.
	if err := c.openCache(ctx, true, ""); err != nil {
		return fmt.Errorf("%w\nA setup link is stored in Redis, so it cannot be issued while Redis is down. Create the owner directly instead:\n  warmblyctl user create --email you@example.com --admin", err)
	}

	svc := bootstrap.NewService(c.users, c.userService(), c.orgService(), c.trialService(), c.admins, c.cache)
	link, lerr := svc.IssueSetupLink(ctx)
	if lerr != nil {
		return lerr
	}

	fmt.Printf("Issued a setup link. It replaces any earlier one, is single use, and expires in %s.\n\n  %s\n\n", bootstrap.SetupTokenTTL, link)
	fmt.Println("Only its hash is stored, so this is the only time it is printed.")
	fmt.Println("If that host is wrong, set APP_URL to the URL the dashboard is served from and run this again.")
	return nil
}

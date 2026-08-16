package main

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/auth"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

// resetSessionKeyPrefix must match getResetPasswordSessionKey in
// internal/app/auth/cache.go, or the link this CLI prints is rejected on open.
const resetSessionKeyPrefix = "reset_password:"

// maxResetTTL bounds --ttl: a reset link is a bearer credential for the
// account, so it should expire in hours, not weeks.
const maxResetTTL = 24 * time.Hour

func runUser(ctx context.Context, args []string) error {
	if len(args) == 0 {
		userUsage(os.Stderr)
		return errors.New("`user` needs a subcommand. Pick one from the list above.")
	}

	switch args[0] {
	case "help", "-h", "--help":
		userUsage(os.Stdout)
		return nil
	case "create":
		return runUserCreate(ctx, args[1:])
	case "list":
		return runUserList(ctx, args[1:])
	case "reset-password":
		return runUserResetPassword(ctx, args[1:])
	case "grant-admin":
		return runUserGrantAdmin(ctx, args[1:])
	case "revoke-admin":
		return runUserRevokeAdmin(ctx, args[1:])
	case "disable-2fa":
		return runUserDisable2FA(ctx, args[1:])
	}

	userUsage(os.Stderr)
	return fmt.Errorf("unknown subcommand `user %s`. Pick one from the list above.", args[0])
}

func userUsage(w *os.File) {
	fmt.Fprint(w, "Manage the accounts on this instance.\n\nUsage:\n  warmblyctl user <subcommand> [flags]\n\nSubcommands:\n")
	for _, c := range commands {
		if !strings.HasPrefix(c.name, "user ") {
			continue
		}
		fmt.Fprintf(w, "  %-16s %s\n", strings.TrimPrefix(c.name, "user "), c.summary)
	}
	fmt.Fprint(w, "\nExamples:\n")
	for _, c := range commands {
		if strings.HasPrefix(c.name, "user ") {
			fmt.Fprintf(w, "  %s\n", c.example)
		}
	}
}

func runUserCreate(ctx context.Context, args []string) error {
	fs := newFlagSet("user create")
	address := fs.String("email", "", "address of the account to create (required)")
	admin := fs.Bool("admin", false, "grant every platform admin permission")
	orgName := fs.String("org", "", "name for the new organization (default \"<name>'s Organization\")")
	noOrg := fs.Bool("no-org", false, "create the account with no organization and no trial")
	passwordStdin := fs.Bool("password-stdin", false, "read the password from stdin instead of prompting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}

	parsed, err := requireEmail(*address)
	if err != nil {
		return err
	}
	if *noOrg && strings.TrimSpace(*orgName) != "" {
		return errors.New("--org and --no-org contradict each other. Pass one or neither.")
	}

	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.close()

	if existing, lerr := c.users.GetUserByEmail(ctx, parsed.Address); lerr == nil && existing != nil {
		return fmt.Errorf("an account with the address %s already exists, so nothing was created.\nGive it a new password instead:\n  warmblyctl user reset-password --email %s\nOr make it a platform admin:\n  warmblyctl user grant-admin --email %s --role super", parsed.Address, parsed.Address, parsed.Address)
	}

	if err := c.openCache(ctx, false, "The new account will not be warmed into the cache, which is harmless."); err != nil {
		return err
	}

	password, err := readPassword(ctx, *passwordStdin, "Password for "+parsed.Address)
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

	created, cerr := c.users.CreateUser(ctx, parsed, hash)
	if cerr != nil {
		return fmt.Errorf("creating the account: %w", cerr)
	}
	if c.cache != nil {
		if xerr := c.userService().SaveUser(ctx, created); xerr != nil {
			warn("the account was created but could not be cached: %v", xerr)
		}
	}

	changed := []string{fmt.Sprintf("Created account %s (id %s)", created.Email, created.ID)}

	if !*noOrg {
		name := strings.TrimSpace(*orgName)
		if name == "" {
			name = defaultOrgName(created.FirstName)
		}
		org, oerr := c.orgService().Create(ctx, created.ID, name)
		if oerr != nil {
			return fmt.Errorf("the account %s was created, but its organization was not: %w\nCreate one from the dashboard after signing in.", created.Email, oerr)
		}
		changed = append(changed, fmt.Sprintf("organization %q (id %s)", org.Name, org.ID))

		if terr := c.trialService().StartFreeTrialWithOrg(ctx, created.ID, org.ID); terr != nil {
			warn("the free trial could not be started for %q: %v. The workspace still exists.", org.Name, terr)
		} else {
			changed = append(changed, "a free trial")
		}
	}

	if *admin {
		if aerr := c.admins.GrantBootstrapAdmin(ctx, created.ID, uint32(models.AllAdminPermissions)); aerr != nil {
			return fmt.Errorf("the account %s was created, but the admin grant failed: %w\nRetry it with:\n  warmblyctl user grant-admin --email %s --role super", created.Email, aerr, created.Email)
		}
		changed = append(changed, fmt.Sprintf("every platform admin permission (mask %d)", uint32(models.AllAdminPermissions)))
	}

	fmt.Printf("%s.\n", strings.Join(changed, ", "))

	steps := []string{fmt.Sprintf("Sign in at %s with the password you just set.", config.AppBaseURL())}
	if !*admin {
		steps = append(steps, "This account is not a platform admin. Make it one with:", fmt.Sprintf("  warmblyctl user grant-admin --email %s --role super", created.Email))
	}
	printSteps("Next", steps)
	return nil
}

func runUserList(ctx context.Context, args []string) error {
	fs := newFlagSet("user list")
	adminsOnly := fs.Bool("admin", false, "list only accounts holding platform admin permissions")
	limit := fs.Int("limit", 50, "how many accounts to print (1 to 100)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}
	if *limit < 1 || *limit > 100 {
		return errors.New("--limit must be between 1 and 100.")
	}

	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.close()

	search := &models.AdminUserSearch{Limit: *limit, SortBy: "created_at", SortDesc: false}
	if *adminsOnly {
		search.IsAdmin = adminsOnly
	}

	result, serr := c.admins.SearchUsers(ctx, search)
	if serr != nil {
		return fmt.Errorf("listing accounts: %w", serr)
	}

	if len(result.Data) == 0 {
		if *adminsOnly {
			fmt.Println("No platform admin exists on this instance, so the admin panel is closed to everyone.")
			printSteps("How to fix that", []string{
				"Promote an existing account:",
				"  warmblyctl user grant-admin --email you@example.com --role super",
				"Or create one:",
				"  warmblyctl user create --email you@example.com --admin",
			})
			return nil
		}
		fmt.Println("This instance has no accounts yet.")
		printSteps("How to get in", []string{
			"Print a claim link:  warmblyctl setup-link",
			"Or create the owner directly:",
			"  warmblyctl user create --email you@example.com --admin",
		})
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "EMAIL\tNAME\tADMIN\tCREATED")
	for _, u := range result.Data {
		name := strings.TrimSpace(u.FirstName + " " + u.LastName)
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", u.Email, name, roleLabel(u.AdminPermissions), u.CreatedAt.UTC().Format(time.DateOnly))
	}
	if err := w.Flush(); err != nil {
		return err
	}

	if result.Pagination.HasMore {
		fmt.Printf("\nMore accounts exist. Showing the first %d; raise --limit (max 100) to see more.\n", len(result.Data))
	}
	return nil
}

func runUserResetPassword(ctx context.Context, args []string) error {
	fs := newFlagSet("user reset-password")
	address := fs.String("email", "", "address of the account to reset (required)")
	passwordStdin := fs.Bool("password-stdin", false, "read the new password from stdin and set it immediately")
	ttl := fs.Duration("ttl", auth.PasswordResetTTL, "how long the printed link stays valid")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}

	parsed, err := requireEmail(*address)
	if err != nil {
		return err
	}
	if *ttl <= 0 || *ttl > maxResetTTL {
		return fmt.Errorf("--ttl must be greater than zero and at most %s. A reset link is a bearer credential for the account.", maxResetTTL)
	}

	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.close()

	u, err := lookupUser(ctx, c, parsed.Address)
	if err != nil {
		return err
	}

	if *passwordStdin {
		return setPassword(ctx, c, u)
	}
	return issueResetLink(ctx, c, u, *ttl)
}

// issueResetLink mints the same session ResetPasswordStart does and prints the
// URL, so the new password is chosen in the browser and never enters shell
// history, process listings or the terminal scrollback.
func issueResetLink(ctx context.Context, c *conn, u *models.User, ttl time.Duration) error {
	if err := c.openCache(ctx, true, ""); err != nil {
		return fmt.Errorf("%w\nA reset link is bound to a nonce in Redis, so it cannot be issued while Redis is down. Set the password directly instead:\n  printf '%%s' 'your-password' | warmblyctl user reset-password --email %s --password-stdin", err, u.Email)
	}

	secret, err := authSecret(ctx)
	if err != nil {
		return err
	}

	sessionID := uuid.New()
	nonce, nerr := crypt.Nonce()
	if nerr != nil {
		return fmt.Errorf("generating the session nonce: %w", nerr)
	}

	issuedAt := time.Now()
	expiresAt := issuedAt.Add(ttl)

	tok, terr := c.tokenService(secret).GenerateToken(u.ID, sessionID, u.Email, nonce, issuedAt, expiresAt)
	if terr != nil {
		return fmt.Errorf("signing the reset token: %w", terr)
	}
	if serr := c.cache.SetEx(ctx, resetSessionKeyPrefix+sessionID.String(), nonce, ttl).Err(); serr != nil {
		return fmt.Errorf("storing the reset session: %w", serr)
	}

	fmt.Printf("Issued a password reset session for %s. It is single use and expires at %s. No password has changed yet.\n\n  %s\n\n", u.Email, expiresAt.UTC().Format(time.RFC3339), config.GetPasswordResetURL(tok))
	fmt.Println("Open that link in a browser to choose the new password. Opening it revokes every existing session for the account.")
	fmt.Println("If the host is wrong, set APP_URL to the URL the dashboard is served from and run this again.")
	return nil
}

// setPassword is the automation path: no browser, no link, and the same
// session eviction the browser reset performs.
func setPassword(ctx context.Context, c *conn, u *models.User) error {
	if err := c.openCache(ctx, false, "Existing sessions cannot be revoked without it."); err != nil {
		return err
	}

	password, err := readPassword(ctx, true, "New password for "+u.Email)
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
	if xerr := c.auth.ResetPassword(ctx, u.ID, hash); xerr != nil {
		return fmt.Errorf("writing the new password: %w", xerr)
	}

	// uuid.Nil matches no session, so every one is revoked. The signing key is
	// empty because revocation never mints a token.
	revoked := "and revoked every existing session"
	if c.cache == nil {
		revoked = "but could not revoke existing sessions while Redis is unreachable"
	} else if xerr := c.tokenService("").RevokeOtherSessions(ctx, u.ID, uuid.Nil); xerr != nil {
		revoked = fmt.Sprintf("but the session revocation failed (%v)", xerr)
	}

	fmt.Printf("Changed the password for %s %s.\n", u.Email, revoked)
	printSteps("Next", []string{fmt.Sprintf("Sign in at %s", config.AppBaseURL())})
	return nil
}

func runUserGrantAdmin(ctx context.Context, args []string) error {
	fs := newFlagSet("user grant-admin")
	address := fs.String("email", "", "address of the account to promote (required)")
	role := fs.String("role", "", "one of super, support, ops, analyst (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}

	parsed, err := requireEmail(*address)
	if err != nil {
		return err
	}

	name := models.AdminRoleName(strings.ToLower(strings.TrimSpace(*role)))
	permissions, ok := models.AdminRolePermissions[name]
	if !ok {
		return fmt.Errorf("--role must be one of %s. `super` is the one that opens every admin screen.", strings.Join(adminRoleNames(), ", "))
	}

	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.close()

	u, err := lookupUser(ctx, c, parsed.Address)
	if err != nil {
		return err
	}
	if u.AdminPermissions == permissions {
		fmt.Printf("No change: %s already holds the %s platform admin role (mask %d).\n", u.Email, name, uint32(permissions))
		return nil
	}

	// GrantBootstrapAdmin, not UpdateUserAdminPermissions: admin_granted_by is
	// a foreign key to users and a host-level grant has no admin behind it.
	if aerr := c.admins.GrantBootstrapAdmin(ctx, u.ID, uint32(permissions)); aerr != nil {
		return fmt.Errorf("granting admin permissions: %w", aerr)
	}

	fmt.Printf("Granted %s the %s platform admin role (mask %d, was %s).\n", u.Email, name, uint32(permissions), roleLabel(u.AdminPermissions))
	printSteps("Next", []string{"Sign out and back in: the admin panel reads the permissions from the session.", adminHint()})
	return nil
}

func runUserRevokeAdmin(ctx context.Context, args []string) error {
	fs := newFlagSet("user revoke-admin")
	address := fs.String("email", "", "address of the account to demote (required)")
	force := fs.Bool("force", false, "allow removing the last remaining platform admin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}

	parsed, err := requireEmail(*address)
	if err != nil {
		return err
	}

	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.close()

	u, err := lookupUser(ctx, c, parsed.Address)
	if err != nil {
		return err
	}
	if u.AdminPermissions == 0 {
		fmt.Printf("No change: %s is not a platform admin.\n", u.Email)
		return nil
	}

	result, lerr := c.admins.ListAdmins(ctx, nil, 100)
	if lerr != nil {
		return fmt.Errorf("counting the remaining platform admins: %w", lerr)
	}
	last := len(result.Data) == 1 && !result.Pagination.HasMore
	if last && !*force {
		return fmt.Errorf("%s is the only platform admin left, so removing it closes the admin panel for everyone and nothing else can reopen it from the dashboard.\nGrant someone else first:\n  warmblyctl user grant-admin --email other@example.com --role super\nOr accept that, and re-run with --force.", u.Email)
	}

	if uerr := c.admins.UpdateUserAdminPermissions(ctx, u.ID, 0, uuid.Nil); uerr != nil {
		return fmt.Errorf("revoking admin permissions: %w", uerr)
	}

	fmt.Printf("Removed every platform admin permission from %s (was %s).\n", u.Email, roleLabel(u.AdminPermissions))
	if last {
		printSteps("Warning", []string{
			"This instance now has no platform admin. Grant one before you need it:",
			"  warmblyctl user grant-admin --email you@example.com --role super",
		})
	}
	return nil
}

func runUserDisable2FA(ctx context.Context, args []string) error {
	fs := newFlagSet("user disable-2fa")
	address := fs.String("email", "", "address of the account to clear (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}

	parsed, err := requireEmail(*address)
	if err != nil {
		return err
	}

	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.close()

	u, err := lookupUser(ctx, c, parsed.Address)
	if err != nil {
		return err
	}

	enrolled, gerr := c.totp.Get(ctx, u.ID)
	if gerr != nil {
		return fmt.Errorf("reading the TOTP enrollment: %w", gerr)
	}
	if enrolled == nil {
		fmt.Printf("No change: %s has no authenticator enrolled.\n", u.Email)
		return nil
	}

	if derr := c.totp.Delete(ctx, u.ID); derr != nil {
		return fmt.Errorf("clearing the TOTP enrollment: %w", derr)
	}

	fmt.Printf("Cleared the authenticator and recovery codes for %s.\n", u.Email)
	printSteps("Next", []string{
		"The password still works, so sign in and enroll a new authenticator from account security.",
		fmt.Sprintf("Sign in at %s", config.AppBaseURL()),
	})
	return nil
}

func requireEmail(address string) (*mail.Address, error) {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return nil, errors.New("--email is required. For example: --email you@example.com")
	}
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil {
		return nil, fmt.Errorf("%q is not a valid email address.", trimmed)
	}
	return parsed, nil
}

func lookupUser(ctx context.Context, c *conn, address string) (*models.User, error) {
	u, err := c.users.GetUserByEmail(ctx, address)
	if err != nil || u == nil {
		if err != nil && !errors.Is(err, errx.ErrUser) {
			return nil, fmt.Errorf("looking up %s: %w", address, err)
		}
		return nil, fmt.Errorf("no account on this instance uses the address %s, so nothing was changed.\nSee who does:\n  warmblyctl user list", address)
	}
	return u, nil
}

// defaultOrgName mirrors the bootstrap owner's naming so an account created
// here is indistinguishable from one claimed through the setup link.
func defaultOrgName(firstName string) string {
	if firstName == "" {
		return "My Organization"
	}
	return firstName + "'s Organization"
}

func adminRoleNames() []string {
	return []string{
		string(models.AdminRoleSuper),
		string(models.AdminRoleSupport),
		string(models.AdminRoleOps),
		string(models.AdminRoleAnalyst),
	}
}

// adminHint points at the operator panel, which is a separate app from the
// dashboard and is not served from APP_URL.
func adminHint() string {
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv("ADMIN_URL")), "/"); v != "" {
		return "Admin panel: " + v
	}
	return "The admin panel is a separate app from the dashboard (ADMIN_URL, port 5174 in the default stack)."
}

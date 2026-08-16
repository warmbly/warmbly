package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/warmbly/warmbly/internal/app/instancecheck"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

// errChecksFailed is how `make doctor` fails a script. The findings are already
// on stdout, so main exits non-zero without printing anything else.
var errChecksFailed = errors.New("an instance check reported an error")

// instanceStatus is the whole answer to "what state is this install in".
// --json exists so the Makefile can branch on it without parsing prose. Every
// key here is consumed by something: never rename or drop one, only append.
type instanceStatus struct {
	Accounts      int      `json:"accounts"`
	Claimed       bool     `json:"claimed"`
	SetupRequired bool     `json:"setup_required"`
	AdminCount    int      `json:"admin_count"`
	Admins        []string `json:"admins"`

	Registration       string `json:"registration"`
	RegistrationSource string `json:"registration_source"`

	MailTransport       string `json:"mail_transport"`
	MailDelivers        bool   `json:"mail_delivers"`
	MailTransportSource string `json:"mail_transport_source"`

	AppURL       string `json:"app_url"`
	AppURLSource string `json:"app_url_source"`

	NextSteps []string `json:"next_steps"`

	Checks  []instancecheck.Finding `json:"checks"`
	Summary instancecheck.Summary   `json:"summary"`
}

func runStatus(ctx context.Context, args []string) error {
	fs := newFlagSet("status")
	asJSON := fs.Bool("json", false, "print the state as JSON instead of prose")
	quiet := fs.Bool("quiet", false, "print only the checks, without the instance summary (ignored with --json)")
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

	state, err := collectStatus(ctx, c)
	if err != nil {
		return err
	}

	// --json always exits 0: `make claim` uses it to decide the backend is
	// answering at all, and a script reads summary.error for the verdict.
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(state)
	}

	if !*quiet {
		printStatus(state)
		fmt.Println()
	}
	printChecks(state.Checks, state.Summary, *quiet)

	if state.Summary.Error > 0 {
		return errChecksFailed
	}
	return nil
}

func collectStatus(ctx context.Context, c *conn) (*instanceStatus, error) {
	accounts, err := c.users.CountUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("counting accounts: %w", err)
	}

	admins := []string{}
	result, aerr := c.admins.ListAdmins(ctx, nil, 100)
	if aerr != nil {
		return nil, fmt.Errorf("listing platform admins: %w", aerr)
	}
	for _, a := range result.Data {
		admins = append(admins, fmt.Sprintf("%s (%s)", a.Email, roleLabel(a.AdminPermissions)))
	}

	transport := config.MailTransport()
	delivers := transport != config.MailTransportLog
	registration := config.LoadAuthPolicy(delivers).Registration

	state := &instanceStatus{
		Accounts:      accounts,
		Claimed:       accounts > 0,
		SetupRequired: accounts == 0,
		AdminCount:    len(admins),
		Admins:        admins,

		Registration:       registration,
		RegistrationSource: registrationSource(registration),

		MailTransport:       transport,
		MailDelivers:        delivers,
		MailTransportSource: mailTransportSource(),

		AppURL:       config.AppBaseURL(),
		AppURLSource: appURLSource(),
	}
	state.NextSteps = nextSteps(state)
	state.Checks, state.Summary = runChecks(ctx, c)
	return state, nil
}

func printStatus(s *instanceStatus) {
	claimed := "no, so a setup link can still claim it"
	if s.Claimed {
		claimed = "yes, so no setup link is issued"
	}
	mail := fmt.Sprintf("%s (%s; %s)", s.MailTransport, mailEffect(s.MailTransport, s.MailDelivers), s.MailTransportSource)

	fmt.Println("Instance")
	fmt.Printf("  Accounts          %d\n", s.Accounts)
	fmt.Printf("  Claimed           %s\n", claimed)
	fmt.Printf("  Platform admins   %d\n", s.AdminCount)
	fmt.Printf("  Registration      %s (%s)\n", s.Registration, s.RegistrationSource)
	fmt.Printf("  Mail transport    %s\n", mail)
	fmt.Printf("  App URL           %s (%s)\n", s.AppURL, s.AppURLSource)

	if len(s.Admins) > 0 {
		fmt.Println("\nPlatform admins")
		for _, a := range s.Admins {
			fmt.Printf("  %s\n", a)
		}
	}

	printSteps("How to get in", s.NextSteps)
}

// nextSteps is the part an operator actually reads: what to run, given this
// exact state. Every branch ends in a command.
func nextSteps(s *instanceStatus) []string {
	appURL := s.AppURL

	if !s.Claimed {
		steps := []string{
			"No accounts exist yet, so the first person to open a setup link becomes the owner.",
			"Print one:  warmblyctl setup-link",
			"Or skip the link and create the owner directly:",
			"  warmblyctl user create --email you@example.com --admin",
			"Unattended installs can set WARMBLY_BOOTSTRAP_EMAIL and WARMBLY_BOOTSTRAP_PASSWORD_HASH before the first start.",
		}
		if !s.MailDelivers {
			steps = append(steps, "Mail does not leave this instance, so any emailed code is printed in the backend log instead.")
		}
		return steps
	}

	steps := []string{fmt.Sprintf("Sign in at %s", appURL)}
	if s.AdminCount == 0 {
		steps = append(steps,
			"No platform admin exists, so the admin panel is closed to everyone. Promote an account:",
			"  warmblyctl user grant-admin --email you@example.com --role super",
		)
	}
	steps = append(steps,
		"Lost the password:       warmblyctl user reset-password --email you@example.com",
		"Lost the authenticator:  warmblyctl user disable-2fa --email you@example.com",
		"No account of your own:  warmblyctl user create --email you@example.com --admin",
		"Who is an admin:         warmblyctl user list --admin",
	)

	switch s.Registration {
	case config.RegistrationInviteOnly:
		steps = append(steps, "Registration is invite_only, so the signup form only accepts someone holding an invitation. The commands above do not need one.")
	case config.RegistrationClosed:
		steps = append(steps, "Registration is closed (DISABLE_REGISTRATION=true), so the signup form refuses everyone, invitation or not. The commands above still work.")
	}
	if !s.MailDelivers {
		steps = append(steps, "Mail does not leave this instance, so invitations and reset emails are printed in the backend log instead of being delivered.")
	}
	return steps
}

// registrationSource says whether the operator chose this mode or inherited it,
// which is the difference between "as configured" and "surprise".
func registrationSource(resolved string) string {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("DISABLE_REGISTRATION")))
	switch raw {
	case config.RegistrationOpen, config.RegistrationInviteOnly, config.RegistrationClosed:
		return "DISABLE_REGISTRATION=" + raw
	case "":
		return "DISABLE_REGISTRATION is unset, so the deployment default applies"
	}
	return fmt.Sprintf("DISABLE_REGISTRATION=%s is not a valid mode, so the deployment default applies", raw)
}

func mailTransportSource() string {
	if v := strings.TrimSpace(os.Getenv("MAIL_TRANSPORT")); v != "" {
		return "MAIL_TRANSPORT=" + v
	}
	if strings.TrimSpace(os.Getenv("SMTP_HOST")) != "" {
		return "SMTP_HOST is set, so smtp is implied"
	}
	return "MAIL_TRANSPORT is unset, so ses is the fallback"
}

func mailEffect(transport string, delivers bool) string {
	if !delivers {
		return "written to the backend log, never delivered"
	}
	if transport == config.MailTransportSES {
		return "delivered through AWS SES, which needs credentials and a verified identity"
	}
	return "delivered"
}

func appURLSource() string {
	for _, key := range []string{"APP_URL", "FRONTEND_BASE_URL"} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return key
		}
	}
	return "neither APP_URL nor FRONTEND_BASE_URL is set, so the hosted default applies"
}

// roleLabel names a permission mask, falling back to the number so a custom
// grant is still legible.
func roleLabel(p models.AdminPermission) string {
	if p == 0 {
		return "not an admin"
	}
	for _, role := range []models.AdminRoleName{
		models.AdminRoleSuper,
		models.AdminRoleSupport,
		models.AdminRoleOps,
		models.AdminRoleAnalyst,
	} {
		if models.AdminRolePermissions[role] == p {
			return fmt.Sprintf("%s, mask %d", role, uint32(p))
		}
	}
	return fmt.Sprintf("custom, mask %d", uint32(p))
}

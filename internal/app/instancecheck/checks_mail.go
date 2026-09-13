package instancecheck

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/config"
)

const docsLoginCodes = "/development/accounts-and-access/#login-codes"

func mailChecks() []check {
	return []check{
		{id: "mail_transport_log", run: checkMailTransportLog},
		{id: "mail_preflight_failed", run: checkMailPreflightFailed},
		{id: "mail_identity_unset", run: checkMailIdentityUnset},
		{id: "mail_from_domain_mismatch", run: checkMailFromDomainMismatch},
		{id: "login_code_demoted", run: checkLoginCodeDemoted},
		{id: "login_code_exempt_accounts", run: checkLoginCodeExemptAccounts},
	}
}

func checkMailTransportLog(ctx context.Context, d Deps, in Input) *Finding {
	if mailDelivers(d) {
		return nil
	}
	severity := SeverityWarning
	if isLoopbackURL(appURL()) {
		severity = SeverityInfo
	}
	return result(CategoryMail, severity, "Platform mail is not delivered",
		"Platform mail is not being delivered. MAIL_TRANSPORT=log writes every message to the backend log instead. "+
			"Login codes, password resets, team invitations and notification digests will never arrive. "+
			"Invitations still work: copy the invite link from Settings > Members and send it yourself.",
		docsMail)
}

func checkMailPreflightFailed(ctx context.Context, d Deps, in Input) *Finding {
	// One incident is one row: a transport that does not deliver already has
	// its own finding, so do not also report that it will not dial.
	if d.Transport == nil || !d.Transport.Delivers {
		return nil
	}
	err := d.Transport.Preflight(ctx)
	if err == nil {
		return nil
	}
	return result(CategoryMail, SeverityError, "The mail relay did not accept a connection",
		fmt.Sprintf("The mail relay did not accept a connection: %s. "+
			"Nobody can reset a password or receive an invitation until this is fixed.", err.Error()),
		docsMail)
}

func checkMailIdentityUnset(ctx context.Context, d Deps, in Input) *Finding {
	if env("EMAIL_NAME") != "" && env("EMAIL_ADDRESS") != "" {
		return nil
	}
	return result(CategoryMail, SeverityError, "Platform mail identity is missing",
		"EMAIL_ADDRESS or EMAIL_NAME is missing. The backend refuses to start without them, "+
			"but the consumer only warns and silently disables all notification and digest email, "+
			"so this instance can look healthy while sending nothing.",
		docsMail)
}

func checkMailFromDomainMismatch(ctx context.Context, d Deps, in Input) *Finding {
	address := env("EMAIL_ADDRESS")
	dashboard := hostOf(appURL())
	if address == "" || dashboard == "" || !appURLConfigured() {
		return nil
	}
	parsed, err := mail.ParseAddress(address)
	if err != nil {
		return nil
	}
	at := strings.LastIndex(parsed.Address, "@")
	if at < 0 {
		return nil
	}
	if registrableDomain(parsed.Address[at+1:]) == registrableDomain(dashboard) {
		return nil
	}
	return result(CategoryMail, SeverityInfo, "Mail sender domain does not match the dashboard",
		fmt.Sprintf("Platform mail is sent from %s while the dashboard is at %s. "+
			"Mailbox providers may treat that as a mismatch. This is only a warning if you intended them to differ.",
			parsed.Address, dashboard),
		docsMail)
}

func checkLoginCodeDemoted(ctx context.Context, d Deps, in Input) *Finding {
	requested := strings.ToLower(env("AUTH_LOGIN_CODE"))
	if requested != config.LoginCodeAlways || mailDelivers(d) {
		return nil
	}
	return result(CategoryMail, SeverityInfo, "Login codes were demoted",
		"AUTH_LOGIN_CODE is set to always, but the mail transport does not deliver, so it has been demoted to new_device. "+
			"Otherwise nobody could ever complete a login.",
		docsLoginCodes)
}

// checkLoginCodeExemptAccounts lists accounts excused from the emailed login
// code. Granted deliberately and for a reason, so this is not an error; it is
// here because the failure mode is forgetting. An exemption granted for a
// two-week vendor review is still there a year later unless something says so
// on every run.
func checkLoginCodeExemptAccounts(ctx context.Context, d Deps, in Input) *Finding {
	if d.DB == nil {
		return nil
	}
	rows, err := d.DB.Query(ctx, `
		SELECT email, login_code_exempt_reason, login_code_exempt_at
		FROM users
		WHERE login_code_exempt
		ORDER BY login_code_exempt_at NULLS FIRST`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var email string
		var reason *string
		var at *time.Time
		if err := rows.Scan(&email, &reason, &at); err != nil {
			return nil
		}
		line := email
		if reason != nil && strings.TrimSpace(*reason) != "" {
			line += " (" + strings.TrimSpace(*reason) + ")"
		}
		if at != nil {
			line += ", since " + at.Format("2 Jan 2006")
		}
		lines = append(lines, line)
	}
	// A halted iteration leaves the rows read before the error, and reporting
	// those as the complete list would understate what is exempt.
	if rows.Err() != nil {
		return nil
	}
	if len(lines) == 0 {
		return nil
	}

	return result(CategoryMail, SeverityWarning, "Accounts are exempt from the login code",
		fmt.Sprintf("%s signs in with a password and captcha alone, with no emailed code, whatever AUTH_LOGIN_CODE says. "+
			"Remove an exemption whose reason no longer holds with `warmblyctl user login-code-exempt --email <address> --clear`.",
			strings.Join(lines, "; ")),
		docsLoginCodes)
}

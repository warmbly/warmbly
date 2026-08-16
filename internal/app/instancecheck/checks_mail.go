package instancecheck

import (
	"context"
	"fmt"
	"net/mail"
	"strings"

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

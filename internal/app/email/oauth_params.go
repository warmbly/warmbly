package email

import (
	"net/mail"
	"net/url"
	"strings"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"golang.org/x/oauth2"
)

// authCodeOptions returns the authorization-request parameters for a provider.
// loginHint preselects a mailbox on a reconnect and is empty on a first connect.
//
// The two providers disagree about what it takes to be issued a refresh token,
// and asking for the wrong one is not free:
//
//   - Google issues one only when access_type=offline is set, and re-issues one
//     on a repeat authorization only when the consent screen is forced.
//   - Microsoft issues one off the offline_access scope alone, so prompt=consent
//     buys nothing there and costs a lot: Entra ID re-runs the consent
//     eligibility check on every sign-in instead of honouring the grant already
//     on the tenant, and refuses every non-admin with AADSTS90095 even when
//     tenant-wide admin consent was granted (issue #409). prompt=select_account
//     keeps the account picker, which is what stops a signed-in browser
//     silently connecting the wrong mailbox, without that check.
func authCodeOptions(provider models.InboxProvider, loginHint string) []oauth2.AuthCodeOption {
	var opts []oauth2.AuthCodeOption
	if provider == models.InboxProviderOutlook {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "select_account"))
	} else {
		opts = append(opts, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	}
	if loginHint != "" {
		// Preselect the mailbox being renewed in the provider's picker.
		opts = append(opts, oauth2.SetAuthURLParam("login_hint", loginHint))
	}
	return opts
}

// loginHintOrEmpty passes on a hint only when it is a plausible address, so
// nothing else reaches the provider's authorize URL.
func loginHintOrEmpty(hint string) string {
	hint = strings.TrimSpace(hint)
	if len(hint) > 254 {
		return ""
	}
	if _, err := mail.ParseAddress(hint); err != nil || strings.ContainsAny(hint, " <>") {
		return ""
	}
	return hint
}

// personalGmail reports whether an address is a consumer Gmail one, which connects with an app password.
func personalGmail(addr string) bool {
	if !strings.Contains(addr, "@") {
		return false
	}
	d := mailhost.NormalizeDomain(addr)
	return d == "gmail.com" || d == "googlemail.com"
}

// OutlookAdminApprovalState marks the return from an administrator approving
// single-mailbox Microsoft sign-in for their whole organization.
const OutlookAdminApprovalState = "oac_approval"

// outlookAdminApprovalURL is Microsoft's admin consent page for exactly the
// delegated scopes single-mailbox sign-in requests, for a member whose
// organization lets only an administrator approve an app.
func outlookAdminApprovalURL(cfg *oauth2.Config) string {
	base, _, ok := strings.Cut(cfg.Endpoint.AuthURL, "/common/oauth2/v2.0/authorize")
	if !ok {
		return ""
	}
	q := url.Values{}
	q.Set("client_id", cfg.ClientID)
	q.Set("scope", strings.Join(cfg.Scopes, " "))
	q.Set("redirect_uri", cfg.RedirectURL)
	q.Set("state", OutlookAdminApprovalState)
	return base + "/organizations/v2.0/adminconsent?" + q.Encode()
}

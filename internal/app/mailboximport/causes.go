package mailboximport

import (
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailcause"
	"github.com/warmbly/warmbly/internal/repository"
)

// Causes an import finds before it dials anything, or around the dial.
const (
	causeMissingEmail      = "missing_email"
	causeInvalidEmail      = "invalid_email"
	causeDuplicateRow      = "duplicate_row"
	causeMissingPassword   = "missing_password"
	causeUnknownServers    = "unknown_servers"
	causeInvalidValue      = "invalid_value"
	causeMicrosoftSignin   = "microsoft_signin"
	causeGoogleSignin      = "google_signin"
	causeProviderUnsupport = "provider_unsupported"
	causeAllowanceReached  = "allowance_reached"
	causeNoWorker          = "no_worker"
	causeCreatorRemoved    = "creator_removed"
	causeInternal          = "internal"

	causeVendorUnauthorized = "vendor_unauthorized"
	causeVendorUnreachable  = "vendor_unreachable"
	// causeVendorAuthorizing parks a row while its vendor authorizes Warmbly on the domain.
	causeVendorAuthorizing = repository.ParkedVendorCause
)

// ErrIDVendorUnauthorized is the identifier a VendorSource answers with when the vendor refused the key.
const ErrIDVendorUnauthorized = "mailbox_vendor_unauthorized"

var importCauses = map[string]mailcause.Cause{
	causeMissingEmail: {Key: causeMissingEmail, Title: "No email address",
		Fix: "These rows have no address in the email column. Add one, or remove the row."},
	causeInvalidEmail: {Key: causeInvalidEmail, Title: "Not an email address",
		Fix: "The email column does not hold a valid address on these rows. Check for typos, spaces or a missing domain."},
	causeDuplicateRow: {Key: causeDuplicateRow, Title: "Listed twice in the file",
		Fix: "The same address appears earlier in the file. Only the first row is imported."},
	causeMissingPassword: {Key: causeMissingPassword, Title: "No password",
		Fix: "These rows have no password. Map the password column, or enter one password for every mailbox.", Retryable: true},
	causeUnknownServers: {Key: causeUnknownServers, Title: "Mail servers not found",
		Fix: "We could not find the IMAP and SMTP servers for this domain from its DNS. Add smtp_host and imap_host columns (your mail provider lists them), or fix the servers on each row."},
	causeInvalidValue: {Key: causeInvalidValue, Title: "A value could not be read",
		Fix: "A column on these rows holds a value that does not fit it, such as text in a port or limit column. The row names the column."},
	causeMicrosoftSignin: {Key: causeMicrosoftSignin, Title: "Connect with Microsoft sign-in",
		Fix: "Microsoft 365 and Outlook.com no longer accept passwords for IMAP. Use Sign in on each mailbox; a Microsoft 365 admin can approve Warmbly once for the whole organization so each sign-in is one click."},
	causeGoogleSignin: {Key: causeGoogleSignin, Title: "Connect with Google sign-in",
		Fix: "Google does not accept the account password for IMAP. Use Sign in on each mailbox, or add a 16-letter app password from myaccount.google.com/apppasswords to the row."},
	causeProviderUnsupport: {Key: causeProviderUnsupport, Title: "Provider cannot connect over IMAP",
		Fix: "This provider does not offer IMAP and SMTP sign-in to other apps (Proton Mail needs its Bridge app running on a machine you control). Connect a different mailbox."},
	causeAllowanceReached: {Key: causeAllowanceReached, Title: "Mailbox limit reached",
		Fix: "Your workspace has no room for more mailboxes. Request a higher limit, then retry these rows.", Retryable: true},
	causeNoWorker: {Key: causeNoWorker, Title: "No worker available",
		Fix: "No mail worker answered to check these credentials, so the mail server was never tried. Retry in a few minutes; if it keeps happening, check that a worker is running.", Retryable: true},
	causeCreatorRemoved: {Key: causeCreatorRemoved, Title: "Import owner left the workspace",
		Fix: "The person who started this import is no longer a member, so it cannot connect mailboxes in their name. Import these rows again yourself."},
	causeVendorUnauthorized: {Key: causeVendorUnauthorized, Title: "Vendor refused the API key",
		Fix: "The inbox vendor no longer accepts the API key saved for this account. Update the key from Add account > Inbox vendor, then retry these rows.", Retryable: true},
	causeVendorUnreachable: {Key: causeVendorUnreachable, Title: "Vendor did not return the credentials",
		Fix: "The inbox vendor did not answer with this mailbox's credentials, or no longer has it. Retry in a few minutes, or check the mailbox in the vendor's dashboard.", Retryable: true},
	causeVendorAuthorizing: {Key: causeVendorAuthorizing, Title: "Vendor is authorizing Warmbly",
		Fix: "The inbox vendor is approving Warmbly for these domains through the admin mailbox it holds, so nobody has to sign in. This usually takes a few minutes; the rows connect on their own when it finishes."},
	"mailbox_grant_not_configured": {Key: "mailbox_grant_not_configured", Title: "Admin connections are not set up here",
		Fix: "This instance has no Google service account or Microsoft app for admin connections. An operator configures them; see the self-hosting docs."},
	"google_delegation_unauthorized": {Key: "google_delegation_unauthorized", Title: "Google refused the domain-wide delegation",
		Fix: "In the Google Admin console, open Security > Access and data control > API controls > Domain-wide delegation and add Warmbly's client ID with exactly the scopes shown in the import dialog. Then check the grant and retry.", Retryable: true},
	"microsoft_consent_missing": {Key: "microsoft_consent_missing", Title: "Microsoft 365 admin consent is missing",
		Fix: "A Global Administrator has to approve Warmbly for the organization (Add account > Microsoft > Whole organization), then retry these rows.", Retryable: true},
	"mailbox_grant_mailbox_unreachable": {Key: "mailbox_grant_mailbox_unreachable", Title: "No mailbox for this account",
		Fix: "The provider has no mailbox for this user: the account may be suspended, disabled, or without an email license."},
	"mailbox_grant_domain_mismatch": {Key: "mailbox_grant_domain_mismatch", Title: "Domain not covered by the grant",
		Fix: "The address is on a domain the administrator's grant does not cover. Connect that domain too, or import the row with a password."},
	"mailbox_grant_inactive": {Key: "mailbox_grant_inactive", Title: "The admin grant stopped working",
		Fix: "The administrator's grant for this domain was revoked or failed its last check. Check it from Add account > Google or Microsoft > Whole domain, then retry.", Retryable: true},
	causeInternal: {Key: causeInternal, Title: "Something went wrong",
		Fix: "These rows hit an error on our side. Retry them; if it keeps happening, contact support with the import's id.", Retryable: true},
}

// causeInfo is a cause's title and fix, from the import's own list or the connect-failure catalogue.
func causeInfo(key string) mailcause.Cause {
	if c, ok := importCauses[key]; ok {
		return c
	}
	if c, ok := mailcause.Lookup(key); ok {
		return c
	}
	return mailcause.Cause{Key: key, Title: "Did not connect", Fix: "See the message on each row."}
}

func fillCauses(causes []models.MailboxImportCause) {
	for i := range causes {
		info := causeInfo(causes[i].Cause)
		causes[i].Title, causes[i].Fix = info.Title, info.Fix
		causes[i].Retryable = causes[i].Retryable && info.Retryable
	}
}

package formwire

import (
	"time"

	"github.com/warmbly/warmbly/internal/app/brandkit"
	"github.com/warmbly/warmbly/internal/models"
)

// The client connect page's slice of the internal API. The URL token travels in
// request bodies, never in an internal path, so it reaches no access log.

// ConnectLookup names a link by its URL token, as seen on Host.
type ConnectLookup struct {
	Token string `json:"token"`
	Host  string `json:"host,omitempty"`
}

// ConnectLink is what a client sees on a live link: who is asking and how they may connect.
// Nothing here names the member who made the link or the link's own label.
type ConnectLink struct {
	Workspace string         `json:"workspace"`
	Brand     brandkit.Brand `json:"brand"`
	// Providers are the email_provider values this link and this instance both allow.
	Providers    []string  `json:"providers"`
	MaxMailboxes int       `json:"max_mailboxes"`
	Connected    int       `json:"connected"`
	ExpiresAt    time.Time `json:"expires_at"`
	// WorkspaceFull is the workspace at its mailbox allowance, so no connect can succeed now.
	WorkspaceFull  bool   `json:"workspace_full,omitempty"`
	CaptchaSiteKey string `json:"captcha_site_key,omitempty"`
	// Vendors are the inbox vendors a client can connect an account of, when the link takes one.
	Vendors []ConnectVendor `json:"vendors,omitempty"`
}

// ConnectVendor is one inbox vendor and what connecting an account of it asks for.
type ConnectVendor struct {
	ID         string               `json:"id"`
	Label      string               `json:"label"`
	KeyHelpURL string               `json:"key_help_url,omitempty"`
	Fields     []ConnectVendorField `json:"fields"`
}

type ConnectVendorField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Secret   bool   `json:"secret"`
	Required bool   `json:"required"`
	Help     string `json:"help,omitempty"`
}

// ConnectVendorAccount is a vendor key a client typed, to be checked and listed.
type ConnectVendorAccount struct {
	ConnectLookup
	Vendor       string            `json:"vendor"`
	Fields       map[string]string `json:"fields"`
	CaptchaToken string            `json:"captcha_token,omitempty"`
	RemoteIP     string            `json:"remote_ip"`
}

// ConnectVendorMailboxes is what a client's vendor key reaches. The key is held under Ticket and never sent back.
type ConnectVendorMailboxes struct {
	Ticket    string                 `json:"ticket"`
	Vendor    string                 `json:"vendor"`
	Mailboxes []ConnectVendorMailbox `json:"mailboxes"`
	// Remaining is how many more mailboxes the link connects.
	Remaining int `json:"remaining"`
}

type ConnectVendorMailbox struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Name     string `json:"name,omitempty"`
	Provider string `json:"provider,omitempty"`
	// Connected is a mailbox already in the workspace.
	Connected bool `json:"connected,omitempty"`
}

// ConnectVendorImport picks mailboxes from the account a ticket holds.
type ConnectVendorImport struct {
	ConnectLookup
	Ticket     string   `json:"ticket"`
	MailboxIDs []string `json:"mailbox_ids"`
	RemoteIP   string   `json:"remote_ip"`
}

// ConnectVendorStarted is an import a client started: how many mailboxes are being connected, and the link's counts after it.
type ConnectVendorStarted struct {
	Mailboxes    int `json:"mailboxes"`
	Connected    int `json:"connected"`
	MaxMailboxes int `json:"max_mailboxes"`
}

// ConnectOAuthStart asks for a provider sign-in URL bound to the link.
type ConnectOAuthStart struct {
	ConnectLookup
	Provider string `json:"provider"`
	RemoteIP string `json:"remote_ip"`
}

// ConnectOAuthURL is where the client's browser goes to sign in, and the key its page collects the outcome with.
type ConnectOAuthURL struct {
	URL       string `json:"url"`
	ResultKey string `json:"result_key"`
}

// ConnectSigninResultRequest asks for the outcome of a sign-in the page started.
type ConnectSigninResultRequest struct {
	ConnectLookup
	ResultKey string `json:"result_key"`
}

// ConnectSigninResult is a sign-in's outcome: Status "pending" until the provider sends the browser back, then
// "connected" with the mailbox or "failed" with the refusal.
type ConnectSigninResult struct {
	Status       string `json:"status"`
	Email        string `json:"email,omitempty"`
	Provider     string `json:"provider,omitempty"`
	Code         string `json:"code,omitempty"`
	Message      string `json:"message,omitempty"`
	Connected    int    `json:"connected,omitempty"`
	MaxMailboxes int    `json:"max_mailboxes,omitempty"`
}

// ConnectSMTPIMAP is a client's server details for one mailbox.
type ConnectSMTPIMAP struct {
	ConnectLookup
	Email        string          `json:"email"`
	Name         string          `json:"name,omitempty"`
	SMTP         *models.Service `json:"smtp"`
	IMAP         *models.Service `json:"imap"`
	CaptchaToken string          `json:"captcha_token,omitempty"`
	RemoteIP     string          `json:"remote_ip"`
}

// ConnectedMailbox is one mailbox a client just connected.
type ConnectedMailbox struct {
	Email    string `json:"email"`
	Provider string `json:"provider"`
	// Connected and MaxMailboxes are the link's counts after this connect.
	Connected    int `json:"connected"`
	MaxMailboxes int `json:"max_mailboxes"`
}

// ConnectError is every refusal on these routes; Message is client-facing.
type ConnectError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ConnectTokenLength is the length of a connect link's URL token: 32 random bytes, base64url without padding.
const ConnectTokenLength = 43

// ConnectTokenShape reports whether token could be a connect link token, so garbage is refused without a lookup.
func ConnectTokenShape(token string) bool {
	if len(token) != ConnectTokenLength {
		return false
	}
	for _, r := range token {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

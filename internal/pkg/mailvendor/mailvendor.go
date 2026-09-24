// Package mailvendor reads a customer's mailboxes and their credentials from
// inbox-provisioning vendors' public APIs, so they can be imported without a CSV.
package mailvendor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Vendor ids. They are stored in the database, so never rename one.
const (
	VendorInboxKit     = "inboxkit"
	VendorZapmail      = "zapmail"
	VendorMailforge    = "mailforge"
	VendorInfraforge   = "infraforge"
	VendorMaildoso     = "maildoso"
	VendorCheapInboxes = "cheapinboxes"
	VendorScaledMail   = "scaledmail"
)

// Field keys a descriptor may ask for.
const (
	FieldAPIKey         = "api_key"
	FieldOrganizationID = "organization_id"
)

// Provider values on Mailbox.
const (
	ProviderGoogle    = "google"
	ProviderMicrosoft = "microsoft"
	ProviderSMTP      = "smtp"
)

// Security values on Endpoint.
const (
	SecurityTLS      = "tls"
	SecurityStartTLS = "starttls"
)

// MaxMailboxes caps List; a vendor account larger than this is truncated.
const MaxMailboxes = 10000

// Error kinds callers branch on with errors.Is.
var (
	ErrUnauthorized  = errors.New("mailvendor: key rejected")
	ErrRateLimited   = errors.New("mailvendor: rate limited")
	ErrNotFound      = errors.New("mailvendor: mailbox not found")
	ErrUnknownVendor = errors.New("mailvendor: unknown vendor")
	ErrInvalidConfig = errors.New("mailvendor: invalid configuration")
	ErrNoWorkspace   = errors.New("mailvendor: key reaches no workspace")
)

// Field is a credential the customer must enter, keyed by Key.
type Field struct {
	Key      string
	Label    string
	Secret   bool
	Required bool
	Help     string
	// Legacy fields are read from connections saved before the value was discovered, and never asked for.
	Legacy bool
}

// Descriptor describes a vendor and what connecting it asks for.
type Descriptor struct {
	ID         string
	Label      string
	Website    string
	KeyHelpURL string
	Fields     []Field
	// Verified is true when the integration was built against the vendor's published API docs.
	Verified bool
}

// Mailbox is one vendor mailbox. Provider is "google", "microsoft", "smtp" or "" when the vendor does not say.
type Mailbox struct {
	ID        string
	Email     string
	FirstName string
	LastName  string
	Domain    string
	Provider  string
	Status    string
	// Workspace is the name of the vendor workspace holding the mailbox, empty where the vendor has none.
	Workspace string
	// Admin marks the domain's administrator mailbox, where the vendor says.
	Admin bool
}

// Endpoint is one server a mailbox connects to.
type Endpoint struct {
	Host     string
	Port     int
	Security string
	Username string
	// Password is set only when this protocol's password differs from Credentials.Password.
	Password string
}

// Credentials is whatever the vendor returns; SMTP and IMAP are nil when it gives no server.
type Credentials struct {
	Password    string
	AppPassword string
	SMTP        *Endpoint
	IMAP        *Endpoint
}

// Client talks to one vendor account.
type Client interface {
	Vendor() string
	// Verify checks the key works with a cheap authenticated call.
	Verify(ctx context.Context) error
	// List returns every mailbox, following pagination, capped at MaxMailboxes.
	List(ctx context.Context) ([]Mailbox, error)
	// Credentials reads one mailbox's credentials, from List's cache where the vendor's list carries them.
	Credentials(ctx context.Context, m Mailbox) (Credentials, error)
}

// AppAuthorization asks a vendor to authorize an OAuth app on one of its domains,
// through the administrator mailbox it holds there.
type AppAuthorization struct {
	Domain string
	// Mailbox is any listed mailbox on the domain; its id names the vendor workspace.
	Mailbox  Mailbox
	Provider string
	ClientID string
	// Scopes are delegated scopes (Google: full scope URLs); AppRoles are Microsoft application permissions.
	Scopes   []string
	AppRoles []string
}

// Authorization states.
const (
	AuthorizationPending   = "pending"
	AuthorizationCompleted = "completed"
	AuthorizationFailed    = "failed"
)

// AuthorizationStatus is where an app authorization stands; Reason is the vendor's words on a failure.
type AuthorizationStatus struct {
	State  string
	Reason string
	// Stage is the vendor's own word for where the request is, for showing to a person.
	Stage string
	// UpdatedAt is when the vendor last moved the request; zero when it does not say.
	UpdatedAt time.Time
}

// AppAuthorizer is a vendor that can authorize an app on a domain it administers.
type AppAuthorizer interface {
	AuthorizeApp(ctx context.Context, a AppAuthorization) (requestID string, err error)
	AuthorizationStatus(ctx context.Context, requestID string) (AuthorizationStatus, error)
}

// Option configures New.
type Option func(*options)

type options struct {
	httpClient *http.Client
	baseURL    string
	sleep      func(context.Context, time.Duration) error
}

// WithHTTPClient replaces the default client (20s timeout, no redirects).
func WithHTTPClient(c *http.Client) Option {
	return func(o *options) { o.httpClient = c }
}

// WithBaseURL points the client at another host, for tests.
func WithBaseURL(u string) Option {
	return func(o *options) { o.baseURL = strings.TrimRight(u, "/") }
}

// withSleep swaps the wait used for throttling and retries, for tests.
func withSleep(f func(context.Context, time.Duration) error) Option {
	return func(o *options) { o.sleep = f }
}

var descriptors = []Descriptor{
	{
		ID:         VendorInboxKit,
		Label:      "InboxKit",
		Website:    "https://inboxkit.com",
		KeyHelpURL: "https://app.inboxkit.com/settings/api",
		Fields:     []Field{apiKeyField("Settings > API & Integrations in the InboxKit dashboard. Mailboxes from every workspace the key reaches are listed.")},
		Verified:   true,
	},
	{
		ID:         VendorZapmail,
		Label:      "Zapmail",
		Website:    "https://zapmail.ai",
		KeyHelpURL: "https://docs.zapmail.ai/zapmail-docs-825990m0",
		Fields:     []Field{apiKeyField("Settings > Integrations > API in the Zapmail dashboard. Mailboxes from every workspace the key reaches are listed.")},
		Verified:   true,
	},
	{
		ID:         VendorMailforge,
		Label:      "Mailforge",
		Website:    "https://mailforge.ai",
		KeyHelpURL: "https://api.mailforge.ai/swagger/index.html",
		Fields:     []Field{apiKeyField("Settings > API keys in the Mailforge dashboard.")},
		Verified:   true,
	},
	{
		ID:         VendorInfraforge,
		Label:      "Infraforge",
		Website:    "https://infraforge.ai",
		KeyHelpURL: "https://api.infraforge.ai/public/swagger/index.html",
		Fields:     []Field{apiKeyField("Settings > API keys in the Infraforge dashboard. Mailboxes from every workspace are listed.")},
		Verified:   true,
	},
	{
		ID:         VendorMaildoso,
		Label:      "Maildoso",
		Website:    "https://maildoso.com",
		KeyHelpURL: "https://app.maildoso.com/settings",
		Fields:     []Field{apiKeyField("Settings > API Keys in the Maildoso dashboard (a personal access token).")},
		Verified:   true,
	},
	{
		ID:         VendorCheapInboxes,
		Label:      "Cheap Inboxes",
		Website:    "https://cheapinboxes.com",
		KeyHelpURL: "https://cheapinboxes.com",
		Fields:     []Field{apiKeyField("Integrations > API in the Cheap Inboxes dashboard (starts with ci_live_).")},
		Verified:   true,
	},
	{
		ID:         VendorScaledMail,
		Label:      "ScaledMail",
		Website:    "https://scaledmail.com",
		KeyHelpURL: "https://app.scaledmail.com/settings",
		Fields: []Field{
			apiKeyField("Settings in the ScaledMail dashboard. Mailboxes from every organization the key reaches are listed."),
			{Key: FieldOrganizationID, Label: "Organization ID", Legacy: true},
		},
		Verified: true,
	},
}

func apiKeyField(help string) Field {
	return Field{Key: FieldAPIKey, Label: "API key", Secret: true, Required: true, Help: help}
}

// Descriptors returns every supported vendor in a stable order.
func Descriptors() []Descriptor {
	out := make([]Descriptor, len(descriptors))
	for i, d := range descriptors {
		d.Fields = append([]Field(nil), d.Fields...)
		out[i] = d
	}
	return out
}

// Lookup returns one vendor's descriptor.
func Lookup(id string) (Descriptor, bool) {
	for _, d := range Descriptors() {
		if d.ID == id {
			return d, true
		}
	}
	return Descriptor{}, false
}

// New builds a client for a vendor from the fields the customer entered, keyed by Field.Key.
func New(vendor string, fields map[string]string, opts ...Option) (Client, error) {
	d, ok := Lookup(vendor)
	if !ok {
		return nil, ErrUnknownVendor
	}
	vals := make(map[string]string, len(d.Fields))
	for _, f := range d.Fields {
		v := strings.TrimSpace(fields[f.Key])
		if v == "" && f.Required && !f.Legacy {
			return nil, fmt.Errorf("%w: %s: %s is required", ErrInvalidConfig, vendor, f.Label)
		}
		if strings.ContainsFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return nil, fmt.Errorf("%w: %s: %s contains invalid characters", ErrInvalidConfig, vendor, f.Label)
		}
		vals[f.Key] = v
	}
	o := options{sleep: sleepCtx}
	for _, opt := range opts {
		opt(&o)
	}
	if o.httpClient == nil {
		o.httpClient = defaultHTTPClient()
	}
	switch vendor {
	case VendorInboxKit:
		return newInboxKit(vals, o), nil
	case VendorZapmail:
		return newZapmail(vals, o), nil
	case VendorMailforge:
		return newForge(VendorMailforge, "https://api.mailforge.ai/public", vals, o), nil
	case VendorInfraforge:
		return newForge(VendorInfraforge, "https://api.infraforge.ai/public", vals, o), nil
	case VendorMaildoso:
		return newMaildoso(vals, o), nil
	case VendorCheapInboxes:
		return newCheapInboxes(vals, o), nil
	case VendorScaledMail:
		return newScaledMail(vals, o), nil
	}
	return nil, ErrUnknownVendor
}

func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		// A redirect would replay vendor auth headers to wherever it points.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

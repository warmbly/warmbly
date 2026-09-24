// Package vendorconn connects a workspace to the inbox vendors it buys
// mailboxes from, so they import without a CSV and reconnect themselves when
// the vendor rotates a password.
package vendorconn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/mailboximport"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/mailvendor"
	"github.com/warmbly/warmbly/internal/repository"
)

// Stable codes for refusals a client branches on.
const (
	ErrIDUnknownVendor = "mailbox_vendor_unknown"
	ErrIDInvalidFields = "mailbox_vendor_invalid_fields"
	ErrIDRateLimited   = "mailbox_vendor_rate_limited"
	ErrIDUnavailable   = "mailbox_vendor_unavailable"
	ErrIDNoWorkspace   = "mailbox_vendor_no_workspace"
)

// Mailboxes is the mailbox store's side.
type Mailboxes interface {
	FindManyInOrganization(ctx context.Context, orgID uuid.UUID, emails []string) (map[string]models.EmailRef, *errx.Error)
	SetVendorLink(ctx context.Context, accountID, connectionID uuid.UUID, vendorMailboxID string) *errx.Error
	GetSMTPCredentials(ctx context.Context, emailAccountID uuid.UUID) (*repository.SMTPCredentials, *errx.Error)
}

// Reconnector replaces a mailbox's credentials after verifying them.
type Reconnector interface {
	UpdateSMTPIMAPCredentials(ctx context.Context, orgID *uuid.UUID, accountID uuid.UUID, creds *models.SmtpImap) (*models.Email, *errx.Error)
}

// Importer starts a background import of picked mailboxes.
type Importer interface {
	CreateFromList(ctx context.Context, in mailboximport.ListInput) (*models.MailboxImport, *errx.Error)
}

type Service struct {
	repo      repository.VendorConnectionRepository
	cipher    cipher.CipherService
	mailboxes Mailboxes
	reconnect Reconnector
	importer  Importer
	cache     *cache.Cache
	newClient func(vendor string, fields map[string]string) (mailvendor.Client, error)

	// clients keeps one client per connection a while, so a list the vendor
	// returned once serves every row of an import instead of being fetched per row.
	clients sync.Map // connection id + credential hash -> *cachedClient
	// domainCache keeps each connection's domain list a few minutes.
	domainCache domainCache
}

type cachedClient struct {
	client  mailvendor.Client
	created time.Time
}

const clientTTL = 15 * time.Minute

type Deps struct {
	Repo      repository.VendorConnectionRepository
	Cipher    cipher.CipherService
	Mailboxes Mailboxes
	Reconnect Reconnector
	Importer  Importer
	Cache     *cache.Cache
	// NewClient overrides the vendor client (tests).
	NewClient func(vendor string, fields map[string]string) (mailvendor.Client, error)
}

func NewService(d Deps) *Service {
	s := &Service{repo: d.Repo, cipher: d.Cipher, mailboxes: d.Mailboxes, reconnect: d.Reconnect, importer: d.Importer, cache: d.Cache, newClient: d.NewClient}
	if s.newClient == nil {
		s.newClient = func(vendor string, fields map[string]string) (mailvendor.Client, error) {
			return mailvendor.New(vendor, fields)
		}
	}
	return s
}

// SetImporter attaches the import service after construction; the two depend on each other.
func (s *Service) SetImporter(i Importer) { s.importer = i }

// VendorField is one credential a vendor asks for, as the dashboard renders it.
type VendorField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Secret   bool   `json:"secret"`
	Required bool   `json:"required"`
	Help     string `json:"help,omitempty"`
}

// Vendor is one supported inbox vendor.
type Vendor struct {
	ID         string        `json:"id"`
	Label      string        `json:"label"`
	Website    string        `json:"website"`
	KeyHelpURL string        `json:"key_help_url"`
	Fields     []VendorField `json:"fields"`
}

// Catalog lists every supported vendor and what connecting it asks for.
func (s *Service) Catalog() []Vendor {
	ds := mailvendor.Descriptors()
	out := make([]Vendor, 0, len(ds))
	for _, d := range ds {
		v := Vendor{ID: d.ID, Label: d.Label, Website: d.Website, KeyHelpURL: d.KeyHelpURL}
		for _, f := range d.Fields {
			if f.Legacy {
				continue
			}
			v.Fields = append(v.Fields, VendorField{Key: f.Key, Label: f.Label, Secret: f.Secret, Required: f.Required, Help: f.Help})
		}
		out = append(out, v)
	}
	return out
}

// vendorError turns a vendor client failure into what the person can do about it.
func vendorError(label string, err error) *errx.Error {
	switch {
	case errors.Is(err, mailvendor.ErrUnauthorized):
		return errx.NewWithIdentifier(errx.BadRequest, mailboximport.ErrIDVendorUnauthorized,
			label+" did not accept this API key. Check that it is current and was copied in full.")
	case errors.Is(err, mailvendor.ErrNoWorkspace):
		return errx.NewWithIdentifier(errx.BadRequest, ErrIDNoWorkspace, label+" shows no workspace for this API key. Create one at "+label+", then try again.")
	case errors.Is(err, mailvendor.ErrRateLimited):
		return errx.NewWithIdentifier(errx.TooManyRequests, ErrIDRateLimited, label+" is rate limiting requests. Try again in a minute.")
	case errors.Is(err, mailvendor.ErrInvalidConfig):
		return errx.NewWithIdentifier(errx.BadRequest, ErrIDInvalidFields, "Fill in every required field for "+label+".")
	case errors.Is(err, mailvendor.ErrUnknownVendor):
		return errx.NewWithIdentifier(errx.BadRequest, ErrIDUnknownVendor, "This vendor is not supported.")
	case errors.Is(err, mailvendor.ErrUnsupported):
		return errx.NewWithIdentifier(errx.BadRequest, ErrIDVendorCannot, label+" cannot do this through its API.")
	case errors.Is(err, mailvendor.ErrNotFound):
		return errx.NewWithIdentifier(errx.BadRequest, ErrIDUnavailable, label+" no longer has this mailbox.")
	}
	return errx.NewWithIdentifier(errx.BadRequest, ErrIDUnavailable, label+" did not answer as expected. Try again shortly.")
}

func labelOf(vendor string) string {
	if d, ok := mailvendor.Lookup(vendor); ok {
		return d.Label
	}
	return vendor
}

// CreateInput is a new vendor connection.
type CreateInput struct {
	Vendor string            `json:"vendor"`
	Label  string            `json:"label"`
	Fields map[string]string `json:"fields"`
}

// Create verifies the key against the vendor, then stores it sealed with the workspace's key.
func (s *Service) Create(ctx context.Context, orgID, userID uuid.UUID, in CreateInput) (*models.VendorConnection, *errx.Error) {
	fields := trimFields(in.Fields)
	client, err := s.newClient(in.Vendor, fields)
	if err != nil {
		return nil, vendorError(labelOf(in.Vendor), err)
	}
	if err := client.Verify(ctx); err != nil {
		return nil, vendorError(labelOf(in.Vendor), err)
	}
	sealed, xerr := s.seal(ctx, orgID, fields)
	if xerr != nil {
		return nil, xerr
	}
	c := &models.VendorConnection{ID: uuid.New(), OrganizationID: orgID, Vendor: in.Vendor, Label: cleanLabel(in.Label, in.Vendor), Credentials: sealed}
	if err := s.repo.Create(ctx, c, userID); err != nil {
		return nil, errx.InternalError()
	}
	c.Credentials = ""
	return c, nil
}

// Update renames a connection and, when fields are given, replaces its key after verifying it.
func (s *Service) Update(ctx context.Context, orgID, id uuid.UUID, in CreateInput) (*models.VendorConnection, *errx.Error) {
	c, xerr := s.get(ctx, orgID, id)
	if xerr != nil {
		return nil, xerr
	}
	sealed := ""
	if len(in.Fields) > 0 {
		fields := trimFields(in.Fields)
		client, err := s.newClient(c.Vendor, fields)
		if err != nil {
			return nil, vendorError(labelOf(c.Vendor), err)
		}
		if err := client.Verify(ctx); err != nil {
			return nil, vendorError(labelOf(c.Vendor), err)
		}
		if sealed, xerr = s.seal(ctx, orgID, fields); xerr != nil {
			return nil, xerr
		}
	}
	label := c.Label
	if strings.TrimSpace(in.Label) != "" {
		label = cleanLabel(in.Label, c.Vendor)
	}
	if _, err := s.repo.Update(ctx, orgID, id, label, sealed); err != nil {
		return nil, errx.InternalError()
	}
	s.forget(id)
	out, xerr := s.get(ctx, orgID, id)
	if xerr != nil {
		return nil, xerr
	}
	out.Credentials = ""
	return out, nil
}

func trimFields(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = strings.TrimSpace(v)
	}
	return out
}

func cleanLabel(label, vendor string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		label = labelOf(vendor)
	}
	if r := []rune(label); len(r) > 80 {
		label = string(r[:80])
	}
	return label
}

func (s *Service) seal(ctx context.Context, orgID uuid.UUID, fields map[string]string) (string, *errx.Error) {
	ciph, err := s.cipher.Cipher(ctx, orgID)
	if err != nil {
		errs.CaptureException(err)
		return "", errx.InternalError()
	}
	raw, _ := json.Marshal(fields)
	sealed, err := ciph.Encrypt(ctx, string(raw))
	if err != nil {
		errs.CaptureException(err)
		return "", errx.InternalError()
	}
	return sealed, nil
}

func (s *Service) get(ctx context.Context, orgID, id uuid.UUID) (*models.VendorConnection, *errx.Error) {
	c, err := s.repo.Get(ctx, orgID, id)
	if err != nil {
		return nil, errx.InternalError()
	}
	if c == nil {
		return nil, errx.ErrNotFound
	}
	return c, nil
}

func (s *Service) List(ctx context.Context, orgID uuid.UUID) ([]models.VendorConnection, *errx.Error) {
	list, err := s.repo.List(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	return list, nil
}

// Delete forgets the key. Mailboxes it brought in stay, without automatic reconnects.
func (s *Service) Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error {
	ok, err := s.repo.Delete(ctx, orgID, id)
	if err != nil {
		return errx.InternalError()
	}
	if !ok {
		return errx.ErrNotFound
	}
	s.forget(id)
	return nil
}

// client is the connection's vendor client, reused for a while so one list serves many rows.
func (s *Service) client(ctx context.Context, c *models.VendorConnection) (mailvendor.Client, *errx.Error) {
	sum := sha256.Sum256([]byte(c.Credentials))
	key := c.ID.String() + ":" + hex.EncodeToString(sum[:8])
	if v, ok := s.clients.Load(key); ok {
		cc := v.(*cachedClient)
		if time.Since(cc.created) < clientTTL {
			return cc.client, nil
		}
		s.clients.Delete(key)
	}
	// An archive imported without credentials brings the connection but not its key.
	if c.Credentials == "" {
		_ = s.repo.SetStatus(ctx, c.ID, "invalid", "This connection has no API key on this instance. Update the key.")
		return nil, errx.NewWithIdentifier(errx.BadRequest, mailboximport.ErrIDVendorUnauthorized,
			"This "+labelOf(c.Vendor)+" connection has no API key on this instance. Update the key to use it.")
	}
	ciph, err := s.cipher.Cipher(ctx, c.OrganizationID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	raw, err := ciph.Decrypt(ctx, c.Credentials)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	var fields map[string]string
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return nil, errx.InternalError()
	}
	client, err := s.newClient(c.Vendor, fields)
	if err != nil {
		return nil, vendorError(labelOf(c.Vendor), err)
	}
	s.clients.Store(key, &cachedClient{client: client, created: time.Now()})
	return client, nil
}

// forget drops a connection's cached clients and domain list, so a changed or removed key is never used again.
func (s *Service) forget(id uuid.UUID) {
	s.forgetDomains(id)
	prefix := id.String() + ":"
	s.clients.Range(func(k, _ any) bool {
		if strings.HasPrefix(k.(string), prefix) {
			s.clients.Delete(k)
		}
		return true
	})
}

// failed records a refused key on the connection, so the list shows it.
func (s *Service) failed(ctx context.Context, c *models.VendorConnection, err error) *errx.Error {
	xerr := vendorError(labelOf(c.Vendor), err)
	if errors.Is(err, mailvendor.ErrUnauthorized) {
		_ = s.repo.SetStatus(ctx, c.ID, "invalid", xerr.Message)
		s.forget(c.ID)
	}
	return xerr
}

// Mailboxes lists the vendor account's mailboxes, marking the ones already in the workspace.
func (s *Service) Mailboxes(ctx context.Context, orgID, id uuid.UUID) ([]models.VendorMailbox, *errx.Error) {
	c, xerr := s.get(ctx, orgID, id)
	if xerr != nil {
		return nil, xerr
	}
	client, xerr := s.client(ctx, c)
	if xerr != nil {
		return nil, xerr
	}
	list, err := client.List(ctx)
	if err != nil {
		return nil, s.failed(ctx, c, err)
	}
	_ = s.repo.SetStatus(ctx, c.ID, "active", "")
	out := make([]models.VendorMailbox, 0, len(list))
	emails := make([]string, 0, len(list))
	for _, m := range list {
		if m.Email == "" {
			continue
		}
		out = append(out, models.VendorMailbox{
			ID: m.ID, Email: m.Email, Name: strings.TrimSpace(m.FirstName + " " + m.LastName),
			Domain: m.Domain, Provider: m.Provider, Status: m.Status, Workspace: m.Workspace,
		})
		emails = append(emails, m.Email)
	}
	existing, xerr := s.mailboxes.FindManyInOrganization(ctx, orgID, emails)
	if xerr != nil {
		return nil, xerr
	}
	for i := range out {
		if ref, ok := existing[strings.ToLower(out[i].Email)]; ok {
			id := ref.ID
			out[i].Connected, out[i].AccountID = true, &id
		}
	}
	return out, nil
}

// ImportInput picks mailboxes from a vendor account.
type ImportInput struct {
	MailboxIDs []string                    `json:"mailbox_ids"`
	All        bool                        `json:"all"`
	Options    models.MailboxImportOptions `json:"options"`
}

// Import starts a background import of the picked vendor mailboxes. Their
// credentials are read from the vendor as each row is worked, never earlier.
func (s *Service) Import(ctx context.Context, orgID, userID, id uuid.UUID, in ImportInput) (*models.MailboxImport, *errx.Error) {
	if s.importer == nil {
		return nil, errx.InternalError()
	}
	c, xerr := s.get(ctx, orgID, id)
	if xerr != nil {
		return nil, xerr
	}
	boxes, xerr := s.Mailboxes(ctx, orgID, id)
	if xerr != nil {
		return nil, xerr
	}
	want := map[string]bool{}
	for _, m := range in.MailboxIDs {
		want[m] = true
	}
	rows := make([]mailboximport.ListRow, 0)
	for _, m := range boxes {
		if !want[m.ID] && !(in.All && !m.Connected) {
			continue
		}
		cid := c.ID
		rows = append(rows, mailboximport.ListRow{
			Email: m.Email, Name: m.Name, VendorConnectionID: &cid, VendorMailboxID: m.ID, VendorProvider: m.Provider,
		})
	}
	return s.importer.CreateFromList(ctx, mailboximport.ListInput{
		OrgID: orgID, UserID: userID, Source: "vendor", Vendor: c.Vendor, Label: c.Label, Rows: rows, Options: in.Options,
	})
}

// credentials reads one vendor mailbox's credentials.
func (s *Service) credentials(ctx context.Context, orgID, id uuid.UUID, mailboxID, email, provider string) (mailvendor.Credentials, *errx.Error) {
	c, xerr := s.get(ctx, orgID, id)
	if xerr != nil {
		return mailvendor.Credentials{}, xerr
	}
	client, xerr := s.client(ctx, c)
	if xerr != nil {
		return mailvendor.Credentials{}, xerr
	}
	domain := ""
	if at := strings.LastIndex(email, "@"); at > 0 {
		domain = email[at+1:]
	}
	cr, err := client.Credentials(ctx, mailvendor.Mailbox{ID: mailboxID, Email: email, Domain: domain, Provider: provider})
	if err != nil {
		return mailvendor.Credentials{}, s.failed(ctx, c, err)
	}
	return cr, nil
}

// Fields is a vendor mailbox's credentials as import columns, so the row is judged like a file row.
func (s *Service) Fields(ctx context.Context, orgID, connectionID uuid.UUID, mailboxID, email, provider string) (map[models.MailboxImportField]string, *errx.Error) {
	cr, xerr := s.credentials(ctx, orgID, connectionID, mailboxID, email, provider)
	if xerr != nil {
		return nil, xerr
	}
	f := map[models.MailboxImportField]string{
		models.ImportFieldPassword:    cr.Password,
		models.ImportFieldAppPassword: cr.AppPassword,
	}
	leg := func(e *mailvendor.Endpoint, host, port, sec, user, pw models.MailboxImportField) {
		if e == nil {
			return
		}
		f[host], f[sec], f[user], f[pw] = e.Host, e.Security, e.Username, e.Password
		if e.Port > 0 {
			f[port] = strconv.Itoa(e.Port)
		}
	}
	leg(cr.SMTP, models.ImportFieldSMTPHost, models.ImportFieldSMTPPort, models.ImportFieldSMTPSecurity, models.ImportFieldSMTPUsername, models.ImportFieldSMTPPassword)
	leg(cr.IMAP, models.ImportFieldIMAPHost, models.ImportFieldIMAPPort, models.ImportFieldIMAPSecurity, models.ImportFieldIMAPUsername, models.ImportFieldIMAPPassword)
	return f, nil
}

// Link remembers which vendor mailbox a mailbox came from.
func (s *Service) Link(ctx context.Context, accountID, connectionID uuid.UUID, mailboxID string) {
	if xerr := s.mailboxes.SetVendorLink(ctx, accountID, connectionID, mailboxID); xerr != nil {
		log.Warn().Str("email_account_id", accountID.String()).Msg("vendor connection: recording the link failed")
	}
}

// reconnectCooldown keeps a mailbox whose new password still fails from being retried every pass.
const reconnectCooldown = 6 * time.Hour

// StartReconnect watches vendor mailboxes that stopped on a credential error
// and, when the vendor now has a different password, reconnects them with it.
func (s *Service) StartReconnect(ctx context.Context) {
	jobrun.Loop(ctx, "mailbox_vendor_reconnect", 15*time.Minute, false, func(ctx context.Context) error {
		cands, err := s.repo.ReconnectCandidates(ctx, 50)
		if err != nil {
			return err
		}
		for _, cand := range cands {
			s.reconnectOne(ctx, cand)
		}
		return nil
	})
}

func (s *Service) reconnectOne(ctx context.Context, cand repository.VendorReconnect) {
	if s.cache != nil {
		ok, err := s.cache.SetNX(ctx, "vendor_reconnect:"+cand.AccountID.String(), "1", reconnectCooldown).Result()
		if err != nil || !ok {
			return
		}
	}
	cr, xerr := s.credentials(ctx, cand.OrgID, cand.ConnectionID, cand.VendorMailboxID, cand.Email, "")
	if xerr != nil {
		return
	}
	stored, xerr := s.mailboxes.GetSMTPCredentials(ctx, cand.AccountID)
	if xerr != nil || stored == nil {
		return
	}
	pick := func(e *mailvendor.Endpoint) string {
		if e != nil && e.Password != "" {
			return e.Password
		}
		if cr.AppPassword != "" {
			return cr.AppPassword
		}
		return cr.Password
	}
	smtpPw, imapPw := pick(cr.SMTP), pick(cr.IMAP)
	if smtpPw == "" || (smtpPw == stored.SMTPPassword && imapPw == stored.IMAPPassword) {
		return
	}
	creds := &models.SmtpImap{
		SMTP: &models.Service{Host: stored.SMTPHost, Port: stored.SMTPPort, Username: stored.SMTPUser, Password: smtpPw, Security: stored.SMTPSecurity},
		IMAP: &models.Service{Host: stored.IMAPHost, Port: stored.IMAPPort, Username: stored.IMAPUser, Password: imapPw, Security: stored.IMAPSecurity},
	}
	orgID := cand.OrgID
	if _, xerr := s.reconnect.UpdateSMTPIMAPCredentials(ctx, &orgID, cand.AccountID, creds); xerr != nil {
		log.Info().Str("email_account_id", cand.AccountID.String()).Str("code", xerr.ResponseCode()).Msg("vendor reconnect: the vendor's current password did not sign in either")
		return
	}
	log.Info().Str("email_account_id", cand.AccountID.String()).Msg("vendor reconnect: mailbox reconnected with the vendor's current password")
}

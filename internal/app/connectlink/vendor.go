package connectlink

import (
	"context"
	"sort"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/vendorconn"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/formwire"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
)

// A client who bought mailboxes from an inbox vendor connects the vendor account from the link
// instead of one mailbox at a time. Two steps, so the key is typed once and never returned:
// the key is checked and its mailboxes listed while it is held sealed under a ticket, then the
// mailboxes the client picks are imported in the creator's name, exactly as the dashboard's
// vendor import would.

// Vendors is the slice of the vendor connection service a link needs.
type Vendors interface {
	Catalog() []vendorconn.Vendor
	Hold(ctx context.Context, orgID uuid.UUID, scope string, in vendorconn.CreateInput) (string, []models.VendorMailbox, *errx.Error)
	Redeem(ctx context.Context, orgID uuid.UUID, scope, ticket string) (*vendorconn.CreateInput, *errx.Error)
	Create(ctx context.Context, orgID, userID uuid.UUID, in vendorconn.CreateInput) (*models.VendorConnection, *errx.Error)
	ImportPicked(ctx context.Context, orgID, userID, id uuid.UUID, in vendorconn.ImportInput, limit ...int) (*models.MailboxImport, []string, *errx.Error)
	Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error
}

func (s *Service) vendorCatalog() []formwire.ConnectVendor {
	list := s.d.Vendors.Catalog()
	out := make([]formwire.ConnectVendor, 0, len(list))
	for _, v := range list {
		cv := formwire.ConnectVendor{ID: v.ID, Label: v.Label, KeyHelpURL: v.KeyHelpURL}
		for _, f := range v.Fields {
			cv.Fields = append(cv.Fields, formwire.ConnectVendorField{Key: f.Key, Label: f.Label, Secret: f.Secret, Required: f.Required, Help: f.Help})
		}
		out = append(out, cv)
	}
	return out
}

// VendorAccountInput is a vendor key a client typed, with the signals only the portal service sees.
type VendorAccountInput struct {
	Vendor       string
	Fields       map[string]string
	CaptchaToken string
	RemoteIP     string
}

// VendorMailboxes checks a client's vendor key and lists what it reaches. The key stays on the server under the ticket.
func (s *Service) VendorMailboxes(ctx context.Context, link *models.ClientConnectLink, in VendorAccountInput) (*formwire.ConnectVendorMailboxes, *errx.Error) {
	if s.d.Captcha != nil {
		if xerr := s.d.Captcha.Verify(ctx, in.CaptchaToken, in.RemoteIP); xerr != nil {
			return nil, ErrCaptcha
		}
	}
	if xerr := s.admit(ctx, link, models.ConnectLinkVendor); xerr != nil {
		return nil, xerr
	}
	ticket, boxes, xerr := s.d.Vendors.Hold(ctx, link.OrganizationID, link.ID.String(), vendorconn.CreateInput{Vendor: in.Vendor, Fields: in.Fields})
	if xerr != nil {
		return nil, xerr
	}
	out := &formwire.ConnectVendorMailboxes{
		Ticket: ticket, Vendor: in.Vendor, Remaining: link.MaxMailboxes - link.MailboxesConnected,
		Mailboxes: make([]formwire.ConnectVendorMailbox, 0, len(boxes)),
	}
	for _, m := range boxes {
		out.Mailboxes = append(out.Mailboxes, formwire.ConnectVendorMailbox{ID: m.ID, Email: m.Email, Name: m.Name, Provider: m.Provider, Connected: m.Connected})
	}
	sort.SliceStable(out.Mailboxes, func(i, j int) bool { return out.Mailboxes[i].Email < out.Mailboxes[j].Email })
	return out, nil
}

// VendorImportInput picks mailboxes from the account a ticket holds.
type VendorImportInput struct {
	Ticket     string
	MailboxIDs []string
	RemoteIP   string
}

// VendorResult is an import a client started through a link.
type VendorResult struct {
	Link    *models.ClientConnectLink
	ActorID uuid.UUID
	Import  *models.MailboxImport
	Vendor  string
	Emails  []string
}

// VendorImport connects the mailboxes a client picked. Their slots on the link are taken before
// anything is imported and are kept even for a mailbox that later fails to connect, so a link is
// never good for more attempts than it allows.
func (s *Service) VendorImport(ctx context.Context, link *models.ClientConnectLink, in VendorImportInput) (*VendorResult, *errx.Error) {
	if xerr := s.admit(ctx, link, models.ConnectLinkVendor); xerr != nil {
		return nil, xerr
	}
	ids := make([]string, 0, len(in.MailboxIDs))
	seen := map[string]bool{}
	for _, id := range in.MailboxIDs {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, ErrNoMailboxes
	}
	// Refused before the ticket is spent, so picking fewer needs no key again.
	if len(ids) > link.MaxMailboxes-link.MailboxesConnected {
		return nil, ErrTooManyMailboxes
	}
	orgID := link.OrganizationID
	if a, xerr := s.d.Workspaces.MailboxAllowance(ctx, orgID); xerr != nil {
		return nil, xerr
	} else if a != nil && !a.CanAdd(len(ids)) {
		return nil, errx.MailboxAllowanceReached(a.Used, allowanceOf(a), a.Paid)
	}
	actor, xerr := s.creatorCanConnect(ctx, link)
	if xerr != nil {
		return nil, xerr
	}
	account, xerr := s.d.Vendors.Redeem(ctx, orgID, link.ID.String(), in.Ticket)
	if xerr != nil {
		return nil, xerr
	}
	reserved, xerr := s.d.Repo.ReserveMany(ctx, orgID, link.ID, len(ids))
	if xerr != nil {
		return nil, xerr
	}
	if !reserved {
		fresh, gerr := s.d.Repo.Get(ctx, orgID, link.ID)
		if gerr != nil || fresh == nil {
			return nil, ErrRevoked
		}
		if se := statusError(fresh.StatusAt(s.d.Now())); se != nil {
			return nil, se
		}
		return nil, ErrTooManyMailboxes
	}
	giveBack := func(n int) {
		if rerr := s.d.Repo.ReleaseMany(ctx, orgID, link.ID, n); rerr != nil {
			log.Warn().Str("link_id", link.ID.String()).Msg("connect link: could not give back slots after a failed vendor import")
		}
	}
	account.Label = vendorLabel(s.d.Vendors.Catalog(), account.Vendor) + " (from a client)"
	conn, xerr := s.d.Vendors.Create(ctx, orgID, actor, *account)
	if xerr != nil {
		giveBack(len(ids))
		return nil, xerr
	}
	opts := models.MailboxImportOptions{}
	for _, t := range link.TagIDs {
		opts.Settings.TagIDs = append(opts.Settings.TagIDs, t.String())
	}
	if link.Warmup {
		on := true
		opts.Settings.Warmup = &on
	}
	imp, emails, xerr := s.d.Vendors.ImportPicked(ctx, orgID, actor, conn.ID, vendorconn.ImportInput{MailboxIDs: ids, Options: opts}, len(ids))
	if xerr == nil && len(emails) == 0 {
		xerr = ErrNoMailboxes
	}
	if xerr != nil {
		giveBack(len(ids))
		if derr := s.d.Vendors.Delete(ctx, orgID, conn.ID); derr != nil {
			log.Warn().Str("link_id", link.ID.String()).Msg("connect link: a vendor connection with no import was left behind")
		}
		return nil, xerr
	}
	// An id the vendor no longer lists took a slot nothing will use.
	if extra := len(ids) - len(emails); extra > 0 {
		giveBack(extra)
	}
	link.MailboxesConnected += len(emails)
	hashed := ipHash(link.ID, in.RemoteIP)
	for _, email := range emails {
		use := &models.ClientConnectLinkUse{LinkID: link.ID, Email: email, Provider: string(models.ConnectLinkVendor)}
		if xerr := s.d.Repo.RecordUse(ctx, use, orgID, hashed); xerr != nil {
			log.Warn().Str("link_id", link.ID.String()).Msg("connect link: a vendor mailbox is importing but its use was not recorded")
		}
	}
	s.notifyVendorImport(ctx, link, actor, len(emails))
	return &VendorResult{Link: s.withStatus(link), ActorID: actor, Import: imp, Vendor: account.Vendor, Emails: emails}, nil
}

func vendorLabel(catalog []vendorconn.Vendor, id string) string {
	for _, v := range catalog {
		if v.ID == id {
			return v.Label
		}
	}
	return "Inbox vendor"
}

func (s *Service) notifyVendorImport(ctx context.Context, link *models.ClientConnectLink, creator uuid.UUID, n int) {
	if s.d.Notifier == nil {
		return
	}
	via := "your connect link"
	if label := displayname.Displayable(link.Label); label != "" {
		via = "your connect link \"" + label + "\""
	}
	orgID := link.OrganizationID
	what := "a mailbox"
	if n != 1 {
		what = "mailboxes"
	}
	s.d.Notifier.Notify(ctx, creator, &orgID, models.NotifTeamActivity,
		"A client is connecting "+what+" from a vendor account",
		"An import was started through "+via+". Open Mailboxes to follow it; rows that need you are listed under Imports.",
		"/app/emails",
		map[string]any{"connect_link_id": link.ID.String(), "mailboxes": n})
}

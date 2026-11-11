package vendorconn

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// A key typed by someone who is not a member (a client on a connect page) is held for a few
// minutes while they pick mailboxes, so it is sent once and never returned to the page. The hold
// is sealed with the workspace's key, bound to the workspace and to what it was made for, and
// read exactly once.

const holdTTL = 15 * time.Minute

// ErrHoldExpired answers a ticket that was used, has expired or was made for something else.
var ErrHoldExpired = errx.NewWithIdentifier(errx.BadRequest, "mailbox_vendor_hold_expired",
	"That took too long or was already used. Enter the key again.")

type heldAccount struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	Scope          string    `json:"scope"`
	Vendor         string    `json:"vendor"`
	Sealed         string    `json:"sealed"`
}

func holdKey(ticket string) string { return "mailbox_vendor_hold:" + ticket }

// Peek verifies a key with the vendor and lists what it reaches, marking the mailboxes already in the workspace. Nothing is stored.
func (s *Service) Peek(ctx context.Context, orgID uuid.UUID, in CreateInput) ([]models.VendorMailbox, string, *errx.Error) {
	raw, xerr := s.verified(ctx, in.Vendor, in.Fields)
	if xerr != nil {
		return nil, "", xerr
	}
	client, err := s.newClient(in.Vendor, trimFields(in.Fields))
	if err != nil {
		return nil, "", vendorError(labelOf(in.Vendor), err)
	}
	list, err := client.List(ctx)
	if err != nil {
		return nil, "", vendorError(labelOf(in.Vendor), err)
	}
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
		return nil, "", xerr
	}
	for i := range out {
		if ref, ok := existing[strings.ToLower(out[i].Email)]; ok {
			id := ref.ID
			out[i].Connected, out[i].AccountID = true, &id
		}
	}
	return out, raw, nil
}

// Hold verifies a key, lists its mailboxes and keeps the key sealed under a ticket for scope.
func (s *Service) Hold(ctx context.Context, orgID uuid.UUID, scope string, in CreateInput) (string, []models.VendorMailbox, *errx.Error) {
	if s.cache == nil {
		return "", nil, errx.New(errx.ServiceUnavailable, "inbox vendors are not available here")
	}
	boxes, raw, xerr := s.Peek(ctx, orgID, in)
	if xerr != nil {
		return "", nil, xerr
	}
	sealed, xerr := s.sealFor(ctx, orgID, raw)
	if xerr != nil {
		return "", nil, xerr
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, errx.InternalError()
	}
	ticket := hex.EncodeToString(buf)
	held := heldAccount{OrganizationID: orgID, Scope: scope, Vendor: in.Vendor, Sealed: sealed}
	if err := s.cache.SetJSON(ctx, holdKey(ticket), held, holdTTL); err != nil {
		errs.CaptureException(err)
		return "", nil, errx.InternalError()
	}
	return ticket, boxes, nil
}

// Redeem returns the key a ticket holds, once, to the workspace and scope it was made for.
func (s *Service) Redeem(ctx context.Context, orgID uuid.UUID, scope, ticket string) (*CreateInput, *errx.Error) {
	if s.cache == nil || len(ticket) != 64 {
		return nil, ErrHoldExpired
	}
	raw, err := s.cache.GetDel(ctx, holdKey(ticket)).Result()
	if err != nil || raw == "" {
		return nil, ErrHoldExpired
	}
	var held heldAccount
	if json.Unmarshal([]byte(raw), &held) != nil || held.OrganizationID != orgID || held.Scope != scope {
		return nil, ErrHoldExpired
	}
	ciph, err := s.cipher.Cipher(ctx, orgID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	plain, err := ciph.Decrypt(ctx, held.Sealed)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	var fields map[string]string
	if json.Unmarshal([]byte(plain), &fields) != nil {
		return nil, errx.InternalError()
	}
	return &CreateInput{Vendor: held.Vendor, Fields: fields}, nil
}

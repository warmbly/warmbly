package vendorconn

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/repository"
)

// An agency holds a vendor account once and uses it in any client workspace.
// The account's key is sealed with the instance key; using it in a client makes
// an ordinary connection in that workspace, sealed with the workspace's own key,
// so imports, reconnects and rotated passwords work there exactly as they do for
// a key the workspace was given directly. Only agency staff browse or import
// from such a connection: the account lists every client's mailboxes.

// Agencies is the slice of the agency service that proves staff.
type Agencies interface {
	Get(ctx context.Context, agencyID, userID uuid.UUID) (*models.Agency, *errx.Error)
}

// AgencyClients lists an agency's client workspaces with the caller's membership in each.
type AgencyClients interface {
	ClientMemberships(ctx context.Context, agencyID, userID uuid.UUID) ([]models.AgencyClientMembership, error)
}

var (
	errAgencyVendorsOff    = errx.New(errx.ServiceUnavailable, "agency vendor accounts are not available on this instance")
	errAgencyVendorManage  = errx.NewWithIdentifier(errx.Forbidden, "agency_forbidden", "Only the agency's owner and admins can do that.")
	errAgencyVendorClient  = errx.NewWithIdentifier(errx.NotFound, "agency_client_not_found", "That workspace is not one of this agency's clients.")
	errAgencyVendorMailbox = errx.NewWithIdentifier(errx.Forbidden, "agency_client_mailboxes_forbidden", "You cannot manage mailboxes in this client's workspace.")
	// ErrAgencyVendorStaffOnly refuses a workspace member who is not on the agency's staff.
	ErrAgencyVendorStaffOnly = errx.NewWithIdentifier(errx.Forbidden, "agency_vendor_staff_only",
		"This vendor account belongs to your agency. Only the agency's staff can list or import its mailboxes.")
	// ErrAgencyVendorManaged refuses a key change made in one workspace.
	ErrAgencyVendorManaged = errx.NewWithIdentifier(errx.Conflict, "agency_vendor_managed",
		"This vendor account belongs to your agency. Change its key under Agency > Mailboxes.")
)

// WireAgencies turns on agency-held vendor accounts.
func (s *Service) WireAgencies(repo repository.AgencyVendorRepository, agencies Agencies, clients AgencyClients, instance *encrypt.Encrypter) {
	s.agencyRepo, s.agencies, s.agencyClients, s.instance = repo, agencies, clients, instance
}

func (s *Service) agencyStaff(ctx context.Context, agencyID, userID uuid.UUID, manage bool) *errx.Error {
	if s.agencyRepo == nil || s.agencies == nil || s.instance == nil {
		return errAgencyVendorsOff
	}
	a, xerr := s.agencies.Get(ctx, agencyID, userID)
	if xerr != nil {
		return xerr
	}
	if manage && !a.Role.CanManage() {
		return errAgencyVendorManage
	}
	return nil
}

// verified checks the fields against the vendor and returns them ready to seal.
func (s *Service) verified(ctx context.Context, vendor string, in map[string]string) (string, *errx.Error) {
	fields := trimFields(in)
	client, err := s.newClient(vendor, fields)
	if err != nil {
		return "", vendorError(labelOf(vendor), err)
	}
	if err := client.Verify(ctx); err != nil {
		return "", vendorError(labelOf(vendor), err)
	}
	raw, _ := json.Marshal(fields)
	return string(raw), nil
}

func (s *Service) sealInstance(raw string) (string, *errx.Error) {
	sealed, err := s.instance.Encrypt(raw)
	if err != nil {
		errs.CaptureException(err)
		return "", errx.InternalError()
	}
	return sealed, nil
}

// sealFor seals an already-marshalled field map with a workspace's key.
func (s *Service) sealFor(ctx context.Context, orgID uuid.UUID, raw string) (string, *errx.Error) {
	ciph, err := s.cipher.Cipher(ctx, orgID)
	if err != nil {
		errs.CaptureException(err)
		return "", errx.InternalError()
	}
	sealed, err := ciph.Encrypt(ctx, raw)
	if err != nil {
		errs.CaptureException(err)
		return "", errx.InternalError()
	}
	return sealed, nil
}

// AgencyAccounts lists the agency's vendor accounts and the clients each is used in.
func (s *Service) AgencyAccounts(ctx context.Context, agencyID, userID uuid.UUID) ([]models.AgencyVendorAccount, *errx.Error) {
	if xerr := s.agencyStaff(ctx, agencyID, userID, false); xerr != nil {
		return nil, xerr
	}
	list, err := s.agencyRepo.List(ctx, agencyID)
	if err != nil {
		return nil, errx.InternalError()
	}
	return list, nil
}

// CreateAgencyAccount verifies the key against the vendor, then stores it sealed with the instance key.
func (s *Service) CreateAgencyAccount(ctx context.Context, agencyID, userID uuid.UUID, in CreateInput) (*models.AgencyVendorAccount, *errx.Error) {
	if xerr := s.agencyStaff(ctx, agencyID, userID, true); xerr != nil {
		return nil, xerr
	}
	raw, xerr := s.verified(ctx, in.Vendor, in.Fields)
	if xerr != nil {
		return nil, xerr
	}
	sealed, xerr := s.sealInstance(raw)
	if xerr != nil {
		return nil, xerr
	}
	a := &models.AgencyVendorAccount{ID: uuid.New(), AgencyID: agencyID, Vendor: in.Vendor, Label: cleanLabel(in.Label, in.Vendor), Credentials: sealed}
	if err := s.agencyRepo.Create(ctx, a, userID); err != nil {
		return nil, errx.InternalError()
	}
	a.Credentials = ""
	return a, nil
}

func (s *Service) agencyAccount(ctx context.Context, agencyID, id uuid.UUID) (*models.AgencyVendorAccount, *errx.Error) {
	a, err := s.agencyRepo.Get(ctx, agencyID, id)
	if err != nil {
		return nil, errx.InternalError()
	}
	if a == nil {
		return nil, errx.ErrNotFound
	}
	return a, nil
}

// UpdateAgencyAccount renames an account and, when fields are given, replaces its key after
// verifying it, in the account and in every client workspace that uses it.
func (s *Service) UpdateAgencyAccount(ctx context.Context, agencyID, userID, id uuid.UUID, in CreateInput) (*models.AgencyVendorAccount, *errx.Error) {
	if xerr := s.agencyStaff(ctx, agencyID, userID, true); xerr != nil {
		return nil, xerr
	}
	a, xerr := s.agencyAccount(ctx, agencyID, id)
	if xerr != nil {
		return nil, xerr
	}
	raw, sealed := "", ""
	if len(in.Fields) > 0 {
		if raw, xerr = s.verified(ctx, a.Vendor, in.Fields); xerr != nil {
			return nil, xerr
		}
		if sealed, xerr = s.sealInstance(raw); xerr != nil {
			return nil, xerr
		}
	}
	label := a.Label
	if strings.TrimSpace(in.Label) != "" {
		label = cleanLabel(in.Label, a.Vendor)
	}
	if _, err := s.agencyRepo.Update(ctx, agencyID, id, label, sealed); err != nil {
		return nil, errx.InternalError()
	}
	links, err := s.agencyRepo.Links(ctx, id)
	if err != nil {
		return nil, errx.InternalError()
	}
	for _, l := range links {
		workspaceSealed := ""
		if raw != "" {
			if workspaceSealed, xerr = s.sealFor(ctx, l.OrganizationID, raw); xerr != nil {
				return nil, xerr
			}
		}
		if _, err := s.repo.Update(ctx, l.OrganizationID, l.ConnectionID, label, workspaceSealed); err != nil {
			return nil, errx.InternalError()
		}
		s.forget(l.ConnectionID)
	}
	return s.agencyAccountView(ctx, agencyID, id)
}

// agencyAccountView is one account as the list shows it.
func (s *Service) agencyAccountView(ctx context.Context, agencyID, id uuid.UUID) (*models.AgencyVendorAccount, *errx.Error) {
	list, err := s.agencyRepo.List(ctx, agencyID)
	if err != nil {
		return nil, errx.InternalError()
	}
	for i := range list {
		if list[i].ID == id {
			return &list[i], nil
		}
	}
	return nil, errx.ErrNotFound
}

// DeleteAgencyAccount forgets the key everywhere it is used. Mailboxes it brought in stay, without automatic reconnects.
func (s *Service) DeleteAgencyAccount(ctx context.Context, agencyID, userID, id uuid.UUID) *errx.Error {
	if xerr := s.agencyStaff(ctx, agencyID, userID, true); xerr != nil {
		return xerr
	}
	links, err := s.agencyRepo.Links(ctx, id)
	if err != nil {
		return errx.InternalError()
	}
	ok, err := s.agencyRepo.Delete(ctx, agencyID, id)
	if err != nil {
		return errx.InternalError()
	}
	if !ok {
		return errx.ErrNotFound
	}
	for _, l := range links {
		s.forget(l.ConnectionID)
	}
	return nil
}

// UseAgencyAccount gives a client workspace its connection to one of the agency's accounts, making it
// the first time, and returns it. The caller has to manage mailboxes in that workspace.
func (s *Service) UseAgencyAccount(ctx context.Context, agencyID, userID, id, orgID uuid.UUID) (*models.VendorConnection, *errx.Error) {
	if xerr := s.agencyStaff(ctx, agencyID, userID, false); xerr != nil {
		return nil, xerr
	}
	memberships, err := s.agencyClients.ClientMemberships(ctx, agencyID, userID)
	if err != nil {
		return nil, errx.InternalError()
	}
	var client *models.AgencyClientMembership
	for i := range memberships {
		if memberships[i].OrganizationID == orgID {
			client = &memberships[i]
		}
	}
	if client == nil {
		return nil, errAgencyVendorClient
	}
	if !client.Can(models.PermManageEmails) {
		return nil, errAgencyVendorMailbox
	}
	a, xerr := s.agencyAccount(ctx, agencyID, id)
	if xerr != nil {
		return nil, xerr
	}
	linked, err := s.agencyRepo.Linked(ctx, orgID, id)
	if err != nil {
		return nil, errx.InternalError()
	}
	if linked == nil {
		raw, derr := s.instance.Decrypt(a.Credentials)
		if derr != nil {
			errs.CaptureException(derr)
			return nil, errx.InternalError()
		}
		sealed, xerr := s.sealFor(ctx, orgID, raw)
		if xerr != nil {
			return nil, xerr
		}
		c := &models.VendorConnection{ID: uuid.New(), OrganizationID: orgID, Vendor: a.Vendor, Label: a.Label, Credentials: sealed}
		made, err := s.agencyRepo.CreateLinked(ctx, c, id, userID)
		if err != nil {
			return nil, errx.InternalError()
		}
		linked = c
		if !made {
			// Another request made it first, or the workspace left the agency in between.
			if linked, err = s.agencyRepo.Linked(ctx, orgID, id); err != nil {
				return nil, errx.InternalError()
			}
			if linked == nil {
				return nil, errAgencyVendorClient
			}
		}
	}
	linked.Credentials = ""
	return linked, nil
}

// GuardAgencyAccount refuses a workspace member who is not on the staff of the agency a connection
// was made from, and a key change made in the workspace. A connection of the workspace's own passes.
func (s *Service) GuardAgencyAccount(ctx context.Context, orgID, id, userID uuid.UUID, changesKey bool) *errx.Error {
	c, xerr := s.get(ctx, orgID, id)
	if xerr != nil {
		return xerr
	}
	if c.AgencyConnectionID == nil {
		return nil
	}
	if s.agencyRepo == nil || s.agencies == nil {
		return ErrAgencyVendorStaffOnly
	}
	agencyID, ok, err := s.agencyRepo.AgencyOf(ctx, *c.AgencyConnectionID)
	if err != nil {
		return errx.InternalError()
	}
	if !ok {
		return ErrAgencyVendorStaffOnly
	}
	if _, xerr := s.agencies.Get(ctx, agencyID, userID); xerr != nil {
		return ErrAgencyVendorStaffOnly
	}
	if changesKey {
		return ErrAgencyVendorManaged
	}
	return nil
}

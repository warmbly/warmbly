package auth

import (
	"context"
	"net/mail"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/user"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type invitedSignupUsers struct {
	recordingUserRepo
	empty bool
}

func (r *invitedSignupUsers) IsEmpty(context.Context) (bool, error) { return r.empty, nil }

type invitedSignupOrgs struct {
	organization.OrganizationService
	err             *errx.Error
	invited         *models.User
	personalCreated bool
}

func (o *invitedSignupOrgs) CreateInvitedUser(_ context.Context, _ string, email *mail.Address, _ string) (*models.User, *models.OrganizationMember, *errx.Error) {
	if o.err != nil {
		return nil, nil, o.err
	}
	o.invited = &models.User{ID: uuid.New(), Email: email.Address}
	return o.invited, &models.OrganizationMember{OrganizationID: uuid.New(), UserID: o.invited.ID}, nil
}

func (o *invitedSignupOrgs) Get(context.Context, uuid.UUID) (*models.Organization, *errx.Error) {
	return nil, errx.InternalError()
}

func (o *invitedSignupOrgs) Create(context.Context, uuid.UUID, string, string) (*models.Organization, *errx.Error) {
	o.personalCreated = true
	return nil, nil
}

type invitedSignupCache struct {
	user.UserService
	err   *errx.Error
	saved *models.User
}

func (c *invitedSignupCache) SaveUser(_ context.Context, u *models.User) *errx.Error {
	c.saved = u
	return c.err
}

func TestCreateAccountInvitationFailureDoesNotProvision(t *testing.T) {
	internal := errx.InternalError()
	for _, tc := range []struct {
		name         string
		registration string
		empty        bool
		err          *errx.Error
		fallback     bool
	}{
		{"invite-only invalid", config.RegistrationInviteOnly, false, errx.ErrInvitationInvalid, false},
		{"invite-only database failure", config.RegistrationInviteOnly, false, internal, false},
		{"open invalid", config.RegistrationOpen, false, errx.ErrInvitationInvalid, true},
		{"open database failure", config.RegistrationOpen, false, internal, false},
		{"open duplicate account", config.RegistrationOpen, false, errx.ErrAccountExists, false},
		{"first account invalid", config.RegistrationInviteOnly, true, errx.ErrInvitationInvalid, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &invitedSignupUsers{empty: tc.empty}
			orgs := &invitedSignupOrgs{err: tc.err}
			cache := &invitedSignupCache{}
			s := &authService{policy: &config.AuthPolicy{Registration: tc.registration}, userRepository: repo, userService: cache, organizationService: orgs}
			u, err := s.createAccount(context.Background(), "Invitee@acme.com", "hash", SignupAttribution{Invite: "token"}, SignupOrigin{})
			if tc.fallback {
				if err != nil || u == nil || repo.created == nil || !orgs.personalCreated {
					t.Fatalf("open signup did not create a personal workspace: user=%v error=%v", u, err)
				}
				return
			}
			if err != tc.err || u != nil || repo.created != nil || cache.saved != nil || repo.recordHits != 0 || orgs.personalCreated {
				t.Fatalf("failed invitation performed provisioning: user=%v error=%v repository=%+v cache=%+v", u, err, repo, cache)
			}
		})
	}
}

func TestCreateAccountInvitedUserIsAlreadyProvisioned(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  *errx.Error
	}{{"cache available", nil}, {"cache unavailable", errx.InternalError()}} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &invitedSignupUsers{}
			orgs := &invitedSignupOrgs{}
			cache := &invitedSignupCache{err: tc.err}
			s := &authService{policy: &config.AuthPolicy{Registration: config.RegistrationInviteOnly}, userRepository: repo, userService: cache, organizationService: orgs}
			u, err := s.createAccount(context.Background(), "Invitee@acme.com", "hash", SignupAttribution{Invite: "token"}, SignupOrigin{})
			if err != nil || u != orgs.invited || u.Email != "invitee@acme.com" || cache.saved != u || repo.created != nil || orgs.personalCreated || repo.recordHits != 1 {
				t.Fatalf("invited signup was not completed exactly once: user=%v error=%v", u, err)
			}
		})
	}
}

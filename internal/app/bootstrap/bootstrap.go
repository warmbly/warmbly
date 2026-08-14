// Package bootstrap provisions the first owner of a fresh install.
//
// Without it the only documented path was: register through the public form,
// read the emailed code out of a mail catcher, then run a psql UPDATE from the
// host. That is three manual steps, it depends on working mail, and the window
// between "instance is reachable" and "an admin exists" is claimable by anyone
// who finds the URL first, which is a recurring CVE class in self-hosted
// software.
//
// Two mechanisms, both standard in the field:
//
//   - WARMBLY_BOOTSTRAP_EMAIL plus WARMBLY_BOOTSTRAP_PASSWORD_HASH, read only
//     while the users table is empty (authentik, n8n and NocoDB all do this).
//   - Otherwise a single-use setup token printed once to the logs, which is
//     Zulip's realm-creation link, invalidated the moment it is used.
package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/trial"
	"github.com/warmbly/warmbly/internal/app/user"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
	"github.com/warmbly/warmbly/internal/repository"
)

// SetupTokenTTL bounds the window in which the printed token can be claimed.
const SetupTokenTTL = 24 * time.Hour

const setupTokenKey = "bootstrap:setup_token"

type Service struct {
	users     repository.UserRepository
	userSvc   user.UserService
	orgSvc    organization.OrganizationService
	trialSvc  trial.TrialService
	adminRepo repository.AdminRepository
	cache     *cache.Cache
}

func NewService(
	users repository.UserRepository,
	userSvc user.UserService,
	orgSvc organization.OrganizationService,
	trialSvc trial.TrialService,
	adminRepo repository.AdminRepository,
	cache *cache.Cache,
) *Service {
	return &Service{
		users:     users,
		userSvc:   userSvc,
		orgSvc:    orgSvc,
		trialSvc:  trialSvc,
		adminRepo: adminRepo,
		cache:     cache,
	}
}

// Run is called once at boot, after migrations. It is a no-op on an instance
// that already has users, so it is safe on every restart.
func (s *Service) Run(ctx context.Context) error {
	empty, err := s.users.IsEmpty(ctx)
	if err != nil {
		return fmt.Errorf("bootstrap: checking for existing users: %w", err)
	}
	if !empty {
		return nil
	}

	email := strings.TrimSpace(os.Getenv("WARMBLY_BOOTSTRAP_EMAIL"))
	if email != "" {
		return s.createOwner(ctx, email)
	}

	return s.printSetupToken(ctx)
}

func (s *Service) createOwner(ctx context.Context, address string) error {
	parsed, err := mail.ParseAddress(address)
	if err != nil {
		return fmt.Errorf("bootstrap: WARMBLY_BOOTSTRAP_EMAIL is not a valid address: %w", err)
	}

	hash := strings.TrimSpace(os.Getenv("WARMBLY_BOOTSTRAP_PASSWORD_HASH"))
	if hash == "" {
		// A plaintext password is accepted as a convenience, with the same
		// warning authentik gives: environment variables leak through process
		// listings, orchestrator APIs and crash dumps.
		plain := os.Getenv("WARMBLY_BOOTSTRAP_PASSWORD")
		if plain == "" {
			return fmt.Errorf("bootstrap: WARMBLY_BOOTSTRAP_EMAIL is set but neither WARMBLY_BOOTSTRAP_PASSWORD_HASH nor WARMBLY_BOOTSTRAP_PASSWORD is")
		}
		log.Printf("Warning: WARMBLY_BOOTSTRAP_PASSWORD is a plaintext password in an environment variable. Prefer WARMBLY_BOOTSTRAP_PASSWORD_HASH.")
		hashed, herr := argon2.Hash(plain)
		if herr != nil {
			return fmt.Errorf("bootstrap: hashing the password: %w", herr)
		}
		hash = hashed
	}

	u, uerr := s.users.CreateUser(ctx, parsed, hash)
	if uerr != nil {
		return fmt.Errorf("bootstrap: creating the owner: %w", uerr)
	}
	if err := s.userSvc.SaveUser(ctx, u); err != nil {
		return fmt.Errorf("bootstrap: saving the owner: %w", err)
	}

	orgName := strings.TrimSpace(os.Getenv("WARMBLY_BOOTSTRAP_ORG"))
	if orgName == "" {
		orgName = defaultOrgName(u.FirstName)
	}
	org, orgErr := s.orgSvc.Create(ctx, u.ID, orgName)
	if orgErr != nil {
		return fmt.Errorf("bootstrap: creating the organization: %w", orgErr)
	}
	if s.trialSvc != nil {
		_ = s.trialSvc.StartFreeTrialWithOrg(ctx, u.ID, org.ID)
	}

	// The bootstrap account is the platform operator, so it gets full admin.
	// This is the only path that grants admin without an existing admin.
	if err := s.adminRepo.GrantBootstrapAdmin(ctx, u.ID, uint32(models.AllAdminPermissions)); err != nil {
		return fmt.Errorf("bootstrap: granting admin: %w", err)
	}

	log.Printf("Bootstrap: created owner %s with full admin permissions and organization %q.", parsed.Address, orgName)
	return nil
}

// printSetupToken mints a single-use claim token for the first account and
// prints it once. Only its hash is stored, so reading the database does not
// yield a usable token, and claiming it deletes the key.
func (s *Service) printSetupToken(ctx context.Context) error {
	if s.cache == nil {
		return nil
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	token := hex.EncodeToString(buf)

	if err := s.cache.SetEx(ctx, setupTokenKey, hashToken(token), SetupTokenTTL).Err(); err != nil {
		return fmt.Errorf("bootstrap: storing the setup token: %w", err)
	}

	url := fmt.Sprintf("%s/setup?token=%s", config.AppBaseURL(), token)
	log.Printf("\n"+
		"┌──────────────────────────────────────────────────────────────────────\n"+
		"│ No accounts exist yet. Claim this instance with the link below.\n"+
		"│\n"+
		"│   %s\n"+
		"│\n"+
		"│ Single use, valid for %s. It is printed only once, and only its hash\n"+
		"│ is stored. Set WARMBLY_BOOTSTRAP_EMAIL and\n"+
		"│ WARMBLY_BOOTSTRAP_PASSWORD_HASH to provision the owner without it.\n"+
		"└──────────────────────────────────────────────────────────────────────\n",
		url, SetupTokenTTL)

	return nil
}

// Required reports whether this instance still needs to be claimed. Drives the
// setup page and the `setup_required` flag on GET /auth/config.
func (s *Service) Required(ctx context.Context) bool {
	empty, err := s.users.IsEmpty(ctx)
	return err == nil && empty
}

// Claim exchanges the printed setup token for the owner account.
//
// The token is consumed before the account is created, not after: two requests
// racing with the same token must not both succeed, and Redis GETDEL is the
// atomic step that decides which one wins.
func (s *Service) Claim(ctx context.Context, token, address, password, firstName, lastName string) (*models.User, *errx.Error) {
	if s.cache == nil || token == "" {
		return nil, errx.ErrToken
	}

	parsed, perr := mail.ParseAddress(address)
	if perr != nil {
		return nil, errx.ErrEmail
	}
	if !crypt.ValidatePassword(password) {
		return nil, errx.ErrPassword
	}

	// Refuse on an instance that already has accounts, even with a valid
	// token: a stale link out of an old log must never mint a second owner.
	empty, eerr := s.users.IsEmpty(ctx)
	if eerr != nil {
		sentry.CaptureException(eerr)
		return nil, errx.InternalError()
	}
	if !empty {
		return nil, errx.New(errx.Forbidden, "this instance has already been set up")
	}

	stored, gerr := s.cache.GetDel(ctx, setupTokenKey).Result()
	if gerr != nil || stored == "" || stored != hashToken(token) {
		// Put a valid-but-losing token back only when the value did not match,
		// so a typo does not burn the real one.
		if gerr == nil && stored != "" && stored != hashToken(token) {
			_ = s.cache.SetEx(ctx, setupTokenKey, stored, SetupTokenTTL).Err()
		}
		return nil, errx.ErrToken
	}

	hash, herr := argon2.Hash(password)
	if herr != nil {
		sentry.CaptureException(herr)
		return nil, errx.InternalError()
	}

	u, uerr := s.users.CreateUser(ctx, parsed, hash)
	if uerr != nil {
		sentry.CaptureException(uerr)
		return nil, errx.InternalError()
	}
	if firstName != "" {
		if err := s.users.UpdateProfile(ctx, u.ID, firstName, lastName); err == nil {
			u.FirstName, u.LastName = firstName, lastName
		}
	}
	if err := s.userSvc.SaveUser(ctx, u); err != nil {
		return nil, err
	}

	orgName := strings.TrimSpace(os.Getenv("WARMBLY_BOOTSTRAP_ORG"))
	if orgName == "" {
		orgName = defaultOrgName(u.FirstName)
	}
	org, orgErr := s.orgSvc.Create(ctx, u.ID, orgName)
	if orgErr != nil {
		sentry.CaptureException(orgErr)
		return nil, errx.InternalError()
	}
	if s.trialSvc != nil {
		_ = s.trialSvc.StartFreeTrialWithOrg(ctx, u.ID, org.ID)
	}

	if err := s.adminRepo.GrantBootstrapAdmin(ctx, u.ID, uint32(models.AllAdminPermissions)); err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	log.Printf("Setup: %s claimed this instance and is now its owner and platform admin.", parsed.Address)
	return u, nil
}

func defaultOrgName(firstName string) string {
	if firstName == "" {
		return "My Organization"
	}
	return firstName + "'s Organization"
}

func hashToken(token string) string {
	// argon2 is overkill for a 256-bit random token, so the shared SHA-256
	// helper is the right cost here.
	return crypt.SHA256(token)
}

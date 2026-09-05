// Package cliauth is the device-code sign-in the `warmbly` CLI uses.
//
// The CLI has no credential of its own, so it opens a handshake, shows the
// user an eight character code, and polls. A signed-in member approves the
// code in the browser, and the approval mints an ordinary API key through the
// existing service: same hash, same scopes, same revocation, visible under
// Settings > API keys like every other key. Nothing here is a new credential
// type and nothing here is a new authentication path.
package cliauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/apikey"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/user"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

var (
	ErrCodeNotFound   = errx.NewWithIdentifier(errx.NotFound, "cli_auth_code_not_found", "That code is unknown or has expired. Run `warmbly auth login` again for a fresh one.")
	ErrCodeNotPending = errx.NewWithIdentifier(errx.Conflict, "cli_auth_code_used", "That code has already been used.")
	ErrBadRequest     = errx.NewWithIdentifier(errx.BadRequest, "cli_auth_request", "device_code is required.")
	ErrBadScopes      = errx.NewWithIdentifier(errx.BadRequest, "cli_auth_scopes", "The requested scopes include bits this instance does not grant.")
	ErrForbidden      = errx.NewWithIdentifier(errx.Forbidden, "cli_auth_forbidden", "Managing API keys is required to authorize a CLI in this workspace.")
)

type Service interface {
	// StartCode opens a handshake for a CLI that holds no key yet.
	StartCode(ctx context.Context, req models.CLIAuthStartRequest) (*models.CLIAuthStartResponse, *errx.Error)
	// PollCode is what the CLI calls until a member decides.
	PollCode(ctx context.Context, deviceCode string) (*models.CLIAuthPollResponse, *errx.Error)
	// DescribeCode is what the approving member sees before deciding.
	DescribeCode(ctx context.Context, userCode string) (*models.CLIAuthCode, *errx.Error)
	// ApproveCode mints the key into the named workspace.
	ApproveCode(ctx context.Context, userCode string, orgID, userID uuid.UUID) (*models.CLIAuthCode, *errx.Error)
	DenyCode(ctx context.Context, userCode string) *errx.Error
}

type service struct {
	repo   repository.CLIAuthRepository
	keys   apikey.APIKeyService
	orgs   organization.OrganizationService
	users  user.UserService
	orgRep repository.OrganizationRepository
}

func NewService(
	repo repository.CLIAuthRepository,
	keys apikey.APIKeyService,
	orgs organization.OrganizationService,
	users user.UserService,
	orgRep repository.OrganizationRepository,
) Service {
	return &service{repo: repo, keys: keys, orgs: orgs, users: users, orgRep: orgRep}
}

// Unambiguous alphabet: no 0/O, 1/I/L. Same as the pool link handshake, because
// both codes get read off one screen and typed into another.
const userCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func randomUserCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 0, 9)
	for i, v := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, userCodeAlphabet[int(v)%len(userCodeAlphabet)])
	}
	return string(out), nil
}

func randomDeviceCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashDeviceCode(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// NormalizeUserCode accepts any case, with or without the dash, so a user who
// retypes the code by hand is not punished for the formatting.
func NormalizeUserCode(raw string) string {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	raw = strings.ReplaceAll(raw, "-", "")
	raw = strings.ReplaceAll(raw, " ", "")
	if len(raw) != 8 {
		return raw
	}
	return raw[:4] + "-" + raw[4:]
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (s *service) StartCode(ctx context.Context, req models.CLIAuthStartRequest) (*models.CLIAuthStartResponse, *errx.Error) {
	req.ClientName = clip(req.ClientName, 60)
	if req.ClientName == "" {
		req.ClientName = "Warmbly CLI"
	}
	req.Hostname = clip(req.Hostname, 80)
	req.CLIVersion = clip(req.CLIVersion, 40)

	// An unknown bit would grant a scope the approval screen never showed.
	if req.Scopes&^models.AllAPIPermissionsMask != 0 {
		return nil, ErrBadScopes
	}
	if req.Scopes == 0 {
		req.Scopes = models.APIPermFullAccess
	}

	deviceCode, err := randomDeviceCode()
	if err != nil {
		return nil, errx.InternalError()
	}
	_ = s.repo.DeleteExpiredCodes(ctx)

	var code *models.CLIAuthCode
	for attempt := 0; attempt < 3; attempt++ {
		userCode, uerr := randomUserCode()
		if uerr != nil {
			return nil, errx.InternalError()
		}
		created, cerr := s.repo.CreateCode(ctx, hashDeviceCode(deviceCode), userCode, req, time.Now().Add(config.CLIAuthCodeTTLMinutes*time.Minute))
		if cerr == nil && created != nil {
			code = created
			break
		}
		// A user-code collision is the only expected failure; retry with a new one.
	}
	if code == nil {
		return nil, errx.InternalError()
	}

	verify := config.AppBaseURL() + "/cli"
	return &models.CLIAuthStartResponse{
		DeviceCode:              deviceCode,
		UserCode:                code.UserCode,
		VerificationURL:         verify,
		VerificationURLComplete: verify + "?code=" + url.QueryEscape(code.UserCode),
		ExpiresIn:               config.CLIAuthCodeTTLMinutes * 60,
		Interval:                config.CLIAuthPollIntervalSeconds,
	}, nil
}

func (s *service) PollCode(ctx context.Context, deviceCode string) (*models.CLIAuthPollResponse, *errx.Error) {
	deviceCode = strings.TrimSpace(deviceCode)
	if deviceCode == "" {
		return nil, ErrBadRequest
	}
	code, secret, err := s.repo.ClaimCode(ctx, hashDeviceCode(deviceCode))
	if err != nil {
		return nil, errx.InternalError()
	}
	if code == nil {
		// Expired and unknown are the same answer on purpose: a poller that
		// can tell them apart can probe for live handshakes.
		return nil, ErrCodeNotFound
	}
	res := &models.CLIAuthPollResponse{Status: code.Status}
	if secret == "" {
		return res, nil
	}

	res.Token = secret
	res.Scopes = code.Scopes
	res.ScopeNames = code.ScopeNames
	res.OrganizationID = code.OrganizationID

	// Identity is a convenience for `warmbly auth status`, not part of the
	// grant, so a lookup failure must not lose the user their token.
	if key, kerr := s.keys.ValidateKey(ctx, secret); kerr == nil && key != nil {
		res.APIKeyID = &key.ID
		res.UserID = &key.UserID
		if u, uerr := s.users.GetUser(ctx, key.UserID); uerr == nil && u != nil {
			res.UserEmail = u.Email
			res.UserName = strings.TrimSpace(u.FirstName + " " + u.LastName)
		}
	}
	if code.OrganizationID != nil && s.orgRep != nil {
		if org, oerr := s.orgRep.GetByID(ctx, *code.OrganizationID); oerr == nil && org != nil {
			res.OrganizationName = org.Name
		}
	}
	return res, nil
}

func (s *service) DescribeCode(ctx context.Context, userCode string) (*models.CLIAuthCode, *errx.Error) {
	code, err := s.repo.GetCodeByUserCode(ctx, NormalizeUserCode(userCode))
	if err != nil {
		return nil, errx.InternalError()
	}
	if code == nil {
		return nil, ErrCodeNotFound
	}
	return code, nil
}

func (s *service) ApproveCode(ctx context.Context, userCode string, orgID, userID uuid.UUID) (*models.CLIAuthCode, *errx.Error) {
	userCode = NormalizeUserCode(userCode)
	code, xerr := s.DescribeCode(ctx, userCode)
	if xerr != nil {
		return nil, xerr
	}
	if code.Status != models.CLIAuthCodePending {
		return nil, ErrCodeNotPending
	}

	allowed, xerr := s.orgs.HasPermission(ctx, orgID, userID, models.PermManageAPIKeys)
	if xerr != nil {
		return nil, xerr
	}
	if !allowed {
		return nil, ErrForbidden
	}

	// The key is named for the machine that asked, so Settings > API keys shows
	// which laptop a key belongs to and revoking the right one is possible.
	name := code.ClientName
	if code.Hostname != "" {
		name += " on " + code.Hostname
	}
	desc := fmt.Sprintf("Created by `warmbly auth login` for code %s", code.UserCode)
	created, xerr := s.keys.Create(ctx, orgID, userID, &models.CreateAPIKey{
		Name:        clip(name, 255),
		Description: &desc,
		Permissions: code.Scopes,
	})
	if xerr != nil {
		return nil, xerr
	}

	ok, err := s.repo.ApproveCode(ctx, userCode, orgID, userID, created.ID, created.Secret)
	if err != nil {
		return nil, errx.InternalError()
	}
	if !ok {
		// Someone approved or denied between the read and the write. The key
		// would otherwise be an orphan nobody asked for.
		_ = s.keys.Revoke(ctx, orgID, created.ID, "cli authorization was resolved elsewhere")
		return nil, ErrCodeNotPending
	}

	code.Status = models.CLIAuthCodeApproved
	code.OrganizationID = &orgID
	code.APIKeyID = &created.ID
	return code, nil
}

func (s *service) DenyCode(ctx context.Context, userCode string) *errx.Error {
	ok, err := s.repo.DenyCode(ctx, NormalizeUserCode(userCode))
	if err != nil {
		return errx.InternalError()
	}
	if !ok {
		return ErrCodeNotFound
	}
	return nil
}

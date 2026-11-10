package apikey

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

// AgencyKeyPrefix brands an agency API key; it shares no prefix with a workspace key or an OAuth token.
const AgencyKeyPrefix = "wmagency_"

// Stable codes an agency key is refused with.
const (
	CodeAgencyKeyHolderLeft     = "agency_key_holder_left"
	CodeAgencyKeyOrgRequired    = "agency_key_organization_required"
	CodeAgencyKeyNotClient      = "agency_key_not_client"
	CodeAgencyKeyNotAllowed     = "agency_key_not_allowed"
	CodeAgencyKeyIPNotAllowed   = "agency_key_ip_not_allowed"
	CodeAgencyKeyInvalidPerms   = "invalid_agency_key_permissions"
	CodeAgencyKeyNotFound       = "agency_api_key_not_found"
	agencyKeyCredentialsRevoked = "revoked by its creator with a password change or reset"
)

var (
	errAgencyKeyHolderLeft = errx.NewWithIdentifier(errx.Unauthorized, CodeAgencyKeyHolderLeft,
		"The staff member who created this agency API key is no longer an owner or admin of the agency. Create a new key.")
	errAgencyKeyNotClient = errx.NewWithIdentifier(errx.Forbidden, CodeAgencyKeyNotClient,
		"That workspace is not a client of this API key's agency.")
	errAgencyKeyNotFound = errx.NewWithIdentifier(errx.NotFound, CodeAgencyKeyNotFound, "That API key does not exist in this agency.")
	errAgencyKeyManage   = errx.NewWithIdentifier(errx.Forbidden, "agency_forbidden", "Only the agency's owner and admins can do that.")
	errAgencyNotFound    = errx.NewWithIdentifier(errx.NotFound, "agency_not_found", "That agency does not exist or you are not part of it.")
)

// AgencyStaff is the slice of the agency repository the key checks read.
type AgencyStaff interface {
	GetMember(ctx context.Context, agencyID, userID uuid.UUID) (*models.AgencyMember, error)
	GetClient(ctx context.Context, orgID uuid.UUID) (*models.AgencyClient, error)
}

// AgencyKeyService issues agency API keys and decides what each request with one may reach.
type AgencyKeyService struct {
	repo  repository.AgencyAPIKeyRepository
	staff AgencyStaff
	cache *cache.Cache
}

func NewAgencyKeyService(repo repository.AgencyAPIKeyRepository, staff AgencyStaff, c *cache.Cache) *AgencyKeyService {
	return &AgencyKeyService{repo: repo, staff: staff, cache: c}
}

func agencyKeyInternal(err error) *errx.Error {
	errs.CaptureException(err)
	return errx.InternalError()
}

// manager refuses anyone who is not an owner or admin of the agency.
func (s *AgencyKeyService) manager(ctx context.Context, agencyID, userID uuid.UUID) *errx.Error {
	m, err := s.staff.GetMember(ctx, agencyID, userID)
	if err != nil {
		return agencyKeyInternal(err)
	}
	if m == nil {
		return errAgencyNotFound
	}
	if !m.Role.CanManage() {
		return errAgencyKeyManage
	}
	return nil
}

func checkAgencyKeyPermissions(perms uint64) *errx.Error {
	if perms == 0 || perms&^models.AgencyKeyGrantable != 0 {
		return errx.NewWithIdentifier(errx.BadRequest, CodeAgencyKeyInvalidPerms,
			"permissions must be a non-zero set of the bits an agency key can hold")
	}
	return nil
}

func checkAgencyKeyName(name string) (string, *errx.Error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 255 {
		return "", errx.New(errx.BadRequest, "name must be between 1 and 255 characters")
	}
	return name, nil
}

func (s *AgencyKeyService) Create(ctx context.Context, agencyID, userID uuid.UUID, data *models.CreateAgencyAPIKey) (*models.AgencyAPIKeyWithSecret, *errx.Error) {
	if xerr := s.manager(ctx, agencyID, userID); xerr != nil {
		return nil, xerr
	}
	name, xerr := checkAgencyKeyName(data.Name)
	if xerr != nil {
		return nil, xerr
	}
	data.Name = name
	if xerr := checkAgencyKeyPermissions(data.Permissions); xerr != nil {
		return nil, xerr
	}
	if data.ExpiresAt != nil && !data.ExpiresAt.After(time.Now()) {
		return nil, errx.New(errx.BadRequest, "expires_at must be in the future")
	}
	if r := data.RateLimitPerMinute; r != nil && (*r < minRateLimitPerMinute || *r > maxRateLimitPerMinute) {
		return nil, errx.New(errx.BadRequest, fmt.Sprintf("rate_limit_per_minute must be between %d and %d", minRateLimitPerMinute, maxRateLimitPerMinute))
	}
	ips, xerr := validateAllowedIPs(data.AllowedIPs)
	if xerr != nil {
		return nil, xerr
	}
	data.AllowedIPs = ips
	raw, prefix, suffix, hash, err := generateKeyWith(AgencyKeyPrefix)
	if err != nil {
		return nil, agencyKeyInternal(err)
	}
	k, err := s.repo.Create(ctx, agencyID, userID, data, prefix, suffix, hash)
	if err != nil {
		return nil, agencyKeyInternal(err)
	}
	return &models.AgencyAPIKeyWithSecret{AgencyAPIKey: *k, Secret: raw}, nil
}

// List pages the agency's keys, newest first, behind an opaque cursor.
func (s *AgencyKeyService) List(ctx context.Context, agencyID, userID uuid.UUID, limit int, cursor string) (*models.AgencyAPIKeysResult, *errx.Error) {
	if xerr := s.manager(ctx, agencyID, userID); xerr != nil {
		return nil, xerr
	}
	at, id, xerr := paging.DecodeTimeCursor(cursor)
	if xerr != nil {
		return nil, xerr
	}
	var after *time.Time
	if id != uuid.Nil {
		after = &at
	}
	keys, err := s.repo.List(ctx, agencyID, limit+1, after, id)
	if err != nil {
		return nil, agencyKeyInternal(err)
	}
	res := &models.AgencyAPIKeysResult{Data: keys}
	if len(keys) > limit {
		res.Data = keys[:limit]
		last := res.Data[limit-1]
		res.Pagination = models.Pagination{HasMore: true, NextCursor: paging.EncodeTime(last.CreatedAt, last.ID)}
	}
	return res, nil
}

func (s *AgencyKeyService) Update(ctx context.Context, agencyID, userID, keyID uuid.UUID, data *models.UpdateAgencyAPIKey) (*models.AgencyAPIKey, *errx.Error) {
	if xerr := s.manager(ctx, agencyID, userID); xerr != nil {
		return nil, xerr
	}
	if data.Name == nil && data.AllowedIPs == nil {
		return nil, errx.ErrNotEnough
	}
	if data.Name != nil {
		name, xerr := checkAgencyKeyName(*data.Name)
		if xerr != nil {
			return nil, xerr
		}
		data.Name = &name
	}
	if data.AllowedIPs != nil {
		ips, xerr := validateAllowedIPs(data.AllowedIPs)
		if xerr != nil {
			return nil, xerr
		}
		data.AllowedIPs = ips
	}
	k, err := s.repo.Update(ctx, agencyID, keyID, data)
	if err != nil {
		return nil, agencyKeyInternal(err)
	}
	if k == nil {
		return nil, errAgencyKeyNotFound
	}
	return k, nil
}

// Revoke ends a key; revoking one already revoked succeeds, so a retry is safe.
func (s *AgencyKeyService) Revoke(ctx context.Context, agencyID, userID, keyID uuid.UUID) *errx.Error {
	if xerr := s.manager(ctx, agencyID, userID); xerr != nil {
		return xerr
	}
	found, err := s.repo.Revoke(ctx, agencyID, keyID, "Revoked from the agency")
	if err != nil {
		return agencyKeyInternal(err)
	}
	if !found {
		return errAgencyKeyNotFound
	}
	return nil
}

// RevokeUserCredentials ends every agency key userID created, with their password.
func (s *AgencyKeyService) RevokeUserCredentials(ctx context.Context, userID uuid.UUID) error {
	_, err := s.repo.RevokeCreatedBy(ctx, userID, agencyKeyCredentialsRevoked)
	return err
}

// Authenticate resolves a raw key to a live key whose creator still manages its agency.
func (s *AgencyKeyService) Authenticate(ctx context.Context, raw string) (*models.AgencyAPIKey, *errx.Error) {
	if !strings.HasPrefix(raw, AgencyKeyPrefix) {
		return nil, errx.ErrAuth
	}
	k, err := s.repo.GetActiveByHash(ctx, hashKey(raw))
	if err != nil {
		return nil, agencyKeyInternal(err)
	}
	if k == nil {
		return nil, errx.ErrAuth
	}
	m, err := s.staff.GetMember(ctx, k.AgencyID, k.CreatedBy)
	if err != nil {
		return nil, agencyKeyInternal(err)
	}
	if m == nil || !m.Role.CanManage() {
		return nil, errAgencyKeyHolderLeft
	}
	return k, nil
}

// RequireClient refuses a workspace that is not, at this moment, a client of the key's agency.
func (s *AgencyKeyService) RequireClient(ctx context.Context, k *models.AgencyAPIKey, orgID uuid.UUID) *errx.Error {
	c, err := s.staff.GetClient(ctx, orgID)
	if err != nil {
		return agencyKeyInternal(err)
	}
	if c == nil || c.AgencyID != k.AgencyID {
		return errAgencyKeyNotClient
	}
	return nil
}

// IPAllowed applies the key's address allowlist.
func (s *AgencyKeyService) IPAllowed(k *models.AgencyAPIKey, ip string) bool {
	return ipAllowed(k.AllowedIPs, ip)
}

// RateLimit is the key's minute window, shared with nothing else.
func (s *AgencyKeyService) RateLimit(ctx context.Context, k *models.AgencyAPIKey) (remaining, retryAfter int, allowed bool) {
	return minuteWindow(ctx, s.cache, "agencykey:rl:", k.ID, k.RateLimitPerMinute)
}

// Touch records the last call without holding the request.
func (s *AgencyKeyService) Touch(keyID uuid.UUID, ip string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.repo.TouchLastUsed(ctx, keyID, ip)
	}()
}

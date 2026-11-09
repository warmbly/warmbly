// Package appdirectory is the community app directory: OAuth apps a workspace
// publishes for other workspaces to install. A published listing is reachable
// by its link and shown in discovery only when an operator features it or
// enough workspaces use it; any change to what it shows ends a feature.
package appdirectory

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

const (
	// ErrCodeInvalidListing is the response code for a listing field that breaks a rule.
	ErrCodeInvalidListing = "invalid_listing"
	// ErrCodeSlugTaken is the response code for a slug another listing holds.
	ErrCodeSlugTaken = "listing_slug_taken"
	// ErrCodeNotListable is the response code for an app that cannot be listed.
	ErrCodeNotListable = "app_not_listable"
	// ErrCodeListingHidden is the response code for unpublishing a hidden listing.
	ErrCodeListingHidden = "listing_hidden"
	// ErrCodeDeveloperBlocked is the response code when an operator has blocked
	// this workspace or person from publishing apps.
	ErrCodeDeveloperBlocked = "developer_access_blocked"

	maxTagline     = 120
	maxDescription = 2000
	maxURL         = 2048
	maxReviewNote  = 1000
)

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,46}[a-z0-9]$`)

// reservedSlugs keeps a community listing from taking a built-in integration's
// name or the product's own.
var reservedSlugs = func() map[string]bool {
	out := map[string]bool{"warmbly": true, "official": true, "admin": true, "support": true}
	for _, p := range []models.IntegrationProvider{
		models.IntegrationHubSpot, models.IntegrationSalesforce, models.IntegrationPipedrive, models.IntegrationClose,
		models.IntegrationZapier, models.IntegrationMake, models.IntegrationN8N, models.IntegrationSlack,
		models.IntegrationDiscord, models.IntegrationCalendly, models.IntegrationCalCom, models.IntegrationGoogleSheets,
		models.IntegrationMillionVerifier, models.IntegrationCleanMyList,
		models.IntegrationGooglePlaces, models.IntegrationFoursquare, models.IntegrationOutscraper, models.IntegrationJina,
	} {
		out[string(p)] = true
		out[strings.ReplaceAll(string(p), "_", "-")] = true
	}
	return out
}()

type Service struct {
	repo  repository.AppDirectoryRepository
	oauth repository.OAuthRepository
}

func NewService(repo repository.AppDirectoryRepository, oauth repository.OAuthRepository) *Service {
	return &Service{repo: repo, oauth: oauth}
}

func invalid(msg string) *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, ErrCodeInvalidListing, msg)
}

// listableApp loads the organization's app and refuses one that has no public face.
func (s *Service) listableApp(ctx context.Context, orgID, appID uuid.UUID) (*models.OAuthApplication, *errx.Error) {
	app, err := s.oauth.GetApplication(ctx, orgID, appID)
	if err != nil {
		return nil, errx.New(errx.Internal, "lookup failed")
	}
	if app == nil {
		return nil, errx.New(errx.NotFound, "application not found")
	}
	if app.DynamicallyRegistered {
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrCodeNotListable, "a dynamically registered client cannot be listed")
	}
	return app, nil
}

// GetListing returns the app's listing, or nil when it is not published.
func (s *Service) GetListing(ctx context.Context, orgID, appID uuid.UUID) (*models.AppListing, *errx.Error) {
	if _, xerr := s.listableApp(ctx, orgID, appID); xerr != nil {
		return nil, xerr
	}
	l, err := s.repo.GetListing(ctx, orgID, appID)
	if err != nil {
		return nil, errx.New(errx.Internal, "lookup failed")
	}
	return l, nil
}

// SaveOutcome says what a save did, so only a real change is audited.
type SaveOutcome int

const (
	SaveUnchanged SaveOutcome = iota
	SaveCreated
	SaveUpdated
)

// SaveListing publishes or edits the app's listing. An edit that changes
// nothing keeps its status; a real change ends a feature, and a hidden listing stays hidden.
func (s *Service) SaveListing(ctx context.Context, orgID, userID, appID uuid.UUID, w models.AppListingWrite) (*models.AppListing, SaveOutcome, *errx.Error) {
	app, xerr := s.listableApp(ctx, orgID, appID)
	if xerr != nil {
		return nil, SaveUnchanged, xerr
	}
	if block, err := s.oauth.DeveloperBlock(ctx, orgID, userID); err != nil {
		return nil, SaveUnchanged, errx.New(errx.Internal, "lookup failed")
	} else if block != nil {
		return nil, SaveUnchanged, errx.NewWithIdentifier(errx.Forbidden, ErrCodeDeveloperBlocked, "publishing apps is blocked for this workspace")
	}
	if app.SuspendedAt != nil {
		return nil, SaveUnchanged, errx.NewWithIdentifier(errx.BadRequest, ErrCodeNotListable, "this app is suspended")
	}
	if app.Status != models.OAuthAppActive {
		return nil, SaveUnchanged, errx.NewWithIdentifier(errx.BadRequest, ErrCodeNotListable, "enable the app before publishing it")
	}
	next, xerr := normalize(w)
	if xerr != nil {
		return nil, SaveUnchanged, xerr
	}
	next.ApplicationID = appID
	next.OrganizationID = orgID

	current, err := s.repo.GetListing(ctx, orgID, appID)
	if err != nil {
		return nil, SaveUnchanged, errx.New(errx.Internal, "lookup failed")
	}
	if current != nil && sameContent(current, next) {
		return current, SaveUnchanged, nil
	}
	if err := s.repo.SaveListing(ctx, next); err != nil {
		if errors.Is(err, repository.ErrAppListingSlugTaken) {
			return nil, SaveUnchanged, errx.NewWithIdentifier(errx.Conflict, ErrCodeSlugTaken, "another app already uses this link")
		}
		return nil, SaveUnchanged, errx.New(errx.Internal, "save failed")
	}
	if current == nil {
		return next, SaveCreated, nil
	}
	return next, SaveUpdated, nil
}

// DeleteListing unpublishes the app. Installs are untouched: a workspace that
// authorized it keeps its grant until it revokes it.
func (s *Service) DeleteListing(ctx context.Context, orgID, appID uuid.UUID) *errx.Error {
	if _, xerr := s.listableApp(ctx, orgID, appID); xerr != nil {
		return xerr
	}
	current, err := s.repo.GetListing(ctx, orgID, appID)
	if err != nil {
		return errx.New(errx.Internal, "lookup failed")
	}
	if current != nil && current.Status == models.AppListingHidden {
		return errx.NewWithIdentifier(errx.Conflict, ErrCodeListingHidden, "a hidden listing stays until the instance's administrators restore it")
	}
	if err := s.repo.DeleteListing(ctx, orgID, appID); err != nil {
		return errx.New(errx.Internal, "delete failed")
	}
	return nil
}

// Browse lists the apps shown in discovery: featured first, then most installed.
func (s *Service) Browse(ctx context.Context, viewerOrgID uuid.UUID, limit, offset int) ([]models.CommunityApp, int64, *errx.Error) {
	apps, total, err := s.repo.ListListed(ctx, viewerOrgID, config.AppDirectoryPopularInstalls, limit, offset)
	if err != nil {
		return nil, 0, errx.New(errx.Internal, "lookup failed")
	}
	return apps, total, nil
}

// Open returns a published listing by its link, listed or not.
func (s *Service) Open(ctx context.Context, viewerOrgID uuid.UUID, slug string) (*models.CommunityApp, *errx.Error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !slugRe.MatchString(slug) {
		return nil, errx.New(errx.NotFound, "app not found")
	}
	app, err := s.repo.GetPublished(ctx, viewerOrgID, slug, config.AppDirectoryPopularInstalls)
	if err != nil {
		return nil, errx.New(errx.Internal, "lookup failed")
	}
	if app == nil {
		return nil, errx.New(errx.NotFound, "app not found")
	}
	return app, nil
}

func (s *Service) AdminList(ctx context.Context, q *models.AdminAppListingSearch) (*models.AdminAppListingsResult, *errx.Error) {
	if q.Status != "" && !models.AppListingStatus(q.Status).Valid() {
		return nil, errx.New(errx.BadRequest, "invalid status filter")
	}
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	rows, total, err := s.repo.AdminList(ctx, q, config.AppDirectoryPopularInstalls)
	if err != nil {
		return nil, errx.New(errx.Internal, "lookup failed")
	}
	res := &models.AdminAppListingsResult{Data: rows, Pagination: models.Pagination{Total: &total}}
	if int64(q.Offset+len(rows)) < total {
		next := q.Offset + len(rows)
		res.Pagination.HasMore = true
		res.Pagination.NextCursor = paging.EncodeOffset(next)
	}
	return res, nil
}

// SetStatus records an operator's decision. Hiding needs a note, because it is
// the only thing the developer sees about why.
func (s *Service) SetStatus(ctx context.Context, appID, adminID uuid.UUID, status models.AppListingStatus, note string) (*models.AdminAppListing, *errx.Error) {
	if !status.Valid() {
		return nil, errx.New(errx.BadRequest, "status must be published, featured or hidden")
	}
	note = strings.TrimSpace(note)
	if status == models.AppListingHidden && note == "" {
		return nil, errx.New(errx.BadRequest, "a note is required to hide a listing")
	}
	if utf8.RuneCountInString(note) > maxReviewNote {
		return nil, errx.New(errx.BadRequest, "the note is too long")
	}
	if err := s.repo.SetStatus(ctx, appID, status, adminID, note); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.New(errx.NotFound, "listing not found")
		}
		return nil, errx.New(errx.Internal, "update failed")
	}
	item, err := s.repo.AdminGet(ctx, appID, config.AppDirectoryPopularInstalls)
	if err != nil || item == nil {
		return nil, errx.New(errx.Internal, "lookup failed")
	}
	return item, nil
}

func sameContent(a, b *models.AppListing) bool {
	return a.Slug == b.Slug && a.Tagline == b.Tagline && a.Description == b.Description &&
		a.Category == b.Category && a.InstallURL == b.InstallURL &&
		a.SupportURL == b.SupportURL && a.PrivacyURL == b.PrivacyURL
}

func normalize(w models.AppListingWrite) (*models.AppListing, *errx.Error) {
	l := &models.AppListing{
		Slug:     strings.ToLower(strings.TrimSpace(w.Slug)),
		Tagline:  strings.Join(strings.Fields(w.Tagline), " "),
		Category: strings.TrimSpace(w.Category),
	}
	if !slugRe.MatchString(l.Slug) || strings.Contains(l.Slug, "--") {
		return nil, invalid("the link must be 3 to 48 lowercase letters, numbers or single dashes, starting and ending with a letter or number")
	}
	if reservedSlugs[l.Slug] {
		return nil, invalid("this link is reserved")
	}
	if l.Tagline == "" {
		return nil, invalid("a tagline is required")
	}
	if utf8.RuneCountInString(l.Tagline) > maxTagline {
		return nil, invalid("the tagline is longer than 120 characters")
	}
	if !plainText(l.Tagline, false) {
		return nil, invalid("the tagline contains characters that cannot be shown")
	}
	l.Description = strings.TrimSpace(strings.ReplaceAll(w.Description, "\r\n", "\n"))
	if utf8.RuneCountInString(l.Description) > maxDescription {
		return nil, invalid("the description is longer than 2,000 characters")
	}
	if !plainText(l.Description, true) {
		return nil, invalid("the description contains characters that cannot be shown")
	}
	if !slices.Contains(models.AppListingCategories, l.Category) {
		return nil, invalid("unknown category")
	}
	var xerr *errx.Error
	if l.InstallURL, xerr = httpsURL("install_url", w.InstallURL, true); xerr != nil {
		return nil, xerr
	}
	if l.SupportURL, xerr = httpsURL("support_url", w.SupportURL, false); xerr != nil {
		return nil, xerr
	}
	if l.PrivacyURL, xerr = httpsURL("privacy_url", w.PrivacyURL, false); xerr != nil {
		return nil, xerr
	}
	return l, nil
}

// plainText refuses control, invisible, bidi and private-use characters, which
// can make a listing read differently from what was reviewed.
func plainText(s string, multiline bool) bool {
	for _, r := range s {
		if multiline && (r == '\n' || r == '\t') {
			continue
		}
		if r == utf8.RuneError || unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Co, unicode.Cs, unicode.Zl, unicode.Zp) {
			return false
		}
	}
	return true
}

func httpsURL(field, raw string, required bool) (string, *errx.Error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if required {
			return "", invalid(field + " is required")
		}
		return "", nil
	}
	if len(raw) > maxURL {
		return "", invalid(field + " is longer than 2,048 characters")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return "", invalid(field + " must be an https address on a host, without credentials")
	}
	return u.String(), nil
}

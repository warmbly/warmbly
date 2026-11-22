// Package mailboxavatar keeps each mailbox's own profile photo: read from an
// administrator grant, an inbox vendor or the connect handshake, normalised to
// a JPEG and stored as a public object the dashboard can show.
package mailboxavatar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/pkg/safehttp"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// KeyPrefix sits under avatars/, which the public object route already serves.
	KeyPrefix = "avatars/mailboxes/"

	maxBytes     = 2 << 20
	maxDimension = 2048
	// A photo changes rarely; a weekly look is enough to follow it.
	recheckAfter = 7 * 24 * time.Hour
	sweepBatch   = 50
)

// ErrNotImage is a source answering with something that is not a PNG or JPEG photo.
var ErrNotImage = errors.New("mailboxavatar: not a usable image")

// GrantPhotos reads a mailbox's photo through the administrator grant it connects with; nil, nil when it has none.
type GrantPhotos interface {
	MailboxPhoto(ctx context.Context, accountID uuid.UUID) ([]byte, error)
}

// VendorPhotos lists the picture URLs an inbox vendor account publishes, keyed by the vendor's mailbox id.
type VendorPhotos interface {
	Pictures(ctx context.Context, orgID, connectionID uuid.UUID) (map[string]string, error)
}

type Service struct {
	repo    repository.MailboxAvatarRepository
	store   storage.Store
	grants  GrantPhotos
	vendors VendorPhotos
	http    *http.Client
}

func New(repo repository.MailboxAvatarRepository, store storage.Store, grants GrantPhotos, vendors VendorPhotos) *Service {
	return &Service{repo: repo, store: store, grants: grants, vendors: vendors, http: safehttp.Client(20 * time.Second)}
}

// Save stores data as the mailbox's photo, replacing the previous one.
func (s *Service) Save(ctx context.Context, orgID, id uuid.UUID, data []byte) error {
	if s == nil || s.store == nil {
		return nil
	}
	body, err := Normalise(data)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	key := fmt.Sprintf("%s%s-%s.jpg", KeyPrefix, id, hex.EncodeToString(sum[:6]))

	previous, err := s.repo.Current(ctx, orgID, id)
	if err != nil {
		return err
	}
	// Content-addressed, so an unchanged photo is recognised and not uploaded again.
	if keyOf(previous) == key {
		return s.repo.Checked(ctx, orgID, id)
	}
	u, err := s.store.PutPublic(ctx, key, bytes.NewReader(body), "image/jpeg")
	if err != nil {
		return err
	}
	if err := s.repo.Set(ctx, orgID, id, u); err != nil {
		_ = s.store.Delete(ctx, key)
		return err
	}
	s.deleteObject(ctx, previous)
	return nil
}

// clear drops a photo the source no longer has.
func (s *Service) clear(ctx context.Context, orgID, id uuid.UUID, previous string) error {
	if err := s.repo.Set(ctx, orgID, id, ""); err != nil {
		return err
	}
	s.deleteObject(ctx, previous)
	return nil
}

func (s *Service) deleteObject(ctx context.Context, u string) {
	if key := keyOf(u); key != "" {
		_ = s.store.Delete(ctx, key)
	}
}

// keyOf recovers our object key from a stored URL, empty for anything not under KeyPrefix.
func keyOf(u string) string {
	i := strings.Index(u, KeyPrefix)
	if i < 0 {
		return ""
	}
	key := u[i:]
	if q := strings.IndexAny(key, "?#"); q >= 0 {
		key = key[:q]
	}
	if key == KeyPrefix || strings.Contains(key, "..") {
		return ""
	}
	return key
}

// Normalise decodes a PNG or JPEG and re-encodes it as a JPEG, which drops
// anything riding along in the file and gives every photo one content type.
func Normalise(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > maxBytes {
		return nil, ErrNotImage
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") {
		return nil, ErrNotImage
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxDimension || cfg.Height > maxDimension {
		return nil, ErrNotImage
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrNotImage
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Start refreshes grant and vendor mailboxes in the background, new ones first.
func (s *Service) Start(ctx context.Context) {
	jobrun.Loop(ctx, "mailbox_avatar_refresh", 15*time.Minute, true, s.sweep)
}

func (s *Service) sweep(ctx context.Context) error {
	due, err := s.repo.Due(ctx, time.Now().Add(-recheckAfter), sweepBatch)
	if err != nil {
		return err
	}
	// One vendor listing serves every mailbox of that account in this pass.
	pictures := make(map[uuid.UUID]map[string]string)
	for _, c := range due {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		s.refresh(cctx, c, pictures)
		cancel()
	}
	return nil
}

func (s *Service) refresh(ctx context.Context, c repository.MailboxAvatarCandidate, pictures map[uuid.UUID]map[string]string) {
	data, err := s.fetch(ctx, c, pictures)
	switch {
	case err != nil:
		// Marked checked anyway: a source that fails is looked at again next week, not every pass.
		log.Debug().Err(err).Str("email_account_id", c.ID.String()).Msg("mailboxavatar: photo not read")
		err = s.repo.Checked(ctx, c.OrganizationID, c.ID)
	case data == nil && c.AvatarURL != "":
		err = s.clear(ctx, c.OrganizationID, c.ID, c.AvatarURL)
	case data == nil:
		err = s.repo.Checked(ctx, c.OrganizationID, c.ID)
	default:
		if err = s.Save(ctx, c.OrganizationID, c.ID, data); errors.Is(err, ErrNotImage) {
			err = s.repo.Checked(ctx, c.OrganizationID, c.ID)
		}
	}
	if err != nil {
		log.Warn().Err(err).Str("email_account_id", c.ID.String()).Msg("mailboxavatar: refresh failed")
	}
}

// fetch reads the photo from the grant first, then the vendor; nil, nil when neither has one.
func (s *Service) fetch(ctx context.Context, c repository.MailboxAvatarCandidate, pictures map[uuid.UUID]map[string]string) ([]byte, error) {
	// A grant that could not answer keeps what is stored unless the vendor has a photo instead.
	var grantErr error
	if c.DomainGrantID != nil && s.grants != nil {
		data, err := s.grants.MailboxPhoto(ctx, c.ID)
		if err == nil && data != nil {
			return data, nil
		}
		grantErr = err
	}
	if c.VendorConnectionID == nil || c.VendorMailboxID == "" || s.vendors == nil {
		return nil, grantErr
	}
	pics, ok := pictures[*c.VendorConnectionID]
	if !ok {
		var err error
		if pics, err = s.vendors.Pictures(ctx, c.OrganizationID, *c.VendorConnectionID); err != nil {
			return nil, err
		}
		pictures[*c.VendorConnectionID] = pics
	}
	if u := pics[c.VendorMailboxID]; u != "" {
		return s.download(ctx, u)
	}
	return nil, grantErr
}

// download reads a vendor-published picture: an inline data URL, or HTTPS through the SSRF-hardened client.
func (s *Service) download(ctx context.Context, raw string) ([]byte, error) {
	if rest, ok := strings.CutPrefix(raw, "data:"); ok {
		meta, payload, found := strings.Cut(rest, ",")
		if !found || !strings.HasSuffix(meta, ";base64") || !strings.HasPrefix(meta, "image/") || len(payload) > maxBytes*4/3+4 {
			return nil, ErrNotImage
		}
		return base64.StdEncoding.DecodeString(payload)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, ErrNotImage
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("mailboxavatar: picture answered %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	return data, nil
}

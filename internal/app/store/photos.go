package store

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/mailboxavatar"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// PhotoBlobs keeps uploaded photos as private objects under models.StorePhotoKeyPrefix.
type PhotoBlobs interface {
	Put(ctx context.Context, key string, body io.Reader, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// MailboxAvatars shows a mailbox's photo in the dashboard.
type MailboxAvatars interface {
	Save(ctx context.Context, orgID, emailAccountID uuid.UUID, data []byte) error
}

// WireAvatars attaches the dashboard's mailbox photos, built after the store.
func (s *Service) WireAvatars(a MailboxAvatars) {
	if s != nil {
		s.mu.Lock()
		s.avatars = a
		s.mu.Unlock()
	}
}

const (
	// photoMaxBytes is GraphKit's limit, held after normalising.
	photoMaxBytes = 2 << 20
	// photoUploadsPerDay bounds a workspace's uploads, which nothing charges for.
	photoUploadsPerDay = 200
	// An upload no order or mailbox took is removed after a day; one a mailbox replaced, a month after it was used.
	photoUnusedFor = 24 * time.Hour
	photoUsedFor   = 30 * 24 * time.Hour
	// A set GraphKit did not take is tried again at the reconciler's pace, for about a day.
	photoRetryAfter  = 10 * time.Minute
	photoMaxAttempts = 144
	// userAvatarPrefix is where a member's own uploaded photo lives.
	userAvatarPrefix = "avatars/users/"
)

// Codes for photo uploads.
const (
	CodePhotoInvalid  = "photo_invalid"
	CodePhotoNotFound = "photo_not_found"
	CodePhotoLimit    = "photo_limit_reached"
	CodeNoProfilePic  = "profile_photo_missing"
	CodePhotoStorage  = "photo_storage_unavailable"
)

// Photo is an uploaded mailbox photo, referenced by an order's mailbox line or a mailbox.
type Photo struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

func photoNotFound() *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, CodePhotoNotFound, "That photo is no longer available. Choose it again.")
}

// PhotosAvailable reports whether this instance can keep uploaded photos.
func (s *Service) PhotosAvailable() bool { return s.Enabled() && s.Blobs != nil }

// photosOn refuses photos where the store is off or a partner buys for the workspace.
func (s *Service) photosOn(ctx context.Context, orgID uuid.UUID) error {
	if !s.Enabled() {
		return errDisabled
	}
	if s.managed(ctx, orgID) {
		return errManaged
	}
	if s.Blobs == nil {
		return errx.NewWithIdentifier(errx.ServiceUnavailable, CodePhotoStorage, "Photos need file storage, which this instance does not have set up.")
	}
	return nil
}

// UploadPhoto keeps a PNG or JPEG as a normalised JPEG and returns its id.
func (s *Service) UploadPhoto(ctx context.Context, orgID, userID uuid.UUID, data []byte) (*Photo, error) {
	if err := s.photosOn(ctx, orgID); err != nil {
		return nil, err
	}
	body, err := mailboxavatar.Normalise(data)
	if err != nil || len(body) > photoMaxBytes {
		return nil, errx.NewWithIdentifier(errx.BadRequest, CodePhotoInvalid, "Use a PNG or JPG photo up to 2 MB and 2048 px on each side.")
	}
	n, err := s.Repo.CountPhotosSince(ctx, orgID, s.now().Add(-24*time.Hour))
	if err != nil {
		return nil, err
	}
	if n >= photoUploadsPerDay {
		return nil, errx.NewWithIdentifier(errx.TooManyRequests, CodePhotoLimit, "Too many photos were uploaded today. Try again tomorrow.")
	}
	id := uuid.New()
	key := models.StorePhotoKey(orgID, id)
	if err := s.Blobs.Put(ctx, key, bytes.NewReader(body), "image/jpeg"); err != nil {
		return nil, err
	}
	var by *uuid.UUID
	if userID != uuid.Nil {
		by = &userID
	}
	p := &models.StorePhoto{ID: id, OrganizationID: orgID, BlobKey: key, SizeBytes: len(body), CreatedBy: by}
	if err := s.Repo.CreatePhoto(ctx, p); err != nil {
		_ = s.Blobs.Delete(context.WithoutCancel(ctx), key)
		return nil, err
	}
	return &Photo{ID: id, CreatedAt: s.now()}, nil
}

// ProfilePhoto copies the member's own uploaded profile photo into a store photo,
// the checkout's default. A photo from a sign-in provider is not ours to copy.
func (s *Service) ProfilePhoto(ctx context.Context, orgID, userID uuid.UUID) (*Photo, error) {
	if err := s.photosOn(ctx, orgID); err != nil {
		return nil, err
	}
	missing := errx.NewWithIdentifier(errx.NotFound, CodeNoProfilePic, "You have no profile photo to use.")
	if s.People == nil {
		return nil, missing
	}
	u, err := s.People.GetUser(ctx, userID)
	if err != nil || u == nil || u.AvatarURL == nil {
		return nil, missing
	}
	key := objectKey(*u.AvatarURL, userAvatarPrefix)
	if key == "" {
		return nil, missing
	}
	data, err := s.readBlob(ctx, key)
	if err != nil {
		return nil, missing
	}
	return s.UploadPhoto(ctx, orgID, userID, data)
}

// objectKey recovers our own object key from a stored public URL, "" for anything outside prefix.
func objectKey(u, prefix string) string {
	i := strings.Index(u, prefix)
	if i < 0 {
		return ""
	}
	key := u[i:]
	if q := strings.IndexAny(key, "?#"); q >= 0 {
		key = key[:q]
	}
	if key == prefix || strings.Contains(key, "..") {
		return ""
	}
	return key
}

func (s *Service) readBlob(ctx context.Context, key string) ([]byte, error) {
	rc, err := s.Blobs.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, photoMaxBytes+1))
}

// PhotoBytes is one of the workspace's uploaded photos, for the checkout's preview.
func (s *Service) PhotoBytes(ctx context.Context, orgID, id uuid.UUID) ([]byte, error) {
	if err := s.photosOn(ctx, orgID); err != nil {
		return nil, err
	}
	p, err := s.Repo.GetPhoto(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errx.ErrNotFound
	}
	data, err := s.readBlob(ctx, p.BlobKey)
	if err != nil {
		return nil, errx.ErrNotFound
	}
	return data, nil
}

// checkPhotos refuses an order line naming a photo the workspace does not have,
// or a photo for a domain or address the order does not buy mailboxes on.
func (s *Service) checkPhotos(ctx context.Context, orgID uuid.UUID, photos map[string]uuid.UUID, items []graphkit.QuoteItem) (map[string]uuid.UUID, error) {
	if len(photos) == 0 {
		return nil, nil
	}
	if err := s.photosOn(ctx, orgID); err != nil {
		return nil, err
	}
	lines := map[string]bool{}
	for _, it := range items {
		if it.Type == graphkit.ItemMailboxes {
			lines[it.Domain] = true
			for _, m := range it.Mailboxes {
				lines[m.LocalPart+"@"+it.Domain] = true
			}
		}
	}
	out := map[string]uuid.UUID{}
	known := map[uuid.UUID]bool{}
	for key, id := range photos {
		key = photoKey(key)
		if !lines[key] {
			return nil, itemsErr("A photo goes on a domain's mailboxes.")
		}
		// One photo may serve many mailboxes; it is looked up once.
		if !known[id] {
			p, err := s.Repo.GetPhoto(ctx, orgID, id)
			if err != nil {
				return nil, err
			}
			if p == nil {
				return nil, photoNotFound()
			}
			known[id] = true
		}
		out[key] = id
	}
	return out, nil
}

// SetMailboxPhoto makes an uploaded photo a purchased mailbox's profile photo,
// at its provider and in the dashboard. A set GraphKit does not take now is retried.
func (s *Service) SetMailboxPhoto(ctx context.Context, orgID, id, photoID uuid.UUID) (*Mailbox, error) {
	if err := s.photosOn(ctx, orgID); err != nil {
		return nil, err
	}
	m, err := s.mailbox(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if !photoable(m.Status) {
		return nil, errx.NewWithIdentifier(errx.Conflict, CodeStoreStateConflict, "This mailbox has ended, so its photo can't change.")
	}
	ok, err := s.Repo.SetMailboxPhoto(ctx, orgID, id, photoID, false)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, photoNotFound()
	}
	if err := s.Repo.MarkPhotosUsed(ctx, orgID, []uuid.UUID{photoID}); err != nil {
		return nil, err
	}
	if m, err = s.mailbox(ctx, orgID, id); err != nil {
		return nil, err
	}
	actx, cancel := context.WithTimeout(ctx, 20*time.Second)
	s.applyPhoto(actx, m)
	cancel()
	if m, err = s.mailbox(ctx, orgID, id); err != nil {
		return nil, err
	}
	s.publishMailbox(ctx, m)
	out := mailboxView(m, s.unplanned(ctx, orgID))
	return &out, nil
}

// photoable is a mailbox GraphKit takes a photo for: still provisioning, or one the workspace has.
func photoable(status string) bool {
	return status == graphkit.MailboxProvisioning || liveStatus(status)
}

// adoptOrderPhoto gives a mailbox the photo its order line named for it or its domain, unless it already has one.
func (s *Service) adoptOrderPhoto(ctx context.Context, row *models.StoreMailbox, snap quoteSnapshot) {
	if row.PhotoID != nil || len(snap.Photos) == 0 {
		return
	}
	// The mailbox's own photo wins over its domain's.
	photoID, ok := snap.Photos[photoKey(row.Email)]
	if !ok {
		photoID, ok = snap.Photos[photoKey(row.Email[strings.LastIndex(row.Email, "@")+1:])]
	}
	if !ok {
		return
	}
	if set, err := s.Repo.SetMailboxPhoto(ctx, row.OrganizationID, row.ID, photoID, true); err != nil || !set {
		return
	}
	row.PhotoID = &photoID
	s.photoLater(ctx, row)
}

// photoLater sets a mailbox's photo off the caller's request; the reconciler retries what fails.
func (s *Service) photoLater(ctx context.Context, row *models.StoreMailbox) {
	m := *row
	s.connects.Add(1)
	go func() {
		defer s.connects.Done()
		pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		s.applyPhoto(pctx, &m)
	}()
}

// applyPhoto sends the mailbox's photo to GraphKit and shows it in the dashboard; true once GraphKit has it.
// The key is the mailbox and the photo, so a retry after an ambiguous failure is the same request.
func (s *Service) applyPhoto(ctx context.Context, m *models.StoreMailbox) bool {
	if m.PhotoID == nil || !photoable(m.Status) || s.Blobs == nil {
		return false
	}
	photoID := *m.PhotoID
	if m.PhotoAppliedID != nil && *m.PhotoAppliedID == photoID {
		return true
	}
	p, err := s.Repo.GetPhoto(ctx, m.OrganizationID, photoID)
	if err != nil || p == nil {
		_ = s.Repo.RecordPhotoFailure(ctx, m.OrganizationID, m.ID, err == nil)
		return false
	}
	data, err := s.readBlob(ctx, p.BlobKey)
	if err != nil {
		_ = s.Repo.RecordPhotoFailure(ctx, m.OrganizationID, m.ID, false)
		return false
	}
	s.showPhoto(ctx, m, data)
	_, gerr := s.api().SetMailboxPhoto(ctx, m.GraphKitMailboxID, "photo.jpg", data, "store:photo:"+m.ID.String()+":"+photoID.String())
	if gerr != nil {
		logUpstream("mailbox.photo", gerr)
		// A refusal of the photo itself will not change on a retry; anything else is tried again.
		ge, _ := graphkit.AsError(gerr)
		giveUp := ge != nil && (ge.Code == graphkit.CodeInvalidRequest || ge.Code == graphkit.CodeNotFound)
		_ = s.Repo.RecordPhotoFailure(context.WithoutCancel(ctx), m.OrganizationID, m.ID, giveUp)
		return false
	}
	if err := s.Repo.MarkPhotoApplied(context.WithoutCancel(ctx), m.OrganizationID, m.ID, photoID); err != nil {
		log.Warn().Err(err).Str("store_mailbox_id", m.ID.String()).Msg("store: recording the mailbox photo failed")
		return false
	}
	return true
}

// showPhoto hands the photo to the dashboard's mailbox avatars once the mailbox is connected.
func (s *Service) showPhoto(ctx context.Context, m *models.StoreMailbox, data []byte) {
	s.mu.Lock()
	a := s.avatars
	s.mu.Unlock()
	if a == nil || m.EmailAccountID == nil {
		return
	}
	if err := a.Save(context.WithoutCancel(ctx), m.OrganizationID, *m.EmailAccountID, data); err != nil {
		log.Warn().Err(err).Str("store_mailbox_id", m.ID.String()).Msg("store: showing the mailbox photo failed")
	}
}

// showPhotoOnConnect shows a mailbox's chosen photo the moment it connects.
func (s *Service) showPhotoOnConnect(ctx context.Context, m *models.StoreMailbox) {
	if m.PhotoID == nil || s.Blobs == nil {
		return
	}
	p, err := s.Repo.GetPhoto(ctx, m.OrganizationID, *m.PhotoID)
	if err != nil || p == nil {
		return
	}
	if data, err := s.readBlob(ctx, p.BlobKey); err == nil {
		s.showPhoto(ctx, m, data)
	}
}

// retryPhotos sets the photos GraphKit has not taken yet.
func (s *Service) retryPhotos(ctx context.Context) error {
	if s.Blobs == nil {
		return nil
	}
	rows, err := s.Repo.PendingPhotos(ctx, s.now().Add(-photoRetryAfter), photoMaxAttempts, 50)
	if err != nil {
		return err
	}
	for i := range rows {
		pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		s.applyPhoto(pctx, &rows[i])
		cancel()
	}
	return nil
}

// sweepPhotos removes uploads nothing took after a day, and replaced photos a month after they were last used.
func (s *Service) sweepPhotos(ctx context.Context) error {
	if s.Blobs == nil {
		return nil
	}
	now := s.now()
	stale, err := s.Repo.StalePhotos(ctx, now.Add(-photoUnusedFor), now.Add(-photoUsedFor), 100)
	if err != nil {
		return err
	}
	var errs []error
	for _, p := range stale {
		if !strings.HasPrefix(p.BlobKey, models.StorePhotoKeyPrefix+p.OrganizationID.String()+"/") {
			continue
		}
		if err := s.Blobs.Delete(ctx, p.BlobKey); err != nil {
			errs = append(errs, err)
			continue
		}
		errs = append(errs, s.Repo.DeletePhoto(ctx, p.OrganizationID, p.ID))
	}
	return errors.Join(errs...)
}

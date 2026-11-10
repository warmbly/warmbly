package crossorgcopy

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/dailythrottle"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/repository"
)

// Memberships is the caller's workspace list, each with its effective role.
type Memberships interface {
	GetUserOrganizations(ctx context.Context, userID uuid.UUID) ([]models.OrganizationMember, *errx.Error)
}

// StorageLimits is the per-workspace storage quota.
type StorageLimits interface {
	GetStorageLimitBytes(ctx context.Context, orgID uuid.UUID) (int64, *errx.Error)
}

// Deps are the stores a copy reads from and writes to. Storage, Images,
// Attachments, Limits, Throttle and Logs may be nil; what they back is then skipped.
type Deps struct {
	Orgs        Memberships
	Campaigns   repository.CampaignRepository
	Templates   repository.TemplateRepository
	Attachments repository.AttachmentRepository
	Images      repository.EmailImageRepository
	Storage     storage.Store
	Limits      StorageLimits
	Throttle    dailythrottle.Service
	Logs        repository.CampaignLogRepository
}

// Service copies campaigns and templates between a caller's workspaces.
type Service struct{ d Deps }

// NewService builds the copy service.
func NewService(d Deps) *Service { return &Service{d: d} }

// source is everything read from the source workspace, verified to be its own.
type source struct {
	campaign    *models.Campaign
	templates   []models.ReplyTemplate
	attachments []models.CampaignAttachment
	images      []models.EmailImage
}

// CopyCampaign copies one campaign, and optionally some of the source
// workspace's templates, into each destination as a draft.
func (s *Service) CopyCampaign(ctx context.Context, caller Caller, campaignID uuid.UUID, req models.CopyCampaignToWorkspacesRequest) (*models.WorkspaceCopyResponse, *errx.Error) {
	return s.copy(ctx, caller, &campaignID, req.TemplateIDs, req.OrganizationIDs)
}

// CopyTemplates copies reply templates into each destination.
func (s *Service) CopyTemplates(ctx context.Context, caller Caller, req models.CopyTemplatesToWorkspacesRequest) (*models.WorkspaceCopyResponse, *errx.Error) {
	if len(dedupe(req.TemplateIDs)) == 0 {
		return nil, errx.New(errx.BadRequest, "template_ids must name at least one template")
	}
	return s.copy(ctx, caller, nil, req.TemplateIDs, req.OrganizationIDs)
}

func (s *Service) copy(ctx context.Context, caller Caller, campaignID *uuid.UUID, templateIDs, orgIDs []uuid.UUID) (*models.WorkspaceCopyResponse, *errx.Error) {
	templateIDs = dedupe(templateIDs)
	if len(templateIDs) > models.CopyTemplatesMax {
		return nil, errx.NewWithIdentifier(errx.BadRequest, CodeTooManyTemplates,
			"A copy can carry at most 50 templates at a time.")
	}
	members, xerr := s.d.Orgs.GetUserOrganizations(ctx, caller.UserID)
	if xerr != nil {
		return nil, xerr
	}
	dests, xerr := authorize(caller, members, orgIDs, campaignID, len(templateIDs) > 0)
	if xerr != nil {
		return nil, xerr
	}
	src, xerr := s.loadSource(ctx, caller.SourceOrgID, campaignID, templateIDs)
	if xerr != nil {
		return nil, xerr
	}

	out := &models.WorkspaceCopyResponse{Data: make([]models.WorkspaceCopyResult, 0, len(dests))}
	for _, d := range dests {
		res := models.WorkspaceCopyResult{
			OrganizationID:   d.orgID,
			OrganizationName: d.name,
			Templates:        []models.CopiedRef{},
			Review:           []models.CopyReview{},
			Warnings:         []string{},
		}
		if d.refuse != nil {
			res.Status = models.WorkspaceCopyFailed
			res.Error = d.refuse
		} else {
			s.copyInto(ctx, caller, src, d.orgID, &res)
		}
		out.Data = append(out.Data, res)
	}
	return out, nil
}

// loadSource reads every row the copy carries, each through a predicate on
// the source workspace, so nothing named in the request can reach another one.
func (s *Service) loadSource(ctx context.Context, orgID uuid.UUID, campaignID *uuid.UUID, templateIDs []uuid.UUID) (*source, *errx.Error) {
	src := &source{}
	imageCampaign := uuid.Nil
	if campaignID != nil {
		c, err := s.d.Campaigns.GetByID(ctx, *campaignID)
		if err != nil && !errors.Is(err, errx.ErrResourceNotFound) {
			return nil, errx.InternalError()
		}
		if c == nil || c.OrganizationID == nil || *c.OrganizationID != orgID {
			return nil, errx.New(errx.NotFound, "campaign not found")
		}
		src.campaign = c
		imageCampaign = c.ID
		if s.d.Attachments != nil && s.d.Storage != nil {
			atts, err := s.d.Attachments.ListByCampaign(ctx, c.ID)
			if err != nil {
				return nil, errx.InternalError()
			}
			src.attachments = atts
		}
	}
	texts := make([]string, 0, 2*len(templateIDs))
	for _, id := range templateIDs {
		t, err := s.d.Templates.GetByID(ctx, orgID, id)
		if err != nil {
			return nil, errx.InternalError()
		}
		if t == nil {
			return nil, errx.New(errx.NotFound, "template not found")
		}
		src.templates = append(src.templates, *t)
		texts = append(texts, t.BodyHTML, t.BodyPlain)
	}
	if s.d.Images != nil && s.d.Storage != nil {
		imgs, err := s.d.Images.ListReferenced(ctx, orgID, imageCampaign, texts)
		if err != nil {
			return nil, errx.InternalError()
		}
		src.images = imgs
	}
	return src, nil
}

// copyInto writes one destination's copy and records what happened in res.
func (s *Service) copyInto(ctx context.Context, caller Caller, src *source, dest uuid.UUID, res *models.WorkspaceCopyResult) {
	if src.campaign != nil && s.d.Throttle != nil {
		if xerr := s.d.Throttle.CheckAndIncrement(ctx, dest, dailythrottle.ResourceCampaign, config.DailyThrottleNewCampaigns); xerr != nil {
			fail(res, xerr)
			return
		}
	}

	same := dest == caller.SourceOrgID
	var rewrites map[string]string
	var images []models.EmailImage
	if !same {
		var warned bool
		rewrites, images, warned = s.copyImages(ctx, caller.UserID, src.images, dest)
		if warned {
			res.Warnings = append(res.Warnings, "Some images could not be copied, so the copy still shows them from the original workspace.")
		}
	}

	if src.campaign != nil {
		newID := uuid.New()
		atts, skipped, undo, xerr := s.copyAttachments(ctx, src.attachments, dest, newID)
		if xerr != nil {
			s.undoImages(ctx, images)
			fail(res, xerr)
			return
		}
		if skipped > 0 {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%d attachment(s) could not be copied and were left out.", skipped))
		}
		in := repository.CopyCampaignAcrossInput{
			SourceOrgID:  caller.SourceOrgID,
			DestOrgID:    dest,
			SourceID:     src.campaign.ID,
			NewID:        newID,
			UserID:       caller.UserID,
			Attachments:  atts,
			StorageLimit: s.limitFunc(dest),
			URLRewrites:  rewrites,
		}
		copied, review, err := s.d.Campaigns.CopyToOrganization(ctx, in)
		if err != nil {
			undo()
			s.undoImages(ctx, images)
			switch {
			case errors.Is(err, repository.ErrStorageQuotaExceeded):
				fail(res, errx.NewWithIdentifier(errx.BadRequest, "storage_limit_reached",
					"The attachments do not fit in this workspace's storage. Remove files there, or copy without them."))
			case errors.Is(err, errx.ErrResourceNotFound):
				fail(res, errx.New(errx.NotFound, "campaign not found"))
			default:
				errs.CaptureException(fmt.Errorf("copy campaign %s to workspace %s: %w", src.campaign.ID, dest, err))
				fail(res, errx.InternalError())
			}
			return
		}
		res.Campaign = &models.CopiedRef{ID: copied.ID, Name: copied.Name}
		res.Review = append(res.Review, review...)
		if s.d.Logs != nil {
			_ = s.d.Logs.CreateLog(ctx, &repository.CampaignLogEntry{
				CampaignID: copied.ID,
				EventType:  "created",
				Message:    "Copied from another workspace as a draft",
				Metadata:   map[string]interface{}{},
			})
		}
	}

	for _, t := range src.templates {
		data := &models.CreateReplyTemplate{
			Name:      t.Name,
			Subject:   t.Subject,
			BodyHTML:  rewrite(t.BodyHTML, rewrites),
			BodyPlain: rewrite(t.BodyPlain, rewrites),
		}
		created, err := s.d.Templates.Create(ctx, dest, caller.UserID, data)
		if err != nil {
			errs.CaptureException(fmt.Errorf("copy template %s to workspace %s: %w", t.ID, dest, err))
			res.Warnings = append(res.Warnings, fmt.Sprintf("The template %q could not be copied.", t.Name))
			continue
		}
		res.Templates = append(res.Templates, models.CopiedRef{ID: created.ID, Name: created.Name})
	}

	switch {
	case src.campaign == nil && len(res.Templates) == 0:
		fail(res, errx.InternalError())
	case len(res.Warnings) > 0:
		res.Status = models.WorkspaceCopyPartial
	default:
		res.Status = models.WorkspaceCopyCopied
	}
}

// copyImages gives the destination its own copy of every image the copied
// content shows, under its own key and quota, and returns the URL rewrites
// and the rows it created.
func (s *Service) copyImages(ctx context.Context, userID uuid.UUID, images []models.EmailImage, dest uuid.UUID) (map[string]string, []models.EmailImage, bool) {
	rewrites := map[string]string{}
	var created []models.EmailImage
	warned := false
	for _, img := range images {
		row, err := s.copyImage(ctx, userID, img, dest)
		if err != nil {
			warned = true
			continue
		}
		rewrites[img.URL] = row.URL
		created = append(created, *row)
	}
	return rewrites, created, warned
}

// undoImages drops a failed destination's image copies so they hold none of its quota.
func (s *Service) undoImages(ctx context.Context, images []models.EmailImage) {
	for _, img := range images {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		err := s.d.Images.Delete(c, img.ID)
		cancel()
		if err != nil {
			errs.CaptureException(fmt.Errorf("copy undo: image %s: %w", img.ID, err))
			continue
		}
		s.deleteDetached(ctx, img.StorageKey)
	}
}

func (s *Service) copyImage(ctx context.Context, userID uuid.UUID, img models.EmailImage, dest uuid.UUID) (*models.EmailImage, error) {
	body, err := s.d.Storage.Get(ctx, img.StorageKey)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	key := models.EmailImageObjectKey(dest, path.Ext(img.StorageKey))
	stored, err := s.d.Storage.PutPublic(ctx, key, body, img.MimeType)
	if err != nil {
		return nil, err
	}
	row := &models.EmailImage{
		OrganizationID: dest,
		UserID:         &userID,
		Filename:       img.Filename,
		MimeType:       img.MimeType,
		Size:           img.Size,
		Width:          img.Width,
		Height:         img.Height,
		StorageKey:     key,
		URL:            copiedURL(img.URL, img.StorageKey, key, stored),
	}
	limit := s.limitFunc(dest)
	if limit == nil {
		limit = func(context.Context) (int64, error) { return math.MaxInt64, nil }
	}
	created, _, _, err := s.d.Images.CreateWithinQuota(ctx, row, limit)
	if err != nil || !created {
		s.deleteDetached(ctx, key)
		if err == nil {
			err = repository.ErrStorageQuotaExceeded
		}
		return nil, err
	}
	return row, nil
}

// copyAttachments writes the destination's copy of each attachment under the
// new campaign. Bytes that cannot be read are skipped and counted.
func (s *Service) copyAttachments(ctx context.Context, sources []models.CampaignAttachment, dest, campaignID uuid.UUID) ([]models.CampaignAttachment, int, func(), *errx.Error) {
	noop := func() {}
	if len(sources) == 0 || s.d.Storage == nil {
		return nil, 0, noop, nil
	}
	if s.d.Limits != nil && s.d.Attachments != nil {
		limit, xerr := s.d.Limits.GetStorageLimitBytes(ctx, dest)
		if xerr != nil {
			return nil, 0, noop, xerr
		}
		used, err := s.d.Attachments.SumStorageUsedByOrg(ctx, dest)
		if err != nil {
			return nil, 0, noop, errx.InternalError()
		}
		var adding int64
		for _, a := range sources {
			adding += a.Size
		}
		if used+adding > limit {
			return nil, 0, noop, errx.StorageLimitReached(used, limit, adding)
		}
	}
	copied := make([]models.CampaignAttachment, 0, len(sources))
	skipped := 0
	for _, att := range sources {
		body, err := s.d.Storage.Get(ctx, att.S3Key)
		if err != nil {
			skipped++
			continue
		}
		key := models.AttachmentObjectKey(campaignID, att.Filename)
		err = s.d.Storage.Put(ctx, key, body, storage.DownloadType(att.MimeType))
		body.Close()
		if err != nil {
			skipped++
			continue
		}
		att.S3Key = key
		copied = append(copied, att)
	}
	return copied, skipped, func() {
		for _, att := range copied {
			s.deleteDetached(ctx, att.S3Key)
		}
	}, nil
}

func (s *Service) limitFunc(orgID uuid.UUID) repository.StorageLimitFunc {
	if s.d.Limits == nil {
		return nil
	}
	return func(ctx context.Context) (int64, error) {
		v, xerr := s.d.Limits.GetStorageLimitBytes(ctx, orgID)
		if xerr != nil {
			return 0, xerr
		}
		return v, nil
	}
}

// deleteDetached removes an object on a context that outlives the request.
func (s *Service) deleteDetached(ctx context.Context, key string) {
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := s.d.Storage.Delete(c, key); err != nil {
		errs.CaptureException(fmt.Errorf("copy undo: object %s: %w", key, err))
	}
}

// copiedURL is the copy's public URL on the host the original was served from.
func copiedURL(srcURL, srcKey, newKey, stored string) string {
	if strings.HasPrefix(stored, "http://") || strings.HasPrefix(stored, "https://") {
		return stored
	}
	if srcKey != "" && strings.Contains(srcURL, srcKey) {
		return strings.Replace(srcURL, srcKey, newKey, 1)
	}
	return stored
}

func rewrite(text string, rewrites map[string]string) string {
	for from, to := range rewrites {
		text = strings.ReplaceAll(text, from, to)
	}
	return text
}

func fail(res *models.WorkspaceCopyResult, xerr *errx.Error) {
	res.Status = models.WorkspaceCopyFailed
	msg := xerr.Message
	if xerr.Code == errx.Internal && !xerr.Public {
		msg = "Something went wrong copying into this workspace."
	}
	res.Error = &models.CopyFailure{Code: xerr.ResponseCode(), Message: msg}
	res.Campaign = nil
}

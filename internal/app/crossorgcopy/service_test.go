package crossorgcopy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/orgtransfer"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type fakeOrgs struct{ members []models.OrganizationMember }

func (f fakeOrgs) GetUserOrganizations(context.Context, uuid.UUID) ([]models.OrganizationMember, *errx.Error) {
	return f.members, nil
}

type fakeCampaigns struct {
	repository.CampaignRepository
	byID    map[uuid.UUID]*models.Campaign
	copies  []repository.CopyCampaignAcrossInput
	copyErr error
}

func (f *fakeCampaigns) GetByID(_ context.Context, id uuid.UUID) (*models.Campaign, error) {
	if c, ok := f.byID[id]; ok {
		return c, nil
	}
	return nil, errx.ErrResourceNotFound
}

func (f *fakeCampaigns) CopyToOrganization(_ context.Context, in repository.CopyCampaignAcrossInput) (*models.Campaign, []models.CopyReview, error) {
	f.copies = append(f.copies, in)
	if f.copyErr != nil {
		return nil, nil, f.copyErr
	}
	src := f.byID[in.SourceID]
	return &models.Campaign{ID: in.NewID, Name: src.Name, OrganizationID: &in.DestOrgID, Status: "draft"}, nil, nil
}

type fakeTemplates struct {
	repository.TemplateRepository
	rows    []models.ReplyTemplate
	created []models.ReplyTemplate
}

func (f *fakeTemplates) GetByID(_ context.Context, orgID, id uuid.UUID) (*models.ReplyTemplate, error) {
	for _, t := range f.rows {
		if t.ID == id && t.OrganizationID == orgID {
			return &t, nil
		}
	}
	return nil, nil
}

func (f *fakeTemplates) Create(_ context.Context, orgID, userID uuid.UUID, d *models.CreateReplyTemplate) (*models.ReplyTemplate, error) {
	t := models.ReplyTemplate{ID: uuid.New(), OrganizationID: orgID, UserID: userID, Name: d.Name, Subject: d.Subject, BodyHTML: d.BodyHTML, BodyPlain: d.BodyPlain}
	f.created = append(f.created, t)
	return &t, nil
}

type fakeImages struct {
	repository.EmailImageRepository
	rows    []models.EmailImage
	created []models.EmailImage
	deleted []uuid.UUID
}

func (f *fakeImages) ListReferenced(_ context.Context, orgID, _ uuid.UUID, texts []string) ([]models.EmailImage, error) {
	var out []models.EmailImage
	for _, img := range f.rows {
		if img.OrganizationID != orgID {
			continue
		}
		for _, t := range texts {
			if strings.Contains(t, img.URL) {
				out = append(out, img)
				break
			}
		}
	}
	return out, nil
}

func (f *fakeImages) Delete(_ context.Context, id uuid.UUID) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeImages) CreateWithinQuota(_ context.Context, img *models.EmailImage, _ repository.StorageLimitFunc) (bool, int64, int64, error) {
	img.ID = uuid.New()
	f.created = append(f.created, *img)
	return true, 0, 0, nil
}

type memStore struct {
	storage.Store
	objects map[string][]byte
}

func (m *memStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	b, ok := m.objects[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memStore) PutPublic(_ context.Context, key string, body io.Reader, _ string) (string, error) {
	b, _ := io.ReadAll(body)
	m.objects[key] = b
	return "/public/" + key, nil
}

func (m *memStore) Delete(_ context.Context, key string) error {
	delete(m.objects, key)
	return nil
}

type fixture struct {
	src, dst, user uuid.UUID
	campaign       *models.Campaign
	tmpl, foreign  models.ReplyTemplate
	image          models.EmailImage
	campaigns      *fakeCampaigns
	templates      *fakeTemplates
	images         *fakeImages
	store          *memStore
	svc            *Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{src: uuid.New(), dst: uuid.New(), user: uuid.New()}
	other := uuid.New()
	f.campaign = &models.Campaign{ID: uuid.New(), Name: "Q3 outbound", OrganizationID: &f.src}
	srcKey := models.EmailImageObjectKey(f.src, ".png")
	f.image = models.EmailImage{ID: uuid.New(), OrganizationID: f.src, StorageKey: srcKey, URL: "https://api.example.test/public/" + srcKey, MimeType: "image/png", Size: 4, Filename: "logo.png"}
	f.tmpl = models.ReplyTemplate{ID: uuid.New(), OrganizationID: f.src, Name: "Follow-up", BodyHTML: `<img src="` + f.image.URL + `">`}
	f.foreign = models.ReplyTemplate{ID: uuid.New(), OrganizationID: other, Name: "Someone else's"}
	f.campaigns = &fakeCampaigns{byID: map[uuid.UUID]*models.Campaign{f.campaign.ID: f.campaign}}
	f.templates = &fakeTemplates{rows: []models.ReplyTemplate{f.tmpl, f.foreign}}
	f.images = &fakeImages{rows: []models.EmailImage{f.image}}
	f.store = &memStore{objects: map[string][]byte{srcKey: []byte("png!")}}
	manager := models.PermViewCampaigns | models.PermManageCampaigns
	orgs := fakeOrgs{members: []models.OrganizationMember{
		member(f.src, "member", manager, models.AccessScopeWorkspace),
		member(f.dst, "member", manager, models.AccessScopeWorkspace),
		member(other, "owner", 0, models.AccessScopeWorkspace),
	}}
	f.svc = NewService(Deps{Orgs: orgs, Campaigns: f.campaigns, Templates: f.templates, Images: f.images, Storage: f.store})
	return f
}

func (f *fixture) caller() Caller { return Caller{UserID: f.user, SourceOrgID: f.src} }

func TestCopyCampaignWritesADraftInTheDestination(t *testing.T) {
	f := newFixture(t)
	resp, xerr := f.svc.CopyCampaign(t.Context(), f.caller(), f.campaign.ID, models.CopyCampaignToWorkspacesRequest{
		OrganizationIDs: []uuid.UUID{f.dst},
		TemplateIDs:     []uuid.UUID{f.tmpl.ID},
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	r := resp.Data[0]
	if r.Status != models.WorkspaceCopyCopied || r.Campaign == nil || len(r.Templates) != 1 {
		t.Fatalf("result = %+v", r)
	}
	in := f.campaigns.copies[0]
	if in.SourceOrgID != f.src || in.DestOrgID != f.dst || in.SourceID != f.campaign.ID || in.UserID != f.user {
		t.Fatalf("copy input = %+v", in)
	}
	if f.templates.created[0].OrganizationID != f.dst {
		t.Fatalf("template landed in %s", f.templates.created[0].OrganizationID)
	}
}

func TestCopyRefusesAReferenceFromAnotherWorkspace(t *testing.T) {
	f := newFixture(t)
	_, xerr := f.svc.CopyCampaign(t.Context(), f.caller(), f.campaign.ID, models.CopyCampaignToWorkspacesRequest{
		OrganizationIDs: []uuid.UUID{f.dst},
		TemplateIDs:     []uuid.UUID{f.foreign.ID},
	})
	if xerr == nil || xerr.Code != errx.NotFound {
		t.Fatalf("foreign template: %v", xerr)
	}
	foreignCampaign := &models.Campaign{ID: uuid.New(), Name: "Not ours", OrganizationID: &f.foreign.OrganizationID}
	f.campaigns.byID[foreignCampaign.ID] = foreignCampaign
	_, xerr = f.svc.CopyCampaign(t.Context(), f.caller(), foreignCampaign.ID, models.CopyCampaignToWorkspacesRequest{OrganizationIDs: []uuid.UUID{f.dst}})
	if xerr == nil || xerr.Code != errx.NotFound {
		t.Fatalf("foreign campaign: %v", xerr)
	}
	if len(f.campaigns.copies) != 0 || len(f.templates.created) != 0 {
		t.Fatal("a refused request wrote something")
	}
}

func TestCopyReportsAForbiddenDestinationWithoutWritingThere(t *testing.T) {
	f := newFixture(t)
	stranger := uuid.New()
	resp, xerr := f.svc.CopyTemplates(t.Context(), f.caller(), models.CopyTemplatesToWorkspacesRequest{
		TemplateIDs:     []uuid.UUID{f.tmpl.ID},
		OrganizationIDs: []uuid.UUID{stranger, f.dst},
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if resp.Data[0].Status != models.WorkspaceCopyFailed || resp.Data[0].Error.Code != CodeDestinationForbidden {
		t.Fatalf("stranger = %+v", resp.Data[0])
	}
	if resp.Data[1].Status != models.WorkspaceCopyCopied {
		t.Fatalf("member = %+v", resp.Data[1])
	}
	for _, c := range f.templates.created {
		if c.OrganizationID == stranger {
			t.Fatal("wrote into a workspace the caller cannot write")
		}
	}
}

func TestCopyGivesTheDestinationItsOwnImages(t *testing.T) {
	f := newFixture(t)
	if _, xerr := f.svc.CopyTemplates(t.Context(), f.caller(), models.CopyTemplatesToWorkspacesRequest{
		TemplateIDs: []uuid.UUID{f.tmpl.ID}, OrganizationIDs: []uuid.UUID{f.dst},
	}); xerr != nil {
		t.Fatal(xerr)
	}
	if len(f.images.created) != 1 {
		t.Fatalf("images created = %d", len(f.images.created))
	}
	img := f.images.created[0]
	if img.OrganizationID != f.dst || !strings.HasPrefix(img.StorageKey, models.EmailImageKeyPrefix+f.dst.String()+"/") {
		t.Fatalf("image row = %+v", img)
	}
	if _, ok := f.store.objects[img.StorageKey]; !ok {
		t.Fatal("bytes were not copied under the destination's key")
	}
	body := f.templates.created[0].BodyHTML
	if strings.Contains(body, f.image.URL) || !strings.Contains(body, img.URL) || !strings.HasPrefix(img.URL, "https://api.example.test/") {
		t.Fatalf("template body = %q, image url %q", body, img.URL)
	}
}

func TestFailedCampaignCopyRemovesItsImageCopies(t *testing.T) {
	f := newFixture(t)
	f.campaigns.copyErr = errors.New("boom")
	resp, xerr := f.svc.CopyCampaign(t.Context(), f.caller(), f.campaign.ID, models.CopyCampaignToWorkspacesRequest{
		OrganizationIDs: []uuid.UUID{f.dst},
		TemplateIDs:     []uuid.UUID{f.tmpl.ID},
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if resp.Data[0].Status != models.WorkspaceCopyFailed {
		t.Fatalf("result = %+v", resp.Data[0])
	}
	if len(f.images.created) != 1 || len(f.images.deleted) != 1 || f.images.deleted[0] != f.images.created[0].ID {
		t.Fatalf("created %d, deleted %v", len(f.images.created), f.images.deleted)
	}
	if _, ok := f.store.objects[f.images.created[0].StorageKey]; ok {
		t.Fatal("the image copy's bytes were left in the destination")
	}
}

// Every table a copy writes holds no ciphertext, so nothing sealed with the
// source workspace's key can land in another workspace.
func TestCopiedTablesCarryNoSealedColumns(t *testing.T) {
	copied := map[string]bool{
		"campaigns": true, "sequences": true, "campaign_ab_variants": true,
		"campaign_advanced_settings": true, "campaign_attachments": true,
		"email_images": true, "reply_templates": true,
	}
	seen := 0
	for _, tbl := range orgtransfer.Tables {
		if !copied[tbl.Name] {
			continue
		}
		seen++
		if len(tbl.Secrets) > 0 {
			t.Errorf("%s now holds sealed columns; the copy must re-seal them for the destination or leave them out", tbl.Name)
		}
	}
	if seen != len(copied) {
		t.Fatalf("found %d of %d copied tables in the registry", seen, len(copied))
	}
}

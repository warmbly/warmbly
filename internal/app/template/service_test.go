package template

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func TestRenderString_BasicSubstitution(t *testing.T) {
	out := RenderString("Hi {{.FirstName}} at {{.Company}}", map[string]string{
		"FirstName": "Ada",
		"Company":   "Acme",
	})
	if out != "Hi Ada at Acme" {
		t.Fatalf("got %q", out)
	}
}

func TestRenderString_MissingPlaceholdersStripped(t *testing.T) {
	// Unknown placeholders must NOT leak the {{.Key}} syntax into output.
	out := RenderString("Hi {{.FirstName}} from {{.Unknown}}!", map[string]string{
		"FirstName": "Ada",
	})
	if out != "Hi Ada from !" {
		t.Fatalf("got %q", out)
	}
}

func TestRenderString_Empty(t *testing.T) {
	if got := RenderString("", map[string]string{"a": "b"}); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestRenderString_NoPlaceholdersUnchanged(t *testing.T) {
	src := "Just a regular sentence."
	if got := RenderString(src, nil); got != src {
		t.Fatalf("got %q", got)
	}
}

// fakeRepo is a minimal in-memory TemplateRepository for service tests.
// Only behaviors exercised by the tests are implemented faithfully.
type fakeRepo struct {
	items          map[uuid.UUID]*models.ReplyTemplate
	listErr        error
	updateErr      error
	reorderHistory [][]uuid.UUID
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{items: map[uuid.UUID]*models.ReplyTemplate{}}
}

func (f *fakeRepo) Create(_ context.Context, orgID, userID uuid.UUID, data *models.CreateReplyTemplate) (*models.ReplyTemplate, error) {
	t := &models.ReplyTemplate{
		ID:             uuid.New(),
		OrganizationID: orgID,
		UserID:         userID,
		Name:           data.Name,
		Subject:        data.Subject,
		BodyHTML:       data.BodyHTML,
		BodyPlain:      data.BodyPlain,
		Position:       len(f.items) + 1,
	}
	f.items[t.ID] = t
	return t, nil
}

func (f *fakeRepo) GetByID(_ context.Context, orgID, templateID uuid.UUID) (*models.ReplyTemplate, error) {
	t, ok := f.items[templateID]
	if !ok || t.OrganizationID != orgID {
		return nil, nil
	}
	return t, nil
}

func (f *fakeRepo) List(_ context.Context, orgID uuid.UUID, _ string) ([]models.ReplyTemplate, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := []models.ReplyTemplate{}
	for _, t := range f.items {
		if t.OrganizationID == orgID {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (f *fakeRepo) Update(_ context.Context, orgID, templateID uuid.UUID, data *models.UpdateReplyTemplate) (*models.ReplyTemplate, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	t, ok := f.items[templateID]
	if !ok || t.OrganizationID != orgID {
		return nil, nil
	}
	if data.Name != nil {
		t.Name = *data.Name
	}
	if data.Subject != nil {
		t.Subject = *data.Subject
	}
	if data.BodyHTML != nil {
		t.BodyHTML = *data.BodyHTML
	}
	if data.BodyPlain != nil {
		t.BodyPlain = *data.BodyPlain
	}
	return t, nil
}

func (f *fakeRepo) Delete(_ context.Context, orgID, templateID uuid.UUID) error {
	if t, ok := f.items[templateID]; ok && t.OrganizationID == orgID {
		delete(f.items, templateID)
	}
	return nil
}

func (f *fakeRepo) Duplicate(ctx context.Context, orgID, userID, templateID uuid.UUID) (*models.ReplyTemplate, error) {
	src, _ := f.GetByID(ctx, orgID, templateID)
	if src == nil {
		return nil, nil
	}
	return f.Create(ctx, orgID, userID, &models.CreateReplyTemplate{
		Name:      src.Name + " (copy)",
		Subject:   src.Subject,
		BodyHTML:  src.BodyHTML,
		BodyPlain: src.BodyPlain,
	})
}

func (f *fakeRepo) Reorder(_ context.Context, orgID uuid.UUID, ids []uuid.UUID) error {
	f.reorderHistory = append(f.reorderHistory, ids)
	for i, id := range ids {
		if t, ok := f.items[id]; ok && t.OrganizationID == orgID {
			t.Position = i + 1
		}
	}
	return nil
}

func TestService_Create_TrimsAndRejectsEmpty(t *testing.T) {
	svc := NewService(newFakeRepo())
	ctx := context.Background()
	orgID, userID := uuid.New(), uuid.New()

	if _, xerr := svc.Create(ctx, orgID, userID, &models.CreateReplyTemplate{Name: "   "}); xerr == nil {
		t.Fatal("expected error for whitespace-only name")
	}

	t1, xerr := svc.Create(ctx, orgID, userID, &models.CreateReplyTemplate{Name: "  Hello  "})
	if xerr != nil {
		t.Fatalf("create failed: %v", xerr)
	}
	if t1.Name != "Hello" {
		t.Fatalf("expected name trimmed to 'Hello', got %q", t1.Name)
	}
}

func TestService_Update_RejectsEmptyAndOverlongName(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	ctx := context.Background()
	orgID, userID := uuid.New(), uuid.New()

	t1, _ := svc.Create(ctx, orgID, userID, &models.CreateReplyTemplate{Name: "Initial"})
	empty := "   "
	if _, xerr := svc.Update(ctx, orgID, t1.ID, &models.UpdateReplyTemplate{Name: &empty}); xerr == nil {
		t.Fatal("expected error for whitespace-only name")
	}
	long := make([]byte, 256)
	for i := range long {
		long[i] = 'a'
	}
	s := string(long)
	if _, xerr := svc.Update(ctx, orgID, t1.ID, &models.UpdateReplyTemplate{Name: &s}); xerr == nil {
		t.Fatal("expected error for >255 char name")
	}
}

func TestService_Update_NotFound(t *testing.T) {
	svc := NewService(newFakeRepo())
	ctx := context.Background()
	name := "x"
	_, xerr := svc.Update(ctx, uuid.New(), uuid.New(), &models.UpdateReplyTemplate{Name: &name})
	if xerr == nil || xerr.Code != errx.NotFound {
		t.Fatalf("expected NotFound, got %v", xerr)
	}
}

func TestService_List_InternalErrorBubbles(t *testing.T) {
	repo := newFakeRepo()
	repo.listErr = errors.New("boom")
	svc := NewService(repo)
	_, xerr := svc.List(context.Background(), uuid.New(), "")
	if xerr == nil || xerr.Code != errx.Internal {
		t.Fatalf("expected Internal, got %v", xerr)
	}
}

func TestService_List_NilCoercedToEmptySlice(t *testing.T) {
	svc := NewService(newFakeRepo())
	out, xerr := svc.List(context.Background(), uuid.New(), "")
	if xerr != nil {
		t.Fatalf("unexpected err: %v", xerr)
	}
	if out == nil || len(out) != 0 {
		t.Fatalf("expected empty slice, got %#v", out)
	}
}

func TestService_Duplicate(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	ctx := context.Background()
	orgID, userID := uuid.New(), uuid.New()

	t1, _ := svc.Create(ctx, orgID, userID, &models.CreateReplyTemplate{Name: "Original", Subject: "Re:"})
	dup, xerr := svc.Duplicate(ctx, orgID, userID, t1.ID)
	if xerr != nil {
		t.Fatalf("duplicate failed: %v", xerr)
	}
	if dup.ID == t1.ID {
		t.Fatal("duplicate should have a fresh ID")
	}
	if dup.Name != "Original (copy)" {
		t.Fatalf("expected name 'Original (copy)', got %q", dup.Name)
	}
	if dup.Subject != "Re:" {
		t.Fatalf("subject should be cloned, got %q", dup.Subject)
	}
}

func TestService_Reorder_RejectsEmpty(t *testing.T) {
	svc := NewService(newFakeRepo())
	if xerr := svc.Reorder(context.Background(), uuid.New(), nil); xerr == nil {
		t.Fatal("expected error for nil ids")
	}
}

func TestService_Reorder_RejectsDuplicates(t *testing.T) {
	svc := NewService(newFakeRepo())
	id := uuid.New()
	if xerr := svc.Reorder(context.Background(), uuid.New(), []uuid.UUID{id, id}); xerr == nil {
		t.Fatal("expected error for duplicate ids")
	}
}

func TestService_Reorder_AppliesOrder(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	ctx := context.Background()
	orgID, userID := uuid.New(), uuid.New()

	a, _ := svc.Create(ctx, orgID, userID, &models.CreateReplyTemplate{Name: "A"})
	b, _ := svc.Create(ctx, orgID, userID, &models.CreateReplyTemplate{Name: "B"})
	c, _ := svc.Create(ctx, orgID, userID, &models.CreateReplyTemplate{Name: "C"})

	if xerr := svc.Reorder(ctx, orgID, []uuid.UUID{c.ID, a.ID, b.ID}); xerr != nil {
		t.Fatalf("reorder failed: %v", xerr)
	}

	got, _ := svc.GetByID(ctx, orgID, c.ID)
	if got.Position != 1 {
		t.Fatalf("expected c at pos 1, got %d", got.Position)
	}
	got, _ = svc.GetByID(ctx, orgID, b.ID)
	if got.Position != 3 {
		t.Fatalf("expected b at pos 3, got %d", got.Position)
	}
}

func TestService_Render_ExpandsTemplate(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	ctx := context.Background()
	orgID, userID := uuid.New(), uuid.New()

	t1, _ := svc.Create(ctx, orgID, userID, &models.CreateReplyTemplate{
		Name:      "Hello",
		Subject:   "Re: {{.Topic}}",
		BodyHTML:  "<p>Hi {{.FirstName}}</p>",
		BodyPlain: "Hi {{.FirstName}}",
	})

	out, xerr := svc.Render(ctx, orgID, t1.ID, map[string]string{
		"Topic":     "Pricing",
		"FirstName": "Ada",
	})
	if xerr != nil {
		t.Fatalf("render failed: %v", xerr)
	}
	if out.Subject != "Re: Pricing" {
		t.Fatalf("subject: %q", out.Subject)
	}
	if out.BodyHTML != "<p>Hi Ada</p>" {
		t.Fatalf("html: %q", out.BodyHTML)
	}
	if out.BodyPlain != "Hi Ada" {
		t.Fatalf("plain: %q", out.BodyPlain)
	}
}

func TestService_Render_NotFound(t *testing.T) {
	svc := NewService(newFakeRepo())
	_, xerr := svc.Render(context.Background(), uuid.New(), uuid.New(), nil)
	if xerr == nil || xerr.Code != errx.NotFound {
		t.Fatalf("expected NotFound, got %v", xerr)
	}
}

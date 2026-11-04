package sequence

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
	"github.com/warmbly/warmbly/internal/repository"
)

type recordingRepo struct {
	repository.SequenceRepository
	saved *models.UpdateSequence
	steps []models.Sequence
}

func (r *recordingRepo) Get(_ context.Context, _, _ string) ([]models.Sequence, *errx.Error) {
	return r.steps, nil
}

func (r *recordingRepo) Update(_ context.Context, _, _, _ string, data *models.UpdateSequence) (*models.Sequence, *errx.Error) {
	r.saved = data
	return &models.Sequence{}, nil
}

func TestUpdateRefusesCopyThatWouldNotRender(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := []struct {
		name string
		data models.UpdateSequence
		want string
	}{
		{"unclosed if in plain body", models.UpdateSequence{BodyPlain: str("{{if .Company}}hi"), BodyHTML: str("<p>ok</p>")}, "plain-text body"},
		{"unknown bare name in html body", models.UpdateSequence{BodyHTML: str("<p>Hi {{frist_name}}</p>")}, `unknown variable "frist_name"`},
		{"unknown function in subject", models.UpdateSequence{Subject: str("{{shout .FirstName}}")}, "subject"},
	}
	for _, tc := range cases {
		repo := &recordingRepo{}
		_, xerr := NewService(repo).Update(context.Background(), "org", "c", "s", &tc.data)
		if xerr == nil || xerr.Identifier != "invalid_template" || !strings.Contains(xerr.Message, tc.want) {
			t.Errorf("%s: got %+v, want invalid_template naming %q", tc.name, xerr, tc.want)
		}
		if repo.saved != nil {
			t.Errorf("%s: refused copy was saved", tc.name)
		}
	}
}

func TestUpdateAcceptsAliasesAndCustomFields(t *testing.T) {
	body := `<p>Hi {{first_name}}, {{if .company_name}}at {{.company_name}}{{end}} {{.job title}} {{.anything_custom | default "x"}}</p>`
	repo := &recordingRepo{}
	if _, xerr := NewService(repo).Update(context.Background(), "org", "c", "s", &models.UpdateSequence{BodyHTML: &body}); xerr != nil {
		t.Fatalf("valid copy refused: %v", xerr)
	}
	want := `<p>Hi {{.FirstName}}, {{if .company_name}}at {{.company_name}}{{end}} {{.job title}} {{.anything_custom | default "x"}}</p>`
	if repo.saved == nil || *repo.saved.BodyHTML != want {
		t.Fatalf("only the bare alias is stored in canonical form, got %q", *repo.saved.BodyHTML)
	}
}

func TestUpdateDerivesHTMLForPlainOnlyCopy(t *testing.T) {
	id := uuid.New()
	plain := "Hi {{first_name}},\n\nWorth a chat?"
	repo := &recordingRepo{steps: []models.Sequence{{ID: id}}}
	if _, xerr := NewService(repo).Update(context.Background(), "org", "c", id.String(), &models.UpdateSequence{BodyPlain: &plain}); xerr != nil {
		t.Fatalf("valid plain-only copy refused: %v", xerr)
	}
	canonical := "Hi {{.FirstName}},\n\nWorth a chat?"
	if repo.saved == nil || *repo.saved.BodyPlain != canonical || repo.saved.BodyHTML == nil || *repo.saved.BodyHTML != mailhtml.FromText(canonical) {
		t.Fatalf("plain-only copy must store HTML derived from it, got %+v", repo.saved)
	}
	broken := "Hi {{if .Company}}"
	repo = &recordingRepo{steps: []models.Sequence{{ID: id}}}
	if _, xerr := NewService(repo).Update(context.Background(), "org", "c", id.String(), &models.UpdateSequence{BodyPlain: &broken}); xerr == nil || repo.saved != nil {
		t.Fatal("broken plain-only copy must be refused before any HTML is derived or saved")
	}
}

func TestUpdateRefusesEscaperBeforePipeInHTMLOnly(t *testing.T) {
	body := `{{html .FirstName | printf "%s"}}`
	repo := &recordingRepo{}
	if _, xerr := NewService(repo).Update(context.Background(), "org", "c", "s", &models.UpdateSequence{BodyHTML: &body}); xerr == nil || xerr.Identifier != "invalid_template" {
		t.Fatalf("an HTML body the HTML renderer refuses should be refused, got %v", xerr)
	}
	repo = &recordingRepo{}
	if _, xerr := NewService(repo).Update(context.Background(), "org", "c", "s", &models.UpdateSequence{Subject: &body}); xerr != nil {
		t.Fatalf("the same text in a plain subject renders, got %v", xerr)
	}
}

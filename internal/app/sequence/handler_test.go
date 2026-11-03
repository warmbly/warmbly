package sequence

import (
	"context"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type recordingRepo struct {
	repository.SequenceRepository
	saved *models.UpdateSequence
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
	if repo.saved == nil || *repo.saved.BodyHTML != body {
		t.Fatal("copy must be stored as written, not rewritten")
	}
}

package tasks

import (
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// A lead's preview and every send to them carry the same spintax choices.
func TestRenderLeadCopyIsStablePerLeadAndStep(t *testing.T) {
	campaign, step := uuid.New(), uuid.New()
	contact := models.Contact{ID: uuid.New(), FirstName: "Ada"}
	subject := "{Hi|Hello|Hey} {{.FirstName}}"
	html := "<p>{one|two|three|four|five|six}</p>"
	plain := "{one|two|three|four|five|six}"

	s1, h1, p1 := renderLeadCopy(campaign, step, contact, TemplateContext{}, subject, html, plain)
	for i := 0; i < 20; i++ {
		s2, h2, p2 := renderLeadCopy(campaign, step, contact, TemplateContext{}, subject, html, plain)
		if s1 != s2 || h1 != h2 || p1 != p2 {
			t.Fatalf("render %d differs: %q/%q/%q vs %q/%q/%q", i, s1, h1, p1, s2, h2, p2)
		}
	}
	if "<p>"+p1+"</p>" != h1 {
		t.Fatalf("HTML and plain parts chose differently: %q vs %q", h1, p1)
	}

	// Across leads the choices still vary.
	seen := map[string]bool{}
	for i := 0; i < 40; i++ {
		other := models.Contact{ID: uuid.New(), FirstName: "Ada"}
		_, _, p := renderLeadCopy(campaign, step, other, TemplateContext{}, subject, html, plain)
		seen[p] = true
	}
	if len(seen) < 3 {
		t.Fatalf("spintax barely varies across leads: %v", seen)
	}
}

func TestMissingForListsOnlyUnguardedEmptyFields(t *testing.T) {
	values := map[string]string{"FirstName": "", "Company": "", "LastName": "Lovelace"}
	got := missingFor(values,
		"Hi {{.FirstName}} at {{.LastName}}",
		`{{if .Company}}About {{.Company}}{{end}}`,
	)
	if want := []string{"FirstName"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("missingFor = %v, want %v", got, want)
	}
}

// A preview marks written AI copy and leaves a marker where copy is still to come.
func TestPreviewAIVariablesMarksWrittenAndPending(t *testing.T) {
	body := `<p><span data-ai-var="a" data-ai-config="e30=">[[ai:a]]</span> <span data-ai-var="b" data-ai-config="e30=">[[ai:b]]</span></p>`
	_, html, plain := previewAIVariables("", body, "[[ai:a]] [[ai:b]]", map[string]string{"a": "Loved the <launch>"},
		[]AIBlockRef{{ID: "a", Name: "Opener"}, {ID: "b", Name: "Hook"}})
	if want := `<p><span data-ai-block="a">Loved the &lt;launch&gt;</span> <span data-ai-pending="b" data-ai-name="Hook"></span></p>`; html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
	if plain != "Loved the <launch> [AI: Hook]" {
		t.Fatalf("plain = %q", plain)
	}
}

package tasks

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// passThrough is the control arm: it hands back whatever copy it was given.
type passThrough struct{ variant *models.VariantSelection }

func (p passThrough) SelectVariant(_ context.Context, _, _, _, _ uuid.UUID, subject, bodyHTML, bodyPlain string) (*models.VariantSelection, *errx.Error) {
	if p.variant != nil {
		return p.variant, nil
	}
	return &models.VariantSelection{Subject: subject, BodyHTML: bodyHTML, BodyPlain: bodyPlain}, nil
}

// A template action a contact typed into a field is sent as text, on the control arm and on a variant.
func TestSendRendersContactDataOnce(t *testing.T) {
	contact := models.Contact{ID: uuid.New(), Email: "ann@example.com",
		FirstName:    `{{.Email}}{{range $k, $v := .}}{{$k}}={{$v}};{{end}}`,
		CustomFields: map[string]string{"internal_note": "never shown"}}
	variantID := uuid.New()
	variant := &models.VariantSelection{Subject: "Hi {{.FirstName}}", BodyHTML: "<p>Hi {{.FirstName}}</p>", BodyPlain: "Hi {{.FirstName}}",
		StepArm: &models.StepABArm{VariantID: &variantID}}
	for name, picker := range map[string]variantSelector{"no A/B": nil, "control arm": passThrough{}, "variant": passThrough{variant: variant}} {
		t.Run(name, func(t *testing.T) {
			rendered, xerr := selectAndRender(context.Background(), picker, uuid.New(), uuid.New(), uuid.New(), contact, TemplateContext{}, "",
				"Hi {{.FirstName}}", "<p>Hi {{.FirstName}}</p>", "Hi {{.FirstName}}", nil)
			if xerr != nil {
				t.Fatal(xerr)
			}
			// The drawn arm reaches the reservation, so the send records which copy it carried.
			if wantArm := name == "variant"; (rendered.Arm != nil) != wantArm || (wantArm && *rendered.Arm.VariantID != variantID) {
				t.Fatalf("arm = %+v", rendered.Arm)
			}
			for _, out := range []string{rendered.Subject, rendered.BodyHTML, rendered.BodyPlain} {
				if strings.Contains(out, "ann@example.com") || strings.Contains(out, "never shown") || !strings.Contains(out, "{{.Email}}") {
					t.Fatalf("contact data was rendered as a template: %q", out)
				}
			}
		})
	}
}

// A variant's own form-link markers are resolved, not sent as literal text.
func TestSendResolvesFormLinksInTheChosenVariant(t *testing.T) {
	variant := &models.VariantSelection{Subject: "Book {{form_link:abc123}}", BodyHTML: "<p>{{form_link:abc123}}</p>", BodyPlain: "{{form_link:abc123}}"}
	resolve := func(parts ...*string) {
		for _, p := range parts {
			*p = models.FormLinkMarkerRE.ReplaceAllString(*p, "https://forms.example/f")
		}
	}
	rendered, xerr := selectAndRender(context.Background(), passThrough{variant: variant}, uuid.New(), uuid.New(), uuid.New(), models.Contact{ID: uuid.New()}, TemplateContext{}, "",
		"step", "<p>step</p>", "step", resolve)
	if xerr != nil {
		t.Fatal(xerr)
	}
	for _, out := range []string{rendered.Subject, rendered.BodyHTML, rendered.BodyPlain} {
		if strings.Contains(out, "form_link") || !strings.Contains(out, "https://forms.example/f") {
			t.Fatalf("variant marker not resolved: %q", out)
		}
	}
}

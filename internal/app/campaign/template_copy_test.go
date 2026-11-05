package campaign

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestCheckCreateCopyNamesTheBrokenStepAndVariant(t *testing.T) {
	ok := models.CreateSequenceInput{Subject: "Hi {{first_name}}", BodyPlain: "{{if .Company}}x{{end}}"}
	cases := []struct {
		data models.CreateCampaign
		want string
	}{
		{models.CreateCampaign{Sequences: []models.CreateSequenceInput{ok, {BodyHTML: "<p>{{if .X}}</p>"}}}, "step 2 body"},
		{models.CreateCampaign{Sequences: []models.CreateSequenceInput{ok}, Variants: []models.CreateCampaignABVariantRequest{{Name: "B", Subject: "{{nope}}"}}}, `variant "B" subject`},
	}
	for _, tc := range cases {
		err := checkCreateCopy(&tc.data)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("got %v, want an error naming %q", err, tc.want)
		}
	}
	if err := checkCreateCopy(&models.CreateCampaign{Sequences: []models.CreateSequenceInput{ok}}); err != nil {
		t.Errorf("valid copy refused: %v", err)
	}
}

func TestCheckActiveVariantCopySkipsInactiveAndControl(t *testing.T) {
	broken := "{{if .Company}}x"
	step := uuid.New()
	variants := []models.CampaignABVariant{
		{Name: "off", BodyPlain: broken, IsActive: false},
		{Name: "control", BodyPlain: broken, IsActive: true, IsControl: true, SequenceID: &step},
		{Name: "ok", Subject: "Hi {{first_name}}", IsActive: true},
	}
	if err := checkActiveVariantCopy(variants); err != nil {
		t.Fatalf("inactive or control arms refused: %v", err)
	}
	variants = append(variants, models.CampaignABVariant{Name: "B", BodyHTML: "<p>" + broken + "</p>", IsActive: true})
	if err := checkActiveVariantCopy(variants); err == nil || !strings.Contains(err.Error(), `variant "B" body`) {
		t.Fatalf("got %v, want the broken active variant named", err)
	}
	// A campaign-level control arm sends its own copy, so it is checked.
	campaignControl := []models.CampaignABVariant{{Name: "campaign control", Subject: broken, IsActive: true, IsControl: true}}
	if err := checkActiveVariantCopy(campaignControl); err == nil {
		t.Fatal("a campaign-level control arm's broken copy should be refused")
	}
}

func TestCanonicalizeCreateCopy(t *testing.T) {
	data := &models.CreateCampaign{
		Sequences: []models.CreateSequenceInput{{Subject: "Hi {{first_name}}", BodyPlain: "{{.first_name}}"}},
		Variants:  []models.CreateCampaignABVariantRequest{{Name: "B", BodyHTML: "<p>{{company_name}}</p>"}},
	}
	canonicalizeCreateCopy(data)
	if data.Sequences[0].Subject != "Hi {{.FirstName}}" || data.Sequences[0].BodyPlain != "{{.first_name}}" || data.Variants[0].BodyHTML != "<p>{{.Company}}</p>" {
		t.Fatalf("got %+v", data)
	}
}

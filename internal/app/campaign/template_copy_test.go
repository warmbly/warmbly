package campaign

import (
	"strings"
	"testing"

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

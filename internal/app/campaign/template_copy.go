package campaign

import (
	"fmt"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/tmplvars"
)

// stepCopy names the three copy fields of one step or variant for the validator.
func stepCopy(owner, subject, bodyHTML, bodyPlain string) []tmplvars.Part {
	return []tmplvars.Part{
		{Name: owner + " subject", Text: subject},
		{Name: owner + " body", Text: bodyHTML},
		{Name: owner + " plain-text body", Text: bodyPlain},
	}
}

// checkCreateCopy refuses a new campaign whose steps or variants would not render as written.
func checkCreateCopy(data *models.CreateCampaign) error {
	var parts []tmplvars.Part
	for i, seq := range data.Sequences {
		parts = append(parts, stepCopy(fmt.Sprintf("step %d", i+1), seq.Subject, seq.BodyHTML, seq.BodyPlain)...)
	}
	for _, v := range data.Variants {
		parts = append(parts, stepCopy(fmt.Sprintf("variant %q", v.Name), v.Subject, v.BodyHTML, v.BodyPlain)...)
	}
	return tmplvars.CheckCopy(parts...)
}

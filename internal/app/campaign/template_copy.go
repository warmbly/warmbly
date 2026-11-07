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
		{Name: owner + " body", Text: bodyHTML, HTML: true},
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

// canonicalizeCreateCopy stores the new campaign's copy in canonical form.
func canonicalizeCreateCopy(data *models.CreateCampaign) {
	for i := range data.Sequences {
		seq := &data.Sequences[i]
		tmplvars.CanonicalizeAll(&seq.Subject, &seq.BodyHTML, &seq.BodyPlain)
	}
	for i := range data.Variants {
		v := &data.Variants[i]
		tmplvars.CanonicalizeAll(&v.Subject, &v.BodyHTML, &v.BodyPlain)
	}
}

// checkActiveVariantCopy refuses a start while an active variant that sends its own copy would not render.
func checkActiveVariantCopy(variants []models.CampaignABVariant) error {
	var parts []tmplvars.Part
	for _, v := range variants {
		if v.IsActive && models.VariantSendsOwnCopy(v.SequenceID, v.IsControl) {
			parts = append(parts, stepCopy(fmt.Sprintf("variant %q", v.Name), v.Subject, v.BodyHTML, v.BodyPlain)...)
		}
	}
	return tmplvars.CheckCopy(parts...)
}

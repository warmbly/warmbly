package tasks

import (
	"context"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/net/publicsuffix"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"github.com/warmbly/warmbly/internal/pkg/tmplvars"
	"github.com/warmbly/warmbly/internal/pkg/warmlint"
)

// warmupFillableFields are the built-in contact fields a pool recipient can
// supply: its mailbox name, its address and a company named by a business
// domain. A lead's phone or custom fields have no counterpart.
var warmupFillableFields = map[string]bool{
	"FirstName":              true,
	"LastName":               true,
	"Email":                  true,
	"Company":                true,
	tmplvars.SenderField:     true,
	tmplvars.UnsubscribeLink: true,
}

// campaignCopyIneligible says in a sentence why warmup cannot carry a step's
// copy as the campaign sends it, or "" when it can for a recipient who has
// every field it reads.
func campaignCopyIneligible(subject, bodyHTML, bodyPlain string) string {
	if strings.TrimSpace(subject) == "" {
		return "The step has no subject of its own."
	}
	if strings.TrimSpace(bodyHTML) == "" && strings.TrimSpace(bodyPlain) == "" {
		return "The step has no body."
	}
	parts := []string{subject, bodyHTML, bodyPlain}
	for _, p := range parts {
		if aiVarTokenRE.MatchString(p) || aiVarSpanRE.MatchString(p) {
			return "It has AI blocks, which are written for each lead."
		}
		if models.FormLinkMarkerRE.MatchString(p) {
			return "It links a form, which is personalised for each lead."
		}
	}
	for _, p := range parts {
		builtins, other, ok := tmplvars.ContactReads(p)
		if !ok {
			return "Its merge fields do not compile."
		}
		if other {
			return "It uses custom fields, which a warmup recipient does not have."
		}
		for field := range builtins {
			if !warmupFillableFields[field] {
				return "It uses the lead's " + strings.ToLower(field) + ", which a warmup recipient does not have."
			}
		}
	}
	plain := bodyPlain
	if strings.TrimSpace(plain) == "" {
		plain = ExtractPlainTextFromHTML(bodyHTML)
	}
	if generation.ValidateWarmupWording(subject, plain) != nil {
		return "It calls itself automated, a test or a warmup, which warmup never sends."
	}
	if err := warmlint.Check(subject, plain, false); err != nil {
		return "It reads as spam to the warmup checks: " + err.Error() + "."
	}
	return ""
}

// warmupRecipientContact is a pool recipient as campaign copy addresses it.
// The name is another workspace's, so it reaches the copy only when it is safe
// to show; otherwise the fields stay empty and copy that needs them waits.
func warmupRecipientContact(recipient *Email) models.Contact {
	first, last := splitSenderName(displayname.Displayable(recipient.Name))
	return models.Contact{
		ID:        recipient.ID,
		FirstName: first,
		LastName:  last,
		Email:     recipient.Email,
		Company:   companyFromAddress(recipient.Email),
	}
}

// companyFromAddress names the company a business address belongs to from its
// registrable domain ("ops@north-ridge.co.uk" is "North Ridge"); a consumer
// mail domain names nobody's company.
func companyFromAddress(address string) string {
	at := strings.LastIndex(address, "@")
	if at < 0 {
		return ""
	}
	domain := strings.ToLower(strings.TrimSpace(address[at+1:]))
	if domain == "" || mailhost.SharedProvider(domain) {
		return ""
	}
	etld1, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		return ""
	}
	label, _, _ := strings.Cut(etld1, ".")
	if strings.HasPrefix(label, "xn--") {
		return ""
	}
	words := strings.FieldsFunc(label, func(r rune) bool { return r == '-' || r == '_' })
	for i, w := range words {
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return displayname.Displayable(strings.Join(words, " "))
}

// copyFillable reports whether the contact has a value for every built-in
// field the copy reads. A merge field with a fallback is held to it too: the
// copy is skipped for this recipient rather than guessed at.
func copyFillable(c models.Contact, parts ...string) bool {
	have := map[string]bool{"FirstName": c.FirstName != "", "LastName": c.LastName != "", "Email": c.Email != "", "Company": c.Company != ""}
	for _, p := range parts {
		builtins, _, _ := tmplvars.ContactReads(p)
		for field := range builtins {
			if filled, contactField := have[field]; contactField && !filled {
				return false
			}
		}
	}
	return true
}

// campaignCopyContent renders the least recently carried opening arm of the
// sender's running campaigns for this recipient, exactly as the campaign
// renders it (merge fields, spintax, plain-text rule, signature, opt-out, UTM
// tags, and the open pixel and wrapped links on its tracking domain), skipping
// any arm warmup cannot carry faithfully. ok is false when no arm qualifies,
// and the thread opens with ordinary content instead.
func (s *tasksService) campaignCopyContent(ctx context.Context, account Email, recipient *Email, taskID uuid.UUID) (warmupContent, bool, error) {
	if account.OrganizationID == nil || recipient == nil || s.campaignRepo == nil {
		return warmupContent{}, false, nil
	}
	orgID := *account.OrganizationID
	candidates, err := s.warmupContentRepo.CampaignCopyCandidates(ctx, orgID, account.ID, recipient.ID)
	if err != nil {
		return warmupContent{}, false, err
	}
	contact := warmupRecipientContact(recipient)
	campaigns := map[uuid.UUID]*models.Campaign{}
	for _, c := range candidates {
		if campaignCopyIneligible(c.Subject, c.BodyHTML, c.BodyPlain) != "" || !copyFillable(contact, c.Subject, c.BodyHTML, c.BodyPlain) {
			continue
		}
		campaign, seen := campaigns[c.CampaignID]
		if !seen {
			found, err := s.campaignRepo.GetByID(ctx, c.CampaignID)
			if err != nil || found == nil || found.OrganizationID == nil || *found.OrganizationID != orgID {
				found = nil
			}
			campaigns[c.CampaignID], campaign = found, found
		}
		if campaign == nil {
			continue
		}
		base := s.renderCopyBase(ctx, orgID, campaign, c.SequenceID, &account, contact, renderAI{drop: true}, c.Subject, c.BodyHTML, c.BodyPlain)
		if base.Refusal != "" {
			continue
		}
		subject := strings.TrimSpace(base.Subject)
		// Spintax and merge values are only known now; checking before tracking mints no tickets for refused copy.
		if !renderedCopySendable(subject, base.BodyHTML, base.BodyPlain) {
			continue
		}
		// Tickets name no campaign: an open or click on warmup mail is not a lead's, and the consumer drops it.
		base.CampaignID = uuid.Nil
		bodyHTML, bodyPlain, tracking := s.finishPlacementProbe(ctx, base, taskID, campaign.OpenTracking, campaign.LinkTracking)
		if bodyPlain == "" && bodyHTML != "" {
			bodyPlain = ExtractPlainTextFromHTML(bodyHTML)
		}
		// The signature and opt-out go on last, and the send path lints the whole body.
		if !renderedCopySendable(subject, "", bodyPlain) {
			continue
		}
		sequenceID := c.SequenceID
		return warmupContent{
			subject:          subject,
			body:             bodyPlain,
			bodyHTML:         bodyHTML,
			unsubscribeURL:   base.HeaderURL,
			tracking:         tracking,
			contentSource:    models.WarmupContentSourceCampaign,
			sequenceID:       &sequenceID,
			variantID:        c.VariantID,
			scenarioVersion:  generation.WarmupScenarioVersion,
			renderingVersion: generation.CanonicalRenderingVersion,
			maxTurns:         models.WarmupOwnCopyTurns,
		}, true, nil
	}
	return warmupContent{}, false, nil
}

// renderedCopySendable holds rendered copy to the gates the warmup send path applies.
func renderedCopySendable(subject, bodyHTML, bodyPlain string) bool {
	if bodyPlain == "" && bodyHTML != "" {
		bodyPlain = ExtractPlainTextFromHTML(bodyHTML)
	}
	if subject == "" || strings.ContainsAny(subject, "\r\n\x00") || strings.TrimSpace(bodyPlain) == "" || strings.IndexByte(bodyPlain, 0) >= 0 {
		return false
	}
	return generation.ValidateWarmupWording(subject, bodyPlain) == nil && warmlint.Check(subject, bodyPlain, false) == nil
}

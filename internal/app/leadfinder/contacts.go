package leadfinder

import (
	"context"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/emailverify"
)

// AddToContactsRequest turns selected leads, or the people at them, into contacts.
type AddToContactsRequest struct {
	models.LeadSelection
	CampaignIDs []string `json:"campaign_ids"`
	CategoryIDs []string `json:"category_ids"`
	SegmentIDs  []string `json:"segment_ids"`
	// VerifiedOnly keeps out every address that did not check as valid.
	VerifiedOnly bool `json:"verified_only"`
	// Who is "business" (each lead's own address, the default), "people" (the
	// people found at each lead) or "both".
	Who string `json:"who"`
	// IncludeGuessed adds people whose address was worked out but not confirmed.
	IncludeGuessed bool `json:"include_guessed"`
	// KeepExisting leaves a contact already in the workspace as it is; it
	// still gets the segments, labels and campaigns.
	KeepExisting bool `json:"keep_existing"`
	// SkipActiveInOtherCampaigns enrolls nobody another campaign is emailing.
	SkipActiveInOtherCampaigns bool `json:"skip_active_in_other_campaigns"`
}

// AddToContactsResult says what became of the selection.
type AddToContactsResult struct {
	Selected   int                            `json:"selected"`
	Imported   int                            `json:"imported"`
	Updated    int                            `json:"updated"`
	Skipped    int                            `json:"skipped"`
	Failed     int                            `json:"failed"`
	NoEmail    int                            `json:"no_email"`
	Unverified int                            `json:"unverified"`
	Suppressed int                            `json:"suppressed"`
	Guessed    int                            `json:"guessed"`
	People     int                            `json:"people"`
	Errors     []models.ContactImportRowError `json:"errors,omitempty"`
}

// leadColumns are the contact fields a lead fills, in cell order.
var leadColumns = []struct {
	target models.ContactImportColumnTarget
	custom string
	value  func(l *models.Lead) string
}{
	{models.ContactImportTargetEmail, "", func(l *models.Lead) string { return l.Email }},
	{models.ContactImportTargetCompany, "", func(l *models.Lead) string { return l.Name }},
	{models.ContactImportTargetPhone, "", func(l *models.Lead) string { return l.Phone }},
	{models.ContactImportTargetVerificationStatus, "", func(l *models.Lead) string {
		if e := primaryEmail(l); e != nil {
			switch e.Status {
			case string(emailverify.StatusValid), string(emailverify.StatusRisky), string(emailverify.StatusInvalid):
				return e.Status
			}
		}
		return ""
	}},
	{models.ContactImportTargetCustom, "website", func(l *models.Lead) string { return l.Website }},
	{models.ContactImportTargetCustom, "business_category", func(l *models.Lead) string { return l.Category }},
	{models.ContactImportTargetCustom, "address", func(l *models.Lead) string { return l.Address }},
	{models.ContactImportTargetCustom, "city", func(l *models.Lead) string { return l.City }},
	{models.ContactImportTargetCustom, "state", func(l *models.Lead) string { return l.Region }},
	{models.ContactImportTargetCustom, "postcode", func(l *models.Lead) string { return l.Postcode }},
	{models.ContactImportTargetCustom, "country", func(l *models.Lead) string { return l.Country }},
	{models.ContactImportTargetCustom, "rating", func(l *models.Lead) string { return fmtFloat(l.Rating) }},
	{models.ContactImportTargetCustom, "review_count", func(l *models.Lead) string { return fmtInt(l.ReviewCount) }},
	{models.ContactImportTargetCustom, "map_url", func(l *models.Lead) string { return l.MapsURL }},
	{models.ContactImportTargetCustom, "facebook_url", social("facebook")},
	{models.ContactImportTargetCustom, "instagram_url", social("instagram")},
	{models.ContactImportTargetCustom, "linkedin_url", social("linkedin")},
	{models.ContactImportTargetCustom, "website_platform", func(l *models.Lead) string { return l.Site.Platform }},
	{models.ContactImportTargetCustom, "lead_score", func(l *models.Lead) string { return strconv.Itoa(l.Score) }},
}

// personColumns are the contact fields a person at a lead fills, in cell order.
var personColumns = []struct {
	target models.ContactImportColumnTarget
	custom string
	value  func(p *models.LeadPerson, l *models.Lead) string
}{
	{models.ContactImportTargetEmail, "", func(p *models.LeadPerson, _ *models.Lead) string { return p.Email }},
	{models.ContactImportTargetFirstName, "", func(p *models.LeadPerson, _ *models.Lead) string { return p.FirstName }},
	{models.ContactImportTargetLastName, "", func(p *models.LeadPerson, _ *models.Lead) string { return p.LastName }},
	{models.ContactImportTargetCompany, "", func(_ *models.LeadPerson, l *models.Lead) string { return l.Name }},
	{models.ContactImportTargetPhone, "", func(p *models.LeadPerson, l *models.Lead) string { return firstNonEmpty(p.Phone, l.Phone) }},
	{models.ContactImportTargetVerificationStatus, "", func(p *models.LeadPerson, _ *models.Lead) string {
		switch p.EmailStatus {
		case string(emailverify.StatusValid), string(emailverify.StatusRisky), string(emailverify.StatusInvalid):
			return p.EmailStatus
		}
		return ""
	}},
	{models.ContactImportTargetCustom, "job_title", func(p *models.LeadPerson, _ *models.Lead) string { return p.Title }},
	{models.ContactImportTargetCustom, "seniority", func(p *models.LeadPerson, _ *models.Lead) string { return string(p.Seniority) }},
	{models.ContactImportTargetCustom, "linkedin_url", func(p *models.LeadPerson, _ *models.Lead) string { return p.LinkedInURL }},
	{models.ContactImportTargetCustom, "company_linkedin_url", func(_ *models.LeadPerson, l *models.Lead) string { return l.Socials["linkedin"] }},
	{models.ContactImportTargetCustom, "website", func(_ *models.LeadPerson, l *models.Lead) string { return l.Website }},
	{models.ContactImportTargetCustom, "business_category", func(_ *models.LeadPerson, l *models.Lead) string { return l.Category }},
	{models.ContactImportTargetCustom, "address", func(_ *models.LeadPerson, l *models.Lead) string { return l.Address }},
	{models.ContactImportTargetCustom, "city", func(_ *models.LeadPerson, l *models.Lead) string { return l.City }},
	{models.ContactImportTargetCustom, "state", func(_ *models.LeadPerson, l *models.Lead) string { return l.Region }},
	{models.ContactImportTargetCustom, "country", func(_ *models.LeadPerson, l *models.Lead) string { return l.Country }},
	{models.ContactImportTargetCustom, "map_url", func(_ *models.LeadPerson, l *models.Lead) string { return l.MapsURL }},
	{models.ContactImportTargetCustom, "lead_score", func(_ *models.LeadPerson, l *models.Lead) string { return strconv.Itoa(l.Score) }},
}

// AddToContacts imports the selected leads' addresses, the people at them, or
// both as contacts, optionally straight into campaigns, categories and
// segments. People go first, so an owner whose address is also the
// business's lands as a person.
func (s *Service) AddToContacts(ctx context.Context, orgID, userID uuid.UUID, req *AddToContactsRequest) (*AddToContactsResult, error) {
	who := strings.ToLower(strings.TrimSpace(req.Who))
	if who == "" {
		who = "business"
	}
	if who != "business" && who != "people" && who != "both" {
		return nil, errx.New(errx.BadRequest, "who must be business, people or both")
	}
	ids, err := s.Resolve(ctx, orgID, &req.LeadSelection)
	if err != nil {
		return nil, err
	}
	res := &AddToContactsResult{Selected: len(ids)}
	var leads []models.Lead
	for start := 0; start < len(ids); start += 500 {
		batch, err := s.Repo.LeadsByIDs(ctx, orgID, ids[start:min(start+500, len(ids))])
		if err != nil {
			return nil, err
		}
		if who != "business" {
			people, err := s.Repo.PeopleForLeads(ctx, orgID, ids[start:min(start+500, len(ids))])
			if err != nil {
				return nil, err
			}
			for i := range batch {
				batch[i].People = people[batch[i].ID]
			}
		}
		leads = append(leads, batch...)
	}
	var emails []string
	for _, l := range leads {
		if l.Email != "" {
			emails = append(emails, l.Email)
		}
		for _, p := range l.People {
			if p.Email != "" {
				emails = append(emails, p.Email)
			}
		}
	}
	suppressed, err := s.Repo.SuppressedEmails(ctx, orgID, emails)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	usable := func(email, status string) bool {
		switch {
		case email == "":
			res.NoEmail++
		case suppressed[email] || status == "suppressed":
			res.Suppressed++
		case status == string(emailverify.StatusInvalid):
			res.Unverified++
		case status == models.LeadPersonStatusGuessed && !req.IncludeGuessed:
			res.Guessed++
		case req.VerifiedOnly && status != string(emailverify.StatusValid):
			res.Unverified++
		case seen[email]:
			res.Skipped++
		default:
			seen[email] = true
			return true
		}
		return false
	}

	opts := s.importOptions(req)
	if who != "business" {
		type pick struct {
			person *models.LeadPerson
			lead   *models.Lead
		}
		var picks []pick
		for i := range leads {
			l := &leads[i]
			for k := range l.People {
				p := &l.People[k]
				res.People++
				if usable(p.Email, p.EmailStatus) {
					picks = append(picks, pick{p, l})
				}
			}
		}
		if len(picks) > 0 {
			cells := make([][]string, len(picks))
			for i, pk := range picks {
				row := make([]string, len(personColumns))
				for j, c := range personColumns {
					row[j] = truncate(c.value(pk.person, pk.lead), 500)
				}
				cells[i] = row
			}
			mapping := make([]models.ContactImportColumnMapping, len(personColumns))
			for i, c := range personColumns {
				mapping[i] = models.ContactImportColumnMapping{Index: i, Target: c.target, CustomKey: c.custom}
			}
			contacts, err := s.runImport(ctx, orgID, userID, mapping, cells, opts, res)
			if err != nil {
				return nil, err
			}
			links := map[uuid.UUID]uuid.UUID{}
			leadLinks := map[uuid.UUID]uuid.UUID{}
			for i, pk := range picks {
				if c, ok := contacts[i]; ok {
					links[pk.person.ID] = c
					if _, done := leadLinks[pk.lead.ID]; !done && pk.lead.ContactID == nil {
						leadLinks[pk.lead.ID] = c
					}
				}
			}
			if err := s.Repo.LinkPersonContacts(ctx, orgID, links); err != nil {
				return res, errx.New(errx.Internal, "the contacts were added but could not be linked back to their people")
			}
			if who == "people" {
				// The lead counts as reached through its first person.
				if err := s.Repo.LinkContacts(ctx, orgID, leadLinks); err != nil {
					return res, errx.New(errx.Internal, "the contacts were added but could not be linked back to their leads")
				}
			}
		}
	}
	if who == "people" {
		return res, nil
	}

	var fresh []*models.Lead
	for i := range leads {
		l := &leads[i]
		status := ""
		if e := primaryEmail(l); e != nil {
			status = e.Status
		}
		if usable(l.Email, status) {
			fresh = append(fresh, l)
		}
	}
	if len(fresh) == 0 {
		return res, nil
	}
	mapping := make([]models.ContactImportColumnMapping, len(leadColumns))
	for i, c := range leadColumns {
		mapping[i] = models.ContactImportColumnMapping{Index: i, Target: c.target, CustomKey: c.custom}
	}
	cells := make([][]string, len(fresh))
	for i, l := range fresh {
		row := make([]string, len(leadColumns))
		for j, c := range leadColumns {
			row[j] = truncate(c.value(l), 500)
		}
		cells[i] = row
	}
	contacts, err := s.runImport(ctx, orgID, userID, mapping, cells, opts, res)
	if err != nil {
		return nil, err
	}
	links := map[uuid.UUID]uuid.UUID{}
	for i, l := range fresh {
		if c, ok := contacts[i]; ok {
			links[l.ID] = c
		}
	}
	if err := s.Repo.LinkContacts(ctx, orgID, links); err != nil {
		return res, errx.New(errx.Internal, "the contacts were added but could not be linked back to their leads")
	}
	return res, nil
}

func (s *Service) importOptions(req *AddToContactsRequest) *models.ContactImportCommit {
	subscribed := true
	dedup := models.ContactImportDedupUpdate
	if req.KeepExisting {
		dedup = models.ContactImportDedupSkip
	}
	return &models.ContactImportCommit{
		Dedup:                      dedup,
		CategoryIDs:                req.CategoryIDs,
		CampaignIDs:                req.CampaignIDs,
		SegmentIDs:                 req.SegmentIDs,
		SkipActiveInOtherCampaigns: req.SkipActiveInOtherCampaigns,
		SubscribedDefault:          &subscribed,
		Source:                     models.ContactSourceLeadFinder,
		SourceDetail:               "Lead finder",
	}
}

// runImport imports rows through the contact importer, adds its counts to res
// and returns the contact each row (by index) became.
func (s *Service) runImport(ctx context.Context, orgID, userID uuid.UUID, mapping []models.ContactImportColumnMapping, cells [][]string, base *models.ContactImportCommit, res *AddToContactsResult) (map[int]uuid.UUID, error) {
	opts := *base
	opts.Mapping = mapping
	rows := make([]contact.ImportRow, len(cells))
	for i, c := range cells {
		rows[i] = contact.ImportRow{Line: i + 1, Cells: c}
	}
	sink := &outcomeSink{outcomes: map[int]models.ContactImportRowOutcome{}}
	result, xerr := s.Contacts.RunImport(ctx, userID.String(), orgID, rows, &opts, sink, nil)
	if xerr != nil {
		return nil, xerr
	}
	res.Imported += result.Imported
	res.Updated += result.Updated
	res.Failed += result.Failed
	res.Skipped += result.Skipped
	res.Errors = append(res.Errors, result.Errors...)
	out := map[int]uuid.UUID{}
	for i := range cells {
		if o, ok := sink.outcomes[i+1]; ok && o.ContactID != nil && o.Status != models.ContactImportRowFailed {
			out[i] = *o.ContactID
		}
	}
	return out, nil
}

// outcomeSink keeps every row outcome of an import by line.
type outcomeSink struct {
	outcomes map[int]models.ContactImportRowOutcome
}

func (c *outcomeSink) Settle(_ context.Context, outcomes []models.ContactImportRowOutcome) error {
	for _, o := range outcomes {
		c.outcomes[o.Line] = o
	}
	return nil
}

func (c *outcomeSink) Cancelled(context.Context) bool { return false }

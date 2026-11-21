package social

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/contact"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

const (
	maxImport = 500
	// importPages bounds the search pages an import may run to find hits it does not hold.
	importPages = 20
)

// ImportRequest is POST /v1/social/search/import: the hits named by keys, or the first max of the query.
type ImportRequest struct {
	Query        json.RawMessage `json:"query"`
	Keys         []string        `json:"keys"`
	Max          int             `json:"max"`
	AccountID    *uuid.UUID      `json:"account_id"`
	SegmentIDs   []string        `json:"segment_ids"`
	CategoryIDs  []string        `json:"category_ids"`
	CampaignIDs  []string        `json:"campaign_ids"`
	SkipExisting bool            `json:"skip_existing"`
	// FindEmails looks up a work email for each person without one, in the background.
	FindEmails bool `json:"find_emails"`
}

// ImportResult says what became of an import. NoEmail counts people skipped because a contact needs an email
// address and LinkedIn gives none.
type ImportResult struct {
	Created    int         `json:"created"`
	Updated    int         `json:"updated"`
	Skipped    int         `json:"skipped"`
	NoEmail    int         `json:"no_email"`
	ContactIDs []uuid.UUID `json:"contact_ids"`
}

// importColumns are the contact fields a hit fills, in cell order.
var importColumns = []struct {
	target models.ContactImportColumnTarget
	custom string
}{
	{models.ContactImportTargetEmail, ""},
	{models.ContactImportTargetFirstName, ""},
	{models.ContactImportTargetLastName, ""},
	{models.ContactImportTargetCompany, ""},
	{models.ContactImportColumnTarget("social." + models.SocialLinkedIn), ""},
	{models.ContactImportTargetCustom, "job_title"},
	{models.ContactImportTargetCustom, "location"},
}

// Import adds search hits to contacts, segments, categories and campaigns through the contact importer. A
// person already a contact (matched by LinkedIn profile) is updated, or kept as is with skip_existing, and still
// joins the segments, categories and campaigns.
func (s *Service) Import(ctx context.Context, orgID, userID uuid.UUID, in ImportRequest) (*ImportResult, error) {
	if err := s.checkImport(ctx, orgID, in); err != nil {
		return nil, err
	}
	hits, err := s.collect(ctx, orgID, in)
	if err != nil {
		return nil, err
	}
	people, emails, res, err := s.importPeople(ctx, orgID, hits)
	if err != nil {
		return nil, err
	}
	var cells [][]string
	for i := range people {
		if email := emails[i]; email != "" {
			cells = append(cells, hitCells(&people[i], email))
		} else {
			res.Skipped++
			res.NoEmail++
		}
	}
	if _, err := s.importCells(ctx, orgID, userID, in, cells, res); err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Service) checkImport(ctx context.Context, orgID uuid.UUID, in ImportRequest) error {
	if !validQuery(in.Query) {
		return errBadQuery
	}
	switch {
	case len(in.Keys) > maxImport:
		return invalid("Import at most 500 people at once.")
	case len(in.Keys) == 0 && (in.Max < 1 || in.Max > maxImport):
		return invalid("Send keys, or max between 1 and 500.")
	}
	return s.requireSearch(ctx, orgID)
}

// importPeople is the distinct people among hits, each with the email its contact already has ("" when none).
func (s *Service) importPeople(ctx context.Context, orgID uuid.UUID, hits []graphkit.Hit) ([]graphkit.Hit, []string, *ImportResult, error) {
	res := &ImportResult{ContactIDs: []uuid.UUID{}}
	var ids []string
	for i := range hits {
		if id := hitPublicID(&hits[i]); id != "" {
			ids = append(ids, id)
		}
	}
	matches, err := s.Repo.ContactMatches(ctx, orgID, ids)
	if err != nil {
		return nil, nil, nil, err
	}
	contactIDs := make([]uuid.UUID, 0, len(matches))
	for _, m := range matches {
		contactIDs = append(contactIDs, m.ContactID)
	}
	known, err := s.Repo.ContactEmails(ctx, orgID, contactIDs)
	if err != nil {
		return nil, nil, nil, err
	}
	seen := map[string]bool{}
	var people []graphkit.Hit
	var emails []string
	for i := range hits {
		h := &hits[i]
		id := hitPublicID(h)
		if h.Person == nil || id == "" || seen[id] {
			res.Skipped++
			continue
		}
		seen[id] = true
		email := ""
		if m, ok := matches[id]; ok {
			email = known[m.ContactID]
		}
		people = append(people, *h)
		emails = append(emails, email)
	}
	return people, emails, res, nil
}

// importCells runs the contact importer over rows of importColumns and folds its counts into res; the outcome
// of each row is returned by its index.
func (s *Service) importCells(ctx context.Context, orgID, userID uuid.UUID, in ImportRequest, cells [][]string, res *ImportResult) (map[int]models.ContactImportRowOutcome, error) {
	if len(cells) == 0 {
		return map[int]models.ContactImportRowOutcome{}, nil
	}
	mapping := make([]models.ContactImportColumnMapping, len(importColumns))
	for i, c := range importColumns {
		mapping[i] = models.ContactImportColumnMapping{Index: i, Target: c.target, CustomKey: c.custom}
	}
	dedup := models.ContactImportDedupUpdate
	if in.SkipExisting {
		dedup = models.ContactImportDedupSkip
	}
	subscribed := true
	opts := &models.ContactImportCommit{Mapping: mapping, Dedup: dedup, CategoryIDs: in.CategoryIDs, CampaignIDs: in.CampaignIDs,
		SegmentIDs: in.SegmentIDs, SubscribedDefault: &subscribed, Source: models.ContactSourceImport, SourceDetail: "LinkedIn search"}
	rows := make([]contact.ImportRow, len(cells))
	for i, c := range cells {
		rows[i] = contact.ImportRow{Line: i + 1, Cells: c}
	}
	sink := &outcomes{byLine: map[int]models.ContactImportRowOutcome{}}
	result, xerr := s.Contacts.RunImport(ctx, userID.String(), orgID, rows, opts, sink, nil)
	if xerr != nil {
		return nil, xerr
	}
	res.Created += result.Imported
	res.Updated += result.Updated
	res.Skipped += result.Skipped + result.Failed
	out := make(map[int]models.ContactImportRowOutcome, len(cells))
	for i := range cells {
		o, ok := sink.byLine[i+1]
		if !ok {
			continue
		}
		out[i] = o
		if o.ContactID != nil && o.Status != models.ContactImportRowFailed {
			res.ContactIDs = append(res.ContactIDs, *o.ContactID)
		}
	}
	return out, nil
}

func hitCells(h *graphkit.Hit, email string) []string {
	p := h.Person
	title, location := p.Headline, p.Location
	if h.Insights != nil && h.Insights.CurrentTitle != "" {
		title = h.Insights.CurrentTitle
	}
	profile := p.ProfileURL
	if id := models.LinkedInPublicID(firstNonEmpty(p.ProfileURL, p.PublicID)); id != "" {
		profile = models.LinkedInProfileURL(id)
	}
	row := []string{email, p.FirstName, p.LastName, hitCompany(h), profile, title, location}
	for i := range row {
		if r := []rune(row[i]); len(r) > 500 {
			row[i] = string(r[:500])
		}
	}
	return row
}

// collect is the hits an import names: from the recent searches kept in memory, then from the query again.
func (s *Service) collect(ctx context.Context, orgID uuid.UUID, in ImportRequest) ([]graphkit.Hit, error) {
	var out []graphkit.Hit
	want := map[string]bool{}
	for _, k := range in.Keys {
		if k == "" || want[k] {
			continue
		}
		if h, ok := s.recall(orgID, k); ok {
			out = append(out, h)
			continue
		}
		want[k] = true
	}
	if len(in.Keys) > 0 && len(want) == 0 {
		return out, nil
	}
	gk, err := s.graphkitAccount(ctx, orgID, in.AccountID)
	if err != nil {
		return nil, err
	}
	var cursor *string
	for page := 0; page < importPages; page++ {
		got, err := s.api().SearchPeople(ctx, ext(orgID), graphkit.SearchInput{AccountID: gk, Query: in.Query, Cursor: cursor, Limit: maxLimit}, opKey("search.import"))
		if err != nil {
			return nil, s.upstream(ctx, "social.search.import", err)
		}
		s.remember(orgID, got.Data)
		for _, h := range got.Data {
			switch {
			case len(in.Keys) > 0 && want[h.Key]:
				out = append(out, h)
				delete(want, h.Key)
			case len(in.Keys) == 0 && len(out) < in.Max:
				out = append(out, h)
			}
		}
		done := (len(in.Keys) > 0 && len(want) == 0) || (len(in.Keys) == 0 && len(out) >= in.Max)
		if done || got.NextCursor == nil {
			break
		}
		cursor = got.NextCursor
	}
	return out, nil
}

// outcomes keeps every row outcome of an import by line.
type outcomes struct {
	byLine map[int]models.ContactImportRowOutcome
}

func (o *outcomes) Settle(_ context.Context, rows []models.ContactImportRowOutcome) error {
	for _, r := range rows {
		o.byLine[r.Line] = r
	}
	return nil
}

func (o *outcomes) Cancelled(context.Context) bool { return false }

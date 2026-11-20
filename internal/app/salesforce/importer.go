package salesforce

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// importCap bounds how many people one run imports; the rest come in on
	// the next run, which skips everyone already brought in.
	importCap = 10000
	// readCap bounds how many records one run reads to find them.
	readCap = 50000
	// RecurringEvery is how often a "keep in sync" source re-reads its list.
	RecurringEvery = 30 * time.Minute
)

// person is one importable record, whatever it was read from.
type person struct {
	recordID string
	object   string
	email    string
	first    string
	last     string
	company  string
	title    string
	phone    string
	owner    string
	status   string
	optedOut bool
	rec      Record
}

// ImportSource names what to read.
type ImportSource struct {
	SourceKind string `json:"source_kind"`
	Object     string `json:"object"`
	SourceID   string `json:"source_id"`
}

func (in *ImportSource) validate() error {
	in.SourceID = NormalizeID(in.SourceID)
	if !ValidID(in.SourceID) {
		return errx.New(errx.BadRequest, "source_id is not a Salesforce id")
	}
	switch in.SourceKind {
	case "list_view":
		if in.Object != ObjectLead && in.Object != ObjectContact {
			return errx.New(errx.BadRequest, "a list view import reads Leads or Contacts")
		}
	case "campaign":
		in.Object = "CampaignMember"
	default:
		return errx.New(errx.BadRequest, "source_kind must be list_view or campaign")
	}
	return nil
}

var reSelect = regexp.MustCompile(`(?is)^\s*SELECT\s+(.*?)\s+FROM\s+`)

// listViewSOQL rewrites a list view's query to select the fields an import
// needs, keeping its filters, scope and order.
func listViewSOQL(st Settings, object, viewSOQL string) (string, bool) {
	m := reSelect.FindStringSubmatchIndex(viewSOQL)
	if m == nil {
		return "", false
	}
	if strings.Contains(strings.ToUpper(viewSOQL[m[2]:m[3]]), "(SELECT") {
		return "", false
	}
	return "SELECT " + strings.Join(selectFields(st, object), ", ") + " FROM " + viewSOQL[m[1]:], true
}

// read collects up to max people from a source.
func (s *Service) read(ctx context.Context, c *conn, in ImportSource, max int) ([]person, int, error) {
	var out []person
	if in.SourceKind == "campaign" {
		soql := "SELECT LeadId, ContactId, Lead.Email, Lead.FirstName, Lead.LastName, Lead.Company, Lead.Title, Lead.Phone, " +
			"Lead.Status, Lead.Owner.Name, Lead.IsConverted, Lead.HasOptedOutOfEmail, Contact.Email, Contact.FirstName, " +
			"Contact.LastName, Contact.Title, Contact.Phone, Contact.Account.Name, Contact.Owner.Name, Contact.HasOptedOutOfEmail " +
			"FROM CampaignMember WHERE CampaignId = " + Quote(in.SourceID)
		total, err := c.client.Query(ctx, soql, max, func(rows []Record) bool {
			for _, r := range rows {
				// A converted Lead's membership carries the Contact it became.
				if cid := r.String("ContactId"); cid != "" {
					out = append(out, person{
						recordID: NormalizeID(cid), object: ObjectContact,
						email: r.String("Contact.Email"), first: r.String("Contact.FirstName"), last: r.String("Contact.LastName"),
						company: r.String("Contact.Account.Name"), title: r.String("Contact.Title"), phone: r.String("Contact.Phone"),
						owner: r.String("Contact.Owner.Name"), optedOut: r.String("Contact.HasOptedOutOfEmail") == "true",
					})
					continue
				}
				out = append(out, person{
					recordID: NormalizeID(r.String("LeadId")), object: ObjectLead,
					email: r.String("Lead.Email"), first: r.String("Lead.FirstName"), last: r.String("Lead.LastName"),
					company: r.String("Lead.Company"), title: r.String("Lead.Title"), phone: r.String("Lead.Phone"),
					owner: r.String("Lead.Owner.Name"), status: r.String("Lead.Status"),
					optedOut: r.String("Lead.HasOptedOutOfEmail") == "true",
				})
			}
			return true
		})
		return out, total, err
	}

	view, err := c.client.ListViewQuery(ctx, in.Object, in.SourceID)
	if err != nil {
		return nil, 0, err
	}
	collect := func(rows []Record) {
		for _, r := range rows {
			out = append(out, personFrom(in.Object, r))
		}
	}
	if soql, ok := listViewSOQL(c.settings, in.Object, view); ok {
		total, err := c.client.Query(ctx, soql, max, func(rows []Record) bool { collect(rows); return true })
		if err == nil || !IsCode(err, "INVALID_FIELD") {
			return out, total, err
		}
		out = out[:0]
	}
	// The view's own query, then the fields for the ids it returned.
	var ids []string
	total, err := c.client.Query(ctx, view, max, func(rows []Record) bool {
		for _, r := range rows {
			ids = append(ids, NormalizeID(r.String("Id")))
		}
		return true
	})
	if err != nil {
		return nil, 0, err
	}
	for start := 0; start < len(ids); start += 200 {
		rows, err := sfQuery(ctx, c, in.Object, "Id IN "+QuoteList(ids[start:min(start+200, len(ids))]), 0)
		if err != nil {
			return nil, 0, err
		}
		collect(rows)
	}
	return out, total, nil
}

func personFrom(object string, r Record) person {
	p := person{
		recordID: NormalizeID(r.String("Id")), object: object, rec: r,
		email: r.String("Email"), first: r.String("FirstName"), last: r.String("LastName"),
		title: r.String("Title"), phone: r.String("Phone"), owner: r.String("Owner.Name"),
		optedOut: r.Bool("HasOptedOutOfEmail"),
	}
	if object == ObjectLead {
		p.company, p.status = r.String("Company"), r.String("Status")
	} else {
		p.company = r.String("Account.Name")
	}
	return p
}

// PreviewRow is one sample row of a preview.
type PreviewRow struct {
	RecordID      string `json:"record_id"`
	Object        string `json:"object"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	Company       string `json:"company"`
	Title         string `json:"title"`
	OwnerName     string `json:"owner_name"`
	Status        string `json:"status"`
	AlreadyLinked bool   `json:"already_linked"`
}

// Preview is the first rows of a source and how many it holds.
type Preview struct {
	Total  int          `json:"total"`
	Sample []PreviewRow `json:"sample"`
}

// Preview reads the first rows of a source without importing.
func (s *Service) Preview(ctx context.Context, orgID, connID uuid.UUID, in ImportSource) (*Preview, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	c, err := s.open(ctx, orgID, connID)
	if err != nil {
		return nil, err
	}
	defer s.settle(ctx, c)
	people, total, err := s.read(ctx, c, in, 25)
	if err != nil {
		return nil, sfError(err)
	}
	ids := make([]string, 0, len(people))
	for _, p := range people {
		ids = append(ids, p.recordID)
	}
	linked, _ := s.Repo.LinkedRecordIDs(ctx, c.ID, ids)
	out := &Preview{Total: total, Sample: make([]PreviewRow, 0, len(people))}
	for _, p := range people {
		out.Sample = append(out.Sample, PreviewRow{
			RecordID: p.recordID, Object: p.object, Name: strings.TrimSpace(p.first + " " + p.last),
			Email: p.email, Company: p.company, Title: p.title, OwnerName: p.owner, Status: p.status,
			AlreadyLinked: linked[p.recordID],
		})
	}
	return out, nil
}

// SourceInput creates or edits an import source.
type SourceInput struct {
	Name          *string     `json:"name"`
	SourceKind    string      `json:"source_kind"`
	Object        string      `json:"object"`
	SourceID      string      `json:"source_id"`
	SourceLabel   string      `json:"source_label"`
	CampaignID    *uuid.UUID  `json:"campaign_id"`
	ClearCampaign bool        `json:"-"`
	CategoryIDs   []uuid.UUID `json:"category_ids"`
	SegmentIDs    []uuid.UUID `json:"segment_ids"`
	Recurring     *bool       `json:"recurring"`
	Enabled       *bool       `json:"enabled"`
}

// CreateSource saves a source and starts its first run.
func (s *Service) CreateSource(ctx context.Context, orgID, connID, userID uuid.UUID, in SourceInput) (*models.SalesforceImportSource, error) {
	src := ImportSource{SourceKind: in.SourceKind, Object: in.Object, SourceID: in.SourceID}
	if err := src.validate(); err != nil {
		return nil, err
	}
	if _, err := s.open(ctx, orgID, connID); err != nil {
		return nil, err
	}
	if len(in.CategoryIDs) > 50 {
		return nil, errx.New(errx.BadRequest, "at most 50 tags")
	}
	if len(in.SegmentIDs) > models.ImportSourceSegmentsMax {
		return nil, errx.New(errx.BadRequest, fmt.Sprintf("at most %d segments", models.ImportSourceSegmentsMax))
	}
	label := truncate(strings.TrimSpace(in.SourceLabel), 200)
	name := label
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		name = truncate(strings.TrimSpace(*in.Name), 200)
	}
	uid := userID
	row := &models.SalesforceImportSource{
		OrganizationID:  orgID,
		ConnectionID:    connID,
		CreatedByUserID: &uid,
		Name:            name,
		SourceKind:      src.SourceKind,
		SObject:         src.Object,
		SourceID:        src.SourceID,
		SourceLabel:     label,
		CampaignID:      in.CampaignID,
		CategoryIDs:     in.CategoryIDs,
		SegmentIDs:      in.SegmentIDs,
		Recurring:       in.Recurring != nil && *in.Recurring,
		Enabled:         true,
	}
	if err := s.Repo.CreateSource(ctx, row); err != nil {
		if errors.Is(err, repository.ErrSalesforceSourceRefs) {
			return nil, errx.New(errx.BadRequest, "The campaign to enroll into, a label or a segment was not found in this workspace")
		}
		return nil, err
	}
	return s.StartRun(ctx, orgID, row.ID)
}

// UpdateSource edits a source's targets and schedule.
func (s *Service) UpdateSource(ctx context.Context, orgID, id uuid.UUID, in SourceInput) (*models.SalesforceImportSource, error) {
	src, err := s.Repo.GetSource(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, errx.New(errx.NotFound, "import source not found")
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		src.Name = truncate(strings.TrimSpace(*in.Name), 200)
	}
	if in.ClearCampaign {
		src.CampaignID = nil
	} else if in.CampaignID != nil {
		src.CampaignID = in.CampaignID
	}
	if in.CategoryIDs != nil {
		if len(in.CategoryIDs) > 50 {
			return nil, errx.New(errx.BadRequest, "at most 50 tags")
		}
		src.CategoryIDs = in.CategoryIDs
	}
	if in.SegmentIDs != nil {
		if len(in.SegmentIDs) > models.ImportSourceSegmentsMax {
			return nil, errx.New(errx.BadRequest, fmt.Sprintf("at most %d segments", models.ImportSourceSegmentsMax))
		}
		src.SegmentIDs = in.SegmentIDs
	}
	if in.Recurring != nil {
		src.Recurring = *in.Recurring
	}
	if in.Enabled != nil {
		src.Enabled = *in.Enabled
	}
	if err := s.Repo.UpdateSource(ctx, src); err != nil {
		if errors.Is(err, repository.ErrSalesforceSourceRefs) {
			return nil, errx.New(errx.BadRequest, "The campaign to enroll into, a label or a segment was not found in this workspace")
		}
		return nil, err
	}
	return s.Repo.GetSource(ctx, orgID, id)
}

// StartRun claims a source and runs it in the background.
func (s *Service) StartRun(ctx context.Context, orgID, id uuid.UUID) (*models.SalesforceImportSource, error) {
	src, err := s.Repo.GetSource(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, errx.New(errx.NotFound, "import source not found")
	}
	claimed, err := s.Repo.ClaimSource(ctx, id)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, errx.NewWithIdentifier(errx.Conflict, "import_running", "This import is already running.")
	}
	go func(src models.SalesforceImportSource) {
		bg, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		s.runSource(bg, src)
	}(*src)
	src.Status = "running"
	return src, nil
}

// RunRecurring runs every "keep in sync" source that is due.
func (s *Service) RunRecurring(ctx context.Context) error {
	due, err := s.Repo.DueRecurringSources(ctx, RecurringEvery)
	if err != nil {
		return err
	}
	for _, src := range due {
		claimed, err := s.Repo.ClaimSource(ctx, src.ID)
		if err != nil || !claimed {
			continue
		}
		s.runSource(ctx, src)
	}
	return nil
}

func (s *Service) runSource(ctx context.Context, src models.SalesforceImportSource) {
	res, err := s.importOnce(ctx, src)
	msg := ""
	if err != nil {
		msg = describeErr(err)
		var xe *errx.Error
		if errors.As(err, &xe) {
			msg = xe.Message
		}
		log.Warn().Err(err).Str("source", src.ID.String()).Msg("salesforce: import run failed")
	}
	_ = s.Repo.FinishSource(ctx, src.ID, res, msg)
}

// importOnce reads the source, imports people it has not brought in before,
// and links every one of them to its record.
func (s *Service) importOnce(ctx context.Context, src models.SalesforceImportSource) (*models.SalesforceRunResult, error) {
	c, err := s.open(ctx, src.OrganizationID, src.ConnectionID)
	if err != nil {
		return nil, err
	}
	defer s.settle(ctx, c)
	people, total, err := s.read(ctx, c, ImportSource{SourceKind: src.SourceKind, Object: src.SObject, SourceID: src.SourceID}, readCap)
	if err != nil {
		return nil, err
	}
	res := &models.SalesforceRunResult{Read: len(people), Truncated: total > len(people)}
	ids := make([]string, 0, len(people))
	for _, p := range people {
		ids = append(ids, p.recordID)
	}
	seen, err := s.Repo.SourceMembers(ctx, src.ID, ids)
	if err != nil {
		return nil, err
	}
	honourOptOut := c.settings.Inbound.OptOut == "both" || c.settings.Inbound.OptOut == "from_salesforce"
	var fresh []person
	for _, p := range people {
		switch {
		case seen[p.recordID]:
			res.Skipped++
		case !strings.Contains(p.email, "@"):
			// Not remembered: once the record gets an address, it comes in.
			res.NoEmail++
		case p.optedOut && honourOptOut:
			res.OptedOut++
			s.optOutFromSalesforce(ctx, c, repository.SalesforceContact{Email: strings.ToLower(strings.TrimSpace(p.email))})
		case len(fresh) >= importCap:
			res.Truncated = true
		default:
			fresh = append(fresh, p)
		}
	}
	if len(fresh) == 0 {
		return res, nil
	}

	csvBytes, mapping := s.importCSV(c, fresh)
	var campaigns []string
	if src.CampaignID != nil {
		campaigns = []string{src.CampaignID.String()}
	}
	cats := make([]string, 0, len(src.CategoryIDs))
	for _, id := range src.CategoryIDs {
		cats = append(cats, id.String())
	}
	segs := make([]string, 0, len(src.SegmentIDs))
	for _, id := range src.SegmentIDs {
		segs = append(segs, id.String())
	}
	subscribed := true
	opts := &models.ContactImportCommit{
		Mapping:             mapping,
		Dedup:               models.ContactImportDedupUpdate,
		HasHeader:           true,
		CategoryIDs:         cats,
		CampaignIDs:         campaigns,
		SegmentIDs:          segs,
		SkipMissingSegments: true,
		SubscribedDefault:   &subscribed,
		Source:              models.ContactSourceCRMSync,
		SourceDetail:        truncate("Salesforce: "+orDefault(src.SourceLabel, src.Name), 200),
	}
	actor := uuid.Nil
	if src.CreatedByUserID != nil {
		actor = *src.CreatedByUserID
	} else if c.ConnectedByUserID != nil {
		actor = *c.ConnectedByUserID
	}
	result, xerr := s.Contacts.ImportCommit(ctx, actor.String(), src.OrganizationID, bytes.NewReader(csvBytes), "salesforce-import.csv", opts)
	if xerr != nil {
		return res, xerr
	}
	res.Imported, res.Updated, res.Failed = result.Imported, result.Updated, result.Failed
	res.Skipped += result.Skipped

	// Link every row to its record, and remember it for the next run.
	emails := make([]string, 0, len(fresh))
	for _, p := range fresh {
		emails = append(emails, p.email)
	}
	contacts, err := s.Repo.ContactsByEmails(ctx, src.OrganizationID, emails)
	if err != nil {
		return res, nil
	}
	members := map[string]uuid.UUID{}
	for _, p := range fresh {
		ct, ok := contacts[strings.ToLower(strings.TrimSpace(p.email))]
		if !ok {
			continue
		}
		members[p.recordID] = ct.ID
		rec := p.rec
		if rec == nil {
			rec = Record{"Id": p.recordID, "Email": p.email, "FirstName": p.first, "LastName": p.last, "Title": p.title,
				"Phone": p.phone, "Status": p.status, "Owner": map[string]any{"Name": p.owner}}
			if p.object == ObjectLead {
				rec["Company"] = p.company
			} else {
				rec["Account"] = map[string]any{"Name": p.company}
			}
		}
		if err := s.Repo.UpsertLink(ctx, linkFrom(c, ct.ID, p.object, rec, "import")); err == nil {
			res.Linked++
		}
	}
	_ = s.Repo.AddSourceMembers(ctx, src.ID, members)
	return res, nil
}

// importCSV lays the people out as the contact importer's input: the identity
// columns, title, then every custom field a pull rule fills.
func (s *Service) importCSV(c *conn, people []person) ([]byte, []models.ContactImportColumnMapping) {
	header := []string{"email", "first_name", "last_name", "company", "phone", "title"}
	mapping := []models.ContactImportColumnMapping{
		{Index: 0, Target: models.ContactImportTargetEmail},
		{Index: 1, Target: models.ContactImportTargetFirstName},
		{Index: 2, Target: models.ContactImportTargetLastName},
		{Index: 3, Target: models.ContactImportTargetCompany},
		{Index: 4, Target: models.ContactImportTargetPhone},
		{Index: 5, Target: models.ContactImportTargetCustom, CustomKey: "title"},
	}
	type extra struct{ key, field, object string }
	var extras []extra
	seen := map[string]bool{"title": true}
	for _, r := range c.settings.FieldMap {
		key, ok := strings.CutPrefix(r.Warmbly, "custom:")
		if !ok || seen[key] || (r.Direction != DirectionPull && r.Direction != DirectionBoth) {
			continue
		}
		seen[key] = true
		extras = append(extras, extra{key: key, field: r.Salesforce, object: r.Object})
		mapping = append(mapping, models.ContactImportColumnMapping{Index: len(header), Target: models.ContactImportTargetCustom, CustomKey: key})
		header = append(header, key)
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write(header)
	for _, p := range people {
		row := []string{strings.TrimSpace(p.email), p.first, p.last, p.company, p.phone, p.title}
		for _, e := range extras {
			v := ""
			if p.rec != nil && e.object == p.object {
				v = p.rec.String(e.field)
			}
			row = append(row, v)
		}
		_ = w.Write(row)
	}
	w.Flush()
	return buf.Bytes(), mapping
}

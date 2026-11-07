package leadfinder

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// ExportColumn is one column a lead export can carry.
type ExportColumn struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	value func(l *models.Lead) string
}

func fmtFloat(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

func fmtInt(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

func primaryEmail(l *models.Lead) *models.LeadEmail {
	for i := range l.Emails {
		if l.Emails[i].Email == l.Email {
			return &l.Emails[i]
		}
	}
	return nil
}

func social(kind string) func(l *models.Lead) string {
	return func(l *models.Lead) string { return l.Socials[kind] }
}

// ExportColumns are every column, in file order.
var ExportColumns = []ExportColumn{
	{"name", "Name", func(l *models.Lead) string { return l.Name }},
	{"category", "Category", func(l *models.Lead) string { return l.Category }},
	{"email", "Email", func(l *models.Lead) string { return l.Email }},
	{"email_status", "Email status", func(l *models.Lead) string {
		if e := primaryEmail(l); e != nil {
			return e.Status
		}
		return ""
	}},
	{"other_emails", "Other emails", func(l *models.Lead) string {
		var out []string
		for _, e := range l.Emails {
			if e.Email != l.Email && e.Status != "suppressed" && e.Status != "invalid" {
				out = append(out, e.Email)
			}
		}
		return strings.Join(out, "; ")
	}},
	{"phone", "Phone", func(l *models.Lead) string { return l.Phone }},
	{"website", "Website", func(l *models.Lead) string { return l.Website }},
	{"domain", "Domain", func(l *models.Lead) string { return l.Domain }},
	{"address", "Address", func(l *models.Lead) string { return l.Address }},
	{"street", "Street", func(l *models.Lead) string { return l.Street }},
	{"city", "City", func(l *models.Lead) string { return l.City }},
	{"region", "Region", func(l *models.Lead) string { return l.Region }},
	{"postcode", "Postcode", func(l *models.Lead) string { return l.Postcode }},
	{"country", "Country", func(l *models.Lead) string { return l.Country }},
	{"latitude", "Latitude", func(l *models.Lead) string { return strconv.FormatFloat(l.Latitude, 'f', 6, 64) }},
	{"longitude", "Longitude", func(l *models.Lead) string { return strconv.FormatFloat(l.Longitude, 'f', 6, 64) }},
	{"rating", "Rating", func(l *models.Lead) string { return fmtFloat(l.Rating) }},
	{"review_count", "Reviews", func(l *models.Lead) string { return fmtInt(l.ReviewCount) }},
	{"price_level", "Price level", func(l *models.Lead) string { return fmtInt(l.PriceLevel) }},
	{"business_status", "Business status", func(l *models.Lead) string { return l.BusinessStatus }},
	{"hours", "Hours", func(l *models.Lead) string { return strings.Join(l.Hours, "; ") }},
	{"facebook", "Facebook", social("facebook")},
	{"instagram", "Instagram", social("instagram")},
	{"linkedin", "LinkedIn", social("linkedin")},
	{"twitter", "X (Twitter)", social("twitter")},
	{"tiktok", "TikTok", social("tiktok")},
	{"youtube", "YouTube", social("youtube")},
	{"site_platform", "Website platform", func(l *models.Lead) string { return l.Site.Platform }},
	{"site_signals", "Website signals", func(l *models.Lead) string { return strings.Join(l.Site.Signals, "; ") }},
	{"chain", "Chain", func(l *models.Lead) string { return l.Chain }},
	{"score", "Score", func(l *models.Lead) string { return strconv.Itoa(l.Score) }},
	{"status", "Status", func(l *models.Lead) string { return string(l.Status) }},
	{"tags", "Tags", func(l *models.Lead) string { return strings.Join(l.Tags, "; ") }},
	{"notes", "Notes", func(l *models.Lead) string { return l.Notes }},
	{"maps_url", "Map link", func(l *models.Lead) string { return l.MapsURL }},
	{"people", "People", func(l *models.Lead) string {
		out := make([]string, 0, len(l.People))
		for _, p := range l.People {
			v := p.Name
			if p.Title != "" {
				v += " (" + p.Title + ")"
			}
			if p.Email != "" && p.EmailStatus != "suppressed" {
				v += " <" + p.Email + ">"
			}
			out = append(out, v)
		}
		return strings.Join(out, "; ")
	}},
	{"provider", "Source", func(l *models.Lead) string { return providerMeta[l.Provider].Name }},
	{"external_id", "Source id", func(l *models.Lead) string { return l.ExternalID }},
	{"first_seen_at", "First seen", func(l *models.Lead) string { return l.FirstSeenAt.UTC().Format(time.RFC3339) }},
}

// PersonColumn is one column a one-row-per-person export leads with.
type PersonColumn struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	value func(p *models.LeadPerson) string
}

// PersonColumns lead every row of a one-row-per-person export, in file order.
var PersonColumns = []PersonColumn{
	{"person_name", "Person", func(p *models.LeadPerson) string { return p.Name }},
	{"person_first_name", "First name", func(p *models.LeadPerson) string { return p.FirstName }},
	{"person_last_name", "Last name", func(p *models.LeadPerson) string { return p.LastName }},
	{"person_title", "Title", func(p *models.LeadPerson) string { return p.Title }},
	{"person_seniority", "Seniority", func(p *models.LeadPerson) string { return string(p.Seniority) }},
	{"person_email", "Person email", func(p *models.LeadPerson) string {
		if p.EmailStatus == "suppressed" {
			return ""
		}
		return p.Email
	}},
	{"person_email_status", "Person email status", func(p *models.LeadPerson) string { return p.EmailStatus }},
	{"person_email_confidence", "Person email confidence", func(p *models.LeadPerson) string {
		if p.Email == "" {
			return ""
		}
		return strconv.Itoa(p.EmailConfidence)
	}},
	{"person_linkedin", "Person LinkedIn", func(p *models.LeadPerson) string { return p.LinkedInURL }},
	{"person_phone", "Person phone", func(p *models.LeadPerson) string { return p.Phone }},
	{"person_source", "Found on", func(p *models.LeadPerson) string { return p.SourceURL }},
}

// DefaultExportColumns is what an export carries when none are picked.
var DefaultExportColumns = []string{
	"name", "category", "email", "email_status", "other_emails", "phone", "website", "address", "city", "region",
	"postcode", "country", "rating", "review_count", "facebook", "instagram", "linkedin", "site_platform",
	"site_signals", "score", "status", "tags", "maps_url",
}

// ExportRequest asks for a file of leads.
type ExportRequest struct {
	models.LeadSelection
	Format  string   `json:"format"`
	Columns []string `json:"columns"`
	// Rows is "leads" (one row per business, the default) or "people" (one row
	// per person, with the business's columns repeated on each).
	Rows string `json:"rows"`
}

// Export writes the selected leads to w and returns the content type and filename.
func (s *Service) Export(ctx context.Context, orgID uuid.UUID, req *ExportRequest, w io.Writer) (string, string, error) {
	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "xlsx" && format != "json" {
		return "", "", errx.New(errx.BadRequest, "format must be csv, xlsx or json")
	}
	keys := req.Columns
	if len(keys) == 0 {
		keys = DefaultExportColumns
	}
	cols := make([]ExportColumn, 0, len(keys))
	for _, k := range keys {
		c := columnByKey(k)
		if c == nil {
			return "", "", errx.New(errx.BadRequest, "unknown export column: "+k)
		}
		cols = append(cols, *c)
	}
	rows := strings.ToLower(strings.TrimSpace(req.Rows))
	if rows == "" {
		rows = "leads"
	}
	if rows != "leads" && rows != "people" {
		return "", "", errx.New(errx.BadRequest, "rows must be leads or people")
	}
	ids, err := s.Resolve(ctx, orgID, &req.LeadSelection)
	if err != nil {
		return "", "", err
	}
	needPeople := rows == "people"
	for _, k := range keys {
		needPeople = needPeople || k == "people"
	}
	leads := make([]models.Lead, 0, len(ids))
	for start := 0; start < len(ids); start += 500 {
		batch, err := s.Repo.LeadsByIDs(ctx, orgID, ids[start:min(start+500, len(ids))])
		if err != nil {
			return "", "", err
		}
		if needPeople {
			people, err := s.Repo.PeopleForLeads(ctx, orgID, ids[start:min(start+500, len(ids))])
			if err != nil {
				return "", "", err
			}
			for i := range batch {
				batch[i].People = people[batch[i].ID]
			}
		}
		leads = append(leads, batch...)
	}

	var t exportTable
	if rows == "people" {
		for _, c := range PersonColumns {
			t.keys, t.header = append(t.keys, c.Key), append(t.header, c.Label)
		}
	}
	for _, c := range cols {
		t.keys, t.header = append(t.keys, c.Key), append(t.header, c.Label)
	}
	for i := range leads {
		l := &leads[i]
		lead := make([]string, len(cols))
		for j, c := range cols {
			lead[j] = c.value(l)
		}
		if rows == "leads" {
			t.rows = append(t.rows, lead)
			continue
		}
		for k := range l.People {
			row := make([]string, 0, len(PersonColumns)+len(cols))
			for _, c := range PersonColumns {
				row = append(row, c.value(&l.People[k]))
			}
			t.rows = append(t.rows, append(row, lead...))
		}
	}

	base := "leads-"
	if rows == "people" {
		base = "lead-people-"
	}
	name := base + time.Now().UTC().Format("2006-01-02") + "." + format
	switch format {
	case "json":
		out := make([]map[string]string, len(t.rows))
		for i, r := range t.rows {
			row := make(map[string]string, len(t.keys))
			for j, k := range t.keys {
				row[k] = r[j]
			}
			out[i] = row
		}
		return "application/json", name, json.NewEncoder(w).Encode(out)
	case "xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", name, writeXLSX(w, &t)
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(t.header); err != nil {
		return "", "", err
	}
	for _, r := range t.rows {
		row := make([]string, len(r))
		for j, v := range r {
			row[j] = cellSafe(v)
		}
		if err := cw.Write(row); err != nil {
			return "", "", err
		}
	}
	cw.Flush()
	return "text/csv; charset=utf-8", name, cw.Error()
}

// exportTable is an export laid out as cells, whatever the file format.
type exportTable struct {
	keys   []string
	header []string
	rows   [][]string
}

func columnByKey(k string) *ExportColumn {
	for i := range ExportColumns {
		if ExportColumns[i].Key == k {
			return &ExportColumns[i]
		}
	}
	return nil
}

// phoneShape is a value that is only a phone number, which no spreadsheet runs.
var phoneShape = regexp.MustCompile(`^\+[0-9 ().\-/]+$`)

// cellSafe keeps a provider's text from running as a spreadsheet formula.
func cellSafe(v string) string {
	if v == "" || !strings.ContainsRune("=+-@\t\r\n", rune(v[0])) || phoneShape.MatchString(v) {
		return v
	}
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		return v
	}
	return "'" + v
}

func writeXLSX(w io.Writer, t *exportTable) error {
	f := excelize.NewFile()
	defer f.Close()
	sheet := "Leads"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return err
	}
	sw, err := f.NewStreamWriter(sheet)
	if err != nil {
		return err
	}
	bold, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#F1F5F9"}},
	})
	_ = sw.SetColWidth(1, len(t.keys), 22)
	if err := sw.SetPanes(&excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	header := make([]any, len(t.header))
	for i, h := range t.header {
		header[i] = excelize.Cell{StyleID: bold, Value: h}
	}
	if err := sw.SetRow("A1", header); err != nil {
		return err
	}
	numeric := map[string]bool{"latitude": true, "longitude": true, "rating": true, "review_count": true, "price_level": true, "score": true, "person_email_confidence": true}
	for r, cells := range t.rows {
		row := make([]any, len(cells))
		for i, v := range cells {
			if numeric[t.keys[i]] && v != "" {
				if n, err := strconv.ParseFloat(v, 64); err == nil {
					row[i] = n
					continue
				}
			}
			// Excel stores these as text, never formulas, so they are written as they are.
			row[i] = v
		}
		cell, _ := excelize.CoordinatesToCellName(1, r+2)
		if err := sw.SetRow(cell, row); err != nil {
			return err
		}
	}
	if err := sw.Flush(); err != nil {
		return err
	}
	return f.Write(w)
}

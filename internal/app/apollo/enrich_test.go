package apollo

import (
	"strings"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// cell is the value enrichCells wrote for one target, or "" when it left it alone.
func cell(cells []string, target models.ContactImportColumnTarget, custom string) string {
	for i, col := range importColumns {
		if col.target == target && col.custom == custom {
			return cells[i]
		}
	}
	return ""
}

func TestEnrichCellsFillsBlanksAndKeepsWhatIsThere(t *testing.T) {
	ref := repository.ApolloContactRef{
		Email: "ana@acme.test", FirstName: "Ana", Company: "",
		CustomFields: map[string]string{"title": "Founder"},
	}
	p := Person{
		Email: "someone-else@acme.test", FirstName: "Anna", LastName: "López", OrganizationName: " Acme ",
		Title: "CEO", City: "Madrid", PhoneNumbers: []PhoneNumber{{RawNumber: "+34 600 000 000"}},
	}
	cells, changed := enrichCells(ref, p, false)
	if !changed {
		t.Fatal("blank fields were not filled")
	}
	if got := cell(cells, models.ContactImportTargetEmail, ""); got != "ana@acme.test" {
		t.Errorf("email = %q, want the contact's own address", got)
	}
	for name, got := range map[string]string{
		"first name": cell(cells, models.ContactImportTargetFirstName, ""),
		"title":      cell(cells, models.ContactImportTargetCustom, "title"),
	} {
		if got != "" {
			t.Errorf("%s overwritten with %q without overwrite", name, got)
		}
	}
	for name, tc := range map[string]struct{ got, want string }{
		"last name": {cell(cells, models.ContactImportTargetLastName, ""), "López"},
		"company":   {cell(cells, models.ContactImportTargetCompany, ""), "Acme"},
		"phone":     {cell(cells, models.ContactImportTargetPhone, ""), "+34 600 000 000"},
		"city":      {cell(cells, models.ContactImportTargetCustom, "city"), "Madrid"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", name, tc.got, tc.want)
		}
	}
}

func TestEnrichCellsOverwriteAndNoChange(t *testing.T) {
	ref := repository.ApolloContactRef{Email: "ana@acme.test", FirstName: "Ana", CustomFields: map[string]string{"title": "Founder"}}
	cells, changed := enrichCells(ref, Person{FirstName: "Anna", Title: "CEO"}, true)
	if !changed || cell(cells, models.ContactImportTargetFirstName, "") != "Anna" || cell(cells, models.ContactImportTargetCustom, "title") != "CEO" {
		t.Errorf("overwrite did not replace: changed=%v cells=%q", changed, cells)
	}
	// The same values, or nothing new, change nothing.
	if _, changed := enrichCells(ref, Person{FirstName: "Ana", Title: "Founder"}, true); changed {
		t.Error("identical values reported a change")
	}
	if _, changed := enrichCells(ref, Person{}, true); changed {
		t.Error("an empty match reported a change")
	}
	long := strings.Repeat("x", 600)
	cells, _ = enrichCells(repository.ApolloContactRef{Email: "a@acme.test"}, Person{Title: long}, false)
	if got := cell(cells, models.ContactImportTargetCustom, "title"); len(got) != 500 {
		t.Errorf("title length = %d, want 500", len(got))
	}
}

func TestPersonAccessorsReadWhereApolloPutsTheValue(t *testing.T) {
	org := &Org{Name: "Org Co", WebsiteURL: " https://org.test ", PrimaryDomain: "", Domain: "org.test", Industry: " Software "}
	p := Person{Organization: org, Account: &Org{Name: "Account Co"}}
	if p.Company() != "Org Co" || p.Website() != "https://org.test" || p.CompanyDomain() != "org.test" || p.Industry() != "Software" {
		t.Errorf("organization fields = %q %q %q %q", p.Company(), p.Website(), p.CompanyDomain(), p.Industry())
	}
	if got := (Person{OrganizationName: " Named ", Organization: org}).Company(); got != "Named" {
		t.Errorf("organization_name did not win: %q", got)
	}
	if got := (Person{Account: &Org{Name: "Account Co", PrimaryDomain: "acct.test"}}).CompanyDomain(); got != "acct.test" {
		t.Errorf("account domain = %q", got)
	}
	contactPhone := Person{Contact: &struct {
		PhoneNumbers []PhoneNumber `json:"phone_numbers"`
	}{PhoneNumbers: []PhoneNumber{{RawNumber: " "}, {SanitizedNumber: "+15550100", RawNumber: "(555) 0100"}}}}
	if got := contactPhone.Phone(); got != "+15550100" {
		t.Errorf("phone = %q, want the first usable sanitized number", got)
	}
	if got := (Person{FirstName: "Ana", LastName: "López"}).DisplayName(); got != "Ana López" {
		t.Errorf("display name = %q", got)
	}
}

func TestBackoffHonoursABoundedRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		attempt    int
		retryAfter string
		want       time.Duration
	}{
		{0, "", time.Second},
		{3, "", 8 * time.Second},
		{0, "5", 5 * time.Second},
		{0, "120", time.Second},
		{2, "soon", 4 * time.Second},
	} {
		if got := backoff(tc.attempt, tc.retryAfter); got != tc.want {
			t.Errorf("backoff(%d, %q) = %s, want %s", tc.attempt, tc.retryAfter, got, tc.want)
		}
	}
}

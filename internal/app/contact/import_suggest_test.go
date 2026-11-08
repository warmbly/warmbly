package contact

import (
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func TestSuggestMappingMatchesExistingCustomFields(t *testing.T) {
	headers := []string{"Email", "industry", "Company URL", "Total Score", "Company", "INDUSTRY", "Notes"}
	existing := []string{"Industry", "company_url", "industry", "Company"}

	got := SuggestMapping(headers, nil, existing)

	want := []models.ContactImportColumnMapping{
		{Index: 0, Target: models.ContactImportTargetEmail},
		// An exact name wins over a more used spelling of it.
		{Index: 1, Target: models.ContactImportTargetCustom, CustomKey: "industry"},
		// Otherwise the stored spelling, the most used of several.
		{Index: 2, Target: models.ContactImportTargetCustom, CustomKey: "company_url"},
		// No field by that name: left for the user, never invented.
		{Index: 3, Target: models.ContactImportTargetIgnore},
		// A standard field outranks a custom field of the same name.
		{Index: 4, Target: models.ContactImportTargetCompany},
		{Index: 5, Target: models.ContactImportTargetCustom, CustomKey: "Industry"},
		{Index: 6, Target: models.ContactImportTargetIgnore},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d mappings, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("column %d (%q): got %+v, want %+v", i, headers[i], got[i], want[i])
		}
	}
}

func TestSuggestMappingGivesEachFieldOneColumn(t *testing.T) {
	got := SuggestMapping([]string{"Email", "Industry", "INDUSTRY"}, nil, []string{"Industry"})
	if got[1].CustomKey != "Industry" || got[2].Target != models.ContactImportTargetIgnore {
		t.Fatalf("got %+v, want the second spelling left ignored", got)
	}
}

func TestSuggestMappingKeepsVerificationAheadOfCustomFields(t *testing.T) {
	headers := []string{"email", "ZeroBounce Status"}
	got := SuggestMapping(headers, [][]string{{"a@x.com", "valid"}}, []string{"ZeroBounce Status"})
	if got[1].Target != models.ContactImportTargetVerificationStatus {
		t.Fatalf("verdict column mapped to %q, want verification_status", got[1].Target)
	}
}

func TestSuggestMappingWithoutExistingFields(t *testing.T) {
	got := SuggestMapping([]string{"email", "Industry"}, nil, nil)
	if got[1].Target != models.ContactImportTargetIgnore {
		t.Fatalf("unknown header mapped to %q, want ignore", got[1].Target)
	}
}

func TestFoldCustomFieldKey(t *testing.T) {
	for in, want := range map[string]string{
		"Company URL":    "companyurl",
		"company_url":    "companyurl",
		"company-url":    "companyurl",
		" Company.URL ":  "companyurl",
		"Revenue ($)":    "revenue",
		"###":            "",
		"Straße Nummer2": "straßenummer2",
		"Area m²":        "aream²",
	} {
		if got := FoldCustomFieldKey(in); got != want {
			t.Errorf("FoldCustomFieldKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSuggestMappingFindsEmailByItsValues(t *testing.T) {
	sample := [][]string{{"Dana", "dana@acme.com"}, {"Lee", "Lee <lee@beta.io>"}}
	got := SuggestMapping([]string{"Name", "Work contact"}, sample, nil)
	if got[1].Target != models.ContactImportTargetEmail {
		t.Fatalf("a column of addresses mapped to %q, want email", got[1].Target)
	}

	// An existing custom field of the same name does not take it.
	got = SuggestMapping([]string{"Name", "Work contact"}, sample, []string{"Work contact"})
	if got[1].Target != models.ContactImportTargetEmail {
		t.Fatalf("a custom field took the only column of addresses: %+v", got[1])
	}

	// A named email column is never second-guessed by the values.
	got = SuggestMapping([]string{"Email", "Backup"}, [][]string{{"a@x.com", "b@y.com"}}, nil)
	if got[0].Target != models.ContactImportTargetEmail || got[1].Target != models.ContactImportTargetIgnore {
		t.Fatalf("got %+v, want only the named column as email", got)
	}
}

func TestSuggestMappingKeepsLocationColumns(t *testing.T) {
	headers := []string{"Email", "Country/Region", "State/Province", "City", "Time Zone", "Company Country", "Region", "Industry"}
	got := SuggestMapping(headers, nil, []string{"Country"})
	want := []string{"", "Country", "state", "city", "timezone", "company_country", "region", ""}
	for i, key := range want {
		if key == "" {
			continue
		}
		if got[i].Target != models.ContactImportTargetCustom || got[i].CustomKey != key {
			t.Errorf("%q mapped to %q/%q, want custom %q", headers[i], got[i].Target, got[i].CustomKey, key)
		}
	}
	if got[7].Target != models.ContactImportTargetIgnore {
		t.Errorf("Industry mapped to %q, want ignore", got[7].Target)
	}
}

func TestSuggestMappingGivesALocationKeyOneColumn(t *testing.T) {
	got := SuggestMapping([]string{"email", "Country", "Country Code"}, nil, nil)
	if got[1].CustomKey != "country" || got[2].Target != models.ContactImportTargetIgnore {
		t.Fatalf("got %+v, want only the first country column kept", got)
	}
}

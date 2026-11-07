package clay

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestParseBodyAcceptsEveryShapeClaySends(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		rows int
	}{
		"one object":                    {`{"email":"a@x.test"}`, 1},
		"an array":                      {`[{"email":"a@x.test"},{"email":"b@x.test"}]`, 2},
		"under rows":                    {`{"rows":[{"email":"a@x.test"},{"email":"b@x.test"}]}`, 2},
		"under contacts":                {`{"contacts":[{"email":"a@x.test"}]}`, 1},
		"under records":                 {`{"records":[{"email":"a@x.test"},{"email":"b@x.test"},{"email":"c@x.test"}]}`, 3},
		"a rows field that is no array": {`{"email":"a@x.test","rows":"3"}`, 1},
		"surrounding whitespace":        {"  \n[{\"email\":\"a@x.test\"}]\n", 1},
	} {
		t.Run(name, func(t *testing.T) {
			rows, err := parseBody([]byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != tc.rows {
				t.Errorf("rows = %d, want %d", len(rows), tc.rows)
			}
		})
	}
}

func TestParseBodyRefusesWhatIsNotRows(t *testing.T) {
	tooMany := "[" + strings.TrimSuffix(strings.Repeat(`{"email":"a@x.test"},`, MaxRows+1), ",") + "]"
	for name, body := range map[string]string{
		"empty":                     "",
		"not JSON":                  "email=a@x.test",
		"a bare string":             `"a@x.test"`,
		"broken object":             `{"email":`,
		"an empty array":            `[]`,
		"rows holding a non-object": `{"rows":["a@x.test"]}`,
		"more than MaxRows":         tooMany,
	} {
		t.Run(name, func(t *testing.T) {
			if rows, err := parseBody([]byte(body)); err == nil {
				t.Errorf("parseBody accepted %d rows", len(rows))
			}
		})
	}
}

func TestNormalizeRowReadsClayColumnSpellings(t *testing.T) {
	r := normalizeRow(map[string]any{
		"Work Email":      " ana@acme.test ",
		"Full Name":       "Ana María López",
		"Company Name":    "Acme",
		"Mobile Phone":    "+44 20 7946 0000",
		"Job Title":       "Head of Sales",
		"LinkedIn URL":    "https://linkedin.com/in/ana",
		"Funding Stage":   "Series B",
		"Employee Count":  float64(120),
		"_clay_row_id":    "r_1",
		"Notes":           "",
		"Tags":            "vip, warm; q4",
		"custom_fields":   map[string]any{"Lead Score": float64(87)},
		"Email Status":    "valid",
		"Unrelated Empty": nil,
	})
	if r.email != "ana@acme.test" || r.company != "Acme" || r.phone != "+44 20 7946 0000" || r.verification != "valid" {
		t.Errorf("fields = %+v", r)
	}
	// A full name fills first and last only when neither came on its own.
	if r.first != "Ana" || r.last != "María López" {
		t.Errorf("name = %q %q", r.first, r.last)
	}
	if !reflect.DeepEqual(r.categories, []string{"vip", "warm", "q4"}) {
		t.Errorf("categories = %q", r.categories)
	}
	want := map[string]string{
		"title":          "Head of Sales",
		"linkedin_url":   "https://linkedin.com/in/ana",
		"employee_count": "120",
		"Funding Stage":  "Series B",
		"Lead Score":     "87",
	}
	if !reflect.DeepEqual(r.custom, want) {
		t.Errorf("custom = %v, want %v", r.custom, want)
	}
}

func TestNormalizeRowKeepsExplicitNamesAndReadsTheCampaign(t *testing.T) {
	r := normalizeRow(map[string]any{
		"email": "b@acme.test", "first_name": "Bo", "name": "Robert Smith",
		"campaign_id": "3f6b3c1e-6d0b-4d9e-9a8e-2f3c4b5a6d7e",
	})
	if r.first != "Bo" || r.last != "" {
		t.Errorf("an explicit first name lost to the full name: %q %q", r.first, r.last)
	}
	if r.campaign == nil || r.campaign.String() != "3f6b3c1e-6d0b-4d9e-9a8e-2f3c4b5a6d7e" || r.campaignErr {
		t.Errorf("campaign = %v, err %v", r.campaign, r.campaignErr)
	}
	if bad := normalizeRow(map[string]any{"email": "c@acme.test", "campaign_id": "not-a-uuid"}); bad.campaign != nil || !bad.campaignErr {
		t.Errorf("a malformed campaign id was not flagged: %+v", bad)
	}
}

func TestNormalizeRowCapsTags(t *testing.T) {
	tags := make([]any, 30)
	for i := range tags {
		tags[i] = fmt.Sprintf("t%d", i)
	}
	if r := normalizeRow(map[string]any{"email": "d@acme.test", "tags": tags}); len(r.categories) != 20 || r.categories[0] != "t0" {
		t.Errorf("categories = %d, first %q", len(r.categories), r.categories[0])
	}
}

func TestSplitNameAndScalar(t *testing.T) {
	for in, want := range map[string][2]string{"": {"", ""}, "Ana": {"Ana", ""}, " Ana  María  López ": {"Ana", "María López"}} {
		if f, l := splitName(in); f != want[0] || l != want[1] {
			t.Errorf("splitName(%q) = %q %q", in, f, l)
		}
	}
	for v, want := range map[any]string{" x ": "x", float64(1.5): "1.5", true: "true"} {
		if got := scalar(v); got != want {
			t.Errorf("scalar(%v) = %q, want %q", v, got, want)
		}
	}
	if got := scalar([]any{"a", "", float64(2)}); got != "a, 2" {
		t.Errorf("scalar(list) = %q", got)
	}
	if got := scalar(map[string]any{"a": "b"}); got != "" {
		t.Errorf("an object read as a cell: %q", got)
	}
}

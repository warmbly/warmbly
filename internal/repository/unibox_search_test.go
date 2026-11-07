package repository

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// prefixTSQuery builds a tsquery from whatever lands in a search box, so the
// cases that matter are the hostile ones: to_tsquery is a parser and raises on
// syntax it cannot read, which would turn a stray bracket into a 500.
func TestPrefixTSQuery(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"single word", "dyno", "dyno:*"},
		{"two words are ANDed", "quick collab", "quick:* & collab:*"},
		{"lowercased", "DynoWeb", "dynoweb:*"},
		{"an address splits into its parts", "suman@mail.dynoweb.app", "suman:* & mail:* & dynoweb:* & app:*"},

		// Every one of these makes to_tsquery raise when passed through raw.
		{"operators are stripped", "a & b | c", "a:* & b:* & c:*"},
		{"unbalanced bracket", "re: (urgent", "re:* & urgent:*"},
		{"punctuation and colons", "!foo:bar", "foo:* & bar:*"},
		{"negation keeps web-search semantics", "foo -bar", ""},
		{"quotes keep web-search semantics", `"exact phrase"`, ""},
		{"OR keeps web-search semantics", "foo OR bar", ""},
		{"a lone operator has nothing to search", "&&&", ""},

		{"empty", "", ""},
		{"whitespace only", "   \t ", ""},
		{"digits are kept", "invoice 2026", "invoice:* & 2026:*"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := prefixTSQuery(tc.input); got != tc.want {
				t.Fatalf("prefixTSQuery(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// Long input stays on websearch_to_tsquery instead of being broadened by a truncated prefix query.
func TestPrefixTSQueryCapsTerms(t *testing.T) {
	got := prefixTSQuery("one two three four five six seven eight nine ten eleven")
	want := ""
	if got != want {
		t.Fatalf("prefixTSQuery long input = %q, want %q", got, want)
	}
}

func TestEscapeLikePattern(t *testing.T) {
	if got, want := escapeLikePattern(`100%_done\later`), `100\%\_done\\later`; got != want {
		t.Fatalf("escapeLikePattern = %q, want %q", got, want)
	}
}

func TestParticipantSearchable(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  bool
	}{
		{"", false}, {"ab", false}, {"a.b", false}, {"ab cd", false},
		{"%_\\", false}, {"éé", false}, {"東京", false},
		{"abc", true}, {"Élodie", true}, {"東京駅", true},
		{"a@domain.test", true}, {"%abc_", true}, {"123", true},
	} {
		if got := participantSearchable(tc.input); got != tc.want {
			t.Errorf("participantSearchable(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestUniboxSearchQueryBindsOnlyUsedParameters(t *testing.T) {
	for _, input := range []string{"ab", "a.b", "%", "a_", "foobar", "a@domain.test", "alpha\nbeta"} {
		t.Run(input, func(t *testing.T) {
			query, args := uniboxSearchQuery(uuid.New(), &models.MailSearchParams{Subject: &input, PageSize: 50})
			for i := range args {
				if !strings.Contains(query, fmt.Sprintf("$%d", i+1)) {
					t.Errorf("unused argument %d in %s", i+1, query)
				}
			}
			if strings.Contains(query, "public.unibox_participants(") != participantSearchable(input) {
				t.Fatal("participant predicate does not match trigram eligibility")
			}
		})
	}
}

func TestUniboxSearchQueryConversationScope(t *testing.T) {
	campaign, contact, q := uuid.New(), uuid.New(), "pricing"
	for _, tc := range []struct {
		name              string
		params            models.MailSearchParams
		scoped, byAddress bool
		preferCampaign    bool
	}{
		{"unscoped", models.MailSearchParams{}, false, false, false},
		{"campaign", models.MailSearchParams{CampaignID: &campaign}, true, false, true},
		{"contact", models.MailSearchParams{ContactID: &contact}, true, true, false},
		{"campaign and contact", models.MailSearchParams{CampaignID: &campaign, ContactID: &contact, Subject: &q}, true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.params.PageSize = 50
			query, args := uniboxSearchQuery(uuid.New(), &tc.params)
			for i := range args {
				if !strings.Contains(query, fmt.Sprintf("$%d", i+1)) {
					t.Errorf("unused argument %d", i+1)
				}
			}
			if got := strings.Contains(query, "scope_hits"); got != tc.scoped {
				t.Errorf("scope CTEs present = %v, want %v", got, tc.scoped)
			}
			if got := strings.Contains(query, "scope_contact"); got != tc.byAddress {
				t.Errorf("contact address match present = %v, want %v", got, tc.byAddress)
			}
			if got := regexp.MustCompile(`ct.campaign_id = \$[0-9]+ DESC`).MatchString(query); got != tc.preferCampaign {
				t.Errorf("filtered campaign preferred = %v, want %v", got, tc.preferCampaign)
			}
			if !strings.Contains(query, "camp.campaign_id") {
				t.Error("row campaign_id is not selected")
			}
		})
	}
}

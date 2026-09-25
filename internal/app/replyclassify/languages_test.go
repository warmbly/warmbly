package replyclassify

import (
	"os"
	"regexp"
	"slices"
	"sort"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

// Every language with vocabulary must be one a workspace can choose, and the
// dashboard's list of them must say the same thing.
func TestLanguageRulesAgreeWithTheSettingAndTheDashboard(t *testing.T) {
	codes := LanguagesWithRules()
	for _, c := range codes {
		if _, ok := models.MailLanguageNames[c]; !ok {
			t.Errorf("%q has rules but is not a mail language a workspace can choose", c)
		}
	}
	src, err := os.ReadFile("../../../web/src/lib/api/models/app/outreach/OutreachSettings.ts")
	if err != nil {
		t.Fatalf("read dashboard settings: %v", err)
	}
	block := regexp.MustCompile(`(?s)OFFLINE_RULE_LANGUAGES = new Set\(\[(.*?)\]\)`).FindSubmatch(src)
	if block == nil {
		t.Fatal("OFFLINE_RULE_LANGUAGES not found in the dashboard")
	}
	var web []string
	for _, m := range regexp.MustCompile(`"([a-z]+)"`).FindAllSubmatch(block[1], -1) {
		web = append(web, string(m[1]))
	}
	sort.Strings(web)
	if !slices.Equal(web, codes) {
		t.Fatalf("dashboard lists %v, the rules cover %v", web, codes)
	}
}

// No language is read until chosen: the base set is the same object whatever
// unknown or empty codes are passed.
func TestRulesForUnknownLanguagesIsTheBaseSet(t *testing.T) {
	if rulesFor(nil) != rulesFor([]string{"", "xx", "en"}) {
		t.Fatal("codes with no vocabulary changed the rules")
	}
	if rulesFor([]string{"pl", "de"}) != rulesFor([]string{"de", "pl", "pl"}) {
		t.Fatal("the same languages in another order compiled twice")
	}
}

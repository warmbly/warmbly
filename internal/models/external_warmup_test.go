package models

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/config"
)

func TestExternalWarmupMatches(t *testing.T) {
	marked := &EmailMessageStoreData{Subject: "Life with a rescue dog", Flags: []string{config.WarmupVerifyHeader + ":" + "3f1c"}}
	tagged := &EmailMessageStoreData{Subject: "Notes", BodyText: "See you soon.\n\nWRM-7Q2X"}
	plain := &EmailMessageStoreData{Subject: "Lunch on Friday?", BodyText: "Does noon work?"}

	cases := []struct {
		name string
		rule ExternalWarmupSettings
		msg  *EmailMessageStoreData
		want bool
	}{
		{"off hides nothing", ExternalWarmupSettings{}, marked, false},
		{"marker hidden when asked", ExternalWarmupSettings{HideWarmblyMarked: true}, marked, true},
		{"legacy marker", ExternalWarmupSettings{HideWarmblyMarked: true}, &EmailMessageStoreData{Flags: []string{"x-warmbly-token: abc"}}, true},
		{"empty marker value is not a marker", ExternalWarmupSettings{HideWarmblyMarked: true}, &EmailMessageStoreData{Flags: []string{config.WarmupVerifyHeader + ": "}}, false},
		{"marker ignored without the switch", ExternalWarmupSettings{Tags: []string{"zzzz"}}, marked, false},
		{"tag in body, any case", ExternalWarmupSettings{Tags: []string{"wrm-7q2x"}}, tagged, true},
		{"tag in subject", ExternalWarmupSettings{Tags: []string{"RESCUE DOG"}}, marked, true},
		{"tag in snippet", ExternalWarmupSettings{Tags: []string{"code 91"}}, &EmailMessageStoreData{Snippet: "Code 91 inside"}, true},
		{"ordinary mail kept", ExternalWarmupSettings{HideWarmblyMarked: true, Tags: []string{"wrm-7q2x"}}, plain, false},
		{"nil message", ExternalWarmupSettings{HideWarmblyMarked: true}, nil, false},
	}
	for _, c := range cases {
		if got := c.rule.Matches(c.msg); got != c.want {
			t.Errorf("%s: Matches = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestExternalWarmupSettingsAreCleanedAndBounded(t *testing.T) {
	s := DefaultAdvancedOutreachSettings()
	s.ExternalWarmup.Tags = []string{"  WRM-7Q2X ", "wrm-7q2x", "", "lemwarm-abc"}
	s.Normalize()
	if got := strings.Join(s.ExternalWarmup.Tags, ","); got != "WRM-7Q2X,lemwarm-abc" {
		t.Fatalf("normalized tags = %q", got)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// No tags reads back as [], never null, both unsaved and after a save cleared them.
	for _, empty := range []AdvancedOutreachSettings{DefaultAdvancedOutreachSettings(), {ExternalWarmup: ExternalWarmupSettings{Tags: []string{" "}}}} {
		empty.Normalize()
		raw, _ := json.Marshal(empty.ExternalWarmup)
		if !strings.Contains(string(raw), `"tags":[]`) {
			t.Errorf("empty tags encode as %s", raw)
		}
	}

	short := DefaultAdvancedOutreachSettings()
	short.ExternalWarmup.Tags = []string{"re"}
	if err := short.Validate(); err == nil || !strings.Contains(err.Error(), "too short") {
		t.Errorf("a two-letter tag would hide most mail, got %v", err)
	}

	many := DefaultAdvancedOutreachSettings()
	for i := range ExternalWarmupMaxTags + 1 {
		many.ExternalWarmup.Tags = append(many.ExternalWarmup.Tags, "tag-"+strings.Repeat("x", i+1))
	}
	if err := many.Validate(); err == nil {
		t.Error("more tags than the limit were accepted")
	}

	long := DefaultAdvancedOutreachSettings()
	long.ExternalWarmup.Tags = []string{strings.Repeat("a", ExternalWarmupTagMaxLength+1)}
	if err := long.Validate(); err == nil {
		t.Error("an over-long tag was accepted")
	}
}

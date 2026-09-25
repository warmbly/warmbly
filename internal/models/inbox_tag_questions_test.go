package models

import (
	"slices"
	"strings"
	"testing"
)

func TestInboxTagLabelName(t *testing.T) {
	cases := map[string]string{
		"Later maybe":            "Later maybe",
		"  Später  vielleicht! ": "Später vielleicht",
		"Spa\u0308ter":           "Später",
		"<b>x</b>":               "b x b",
		"a--b":                   "a-b",
		"a - b":                  "a b",
		"follow-up\tnow":         "follow-up now",
		"!!!":                    "",
	}
	for in, want := range cases {
		if got := InboxTagLabelName(in); got != want {
			t.Errorf("name(%q) = %q, want %q", in, got, want)
		}
	}
}

func settingsWith(qs ...InboxTagQuestion) *AdvancedOutreachSettings {
	s := DefaultAdvancedOutreachSettings()
	s.InboxTagging.Questions = qs
	return &s
}

func TestInboxTagQuestionsNormalize(t *testing.T) {
	s := settingsWith(
		InboxTagQuestion{Type: " YES_NO ", Question: " Will they  get back later? ", Label: " Later  maybe! ",
			Action: InboxTagQuestionAction{Type: "hold"}, Choices: []InboxTagChoice{{Label: "x"}}},
		InboxTagQuestion{Type: "choice", Question: "Who?", Label: "dropped", Action: InboxTagQuestionAction{Type: "stop"},
			Choices: []InboxTagChoice{{Label: "Recruiter", Description: "agency", Action: InboxTagQuestionAction{Type: "hold", HoldDays: 9999}}, {Label: "Owner", Description: "owner"}}},
	)
	s.InboxTagging.Languages = []string{" DE ", "pl", "de", ""}
	s.Normalize()
	if err := s.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	yn, ch := s.InboxTagging.Questions[0], s.InboxTagging.Questions[1]
	if yn.ID == "" || yn.ID == ch.ID {
		t.Fatalf("ids not minted: %q %q", yn.ID, ch.ID)
	}
	if yn.Type != InboxTagQuestionYesNo || yn.Question != "Will they get back later?" || yn.Label != "Later maybe" || yn.Choices != nil {
		t.Fatalf("yes/no = %+v", yn)
	}
	if yn.Action.HoldDays != InboxTagQuestionHoldDefault {
		t.Fatalf("hold days default = %d", yn.Action.HoldDays)
	}
	if ch.Label != "" || ch.Action.Type != "" {
		t.Fatalf("a choice question carries its labels and actions on its options: %+v", ch)
	}
	if ch.Choices[0].Label != "Recruiter" || ch.Choices[0].Action.HoldDays != InboxTagQuestionHoldMax {
		t.Fatalf("choice = %+v", ch.Choices[0])
	}
	if !slices.Equal(s.InboxTagging.Languages, []string{"de", "pl"}) {
		t.Fatalf("tagging languages = %q", s.InboxTagging.Languages)
	}
}

func TestInboxTagQuestionsValidate(t *testing.T) {
	yes := func(label string) InboxTagQuestion {
		return InboxTagQuestion{Type: InboxTagQuestionYesNo, Question: "q", Label: label}
	}
	bad := map[string]*AdvancedOutreachSettings{
		"no label":       settingsWith(yes("")),
		"duplicate":      settingsWith(yes("Later"), yes("later")),
		"long label":     settingsWith(yes(strings.Repeat("a", InboxTagLabelMaxLen+1))),
		"no question":    settingsWith(InboxTagQuestion{Type: InboxTagQuestionYesNo, Label: "a"}),
		"bad type":       settingsWith(InboxTagQuestion{Type: "score", Question: "q", Label: "a"}),
		"bad action":     settingsWith(InboxTagQuestion{Type: InboxTagQuestionYesNo, Question: "q", Label: "a", Action: InboxTagQuestionAction{Type: "suppress"}}),
		"one option":     settingsWith(InboxTagQuestion{Type: InboxTagQuestionChoice, Question: "q", Choices: []InboxTagChoice{{Label: "a", Description: "d"}}}),
		"no description": settingsWith(InboxTagQuestion{Type: InboxTagQuestionChoice, Question: "q", Choices: []InboxTagChoice{{Label: "a", Description: "d"}, {Label: "b"}}}),
	}
	many := settingsWith()
	for i := 0; i <= InboxTagQuestionsMax; i++ {
		many.InboxTagging.Questions = append(many.InboxTagging.Questions, yes(string(rune('a'+i))))
	}
	bad["too many"] = many
	lang := settingsWith()
	lang.InboxTagging.Languages = []string{"de", "xx"}
	bad["unknown language"] = lang

	for name, s := range bad {
		s.Normalize()
		if err := s.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	dup := settingsWith(yes("a"), yes("b"))
	dup.Normalize()
	dup.InboxTagging.Questions[1].ID = dup.InboxTagging.Questions[0].ID
	if err := dup.Validate(); err == nil {
		t.Error("a repeated id was accepted")
	}
}

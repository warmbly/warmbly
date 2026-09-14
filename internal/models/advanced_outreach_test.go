package models

import "testing"

// The content-score floor reaches the API as a plain integer. Left unclamped, a
// floor above 100 flags every campaign forever and a floor at or below 0 is a
// control that does nothing, since the readers fall back to the default.
func TestNormalizeClampsTheContentScoreFloor(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{-40, 1},
		{0, 1},
		{1, 1},
		{60, 60},
		{100, 100},
		{5000, 100},
	} {
		s := DefaultAdvancedOutreachSettings()
		s.Preflight.MinContentScore = tc.in
		s.Normalize()
		if s.Preflight.MinContentScore != tc.want {
			t.Errorf("floor %d normalized to %d, want %d", tc.in, s.Preflight.MinContentScore, tc.want)
		}
	}
}

func TestNormalizeLeavesTheDefaultsAlone(t *testing.T) {
	s := DefaultAdvancedOutreachSettings()
	before := s
	s.Normalize()
	if s.Preflight != before.Preflight {
		t.Errorf("defaults changed under Normalize: %+v -> %+v", before.Preflight, s.Preflight)
	}
}

// The bug behind issue #471: every classified reply opened a high-priority
// follow-up, including vacation notices and bounces, and one week of sending
// buried the real replies. A default workspace must open a task for a human
// reply and refuse one for a machine.
func TestDefaultSettingsOpenTasksForHumanRepliesOnly(t *testing.T) {
	s := DefaultAdvancedOutreachSettings().ReplyIntent
	for _, intent := range []ReplyIntentType{
		ReplyIntentPositive, ReplyIntentQuestion, ReplyIntentNeutral, ReplyIntentNegative,
	} {
		if !s.CreatesTaskFor(intent) {
			t.Errorf("%s should open a follow-up task by default", intent)
		}
	}
	for _, intent := range []ReplyIntentType{ReplyIntentOutOfOffice, ReplyIntentAutomated} {
		if s.CreatesTaskFor(intent) {
			t.Errorf("%s should not open a follow-up task by default", intent)
		}
	}
}

// An unset list means the default set, an empty one means none, and the switch
// still overrides both. All three have to stay distinguishable through a JSON
// round trip, because that is how the setting is stored.
func TestCreatesTaskForHonoursTheConfiguredSet(t *testing.T) {
	off := DefaultAdvancedOutreachSettings().ReplyIntent
	off.AutoCreateCRMTask = false
	if off.CreatesTaskFor(ReplyIntentPositive) {
		t.Error("the switch being off must beat any intent list")
	}

	none := DefaultAdvancedOutreachSettings().ReplyIntent
	none.CRMTaskIntents = []ReplyIntentType{}
	for _, intent := range []ReplyIntentType{ReplyIntentPositive, ReplyIntentOutOfOffice} {
		if none.CreatesTaskFor(intent) {
			t.Errorf("an empty list must open nothing, opened for %s", intent)
		}
	}

	ooo := DefaultAdvancedOutreachSettings().ReplyIntent
	ooo.CRMTaskIntents = []ReplyIntentType{ReplyIntentOutOfOffice}
	if !ooo.CreatesTaskFor(ReplyIntentOutOfOffice) {
		t.Error("a workspace that asks for out-of-office tasks must get them")
	}
	if ooo.CreatesTaskFor(ReplyIntentPositive) {
		t.Error("an explicit list must not fall back to the default set")
	}
}

func TestValidateRefusesAnUnknownReplyIntent(t *testing.T) {
	s := DefaultAdvancedOutreachSettings()
	s.ReplyIntent.CRMTaskIntents = []ReplyIntentType{ReplyIntentPositive, "interested?"}
	s.Normalize()
	if err := s.Validate(); err == nil {
		t.Fatal("an unknown intent must be refused rather than silently dropped")
	}

	s.ReplyIntent.CRMTaskIntents = []ReplyIntentType{" Positive ", ReplyIntentPositive, ReplyIntentNeutral}
	s.Normalize()
	if err := s.Validate(); err != nil {
		t.Fatalf("normalized values must validate: %v", err)
	}
	if got := s.ReplyIntent.CRMTaskIntents; len(got) != 2 || got[0] != ReplyIntentPositive || got[1] != ReplyIntentNeutral {
		t.Errorf("Normalize did not trim, lower-case and de-duplicate: %v", got)
	}
}

// Absent must survive a round trip as absent: it is what makes an existing
// workspace, stored before the setting existed, pick up the new default.
func TestNilIntentListStaysNilThroughNormalize(t *testing.T) {
	s := DefaultAdvancedOutreachSettings()
	if s.ReplyIntent.CRMTaskIntents != nil {
		t.Fatal("the default settings must leave the list unset")
	}
	s.Normalize()
	if s.ReplyIntent.CRMTaskIntents != nil {
		t.Error("Normalize turned an unset list into an empty one, which means none")
	}
}

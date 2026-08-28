package listquality

import "testing"

func repeat(email string, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, email)
	}
	return out
}

// The case that decides whether this is usable: an ordinary business list must
// pass silently.
func TestAssessOrdinaryListIsQuiet(t *testing.T) {
	emails := append(repeat("ada@acme.com", 50), repeat("someone@gmail.com", 50)...)
	s := Assess(emails)
	if s.Flagged {
		t.Errorf("an ordinary list was flagged: %+v", s)
	}
	if s.BadSharePct != 0 {
		t.Errorf("bad share = %.1f%%, want 0", s.BadSharePct)
	}
}

func TestAssessFlagsAScrapedList(t *testing.T) {
	emails := append(repeat("not-an-email", 30), repeat("x@mailinator.com", 30)...)
	emails = append(emails, repeat("ada@acme.com", 40)...)
	s := Assess(emails)
	if !s.Flagged {
		t.Errorf("a 60%% unusable list was not flagged: %+v", s)
	}
	if s.Malformed != 30 || s.Disposable != 30 {
		t.Errorf("malformed = %d, disposable = %d, want 30 each", s.Malformed, s.Disposable)
	}
	if s.Summary == "" {
		t.Error("a flagged list must say what is wrong with it")
	}
}

// Role addresses are a choice, not a defect. Plenty of legitimate B2B lists are
// mostly info@ and sales@, and flagging those would be wrong.
func TestAssessDoesNotCountRoleAddressesAsBad(t *testing.T) {
	s := Assess(repeat("info@acme.com", 100))
	if s.Flagged {
		t.Errorf("an all-role list was flagged as unusable: %+v", s)
	}
	if s.Role != 100 {
		t.Errorf("role = %d, want 100 counted but not penalised", s.Role)
	}
	if s.BadSharePct != 0 {
		t.Errorf("bad share = %.1f%%, want 0", s.BadSharePct)
	}
}

func TestAssessIgnoresASampleTooSmallToJudge(t *testing.T) {
	for _, n := range []int{1, 5, MinSample - 1} {
		if s := Assess(repeat("not-an-email", n)); s.Flagged {
			t.Errorf("a list of %d was flagged; too small to judge", n)
		}
	}
	if s := Assess(repeat("not-an-email", MinSample)); !s.Flagged {
		t.Errorf("a fully malformed list of %d should flag", MinSample)
	}
}

func TestAssessHandlesEmptyAndBlank(t *testing.T) {
	if s := Assess(nil); s.Flagged || s.Total != 0 {
		t.Errorf("empty input: %+v", s)
	}
	// Blank rows are skipped rather than counted as malformed, or a file with
	// trailing newlines would look like a bad list.
	s := Assess([]string{"ada@acme.com", "", "   "})
	if s.Malformed != 0 {
		t.Errorf("blank rows counted as malformed: %+v", s)
	}
}

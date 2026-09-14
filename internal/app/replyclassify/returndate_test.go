package replyclassify

import (
	"testing"
	"time"
)

func TestParseReturnDate(t *testing.T) {
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		subject string
		body    string
		now     time.Time // zero = the shared anchor above
		want    string    // "" means no date
	}{
		{
			name:    "english return on with ordinal",
			subject: "Automatic reply: your email",
			body:    "Thanks for your message. I will reply to your email on my return on the 8th September.",
			want:    "2026-09-08",
		},
		{name: "english back on iso", subject: "Out of office", body: "I am away and back on 2026-09-21.", want: "2026-09-21"},
		{name: "english until month day", subject: "Out of Office", body: "I'm out of the office until September 15, back to normal after.", want: "2026-09-15"},
		{name: "german wieder erreichbar am", subject: "Automatische Antwort: Angebot", body: "Ich bin nicht im Büro und wieder erreichbar am 09.09.2026.", want: "2026-09-09"},
		{name: "german ab dem short dotted", subject: "Abwesenheitsnotiz", body: "Ich bin ab dem 10.9. wieder im Büro.", want: "2026-09-10"},
		// "bis einschliesslich" names the last day AWAY, so the return is the
		// day after it; "back on"/"until" name the return itself.
		{name: "german bis einschliesslich is the last day away", subject: "Abwesend", body: "Ich bin bis einschließlich 30. September abwesend.", want: "2026-10-01"},
		{name: "english through is the last day away", subject: "Out of office", body: "Away through 18 September.", want: "2026-09-19"},
		{name: "english until names the return itself", subject: "Out of office", body: "Out of the office until 18 September.", want: "2026-09-18"},
		// A cue window holds prose as well as the date; the date nearest the
		// cue is the one meant, whatever format it happens to be written in.
		{name: "the earliest date in the window wins", subject: "Out of office", body: "I am away until 10 September; ref 2026-10-01 for the contract.", want: "2026-09-10"},
		{name: "french jusqu au", subject: "Réponse automatique", body: "Je suis absent jusqu'au 12 septembre.", want: "2026-09-12"},
		{name: "dutch terug op", subject: "Automatisch antwoord", body: "Ik ben terug op 7 oktober.", want: "2026-10-07"},
		{
			name: "year rolls forward", subject: "Out of office",
			body: "I'll be back on 5 January.",
			now:  time.Date(2026, 12, 20, 9, 0, 0, 0, time.UTC),
			want: "2027-01-05",
		},
		// The cue is there and the text is not a date; the caller falls back.
		{name: "no date at all", subject: "Automatische Antwort: Ihre Nachricht", body: "Ich bin zur Zeit nicht im Büro."},
		{name: "date with no cue", subject: "Automatic reply", body: "Our next release is 2026-10-01."},
		{name: "date already past", subject: "Out of office", body: "I was back on 2026-08-01."},
		{name: "date beyond the window", subject: "Out of office", body: "I am back on 2027-06-01."},
		{name: "ambiguous slash date is refused", subject: "Out of office", body: "I'm back on 9/8/2026."},
		{name: "a time is not a date", subject: "Out of office", body: "Back on 9.9.2026 (I leave at 17.30.)", want: "2026-09-09"},
		// A month with no day is not a return date: reading the first two
		// digits of the year as one invents a date weeks out and parks a live
		// lead on it.
		{name: "a bare month and year is not a date", subject: "Out of office", body: "I'm out of the office until October 2026."},
		{name: "a bare month and year, back on", subject: "Out of office", body: "I'll be back on November 2026."},
		// Mail clients autocorrect the apostrophe; the cue has to survive it.
		{name: "french typographic apostrophe", subject: "Réponse automatique", body: "Je suis absent jusqu\u2019au 12 septembre.", want: "2026-09-12"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := tc.now
			if at.IsZero() {
				at = now
			}
			got, ok := ParseReturnDate(tc.subject, tc.body, at)
			if tc.want == "" {
				if ok {
					t.Fatalf("ParseReturnDate = %s, want no date", got.Format("2006-01-02"))
				}
				return
			}
			if !ok {
				t.Fatalf("ParseReturnDate found no date, want %s", tc.want)
			}
			if s := got.Format("2006-01-02"); s != tc.want {
				t.Fatalf("ParseReturnDate = %s, want %s", s, tc.want)
			}
		})
	}
}

func TestNextBusinessDay(t *testing.T) {
	// Friday 2026-09-04 -> Monday 2026-09-07.
	if got := NextBusinessDay(time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)); got.Format("2006-01-02") != "2026-09-07" {
		t.Fatalf("Friday + 1 business day = %s, want 2026-09-07", got.Format("2006-01-02"))
	}
	// Tuesday -> Wednesday.
	if got := NextBusinessDay(time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)); got.Format("2006-01-02") != "2026-09-09" {
		t.Fatalf("Tuesday + 1 business day = %s, want 2026-09-09", got.Format("2006-01-02"))
	}
}

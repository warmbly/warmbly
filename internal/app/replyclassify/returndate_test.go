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
		// A weekday before the date takes "den" or a comma, the way German
		// writes it.
		{name: "german ab montag den", subject: "Abwesenheitsnotiz", body: "Ab Montag den 14.09. bin ich wieder im Büro.", want: "2026-09-14"},
		{name: "german ab montag comma den", subject: "Abwesenheitsnotiz", body: "Ab Montag, den 14.09., bin ich wieder im Büro.", want: "2026-09-14"},
		{name: "german ab montag comma", subject: "Abwesenheitsnotiz", body: "Ab Montag, 14.9., bin ich wieder im Büro.", want: "2026-09-14"},
		{name: "german ab dienstag comma", subject: "Abwesenheitsnotiz", body: "Ab Dienstag, 15. September, bin ich wieder erreichbar.", want: "2026-09-15"},
		// A calendar week reads as its Monday; after "bis" it is the last
		// week away, so the return is the Monday after it.
		{name: "german ab kw", subject: "Abwesenheitsnotiz", body: "Ich bin ab KW 38 wieder erreichbar.", want: "2026-09-14"},
		{name: "german ab kw without a space", subject: "Abwesenheitsnotiz", body: "Ab KW38 wieder im Büro.", want: "2026-09-14"},
		{name: "german ab der kalenderwoche", subject: "Abwesenheitsnotiz", body: "Ab der Kalenderwoche 38 bin ich wieder im Büro.", want: "2026-09-14"},
		{name: "german bis kw is the last week away", subject: "Abwesenheitsnotiz", body: "Ich bin bis KW37 im Urlaub.", want: "2026-09-14"},
		{name: "german bis einschliesslich kw", subject: "Abwesenheitsnotiz", body: "Ich bin bis einschließlich KW 37 nicht im Büro.", want: "2026-09-14"},
		{name: "a week under way means back now", subject: "Abwesenheitsnotiz", body: "Ab KW 36 bin ich wieder da.", want: "2026-09-04"},
		{name: "german bis kw range is away through the last week", subject: "Abwesenheitsnotiz", body: "Ich bin bis KW 37/38 im Urlaub.", want: "2026-09-21"},
		{name: "german kw range with an en dash", subject: "Abwesenheitsnotiz", body: "Ich bin bis KW 37\u201338 im Urlaub.", want: "2026-09-21"},
		{name: "german ab kw range starts with its first week", subject: "Abwesenheitsnotiz", body: "Ab KW 38/39 bin ich wieder erreichbar.", want: "2026-09-14"},
		// "ab ... bis ..." names the first day away and then the end.
		{name: "german ab weekday bis is a range", subject: "Abwesenheitsnotiz", body: "Ich bin ab Freitag, 4.9., bis 18.9. im Urlaub.", want: "2026-09-18"},
		{name: "german ab dem bis zum is a range", subject: "Abwesenheitsnotiz", body: "Ich bin ab dem 4.9. bis zum 18.9. nicht im Büro.", want: "2026-09-18"},
		{name: "a negated return word keeps the range", subject: "Abwesenheitsnotiz", body: "Ab dem 7.9. bin ich nicht erreichbar, bis einschließlich 18.9. vertritt mich Frau Klein.", want: "2026-09-19"},
		{name: "a month abbreviation keeps the range", subject: "Abwesenheitsnotiz", body: "Ich bin ab dem 4. Sept. bis zum 18. Sept. im Urlaub.", want: "2026-09-18"},
		{name: "a return word ends the range", subject: "Abwesenheitsnotiz", body: "Ab dem 14.9. bin ich wieder erreichbar, bis zum 30.9. jedoch nur eingeschränkt.", want: "2026-09-14"},
		{name: "a sentence ends the range", subject: "Abwesenheitsnotiz", body: "Ab Montag, 14.9., bin ich im Haus. Bis 30.9. gilt unser Aktionspreis.", want: "2026-09-14"},
		{name: "a sentence opening on bis after a date ends the range", subject: "Abwesenheitsnotiz", body: "Ich bin ab dem 4.9. Bis zum 18.9. vertritt mich Frau Klein, danach Herr Braun.", want: "2026-09-04"},
		{name: "a bis before the return date is not its end", subject: "Abwesenheitsnotiz", body: "Ab dem 14.9. wieder da. Bis zum 11.9. vertritt mich Frau Klein.", want: "2026-09-14"},
		{name: "a week already over is not a return date", subject: "Abwesenheitsnotiz", body: "Ich bin bis KW 30 im Urlaub."},
		{name: "a week that does not exist", subject: "Abwesenheitsnotiz", body: "Ich bin ab KW 60 wieder da."},
		{
			name: "week rolls into next year", subject: "Abwesenheitsnotiz",
			body: "Ab KW 2 bin ich wieder im Büro.",
			now:  time.Date(2026, 12, 21, 9, 0, 0, 0, time.UTC),
			want: "2027-01-11",
		},
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

// The phrase is what a second reader is asked about, so it names the date the
// parser used, as written, and never a date the parser computed.
func TestFindReturnDatePhrase(t *testing.T) {
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name, body, back, phrase string
	}{
		{"a return date", "I am away and back on 2026-09-21. Thanks!", "2026-09-21", "back on 2026-09-21"},
		{"the last day away keeps its own date", "Ich bin bis einschließlich 30. September abwesend.", "2026-10-01", "bis einschliesslich 30. september"},
		{"a calendar week", "Ich bin bis KW 41 im Urlaub.", "2026-10-12", "bis kw 41"},
		{"the end of a range", "Ab dem 4.9. bis zum 18.9. nicht im Büro.", "2026-09-18", "bis zum 18.9."},
		{"a sentence-opening Bis leaves no marker", "Ich bin ab dem 14.9. wieder erreichbar. Bis dahin vertritt mich Frau X.", "2026-09-14", "ab dem 14.9."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := FindReturnDate("Abwesenheitsnotiz", tc.body, now)
			if !ok {
				t.Fatal("no date found")
			}
			if day := got.Back.Format("2006-01-02"); day != tc.back {
				t.Fatalf("back = %s, want %s", day, tc.back)
			}
			if got.Phrase != tc.phrase {
				t.Fatalf("phrase = %q, want %q", got.Phrase, tc.phrase)
			}
		})
	}
}

// Return dates as away messages write them in each language. The date is found
// once the language is chosen, and not the same way without it.
func TestReturnDateCuesByLanguage(t *testing.T) {
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		lang, body, want string
	}{
		{"sv", "Jag är tillbaka den 14 september.", "2026-09-14"},
		{"sv", "Jag är ledig till och med 18 september.", "2026-09-19"},
		{"sv", "Åter på kontoret den 7 oktober.", "2026-10-07"},
		{"sv", "Jag nås igen den 21.9.", "2026-09-21"},
		{"da", "Jeg er tilbage den 14. september.", "2026-09-14"},
		{"da", "Jeg holder ferie til og med 18. september.", "2026-09-19"},
		{"da", "Jeg træffes igen den 7. oktober.", "2026-10-07"},
		{"nb", "Jeg er tilbake den 14. september.", "2026-09-14"},
		{"nb", "Jeg er på ferie til og med 18. september.", "2026-09-19"},
		{"nb", "Tilgjengelig igjen den 2. desember.", "2026-12-02"},
		{"fi", "Palaan toimistolle 14.9.", "2026-09-14"},
		{"fi", "Olen takaisin 21. syyskuuta.", "2026-09-21"},
		{"fi", "Tavoitettavissa jälleen 7. lokakuuta.", "2026-10-07"},
		{"pl", "Wracam 14 września.", "2026-09-14"},
		{"pl", "Będę z powrotem 7 października.", "2026-10-07"},
		{"pl", "Jestem nieobecny do dnia 21.09.2026.", "2026-09-21"},
		{"cs", "Budu zpět 14. září.", "2026-09-14"},
		{"cs", "Vracím se 7. října.", "2026-10-07"},
		{"cs", "Zpět v kanceláři 21.9.2026.", "2026-09-21"},
	}
	for _, tc := range cases {
		day := func(langs ...string) string {
			back, ok := ParseReturnDate("Re: Hello", tc.body, now, langs...)
			if !ok {
				return ""
			}
			return back.Format("2006-01-02")
		}
		if got := day(tc.lang); got != tc.want {
			t.Errorf("%s %q = %q, want %q", tc.lang, tc.body, got, tc.want)
		}
		if got := day(); got == tc.want {
			t.Errorf("%s %q read as %q without %s chosen", tc.lang, tc.body, got, tc.lang)
		}
	}
}

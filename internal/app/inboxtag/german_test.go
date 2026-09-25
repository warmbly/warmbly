package inboxtag

import (
	"context"
	"testing"
)

// German mail through the offline layers, which decide before any call and
// shape what the model reads. The model's own German answers need recorded
// responses; see TestRecordFixtures.

func TestGermanAutomatedMailIsDecidedOffline(t *testing.T) {
	cases := map[string]struct {
		subject, body, want string
	}{
		"abwesenheitsnotiz": {"Abwesenheitsnotiz: Ihre Anfrage", "Ich bin bis zum 12. Oktober nicht im Büro und habe keinen Zugriff auf meine E-Mails.", KindAutoReplyOOO},
		"automatische":      {"Automatische Antwort: AW: Zusammenarbeit", "Vielen Dank für Ihre Nachricht. Ich bin ab Montag wieder erreichbar.", KindAutoReplyOOO},
		"unzustellbar":      {"Unzustellbar: Zusammenarbeit", "Ihre Nachricht konnte an folgende Empfänger nicht zugestellt werden.", KindBounceHard},
	}
	for name, c := range cases {
		asker := &countingAsker{}
		svc := newService(t, asker, &fakeRepo{})
		m := inboundMessage()
		m.Subject, m.BodyText = c.subject, c.body
		d, err := svc.Classify(context.Background(), m)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if d.Kind != c.want || d.KindSource != "header" || asker.calls != 0 {
			t.Errorf("%s: kind %q from %q after %d calls, want %q offline", name, d.Kind, d.KindSource, asker.calls, c.want)
		}
	}
}

// A German reply is read without the history Outlook quotes under it, once
// the workspace says its mail is German.
func TestGermanReplyStateDropsQuotedHistory(t *testing.T) {
	body := "Vielen Dank, aktuell haben wir leider keine passende Position. Ich melde mich, sobald sich das ändert.\n\n" +
		"Von: Anna Beispiel <anna@example.com>\nGesendet: Dienstag, 4. März 2025 09:30\nAn: Max Muster\nBetreff: Bewerbung\n\n" +
		"Hallo Max, ich wollte mich kurz vorstellen."
	st := BuildState("AW: Bewerbung", body, "", "", "de")
	want := "Vielen Dank, aktuell haben wir leider keine passende Position. Ich melde mich, sobald sich das ändert."
	if st.Body != want {
		t.Fatalf("body = %q", st.Body)
	}
	if st := BuildState("AW: Bewerbung", body, "", ""); st.Body == want {
		t.Fatal("German Outlook quotes were cut for a workspace that did not choose German")
	}
}

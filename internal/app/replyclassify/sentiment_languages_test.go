package replyclassify

import "testing"

// Replies that turn the offer down, in each language as people write them
// (capitalised, with and without accents). Each is negative once its language
// is chosen and undecided without it, so no workspace reads a language it did
// not pick.
func TestDeclinedAndNegativeRepliesByLanguage(t *testing.T) {
	negative := map[string][]string{
		"de": {
			"Danke, aber wir haben kein Interesse.",
			"Kein Interesse.",
			"Wir sind derzeit nicht interessiert.",
			"Das interessiert uns nicht, danke.",
			"Nein danke.",
			"Nein, danke!",
			"Aktuell kein Bedarf.",
			"Wir haben keinen Bedarf an Ihrer Lösung.",
			"Ich bin dafür nicht zuständig.",
			"Da sind Sie bei mir leider an den falschen Ansprechpartner geraten.",
		},
		"fr": {
			"Merci, mais je ne suis pas intéressé.",
			"Pas interesse, merci.",
			"Nous ne sommes pas intéressés pour le moment.",
			"Ça ne nous intéresse pas.",
			"Aucun intérêt pour nous.",
			"Je ne suis plus intéressée.",
			"Non merci.",
			"Non, merci beaucoup.",
			"Je ne suis pas le bon interlocuteur.",
			"Nous avons déjà une solution en place.",
		},
		"es": {
			"Gracias, pero no estoy interesado.",
			"No estamos interesados en este momento.",
			"No me interesa, gracias.",
			"No nos interesa.",
			"No tenemos interes.",
			"No, gracias.",
			"No gracias",
			"No soy la persona adecuada para esto.",
			"Creo que ha contactado a la persona equivocada.",
			"Ya tenemos una solución para eso.",
		},
	}
	for lang, replies := range negative {
		for _, body := range replies {
			in := Input{Subject: "Re: Hallo", BodyText: body, Languages: []string{lang}}
			if got := ClassifyOffline(in); got.Class != ClassNegative {
				t.Errorf("%s %q = %q, want negative", lang, body, got.Class)
			}
			in.Languages = nil
			if got := ClassifyOffline(in); got.Class == ClassNegative {
				t.Errorf("%s %q read as negative without %s chosen", lang, body, lang)
			}
		}
	}
}

// Interest, questions and thanks stay undecided offline: this layer only
// learns how people say no.
func TestInterestedRepliesAreNotReadAsNegative(t *testing.T) {
	neutral := map[string][]string{
		"de": {
			"Sehr interessant, können wir nächste Woche telefonieren?",
			"Interesse besteht, bitte schicken Sie mir mehr Informationen.",
			"Danke für die Nachricht, ich melde mich.",
			"Wir sind interessiert, wann passt es Ihnen?",
		},
		"fr": {
			"Très intéressé, appelons-nous mardi.",
			"Merci, c'est intéressant. Pouvez-vous m'envoyer une offre ?",
			"Je suis intéressée, quel est le tarif ?",
		},
		"es": {
			"Bueno gracias, estoy interesado en una demo.",
			"Me interesa mucho, ¿hablamos el jueves?",
			"Gracias, estoy interesado en saber más.",
			"Sí, gracias, envíeme la propuesta.",
		},
	}
	all := []string{"de", "fr", "es"}
	for lang, replies := range neutral {
		for _, body := range replies {
			if got := ClassifyOffline(Input{Subject: "Re: Hallo", BodyText: body, Languages: all}); got.Class == ClassNegative {
				t.Errorf("%s %q read as negative", lang, body)
			}
		}
	}
}

// Choosing a language adds its phrases and keeps the English ones.
func TestChosenLanguagesKeepTheEnglishPhrases(t *testing.T) {
	for _, body := range []string{"Not interested, thanks.", "No thanks.", "Sounds good, let's talk.", "Not réally interested? I am, tell me more."} {
		base := ClassifyOffline(Input{BodyText: body})
		withLanguages := ClassifyOffline(Input{BodyText: body, Languages: []string{"de", "fr", "es"}})
		if base.Class != withLanguages.Class {
			t.Errorf("%q = %q without languages, %q with them", body, base.Class, withLanguages.Class)
		}
	}
}

// A chosen language's phrase must stand as whole words; the English base set
// keeps its substring match.
func TestLanguagePhrasesMatchWholeWords(t *testing.T) {
	bounded := phrase{text: "no gracias", bounded: true}
	for text, want := range map[string]bool{
		"no gracias":                 true,
		"vale, no gracias.":          true,
		"bueno gracias, me interesa": false,
		"bueno gracias y no gracias": true,
		"no graciasx":                false,
	} {
		if got := bounded.in(text, text); got != want {
			t.Errorf("bounded %q in %q = %v, want %v", bounded.text, text, got, want)
		}
	}
	if !(phrase{text: "n't interested"}).in("we aren't interested", "we aren't interested") {
		t.Error("an English phrase lost its substring match")
	}
}

// The English base set reads the reply as written, so folding accents for the
// chosen languages never changes an English verdict.
func TestEnglishPhrasesReadTheTextAsWritten(t *testing.T) {
	if (phrase{text: "not really interested"}).in("not réally interested", "not really interested") {
		t.Error("an English phrase matched the accent-folded text")
	}
	if got := ClassifyOffline(Input{BodyText: "Not réally interested? I am, tell me more."}); got.Class == ClassNegative {
		t.Errorf("English reply with an accent = %q, want it read as before", got.Class)
	}
}

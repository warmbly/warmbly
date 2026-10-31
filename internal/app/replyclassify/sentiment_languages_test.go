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
		"nl": {
			"Bedankt, maar we hebben geen interesse.",
			"Geen interesse.",
			"Wij zijn niet geïnteresseerd.",
			"We zijn niet meer geinteresseerd.",
			"Dat interesseert ons niet.",
			"Nee, dank je wel.",
			"Nee bedankt.",
			"Op dit moment geen behoefte.",
			"Ik ben niet de juiste persoon hiervoor.",
			"Wij hebben al een oplossing.",
		},
		"it": {
			"Grazie, ma non sono interessato.",
			"Non siamo interessati al momento.",
			"Non sono più interessata.",
			"Non mi interessa, grazie.",
			"Nessun interesse.",
			"No, grazie.",
			"No grazie",
			"Non fa per noi.",
			"Credo abbia scritto alla persona sbagliata.",
			"Abbiamo già una soluzione.",
		},
		"pt": {
			"Obrigado, mas não tenho interesse.",
			"Não temos interesse no momento.",
			"Não estou interessada.",
			"Nao estamos interessados.",
			"Não me interessa.",
			"Não, obrigado.",
			"Nao obrigada",
			"Não sou a pessoa certa para isso.",
			"Acho que falou com a pessoa errada.",
			"Já temos uma solução.",
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
		"nl": {
			"Interessant, kunnen we volgende week bellen?",
			"Ja graag, stuur me meer informatie.",
			"Bedankt, ik ben geïnteresseerd.",
		},
		"it": {
			"Buono, grazie mille, sono interessato a una demo.",
			"Molto interessante, possiamo sentirci giovedì?",
			"Grazie, sono interessata a saperne di più.",
		},
		"pt": {
			"Tenho interesse, podemos falar amanhã?",
			"Obrigado, estou interessado em saber mais.",
			"Sim, obrigada, envie a proposta.",
		},
		"es": {
			"Bueno gracias, estoy interesado en una demo.",
			"Me interesa mucho, ¿hablamos el jueves?",
			"Gracias, estoy interesado en saber más.",
			"Sí, gracias, envíeme la propuesta.",
		},
	}
	all := []string{"de", "fr", "es", "nl", "it", "pt"}
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
		withLanguages := ClassifyOffline(Input{BodyText: body, Languages: []string{"de", "fr", "es", "nl", "it", "pt"}})
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

// Replies that show interest, in each language as people write them. Each is
// positive once its language is chosen and not positive without it.
func TestInterestedRepliesByLanguage(t *testing.T) {
	positive := map[string][]string{
		"de": {
			"Klingt gut, lassen Sie uns telefonieren.",
			"Ich bin interessiert, was kostet das?",
			"Klingt interessant!",
			"Wir sind interessiert.",
			"Gerne mehr erfahren.",
			"Können wir telefonieren?",
		},
		"fr": {
			"Ça m'intéresse, quel est le tarif ?",
			"Je suis intéressée.",
			"Nous sommes intéressés, prenons rendez-vous.",
			"Cela semble intéressant.",
			"Envoyez-moi un devis s'il vous plaît.",
			"Ca nous interesse.",
		},
		"es": {
			"Me interesa mucho.",
			"Sí, me interesa.",
			"Estoy muy interesado, ¿cuánto cuesta?",
			"Agendemos una llamada.",
			"Envíeme más información, por favor.",
			"Estamos muy interesados.",
			"Me interesa mucho, pero tengo una pregunta.",
		},
	}
	for lang, replies := range positive {
		for _, body := range replies {
			in := Input{Subject: "Re: Hallo", BodyText: body, Languages: []string{lang}}
			if got := ClassifyOffline(in); got.Class != ClassPositive {
				t.Errorf("%s %q = %q, want positive", lang, body, got.Class)
			}
			in.Languages = nil
			if got := ClassifyOffline(in); got.Class == ClassPositive {
				t.Errorf("%s %q read as positive without %s chosen", lang, body, lang)
			}
		}
	}
}

// An interest phrase that is negated, outweighed by a no, or only conditional
// never reads as interest.
func TestNegatedAndHedgedInterestIsNotPositive(t *testing.T) {
	all := []string{"de", "fr", "es", "nl", "it", "pt"}
	for _, body := range []string{
		"Ich bin nicht interessiert.",
		"Klingt gut, aber wir haben keinen Bedarf.",
		"Wir möchten keinen Termin vereinbaren.",
		"Wir wären interessiert, wenn der Preis stimmt.",
		"Falls Interesse besteht, melden wir uns.",
		"Ça m'intéresse pas.",
		"Cela ne nous intéresse plus.",
		"Je ne suis pas intéressé.",
		"Nous ne sommes pas intéressés.",
		"No me interesa mucho.",
		"No estoy muy interesado.",
		"Gracias, pero no estamos muy interesados.",
		"Si me interesa, les aviso.",
		"Si me interesa mucho, les aviso.",
		"Si cela m'intéresse, je vous contacterai.",
		"Si ça m'intéresse, je vous recontacte.",
		"Falls es passt, können wir telefonieren.",
		"No\nme interesa mucho.",
		"No quiero agendar una llamada.",
		"Wir wollen nicht gleich einen Termin vereinbaren.",
		"Ich glaube nicht, dass wir einen Termin vereinbaren sollten.",
		"No me parece buena idea programar una llamada.",
		"No sé todavía si nos interesa mucho.",
		"Je ne crois pas, honnêtement, que cela nous intéresse.",
		"Me interesa mucho, pero no ahora.",
		"Me interesa mucho. Pero no este año.",
		"Klingt interessant, aber nicht vor dem nächsten Quartal.",
		"Ça m'intéresse, mais pas maintenant.",
	} {
		if got := ClassifyOffline(Input{Subject: "Re: Hallo", BodyText: body, Languages: all}); got.Class == ClassPositive {
			t.Errorf("%q read as positive", body)
		}
	}
}

// A negator or condition earlier in the sentence qualifies the interest.
func TestNegatedOrConditionalAtReadsTheSentence(t *testing.T) {
	for text, want := range map[string]bool{
		"no me interesa mucho":          true,
		"no, me interesa mucho":         true,
		"pero no  me interesa mucho":    true,
		"no\nme interesa mucho":         true,
		"no a b c me interesa mucho":    true,
		"si me interesa mucho":          true,
		"bueno me interesa mucho":       false,
		"me interesa mucho":             false,
		"no. me interesa mucho":         false,
		"por que no? me interesa mucho": false,
	} {
		if got := negatedOrConditionalAt(text, len(text)-len("me interesa mucho")); got != want {
			t.Errorf("negatedOrConditionalAt(%q) = %v, want %v", text, got, want)
		}
	}
}

// A negator in the subject never turns the body's interest around.
func TestSubjectNegatorDoesNotReachTheBody(t *testing.T) {
	in := Input{Subject: "Re: No-code platform", BodyText: "Me interesa mucho.", Languages: []string{"es"}}
	if got := ClassifyOffline(in); got.Class != ClassPositive {
		t.Errorf("%q under %q = %q, want positive", in.BodyText, in.Subject, got.Class)
	}
}

package replyclassify

import "testing"

// The subject an autoresponder writes, in each language, as the mail client
// sends it (mixed case, all caps, accents or none). The ones every workspace
// has always had match without a language; the rest only once it is chosen.
func TestOOOSubjectsByLanguage(t *testing.T) {
	base := []string{
		"Automatic reply: Hello", "Automatische Antwort: Hallo", "Réponse automatique : Bonjour",
		"Respuesta automática: Hola", "Resposta automática: Olá", "Risposta automatica: Ciao",
		"Automatisch antwoord: Hallo", "Automatiskt svar: Hej", "Automatisk svar: Hej",
	}
	for _, subject := range base {
		if !matchesOOOSubject(subject, nil) {
			t.Errorf("%q not read as an away message", subject)
		}
	}
	chosen := map[string]string{
		"Automaattinen vastaus: Hei": "fi", "Odpowiedź automatyczna: Cześć": "pl", "Automatická odpověď: Dobrý den": "cs",
		"Răspuns automat: Salut": "ro", "Automatikus válasz: Szia": "hu", "Автоматический ответ: Привет": "ru",
		"Автоматична відповідь: Привіт": "uk", "Αυτόματη απάντηση: Γεια": "el", "ΑΥΤΟΜΑΤΗ ΑΠΑΝΤΗΣΗ: ΓΕΙΑ": "el",
		"Otomatik yanıt: Merhaba": "tr", "OTOMATİK YANIT: MERHABA": "tr", "自動応答: こんにちは": "ja", "自动回复: 你好": "zh",
		"自動回覆: 你好": "zh", "자동 회신: 안녕하세요": "ko", "Balasan otomatis: Halo": "id", "Trả lời tự động: Xin chào": "vi",
		"ตอบกลับอัตโนมัติ: สวัสดี": "th", "स्वचालित उत्तर: नमस्ते": "hi", "رد تلقائي: مرحبا": "ar", "תשובה אוטומטית: שלום": "he",
		"Fuera de la oficina: Hola": "es", "Buiten kantoor: Hallo": "nl",
	}
	for subject, lang := range chosen {
		if !matchesOOOSubject(subject, []string{lang}) {
			t.Errorf("%q not read as an away message with %s", subject, lang)
		}
		if matchesOOOSubject(subject, nil) {
			t.Errorf("%q read as an away message without %s", subject, lang)
		}
	}
}

// A person's reply starts with their client's reply prefix, never a marker.
func TestOOOSubjectsNeverMatchAReply(t *testing.T) {
	all := LanguagesWithRules()
	for _, subject := range []string{
		"Re: Automatische Antwort", "AW: Réponse automatique", "Odp: poza biurem",
		"RE: 自動応答", "回复: 自动回复", "Ynt: Otomatik yanıt",
	} {
		if matchesOOOSubject(subject, all) {
			t.Errorf("%q read as an away message", subject)
		}
	}
}

// A localized non-delivery subject is a bounce only for a workspace that reads
// that language; the base subjects are for everyone.
func TestLocalizedBounceSubjects(t *testing.T) {
	for subject, lang := range map[string]string{
		"No se puede entregar: Oferta": "es", "Non recapitabile: Offerta": "it",
		"Onbestelbaar: Aanbod": "nl", "Não é possível entregar: Oferta": "pt",
	} {
		if !IsDeliveryFailure(Input{Subject: subject, Languages: []string{lang}}) {
			t.Errorf("%q not a bounce with %s", subject, lang)
		}
		if IsDeliveryFailure(Input{Subject: subject}) {
			t.Errorf("%q a bounce without %s", subject, lang)
		}
	}
	if !IsDeliveryFailure(Input{Subject: "Unzustellbar: Angebot"}) {
		t.Error("the base German subject needs no language")
	}
	if IsDeliveryFailure(Input{Subject: "Re: No se puede entregar", Languages: []string{"es"}}) {
		t.Error("a reply about a bounce read as one")
	}
}

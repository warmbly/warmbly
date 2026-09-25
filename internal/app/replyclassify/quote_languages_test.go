package replyclassify

import (
	"strings"
	"testing"
)

const quotedTail = "\n> Hello Anna, a short note about working together.\n> Reply stop to unsubscribe."

// Each client's attribution line, as Gmail, Thunderbird or Apple Mail writes
// it in that language, ends the reply once the workspace reads that language.
// base marks the ones every workspace has always had.
func TestStripQuotedAttributionsByLanguage(t *testing.T) {
	cases := []struct {
		lang, line string
		base       bool
	}{
		{"en", "On Mon, 3 Mar 2025 at 10:12, Anna <anna@example.com> wrote:", true},
		{"de", "Am Mo., 3. März 2025 um 10:12 Uhr schrieb Anna <anna@example.com>:", true},
		{"fr", "Le lun. 3 mars 2025 à 10:12, Anna <anna@example.com> a écrit :", true},
		{"es", "El lun, 3 mar 2025 a las 10:12, Anna (<anna@example.com>) escribió:", true},
		{"it", "Il giorno lun 3 mar 2025 alle ore 10:12 Anna <anna@example.com> ha scritto:", true},
		{"nl", "Op ma 3 mrt 2025 om 10:12 schreef Anna <anna@example.com>:", true},
		{"pt", "Em seg., 3 de mar. de 2025 às 10:12, Anna <anna@example.com> escreveu:", false},
		{"sv", "Den mån 3 mars 2025 kl 10:12 skrev Anna <anna@example.com>:", false},
		{"da", "Den man. 3. mar. 2025 kl. 10.12 skrev Anna <anna@example.com>:", false},
		{"nb", "man. 3. mar. 2025 kl. 10:12 skrev Anna <anna@example.com>:", false},
		{"fi", "ma 3. maalisk. 2025 klo 10.12 Anna (anna@example.com) kirjoitti:", false},
		{"pl", "pon., 3 mar 2025 o 10:12 Anna <anna@example.com> napisał(a):", false},
		{"pl", "W dniu 3.03.2025 o 10:12, Anna pisze:", false},
		{"cs", "po 3. 3. 2025 v 10:12 odesílatel Anna <anna@example.com> napsal:", false},
		{"ro", "Pe lun., 3 mar. 2025 la 10:12, Anna <anna@example.com> a scris:", false},
		{"hu", "Anna <anna@example.com> ezt írta (időpont: 2025. márc. 3., H, 10:12):", false},
		{"hu", "2025. 03. 03. 10:12 keltezéssel, Anna írta:", false},
		{"ru", "пн, 3 мар. 2025 г. в 10:12, Anna <anna@example.com>:", false},
		{"ru", "03.03.2025 10:12, Anna пишет:", false},
		{"uk", "пн, 3 бер. 2025 р. о 10:12 Anna <anna@example.com> пише:", false},
		{"el", "Στις Δευ 3 Μαρ 2025 στις 10:12, ο/η Anna <anna@example.com> έγραψε:", false},
		{"tr", "Anna <anna@example.com>, 3 Mar 2025 Pzt, 10:12 tarihinde şunu yazdı:", false},
		{"ja", "2025年3月3日(月) 10:12 Anna <anna@example.com>:", false},
		{"zh", "Anna <anna@example.com> 于2025年3月3日周一 10:12写道：", false},
		{"zh", "Anna <anna@example.com> 於 2025年3月3日 週一 上午10:12寫道：", false},
		{"ko", "2025년 3월 3일 (월) 오전 10:12, Anna <anna@example.com>님이 작성:", false},
		{"id", "Pada Sen, 3 Mar 2025 pukul 10.12 Anna <anna@example.com> menulis:", false},
		{"vi", "Vào Th 2, 3 thg 3, 2025 vào lúc 10:12 Anna <anna@example.com> đã viết:", false},
		{"th", "เมื่อ จ. 3 มี.ค. 2025 เวลา 10:12 Anna <anna@example.com> เขียนว่า:", false},
		{"hi", "सोम, 3 मार्च 2025 को 10:12 am बजे Anna <anna@example.com> ने लिखा:", false},
		{"ar", "في الاثنين، 3 مارس 2025 في 10:12 ص كتب Anna <anna@example.com>:", false},
		{"he", "בתאריך יום ב׳, 3 במרץ 2025 ב-10:12 מאת Anna <anna@example.com>:", false},
	}
	for _, c := range cases {
		body := "The reply itself.\n\n" + c.line + quotedTail
		if got := StripQuoted(body, c.lang); got != "The reply itself." {
			t.Errorf("%s: StripQuoted = %q", c.lang, got)
		}
		if IsOptOut("Re: Hello", StripQuoted(body, c.lang)) {
			t.Errorf("%s: the quoted footer opted out", c.lang)
		}
		// Without the language only the base set reads it.
		if stripped := StripQuoted(body) == "The reply itself."; stripped != c.base {
			t.Errorf("%s: read without the language = %v, want %v: %q", c.lang, stripped, c.base, c.line)
		}
	}
}

// Outlook's quoted header block, with that language's labels.
func TestStripQuotedOutlookBlocksByLanguage(t *testing.T) {
	cases := map[string][2]string{
		"de": {"Von:", "Gesendet:"}, "fr": {"De :", "Envoyé :"}, "es": {"De:", "Enviado:"}, "pt": {"De:", "Enviada em:"},
		"it": {"Da:", "Inviato:"}, "nl": {"Van:", "Verzonden:"}, "sv": {"Från:", "Skickat:"},
		"da": {"Fra:", "Sendt:"}, "fi": {"Lähettäjä:", "Lähetetty:"}, "pl": {"Od:", "Wysłano:"},
		"cs": {"Od:", "Odesláno:"}, "ro": {"De la:", "Trimis:"}, "hu": {"Feladó:", "Elküldve:"},
		"ru": {"От:", "Отправлено:"}, "uk": {"Від:", "Надіслано:"}, "el": {"Από:", "Στάλθηκε:"},
		"tr": {"Kimden:", "Gönderildi:"}, "ja": {"差出人:", "送信日時:"}, "zh": {"发件人:", "发送时间:"},
		"ko": {"보낸 사람:", "보낸 날짜:"}, "id": {"Dari:", "Dikirim:"},
		"vi": {"Từ:", "Đã gửi:"}, "ar": {"من:", "تاريخ الإرسال:"}, "he": {"מאת:", "נשלח:"},
	}
	for lang, labels := range cases {
		body := "The reply itself.\n\n" + labels[0] + " Anna <anna@example.com>\n" + labels[1] + " 3.3.2025 10:12\nTo: Max" + quotedTail
		if got := StripQuoted(body, lang); got != "The reply itself." {
			t.Errorf("%s: StripQuoted = %q", lang, got)
		}
		if StripQuoted(body) == "The reply itself." {
			t.Errorf("%s: read without the language", lang)
		}
	}
}

func TestStripQuotedSeparatorsByLanguage(t *testing.T) {
	for sep, lang := range map[string]string{
		"-----Original Message-----": "", "-----Message d'origine-----": "fr", "-----Mensaje original-----": "es",
		"---------- Mensagem encaminhada ---------": "pt", "-----Oorspronkelijk bericht-----": "nl",
		"-----Исходное сообщение-----": "ru", "-----原始邮件-----": "zh", "---------- 転送されたメッセージ ---------": "ja",
		"-----Oryginalna wiadomość-----": "pl", "-----Αρχικό μήνυμα-----": "el",
	} {
		body := "The reply itself.\n\n" + sep + "\nAnna" + quotedTail
		if got := StripQuoted(body, lang); got != "The reply itself." {
			t.Errorf("%s: StripQuoted = %q", sep, got)
		}
		if stripped := StripQuoted(body) == "The reply itself."; stripped != (lang == "") {
			t.Errorf("%s: read without the language = %v", sep, stripped)
		}
	}
}

// Prose that mentions a date and someone writing is the reply, not a quote,
// even for a workspace that reads that language.
func TestStripQuotedKeepsProseInEveryLanguage(t *testing.T) {
	all := LanguagesWithRules()
	for _, prose := range []string{
		"Tack, vi hörs den 3 mars 2025 kl 10.",
		"Спасибо, в 2024 году клиент пишет: всё хорошо.",
		"شكرا، في 2024 كتب لنا العميل: كل شيء جيد.",
		"Ευχαριστώ, στις 3 Μαρτίου 2025 ο πελάτης έγραψε: όλα καλά.",
		"धन्यवाद, 2025 को उन्होंने ने लिखा: सब ठीक है।",
		"תודה, בתאריך 3.3.2025 קיבלנו מכתב מאת הלקוח.",
		"Od: jutra jestem dostępny, zapraszam.",
		"Obrigado, em 2024 o cliente escreveu muito.",
		"Давайте созвонимся 12 марта 2026 г. в 14:00, если вам удобно.",
		"2026年3月12日(木) 14:00 からいかがでしょうか。",
	} {
		if got := StripQuoted(prose, all...); got != prose {
			t.Errorf("prose cut: %q -> %q", prose, got)
		}
	}
}

// A body stored on one line has no line to go back to, so an attribution that
// opens with a name or a weekday cuts at the match and the reply survives.
func TestStripQuotedOneLineBodyKeepsTheReply(t *testing.T) {
	body := "Dziękuję, zgoda. pon., 3 mar 2025 o 10:12 Anna <anna@example.com> napisał(a): > Hello"
	got := StripQuoted(body, "pl")
	if !strings.HasPrefix(got, "Dziękuję, zgoda.") || strings.Contains(got, "napisał") {
		t.Fatalf("StripQuoted = %q", got)
	}
}

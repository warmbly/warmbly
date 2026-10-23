package replyclassify

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// The offline layers read every reply with the base vocabulary below, and a
// workspace's tagging languages (models.MailLanguageNames codes) add theirs.
// A language nobody chose adds no pattern, so it cannot misread anyone's mail.
// The one exception is the opt-out scan, which always cuts the quote markers
// of the languages its phrases are in (optOutLanguages in lexicon.go).

// langRules is one language's vocabulary, as its mail clients and servers
// write it.
type langRules struct {
	// quote begins the quoted history under a reply.
	quote []quoteMarker
	// separators are the words inside a dashed original or forwarded line.
	separators []string
	// ooo are away-message subject prefixes, in their own spelling.
	ooo []string
	// cues introduce a return date; inclusive ones name the last day away.
	// Only the base set carries them: nothing passes languages to the
	// return-date reader.
	cues, inclusive []string
	months          map[int][]string
	// bounce are non-delivery subject prefixes, lower case.
	bounce []string
	// declined turns down interest and is read before any interest phrase;
	// negative is any other clear no. Both are phrases, never single words.
	declined, negative []string
}

var baseRules = langRules{
	declined: declinedInterestKeywords,
	negative: negativeKeywords,
	quote: []quoteMarker{
		{re: attribution(`on`, `wrote:`)},
		{re: regexp.MustCompile(`(?i)_{10,}`)},
		{re: regexp.MustCompile(`(?im)^\s*from:\s.+$`)},
		{re: headerBlock(`from`, `sent|date`)},
		{re: attribution(`le`, `a écrit\s*:`)},
		{re: attribution(`am`, `schrieb\b`)},
		{re: attribution(`el`, `escribió\s*:`)},
		{re: attribution(`op`, `schreef\b`)},
		{re: attribution(`il`, `ha scritto\s*:`)},
	},
	separators: []string{`original message`, `forwarded message`},
	ooo: []string{
		// English
		"out of office", "out of the office", "automatic reply", "automated reply",
		"autoreply", "auto-reply", "auto reply", "auto:", "away:", "on vacation:", "vacation reply",
		// German
		"abwesenheit", "abwesend", "automatische antwort", "autom. antwort",
		"ausser haus", "nicht im buero", "im urlaub:",
		// French
		"reponse automatique", "absence du bureau", "message d'absence",
		// Spanish / Portuguese
		"respuesta automatica", "ausencia de la oficina", "ausencia temporal",
		"resposta automatica", "fora do escritorio",
		// Italian
		"risposta automatica", "fuori sede:", "assente dall'ufficio",
		// Dutch
		"automatisch antwoord", "afwezigheid", "afwezigheidsbericht",
		// Nordic / Polish
		"automatiskt svar", "automatisk svar", "autosvar", "fravaer", "fravaersmelding",
		"automatyczna odpowiedz",
	},
	cues: []string{
		// English
		`back on`, `back in the office on`, `back at my desk on`, `be back on`,
		`return on`, `returning on`, `will return on`, `i return on`, `my return on`,
		`returns on`, `available again on`, `reachable again on`, `back from`,
		`until`, `till`, `through`,
		// German
		`zurueck am`, `zurueck ab`, `wieder am`, `wieder ab`, `ab dem`,
		// "ab Montag den 12.10.", "ab Montag, den 12.10." and "ab Montag,
		// 12.10.": the weekday needs "den" or a comma before the date.
		`ab (?:montag|dienstag|mittwoch|donnerstag|freitag)(?:,? de[nm]|, ?)`,
		`wieder erreichbar am`, `wieder erreichbar ab`, `wieder im buero am`,
		`bis einschliesslich`, `bis zum`, `bis`,
		// A calendar week ("ab KW 42"); "Kalenderwoche" reads as "kw".
		`ab kw`, `ab der kw`,
		// French
		`de retour le`, `jusqu'au`, `jusqu au`, `a partir du`,
		// Spanish / Portuguese
		`de vuelta el`, `regreso el`, `hasta el`, `a partir del`,
		`de volta a`, `ate o dia`, `a partir de`,
		// Dutch
		`terug op`, `weer aanwezig op`, `tot en met`,
		// Italian
		`di ritorno il`, `fino al`, `rientro il`,
	},
	inclusive: []string{"through", "bis einschliesslich", "tot en met"},
	months: map[int][]string{
		1:  {"january", "jan", "januar", "janvier", "enero", "ene", "janeiro", "januari", "gennaio"},
		2:  {"february", "feb", "februar", "fevrier", "febrero", "fevereiro", "februari", "febbraio"},
		3:  {"march", "mar", "maerz", "marz", "mars", "marzo", "marco", "maart"},
		4:  {"april", "apr", "avril", "abril", "aprile"},
		5:  {"may", "mai", "mayo", "maio", "mei", "maggio"},
		6:  {"june", "jun", "juni", "juin", "junio", "junho", "giugno"},
		7:  {"july", "jul", "juli", "juillet", "julio", "julho", "luglio"},
		8:  {"august", "aug", "aout", "agosto", "augustus", "ago"},
		9:  {"september", "sep", "sept", "septembre", "septiembre", "setembro", "settembre"},
		10: {"october", "oct", "oktober", "octobre", "octubre", "outubro", "okt", "ottobre"},
		11: {"november", "nov", "novembre", "noviembre", "novembro"},
		12: {"december", "dec", "dezember", "decembre", "diciembre", "dezembro", "dez", "dicembre"},
	},
}

// Gmail in Swedish, Danish and Norwegian: "[Den] mån 3 mars 2025 kl 10:12 skrev".
var nordicAttribution = quoteMarker{re: regexp.MustCompile(`(?i)(^|\s)(den\s+)?(\pL{2,4}\.?,?\s+)?\d{1,2}\.?\s*\pL{3,9}\.?\s+\d{4},?\s+kl\.?\s+\d{1,2}[:.]\d{2}\s[^\n]{0,250}?\bskrev\b`)}

// Thunderbird in Russian and Ukrainian: "03.03.2025 10:12, Name пишет:".
var cyrillicAttribution = quoteMarker{re: regexp.MustCompile(`(?i)\d{1,2}[./]\d{1,2}[./]\d{2,4},?\s+\d{1,2}:\d{2},\s[^\n]{0,200}?\s(пишет|пише|написал(\(а\))?|написав(\(ла\))?)\s*:`), lineStart: true}

// gmailSigned is Gmail's attribution in the languages whose verb follows the
// quoted address: "<date> Anna <anna@example.com> kirjutas:".
func gmailSigned(verbs string) quoteMarker {
	return quoteMarker{re: regexp.MustCompile(`(?i)@[^\n]{0,120}?[>)]\s*[^\n]{0,30}?(` + verbs + `)\s*[:：]`), lineStart: true}
}

var languageRules = map[string]langRules{
	"de": {
		// GMX and web.de put the date first: "Gesendet: ... Von: ...".
		quote:      []quoteMarker{{re: headerBlock(`von`, `gesendet|datum`)}, {re: headerBlock(`gesendet`, `von`)}},
		separators: []string{`ursprüngliche nachricht`, `original-nachricht`, `weitergeleitete nachricht`},
		declined: []string{
			"kein interesse", "keinerlei interesse", "nicht interessiert", "nicht mehr interessiert",
			"interessiert uns nicht", "interessiert mich nicht",
		},
		negative: []string{
			"nein danke", "nein, danke", "kein bedarf", "keinen bedarf", "falscher ansprechpartner", "falschen ansprechpartner",
			"nicht der richtige ansprechpartner", "nicht die richtige ansprechpartnerin", "nicht zuständig",
		},
	},
	"fr": {
		quote:      []quoteMarker{{re: headerBlock(`de ?`, `envoyé ?|date ?`)}},
		separators: []string{`message d'origine`, `message transféré`},
		ooo:        []string{"absent du bureau", "absente du bureau"},
		declined: []string{
			"pas intéressé", "pas intéressée", "pas intéressés", "pas intéressées", "aucun intérêt",
			"ne nous intéresse pas", "ne m'intéresse pas", "ne suis plus intéressé", "ne suis plus intéressée",
			"ne sommes plus intéressés", "ne sommes plus intéressées",
		},
		negative: []string{
			"non merci", "non, merci", "pas le bon interlocuteur", "pas la bonne personne",
			"nous avons déjà une solution", "nous avons déjà un prestataire",
		},
	},
	"es": {
		quote:      []quoteMarker{{re: headerBlock(`de`, `enviado|fecha`)}},
		separators: []string{`mensaje original`, `mensaje reenviado`},
		ooo:        []string{"fuera de la oficina"},
		bounce:     []string{"no se puede entregar"},
		declined: []string{
			"no estoy interesado", "no estoy interesada", "no estamos interesados", "no estamos interesadas",
			"no me interesa", "no nos interesa", "no tengo interés", "no tenemos interés", "sin interés",
		},
		negative: []string{
			"no gracias", "no, gracias", "no soy la persona", "persona equivocada",
			"ya tenemos una solución", "ya tenemos proveedor",
		},
	},
	"pt": {
		quote: []quoteMarker{
			{re: attribution(`em`, `escreveu\s*:`)},
			{re: headerBlock(`de`, `enviado|enviada em|data`)},
		},
		separators: []string{`mensagem original`, `mensagem encaminhada`},
		bounce:     []string{"não é possível entregar"},
	},
	"it": {
		quote:      []quoteMarker{{re: headerBlock(`da`, `inviato|data`)}},
		separators: []string{`messaggio originale`, `messaggio inoltrato`},
		ooo:        []string{"fuori ufficio"},
		bounce:     []string{"non recapitabile"},
	},
	"nl": {
		quote:      []quoteMarker{{re: headerBlock(`van`, `verzonden|datum`)}},
		separators: []string{`oorspronkelijk bericht`, `doorgestuurd bericht`},
		ooo:        []string{"buiten kantoor"},
		bounce:     []string{"onbestelbaar"},
	},
	"sv": {
		quote:      []quoteMarker{nordicAttribution, {re: headerBlock(`från`, `skickat|datum`)}},
		separators: []string{`ursprungligt meddelande`, `vidarebefordrat meddelande`},
		ooo:        []string{"frånvaro"},
	},
	"da": {
		quote:      []quoteMarker{nordicAttribution, {re: headerBlock(`fra`, `sendt|dato`)}},
		separators: []string{`oprindelig meddelelse`, `videresendt meddelelse`},
	},
	"nb": {
		quote:      []quoteMarker{nordicAttribution, {re: headerBlock(`fra`, `sendt|dato`)}},
		separators: []string{`opprinnelig melding`, `videresendt melding`},
	},
	"fi": {
		quote: []quoteMarker{
			{re: regexp.MustCompile(`(?i)\d{4}\s+klo\s+\d{1,2}[:.]\d{2}\s[^\n]{0,250}?\skirjoitti\s*:`), lineStart: true},
			{re: headerBlock(`lähettäjä`, `lähetetty|päivämäärä`)},
		},
		separators: []string{`alkuperäinen viesti`, `välitetty viesti`},
		ooo:        []string{"automaattinen vastaus", "poissa toimistolta"},
	},
	"pl": {
		quote: []quoteMarker{
			{re: regexp.MustCompile(`(?i)\d{4},?\s+o\s+\d{1,2}:\d{2}\s[^\n]{0,250}?\snapisał(\(a\))?\s*:`), lineStart: true},
			{re: attribution(`w dniu`, `(napisał(\(a\))?|pisze)\s*:`)},
			{re: headerBlock(`od`, `wysłano|data`)},
		},
		separators: []string{`oryginalna wiadomość`, `wiadomość oryginalna`, `wiadomość przekazana`},
		ooo:        []string{"odpowiedź automatyczna", "poza biurem"},
	},
	"cs": {
		quote: []quoteMarker{
			{re: regexp.MustCompile(`(?i)\d{4}\s+v\s+\d{1,2}:\d{2}\s+odesílatel\s`), lineStart: true},
			{re: headerBlock(`od`, `odesláno|datum`)},
		},
		separators: []string{`původní zpráva`, `přeposlaná zpráva`},
		ooo:        []string{"automatická odpověď", "mimo kancelář"},
	},
	"ro": {
		quote:      []quoteMarker{{re: attribution(`pe`, `a scris\s*:`)}, {re: headerBlock(`de la`, `trimis|data`)}},
		separators: []string{`mesaj original`, `mesaj redirecționat`},
		ooo:        []string{"răspuns automat", "în afara biroului"},
	},
	"hu": {
		quote: []quoteMarker{
			{re: regexp.MustCompile(`(?i)\sezt írta\s*\(időpont:`), lineStart: true},
			{re: regexp.MustCompile(`(?i)\skeltezéssel,[^\n]{0,250}?\sírta\s*:`), lineStart: true},
			{re: headerBlock(`feladó`, `elküldve|dátum`)},
		},
		separators: []string{`eredeti üzenet`, `továbbított üzenet`},
		ooo:        []string{"automatikus válasz", "házon kívül"},
	},
	"ru": {
		quote: []quoteMarker{
			{re: regexp.MustCompile(`(?i)\d{4}\s*г\.\s+в\s+\d{1,2}:\d{2},\s[^\n]{0,200}?<[^<>\n]*@[^<>\n]*>\s*:`), lineStart: true},
			cyrillicAttribution,
			{re: headerBlock(`от`, `отправлено|дата`)},
		},
		separators: []string{`исходное сообщение`, `пересылаемое сообщение`},
		ooo:        []string{"автоматический ответ", "автоответ", "вне офиса"},
	},
	"uk": {
		quote: []quoteMarker{
			{re: regexp.MustCompile(`(?i)\d{4}\s*р\.\s+о\s+\d{1,2}:\d{2}\s[^\n]{0,250}?\sпише\s*:`), lineStart: true},
			cyrillicAttribution,
			{re: headerBlock(`від`, `надіслано|дата`)},
		},
		separators: []string{`оригінальне повідомлення`, `переслане повідомлення`},
		ooo:        []string{"автоматична відповідь", "поза офісом"},
	},
	"el": {
		quote: []quoteMarker{
			{re: regexp.MustCompile(`(?i)(^|\s)στις\s[^\n]{0,250}?@[^\n]{0,80}?>\s*έγραψε\s*:`)},
			{re: headerBlock(`από`, `στάλθηκε|ημερομηνία`)},
		},
		separators: []string{`αρχικό μήνυμα`, `προωθημένο μήνυμα`},
		ooo:        []string{"αυτόματη απάντηση", "εκτός γραφείου"},
	},
	"tr": {
		quote: []quoteMarker{
			{re: regexp.MustCompile(`(?i)@[^\n]{0,80}?\d{1,2}:\d{2}\s+tarihinde\s+şunu\s+yazdı\s*:`), lineStart: true},
			{re: headerBlock(`kimden`, `gönderildi|gönderilme tarihi|tarih`)},
		},
		separators: []string{`özgün ileti`, `orijinal mesaj`, `iletilen ileti`},
		ooo:        []string{"otomatik yanıt", "otomatik cevap", "ofis dışında"},
	},
	"ja": {
		quote: []quoteMarker{
			{re: regexp.MustCompile(`\d{4}年\s?\d{1,2}月\s?\d{1,2}日\s*[(（][^)）\n]{1,4}[)）]\s*\d{1,2}:\d{2}\s[^\n]{0,200}?<[^<>\n]*@[^<>\n]*>\s*[:：]`), lineStart: true},
			{re: regexp.MustCompile(`(?s)差出人\s?[:：].{1,300}?\s(送信日時|日付)\s?[:：]`)},
		},
		separators: []string{`元のメッセージ`, `転送されたメッセージ`},
		ooo:        []string{"自動応答", "自動返信", "不在通知", "不在のお知らせ"},
	},
	"zh": {
		quote: []quoteMarker{
			{re: regexp.MustCompile(`[于於]\s?\d{4}年\s?\d{1,2}月\s?\d{1,2}日[^\n]{0,40}?[写寫]道\s?[:：]`), lineStart: true},
			{re: regexp.MustCompile(`(?s)(发件人|寄件者)\s?[:：].{1,300}?\s(发送时间|寄件日期|日期)\s?[:：]`)},
		},
		separators: []string{`原始邮件`, `原始郵件`, `转发的邮件`, `已轉寄郵件`},
		ooo:        []string{"自动回复", "自动答复", "自動回覆", "自動回复"},
	},
	"ko": {
		quote: []quoteMarker{
			{re: regexp.MustCompile(`\d{4}년\s*\d{1,2}월\s*\d{1,2}일[^\n]{0,150}?님이\s?작성\s*:`), lineStart: true},
			{re: regexp.MustCompile(`(?s)보낸\s?사람\s?:.{1,300}?\s보낸\s?날짜\s?:`)},
		},
		separators: []string{`원본 메시지`, `전달된 메시지`},
		ooo:        []string{"자동 회신", "자동 응답", "부재중"},
	},
	"id": {
		quote:      []quoteMarker{{re: attribution(`pada`, `menulis\s*:`)}, {re: headerBlock(`dari`, `dikirim|tanggal`)}},
		separators: []string{`pesan asli`, `pesan yang diteruskan`},
		ooo:        []string{"balasan otomatis", "di luar kantor"},
	},
	"vi": {
		quote:      []quoteMarker{{re: attribution(`vào`, `đã viết\s*:`)}, {re: headerBlock(`từ`, `đã gửi|ngày`)}},
		separators: []string{`tin nhắn gốc`, `thư được chuyển tiếp`},
		ooo:        []string{"trả lời tự động"},
	},
	"th": {
		quote: []quoteMarker{{re: attribution(`เมื่อ`, `เขียนว่า\s*:`)}},
		ooo:   []string{"ตอบกลับอัตโนมัติ"},
	},
	"hi": {
		quote: []quoteMarker{{re: regexp.MustCompile(`\d{4}\s+को[^\n]{0,200}?@[^\n]{0,80}?\sने\s+लिखा\s*:`), lineStart: true}},
		ooo:   []string{"स्वचालित उत्तर"},
	},
	"ar": {
		quote: []quoteMarker{
			{re: regexp.MustCompile(`(^|\s)في\s[^\n]{0,250}?\d{1,2}:\d{2}[^\n]{0,40}?\sكتب\s[^\n]{0,120}?@[^\n]{0,80}?>\s*:`)},
			{re: headerBlock(`من`, `تاريخ الإرسال|أرسل|التاريخ`)},
		},
		separators: []string{`الرسالة الأصلية`, `رسالة معاد توجيهها`},
		ooo:        []string{"رد تلقائي", "خارج المكتب"},
	},
	"he": {
		quote: []quoteMarker{
			{re: regexp.MustCompile(`בתאריך[^\n]{0,150}?\d{4}[^\n]{0,80}?\sמאת\s[^\n]{0,120}?@[^\n]{0,80}?>`), lineStart: true},
			{re: headerBlock(`מאת`, `נשלח|תאריך`)},
		},
		separators: []string{`הודעה מקורית`, `הודעה שהועברה`},
		ooo:        []string{"תשובה אוטומטית", "מחוץ למשרד"},
	},
	"bg": {
		quote:      []quoteMarker{gmailSigned(`написа`), {re: headerBlock(`от`, `изпратено|дата`)}},
		separators: []string{`оригинално съобщение`, `препратено съобщение`},
	},
	"bn": {quote: []quoteMarker{gmailSigned(`লিখেছেন`)}},
	"ca": {
		quote:      []quoteMarker{gmailSigned(`va escriure`), {re: headerBlock(`de`, `enviat( el)?|data`)}},
		separators: []string{`missatge original`, `missatge reenviat`},
	},
	"et": {
		quote:      []quoteMarker{gmailSigned(`kirjutas`), {re: headerBlock(`saatja`, `saadetud|kuupäev`)}},
		separators: []string{`algne sõnum`, `edasisaadetud sõnum`},
	},
	"fa":  {quote: []quoteMarker{gmailSigned(`نوشت`), {re: headerBlock(`از`, `ارسال شده|تاریخ`)}}},
	"fil": {quote: []quoteMarker{gmailSigned(`sumulat`)}},
	"hr": {
		quote:      []quoteMarker{gmailSigned(`(je\s+)?napisa(o|la)(/la)?(\s+je)?`), {re: headerBlock(`šalje|od`, `poslano|datum`)}},
		separators: []string{`izvorna poruka`, `proslijeđena poruka`},
	},
	"lt": {
		quote:      []quoteMarker{gmailSigned(`rašė`), {re: headerBlock(`nuo`, `išsiųsta|data`)}},
		separators: []string{`pradinis laiškas`, `persiųstas laiškas`},
	},
	"lv": {
		quote:      []quoteMarker{gmailSigned(`rakstīja`), {re: headerBlock(`no`, `nosūtīts|datums`)}},
		separators: []string{`sākotnējais ziņojums`, `pārsūtīts ziņojums`},
	},
	"ms": {
		quote:      []quoteMarker{gmailSigned(`menulis`), {re: headerBlock(`daripada`, `dihantar|tarikh`)}},
		separators: []string{`mesej asal`, `mesej dimajukan`},
	},
	"sk": {
		quote:      []quoteMarker{gmailSigned(`napísal(\(a\)|a)?`), {re: headerBlock(`od`, `odoslané|dátum`)}},
		separators: []string{`pôvodná správa`, `preposlaná správa`},
	},
	"sl": {
		quote:      []quoteMarker{gmailSigned(`(je\s+)?napisal(\(-?a\)|a)?`), {re: headerBlock(`od`, `poslano|datum`)}},
		separators: []string{`izvirno sporočilo`, `posredovano sporočilo`},
	},
	"sr": {
		quote: []quoteMarker{
			gmailSigned(`(је\s+|je\s+)?(написао|написала|napisao|napisala)(/ла|/la)?(\s+је|\s+je)?`),
			{re: headerBlock(`од|od`, `послато|послано|poslato|poslano|датум|datum`)},
		},
		separators: []string{`оригинална порука`, `originalna poruka`, `прослеђена порука`, `prosleđena poruka`},
	},
	"sw": {quote: []quoteMarker{gmailSigned(`aliandika`)}},
	"ta": {quote: []quoteMarker{gmailSigned(`எழுதியது`)}},
	"ur": {quote: []quoteMarker{gmailSigned(`نے لکھا`)}},
}

// LanguagesWithRules lists the codes that add offline vocabulary, sorted.
func LanguagesWithRules() []string {
	out := make([]string, 0, len(languageRules))
	for code := range languageRules {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

// ruleSet is the compiled vocabulary for one combination of languages.
type ruleSet struct {
	quote     []quoteMarker
	ooo       []string
	cue       *regexp.Regexp
	inclusive map[string]bool
	months    map[string]int
	bounce    []string
	declined  []phrase
	negative  []phrase
}

// phrase is a sentiment phrase as compiled. The English base set keeps the
// substring match it always had; a chosen language's phrase must stand as
// whole words, so "no gracias" never matches inside "bueno gracias".
type phrase struct {
	text    string
	bounded bool
}

// in matches an English base phrase against the text as written and a chosen
// language's phrase, whole words only, against its accent-folded form.
func (p phrase) in(text, folded string) bool {
	if !p.bounded {
		return strings.Contains(text, p.text)
	}
	text = folded
	for i := 0; i < len(text); {
		j := strings.Index(text[i:], p.text)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(p.text)
		before, _ := utf8.DecodeLastRuneInString(text[:start])
		after, _ := utf8.DecodeRuneInString(text[end:])
		if !wordRune(before) && !wordRune(after) {
			return true
		}
		_, size := utf8.DecodeRuneInString(text[start:])
		i = start + size
	}
	return false
}

func wordRune(r rune) bool {
	return r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

var ruleSets sync.Map // sorted codes joined by "," -> *ruleSet

// rulesFor is the base vocabulary plus the given languages'. Unknown codes,
// and codes that add nothing, share the base set.
func rulesFor(langs []string) *ruleSet {
	var codes []string
	for _, l := range langs {
		l = strings.ToLower(strings.TrimSpace(l))
		if _, ok := languageRules[l]; ok && !slices.Contains(codes, l) {
			codes = append(codes, l)
		}
	}
	sort.Strings(codes)
	key := strings.Join(codes, ",")
	if rs, ok := ruleSets.Load(key); ok {
		return rs.(*ruleSet)
	}
	parts := []langRules{baseRules}
	for _, c := range codes {
		parts = append(parts, languageRules[c])
	}
	rs := compileRules(parts)
	actual, _ := ruleSets.LoadOrStore(key, rs)
	return actual.(*ruleSet)
}

func compileRules(parts []langRules) *ruleSet {
	rs := &ruleSet{inclusive: map[string]bool{}, months: map[string]int{}}
	var separators, cues []string
	for i, p := range parts {
		rs.quote = append(rs.quote, p.quote...)
		separators = append(separators, p.separators...)
		for _, m := range p.ooo {
			rs.ooo = append(rs.ooo, foldAccents(m))
		}
		cues = append(cues, p.cues...)
		for _, c := range p.inclusive {
			rs.inclusive[c] = true
		}
		for month, names := range p.months {
			for _, n := range names {
				rs.months[n] = month
			}
		}
		rs.bounce = append(rs.bounce, p.bounce...)
		// parts[0] is always baseRules.
		for _, d := range p.declined {
			rs.declined = append(rs.declined, phrase{text: accentFolder.Replace(strings.ToLower(d)), bounded: i > 0})
		}
		for _, n := range p.negative {
			rs.negative = append(rs.negative, phrase{text: accentFolder.Replace(strings.ToLower(n)), bounded: i > 0})
		}
	}
	rs.quote = append(rs.quote, quoteMarker{re: regexp.MustCompile(`(?i)-{2,}\s*(` + strings.Join(separators, `|`) + `)\s*-{2,}`)})
	rs.cue = regexp.MustCompile(`(?i)\b(` + strings.Join(longestFirst(cues), `|`) + `)\b`)
	return rs
}

// longestFirst orders alternatives so a longer cue wins over a cue it starts
// with: "till och med" is inclusive, "till" is not.
func longestFirst(in []string) []string {
	out := append([]string(nil), in...)
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

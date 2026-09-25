package replyclassify

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
)

// The offline layers read every reply with the base vocabulary below, and a
// workspace's tagging languages (models.MailLanguageNames codes) add theirs.
// A language nobody chose adds no pattern, so it cannot misread anyone's mail.

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
}

var baseRules = langRules{
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
		`zurueck am`, `zurueck ab`, `wieder am`, `wieder ab`, `ab dem`, `ab montag den`,
		`wieder erreichbar am`, `wieder erreichbar ab`, `wieder im buero am`,
		`bis einschliesslich`, `bis zum`, `bis`,
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

var languageRules = map[string]langRules{
	"de": {
		quote:      []quoteMarker{{re: headerBlock(`von`, `gesendet|datum`)}},
		separators: []string{`ursprüngliche nachricht`, `weitergeleitete nachricht`},
	},
	"fr": {
		quote:      []quoteMarker{{re: headerBlock(`de ?`, `envoyé ?|date ?`)}},
		separators: []string{`message d'origine`, `message transféré`},
		ooo:        []string{"absent du bureau", "absente du bureau"},
	},
	"es": {
		quote:      []quoteMarker{{re: headerBlock(`de`, `enviado|fecha`)}},
		separators: []string{`mensaje original`, `mensaje reenviado`},
		ooo:        []string{"fuera de la oficina"},
		bounce:     []string{"no se puede entregar"},
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
	for _, p := range parts {
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

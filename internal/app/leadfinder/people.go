package leadfinder

import (
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/warmbly/warmbly/internal/models"
)

const (
	peoplePerSite = 25
	segmentMax    = 400
	segmentsMax   = 4000
)

// FoundPerson is someone a business's website names.
type FoundPerson struct {
	Name      string
	First     string
	Last      string
	Title     string
	Seniority models.LeadSeniority
	Email     string
	LinkedIn  string
	Phone     string
	Source    models.LeadPersonSource
	URL       string
}

// --- names -----------------------------------------------------------------

type personName struct {
	full, first, last, suffix string
}

var (
	nameToken     = regexp.MustCompile(`^\p{Lu}[\p{L}'’\-]*\.?$`)
	initialToken  = regexp.MustCompile(`^\p{Lu}\.?$`)
	nameSplitters = regexp.MustCompile(`\s*(?:,|\s[–—|·•]\s|\s-\s)\s*`)
)

// honorifics come before a name and are not part of it.
var honorifics = map[string]bool{
	"dr": true, "dr.": true, "mr": true, "mr.": true, "mrs": true, "mrs.": true, "ms": true, "ms.": true, "miss": true,
	"prof": true, "prof.": true, "herr": true, "frau": true, "mag.": true, "ing.": true, "dipl.": true, "dipl.-ing.": true,
	"med.": true, "dent.": true, "vet.": true, "rer.": true, "nat.": true, "sr.": true, "sra.": true, "mme": true, "m.": true,
	"dott.": true, "dott.ssa": true, "avv.": true, "rev.": true, "hon.": true, "fza": true, "za": true, "zä": true,
	"mudr.": true, "mudr": true, "drs.": true, "dipl.-stom.": true, "m.sc.": true, "msc": true, "univ.-prof.": true, "priv.-doz.": true,
}

// credentials follow a name and read as part of the title.
var credentials = map[string]bool{
	"dds": true, "dmd": true, "md": true, "do": true, "dvm": true, "phd": true, "esq": true, "cpa": true, "jd": true,
	"rn": true, "np": true, "pa": true, "pa-c": true, "od": true, "dc": true, "pt": true, "dpt": true, "mba": true,
	"ms": true, "msc": true, "bsc": true, "ma": true, "ba": true, "rdh": true, "lmt": true, "cfp": true, "pe": true,
	"jr": true, "sr": true, "ii": true, "iii": true, "iv": true, "ll.m": true, "llm": true, "faia": true, "aia": true,
}

// particles sit lowercase inside a surname.
var particles = map[string]bool{
	"van": true, "von": true, "de": true, "da": true, "del": true, "der": true, "den": true, "di": true, "la": true,
	"le": true, "du": true, "ten": true, "ter": true, "bin": true, "al": true, "dos": true, "das": true, "y": true,
}

// notNameWords are words that start with a capital on a website but are never
// part of a person's name: navigation, services, places and roles.
var notNameWords = toSet(`about our team contact us home services service book now read more learn view all meet the and
privacy policy terms cookie cookies menu gallery blog news shop cart call today get free quote online appointment
appointments request hours location locations directions map email phone follow facebook instagram twitter linkedin
youtube tiktok reviews review testimonials careers jobs welcome family dental dentistry clinic center centre care health
medical group inc llc ltd gmbh co company street avenue road suite floor monday tuesday wednesday thursday friday
saturday sunday january february march april may june july august september october november december new patients
patient insurance payment financing specials offers emergency cosmetic general orthodontics implants whitening
cleaning exams pediatric restaurant bar grill cafe coffee pizza kitchen lunch dinner breakfast brunch catering events
event private party gift cards order delivery pickup takeout salon spa hair nails beauty fitness gym studio yoga
classes schedule pricing plans membership law firm attorneys lawyers practice areas real estate homes sale rent buy
sell agents office offices main downtown north south east west city county state united states america copyright
rights reserved powered website design designed sitemap accessibility statement skip content search login sign
account click here back top next previous page submit send message name first last your you we my who what why how
where when mission vision values history story approach philosophy leadership staff board directors founders
management partners associates doctors providers hygienists members owner founder director manager ceo president
chief officer partner principal dentist doctor attorney lawyer agent broker chef stylist therapist trainer coach
specialist assistant receptionist coordinator administrator technician nurse consultant advisor engineer designer
see open closed holiday holidays special wine beer drinks cocktails tickets visit visitors shipping returns faq faqs
help support resources library portal download downloads video videos photo photos community sponsors donate
volunteer subscribe newsletter latest featured popular best top quality professional professionals expert experts
certified licensed insured trusted local award winning years experience dds dmd md phd esq cpa inhaber
geschäftsführer impressum datenschutz kontakt startseite über uns leistungen termin praxis kanzlei zum zur profil
mehr weiter lesen jetzt hier unser unsere ihr ihre alle neu anrufen buchen online-termin zahnarzt zahnärztin
google ads maps analytics consent pixel hotjar matomo recaptcha cloudflare vimeo wordpress shopify wix squarespace
nuestro nuestra equipo ver más leer notre équipe voir plus lire nostro chi siamo scopri`)

func toSet(words string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(words) {
		out[w] = true
	}
	return out
}

// parseName reads a person's name out of a short piece of text. It is strict:
// a page holds far more capitalised phrases than names, and a wrong person is
// worse than a missed one.
func parseName(raw string) (personName, bool) {
	s := strings.Join(strings.Fields(strings.Trim(raw, " \t\r\n.,;:!?()[]\"'“”‘’*•|")), " ")
	if s == "" || utf8.RuneCountInString(s) > 60 || strings.ContainsAny(s, "@/\\:0123456789&+=#%_<>") {
		return personName{}, false
	}
	var suffix []string
	if i := strings.IndexByte(s, ','); i >= 0 {
		for _, c := range strings.Split(s[i+1:], ",") {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			if !credentials[strings.ToLower(strings.Trim(c, "."))] {
				return personName{}, false
			}
			suffix = append(suffix, c)
		}
		s = strings.TrimSpace(s[:i])
	}
	tokens := strings.Fields(s)
	for len(tokens) > 0 && honorifics[strings.ToLower(tokens[0])] {
		tokens = tokens[1:]
	}
	for len(tokens) > 0 && credentials[strings.ToLower(strings.Trim(tokens[len(tokens)-1], "."))] {
		suffix = append([]string{tokens[len(tokens)-1]}, suffix...)
		tokens = tokens[:len(tokens)-1]
	}
	if len(tokens) < 2 || len(tokens) > 4 {
		return personName{}, false
	}
	allCaps := true
	for _, t := range tokens {
		if strings.ToUpper(t) != t {
			allCaps = false
		}
	}
	for i, t := range tokens {
		lower := strings.ToLower(t)
		if notNameWords[strings.Trim(lower, ".")] {
			return personName{}, false
		}
		if allCaps {
			t = titleWord(t)
			tokens[i] = t
		}
		inner := i > 0 && i < len(tokens)-1
		switch {
		case inner && particles[lower]:
		case inner && initialToken.MatchString(t):
		case nameToken.MatchString(t) && utf8.RuneCountInString(strings.Trim(t, ".")) >= 2 && !strings.HasSuffix(t, "."):
		default:
			return personName{}, false
		}
	}
	first := tokens[0]
	last := tokens[len(tokens)-1]
	if len(tokens) >= 3 && particles[strings.ToLower(tokens[len(tokens)-2])] {
		last = tokens[len(tokens)-2] + " " + last
	}
	return personName{full: strings.Join(tokens, " "), first: first, last: last, suffix: strings.Join(suffix, ", ")}, true
}

func titleWord(w string) string {
	var b strings.Builder
	up := true
	for _, r := range strings.ToLower(w) {
		if up {
			b.WriteRune(unicode.ToUpper(r))
		} else {
			b.WriteRune(r)
		}
		up = r == '-' || r == '\'' || r == '’'
	}
	return b.String()
}

// looseName splits a name someone typed, without the website checks.
func looseName(first, last, full string) personName {
	first, last = strings.TrimSpace(first), strings.TrimSpace(last)
	if first == "" && last == "" {
		tokens := strings.Fields(full)
		for len(tokens) > 1 && honorifics[strings.ToLower(tokens[0])] {
			tokens = tokens[1:]
		}
		switch len(tokens) {
		case 0:
		case 1:
			first = tokens[0]
		default:
			first, last = tokens[0], tokens[len(tokens)-1]
		}
	}
	name := strings.Join(strings.Fields(full), " ")
	if name == "" {
		name = strings.TrimSpace(first + " " + last)
	}
	return personName{full: name, first: first, last: last}
}

var foldMarks = runes.Remove(runes.In(unicode.Mn))

var foldExtra = strings.NewReplacer("ß", "ss", "ø", "o", "æ", "ae", "œ", "oe", "ł", "l", "đ", "d", "þ", "th")

// asciiFold lowercases and strips accents, so "José Núñez" reads "jose nunez".
func asciiFold(s string) string {
	out, _, err := transform.String(transform.Chain(norm.NFD, foldMarks, norm.NFC), strings.ToLower(s))
	if err != nil {
		out = strings.ToLower(s)
	}
	return foldExtra.Replace(out)
}

// nameKey is how two mentions of the same person are matched.
func nameKey(first, last, full string) string {
	if first != "" && last != "" {
		return asciiFold(first) + " " + asciiFold(last)
	}
	return asciiFold(strings.Join(strings.Fields(full), " "))
}

// localPart keeps only the letters an address can carry.
func localPart(s string) string {
	var b strings.Builder
	for _, r := range asciiFold(s) {
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// --- roles -----------------------------------------------------------------

var roleWords = regexp.MustCompile(`(?i)\b(owner|co-?owner|founder|co-?founder|ceo|cfo|coo|cto|cmo|chief|president|vice[- ]president|vp|chair(?:man|woman|person)?|managing director|director|manager|head of|partner|principal|proprietor|executive|supervisor|coordinator|administrator|officer|dentist|orthodontist|hygienist|physician|surgeon|doctor|veterinarian|optometrist|pharmacist|nurse|therapist|chiropractor|attorney|lawyer|counsel|paralegal|accountant|bookkeeper|agent|broker|realtor|chef|sommelier|stylist|barber|trainer|coach|instructor|consultant|advisor|adviser|engineer|designer|architect|developer|editor|technician|specialist|practitioner|associate|receptionist|assistant|team lead|inhaber(?:in)?|geschäftsführer(?:in)?|gründer(?:in)?|leiter(?:in)?|propietari[oa]|gerente|fundador[a]?|soci[oa]|fondat(?:eur|rice|ore)|gérante?|directeur|directrice|titolare|dds|dmd|dvm|esq|cpa)\b`)

// roleText reports whether a short piece of text reads as someone's job title.
func roleText(s string) bool {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	return n >= 2 && n <= 80 && roleWords.MatchString(s) && !strings.ContainsAny(s, "@") && !looksLikeLink(s) && strings.Count(s, " ") <= 9
}

func looksLikeLink(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "://") || strings.Contains(l, "www.") || strings.Contains(l, ".com/") || strings.Contains(l, ".html")
}

var (
	ownerTitle     = regexp.MustCompile(`(?i)\b(owner|co-?owner|founder|co-?founder|proprietor|inhaber(?:in)?|propietari[oa]|titolare|titular|fondat(?:eur|rice|ore)|gérante?|gerant|managing partner|principal|gründer(?:in)?)\b`)
	executiveTitle = regexp.MustCompile(`(?i)\b(ceo|cfo|coo|cto|cmo|cio|chief|president|vice[- ]president|vp|managing director|geschäftsführer(?:in)?|director general|chair(?:man|woman|person)?|partner|executive director)\b`)
	managerTitle   = regexp.MustCompile(`(?i)\b(manager|director|head of|head|lead|supervisor|coordinator|gerente|leiter(?:in)?|leitung|praxismanager(?:in)?|directeur|directrice|responsabile|jefe|jefa)\b`)
)

// seniorityOf places a title.
func seniorityOf(title string) models.LeadSeniority {
	switch {
	case ownerTitle.MatchString(title):
		return models.LeadSeniorityOwner
	case executiveTitle.MatchString(title):
		return models.LeadSeniorityExecutive
	case managerTitle.MatchString(title):
		return models.LeadSeniorityManager
	}
	return models.LeadSeniorityStaff
}

func seniorityRank(s models.LeadSeniority) int {
	switch s {
	case models.LeadSeniorityOwner:
		return 0
	case models.LeadSeniorityExecutive:
		return 1
	case models.LeadSeniorityManager:
		return 2
	}
	return 3
}

func cleanTitle(s string) string {
	s = strings.Join(strings.Fields(strings.Trim(s, " \t\r\n.,;:|-–—•·()[]")), " ")
	if utf8.RuneCountInString(s) > 80 || looksLikeLink(s) {
		return ""
	}
	return s
}

// --- extraction ------------------------------------------------------------

// imprintLine is a legal notice naming who runs the business, which German,
// Austrian, Swiss and several other jurisdictions require on every site.
var imprintLine = regexp.MustCompile(`(?i)(geschäftsführer(?:in)?|geschäftsführung|inhaber(?:in)?|vertreten durch|vertretungsberechtigt(?:e|er)?(?: geschäftsführer(?:in)?)?|managing directors?|owner|proprietor|propietari[oa]|titular|gérante?|directeur de (?:la )?publication|responsable de (?:la )?publication|amministratore|titolare)\s*[:：]\s*(.{4,80})`)

func newPerson(n personName, title string, src models.LeadPersonSource, u string) FoundPerson {
	title = cleanTitle(title)
	if title == "" && n.suffix != "" {
		title = n.suffix
	}
	return FoundPerson{Name: n.full, First: n.first, Last: n.last, Title: title, Seniority: seniorityOf(title), Source: src, URL: u}
}

// extractPeople reads the people one page names.
func extractPeople(p *page) []FoundPerson {
	u := ""
	if p.url != nil {
		u = p.url.String()
	}
	var out []FoundPerson
	out = append(out, schemaPeople(p.jsonld, u)...)
	imprintPage := imprintURL(u)
	for _, seg := range p.segments {
		for _, m := range imprintLine.FindAllStringSubmatch(seg, 3) {
			label := strings.ToLower(m[1])
			legal := imprintPage || strings.Contains(label, "geschäftsf") || strings.Contains(label, "inhaber") || strings.Contains(label, "vertret")
			for _, part := range strings.FieldsFunc(strings.ReplaceAll(m[2], " und ", ","), func(r rune) bool { return r == ',' || r == ';' || r == '|' }) {
				if n, ok := leadingName(strings.Fields(part)); ok {
					src := models.LeadPersonPage
					if legal {
						src = models.LeadPersonImprint
					}
					out = append(out, newPerson(n, imprintTitle(label), src, u))
				}
			}
		}
	}
	// A name with its role next to it, on one line or in neighbouring elements.
	for i, seg := range p.segments {
		if n, role, ok := nameWithRole(seg); ok {
			out = append(out, newPerson(n, role, models.LeadPersonPage, u))
			continue
		}
		n, ok := parseName(seg)
		if !ok {
			continue
		}
		roleAt := func(j int) bool {
			return j >= 0 && j < len(p.segments) && roleText(p.segments[j]) && !namesSomeone(p.segments[j])
		}
		switch {
		case roleAt(i + 1):
			out = append(out, newPerson(n, p.segments[i+1], models.LeadPersonPage, u))
		case i+1 < len(p.segments) && utf8.RuneCountInString(p.segments[i+1]) <= 3 && roleAt(i+2):
			out = append(out, newPerson(n, p.segments[i+2], models.LeadPersonPage, u))
		case roleAt(i - 1):
			out = append(out, newPerson(n, p.segments[i-1], models.LeadPersonPage, u))
		case n.suffix != "":
			out = append(out, newPerson(n, "", models.LeadPersonPage, u))
		}
	}
	for _, l := range p.links {
		lower := strings.ToLower(l.href)
		switch {
		case strings.HasPrefix(lower, "mailto:"):
			if n, ok := parseName(l.text); ok {
				fp := newPerson(n, "", models.LeadPersonEmail, u)
				fp.Email = acceptEmail(l.href)
				if fp.Email != "" {
					out = append(out, fp)
				}
			}
		case strings.Contains(lower, "linkedin.com/in/"):
			if fp, ok := linkedInPerson(l, p.url); ok {
				fp.URL = u
				out = append(out, fp)
			}
		}
	}
	return out
}

// nameWithRole reads "Jane Doe, Owner" or "Jane Doe – Owner" from one line. A
// line that only adds credentials ("Jane Doe, DDS") is a name, not a role.
func nameWithRole(seg string) (personName, string, bool) {
	parts := nameSplitters.Split(seg, 3)
	if len(parts) < 2 {
		return personName{}, "", false
	}
	n, ok := parseName(parts[0])
	role := strings.Join(parts[1:], ", ")
	if !ok || !roleText(role) || onlyCredentials(role) {
		return personName{}, "", false
	}
	return n, role, true
}

func onlyCredentials(s string) bool {
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		if !credentials[strings.ToLower(strings.Trim(w, "."))] {
			return false
		}
	}
	return true
}

// namesSomeone reports whether a line is, or starts with, a person's name, so
// it is not read as the role of the name before it.
func namesSomeone(s string) bool {
	if _, ok := parseName(s); ok {
		return true
	}
	_, _, ok := nameWithRole(s)
	return ok
}

// leadingName takes the longest name at the start of words: "Max Muster
// Registergericht ..." reads "Max Muster".
func leadingName(words []string) (personName, bool) {
	start := 0
	for start < len(words) && honorifics[strings.ToLower(words[start])] {
		start++
	}
	for end := min(len(words), start+4); end >= start+2; end-- {
		if n, ok := parseName(strings.Join(words[start:end], " ")); ok {
			return n, true
		}
	}
	return personName{}, false
}

func imprintTitle(label string) string {
	switch {
	case strings.Contains(label, "inhaber"), strings.Contains(label, "owner"), strings.Contains(label, "proprietor"),
		strings.Contains(label, "propietari"), strings.Contains(label, "titolare"), strings.Contains(label, "titular"):
		return "Owner"
	case strings.Contains(label, "publication"):
		return "Publisher"
	}
	return "Managing director"
}

// linkedInPerson reads a profile link: the anchor text when it is a name,
// else the profile's slug ("/in/jane-doe-4a1b2c").
func linkedInPerson(l link, base *url.URL) (FoundPerson, bool) {
	u, err := url.Parse(l.href)
	if err != nil {
		return FoundPerson{}, false
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "in" || parts[1] == "" {
		return FoundPerson{}, false
	}
	profile := "https://www.linkedin.com/in/" + parts[1]
	if n, ok := parseName(l.text); ok {
		fp := newPerson(n, "", models.LeadPersonLinkedIn, "")
		fp.LinkedIn = profile
		return fp, true
	}
	slug, _ := url.PathUnescape(parts[1])
	var words []string
	for _, w := range strings.Split(slug, "-") {
		if w == "" || strings.ContainsAny(w, "0123456789") {
			break
		}
		words = append(words, titleWord(w))
	}
	if len(words) < 2 || len(words) > 3 {
		return FoundPerson{}, false
	}
	n, ok := parseName(strings.Join(words, " "))
	if !ok {
		return FoundPerson{}, false
	}
	fp := newPerson(n, "", models.LeadPersonLinkedIn, "")
	fp.LinkedIn = profile
	return fp, true
}

// schemaPeople reads schema.org Person entries out of a page's JSON-LD.
func schemaPeople(blocks []string, u string) []FoundPerson {
	var out []FoundPerson
	visited := 0
	var walk func(v any, depth int)
	walk = func(v any, depth int) {
		if depth > 8 || visited > 400 {
			return
		}
		visited++
		switch x := v.(type) {
		case []any:
			for _, e := range x {
				walk(e, depth+1)
			}
		case map[string]any:
			if isSchemaType(x["@type"], "Person") {
				if fp, ok := schemaPerson(x, u); ok {
					out = append(out, fp)
				}
			}
			for k, e := range x {
				if k != "@context" {
					walk(e, depth+1)
				}
			}
		}
	}
	for _, b := range blocks {
		var v any
		if json.Unmarshal([]byte(strings.TrimSpace(b)), &v) == nil {
			walk(v, 0)
		}
	}
	return out
}

func isSchemaType(t any, want string) bool {
	switch x := t.(type) {
	case string:
		return strings.EqualFold(strings.TrimPrefix(x, "schema:"), want)
	case []any:
		for _, e := range x {
			if isSchemaType(e, want) {
				return true
			}
		}
	}
	return false
}

func schemaString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case []any:
		for _, e := range x {
			if s := schemaString(e); s != "" {
				return s
			}
		}
	case map[string]any:
		return schemaString(x["name"])
	}
	return ""
}

func schemaPerson(x map[string]any, u string) (FoundPerson, bool) {
	given, family := schemaString(x["givenName"]), schemaString(x["familyName"])
	full := schemaString(x["name"])
	if full == "" {
		full = strings.TrimSpace(given + " " + family)
	}
	n, ok := parseName(full)
	if !ok {
		if given == "" || family == "" || strings.ContainsAny(full, "@0123456789") || utf8.RuneCountInString(full) > 60 {
			return FoundPerson{}, false
		}
		n = personName{full: full, first: given, last: family}
	}
	fp := newPerson(n, schemaString(x["jobTitle"]), models.LeadPersonSchema, u)
	fp.Email = acceptEmail(schemaString(x["email"]))
	if tel := telDigits.ReplaceAllString(schemaString(x["telephone"]), ""); len(tel) >= 7 {
		fp.Phone = tel
	}
	var links []string
	switch s := x["sameAs"].(type) {
	case string:
		links = []string{s}
	case []any:
		for _, e := range s {
			if v, ok := e.(string); ok {
				links = append(links, v)
			}
		}
	}
	for _, l := range links {
		if strings.Contains(strings.ToLower(l), "linkedin.com/in/") {
			fp.LinkedIn = strings.TrimSpace(l)
			break
		}
	}
	return fp, true
}

// sourceRank orders sources from most to least trustworthy.
func sourceRank(s models.LeadPersonSource) int {
	switch s {
	case models.LeadPersonManual:
		return 0
	case models.LeadPersonSchema:
		return 1
	case models.LeadPersonImprint:
		return 2
	case models.LeadPersonPage:
		return 3
	case models.LeadPersonEmail:
		return 4
	}
	return 5
}

// mergePeople folds several mentions of one person into one, keeping the best
// source and every detail any of them carried.
func mergePeople(found []FoundPerson) []FoundPerson {
	byKey := map[string]int{}
	var out []FoundPerson
	for _, f := range found {
		key := nameKey(f.First, f.Last, f.Name)
		if key == "" {
			continue
		}
		i, ok := byKey[key]
		if !ok {
			byKey[key] = len(out)
			out = append(out, f)
			continue
		}
		cur := &out[i]
		if sourceRank(f.Source) < sourceRank(cur.Source) {
			cur.Source, cur.URL, cur.Name = f.Source, f.URL, f.Name
		}
		if cur.Title == "" && f.Title != "" {
			cur.Title, cur.Seniority = f.Title, f.Seniority
		}
		if cur.Email == "" {
			cur.Email = f.Email
		}
		if cur.LinkedIn == "" {
			cur.LinkedIn = f.LinkedIn
		}
		if cur.Phone == "" {
			cur.Phone = f.Phone
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := seniorityRank(out[i].Seniority), seniorityRank(out[j].Seniority); a != b {
			return a < b
		}
		return sourceRank(out[i].Source) < sourceRank(out[j].Source)
	})
	if len(out) > peoplePerSite {
		out = out[:peoplePerSite]
	}
	return out
}

// --- address patterns ------------------------------------------------------

// emailPattern builds a person's address from their name the way one domain does.
type emailPattern struct {
	key   string
	build func(f, l string) string
}

// emailPatterns are the shapes businesses use, most common first. A pattern
// that needs a last name returns "" without one.
var emailPatterns = []emailPattern{
	{"first.last", func(f, l string) string { return join(f, ".", l) }},
	{"first", func(f, _ string) string { return f }},
	{"flast", func(f, l string) string { return join(initial(f), "", l) }},
	{"firstlast", func(f, l string) string { return join(f, "", l) }},
	{"f.last", func(f, l string) string { return join(initial(f), ".", l) }},
	{"last", func(_, l string) string { return l }},
	{"first_last", func(f, l string) string { return join(f, "_", l) }},
	{"first-last", func(f, l string) string { return join(f, "-", l) }},
	{"last.first", func(f, l string) string { return join(l, ".", f) }},
	{"lastf", func(f, l string) string { return join(l, "", initial(f)) }},
	{"firstl", func(f, l string) string { return join(f, "", initial(l)) }},
	{"first.l", func(f, l string) string { return join(f, ".", initial(l)) }},
}

// probeOrder is how many of the common patterns an unknown domain is tried with.
const probeOrder = 6

func initial(s string) string {
	if s == "" {
		return ""
	}
	return s[:1]
}

func join(a, sep, b string) string {
	if a == "" || b == "" {
		return ""
	}
	return a + sep + b
}

func patternByKey(key string) *emailPattern {
	for i := range emailPatterns {
		if emailPatterns[i].key == key {
			return &emailPatterns[i]
		}
	}
	return nil
}

// candidates lists the addresses to try for a name at domain: the domain's
// known pattern first, then the common ones.
func candidates(pattern, first, last, domain string, limit int) []string {
	f, l := localPart(first), localPart(last)
	if f == "" || domain == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(p *emailPattern) {
		if p == nil || len(out) >= limit {
			return
		}
		if local := p.build(f, l); local != "" && !seen[local] {
			seen[local] = true
			out = append(out, local+"@"+domain)
		}
	}
	add(patternByKey(pattern))
	for i := range emailPatterns[:probeOrder] {
		add(&emailPatterns[i])
	}
	return out
}

// patternOf names the pattern that turns this person's name into local, or "".
func patternOf(local, first, last string) string {
	f, l := localPart(first), localPart(last)
	if f == "" {
		return ""
	}
	for _, p := range emailPatterns {
		if v := p.build(f, l); v != "" && v == local {
			return p.key
		}
	}
	return ""
}

// separatedLocal splits "jane.doe" style locals into a first and last name.
var separatedLocal = regexp.MustCompile(`^([a-z]{2,20})[._-]([a-z]{2,24})$`)

// attachEmails gives people the personal addresses the site published, adds
// the people only an address names, and learns the domain's pattern from them.
func attachEmails(people []FoundPerson, emails []FoundEmail, domain string) ([]FoundPerson, string) {
	votes := map[string]int{}
	byLocal := map[string]FoundEmail{}
	for _, e := range emails {
		at := strings.LastIndexByte(e.Email, '@')
		if at < 0 || registrable(e.Email[at+1:]) != domain {
			continue
		}
		byLocal[e.Email[:at]] = e
	}
	locals := make([]string, 0, len(byLocal))
	for local := range byLocal {
		locals = append(locals, local)
	}
	sort.Strings(locals)
	taken := map[string]bool{}
	for i := range people {
		p := &people[i]
		if p.Email != "" {
			taken[p.Email] = true
			if at := strings.LastIndexByte(p.Email, '@'); at > 0 && registrable(p.Email[at+1:]) == domain {
				if k := patternOf(p.Email[:at], p.First, p.Last); k != "" {
					votes[k]++
				}
			}
			continue
		}
		for _, local := range locals {
			e := byLocal[local]
			if taken[e.Email] {
				continue
			}
			// "first" or "last" alone matches too loosely to name the owner.
			if k := patternOf(local, p.First, p.Last); k != "" && k != "first" && k != "last" {
				p.Email = e.Email
				taken[e.Email] = true
				votes[k]++
				break
			}
		}
	}
	for _, local := range locals {
		e := byLocal[local]
		if taken[e.Email] || genericLocals[local] {
			continue
		}
		m := separatedLocal.FindStringSubmatch(local)
		if m == nil || notNameWords[m[1]] || notNameWords[m[2]] {
			continue
		}
		n := personName{first: titleWord(m[1]), last: titleWord(m[2])}
		n.full = n.first + " " + n.last
		fp := newPerson(n, "", models.LeadPersonEmail, e.URL)
		fp.Email = e.Email
		people = append(people, fp)
		votes[patternOf(local, n.first, n.last)]++
	}
	best, top := "", 0
	for k, v := range votes {
		if k != "" && (v > top || (v == top && k < best)) {
			best, top = k, v
		}
	}
	return people, best
}

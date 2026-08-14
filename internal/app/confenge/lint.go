package confenge

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const NearDupThreshold = 0.72
const NgramSize = 3

const (
	PreferMinEmailWords = 70
	PreferMaxEmailWords = 140
	MaxEmailParagraphs  = 5
	MaxBulletLines      = 3
	MaxCompanyNameHits  = 3
	MaxWhatsAppWords    = 70
)

var antiTemplatePhrases = []string{
	"identificamos uma oportunidade", "identificamos a oportunidade",
	"oportunidade unica", "oportunidade única", "solucao inovadora", "solução inovadora",
	"melhor do mercado", "lider absoluto", "líder absoluto",
	"revolucionar o seu", "revolucionar seu", "sem compromisso e sem custo",
	"somos especialistas em ajudar empresas como a sua", "como empresa lider", "como empresa líder",
	"espero que esteja bem", "tudo bem por ai", "tudo bem por aí",
	"passando para compartilhar", "gostaria de agendar uma call de 15 minutos",
	"sinergia entre nossas empresas", "alavancar resultados", "potencial inexplorado",
}

var inventedUrgencyPhrases = []string{
	"ultimas vagas", "últimas vagas", "apenas hoje", "somente hoje",
	"nao perca essa chance", "não perca essa chance", "urgente:",
	"corresponde a uma janela que fecha", "prazo final amanha", "prazo final amanhã", "restam poucas horas",
}

var financialPromisePhrases = []string{
	"economia garantida", "retorno financeiro garantido", "voce vai receber", "você vai receber",
	"milhoes a recuperar", "milhões a recuperar", "lucro assegurado", "roi garantido",
}

var hypothesisAsFactPhrases = []string{
	"voces nao tem equipe", "vocês não têm equipe", "voces nao possuem equipe", "vocês não possuem equipe",
	"sei que voces nao tem", "sei que vocês não têm", "sei que nao tem equipe", "sei que não tem equipe",
	"falta de equipe interna", "nao possuem estrutura interna", "não possuem estrutura interna",
	"equipe insuficiente para", "nao ha time de", "não há time de", "voces nao controlam", "vocês não controlam",
}

var genericSubjects = []string{
	"oportunidade", "parceria", "proposta comercial", "proposta de parceria",
	"apresentacao", "apresentação", "contato comercial", "follow up", "follow-up", "ola", "olá", "oi",
}

type LintResult struct {
	OK       bool
	Errors   []string
	Warnings []string
	Flags    []string
}

func LintCopy(channel, subject, body, companyName string) LintResult {
	res := LintResult{OK: true}
	subj := strings.TrimSpace(subject)
	body = strings.TrimSpace(body)
	blob := strings.ToLower(subj + "\n" + body)
	isWA := IsWhatsAppChannel(channel)
	isEmail := IsEmailChannel(channel)

	if emDashRe.MatchString(subj + body) {
		res.OK = false
		res.Errors = append(res.Errors, "em dash / en dash not allowed in outreach copy")
	}
	for _, p := range antiTemplatePhrases {
		if strings.Contains(blob, p) {
			res.OK = false
			res.Errors = append(res.Errors, "anti-template phrase: "+p)
		}
	}
	for _, p := range inventedUrgencyPhrases {
		if strings.Contains(blob, p) {
			res.OK = false
			res.Errors = append(res.Errors, "invented urgency: "+p)
		}
	}
	for _, p := range financialPromisePhrases {
		if strings.Contains(blob, p) {
			res.OK = false
			res.Errors = append(res.Errors, "financial promise: "+p)
		}
	}
	for _, p := range hypothesisAsFactPhrases {
		if strings.Contains(blob, p) {
			res.OK = false
			res.Errors = append(res.Errors, "internal-structure hypothesis stated as fact: "+p)
		}
	}
	if strings.Contains(blob, "oportunidade") && !hasConcreteToken(body) {
		if strings.Contains(blob, "identificamos") || strings.Contains(blob, "detectamos") {
			res.OK = false
			res.Errors = append(res.Errors, "opportunity language without concrete fact token")
		}
	}
	paras := countParagraphs(body)
	if isEmail && paras > MaxEmailParagraphs {
		res.OK = false
		res.Errors = append(res.Errors, fmt.Sprintf("excessive paragraphs (%d > %d)", paras, MaxEmailParagraphs))
	}
	bullets := countBulletLines(body)
	if bullets > MaxBulletLines {
		res.OK = false
		res.Errors = append(res.Errors, fmt.Sprintf("excessive bullet lines (%d > %d)", bullets, MaxBulletLines))
	}
	if isEmail && subj != "" {
		lowSubj := strings.ToLower(subj)
		for _, g := range genericSubjects {
			if lowSubj == g || lowSubj == g+"." {
				res.OK = false
				res.Errors = append(res.Errors, "generic subject: "+subj)
				break
			}
		}
	}
	if company := strings.TrimSpace(companyName); company != "" && len([]rune(company)) >= 4 {
		hits := countCaseInsensitive(body, company)
		if hits > MaxCompanyNameHits {
			res.OK = false
			res.Errors = append(res.Errors, fmt.Sprintf("company name repeated artificially (%d times)", hits))
		}
	}
	words := countWords(body)
	switch {
	case isWA:
		if words > MaxWhatsAppWords {
			res.OK = false
			res.Errors = append(res.Errors, fmt.Sprintf("whatsapp body exceeds %d words (%d)", MaxWhatsAppWords, words))
		}
		if words > 0 && words < 8 {
			res.Warnings = append(res.Warnings, "whatsapp body very short")
		}
		if paras >= 4 {
			res.OK = false
			res.Errors = append(res.Errors, "whatsapp body looks like pasted email (too many paragraphs)")
		}
	case channel == ChannelEmailInitial || channel == "":
		if words > 0 && words < PreferMinEmailWords {
			res.Warnings = append(res.Warnings, fmt.Sprintf("email shorter than preferred %d words (%d)", PreferMinEmailWords, words))
			res.Flags = append(res.Flags, "short_email")
		}
		if words > PreferMaxEmailWords {
			res.Warnings = append(res.Warnings, fmt.Sprintf("email longer than preferred %d words (%d)", PreferMaxEmailWords, words))
			res.Flags = append(res.Flags, "long_email")
		}
	}
	if !res.OK && len(res.Errors) == 0 {
		res.Errors = append(res.Errors, "lint failed")
	}
	return res
}

func NearDuplicate(body string, recent []string) (maxScore float64, hit bool) {
	body = strings.TrimSpace(body)
	if body == "" || len(recent) == 0 {
		return 0, false
	}
	for _, r := range recent {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		s := JaccardNgramSimilarity(body, r, NgramSize)
		if s > maxScore {
			maxScore = s
		}
	}
	return maxScore, maxScore >= NearDupThreshold
}

func JaccardNgramSimilarity(a, b string, n int) float64 {
	if n < 1 {
		n = 3
	}
	sa := ngramSet(normalizeForNgram(a), n)
	sb := ngramSet(normalizeForNgram(b), n)
	if len(sa) == 0 && len(sb) == 0 {
		return 1
	}
	if len(sa) == 0 || len(sb) == 0 {
		return 0
	}
	inter := 0
	for k := range sa {
		if sb[k] {
			inter++
		}
	}
	union := len(sa) + len(sb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func normalizeForNgram(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevSpace = false
			continue
		}
		if !prevSpace {
			b.WriteByte(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func ngramSet(s string, n int) map[string]bool {
	runes := []rune(s)
	out := make(map[string]bool)
	if len(runes) < n {
		if len(runes) > 0 {
			out[string(runes)] = true
		}
		return out
	}
	for i := 0; i+n <= len(runes); i++ {
		out[string(runes[i:i+n])] = true
	}
	return out
}

func countParagraphs(body string) int {
	parts := strings.Split(body, "\n")
	n := 0
	inPara := false
	for _, line := range parts {
		if strings.TrimSpace(line) == "" {
			if inPara {
				n++
				inPara = false
			}
			continue
		}
		inPara = true
	}
	if inPara {
		n++
	}
	return n
}

var bulletLineRe = regexp.MustCompile(`(?m)^\s*([-*•]|\d+[.)])\s+\S`)

func countBulletLines(body string) int {
	return len(bulletLineRe.FindAllStringIndex(body, -1))
}

func countCaseInsensitive(haystack, needle string) int {
	h := strings.ToLower(haystack)
	n := strings.ToLower(needle)
	if n == "" {
		return 0
	}
	c := 0
	for {
		i := strings.Index(h, n)
		if i < 0 {
			break
		}
		c++
		h = h[i+len(n):]
	}
	return c
}

var concreteTokenRe = regexp.MustCompile(`(?i)(\d{1,2}/\d{4}|\d{4}-\d{2}-\d{2}|contrato\s+\S+|aditivo\s+\S+|pncp|reajuste|prorrog|n[ºo°]\s*\d+)`)

func hasConcreteToken(body string) bool {
	return concreteTokenRe.MatchString(body)
}

const VaryStructureHint = `
REGENERACAO (unica): o rascunho anterior ficou demasiado semelhante a um envio recente.
Varie o gancho e a ordem dos paragrafos; preserve os mesmos fatos e evidence_ids; nao invente dados.
`

const MaxNearDupRegens = 1

// Package docsearch is a small full-text index over the embedded customer
// docs, so an agent can answer how Warmbly works from the same pages customers read.
package docsearch

import (
	"errors"
	"io/fs"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/warmbly/warmbly/docs"
)

// BaseURL is where the docs are published.
const BaseURL = "https://docs.warmbly.com/"

const (
	root       = "content/docs"
	readBudget = 12000
	bm25K1     = 1.2
	bm25B      = 0.75
)

// Page is one docs page.
type Page struct {
	Slug        string
	Title       string
	Description string
	Text        string
	Sections    []Section
}

// URL is the page's public address.
func (p *Page) URL() string { return BaseURL + p.Slug + "/" }

// Section is the part of a page under one "##" heading; the intro has an empty heading.
type Section struct {
	Heading string
	Text    string
	terms   map[string]int
	length  int
}

// Index holds every page.
type Index struct {
	pages []*Page
	bySlg map[string]*Page
	df    map[string]int
	n     int
	avg   float64
}

var (
	once     sync.Once
	defIndex *Index
	defErr   error
)

// Default is the index over the embedded docs.
func Default() (*Index, error) {
	once.Do(func() { defIndex, defErr = Build(docs.Content, root) })
	return defIndex, defErr
}

// Build indexes every .mdx file under dir.
func Build(fsys fs.FS, dir string) (*Index, error) {
	idx := &Index{bySlg: map[string]*Page{}, df: map[string]int{}}
	err := fs.WalkDir(fsys, dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".mdx") {
			return err
		}
		raw, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		slug := strings.TrimSuffix(strings.TrimPrefix(path, dir+"/"), ".mdx")
		slug = strings.TrimSuffix(strings.TrimSuffix(slug, "index"), "/")
		p := parsePage(slug, string(raw))
		idx.pages = append(idx.pages, p)
		idx.bySlg[slug] = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(idx.pages, func(i, j int) bool { return idx.pages[i].Slug < idx.pages[j].Slug })
	for _, p := range idx.pages {
		for i := range p.Sections {
			s := &p.Sections[i]
			s.terms = countTerms(p.Title + " " + s.Heading + " " + s.Text)
			for t, n := range s.terms {
				idx.df[t]++
				s.length += n
			}
			idx.n++
			idx.avg += float64(s.length)
		}
	}
	if idx.n > 0 {
		idx.avg /= float64(idx.n)
	}
	return idx, nil
}

// Hit is one matching section.
type Hit struct {
	Page    string `json:"page"`
	Title   string `json:"title"`
	Section string `json:"section,omitempty"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// Search ranks sections by BM25, favouring matches in titles and headings. area narrows to one top-level folder (guides, learn, api, development).
func (idx *Index) Search(query, area string, limit int) []Hit {
	if limit <= 0 || limit > 20 {
		limit = 8
	}
	terms := queryTerms(query)
	if len(terms) == 0 {
		return nil
	}
	type scored struct {
		p     *Page
		s     *Section
		score float64
	}
	var all []scored
	for _, p := range idx.pages {
		if area != "" && p.Slug != area && !strings.HasPrefix(p.Slug, area+"/") {
			continue
		}
		title := strings.ToLower(p.Title + " " + p.Slug)
		for i := range p.Sections {
			s := &p.Sections[i]
			heading := strings.ToLower(s.Heading)
			score, matched := 0.0, 0
			for _, t := range terms {
				tf := s.terms[t]
				if tf == 0 {
					continue
				}
				matched++
				idf := math.Log(1 + (float64(idx.n)-float64(idx.df[t])+0.5)/(float64(idx.df[t])+0.5))
				norm := float64(tf) * (bm25K1 + 1) / (float64(tf) + bm25K1*(1-bm25B+bm25B*float64(s.length)/idx.avg))
				if strings.Contains(title, t) {
					norm += 1.5
				}
				if strings.Contains(heading, t) {
					norm += 1
				}
				score += norm * idf
			}
			if matched == 0 {
				continue
			}
			// Sections covering more of the query rank first.
			score *= float64(matched) / float64(len(terms))
			all = append(all, scored{p, s, score})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	out := make([]Hit, 0, limit)
	perPage := map[string]int{}
	for _, a := range all {
		if len(out) == limit {
			break
		}
		// Two sections per page keeps one long page from filling the results.
		if perPage[a.p.Slug] == 2 {
			continue
		}
		perPage[a.p.Slug]++
		out = append(out, Hit{Page: a.p.Slug, Title: a.p.Title, Section: a.s.Heading, URL: a.p.URL(), Snippet: snippet(a.s.Text, terms)})
	}
	return out
}

// ErrNoPage means no docs page has that slug.
var ErrNoPage = errors.New("no docs page by that name; use search_docs to find one")

// Read is a page, or one section of it, in chunks of readBudget characters.
type Read struct {
	Page        string   `json:"page"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	URL         string   `json:"url"`
	Sections    []string `json:"sections,omitempty"`
	Text        string   `json:"text"`
	NextOffset  *int     `json:"next_offset,omitempty"`
}

// Read returns a page by slug or URL, optionally only the section whose heading contains section.
func (idx *Index) Read(page, section string, offset int) (*Read, error) {
	slug := strings.TrimSpace(page)
	slug = strings.TrimPrefix(slug, strings.TrimSuffix(BaseURL, "/"))
	slug = strings.Trim(strings.SplitN(slug, "#", 2)[0], "/")
	slug = strings.TrimPrefix(slug, "docs/")
	p := idx.bySlg[slug]
	if p == nil {
		return nil, ErrNoPage
	}
	text := p.Text
	if section = strings.ToLower(strings.TrimSpace(section)); section != "" {
		text = ""
		for _, s := range p.Sections {
			if strings.Contains(strings.ToLower(s.Heading), section) {
				text += "## " + s.Heading + "\n\n" + s.Text + "\n\n"
			}
		}
		if text == "" {
			return nil, errors.New("that page has no section with that heading")
		}
	}
	r := &Read{Page: p.Slug, Title: p.Title, Description: p.Description, URL: p.URL()}
	if offset == 0 && section == "" {
		for _, s := range p.Sections {
			if s.Heading != "" {
				r.Sections = append(r.Sections, s.Heading)
			}
		}
	}
	runes := []rune(text)
	offset = max(0, min(offset, len(runes)))
	end := min(offset+readBudget, len(runes))
	r.Text = string(runes[offset:end])
	if end < len(runes) {
		r.NextOffset = &end
	}
	return r, nil
}

// Pages lists every page slug and title, for browsing.
func (idx *Index) Pages(area string) []Hit {
	var out []Hit
	for _, p := range idx.pages {
		if area != "" && p.Slug != area && !strings.HasPrefix(p.Slug, area+"/") {
			continue
		}
		out = append(out, Hit{Page: p.Slug, Title: p.Title, URL: p.URL(), Snippet: p.Description})
	}
	return out
}

var (
	frontmatterRe = regexp.MustCompile(`(?s)\A---\n(.*?)\n---\n`)
	fmFieldRe     = regexp.MustCompile(`(?m)^(title|description):\s*(.+)$`)
	jsxLineRe     = regexp.MustCompile(`^</?[A-Z][A-Za-z]*`)
	jsxTitleRe    = regexp.MustCompile(`title=["{]+([^"}]+)["}]+`)
	imageRe       = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	wordRe        = regexp.MustCompile(`[a-z0-9][a-z0-9_]+`)
)

func parsePage(slug, raw string) *Page {
	p := &Page{Slug: slug}
	if m := frontmatterRe.FindStringSubmatch(raw); m != nil {
		for _, f := range fmFieldRe.FindAllStringSubmatch(m[1], -1) {
			v := strings.Trim(strings.TrimSpace(f[2]), `"'`)
			if f[1] == "title" {
				p.Title = v
			} else {
				p.Description = v
			}
		}
		raw = raw[len(m[0]):]
	}
	if p.Title == "" {
		p.Title = slug
	}
	p.Text = cleanMDX(raw)
	cur := Section{}
	var body strings.Builder
	inFence := false
	flush := func() {
		cur.Text = strings.TrimSpace(body.String())
		if cur.Heading != "" || cur.Text != "" {
			p.Sections = append(p.Sections, cur)
		}
		body.Reset()
	}
	for _, line := range strings.Split(p.Text, "\n") {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(line, "## ") {
			flush()
			cur = Section{Heading: strings.TrimSpace(strings.TrimPrefix(line, "## "))}
			continue
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}
	flush()
	return p
}

// cleanMDX turns MDX into plain Markdown: components become their titles, imports and images go.
func cleanMDX(raw string) string {
	var b strings.Builder
	inFence := false
	blank := false
	for _, line := range strings.Split(raw, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") {
			inFence = !inFence
		}
		if !inFence {
			if strings.HasPrefix(trim, "import ") || strings.HasPrefix(trim, "export ") {
				continue
			}
			if jsxLineRe.MatchString(trim) {
				m := jsxTitleRe.FindStringSubmatch(trim)
				if m == nil {
					continue
				}
				line = "**" + m[1] + "**"
			}
			line = imageRe.ReplaceAllString(line, "")
			if strings.TrimSpace(line) == "" {
				if blank {
					continue
				}
				blank = true
			} else {
				blank = false
			}
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "how": true, "does": true, "what": true, "with": true, "can": true,
	"you": true, "your": true, "are": true, "this": true, "that": true, "from": true, "into": true, "when": true,
	"why": true, "who": true, "which": true, "use": true, "get": true, "set": true, "its": true, "has": true,
	"have": true, "not": true, "out": true, "all": true, "any": true, "about": true, "work": true, "works": true,
}

func countTerms(text string) map[string]int {
	out := map[string]int{}
	for _, w := range wordRe.FindAllString(strings.ToLower(text), -1) {
		out[stem(w)]++
	}
	return out
}

func queryTerms(q string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range wordRe.FindAllString(strings.ToLower(q), -1) {
		if stopwords[w] {
			continue
		}
		w = stem(w)
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

// stem folds the plural and verb endings that matter for matching docs prose.
func stem(w string) string {
	for _, suf := range []string{"ing", "ies", "es", "s", "ed"} {
		if len(w) > len(suf)+3 && strings.HasSuffix(w, suf) {
			if suf == "ies" {
				return w[:len(w)-3] + "y"
			}
			return w[:len(w)-len(suf)]
		}
	}
	return w
}

// snippet is about 280 characters of text around the first query term.
func snippet(text string, terms []string) string {
	flat := strings.Join(strings.Fields(text), " ")
	lower := strings.ToLower(flat)
	at := -1
	for _, t := range terms {
		if i := strings.Index(lower, t); i >= 0 && (at < 0 || i < at) {
			at = i
		}
	}
	start := 0
	if at > 100 && len(lower) == len(flat) {
		start = at - 100
		for start < len(flat) && !utf8.RuneStart(flat[start]) {
			start++
		}
	}
	runes := []rune(flat[start:])
	out := string(runes[:min(len(runes), 280)])
	if start > 0 {
		out = "…" + out
	}
	if len(runes) > 280 {
		out += "…"
	}
	return out
}

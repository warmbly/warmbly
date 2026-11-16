package docsearch

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestSearchFindsTheGuideForAQuestion(t *testing.T) {
	idx, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	for q, want := range map[string]string{
		"suppression list unsubscribe":  "guides/unsubscribe",
		"spam placement health band":    "guides/deliverability",
		"add a follow-up step sequence": "guides/sequences",
	} {
		hits := idx.Search(q, "guides", 3)
		if len(hits) == 0 || hits[0].Page != want {
			t.Fatalf("%q: got %+v, want %s first", q, hits, want)
		}
	}
	if hits := idx.Search("the and for", "", 5); len(hits) != 0 {
		t.Fatal("stopwords alone should match nothing")
	}
}

func TestReadAcceptsSlugsAndURLsAndChunks(t *testing.T) {
	idx, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range []string{"guides/warmup", "/guides/warmup/", BaseURL + "guides/warmup/#pool-safety"} {
		r, err := idx.Read(page, "", 0)
		if err != nil || r.Page != "guides/warmup" || r.Title == "" || len(r.Sections) == 0 {
			t.Fatalf("%s: %+v %v", page, r, err)
		}
	}
	if _, err := idx.Read("guides/nope", "", 0); err != ErrNoPage {
		t.Fatalf("missing page: %v", err)
	}
	r, err := idx.Read("api/endpoints", "", 0)
	if err != nil || r.NextOffset == nil || len([]rune(r.Text)) != readBudget {
		t.Fatal("a long page should come back in chunks")
	}
	next, err := idx.Read("api/endpoints", "", *r.NextOffset)
	if err != nil || next.Text == "" || strings.HasPrefix(r.Text, next.Text) {
		t.Fatal("the next chunk should continue the page")
	}
}

func TestMDXComponentsBecomePlainText(t *testing.T) {
	fsys := fstest.MapFS{"d/guides/x.mdx": {Data: []byte("---\ntitle: X page\ndescription: \"About X.\"\n---\n\nimport { Foo } from 'bar'\n\nIntro.\n\n![shot](/x.webp)\n\n## Setup\n\n<Callout type=\"warn\" title=\"Careful\">\n\nDo the thing.\n\n</Callout>\n\n<Steps>\n<Step>\n\nOne.\n\n</Step>\n</Steps>\n\n```tsx\n<Keep />\n```\n")}}
	idx, err := Build(fsys, "d")
	if err != nil {
		t.Fatal(err)
	}
	r, err := idx.Read("guides/x", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "X page" || r.Description != "About X." {
		t.Fatalf("frontmatter: %+v", r)
	}
	for _, gone := range []string{"import", "<Steps>", "<Callout", "x.webp"} {
		if strings.Contains(r.Text, gone) {
			t.Fatalf("%q left in %q", gone, r.Text)
		}
	}
	for _, kept := range []string{"**Careful**", "Do the thing.", "One.", "<Keep />"} {
		if !strings.Contains(r.Text, kept) {
			t.Fatalf("%q missing from %q", kept, r.Text)
		}
	}
	if hits := idx.Search("careful thing", "", 5); len(hits) != 1 || hits[0].Section != "Setup" {
		t.Fatalf("section search: %+v", hits)
	}
}

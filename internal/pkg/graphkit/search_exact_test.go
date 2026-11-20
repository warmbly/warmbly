package graphkit

import (
	"context"
	"net/http"
	"testing"
)

func TestSearchAsksForSuggestionsOnlyWhenWanted(t *testing.T) {
	var got []string
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Query().Get("suggestions"))
		_, _ = w.Write([]byte(`{"query":"acme","data":[]}`))
	})
	for _, exact := range []bool{true, false} {
		if _, err := c.Search(context.Background(), "acme", nil, 30, exact); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 2 || got[0] != "false" || got[1] != "" {
		t.Fatalf("suggestions params = %q, want [false, unset]", got)
	}
}

func TestFakeExactOnlySearchReturnsOnlyExactMatches(t *testing.T) {
	res, err := NewFake().Search(context.Background(), "acme", nil, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Data) == 0 {
		t.Fatal("no exact matches")
	}
	for _, o := range res.Data {
		if !o.Exact {
			t.Fatalf("%s is a suggestion in an exact-only search", o.Domain)
		}
	}
}

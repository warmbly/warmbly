package companyenrich

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func mockClient(t *testing.T, fn roundTripFunc) *Client {
	t.Helper()
	c := NewClient("test-private-key")
	c.http.Transport = fn
	return c
}

func queryInput(t *testing.T, raw string) models.CompanyEnrichQuery {
	t.Helper()
	var in models.CompanyEnrichQuery
	if err := decodeJSON([]byte(raw), &in); err != nil {
		t.Fatal(err)
	}
	return in
}

func TestCompanyEnrichCoreRoutes(t *testing.T) {
	cases := []struct{ operation, method, path, body, query, result string }{
		{"enrich_company", "GET", "/companies/enrich", "{}", `{"domain":"example.com","expand":["workforce"],"waitForEnrichment":false}`, `{"id":"company","future":{"x":true}}`},
		{"search_companies", "POST", "/companies/search", `{"page":2,"pageSize":5,"semanticQuery":"software","exclude":{"countries":["US"]},"futureFilter":{"threshold":3}}`, `{"expand":["workforce"]}`, `{"items":[],"total":0}`},
		{"similar_companies", "POST", "/companies/similar", `{"domains":["example.com","example.org"],"minScore":0.2}`, `{"waitForEnrichment":false}`, `{"items":[],"page":1}`},
		{"search_people", "POST", "/people/search", `{"positionQuery":["CTO"],"companyFilter":{"search":{"countries":["US"]}}}`, `{"expand":["education"]}`, `{"items":[{"id":9223372036854775807}]}`},
		{"lookup_person", "POST", "/people/lookup", `{"email":"person@example.com"}`, `{"expand":["education"]}`, `{"id":9223372036854775807}`},
		{"workforce", "GET", "/companies/workforce", `{}`, `{"domain":"example.com"}`, `{"total":42}`},
		{"enrich_company_properties", "POST", "/companies/enrich", `{"name":"Example","youTubeUrl":"https://youtube.com/example"}`, `{"expand":["workforce"]}`, `{"name":"Example"}`},
		{"enrich_companies_batch", "POST", "/companies/enrich/batch", `{"domains":["example.com","example.org"]}`, `{"expand":["workforce"]}`, `[{"name":"Example"}]`},
		{"get_company", "GET", "/companies", `{}`, `{"id":"47c09930-62db-4281-8627-61d92920c471"}`, `{"name":"Example"}`},
		{"get_person", "GET", "/people", `{}`, `{"id":"9223372036854775807","expand":["education"]}`, `{"id":9223372036854775807}`},
		{"count_companies", "POST", "/companies/search/count", `{"countries":["US"]}`, `{}`, `0`},
		{"count_similar", "POST", "/companies/similar/count", `{"domains":["example.com"]}`, `{"waitForEnrichment":false}`, `42`},
	}
	if len(operations) != len(cases) {
		t.Fatalf("untested operation: have %d, cases %d", len(operations), len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.operation, func(t *testing.T) {
			in := queryInput(t, `{"operation":"`+tc.operation+`","body":`+tc.body+`,"query":`+tc.query+`}`)
			calls := 0
			client := mockClient(t, func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.Scheme != "https" || req.URL.Host != "api.companyenrich.com" || req.Method != tc.method || req.URL.Path != tc.path {
					t.Fatalf("unexpected request %s %s", req.Method, req.URL)
				}
				if req.Header.Get("Authorization") != "Bearer test-private-key" {
					t.Fatal("missing bearer credential")
				}
				if deadline, ok := req.Context().Deadline(); !ok || time.Until(deadline) > 30*time.Second {
					t.Fatal("unbounded request")
				}
				for key, value := range in.Query {
					if arr, ok := value.([]any); ok {
						if !reflect.DeepEqual(req.URL.Query()[key], []string{arr[0].(string)}) {
							t.Fatalf("query %s lost", key)
						}
					} else if req.URL.Query().Get(key) != strings.Trim(string(mustJSON(t, value)), `"`) {
						t.Fatalf("query %s lost", key)
					}
				}
				if tc.method == "POST" {
					var got map[string]any
					raw, _ := io.ReadAll(req.Body)
					if e := decodeJSON(raw, &got); e != nil {
						t.Fatal(e)
					}
					for key, value := range in.Body {
						if !reflect.DeepEqual(got[key], value) {
							t.Fatalf("body.%s lost: %v", key, got)
						}
					}
					if tc.operation == "similar_companies" || tc.operation == "search_people" {
						if got["pageSize"] != json.Number("10") {
							t.Fatal("missing explicit page size")
						}
					}
				}
				return response(200, tc.result), nil
			})
			out, err := client.Query(context.Background(), in)
			if err != nil || calls != 1 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			want := strings.ReplaceAll(tc.result, "9223372036854775807", `"9223372036854775807"`)
			var expected any
			if err := decodeJSON([]byte(want), &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(out.Data, expected) {
				t.Fatalf("lost payload: %#v want %#v", out.Data, expected)
			}
			if out.CreditsConsumed != nil {
				t.Fatal("guessed credits")
			}
		})
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestCompanyEnrichInvalidInputsNeverRequest(t *testing.T) {
	cases := []string{
		`{"operation":"get_me"}`,
		`{"operation":"https://attacker.example"}`,
		`{"operation":"enrich_company","query":{"domain":" "}}`,
		`{"operation":"enrich_company","query":{"domain":"example.com","url":"https://attacker.example"}}`,
		`{"operation":"enrich_company","query":{"domain":"example.com","waitForEnrichment":"false"}}`,
		`{"operation":"enrich_company","query":{"domain":"example.com","expand":["education"]}}`,
		`{"operation":"search_companies","body":{"page":true}}`,
		`{"operation":"search_companies","body":{"page":1.5}}`,
		`{"operation":"search_companies","body":{"page":0}}`,
		`{"operation":"search_companies","body":{"pageSize":101}}`,
		`{"operation":"search_companies","body":{"page":1001}}`,
		`{"operation":"search_companies","body":{"semanticWeight":true}}`,
		`{"operation":"search_companies","body":{"semanticWeight":1.1}}`,
		`{"operation":"search_companies","body":{"countries":"US"}}`,
		`{"operation":"search_companies","body":{"exclude":[]}}`,
		`{"operation":"search_people","body":{"companyFilter":{"similar":{}}}}`,
		`{"operation":"similar_companies","body":{"domains":[]}}`,
		`{"operation":"similar_companies","body":{"domain":"example.com","domains":["example.com"]}}`,
		`{"operation":"enrich_companies_batch","body":{"domains":[false]}}`,
		`{"operation":"enrich_company_properties","body":{"unknown":"x"}}`,
		`{"operation":"lookup_person","body":{"email":"Name <x@example.com>"}}`,
		`{"operation":"workforce"}`,
		`{"operation":"workforce","query":{"id":"47c09930-62db-4281-8627-61d92920c471","domain":"example.com"}}`,
		`{"operation":"get_company","query":{"id":"bad"}}`,
		`{"operation":"get_person","query":{"id":true}}`,
		`{"operation":"get_person","query":{"id":"9223372036854775808"}}`,
		`{"operation":"get_person","query":{"id":"0"}}`,
		`{"operation":"get_person","query":{"id":"1e3"}}`,
		`{"operation":"count_similar","body":{"domains":["example.com"]},"query":{"expand":["workforce"]}}`,
		`{"operation":"get_company","query":{"id":"47c09930-62db-4281-8627-61d92920c471"},"body":{"x":1}}`,
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			client := mockClient(t, func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid input consumed a request")
				return nil, nil
			})
			if _, err := client.Query(context.Background(), queryInput(t, raw)); err == nil {
				t.Fatal("accepted invalid input")
			}
		})
	}
	for _, tc := range []struct {
		op, key string
		value   any
	}{
		{"search_companies", "query", strings.Repeat("x", 251)},
		{"search_companies", "semanticQuery", strings.Repeat("x", 501)},
		{"similar_companies", "domains", make([]string, 11)},
		{"enrich_companies_batch", "domains", make([]string, 51)},
	} {
		client := mockClient(t, func(*http.Request) (*http.Response, error) { t.Fatal("invalid bound requested"); return nil, nil })
		if _, err := client.Query(context.Background(), models.CompanyEnrichQuery{Operation: tc.op, Body: map[string]any{tc.key: tc.value}}); err == nil {
			t.Fatalf("accepted invalid %s", tc.key)
		}
	}
}

func TestCompanyEnrichErrorsAreSanitizedAndNeverRetried(t *testing.T) {
	for _, status := range []int{301, 302, 400, 401, 402, 403, 404, 429, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			client := mockClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				r := response(status, `{"error":"test-private-key secret-body"}`)
				r.Header.Set("Location", "https://other.example")
				return r, nil
			})
			_, err := client.Query(context.Background(), queryInput(t, `{"operation":"lookup_person","body":{"email":"x@example.com"}}`))
			if calls != 1 || err == nil || strings.Contains(err.Error(), "test-private-key") || strings.Contains(err.Error(), "secret-body") {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		})
	}
	client := mockClient(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("transport: test-private-key") })
	if _, err := client.Query(context.Background(), queryInput(t, `{"operation":"count_companies"}`)); err != ErrUnavailable {
		t.Fatalf("transport error: %v", err)
	}
}

func TestCompanyEnrichRedirectsAreNotFollowed(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Redirect(w, r, "/target", http.StatusFound)
	}))
	defer server.Close()
	client := NewClient("key")
	// Exercise the actual HTTP client's redirect policy independently of the
	// fixed public URL; production has no configurable base URL.
	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := client.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if calls != 1 || resp.StatusCode != 302 {
		t.Fatalf("followed redirect: %d", calls)
	}
	if !client.http.Transport.(*http.Transport).DisableKeepAlives {
		t.Fatal("pooled request may be automatically retried")
	}
}

func TestCompanyEnrichResponseBoundsAndNumbers(t *testing.T) {
	for _, body := range []string{"", "<html>test-private-key</html>", `{} {}`, strings.Repeat("x", maxResponseBytes+1)} {
		client := mockClient(t, func(*http.Request) (*http.Response, error) { return response(200, body), nil })
		if _, err := client.Query(context.Background(), queryInput(t, `{"operation":"count_companies"}`)); err != ErrResponse {
			t.Fatalf("invalid response returned %v", err)
		}
	}
	client := mockClient(t, func(*http.Request) (*http.Response, error) {
		return response(200, `{"id":9223372036854775807,"nested":[9007199254740992,1,1.5],"creditsConsumed":0}`), nil
	})
	out, err := client.Query(context.Background(), queryInput(t, `{"operation":"count_companies"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(mustJSON(t, out.Data)) != `{"creditsConsumed":0,"id":"9223372036854775807","nested":["9007199254740992",1,1.5]}` {
		t.Fatalf("precision lost: %s", mustJSON(t, out.Data))
	}
	if out.CreditsConsumed == nil || *out.CreditsConsumed != 0 {
		t.Fatal("zero credits lost")
	}
}

func TestCompanyEnrichExplicitCreditCost(t *testing.T) {
	for _, tc := range []struct {
		name, body, header string
		want               float64
		present            bool
	}{
		{"header", `[]`, "2.5", 2.5, true},
		{"header zero", `0`, "0", 0, true},
		{"body priority", `{"creditsConsumed":1}`, "9", 1, true},
		{"body zero priority", `{"creditsConsumed":0}`, "9", 0, true},
		{"missing", `{}`, "", 0, false},
		{"negative", `{}`, "-1", 0, false},
		{"invalid", `{}`, "private-provider-message", 0, false},
		{"infinite", `{}`, "Inf", 0, false},
		{"nan", `{}`, "NaN", 0, false},
		{"overflow", `{}`, "1e999", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := mockClient(t, func(*http.Request) (*http.Response, error) {
				r := response(200, tc.body)
				r.Header.Set("X-Credit-Cost", tc.header)
				return r, nil
			})
			out, err := client.Query(context.Background(), queryInput(t, `{"operation":"count_companies"}`))
			if err != nil || (out.CreditsConsumed != nil) != tc.present {
				t.Fatalf("explicit cost presence: %+v, %v", out, err)
			}
			if tc.present && *out.CreditsConsumed != tc.want {
				t.Fatalf("cost=%v, want %v", *out.CreditsConsumed, tc.want)
			}
		})
	}
}

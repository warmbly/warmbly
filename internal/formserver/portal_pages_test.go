package formserver

import (
	"net/http"
	"strings"
	"testing"
)

// Connect pages, reports and their loader belong to the portal service; the forms service answers none of them.
func TestFormsServiceServesNoPortalPages(t *testing.T) {
	_, srv, r := newFixture(t, 0)
	link := strings.Repeat("a", 43)
	for _, path := range []string{"/connect/" + link, "/r/" + link, "/embed.js", "/api/connect/" + link, "/api/reports/" + link} {
		if w := get(r, path, mintFor(srv, link)); w.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, w.Code)
		}
	}
	if w := post(r, "/api/connect/"+link+"/oauth", "", map[string]string{"provider": "gmail"}); w.Code != http.StatusNotFound {
		t.Errorf("POST connect sign-in = %d, want 404", w.Code)
	}
	if w := get(r, "/forms.js", ""); w.Code != http.StatusOK || strings.Contains(w.Body.String(), "data-warmbly-connect") {
		t.Errorf("forms loader = %d, carries connect embeds: %v", w.Code, strings.Contains(w.Body.String(), "data-warmbly-connect"))
	}
}

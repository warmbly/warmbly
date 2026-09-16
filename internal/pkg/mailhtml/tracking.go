package mailhtml

import (
	"net/url"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/net/html"
)

// Remove Warmbly beacons from display copies, including pixels in quoted replies.
func stripOpenTrackingPixels(body string) string {
	z := html.NewTokenizer(strings.NewReader(body))
	var out strings.Builder
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			return out.String()
		}
		raw := string(z.Raw())
		if kind == html.StartTagToken || kind == html.SelfClosingTagToken {
			token := z.Token()
			if token.Data == "img" && hasOpenTrackingSource(token.Attr) {
				continue
			}
		}
		out.WriteString(raw)
	}
}

func hasOpenTrackingSource(attrs []html.Attribute) bool {
	for _, attr := range attrs {
		if attr.Key != "src" {
			continue
		}
		u, err := url.Parse(strings.TrimSpace(attr.Val))
		if err != nil || !strings.HasPrefix(u.Path, "/t/o/") {
			continue
		}
		// Match the endpoint on shared, custom and previous tracking domains.
		id := strings.TrimSuffix(strings.TrimPrefix(u.Path, "/t/o/"), ".png")
		if _, err := uuid.Parse(id); err == nil {
			return true
		}
	}
	return false
}

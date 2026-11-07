package leadfinder

import (
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// NormalizeWebsite turns a provider's website value into an absolute URL and
// its registrable domain; both empty when it is not a usable web address.
func NormalizeWebsite(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + strings.TrimPrefix(raw, "//")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", ""
	}
	host := strings.ToLower(u.Hostname())
	if !strings.Contains(host, ".") {
		return "", ""
	}
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	domain, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		domain = strings.TrimPrefix(host, "www.")
	}
	if socialDomains[domain] {
		// A Facebook page as a website is a social profile, not a site to read.
		return "", ""
	}
	return u.String(), domain
}

var socialDomains = map[string]bool{
	"facebook.com": true, "instagram.com": true, "linkedin.com": true, "twitter.com": true, "x.com": true,
	"tiktok.com": true, "youtube.com": true, "yelp.com": true, "google.com": true, "goo.gl": true,
	"wa.me": true, "whatsapp.com": true, "linktr.ee": true, "pinterest.com": true,
}

// socialKind names the network a profile URL belongs to, or "".
func socialKind(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	host = strings.TrimPrefix(host, "m.")
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return ""
	}
	first := strings.ToLower(strings.SplitN(path, "/", 2)[0])
	switch host {
	case "facebook.com", "fb.com":
		if first == "sharer" || first == "sharer.php" || first == "dialog" || first == "plugins" || first == "tr" {
			return ""
		}
		return "facebook"
	case "instagram.com":
		if first == "p" || first == "explore" {
			return ""
		}
		return "instagram"
	case "linkedin.com":
		if first == "company" || first == "in" || first == "school" {
			return "linkedin"
		}
	case "twitter.com", "x.com":
		if first == "intent" || first == "share" || first == "home" {
			return ""
		}
		return "twitter"
	case "tiktok.com":
		if strings.HasPrefix(first, "@") {
			return "tiktok"
		}
	case "youtube.com":
		if first == "watch" || first == "embed" {
			return ""
		}
		return "youtube"
	case "pinterest.com":
		return "pinterest"
	case "yelp.com":
		if first == "biz" {
			return "yelp"
		}
	}
	return ""
}

// socialURL makes a handle or URL from a provider into a profile URL.
func socialURL(kind, v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
		return v
	}
	if strings.Contains(v, ".") && strings.Contains(v, "/") {
		return "https://" + v
	}
	v = strings.TrimPrefix(v, "@")
	switch kind {
	case "facebook":
		return "https://www.facebook.com/" + v
	case "instagram":
		return "https://www.instagram.com/" + v
	case "twitter":
		return "https://x.com/" + v
	case "tiktok":
		return "https://www.tiktok.com/@" + v
	case "youtube":
		return "https://www.youtube.com/@" + v
	case "linkedin":
		return "https://www.linkedin.com/company/" + v
	}
	return ""
}

// Humanize turns a provider's snake_case type into a label: car_repair, Car repair.
func Humanize(v string) string {
	v = strings.TrimSpace(strings.NewReplacer("_", " ", "-", " ").Replace(v))
	if v == "" {
		return ""
	}
	return strings.ToUpper(v[:1]) + v[1:]
}

func joinAddress(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}

var emailShape = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,24}$`)

// cleanEmail lowercases a plausible address, or returns "".
func cleanEmail(v string) string {
	v = strings.ToLower(strings.Trim(strings.TrimSpace(v), ".,;:<>()[]\"'"))
	v = strings.TrimPrefix(v, "mailto:")
	if i := strings.IndexByte(v, '?'); i >= 0 {
		v = v[:i]
	}
	if !emailShape.MatchString(v) {
		return ""
	}
	return v
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

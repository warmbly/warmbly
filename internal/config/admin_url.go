package config

import (
	"net/url"
	"os"
	"strings"
)

// AdminURL is the admin panel's origin: ADMIN_URL, else the CORS origin on admin.<host> or port 5174; "" when neither names it.
func AdminURL() string {
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv("ADMIN_URL")), "/"); v != "" {
		return v
	}
	for _, raw := range strings.Split(os.Getenv("CORS_ALLOW_ORIGINS"), ",") {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			continue
		}
		if strings.HasPrefix(u.Hostname(), "admin.") || u.Port() == "5174" {
			return u.Scheme + "://" + u.Host
		}
	}
	return ""
}

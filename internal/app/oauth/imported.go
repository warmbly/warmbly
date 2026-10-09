package oauth

import (
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/pkg/whdomain"
)

// AppLogoPrefix is where a workspace's uploaded app logos live in blob storage.
const AppLogoPrefix = "oauth-app-logos/"

// IssuedLogoKey returns the storage key of logoURL when it is a logo this
// instance stored for orgID's apps.
func IssuedLogoKey(pu storage.PublicURLer, orgID uuid.UUID, logoURL string) (string, bool) {
	if pu == nil || logoURL == "" {
		return "", false
	}
	base := pu.PublicURL("")
	if !strings.HasPrefix(logoURL, base) {
		return "", false
	}
	key := strings.TrimPrefix(logoURL, base)
	wantPrefix := AppLogoPrefix + orgID.String() + "/"
	if !strings.HasPrefix(key, wantPrefix) || strings.Contains(key, "..") || pu.PublicURL(key) != logoURL {
		return "", false
	}
	name := strings.TrimPrefix(key, wantPrefix)
	if strings.Contains(name, "/") || !(strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".jpg")) {
		return "", false
	}
	return key, true
}

// The Imported* helpers apply the app write rules to an app arriving in a
// workspace archive, dropping what fails since nobody can be asked to fix it.

// ImportedName is the archive's app name under the naming rules, or a neutral one.
func ImportedName(raw string) string {
	name := displayname.CleanOr(raw, displayname.Workspace, "Imported app")
	if mentionsPlatform(name) {
		return "Imported app"
	}
	return name
}

// ImportedWebsite is the archive's website when it is an http(s) address.
func ImportedWebsite(raw string) string {
	website, err := appWebsite(raw)
	if err != nil {
		return ""
	}
	return website
}

// ImportedRedirectURIs keeps the redirect URIs registration would accept.
func ImportedRedirectURIs(uris []string) []string {
	out := make([]string, 0, len(uris))
	for _, u := range uris {
		if valid, err := validateRedirectURIs([]string{u}); err == nil {
			out = append(out, valid...)
		}
	}
	return out
}

// ImportedWebhook returns the app's normalized webhook domains and whether its
// webhook URL passes the outbound and domain checks.
func ImportedWebhook(webhookURL string, domains []string) ([]string, bool) {
	normalized, err := whdomain.NormalizeList(domains)
	if err != nil {
		normalized = []string{}
	}
	webhookURL = strings.TrimSpace(webhookURL)
	if webhookURL == "" || webhook.ValidateOutboundURL(webhookURL) != nil {
		return normalized, false
	}
	return normalized, whdomain.HostAllowed(hostOf(webhookURL), normalized)
}

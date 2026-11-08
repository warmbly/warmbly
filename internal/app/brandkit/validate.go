package brandkit

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
)

// maxWebsiteURLLength bounds a brand's website link.
const maxWebsiteURLLength = 2048

var accentRe = regexp.MustCompile(`^#[0-9a-f]{6}$`)

var (
	ErrInvalidAccentColor = errx.NewWithIdentifier(errx.BadRequest, "invalid_accent_color",
		"The accent colour must be a hex colour like #0284c7.")
	ErrInvalidWebsiteURL = errx.NewWithIdentifier(errx.BadRequest, "invalid_website_url",
		"The website must be a full https:// address, like https://example.com.")
	ErrWhiteLabelNotEntitled = errx.NewWithIdentifier(errx.Forbidden, "white_label_not_entitled",
		`Removing "Powered by" needs Agency Pro or Enterprise. Upgrade to turn it off.`)
)

// NormalizeAccent lower-cases a #rrggbb colour; ok is false for anything else. Empty stays empty.
func NormalizeAccent(raw string) (string, bool) {
	c := strings.ToLower(strings.TrimSpace(raw))
	if c == "" {
		return "", true
	}
	return c, accentRe.MatchString(c)
}

// NormalizeWebsite accepts an absolute https URL with a host and no credentials. Empty stays empty.
func NormalizeWebsite(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", true
	}
	if len(s) > maxWebsiteURLLength {
		return "", false
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" {
		return "", false
	}
	if host := u.Hostname(); host == "" || !strings.Contains(host, ".") {
		return "", false
	}
	return u.String(), true
}

// NormalizeInput validates a brand write. A name goes through displayname like every name another person sees.
func NormalizeInput(in models.BrandInput) (models.BrandInput, *errx.Error) {
	name, xerr := displayname.Validate("Brand name", in.DisplayName, displayname.Workspace, true)
	if xerr != nil {
		return in, xerr
	}
	accent, ok := NormalizeAccent(in.AccentColor)
	if !ok {
		return in, ErrInvalidAccentColor
	}
	website, ok := NormalizeWebsite(in.WebsiteURL)
	if !ok {
		return in, ErrInvalidWebsiteURL
	}
	return models.BrandInput{DisplayName: name, AccentColor: accent, WebsiteURL: website, HidePoweredBy: in.HidePoweredBy}, nil
}

// CheckHidePoweredBy refuses turning "Powered by" off without the entitlement; keeping a stored value is allowed.
func CheckHidePoweredBy(want, stored, entitled bool) *errx.Error {
	if want && !stored && !entitled {
		return ErrWhiteLabelNotEntitled
	}
	return nil
}

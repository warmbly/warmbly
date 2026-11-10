package brandkit

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
)

// maxWebsiteURLLength bounds a brand's website link.
const maxWebsiteURLLength = 2048

// maxEmailLength and maxEmailLocalLength are the longest address and mailbox name a mail server accepts.
const (
	maxEmailLength      = 254
	maxEmailLocalLength = 64
)

var accentRe = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// emailRe is a plain mailbox@domain address: no display name, quoting or comment.
var emailRe = regexp.MustCompile(`^[A-Za-z0-9_+-]+(?:\.[A-Za-z0-9_+-]+)*@[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)+$`)

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
	in.DisplayName, in.AccentColor, in.WebsiteURL = name, accent, website
	return in, nil
}

// NormalizeEmail accepts a plain email address. Empty stays empty.
func NormalizeEmail(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", true
	}
	if len(s) > maxEmailLength || strings.Index(s, "@") > maxEmailLocalLength || !emailRe.MatchString(s) {
		return "", false
	}
	return s, true
}

func themeEnumError(field, allowed string) *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, "invalid_theme",
		fmt.Sprintf("theme.%s must be %s. Leave it empty for the default.", field, allowed))
}

func themeLinkError(label string) *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, "invalid_brand_link",
		fmt.Sprintf("The %s must be a full https:// address, like https://example.com/help.", label))
}

// themeText is a line a client reads on the sign-in page, held to the rules for every name another person sees.
func themeText(label, raw string, kind displayname.Kind, limit int) (string, *errx.Error) {
	out, r := displayname.Check(raw, kind)
	var fix string
	switch r {
	case displayname.OK:
		return out, nil
	case displayname.Empty:
		return "", nil
	case displayname.TooLong:
		fix = fmt.Sprintf("must be %d characters or less. Shorten it.", limit)
	case displayname.Link:
		fix = "cannot contain a link, web address or email address. Remove it, or use the support and legal links instead."
	case displayname.NoLetter:
		fix = "must contain a letter or a number."
	default:
		fix = "contains characters that are not allowed. Use plain text on one line, without markup such as < or >."
	}
	return "", errx.NewWithIdentifier(errx.BadRequest, "invalid_brand_text", fmt.Sprintf("The %s %s", label, fix))
}

// checkTheme applies every theme rule: the theme with each failing field emptied, and the first failure.
func checkTheme(in models.PortalTheme) (models.PortalTheme, *errx.Error) {
	var out models.PortalTheme
	var first *errx.Error
	fail := func(xerr *errx.Error) {
		if first == nil {
			first = xerr
		}
	}

	if in.Font.Valid() {
		out.Font = in.Font
	} else {
		fail(themeEnumError("font", `"system", "geometric" or "serif"`))
	}
	if in.Corners.Valid() {
		out.Corners = in.Corners
	} else {
		fail(themeEnumError("corners", `"sharp" or "round"`))
	}
	if in.Appearance.Valid() {
		out.Appearance = in.Appearance
	} else {
		fail(themeEnumError("appearance", `"light" or "dark"`))
	}
	if in.SignInLayout.Valid() {
		out.SignInLayout = in.SignInLayout
	} else {
		fail(themeEnumError("sign_in_layout", `"split" or "cover"`))
	}
	if in.SignInBackground.Valid() {
		out.SignInBackground = in.SignInBackground
	} else {
		fail(themeEnumError("sign_in_background", `"accent", "dark" or "image"`))
	}

	var xerr *errx.Error
	if out.SignInHeading, xerr = themeText("sign-in heading", in.SignInHeading, displayname.Heading, displayname.HeadingMaxLength); xerr != nil {
		fail(xerr)
	}
	if out.SignInMessage, xerr = themeText("sign-in message", in.SignInMessage, displayname.Message, displayname.MessageMaxLength); xerr != nil {
		fail(xerr)
	}

	var ok bool
	if out.SupportEmail, ok = NormalizeEmail(in.SupportEmail); !ok {
		fail(errx.NewWithIdentifier(errx.BadRequest, "invalid_brand_link",
			"The support email must be a plain email address, like help@example.com."))
	}
	for _, link := range []struct {
		label string
		in    string
		out   *string
	}{
		{"support link", in.SupportURL, &out.SupportURL},
		{"terms link", in.TermsURL, &out.TermsURL},
		{"privacy link", in.PrivacyURL, &out.PrivacyURL},
	} {
		if *link.out, ok = NormalizeWebsite(link.in); !ok {
			fail(themeLinkError(link.label))
		}
	}
	return out, first
}

// NormalizeTheme validates a theme write and returns its stored form.
func NormalizeTheme(in models.PortalTheme) (models.PortalTheme, *errx.Error) {
	out, xerr := checkTheme(in)
	if xerr != nil {
		return in, xerr
	}
	return out, nil
}

// CleanTheme is a stored theme as it may be shown: a field that fails its rule today reads as unset.
func CleanTheme(stored models.PortalTheme) models.PortalTheme {
	out, _ := checkTheme(stored)
	return out
}

// CheckHidePoweredBy refuses turning "Powered by" off without the entitlement; keeping a stored value is allowed.
func CheckHidePoweredBy(want, stored, entitled bool) *errx.Error {
	if want && !stored && !entitled {
		return ErrWhiteLabelNotEntitled
	}
	return nil
}

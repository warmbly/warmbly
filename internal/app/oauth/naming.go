package oauth

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
)

// platformName is the name no third-party app may present itself under.
const platformName = "warmbly"

// dcrFallbackName stands in for a self-registered client with no usable name.
const dcrFallbackName = "MCP client"

var (
	// confusables folds look-alike characters onto plain letters, after NFKC and lowercasing.
	confusables = strings.NewReplacer(
		"0", "o", "1", "l", "|", "l", "!", "l", "i", "l", "ı", "l", "4", "a", "@", "a",
		"а", "a", "α", "a", "ɑ", "a", "у", "y", "ʏ", "y", "ԝ", "w", "ᴡ", "w",
		"ʀ", "r", "ᴍ", "m", "ʙ", "b", "ʟ", "l",
	)
	digraphs = strings.NewReplacer("rn", "m", "vv", "w")
	// A bracketed aside, as MCP clients append the server's local label: "Claude Code (warmbly)".
	asideRe = regexp.MustCompile(`\s*[(\[][^()\[\]]*[)\]]`)
)

// mentionsPlatform reports whether name spells the platform's name, through
// spacing, punctuation and look-alike characters.
func mentionsPlatform(name string) bool {
	folded := confusables.Replace(strings.ToLower(norm.NFKC.String(name)))
	var b strings.Builder
	for _, r := range folded {
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r)
		}
	}
	return strings.Contains(digraphs.Replace(b.String()), platformName)
}

func errPlatformName() error {
	return errx.NewWithIdentifier(errx.BadRequest, displayname.ErrorCode,
		"name cannot include Warmbly: people connecting the app would take it for Warmbly's own.")
}

// appName applies the shared naming rules plus the platform-name rule. A name
// left as it was (current) keeps passing, so an existing app stays editable.
// An app name is shown to every workspace that sees its consent screen or its directory listing.
func appName(raw, current string) (string, error) {
	name, xerr := displayname.Validate("name", raw, displayname.Workspace, false)
	if xerr != nil {
		return "", xerr
	}
	if name != current && mentionsPlatform(name) {
		return "", errPlatformName()
	}
	return name, nil
}

// dcrClientName is a self-registered client's name with any aside naming the
// platform dropped, or a neutral name when what remains still names it.
func dcrClientName(raw string) string {
	name := displayname.Clean(raw, displayname.Workspace)
	if mentionsPlatform(name) {
		name = displayname.Clean(asideRe.ReplaceAllStringFunc(name, func(aside string) string {
			if mentionsPlatform(aside) {
				return ""
			}
			return aside
		}), displayname.Workspace)
	}
	if name == "" || mentionsPlatform(name) {
		return dcrFallbackName
	}
	return name
}

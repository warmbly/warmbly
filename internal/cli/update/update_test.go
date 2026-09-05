package update

import (
	"runtime"
	"strings"
	"testing"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, candidate string
		want               bool
	}{
		{"v1.2.3", "v1.2.4", true},
		{"v1.2.3", "v1.3.0", true},
		{"v1.2.3", "v2.0.0", true},
		{"v1.2.3", "v1.2.3", false},
		{"v1.2.3", "v1.2.2", false},
		{"v1.10.0", "v1.9.0", false},
		{"v1.9.0", "v1.10.0", true},
		// Someone running their own build is not behind ours, and must not be
		// told to download something.
		{"dev", "v1.2.3", false},
		{"v1.2.3-4-gabc1234", "v1.2.4", false},
		{"", "v1.2.3", false},
		// A malformed tag on the far side is ignored rather than trusted.
		{"v1.2.3", "not-a-version", false},
		{"v1.2.3", "v1.2", false},
	}
	for _, c := range cases {
		if got := IsNewer(c.current, c.candidate); got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.current, c.candidate, got, c.want)
		}
	}
}

func TestDetectMethod(t *testing.T) {
	cases := map[string]Method{
		"/home/jane/.local/bin/warmbly":              MethodBinary,
		"/opt/homebrew/bin/warmbly":                  MethodHomebrew,
		"/usr/local/Cellar/warmbly/1.0/bin/warmbly":  MethodHomebrew,
		"/home/linuxbrew/.linuxbrew/bin/warmbly":     MethodHomebrew,
		"C:\\Users\\jane\\scoop\\shims\\warmbly.exe": MethodScoop,
		"/home/jane/go/bin/warmbly":                  MethodGoInstall,
		"/usr/bin/warmbly":                           MethodPackage,
		"/nix/store/abc-warmbly/bin/warmbly":         MethodPackage,
	}
	for path, want := range cases {
		if got := DetectMethod(path); got != want {
			t.Errorf("DetectMethod(%q) = %v, want %v", path, got, want)
		}
	}
}

// Replacing a file a package manager owns produces a version that silently
// reverts on its next upgrade, so each of these has to name a command.
func TestPackageMethodsNameACommand(t *testing.T) {
	for _, m := range []Method{MethodHomebrew, MethodScoop, MethodGoInstall, MethodPackage} {
		if m.UpgradeCommand() == "" {
			t.Errorf("method %v self-replaces; it must tell the user what to run instead", m)
		}
	}
	if MethodBinary.UpgradeCommand() != "" {
		t.Error("a plain binary should be replaced in place, not delegated")
	}
}

func TestAssetNameMatchesWhatWePublish(t *testing.T) {
	name := AssetName()
	if !strings.HasPrefix(name, "warmbly_"+runtime.GOOS+"_"+runtime.GOARCH) {
		t.Errorf("asset name %q does not name this platform", name)
	}
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(name, ".zip") {
			t.Errorf("windows asset %q should be a zip", name)
		}
	} else if !strings.HasSuffix(name, ".tar.gz") {
		t.Errorf("unix asset %q should be a tar.gz", name)
	}
}

func TestChecksumFor(t *testing.T) {
	sums := `abc123  warmbly_linux_amd64.tar.gz
def456  warmbly_darwin_arm64.tar.gz
`
	if got := checksumFor(sums, "warmbly_linux_amd64.tar.gz"); got != "abc123" {
		t.Errorf("got %q", got)
	}
	if got := checksumFor(sums, "warmbly_windows_amd64.zip"); got != "" {
		t.Errorf("an absent asset must report no checksum, got %q", got)
	}
	// The BSD-style "*name" form has to resolve too, or verification silently
	// degrades to a warning on machines whose sha tool writes it.
	if got := checksumFor("abc123 *warmbly_linux_amd64.tar.gz", "warmbly_linux_amd64.tar.gz"); got != "abc123" {
		t.Errorf("star-prefixed name not matched, got %q", got)
	}
}

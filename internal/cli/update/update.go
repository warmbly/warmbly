// Package update keeps an installed CLI current.
//
// Two jobs: telling someone a newer release exists without getting in their
// way, and replacing the binary when they ask for it.
//
// The version lookup deliberately does not use the GitHub API. The
// unauthenticated API is rate limited per IP, which on a shared CI runner or
// behind a corporate NAT means the check fails for everyone at once; the
// releases/latest redirect is a plain HTTP redirect with no such limit.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	repo = "warmbly/warmbly"
	// LatestURL redirects to the newest release's tag page.
	LatestURL = "https://github.com/" + repo + "/releases/latest"
	// DownloadBase is where the release assets live. Names carry no version,
	// so "latest" resolves without knowing the tag first.
	DownloadBase = "https://github.com/" + repo + "/releases/latest/download"
)

// CheckInterval is how often the background nudge looks for a new release.
// Once a day: often enough to matter, rare enough that nobody notices it.
const CheckInterval = 24 * time.Hour

// LatestVersion resolves the newest published release tag by following the
// latest-release redirect and reading the tag out of the final URL.
func LatestVersion(ctx context.Context, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := &http.Client{
		// Stop at the redirect: the tag is in the Location header, and
		// following it would download an HTML page for nothing.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, LatestURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	location := resp.Header.Get("Location")
	if location == "" {
		return "", errors.New("no release redirect")
	}
	tag := location[strings.LastIndex(location, "/")+1:]
	if !strings.HasPrefix(tag, "v") {
		return "", fmt.Errorf("unexpected release tag %q", tag)
	}
	return tag, nil
}

// IsNewer reports whether candidate is a later release than current. Both are
// vX.Y.Z. A current version that is not a clean release tag (a dev build, a
// git describe string) returns false: someone running their own build does not
// want to be told to download ours.
func IsNewer(current, candidate string) bool {
	cur, ok := parseVersion(current)
	if !ok {
		return false
	}
	next, ok := parseVersion(candidate)
	if !ok {
		return false
	}
	for i := 0; i < 3; i++ {
		if next[i] != cur[i] {
			return next[i] > cur[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	// A git describe string (1.2.3-4-gabc1234) or a prerelease is not a plain
	// release, so it never compares.
	if v == "" || strings.ContainsAny(v, "-+ ") {
		return out, false
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// Method is how this binary got here, which decides how it should be replaced.
type Method int

const (
	// MethodBinary is a plain binary we can overwrite ourselves.
	MethodBinary Method = iota
	MethodHomebrew
	MethodScoop
	MethodGoInstall
	MethodPackage
)

// UpgradeCommand is what to tell the user to run when we must not replace the
// binary ourselves. Empty when a self-replace is the right answer.
func (m Method) UpgradeCommand() string {
	switch m {
	case MethodHomebrew:
		return "brew upgrade warmbly"
	case MethodScoop:
		return "scoop update warmbly"
	case MethodGoInstall:
		return "go install github.com/" + repo + "/cmd/cli@latest"
	case MethodPackage:
		return "your package manager"
	default:
		return ""
	}
}

// DetectMethod works out how this binary was installed from where it sits.
// Fighting a package manager by overwriting the file it owns produces a
// version that reverts on the next upgrade, so this is what stops that.
func DetectMethod(executable string) Method {
	path, err := filepath.EvalSymlinks(executable)
	if err != nil {
		path = executable
	}
	// Backslashes are normalised explicitly rather than with filepath.ToSlash,
	// which is a no-op off Windows: the detection then behaves the same
	// wherever it runs, including in a test.
	lower := strings.ToLower(strings.ReplaceAll(path, `\`, "/"))

	switch {
	case strings.Contains(lower, "/cellar/"), strings.Contains(lower, "/homebrew/"),
		strings.Contains(lower, "/linuxbrew/"):
		return MethodHomebrew
	case strings.Contains(lower, "/scoop/"):
		return MethodScoop
	case strings.Contains(lower, "/go/bin/"), strings.HasSuffix(lower, "/gopath/bin/warmbly"):
		return MethodGoInstall
	case strings.HasPrefix(lower, "/usr/bin/"), strings.HasPrefix(lower, "/opt/"),
		strings.HasPrefix(lower, "/snap/"), strings.HasPrefix(lower, "/nix/"):
		return MethodPackage
	default:
		return MethodBinary
	}
}

// AssetName is the archive published for the running platform.
func AssetName() string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("warmbly_%s_%s.zip", runtime.GOOS, runtime.GOARCH)
	}
	return fmt.Sprintf("warmbly_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
}

// Replace downloads the newest build for this platform, verifies it against
// the published checksums, and swaps it in for the running binary.
//
// The swap is a rename, which is atomic: an interrupted upgrade leaves either
// the old binary or the new one, never half of either.
func Replace(ctx context.Context, executable string, progress func(string)) error {
	if runtime.GOOS == "windows" {
		return errors.New("self-upgrade is not supported on Windows because a running .exe cannot be replaced.\nRun the installer again instead:\n  irm https://warmbly.com/cli.ps1 | iex")
	}

	asset := AssetName()
	progress("downloading " + asset)
	archive, err := download(ctx, DownloadBase+"/"+asset)
	if err != nil {
		return err
	}

	progress("verifying checksum")
	sums, err := download(ctx, DownloadBase+"/checksums.txt")
	if err != nil {
		return fmt.Errorf("could not fetch checksums.txt, so the download was not verified: %w", err)
	}
	want := checksumFor(string(sums), asset)
	if want == "" {
		return fmt.Errorf("checksums.txt has no entry for %s", asset)
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("checksum mismatch for %s.\n  expected %s\n  got      %s\nNothing was changed", asset, want, got)
	}

	progress("unpacking")
	binary, err := extractBinary(archive)
	if err != nil {
		return err
	}

	target, err := filepath.EvalSymlinks(executable)
	if err != nil {
		target = executable
	}
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".warmbly-upgrade-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w\nIf it is system-owned, re-run the installer instead:\n  curl -fsSL https://warmbly.com/cli.sh | sh", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(binary); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("could not replace %s: %w", target, err)
	}
	return nil
}

func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("could not download %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}

func checksumFor(sums, asset string) string {
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset {
			return fields[0]
		}
	}
	return ""
}

// extractBinary pulls just the warmbly executable out of the release archive.
func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("the archive is not readable: %w", err)
	}
	defer gz.Close()

	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(header.Name) != "warmbly" {
			continue
		}
		return io.ReadAll(io.LimitReader(reader, 200<<20))
	}
	return nil, errors.New("the archive did not contain a warmbly binary")
}

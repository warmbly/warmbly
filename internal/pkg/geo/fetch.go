package geo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/oschwald/geoip2-golang/v2"
)

// fetchTimeout bounds the whole download. Nothing reads the database until it
// lands, so this is a boot budget rather than a request budget; GeoLite2-City
// is around 70 MB and a slow link still has to finish.
const fetchTimeout = 3 * time.Minute

// maxDatabaseBytes is what a misconfigured URL is allowed to cost. GeoLite2-City
// is the largest edition anyone points this at, by a wide margin.
const maxDatabaseBytes = 512 << 20

// tarMagicOffset is where the POSIX ustar magic sits in a 512-byte tar header.
const tarMagicOffset = 257

// Ensure puts a MaxMind database at path, downloading it from url when nothing
// is there yet. It reports whether it downloaded one.
//
// An existing file always wins and is never re-fetched: a bind mount, a volume
// or a file an operator dropped in by hand is their copy, and replacing it
// behind their back on a restart is not this function's business. That also
// makes the container case self-correcting, because an ephemeral filesystem
// starts empty and a persistent one keeps what the last boot fetched.
//
// An empty url does nothing and is not an error. The database is optional
// everywhere it is read, so every failure here is a warning to the caller and
// never a reason to refuse to start.
func Ensure(ctx context.Context, path, url string) (bool, error) {
	path, url = strings.TrimSpace(path), strings.TrimSpace(url)
	if path == "" || url == "" {
		return false, nil
	}
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		return false, nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("geo: create %s: %w", dir, err)
	}

	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	body, err := get(ctx, url)
	if err != nil {
		return false, err
	}
	defer body.Close()

	// Written beside the destination rather than in a temp dir, so the rename
	// below stays on one filesystem and is atomic. A reader can therefore only
	// ever see a complete database.
	tmp, err := os.CreateTemp(dir, ".geodb-*")
	if err != nil {
		return false, fmt.Errorf("geo: temp file in %s: %w", dir, err)
	}
	defer os.Remove(tmp.Name())

	src, err := decode(io.LimitReader(body, maxDatabaseBytes))
	if err != nil {
		tmp.Close()
		return false, err
	}
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		return false, fmt.Errorf("geo: write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("geo: write %s: %w", tmp.Name(), err)
	}

	// Opened before it is put in place: a truncated or wrong-format download
	// otherwise becomes the file that "already exists" on every later boot,
	// and the fetch would never be retried.
	db, err := geoip2.Open(tmp.Name())
	if err != nil {
		return false, fmt.Errorf("geo: %s did not serve a MaxMind database: %w", redactURL(url), err)
	}
	_ = db.Close()

	if err := os.Rename(tmp.Name(), path); err != nil {
		return false, fmt.Errorf("geo: install %s: %w", path, err)
	}
	return true, nil
}

// get performs the download, treating any non-2xx as a failure rather than
// writing an error page to disk as if it were a database.
func get(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("geo: %s: %w", redactURL(url), err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("geo: %s: %w", redactURL(url), err)
	}
	if resp.StatusCode/100 != 2 {
		resp.Body.Close()
		return nil, fmt.Errorf("geo: %s returned %s", redactURL(url), resp.Status)
	}
	return resp.Body, nil
}

// decode unwraps whatever the URL served down to the database bytes. The shape
// is read from the content, not from the URL, because the same file is served
// under every naming convention there is: MaxMind's permalink hands back a
// .tar.gz with the database nested under a dated directory, DB-IP serves a bare
// .mmdb.gz, and a mirror often serves the .mmdb itself.
func decode(r io.Reader) (io.Reader, error) {
	head, rest, err := peek(r, 2)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(head, []byte{0x1f, 0x8b}) {
		return rest, nil
	}
	gz, err := gzip.NewReader(rest)
	if err != nil {
		return nil, fmt.Errorf("geo: gzip: %w", err)
	}
	block, plain, err := peek(gz, tarMagicOffset+5)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(block[tarMagicOffset:], []byte("ustar")) {
		return plain, nil
	}
	return firstDatabase(tar.NewReader(plain))
}

// firstDatabase positions the archive on its first .mmdb member. MaxMind ships
// one per archive alongside a licence and a changelog, so there is nothing to
// choose between.
//
// AppleDouble sidecars are skipped rather than matched. An archive rolled up on
// macOS carries a ._name companion holding each file's extended attributes, it
// is a regular file, it sorts ahead of the file it belongs to, and ._db.mmdb
// ends in .mmdb like any other: taking the first match installs 249 bytes of
// xattrs as the database.
func firstDatabase(tr *tar.Reader) (io.Reader, error) {
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("geo: archive holds no .mmdb file")
		}
		if err != nil {
			return nil, fmt.Errorf("geo: read archive: %w", err)
		}
		name := path.Base(filepath.ToSlash(h.Name))
		if h.Typeflag == tar.TypeReg && strings.HasSuffix(name, ".mmdb") && !strings.HasPrefix(name, "._") {
			return tr, nil
		}
	}
}

// peek reads n bytes and returns them along with a reader that still yields
// them, so a stream can be sniffed without being consumed. A stream shorter
// than n is not an error here; the caller's prefix comparison fails instead.
func peek(r io.Reader, n int) ([]byte, io.Reader, error) {
	buf := make([]byte, n)
	read, err := io.ReadFull(r, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, nil, fmt.Errorf("geo: read: %w", err)
	}
	buf = buf[:read]
	if len(buf) < n {
		buf = append(buf, make([]byte, n-len(buf))...)
	}
	return buf, io.MultiReader(bytes.NewReader(buf[:read]), r), nil
}

// redactURL keeps a download URL out of logs and error strings with its query
// intact only in shape. MaxMind's permalink carries the account's licence key
// there, and an error message is the one place nobody expects to find one.
func redactURL(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i] + "?<redacted>"
	}
	return raw
}

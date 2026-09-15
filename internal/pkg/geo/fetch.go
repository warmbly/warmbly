package geo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

// maxDatabaseBytes is what a misconfigured URL is allowed to cost, applied to
// the decoded database rather than the transfer: gzip expands, so a cap on what
// arrives is no cap at all on what lands. GeoLite2-City is the largest edition
// anyone points this at, by a wide margin.
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

	src, err := decode(body)
	if err != nil {
		tmp.Close()
		return false, err
	}
	// One byte past the cap, so an oversized stream is detected rather than
	// silently truncated into a file that then fails to open.
	written, err := io.Copy(tmp, io.LimitReader(src, maxDatabaseBytes+1))
	if err != nil {
		tmp.Close()
		return false, fmt.Errorf("geo: write %s: %w", tmp.Name(), err)
	}
	if written > maxDatabaseBytes {
		tmp.Close()
		return false, fmt.Errorf("geo: %s expands past %d bytes, which no database does", redactURL(url), maxDatabaseBytes)
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
func get(ctx context.Context, raw string) (io.ReadCloser, error) {
	if err := checkURL(raw); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, fmt.Errorf("geo: %s is not a usable URL", redactURL(raw))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("geo: %s: %w", redactURL(raw), cause(err))
	}
	if resp.StatusCode/100 != 2 {
		resp.Body.Close()
		return nil, fmt.Errorf("geo: %s returned %s", redactURL(raw), resp.Status)
	}
	return resp.Body, nil
}

// client refuses to follow a redirect down from https to http, because the
// query string it would carry there is the licence key in cleartext.
var client = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("redirected from https to http")
		}
		return nil
	},
}

// checkURL refuses a URL that would put a credential on the wire in the clear.
// http is allowed for a plain mirror, because a self-hosted one on a private
// network is a reasonable thing to have; it is refused the moment the URL
// carries anything secret, which is what a licence key in the query is.
func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("geo: the configured URL cannot be parsed")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if u.User != nil || u.RawQuery != "" {
			return fmt.Errorf("geo: %s would send a credential in cleartext; use https", redactURL(raw))
		}
		return nil
	default:
		return fmt.Errorf("geo: %s is not an http or https URL", redactURL(raw))
	}
}

// cause strips the URL that net/http puts in its own error text. *url.Error
// prints the address it was given, licence key and all, so wrapping one with
// %w defeats the redaction applied to the URL beside it. Its cause is the part
// worth reading ("connection refused", "no such host") and names nothing.
func cause(err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return uerr.Err
	}
	return err
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

// redactURL keeps a download URL out of logs and error strings with its shape
// intact and nothing else. MaxMind's permalink carries the account's licence
// key in the query, a mirror can carry basic-auth credentials in the userinfo,
// and an error message is the one place nobody expects to find either.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<unparseable url>"
	}
	u.User = nil
	u.Fragment = ""
	if u.RawQuery != "" {
		u.RawQuery = "<redacted>"
	}
	return u.String()
}

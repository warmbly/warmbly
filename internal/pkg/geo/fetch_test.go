package geo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// serve returns a server handing back body once, and the URL to reach it.
func serve(t *testing.T, status int, body []byte) string {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(s.Close)
	return s.URL
}

func gzipped(t *testing.T, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// tarball is MaxMind's permalink shape: a gzipped tar with the database nested
// under a dated directory, next to files that are not databases.
//
// Every archive here carries the AppleDouble sidecars a macOS tar writes, and
// they are the reason this fixture is not simply two members: ._<name>.mmdb is
// a regular file, it is written ahead of the database, and it ends in .mmdb, so
// an extractor taking the first match installs a few hundred bytes of extended
// attributes and the reader then rejects the result.
func tarball(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	write := func(path string, content []byte) {
		if err := tw.WriteHeader(&tar.Header{Name: path, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	write("._GeoLite2-City_20260911", []byte("AppleDouble"))
	write("GeoLite2-City_20260911/._COPYRIGHT.txt", []byte("AppleDouble"))
	write("GeoLite2-City_20260911/COPYRIGHT.txt", []byte("(c) MaxMind"))
	write("GeoLite2-City_20260911/._"+name, []byte("AppleDouble"))
	write("GeoLite2-City_20260911/"+name, body)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return gzipped(t, buf.Bytes())
}

func TestEnsureAcceptsEveryShapeADatabaseIsServedIn(t *testing.T) {
	db := testDatabase(t)
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"bare mmdb", db},
		{"gzipped mmdb", gzipped(t, db)},
		{"maxmind tar.gz", tarball(t, "GeoLite2-City.mmdb", db)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A subdirectory that does not exist yet, because the container
			// case points at one and nothing else creates it.
			path := filepath.Join(t.TempDir(), "geo", "GeoLite2-City.mmdb")
			fetched, err := Ensure(context.Background(), path, serve(t, http.StatusOK, tc.body))
			if err != nil {
				t.Fatalf("Ensure: %v", err)
			}
			if !fetched {
				t.Fatal("reported nothing fetched")
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, db) {
				t.Fatalf("stored %d bytes, want the %d-byte database", len(got), len(db))
			}
			// The reader has to accept what was written, which is the whole
			// point of unwrapping the archive rather than storing it.
			if _, err := New(path); err != nil {
				t.Fatalf("stored database does not open: %v", err)
			}
		})
	}
}

func TestEnsureNeverReplacesAFileThatIsAlreadyThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "GeoLite2-City.mmdb")
	if err := os.WriteFile(path, []byte("the operator's own copy"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Serving a valid database proves the file won, not that the download failed.
	fetched, err := Ensure(context.Background(), path, serve(t, http.StatusOK, testDatabase(t)))
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if fetched {
		t.Fatal("overwrote a database the operator put there")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "the operator's own copy" {
		t.Fatalf("file was replaced: %q", got)
	}
}

func TestEnsureWithNoURLDoesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "GeoLite2-City.mmdb")
	fetched, err := Ensure(context.Background(), path, "  ")
	if err != nil || fetched {
		t.Fatalf("Ensure(no url) = %v, %v; want false, nil", fetched, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("wrote something with no URL configured")
	}
}

// What must never happen is a bad download landing at the path, because an
// existing file is never re-fetched: one truncated body would disable location
// lookups permanently, and the next boot would report nothing wrong.
func TestEnsureLeavesNothingBehindWhenTheDownloadIsNotADatabase(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   []byte
	}{
		{"error page", http.StatusForbidden, []byte("<html>Invalid license key</html>")},
		{"not a database", http.StatusOK, []byte("nowhere near a MaxMind database")},
		{"truncated database", http.StatusOK, testDatabase(t)[:40]},
		{"archive with no database in it", http.StatusOK, tarball(t, "COPYRIGHT2.txt", []byte("no"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "GeoLite2-City.mmdb")
			fetched, err := Ensure(context.Background(), path, serve(t, tc.status, tc.body))
			if err == nil {
				t.Fatal("accepted a body that is not a database")
			}
			if fetched {
				t.Fatal("reported a fetch that did not happen")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("installed a database that does not open")
			}
			// The temp file is written beside the destination, so a failure
			// that leaves it there fills the volume one boot at a time.
			left, _ := os.ReadDir(dir)
			if len(left) != 0 {
				t.Fatalf("left %d file(s) behind: %v", len(left), left)
			}
		})
	}
}

func TestRedactURLDropsTheLicenceKey(t *testing.T) {
	const permalink = "https://download.maxmind.com/app/geoip_download?edition_id=GeoLite2-City&license_key=SECRET&suffix=tar.gz"
	got := redactURL(permalink)
	if bytes.Contains([]byte(got), []byte("SECRET")) {
		t.Fatalf("redactURL kept the licence key: %s", got)
	}
	if got != "https://download.maxmind.com/app/geoip_download?<redacted>" {
		t.Fatalf("redactURL = %q", got)
	}
}

// testDatabase builds the smallest thing the MaxMind reader accepts: a search
// tree of one node whose records both mean "not found", and the metadata that
// describes it. Hand-built so the test needs no vendored binary and no network,
// the same reason tracking/src/asndb.rs builds its own.
func testDatabase(t *testing.T) []byte {
	t.Helper()
	const nodeCount, recordSize = 1, 24

	var out []byte
	// One node, two 24-bit records. A record equal to node_count is the
	// terminator, so this database resolves nothing and still parses.
	for i := 0; i < 2; i++ {
		out = append(out, 0x00, 0x00, byte(nodeCount))
	}
	out = append(out, make([]byte, 16)...) // data section separator

	meta := mmdbMap(
		mmdbString("binary_format_major_version"), mmdbUint16(2),
		mmdbString("binary_format_minor_version"), mmdbUint16(0),
		mmdbString("build_epoch"), mmdbUint64(0),
		mmdbString("database_type"), mmdbString("GeoLite2-City"),
		mmdbString("description"), mmdbMap(),
		mmdbString("ip_version"), mmdbUint16(6),
		mmdbString("languages"), mmdbArray(mmdbString("en")),
		mmdbString("node_count"), mmdbUint32(nodeCount),
		mmdbString("record_size"), mmdbUint16(recordSize),
	)
	out = append(out, []byte("\xab\xcd\xefMaxMind.com")...)
	return append(out, meta...)
}

// The MaxMind DB data format: a control byte carrying a 3-bit type and a size,
// with types above 7 moved into a following byte. Only the fixed-size cases the
// metadata above needs are implemented.
func mmdbControl(kind, size int) []byte {
	if size > 28 {
		panic("mmdb test encoder: size needs the extended form")
	}
	if kind <= 7 {
		return []byte{byte(kind<<5 | size)}
	}
	return []byte{byte(size), byte(kind - 7)}
}

func mmdbString(s string) []byte {
	return append(mmdbControl(2, len(s)), s...)
}

func mmdbUint16(v uint16) []byte { return mmdbUint(5, uint64(v)) }
func mmdbUint32(v uint32) []byte { return mmdbUint(6, uint64(v)) }
func mmdbUint64(v uint64) []byte { return mmdbUint(9, v) }

// Unsigned values are big-endian with leading zero bytes dropped, so the size
// in the control byte is the byte count and zero encodes as no bytes at all.
func mmdbUint(kind int, v uint64) []byte {
	var full [8]byte
	binary.BigEndian.PutUint64(full[:], v)
	body := bytes.TrimLeft(full[:], "\x00")
	return append(mmdbControl(kind, len(body)), body...)
}

func mmdbMap(kv ...[]byte) []byte {
	out := mmdbControl(7, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		out = append(out, kv[i]...)
		out = append(out, kv[i+1]...)
	}
	return out
}

func mmdbArray(items ...[]byte) []byte {
	out := mmdbControl(11, len(items))
	for _, item := range items {
		out = append(out, item...)
	}
	return out
}

package imap

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime/quotedprintable"
	"strconv"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
	"github.com/warmbly/warmbly/internal/config"
)

// textPart is one text/* leaf of a message, addressed by its IMAP part path
// ("1", "1.2", ...) and carrying what the body structure already told us about
// how its bytes are encoded.
type textPart struct {
	path     []int
	encoding string
	charset  string
	html     bool
}

// maxTextParts bounds how many text leaves one message can contribute, so a
// pathological message cannot turn into an unbounded FETCH.
const maxTextParts = 5

// fetchTextParts returns the plain-text and HTML bodies of a message.
//
// Every text part is read from its own section, in a single FETCH. Fetching a
// fixed BODY[1] for each leaf (what this did before) handed the first part's
// bytes back as the HTML body too, so plain text was rendered as markup: line
// breaks collapsed, "&" showed as an entity, and anything inside angle
// brackets vanished.
func fetchTextParts(c *imapclient.Client, uid imap.UID, bs imap.BodyStructure) (plain, html string) {
	if bs == nil {
		return "", ""
	}

	wanted := selectTextParts(bs)
	if len(wanted) == 0 {
		return "", ""
	}

	sections := make([]*imap.FetchItemBodySection, 0, len(wanted))
	for _, p := range wanted {
		sections = append(sections, &imap.FetchItemBodySection{
			Peek: true,
			Part: p.path,
			// Cap the transfer server-side: truncating after download still
			// pulls a 20 MB newsletter across the wire to read a fraction.
			Partial: &imap.SectionPartial{Offset: 0, Size: int64(config.MaxEmailBodySize)},
		})
	}

	raw := fetchSections(c, uid, sections)
	var plainParts, htmlParts []string
	for _, p := range wanted {
		body, ok := raw[partKey(p.path)]
		if !ok {
			continue
		}
		decoded := decodePart(body, p.encoding, p.charset)
		if p.html {
			htmlParts = append(htmlParts, decoded)
		} else {
			plainParts = append(plainParts, decoded)
		}
	}
	// Sibling inline parts (a body followed by a disclaimer, say) are pieces of
	// one message, so they are joined rather than one replacing the other.
	return strings.Join(plainParts, "\n\n"), strings.Join(htmlParts, "")
}

// selectTextParts walks the body structure and returns the text leaves that
// make up the readable body, in document order.
//
// Two rules do the work. Inside a multipart/alternative only the first part of
// each type counts: the alternatives are the same content twice, not two pieces
// of content. Anywhere else (multipart/mixed, multipart/related) sibling text
// parts are additive. Parts marked as attachments are skipped so a .txt
// attachment never stands in for the body.
func selectTextParts(bs imap.BodyStructure) []textPart {
	alternativeAt := make(map[string]bool)
	claimed := make(map[string]bool) // parent path + type, for alternatives
	var out []textPart

	bs.Walk(func(path []int, part imap.BodyStructure) bool {
		if multi, ok := part.(*imap.BodyStructureMultiPart); ok {
			alternativeAt[partKey(path)] = strings.EqualFold(multi.Subtype, "alternative")
			return true
		}
		single, ok := part.(*imap.BodyStructureSinglePart)
		if !ok {
			return true
		}
		mediaType := single.MediaType()
		if mediaType != "text/plain" && mediaType != "text/html" {
			return true
		}
		if disp := single.Disposition(); disp != nil && strings.EqualFold(disp.Value, "attachment") {
			return true
		}
		if len(out) >= maxTextParts {
			return false
		}

		isHTML := mediaType == "text/html"
		var parent string
		if len(path) > 0 {
			parent = partKey(path[:len(path)-1])
		}
		if alternativeAt[parent] {
			key := parent + "/" + mediaType
			if claimed[key] {
				return true
			}
			claimed[key] = true
		}

		p := textPart{
			path:     append([]int(nil), path...),
			encoding: single.Encoding,
			charset:  single.Params["charset"],
			html:     isHTML,
		}
		// A non-multipart message reports no path; its body is section 1.
		if len(p.path) == 0 {
			p.path = []int{1}
		}
		out = append(out, p)
		return true
	})
	return out
}

// fetchSections runs one FETCH for the given sections and returns each part's
// raw (still transfer-encoded) bytes, keyed by part path.
func fetchSections(c *imapclient.Client, uid imap.UID, sections []*imap.FetchItemBodySection) map[string][]byte {
	out := make(map[string][]byte, len(sections))

	cmd := c.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{
		UID:         true,
		BodySection: sections,
	})
	defer cmd.Close()

	for msg := cmd.Next(); msg != nil; msg = cmd.Next() {
		for item := msg.Next(); item != nil; item = msg.Next() {
			v, ok := item.(imapclient.FetchItemDataBodySection)
			if !ok || v.Literal == nil || v.Section == nil {
				continue
			}
			buf := new(bytes.Buffer)
			// Belt and braces alongside the server-side Partial: a server that
			// ignores the partial range must not be able to stream us an
			// unbounded body.
			if _, err := io.Copy(buf, io.LimitReader(v.Literal, int64(config.MaxEmailBodySize))); err != nil {
				continue
			}
			out[partKey(v.Section.Part)] = buf.Bytes()
		}
	}
	return out
}

// partKey renders an IMAP part path ("1", "1.2") so a fetched section can be
// matched back to the part that was asked for.
func partKey(path []int) string {
	parts := make([]string, 0, len(path))
	for _, n := range path {
		parts = append(parts, strconv.Itoa(n))
	}
	return strings.Join(parts, ".")
}

// decodePart reverses the part's Content-Transfer-Encoding and then its
// charset, in that order. Skipping this step left quoted-printable bodies full
// of "=E2=80=99" runs and "=" soft line breaks, and base64 bodies unreadable.
func decodePart(raw []byte, encoding, charsetName string) string {
	decoded := decodeTransferEncoding(raw, encoding)
	return decodeCharset(decoded, charsetName)
}

func decodeTransferEncoding(raw []byte, encoding string) []byte {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "quoted-printable":
		// A body truncated mid-sequence by the size cap makes the reader stop
		// early; keep what decoded cleanly rather than dropping the message.
		out, _ := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(raw)))
		return out
	case "base64":
		// Line breaks are part of the encoding, and a size-capped tail may not
		// land on a 4-character boundary, so trim to one before decoding.
		clean := stripWhitespace(raw)
		if rem := len(clean) % 4; rem != 0 {
			clean = clean[:len(clean)-rem]
		}
		out := make([]byte, base64.StdEncoding.DecodedLen(len(clean)))
		n, err := base64.StdEncoding.Decode(out, clean)
		if err != nil && n == 0 {
			return raw
		}
		return out[:n]
	default:
		// 7bit, 8bit, binary, or absent: the bytes are the content.
		return raw
	}
}

func stripWhitespace(b []byte) []byte {
	out := b[:0:0]
	for _, c := range b {
		switch c {
		case ' ', '\t', '\r', '\n':
		default:
			out = append(out, c)
		}
	}
	return out
}

// decodeCharset converts a part's bytes to UTF-8. An unknown or unsupported
// charset falls back to the raw bytes: a mis-decoded accent beats an empty
// message.
func decodeCharset(data []byte, charsetName string) string {
	name := strings.ToLower(strings.TrimSpace(charsetName))
	if name == "" || name == "utf-8" || name == "utf8" || name == "us-ascii" || name == "ascii" {
		return string(data)
	}
	r, err := charset.Reader(name, bytes.NewReader(data))
	if err != nil {
		return string(data)
	}
	converted, err := io.ReadAll(r)
	if err != nil {
		return string(data)
	}
	return string(converted)
}

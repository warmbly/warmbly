package imap

import (
	"testing"

	"github.com/emersion/go-imap/v2"
)

func TestDecodePartQuotedPrintable(t *testing.T) {
	raw := []byte("Hi Ana,=0D=0A=0D=0AThat=E2=80=99s 100=25 fine =E2=80=94 caf=C3=A9 =\r\nis open.")
	got := decodePart(raw, "quoted-printable", "utf-8")
	want := "Hi Ana,\r\n\r\nThat’s 100% fine — café is open."
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDecodePartBase64(t *testing.T) {
	raw := []byte("SGVsbG8sCgpUaGlzIGlzIHRoZSBmaXJzdCBwYXJhZ3JhcGgu\r\nCgpUaGFuayB5b3Uu\r\n")
	got := decodePart(raw, "BASE64", "")
	want := "Hello,\n\nThis is the first paragraph.\n\nThank you."
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDecodePartBase64TruncatedTail(t *testing.T) {
	// The size cap can cut base64 mid-quantum; what decoded cleanly must survive.
	raw := []byte("SGVsbG8sIHdvcmxkIQ==")[:10]
	if got := decodePart(raw, "base64", ""); got != "Hello," {
		t.Fatalf("got %q", got)
	}
}

func TestDecodePartLatin1(t *testing.T) {
	raw := []byte{'c', 'a', 'f', 0xE9} // café in ISO-8859-1
	if got := decodePart(raw, "8bit", "iso-8859-1"); got != "café" {
		t.Fatalf("got %q", got)
	}
}

func TestDecodePartUnknownCharsetFallsBackToRaw(t *testing.T) {
	if got := decodePart([]byte("plain"), "7bit", "x-made-up"); got != "plain" {
		t.Fatalf("got %q", got)
	}
}

func TestSelectTextPartsAlternative(t *testing.T) {
	bs := &imap.BodyStructureMultiPart{
		Subtype: "alternative",
		Children: []imap.BodyStructure{
			&imap.BodyStructureSinglePart{Type: "text", Subtype: "plain", Encoding: "quoted-printable", Params: map[string]string{"charset": "UTF-8"}},
			&imap.BodyStructureSinglePart{Type: "text", Subtype: "html", Encoding: "base64"},
		},
	}
	parts := selectTextParts(bs)
	if len(parts) != 2 {
		t.Fatalf("expected both alternatives, got %d", len(parts))
	}
	if partKey(parts[0].path) != "1" || parts[0].html || parts[0].encoding != "quoted-printable" || parts[0].charset != "UTF-8" {
		t.Fatalf("plain part wrong: %+v", parts[0])
	}
	// The HTML part must resolve to its own section, not the plain part's.
	if partKey(parts[1].path) != "2" || !parts[1].html || parts[1].encoding != "base64" {
		t.Fatalf("html part wrong: %+v", parts[1])
	}
}

func TestSelectTextPartsAlternativeKeepsOnePerType(t *testing.T) {
	// Some mailers list several flavours of the same alternative; only the
	// first of each counts, or the body renders twice.
	bs := &imap.BodyStructureMultiPart{
		Subtype: "alternative",
		Children: []imap.BodyStructure{
			&imap.BodyStructureSinglePart{Type: "text", Subtype: "plain"},
			&imap.BodyStructureSinglePart{Type: "text", Subtype: "plain"},
			&imap.BodyStructureSinglePart{Type: "text", Subtype: "html"},
			&imap.BodyStructureSinglePart{Type: "text", Subtype: "html"},
		},
	}
	parts := selectTextParts(bs)
	if len(parts) != 2 || partKey(parts[0].path) != "1" || partKey(parts[1].path) != "3" {
		t.Fatalf("unexpected selection: %+v", parts)
	}
}

func TestSelectTextPartsMixedSiblingsAreAdditive(t *testing.T) {
	// A body part followed by an inline disclaimer: both belong to the message.
	bs := &imap.BodyStructureMultiPart{
		Subtype: "mixed",
		Children: []imap.BodyStructure{
			&imap.BodyStructureSinglePart{Type: "text", Subtype: "plain"},
			&imap.BodyStructureSinglePart{Type: "text", Subtype: "plain"},
		},
	}
	parts := selectTextParts(bs)
	if len(parts) != 2 {
		t.Fatalf("expected both inline parts, got %+v", parts)
	}
}

func TestSelectTextPartsNestedWithAttachment(t *testing.T) {
	bs := &imap.BodyStructureMultiPart{
		Subtype: "mixed",
		Children: []imap.BodyStructure{
			&imap.BodyStructureMultiPart{
				Subtype: "alternative",
				Children: []imap.BodyStructure{
					&imap.BodyStructureSinglePart{Type: "text", Subtype: "plain"},
					&imap.BodyStructureSinglePart{Type: "text", Subtype: "html"},
				},
			},
			&imap.BodyStructureSinglePart{
				Type: "text", Subtype: "plain",
				Extended: &imap.BodyStructureSinglePartExt{
					Disposition: &imap.BodyStructureDisposition{Value: "attachment"},
				},
			},
		},
	}
	parts := selectTextParts(bs)
	if len(parts) != 2 {
		t.Fatalf("attachment should be skipped, got %+v", parts)
	}
	if partKey(parts[0].path) != "1.1" || partKey(parts[1].path) != "1.2" {
		t.Fatalf("nested part paths wrong: %+v", parts)
	}
}

func TestSelectTextPartsSinglePartMessage(t *testing.T) {
	bs := &imap.BodyStructureSinglePart{Type: "text", Subtype: "plain"}
	parts := selectTextParts(bs)
	if len(parts) != 1 || partKey(parts[0].path) != "1" || parts[0].html {
		t.Fatalf("unexpected selection: %+v", parts)
	}
}

func TestSelectTextPartsIsBounded(t *testing.T) {
	children := make([]imap.BodyStructure, 0, 12)
	for i := 0; i < 12; i++ {
		children = append(children, &imap.BodyStructureSinglePart{Type: "text", Subtype: "plain"})
	}
	bs := &imap.BodyStructureMultiPart{Subtype: "mixed", Children: children}
	if parts := selectTextParts(bs); len(parts) != maxTextParts {
		t.Fatalf("expected the cap to apply, got %d", len(parts))
	}
}

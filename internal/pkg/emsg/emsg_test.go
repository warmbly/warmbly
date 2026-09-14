package emsg

import (
	"bytes"
	"testing"
)

// Helper: round-trip encode → decode → compare
func roundTrip(t *testing.T, blob *EmailBlob) *EmailBlob {
	data, err := blob.EncodeBinary()
	if err != nil {
		t.Fatalf("EncodeBinary() failed: %v", err)
	}

	out, err := DecodeBinary(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeBinary() failed: %v", err)
	}

	return out
}

func TestEmailBlob_EncodeDecode(t *testing.T) {
	tests := []struct {
		name      string
		input     EmailBlob
		wantPlain []byte
		wantHTML  []byte
	}{
		{
			name:      "empty blob",
			input:     EmailBlob{},
			wantPlain: nil,
			wantHTML:  nil,
		},
		{
			name: "plain text only",
			input: EmailBlob{
				PlainText: []byte("Hello, world!"),
			},
			wantPlain: []byte("Hello, world!"),
		},
		{
			name: "HTML only",
			input: EmailBlob{
				HTMLBody: []byte("<p>Hello!</p>"),
			},
			wantHTML: []byte("<p>Hello!</p>"),
		},
		{
			name: "both sections",
			input: EmailBlob{
				PlainText: []byte("Hi"),
				HTMLBody:  []byte("<b>Hi</b>"),
			},
			wantPlain: []byte("Hi"),
			wantHTML:  []byte("<b>Hi</b>"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := roundTrip(t, &tt.input)

			if !bytes.Equal(got.PlainText, tt.wantPlain) {
				t.Errorf("PlainText mismatch:\n got=%q\nwant=%q", got.PlainText, tt.wantPlain)
			}
			if !bytes.Equal(got.HTMLBody, tt.wantHTML) {
				t.Errorf("HTMLBody mismatch:\n got=%q\nwant=%q", got.HTMLBody, tt.wantHTML)
			}
		})
	}
}

func TestEmailBlob_Attachments(t *testing.T) {
	in := &EmailBlob{
		PlainText: []byte("hi"),
		HTMLBody:  []byte("<b>hi</b>"),
		Attachments: []Attachment{
			{S3Key: "attachments/a/1.pdf", Filename: "report.pdf", MimeType: "application/pdf"},
			{S3Key: "attachments/a/2.png", Filename: "logo.png", MimeType: "image/png"},
		},
	}

	got := roundTrip(t, in)

	if len(got.Attachments) != len(in.Attachments) {
		t.Fatalf("attachment count mismatch: got=%d want=%d", len(got.Attachments), len(in.Attachments))
	}
	for i, want := range in.Attachments {
		if got.Attachments[i] != want {
			t.Errorf("attachment[%d] mismatch:\n got=%+v\nwant=%+v", i, got.Attachments[i], want)
		}
	}
	if !bytes.Equal(got.PlainText, in.PlainText) || !bytes.Equal(got.HTMLBody, in.HTMLBody) {
		t.Errorf("body sections corrupted when attachments present")
	}
}

func TestEmailBlob_NoAttachments(t *testing.T) {
	got := roundTrip(t, &EmailBlob{PlainText: []byte("x")})
	if len(got.Attachments) != 0 {
		t.Errorf("expected no attachments, got %d", len(got.Attachments))
	}
}

// The from-name section is trailing and flagged, so a blob written without it
// decodes as before and one written with it round-trips alongside attachments.
func TestEmailBlob_FromName(t *testing.T) {
	out := roundTrip(t, &EmailBlob{
		PlainText:   []byte("Hi"),
		Attachments: []Attachment{{S3Key: "k", Filename: "deck.pdf", MimeType: "application/pdf"}},
		FromName:    "Renée Doe, Jr.",
	})
	if out.FromName != "Renée Doe, Jr." {
		t.Errorf("FromName = %q", out.FromName)
	}
	if len(out.Attachments) != 1 || out.Attachments[0].Filename != "deck.pdf" {
		t.Errorf("attachments did not survive alongside the from name: %+v", out.Attachments)
	}

	legacy := roundTrip(t, &EmailBlob{PlainText: []byte("Hi")})
	if legacy.FromName != "" {
		t.Errorf("blob without a from name decoded one: %q", legacy.FromName)
	}
	data, _ := (&EmailBlob{PlainText: []byte("Hi")}).EncodeBinary()
	if binaryFlags(data)&FlagFromName != 0 {
		t.Error("empty from name set the flag")
	}
}

// The from-email section is trailing too, and sits after the from name, so
// both orders of "one set, the other not" have to decode.
func TestEmailBlob_FromEmail(t *testing.T) {
	out := roundTrip(t, &EmailBlob{
		PlainText:   []byte("Hi"),
		Attachments: []Attachment{{S3Key: "k", Filename: "deck.pdf", MimeType: "application/pdf"}},
		FromName:    "Renée Doe",
		FromEmail:   "hello@acme.com",
	})
	if out.FromEmail != "hello@acme.com" || out.FromName != "Renée Doe" {
		t.Errorf("identity did not round-trip: name=%q email=%q", out.FromName, out.FromEmail)
	}
	if len(out.Attachments) != 1 {
		t.Errorf("attachments did not survive alongside the identity: %+v", out.Attachments)
	}

	// A send with an alias but no display name: the decoder must not read the
	// address into the name section.
	aliasOnly := roundTrip(t, &EmailBlob{PlainText: []byte("Hi"), FromEmail: "hello@acme.com"})
	if aliasOnly.FromName != "" || aliasOnly.FromEmail != "hello@acme.com" {
		t.Errorf("alias-only blob decoded wrong: name=%q email=%q", aliasOnly.FromName, aliasOnly.FromEmail)
	}

	nameOnly := roundTrip(t, &EmailBlob{PlainText: []byte("Hi"), FromName: "Jane"})
	if nameOnly.FromEmail != "" {
		t.Errorf("blob without an alias decoded one: %q", nameOnly.FromEmail)
	}

	data, _ := (&EmailBlob{PlainText: []byte("Hi")}).EncodeBinary()
	if binaryFlags(data)&FlagFromEmail != 0 {
		t.Error("empty from email set the flag")
	}
}

func binaryFlags(data []byte) uint32 {
	return uint32(data[5])<<24 | uint32(data[6])<<16 | uint32(data[7])<<8 | uint32(data[8])
}

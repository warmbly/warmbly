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

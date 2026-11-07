package attachmenttype

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestDelimitedAttachmentRecordLimits(t *testing.T) {
	const limit = 4096
	for _, format := range []struct {
		name, separator, mime string
	}{
		{"comma.csv", ",", "text/csv"},
		{"semicolon.csv", ";", "text/csv"},
		{"tab.csv", "\t", "text/csv"},
		{"tab.tsv", "\t", "text/tab-separated-values"},
	} {
		for _, fields := range []int{limit - 1, limit, limit + 1} {
			for _, quoted := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/quoted=%t", format.name, fields, quoted), func(t *testing.T) {
					field := "Renée 東京"
					if quoted {
						field = `"Renée ` + format.separator + ` ""東京""` + "\r\nnext line\""
					}
					// The trailing delimiter supplies the final empty field.
					body := []byte("\ufeff" + strings.Repeat(field+format.separator, fields-1) + "\r\n")
					before := bytes.Clone(body)
					got, err := Validate(format.name, body)
					if fields > limit {
						if !errors.Is(err, ErrInvalid) || got != (Type{}) {
							t.Fatalf("type=%+v error=%v", got, err)
						}
					} else if err != nil || got.MIME != format.mime {
						t.Fatalf("type=%+v error=%v", got, err)
					}
					if !bytes.Equal(body, before) {
						t.Fatal("input bytes changed")
					}
				})
			}
		}
	}
}

func TestDelimitedAttachmentLogicalRecords(t *testing.T) {
	for name, body := range map[string]string{
		"many rows":      strings.Repeat("a,b\r\n", 8193),
		"quoted commas":  `"` + strings.Repeat(",", 8193) + `",b`,
		"ragged records": "a,b,c\nx\n\"line\r\nbreak\",\"escaped \"\"quote\"\"\"\r\n",
		"blank lines":    "\n\r\na,b\r\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Validate("data.csv", []byte(body)); err != nil {
				t.Fatal(err)
			}
		})
	}
	body := []byte(strings.Repeat("x,", 4096) + "x")
	if _, err := Validate("notes.txt", body); err != nil {
		t.Fatal("plain text must retain its text profile:", err)
	}
}

// Package attachmenttype validates new campaign files without rewriting approved bytes.
package attachmenttype

import (
	"bytes"
	"errors"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"path"
	"strings"

	"golang.org/x/image/webp"

	csv "github.com/warmbly/warmbly/internal/pkg/boundedcsv"
	"github.com/warmbly/warmbly/internal/pkg/imagetype"
	"github.com/warmbly/warmbly/internal/pkg/spreadsheet"
	"github.com/warmbly/warmbly/internal/pkg/textcontent"
)

const MaxBytes = 15 << 20

const maxDelimitedFieldsPerRecord = 4096

// Type is the canonical outgoing media type of a validated file.
type Type struct {
	MIME      string
	Extension string
}

var ErrInvalid = errors.New("unsupported or malformed attachment; use a supported file with its matching extension")

// Validate checks the final filename extension and bounded content, ignoring client MIME.
func Validate(filename string, body []byte) (Type, error) {
	if len(body) == 0 || len(body) > MaxBytes {
		return Type{}, ErrInvalid
	}
	ext := strings.ToLower(path.Ext(filename))
	t := Type{Extension: ext}
	var ok bool
	switch ext {
	case ".pdf":
		t.MIME, ok = "application/pdf", validPDF(body)
	case ".docx", ".xlsx", ".pptx":
		t.MIME, ok = officeType(body, ext)
	case ".zip":
		t.MIME = "application/zip"
		_, err := readZIP(body)
		ok = err == nil
	case ".txt", ".csv", ".tsv":
		t.MIME = "text/plain"
		ok = textcontent.Validate(body) == nil && strings.HasPrefix(http.DetectContentType(bytes.TrimPrefix(body, []byte("\ufeff"))), "text/plain")
		if ext != ".txt" {
			if ext == ".csv" {
				t.MIME = "text/csv"
			} else {
				t.MIME = "text/tab-separated-values"
			}
			r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(body, []byte("\ufeff"))))
			r.FieldsPerRecord = -1
			r.MaxFieldsPerRecord = maxDelimitedFieldsPerRecord
			r.Comma = spreadsheet.Delimiter(body)
			if ext == ".tsv" {
				r.Comma = '\t'
			}
			if ok {
				for {
					_, err := r.Read()
					if err == io.EOF {
						break
					}
					if err != nil {
						ok = false
						break
					}
				}
			}
		}
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		t.MIME, ok = rasterType(body, ext)
	}
	if !ok {
		return Type{}, ErrInvalid
	}
	return t, nil
}

func rasterType(body []byte, ext string) (string, bool) {
	expected, _ := imagetype.ByExt(ext)
	f, cfg, err := imagetype.Detect(body, expected)
	if err != nil || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > 16_000_000 {
		return "", false
	}
	var decoded image.Image
	switch f.MIME {
	case "image/png":
		decoded, err = png.Decode(bytes.NewReader(body))
	case "image/jpeg":
		decoded, err = jpeg.Decode(bytes.NewReader(body))
	case "image/webp":
		decoded, err = webp.Decode(bytes.NewReader(body))
	case "image/gif":
		// Bound all frames before DecodeAll allocates their pixel buffers.
		if !boundedGIF(body) {
			return "", false
		}
		var g *gif.GIF
		g, err = gif.DecodeAll(bytes.NewReader(body))
		if err == nil && len(g.Image) > 0 {
			decoded = g.Image[0]
		}
	}
	return f.MIME, err == nil && decoded != nil
}

func boundedGIF(b []byte) bool {
	if len(b) < 13 {
		return false
	}
	pos := 13
	if b[10]&128 != 0 {
		pos += 3 * (1 << (1 + (b[10] & 7)))
	}
	pixels, frames := 0, 0
	blocks := func() bool {
		for pos < len(b) {
			n := int(b[pos])
			pos++
			if n == 0 {
				return true
			}
			pos += n
		}
		return false
	}
	for pos < len(b) {
		tag := b[pos]
		pos++
		switch tag {
		case 0x3b:
			return frames > 0 && pos == len(b)
		case 0x21:
			pos++
			if !blocks() {
				return false
			}
		case 0x2c:
			if pos+9 > len(b) {
				return false
			}
			w := int(b[pos+4]) | int(b[pos+5])<<8
			h := int(b[pos+6]) | int(b[pos+7])<<8
			pixels += w * h
			frames++
			if pixels > 16_000_000 || frames > 256 {
				return false
			}
			flags := b[pos+8]
			pos += 9
			if flags&128 != 0 {
				pos += 3 * (1 << (1 + (flags & 7)))
			}
			pos++
			if !blocks() {
				return false
			}
		default:
			return false
		}
	}
	return false
}

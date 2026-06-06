package emsg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const Magic = "EMSG"
const Version = 1

// Bitmask flags for sections
const (
	FlagPlainText   uint32 = 1 << 0
	FlagHTMLBody    uint32 = 1 << 1
	FlagAttachments uint32 = 1 << 2
)

// Attachment is a single attachment reference carried inside the S3 body blob.
// Only the metadata travels here; the worker fetches the bytes from object
// storage by S3Key. Keeping these refs in the blob (not the Avro Kafka event)
// preserves the published worker event contract.
type Attachment struct {
	S3Key    string
	Filename string
	MimeType string
}

// EmailBlob represents a binary-encoded email body and metadata.
type EmailBlob struct {
	PlainText   []byte
	HTMLBody    []byte
	Attachments []Attachment
}

// EncodeBinary serializes the blob into binary format. Layout:
//
//	"EMSG" | version(1) | flags(4) | [plain] | [html] | [attachments]
//
// where each body section is uint32-length-prefixed and only present when its
// flag bit is set. The attachments section, when present, is a uint32 count
// followed by that many (s3key, filename, mimetype) triples of length-prefixed
// strings. Attachment metadata travels here (inside the S3 body blob), not in
// the Avro Kafka event, so the published worker event contract is unchanged.
func (b *EmailBlob) EncodeBinary() ([]byte, error) {
	var flags uint32
	parts := make([][]byte, 0, 2)

	if len(b.PlainText) > 0 {
		flags |= FlagPlainText
		parts = append(parts, b.PlainText)
	}
	if len(b.HTMLBody) > 0 {
		flags |= FlagHTMLBody
		parts = append(parts, b.HTMLBody)
	}
	if len(b.Attachments) > 0 {
		flags |= FlagAttachments
	}

	buf := new(bytes.Buffer)

	// Write header
	buf.WriteString(Magic)                     // 4 bytes
	buf.WriteByte(Version)                     // 1 byte
	binary.Write(buf, binary.BigEndian, flags) // 4 bytes

	// Write each variable-length body section [len][data] in flag order.
	for _, p := range parts {
		binary.Write(buf, binary.BigEndian, uint32(len(p)))
		buf.Write(p)
	}

	// Attachments section: [count] then [len][str] x3 per attachment.
	if flags&FlagAttachments != 0 {
		binary.Write(buf, binary.BigEndian, uint32(len(b.Attachments)))
		writeStr := func(s string) {
			binary.Write(buf, binary.BigEndian, uint32(len(s)))
			buf.WriteString(s)
		}
		for _, a := range b.Attachments {
			writeStr(a.S3Key)
			writeStr(a.Filename)
			writeStr(a.MimeType)
		}
	}

	return buf.Bytes(), nil
}

// DecodeBinary parses a binary blob into an EmailBlob struct.
func DecodeBinary(r io.Reader) (*EmailBlob, error) {
	header := make([]byte, 9)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	if string(header[:4]) != Magic {
		return nil, errors.New("invalid magic header")
	}
	version := header[4]
	if version != Version {
		return nil, fmt.Errorf("unsupported version: %d", version)
	}

	flags := binary.BigEndian.Uint32(header[5:9])

	readSection := func() ([]byte, error) {
		var length uint32
		if err := binary.Read(r, binary.BigEndian, &length); err != nil {
			return nil, err
		}
		data := make([]byte, length)
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, err
		}
		return data, nil
	}

	b := &EmailBlob{}

	if flags&FlagPlainText != 0 {
		b.PlainText, _ = readSection()
	}
	if flags&FlagHTMLBody != 0 {
		b.HTMLBody, _ = readSection()
	}

	if flags&FlagAttachments != 0 {
		var count uint32
		if err := binary.Read(r, binary.BigEndian, &count); err != nil {
			return nil, err
		}
		readStr := func() (string, error) {
			data, err := readSection()
			if err != nil {
				return "", err
			}
			return string(data), nil
		}
		b.Attachments = make([]Attachment, 0, count)
		for i := uint32(0); i < count; i++ {
			s3Key, err := readStr()
			if err != nil {
				return nil, err
			}
			filename, err := readStr()
			if err != nil {
				return nil, err
			}
			mimeType, err := readStr()
			if err != nil {
				return nil, err
			}
			b.Attachments = append(b.Attachments, Attachment{
				S3Key:    s3Key,
				Filename: filename,
				MimeType: mimeType,
			})
		}
	}

	return b, nil
}

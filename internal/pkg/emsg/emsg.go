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
	FlagPlainText uint32 = 1 << 0
	FlagHTMLBody  uint32 = 1 << 1
)

// EmailBlob represents a binary-encoded email body and metadata.
type EmailBlob struct {
	PlainText []byte
	HTMLBody  []byte
}

// EncodeBinary serializes the blob into binary format.
func (b *EmailBlob) EncodeBinary() ([]byte, error) {
	var flags uint32
	parts := make([][]byte, 0, 5)

	if len(b.PlainText) > 0 {
		flags |= FlagPlainText
		parts = append(parts, b.PlainText)
	}
	if len(b.HTMLBody) > 0 {
		flags |= FlagHTMLBody
		parts = append(parts, b.HTMLBody)
	}

	buf := new(bytes.Buffer)

	// Write header
	buf.WriteString(Magic)                     // 4 bytes
	buf.WriteByte(Version)                     // 1 byte
	binary.Write(buf, binary.BigEndian, flags) // 4 bytes

	// Write each section [len][data]
	for _, p := range parts {
		binary.Write(buf, binary.BigEndian, uint32(len(p)))
		buf.Write(p)
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

	return b, nil
}

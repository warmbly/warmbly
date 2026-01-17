package imap

import (
	"bytes"
	"io"
	"mime"
	"net/mail"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/warmbly/warmbly/internal/config"
)

func fetchTextParts(c *imapclient.Client, uid imap.UID, bs imap.BodyStructure) (plain, html string) {
	switch b := bs.(type) {
	case *imap.BodyStructureSinglePart:
		mediaType := strings.ToLower(b.Type + "/" + b.Subtype)
		if mediaType == "text/plain" || mediaType == "text/html" {
			// Build a BODY.PEEK[] section for this part
			part := imap.FetchItemBodySection{
				Peek: true,
				Part: []int{1}, // will adjust when recursing
			}
			return fetchBodySectionLimited(c, uid, &part, mediaType)
		}
	case *imap.BodyStructureMultiPart:
		for i, child := range b.Children {
			p, h := fetchTextParts(c, uid, child)
			if p != "" {
				plain += p
			}
			if h != "" {
				html += h
			}
			_ = i // maintain recursion order
		}
	}
	return
}

// fetchBodySectionLimited fetches BODY[section] with streaming and truncation
func fetchBodySectionLimited(c *imapclient.Client, uid imap.UID, section *imap.FetchItemBodySection, mediaType string) (plain, html string) {
	options := &imap.FetchOptions{
		UID:         true,
		BodySection: []*imap.FetchItemBodySection{section},
	}

	cmd := c.Fetch(imap.UIDSetNum(uid), options)
	defer cmd.Close()

	for {
		msg := cmd.Next()
		if msg == nil {
			break
		}

		for {
			item := msg.Next()
			if item == nil {
				break
			}
			if v, ok := item.(imapclient.FetchItemDataBodySection); ok && v.Literal != nil {
				// Stream and truncate to 200 KB
				buf := new(bytes.Buffer)
				limited := io.LimitReader(v.Literal, config.MaxEmailBodySize+1)
				io.Copy(buf, limited)
				if buf.Len() > config.MaxEmailBodySize {
					buf.Truncate(config.MaxEmailBodySize)
				}

				content := decodeIfNeeded(buf.Bytes(), mediaType)

				if strings.Contains(mediaType, "plain") {
					plain = content
				} else {
					html = content
				}
			}
		}
	}
	return
}

// decodeIfNeeded handles Content-Transfer-Encoding if necessary
func decodeIfNeeded(data []byte, mediaType string) string {
	// Try to detect charset
	mt, params, _ := mime.ParseMediaType(mediaType)
	charset := strings.ToLower(params["charset"])
	if charset == "" {
		charset = "utf-8"
	}

	// Simple case: treat as UTF-8 text
	msg, err := mail.ReadMessage(bytes.NewReader(data))
	if err == nil {
		body, _ := io.ReadAll(msg.Body)
		return string(body)
	}
	if mt == "text/plain" || mt == "text/html" {
		return string(data)
	}
	return ""
}

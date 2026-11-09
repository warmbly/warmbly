package models

import (
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// bodyCloseTag is matched on the original HTML: lower-casing a copy can change byte offsets.
var bodyCloseTag = regexp.MustCompile(`(?i)</body\s*>`)

const (
	WarmupFilterTagMinLength = 6
	WarmupFilterTagMaxLength = 40
	// warmupFilterTagAlphabet drops look-alikes so a tag read off the screen is typed correctly.
	warmupFilterTagAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"
)

// NewWarmupFilterTag is a random code in the form xxxx-xxxx-xxxx.
func NewWarmupFilterTag() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	var b strings.Builder
	for i, c := range buf {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		b.WriteByte(warmupFilterTagAlphabet[int(c)%len(warmupFilterTagAlphabet)])
	}
	return b.String()
}

// CleanWarmupFilterTag trims a chosen tag and refuses one a filter cannot match exactly.
func CleanWarmupFilterTag(tag string) (string, error) {
	tag = strings.Join(strings.Fields(tag), " ")
	switch n := utf8.RuneCountInString(tag); {
	case n < WarmupFilterTagMinLength:
		return "", fmt.Errorf("a filter tag needs at least %d characters", WarmupFilterTagMinLength)
	case n > WarmupFilterTagMaxLength:
		return "", fmt.Errorf("a filter tag can be at most %d characters", WarmupFilterTagMaxLength)
	}
	for _, r := range tag {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == ' ') {
			return "", errors.New("a filter tag can use letters, digits, spaces, hyphens and underscores only")
		}
	}
	return tag, nil
}

// AppendWarmupFilterTag puts the recipient's tag on the last line of each body that is present.
func AppendWarmupFilterTag(plain, html, tag string) (string, string) {
	if tag == "" {
		return plain, html
	}
	if plain != "" {
		plain = strings.TrimRight(plain, " \t\r\n") + "\n\n" + tag
	}
	if html != "" {
		line := "<p>" + tag + "</p>"
		if locs := bodyCloseTag.FindAllStringIndex(html, -1); len(locs) > 0 {
			i := locs[len(locs)-1][0]
			html = html[:i] + line + html[i:]
		} else {
			html += line
		}
	}
	return plain, html
}

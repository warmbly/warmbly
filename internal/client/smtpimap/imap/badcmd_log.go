package imap

import (
	"bytes"
	"regexp"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

// refusedCommandLog logs each command a server answers with BAD, so a server
// that refuses part of the sync can be diagnosed from the worker log. Only
// tagged command lines are kept: LOGIN and AUTHENTICATE arguments are
// replaced, message data is never stored, and each line is cut at 200
// characters. ponytail: every session's traffic passes through this line
// splitter; gate it behind a setting if it ever shows up in a profile.

var taggedLine = regexp.MustCompile(`^(T[0-9]+) (.*)$`)

type refusedCommandLog struct {
	mu      sync.Mutex
	pending []byte
	sent    map[string]string
}

func newRefusedCommandLog() *refusedCommandLog {
	return &refusedCommandLog{sent: map[string]string{}}
}

func (l *refusedCommandLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pending = append(l.pending, p...)
	for {
		i := bytes.IndexByte(l.pending, '\n')
		if i < 0 {
			if len(l.pending) > 4096 {
				l.pending = l.pending[:0]
			}
			return len(p), nil
		}
		l.line(strings.TrimRight(string(l.pending[:i]), "\r"))
		l.pending = l.pending[i+1:]
	}
}

func (l *refusedCommandLog) line(s string) {
	m := taggedLine.FindStringSubmatch(s)
	if m == nil {
		return
	}
	tag, rest := m[1], m[2]
	upper := strings.ToUpper(rest)
	switch {
	case strings.HasPrefix(upper, "BAD"):
		log.Warn().Str("command", l.sent[tag]).Str("reply", cut(rest)).Msg("imap server refused command")
		delete(l.sent, tag)
	case strings.HasPrefix(upper, "OK"), strings.HasPrefix(upper, "NO"):
		delete(l.sent, tag)
	case strings.HasPrefix(upper, "LOGIN"), strings.HasPrefix(upper, "AUTHENTICATE"):
		l.sent[tag] = strings.Fields(rest)[0] + " <hidden>"
	default:
		if len(l.sent) > 64 {
			l.sent = map[string]string{}
		}
		l.sent[tag] = cut(rest)
	}
}

func cut(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

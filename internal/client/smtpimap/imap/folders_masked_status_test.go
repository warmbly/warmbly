package imap

import (
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

// nameInStatus captures the mailbox of a STATUS reply, quoted or bare.
var nameInStatus = regexp.MustCompile(`^\* STATUS (?:"([^"]*)"|(\S+)) \(`)

// maskNonASCIIStatus stands in for NetEase Enterprise Mail (qiye.163.com),
// which answers STATUS for a folder with a non-ASCII name under the literal
// name "???" instead of the name the client asked for. Names travel as
// modified UTF-7, so the '&' that opens a shift sequence marks a non-ASCII
// name. Replies are only rewritten while enabled is set.
func maskNonASCIIStatus(enabled *atomic.Bool) func(string) string {
	return func(line string) string {
		if !enabled.Load() {
			return line
		}
		m := nameInStatus.FindStringSubmatch(line)
		if m == nil {
			return line
		}
		name := m[1]
		if name == "" {
			name = m[2]
		}
		if !strings.Contains(name, "&") {
			return line
		}
		return nameInStatus.ReplaceAllString(line, `* STATUS "???" (`)
	}
}

// A server that names the folder "???" in its STATUS reply used to fail the
// whole listing as "server unreachable", because go-imap drops a reply it
// cannot pair with the command and the zero cursors were then refused. The
// cursors must come out the same as when the server answers normally.
func TestFoldersSurviveMaskedStatusReplies(t *testing.T) {
	var mask atomic.Bool
	c, wire := rewritingServer(t, nil, nil, maskNonASCIIStatus(&mask), "已发送", "已删除", "Templates")

	want, err := c.Folders()
	if err != nil {
		t.Fatalf("Folders: %s", err.Message)
	}
	if len(want) != 4 {
		t.Fatalf("got %d folders, want INBOX and three more", len(want))
	}
	if n := len(wire.commands("EXAMINE")); n != 0 {
		t.Fatalf("a listing answered normally sent %d EXAMINE commands, want none", n)
	}

	mask.Store(true)
	got, err := c.Folders()
	if err != nil {
		t.Fatalf("Folders with masked STATUS replies: %s", err.Message)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d folders with masked replies, want %d", len(got), len(want))
	}
	byName := make(map[string]models.Mailbox, len(got))
	for _, box := range got {
		byName[box.Name] = box
	}
	for _, w := range want {
		g, ok := byName[w.Name]
		if !ok {
			t.Errorf("folder %q missing from the masked listing", w.Name)
			continue
		}
		if g.UIDValidity == 0 || g.UIDNext == 0 {
			t.Errorf("folder %q has empty cursors %+v", w.Name, g)
		}
		if g.UIDValidity != w.UIDValidity || g.UIDNext != w.UIDNext || g.Messages != w.Messages {
			t.Errorf("folder %q: cursors %+v differ from the normal listing %+v", w.Name, g, w)
		}
	}
	if n := len(wire.commands("EXAMINE")); n != 2 {
		t.Errorf("sent %d EXAMINE commands, want one for each of the two masked folders", n)
	}
}

// Only a reply that went unpaired is read another way: folders whose names
// survive on the wire never cost an extra command.
func TestMaskedStatusLeavesAsciiFoldersAlone(t *testing.T) {
	var mask atomic.Bool
	mask.Store(true)
	c, wire := rewritingServer(t, nil, nil, maskNonASCIIStatus(&mask), "Archive", "Templates")

	folders, err := c.Folders()
	if err != nil {
		t.Fatalf("Folders: %s", err.Message)
	}
	if len(folders) != 3 {
		t.Fatalf("got %d folders, want INBOX and two more", len(folders))
	}
	if n := len(wire.commands("EXAMINE")); n != 0 {
		t.Fatalf("sent %d EXAMINE commands for an ASCII listing, want none", n)
	}
}

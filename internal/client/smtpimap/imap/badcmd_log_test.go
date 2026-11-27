package imap

import "testing"

func TestRefusedCommandLogHidesLoginAndPairsTags(t *testing.T) {
	l := newRefusedCommandLog()
	_, _ = l.Write([]byte("T1 LOGIN user@example.com secret\r\nT1 OK done\r\nT2 STATUS \"Inbox\" (MESSAGES UIDNEXT)\r\n"))
	if _, ok := l.sent["T1"]; ok {
		t.Fatal("completed command was kept")
	}
	if got := l.sent["T2"]; got != `STATUS "Inbox" (MESSAGES UIDNEXT)` {
		t.Fatalf("T2 = %q", got)
	}
	_, _ = l.Write([]byte("T3 LOGIN user@example.com secret\r\n"))
	if got := l.sent["T3"]; got != "LOGIN <hidden>" {
		t.Fatalf("login kept as %q", got)
	}
	_, _ = l.Write([]byte("T2 BAD Command Argument Error. 12\r\n"))
	if _, ok := l.sent["T2"]; ok {
		t.Fatal("refused command was kept after logging")
	}
}

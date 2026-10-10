package imap

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/warmbly/warmbly/internal/models"
)

// The in-process server has no CONDSTORE and no LIST-STATUS, which is exactly
// the shape this package used to refuse to talk to (Outlook.com, Microsoft
// 365 over IMAP, Yahoo, plenty of hosted servers). Every test here runs
// against it, so "works on a plain RFC 3501 server" is a thing CI checks
// rather than a thing we believe.
func testServer(t *testing.T, caps imap.CapSet, folders ...string) *Client {
	t.Helper()
	return clientTo(t, startMemServer(t, caps, folders...))
}

// startMemServer runs the in-process server and returns its address.
func startMemServer(t *testing.T, caps imap.CapSet, folders ...string) string {
	t.Helper()
	mem := imapmemserver.New()
	user := imapmemserver.NewUser("warmbly@test", "hunter2")
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatalf("create INBOX: %v", err)
	}
	for _, f := range folders {
		if err := user.Create(f, nil); err != nil {
			t.Fatalf("create %q: %v", f, err)
		}
	}
	mem.AddUser(user)

	if caps == nil {
		caps = imap.CapSet{imap.CapIMAP4rev1: {}}
	}
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		Caps:         caps,
		InsecureAuth: true,
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	return ln.Addr().String()
}

// clientTo points a Client at an already-running server.
func clientTo(t *testing.T, addr string) *Client {
	t.Helper()
	host, port, _ := net.SplitHostPort(addr)
	c := &Client{
		Email:    "warmbly@test",
		AuthType: models.AuthPlain,
		Credentials: &models.Service{
			Username: "warmbly@test",
			Password: "hunter2",
			Host:     host,
			Port:     atoi(port),
		},
		// The in-process server speaks plaintext; no product path does.
		plaintext: true,
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// wireLog is every command the client sent.
//
// The in-process server is lenient: go-imap's own parser takes fetch items
// the advertised capabilities do not cover, which is exactly how #405 shipped
// past a package whose tests all run against a plain RFC 3501 server. A real
// one answers BAD, so what has to be asserted is the command on the wire, not
// whether this server happened to accept it.
type wireLog struct {
	mu    sync.Mutex
	lines []string
}

func (w *wireLog) add(line string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lines = append(w.lines, line)
}

// commands returns every recorded line whose command word matches, uppercased.
func (w *wireLog) commands(name string) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for _, l := range w.lines {
		// "<tag> <COMMAND> ..." and UID-prefixed forms.
		fields := strings.Fields(strings.ToUpper(l))
		if len(fields) >= 2 && (fields[1] == name || (fields[1] == "UID" && len(fields) >= 3 && fields[2] == name)) {
			out = append(out, strings.ToUpper(l))
		}
	}
	return out
}

// recordingServer is startMemServer behind a proxy that records the client's
// half of the conversation.
func recordingServer(t *testing.T, caps imap.CapSet, folders ...string) (*Client, *wireLog) {
	t.Helper()
	return interceptingServer(t, caps, nil, folders...)
}

// interceptingServer is recordingServer with a stand-in for a stricter server:
// when refuse returns a non-empty reply for a command line, the proxy answers
// with it instead of forwarding the command.
func interceptingServer(t *testing.T, caps imap.CapSet, refuse func(line string) string, folders ...string) (*Client, *wireLog) {
	t.Helper()
	return rewritingServer(t, caps, refuse, nil, folders...)
}

// rewritingServer is interceptingServer that can also rewrite what the server
// says, line by line, to stand in for a server whose dialect it does not speak.
func rewritingServer(t *testing.T, caps imap.CapSet, refuse, rewrite func(line string) string, folders ...string) (*Client, *wireLog) {
	t.Helper()
	upstream := startMemServer(t, caps, folders...)
	log := &wireLog{}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			down, err := ln.Accept()
			if err != nil {
				return
			}
			up, err := net.Dial("tcp", upstream)
			if err != nil {
				_ = down.Close()
				return
			}
			toClient := &lockedWriter{w: down}
			go func() {
				defer func() { _ = down.Close() }()
				if rewrite == nil {
					_, _ = io.Copy(toClient, up)
					return
				}
				r := bufio.NewReader(up)
				for {
					line, err := r.ReadString('\n')
					if line != "" {
						if _, werr := toClient.Write([]byte(rewrite(line))); werr != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}()
			go func() {
				defer func() { _ = up.Close() }()
				r := bufio.NewReader(down)
				for {
					line, err := r.ReadString('\n')
					if line != "" {
						trimmed := strings.TrimRight(line, "\r\n")
						log.add(trimmed)
						if refuse != nil {
							if reply := refuse(trimmed); reply != "" {
								if _, werr := toClient.Write([]byte(reply + "\r\n")); werr != nil {
									return
								}
								continue
							}
						}
						if _, werr := up.Write([]byte(line)); werr != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}()
		}
	}()

	return clientTo(t, ln.Addr().String()), log
}

// lockedWriter keeps the proxy's own replies from interleaving with the
// server's bytes.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

// A server without CONDSTORE must connect. Refusing it is what left
// Outlook.com and Yahoo mailboxes unable to sync at all.
func TestConnectWithoutCondStore(t *testing.T) {
	c := testServer(t, nil)
	if err := c.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if c.HasCondStore() {
		t.Error("HasCondStore on a server that does not advertise it")
	}
}

// Without LIST-STATUS the cursors have to come from a STATUS per folder.
// Skipping the folders instead (what the old code did) made the account look
// empty and retired every saved cursor with nothing logged.
func TestFoldersWithoutListStatus(t *testing.T) {
	c := testServer(t, nil, "Sent", "Archive")
	if err := c.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	boxes, err := c.Folders()
	if err != nil {
		t.Fatalf("Folders: %v", err)
	}
	if len(boxes) != 3 {
		t.Fatalf("listed %d folders, want 3: %+v", len(boxes), boxes)
	}
	for _, b := range boxes {
		if b.UIDValidity == 0 {
			t.Errorf("%q came back with no UIDVALIDITY; its cursor would be meaningless", b.Name)
		}
		if b.UIDNext == 0 {
			t.Errorf("%q came back with no UIDNEXT, which is the incremental cursor here", b.Name)
		}
	}
}

// A nested folder must be listed: "%" stopped at the top level, which is how
// Gmail's [Gmail]/Sent Mail and Dovecot's INBOX.Sent went unsynced.
func TestFoldersListsNested(t *testing.T) {
	c := testServer(t, nil, "Clients", "Clients/Acme")
	if err := c.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	boxes, err := c.Folders()
	if err != nil {
		t.Fatalf("Folders: %v", err)
	}
	var names []string
	for _, b := range boxes {
		names = append(names, b.Name)
	}
	if !contains(names, "Clients/Acme") {
		t.Fatalf("nested folder missing from %v", names)
	}
}

// INBOX and the special folders survive the cap; the overflow is reported
// rather than failing the whole mailbox, which is what a user with a lot of
// folders used to get (silently).
func TestFoldersCapKeepsInboxAndSpecialFolders(t *testing.T) {
	var many []string
	for i := 0; i < 30; i++ {
		many = append(many, "Project"+string(rune('a'+i%26))+string(rune('0'+i/26)))
	}
	many = append(many, "Sent", "Trash")
	c := testServer(t, nil, many...)
	if err := c.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	boxes, err := c.foldersCapped(5)
	if err != nil {
		t.Fatalf("Folders: %v", err)
	}
	if len(boxes) != 5 {
		t.Fatalf("kept %d folders, want the cap of 5", len(boxes))
	}
	var names []string
	for _, b := range boxes {
		names = append(names, b.Name)
	}
	for _, want := range []string{"INBOX", "Sent", "Trash"} {
		if !contains(names, want) {
			t.Errorf("%q was cut; the inbox and special folders must survive the cap. kept: %v", want, names)
		}
	}
	if c.FolderOverflow() != len(many)+1-5 {
		t.Errorf("FolderOverflow = %d, want %d", c.FolderOverflow(), len(many)+1-5)
	}
}

// The UIDNEXT search is the incremental set without CONDSTORE: everything at
// or above the cursor, and nothing below it.
func TestSearchNewSince(t *testing.T) {
	c := testServer(t, nil)
	if err := c.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	for i := 0; i < 3; i++ {
		appendMessage(t, c, "INBOX", "<m"+string(rune('1'+i))+"@test>")
	}
	if _, err := c.SelectForSync("INBOX"); err != nil {
		t.Fatalf("SelectForSync: %v", err)
	}

	all, err := c.SearchNewSince(1)
	if err != nil {
		t.Fatalf("SearchNewSince(1): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("SearchNewSince(1) returned %d UIDs, want 3", len(all))
	}
	// A cursor past the end must return nothing. IMAP answers "n:*" with the
	// last message when n is beyond the end, so an unfiltered result would
	// re-offer the newest message on every quiet pass forever.
	none, err := c.SearchNewSince(uint32(all[len(all)-1]) + 1)
	if err != nil {
		t.Fatalf("SearchNewSince(past the end): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("a cursor past the end returned %v, want nothing", none)
	}
}

// The flag scan is how read state is mirrored without CONDSTORE, so it has to
// carry the Message-ID the platform keys on.
func TestFetchFlagsCarriesMessageID(t *testing.T) {
	c := testServer(t, nil)
	if err := c.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	appendMessage(t, c, "INBOX", "<flagme@test>")
	if _, err := c.SelectForSync("INBOX"); err != nil {
		t.Fatalf("SelectForSync: %v", err)
	}
	got, err := c.FetchFlags(context.Background(), 1)
	if err != nil {
		t.Fatalf("FetchFlags: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("FetchFlags returned %d messages, want 1", len(got))
	}
	for _, st := range got {
		if st.MessageID != "flagme@test" && st.MessageID != "<flagme@test>" {
			t.Errorf("MessageID = %q, want the message's own id", st.MessageID)
		}
	}
}

// A session the server has closed must be re-dialed rather than failing
// forever, and the re-dial has to happen without the caller knowing.
func TestEnsureConnectedRedialsAfterDrop(t *testing.T) {
	c := testServer(t, nil)
	if err := c.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if _, err := c.Folders(); err != nil {
		t.Fatalf("first Folders: %v", err)
	}

	// Drop it the way a server does: close the socket under the client.
	_ = c.conn.Conn.Close()
	waitForLogout(t, c)

	if _, err := c.Folders(); err != nil {
		t.Fatalf("Folders after the session was dropped: %v", err)
	}
}

// A peer that goes away without a FIN must not park a command forever.
//
// go-imap puts a 30 second deadline on a response it has already started
// reading, but between responses it clears the deadline entirely, and that is
// where the reader waits for the first byte of the answer to the command we
// just sent. A NAT or firewall that drops the mapping leaves the socket open
// and silent, so before idleConn that wait never ended: the mailbox stopped
// syncing until the worker restarted, the same zombie a dropped session used
// to cause.
func TestIdleConnBoundsTheWaitBetweenResponses(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	// Accept and then say nothing, holding the socket open.
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		select {}
	}()

	raw, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn := &idleConn{Conn: raw, timeout: 300 * time.Millisecond}
	release := conn.arm()
	defer release()

	// Exactly what go-imap does around each response: a deadline while it
	// decodes, cleared when it is done.
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	_ = conn.SetReadDeadline(time.Time{})

	start := time.Now()
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("a read against a silent peer succeeded")
	}
	if waited := time.Since(start); waited > 5*time.Second {
		t.Fatalf("the read waited %v; the cleared deadline left it unbounded", waited)
	}
}

// Between commands the deadline is released, because go-imap's reader sits in
// Read the whole time a session is idle and would otherwise time out a
// perfectly healthy connection.
func TestIdleConnReleasesTheDeadlineBetweenCommands(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		// Quiet for longer than the idle timeout, then speak: a mailbox with
		// no new mail looks exactly like this.
		time.Sleep(400 * time.Millisecond)
		_, _ = conn.Write([]byte("* OK still here\r\n"))
		time.Sleep(time.Second)
	}()

	raw, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn := &idleConn{Conn: raw, timeout: 100 * time.Millisecond}
	conn.arm()()

	if _, err := conn.Read(make([]byte, 1)); err != nil {
		t.Fatalf("an idle session was cut while no command was in flight: %v", err)
	}
}

// A large literal is allowed the time go-imap gives it: the wrapper fills in
// a missing deadline, it never shortens one, so a slow body fetch that is
// making progress is not failed at the idle timeout.
func TestIdleConnDoesNotShortenAnExistingDeadline(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		time.Sleep(300 * time.Millisecond)
		_, _ = conn.Write([]byte("x"))
		time.Sleep(time.Second)
	}()

	raw, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn := &idleConn{Conn: raw, timeout: 50 * time.Millisecond}
	release := conn.arm()
	defer release()
	// go-imap's literal read timeout, far longer than ours.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	if _, err := conn.Read(make([]byte, 1)); err != nil {
		t.Fatalf("a read go-imap had given 5s was cut at the idle timeout: %v", err)
	}
}

func appendMessage(t *testing.T, c *Client, mailbox, messageID string) {
	t.Helper()
	raw := "From: someone@test\r\nTo: warmbly@test\r\nSubject: hello\r\nMessage-ID: " + messageID +
		"\r\nDate: Mon, 2 Jan 2006 15:04:05 -0700\r\n\r\nbody\r\n"
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()
	cmd := c.client.Append(mailbox, int64(len(raw)), nil)
	if _, err := cmd.Write([]byte(raw)); err != nil {
		t.Fatalf("append write: %v", err)
	}
	if err := cmd.Close(); err != nil {
		t.Fatalf("append close: %v", err)
	}
	if _, err := cmd.Wait(); err != nil {
		t.Fatalf("append: %v", err)
	}
}

func waitForLogout(t *testing.T, c *Client) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.lifecycle.RLock()
		state := c.client.State()
		c.lifecycle.RUnlock()
		if state == imap.ConnStateLogout {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the client never noticed the dropped socket")
}

func contains(all []string, want string) bool {
	for _, s := range all {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}

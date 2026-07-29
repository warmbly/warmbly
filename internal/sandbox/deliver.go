package sandbox

import (
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/google/uuid"
)

// deliverToInbox appends a raw RFC822 message into a dovecot user's INBOX.
// This is the sandbox's "final delivery" hop: mail captured by mailpit is
// placed where the worker's real IMAP sync will find it. One short-lived
// connection per delivery keeps the client trivially correct.
func deliverToInbox(imapAddr, user, password string, raw []byte) error {
	c, err := imapclient.DialTLS(imapAddr, &imapclient.Options{
		// Dovecot's cert is self-signed; this client only ever talks to the
		// local sandbox container.
		TLSConfig: &tls.Config{InsecureSkipVerify: true},
	})
	if err != nil {
		return fmt.Errorf("imap dial %s: %w", imapAddr, err)
	}
	defer c.Close()

	if err := c.Login(user, password).Wait(); err != nil {
		return fmt.Errorf("imap login %s: %w", user, err)
	}

	cmd := c.Append("INBOX", int64(len(raw)), nil)
	if _, err := cmd.Write(raw); err != nil {
		return fmt.Errorf("imap append write: %w", err)
	}
	if err := cmd.Close(); err != nil {
		return fmt.Errorf("imap append close: %w", err)
	}
	if _, err := cmd.Wait(); err != nil {
		return fmt.Errorf("imap append: %w", err)
	}
	if err := c.Logout().Wait(); err != nil {
		return fmt.Errorf("imap logout: %w", err)
	}
	return nil
}

// composeReply builds the RFC822 reply a contact sends back to a sandbox
// mailbox. In-Reply-To/References carry the original Message-ID so the
// consumer's reply attribution (tasks.message_id lookup) resolves, and
// automated replies carry Auto-Submitted so the reply classifier gates them
// out of human-reply stats.
func composeReply(fromName, fromAddr, toName, toAddr, subject, origMessageID, body string, automated bool) []byte {
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	mid := fmt.Sprintf("<%s@%s>", uuid.New().String(), domainOf(fromAddr))
	orig := "<" + strings.Trim(origMessageID, "<>") + ">"

	var b strings.Builder
	fmt.Fprintf(&b, "From: %q <%s>\r\n", fromName, fromAddr)
	fmt.Fprintf(&b, "To: %q <%s>\r\n", toName, toAddr)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "Message-ID: %s\r\n", mid)
	fmt.Fprintf(&b, "In-Reply-To: %s\r\n", orig)
	fmt.Fprintf(&b, "References: %s\r\n", orig)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	if automated {
		b.WriteString("Auto-Submitted: auto-replied\r\n")
		b.WriteString("X-Autoreply: yes\r\n")
		b.WriteString("Precedence: auto_reply\r\n")
	}
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	b.WriteString("\r\n")
	return []byte(b.String())
}

func domainOf(addr string) string {
	if at := strings.LastIndex(addr, "@"); at >= 0 {
		return addr[at+1:]
	}
	return "sandbox.test"
}

package imap

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

// gmailListAttrs stands in for Gmail's special-use attributes, which the in-process server never sends.
func gmailListAttrs(line string) string {
	for name, attr := range map[string]string{`"[Gmail]/Spam"`: `\Junk`, `"[Gmail]/All Mail"`: `\All`, `"[Gmail]/Important"`: `\Important`} {
		if !strings.HasPrefix(line, "* LIST (") || !strings.Contains(line, name) {
			continue
		}
		rest := strings.TrimPrefix(line, "* LIST (")
		if strings.HasPrefix(rest, ")") {
			return "* LIST (" + attr + rest
		}
		return "* LIST (" + attr + " " + rest
	}
	return line
}

func gmailShapedServer(t *testing.T) *Client {
	t.Helper()
	caps := imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapMove: {}, imap.CapUIDPlus: {}}
	c, _ := rewritingServer(t, caps, nil, gmailListAttrs, "[Gmail]/Spam", "[Gmail]/All Mail", "[Gmail]/Important")
	if err := c.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	return c
}

// Engagement needs Gmail's views by attribute, which the sync listing deliberately leaves out.
func TestAllFoldersKeepsGmailViewsWithTheirAttributes(t *testing.T) {
	c := gmailShapedServer(t)
	boxes, err := c.AllFolders(context.Background())
	if err != nil {
		t.Fatalf("AllFolders: %v", err)
	}
	for attr, want := range map[imap.MailboxAttr]string{imap.MailboxAttrJunk: "[Gmail]/Spam", imap.MailboxAttrAll: "[Gmail]/All Mail", imap.MailboxAttrImportant: "[Gmail]/Important"} {
		if got := FindSpecialUse(boxes, attr); got == nil || got.Name != want {
			t.Fatalf("%s resolved to %+v, want %q", attr, got, want)
		}
	}
	synced, merr := c.Folders()
	if merr != nil {
		t.Fatalf("Folders: %v", merr)
	}
	for _, b := range synced {
		if b.Name == "[Gmail]/All Mail" || b.Name == "[Gmail]/Important" {
			t.Fatalf("the sync listing now includes the virtual view %q", b.Name)
		}
	}
}

func TestCopyToFolderLeavesTheSourceCopy(t *testing.T) {
	c := gmailShapedServer(t)
	ctx := context.Background()
	appendMessage(t, c, "INBOX", "<warm@test>")
	uid, err := c.FindUIDByMessageID(ctx, "INBOX", "<warm@test>")
	if err != nil || uid == 0 {
		t.Fatalf("find in INBOX: uid=%d err=%v", uid, err)
	}
	if err := c.CopyToFolder(ctx, "INBOX", "[Gmail]/Important", uid); err != nil {
		t.Fatalf("copy: %v", err)
	}
	for _, box := range []string{"INBOX", "[Gmail]/Important"} {
		if got, err := c.FindUIDByMessageID(ctx, box, "<warm@test>"); err != nil || got == 0 {
			t.Fatalf("%s lost the message: uid=%d err=%v", box, got, err)
		}
	}
}

// Gmail answers a bearer token without full mail access in the SASL challenge, not a tagged NO.
func TestOAuthBearerRefusalIsAnAuthenticationFailure(t *testing.T) {
	challenge := base64.StdEncoding.EncodeToString([]byte(`{"status":"400","schemes":"Bearer","scope":"https://mail.google.com/"}`))
	for name, reply := range map[string]func(tag string) string{
		"challenge":  func(string) string { return "+ " + challenge },
		"tagged no":  func(tag string) string { return tag + " NO [AUTHENTICATIONFAILED] Invalid credentials (Failure)" },
		"alert text": func(tag string) string { return tag + " NO [ALERT] Please log in via your web browser (Failure)" },
	} {
		t.Run(name, func(t *testing.T) {
			caps := imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapSASLIR: {}}
			c, _ := interceptingServer(t, caps, func(line string) string {
				fields := strings.Fields(line)
				if len(fields) >= 3 && strings.EqualFold(fields[1], "AUTHENTICATE") {
					return reply(fields[0])
				}
				return ""
			})
			c.AuthType = models.AuthOAuth2
			c.Oauth2 = &models.Oauth2Service{Host: c.Credentials.Host, Port: c.Credentials.Port, Token: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "token"})}
			merr := c.Connect()
			if merr == nil || merr.Code != errx.MailErrorCodeAuthenticationFailed {
				t.Fatalf("Connect = %v, want AUTHENTICATION_FAILED", merr)
			}
		})
	}
}

func TestIMAPDisabledReadsGmailsAlert(t *testing.T) {
	c := &Client{AuthType: models.AuthOAuth2}
	for text, want := range map[string]bool{
		"IMAP access is disabled for your domain. Please contact your domain administrator for questions about this feature. (Failure)": true,
		"Your account is not enabled for IMAP use. Please visit your Gmail settings page and enable your account for IMAP access.":      true,
		"Too many simultaneous connections. (Failure)": false,
	} {
		err := c.handleError(&imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeAlert, Text: text})
		if got := IMAPDisabled(err); got != want {
			t.Fatalf("IMAPDisabled(%q) = %v, want %v", text, got, want)
		}
	}
	if IMAPDisabled(errx.ErrMailServerUnreachable) {
		t.Fatal("a network failure read as IMAP being switched off")
	}
}

// A SELECT the server refuses is an error for the strict lookup and an absent message for the lenient one.
func TestFindUIDInFolderReportsARefusedSelect(t *testing.T) {
	c := gmailShapedServer(t)
	ctx := context.Background()
	if uid, err := c.FindUIDInFolder(ctx, "[Gmail]/Missing", "<warm@test>"); err == nil || uid != 0 {
		t.Fatalf("strict lookup: uid=%d err=%v, want an error", uid, err)
	}
	if uid, err := c.FindUIDByMessageID(ctx, "[Gmail]/Missing", "<warm@test>"); err != nil || uid != 0 {
		t.Fatalf("lenient lookup: uid=%d err=%v, want no match", uid, err)
	}
}

package wmail

import (
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

// One message with no Message-ID header used to end every sync pass: the empty
// string is not a map key, the internal endpoint refuses it with 400, and
// controlPlaneError holds the cursors, so the same batch came back forever.
func TestEnsureMessageKeySynthesizesAStableKey(t *testing.T) {
	w := &WMail{SmtpImapData: &SmtpImapData{folderPath: "INBOX", mailbox: 42}}

	msg := &models.EmailMessageData{UID: 17}
	w.ensureMessageKey(msg)
	if msg.MessageID == "" {
		t.Fatal("a message with no Message-ID still has no key")
	}

	// Stable: the next pass has to recognise the message as already stored.
	again := &models.EmailMessageData{UID: 17}
	w.ensureMessageKey(again)
	if again.MessageID != msg.MessageID {
		t.Errorf("key is not stable across passes: %q then %q", msg.MessageID, again.MessageID)
	}

	other := &models.EmailMessageData{UID: 18}
	w.ensureMessageKey(other)
	if other.MessageID == msg.MessageID {
		t.Errorf("two UIDs share the key %q", other.MessageID)
	}
}

func TestEnsureMessageKeyLeavesARealIDAlone(t *testing.T) {
	w := &WMail{SmtpImapData: &SmtpImapData{folderPath: "INBOX"}}

	msg := &models.EmailMessageData{MessageID: "<real@example.test>", UID: 3}
	w.ensureMessageKey(msg)
	if msg.MessageID != "<real@example.test>" {
		t.Errorf("MessageID = %q, want the header's own value", msg.MessageID)
	}

	// A header that is present but blank is no more of a key than a missing one.
	blank := &models.EmailMessageData{MessageID: "   ", UID: 4}
	w.ensureMessageKey(blank)
	if blank.MessageID == "   " {
		t.Error("a whitespace-only Message-ID was kept as the map key")
	}
}

// An In-Reply-To whose entries are blank is the same wedge one step along: the
// empty parent id would go to the map endpoint as a key.
func TestThreadParentIDSkipsBlankEntries(t *testing.T) {
	if got := threadParentID(&models.EmailMessageData{InReplyTo: []string{"<a@x>", "  "}}); got != "<a@x>" {
		t.Errorf("threadParentID = %q, want the last non-blank id", got)
	}
	if got := threadParentID(&models.EmailMessageData{InReplyTo: []string{"", " "}}); got != "" {
		t.Errorf("threadParentID = %q, want no parent", got)
	}
	if got := threadParentID(nil); got != "" {
		t.Errorf("threadParentID(nil) = %q, want no parent", got)
	}
}

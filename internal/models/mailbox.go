package models

import "time"

// Mailbox is one folder of an account. Its identity is Name, which IMAP
// guarantees is unique per account; UIDValidity is not an identity and never
// was, since RFC 3501 only promises UIDs are stable within one folder and
// servers deriving the number from a creation time give a whole folder tree
// the same one.
type Mailbox struct {
	Name  string   `json:"name" avro:"name"`
	Attrs []string `json:"attributes" avro:"attributes"`
	// UIDValidity is the validity marker for the UIDs held below: when the
	// server changes it, every stored uid for this folder is void and the
	// cursors have to be rebuilt.
	UIDValidity   uint32 `json:"uid_validity" avro:"uid_validity"`
	HighestModSeq uint64 `json:"highestmodseq" avro:"highestmodseq"`
	// UIDNext is the folder's next UID as last seen. It is the incremental
	// cursor on a server without CONDSTORE, where HighestModSeq stays 0.
	UIDNext uint32 `json:"uid_next" avro:"uid_next"`
	// Delim is the hierarchy delimiter this server reported for the folder
	// ("/" on Gmail, "." on many Dovecots). Empty when the server reported
	// none, where the leaf is guessed instead.
	Delim string `json:"delim,omitempty" avro:"delim"`

	UpdatedAt time.Time `json:"updated_at" avro:"updated_at"`
}

package models

import "time"

type Mailbox struct {
	Name          string   `json:"name"`
	Attrs         []string `json:"attributes"`
	UIDValidity   uint32   `json:"uid_validity"`
	HighestModSeq uint64   `json:"highestmodseq"`
	// NumMessages / UIDNext are LIST-STATUS EXISTS/UIDNEXT. Worker-only
	// fallbacks when CONDSTORE HighestModSeq stalls (seen on Hostinger).
	NumMessages uint32 `json:"num_messages,omitempty"`
	UIDNext     uint32 `json:"uid_next,omitempty"`

	UpdatedAt time.Time `json:"updated_at"`
}

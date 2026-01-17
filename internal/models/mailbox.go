package models

import "time"

type Mailbox struct {
	Name          string   `json:"name"`
	Attrs         []string `json:"attributes"`
	UIDValidity   uint32   `json:"uid_validity"`
	HighestModSeq uint64   `json:"highestmodseq"`

	UpdatedAt time.Time `json:"updated_at"`
}

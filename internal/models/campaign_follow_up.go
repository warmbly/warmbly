package models

import "strings"

// FollowUpSender is which mailbox a lead's follow-ups leave from
// (campaigns.follow_up_sender, checked in the database).
type FollowUpSender string

const (
	// FollowUpSameMailbox keeps every step on the address that sent the first.
	FollowUpSameMailbox FollowUpSender = "same"
	// FollowUpSameDomain rotates each step among mailboxes on the domain the
	// lead already hears from.
	FollowUpSameDomain FollowUpSender = "same_domain"
	// FollowUpRotate places each step through the campaign's rotation like a
	// first email.
	FollowUpRotate FollowUpSender = "rotate"
)

// Valid reports whether f is one of the stored values.
func (f FollowUpSender) Valid() bool {
	switch f {
	case FollowUpSameMailbox, FollowUpSameDomain, FollowUpRotate:
		return true
	}
	return false
}

// Rotates reports whether follow-ups are placed by rotation instead of pinned
// to the lead's mailbox. An unknown value (a row written by a newer release)
// keeps the pinned behaviour.
func (f FollowUpSender) Rotates() bool {
	return f == FollowUpSameDomain || f == FollowUpRotate
}

// SenderDomain is the lower-cased domain of the address a mailbox sends as,
// empty when it has none.
func (e *Email) SenderDomain() string {
	addr := strings.TrimSpace(e.SendFrom())
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(addr[at+1:]))
}

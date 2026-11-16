package errx

import (
	"errors"
	"fmt"
)

// ProviderCallError is a provider refusing a mailbox call that is not a send, such as
// opening a push channel. It reads as the call and its status, never as a refused send,
// and unwraps to the classified MailError so callers still read its disposition.
type ProviderCallError struct {
	Op     string
	Detail string
	Mail   *MailError
}

func (e *ProviderCallError) Error() string {
	s := e.Op
	if e.Mail != nil && e.Mail.Failure != nil && e.Mail.Failure.Status != 0 {
		s += fmt.Sprintf(": HTTP %d", e.Mail.Failure.Status)
	} else if e.Mail != nil {
		s += ": " + e.Mail.Error()
	}
	if e.Detail != "" {
		s += " " + e.Detail
	}
	return s
}

func (e *ProviderCallError) Unwrap() error {
	if e.Mail == nil {
		return nil
	}
	return e.Mail
}

// ProviderCall names op on a provider's refusal; detail is the provider's own reason when it has one.
// An error that is not a classified MailError keeps its text behind op.
func ProviderCall(op, detail string, err error) error {
	if err == nil {
		return nil
	}
	var mailErr *MailError
	if !errors.As(err, &mailErr) {
		return fmt.Errorf("%s: %w", op, err)
	}
	if detail == "" && mailErr.Failure != nil {
		detail = mailErr.Failure.Cause
	}
	return &ProviderCallError{Op: op, Detail: detail, Mail: mailErr}
}

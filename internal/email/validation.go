package email

import "net/mail"

func IsValid(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

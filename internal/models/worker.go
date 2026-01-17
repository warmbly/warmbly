package models

import (
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

type Worker struct {
	ID       uuid.UUID `json:"id"`
	IPAddr   string    `json:"ip_addr"`
	Active   bool      `json:"active"`
	FreeTier bool      `json:"free_tier"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UpdateWorker struct {
	IPAddr *string `json:"ip_addr"`
	Active *bool   `json:"active"`
}

type WorkerStatus string

const (
	WorkerStatusOffline WorkerStatus = "offline"
	WorkerStatusLoading WorkerStatus = "loading"
	WorkerStatusOnline  WorkerStatus = "online"
)

type SendEmail struct {
	To        []string     `json:"to"`
	Cc        []string     `json:"cc"`
	Bcc       []string     `json:"bcc"`
	Subject   string       `json:"subject"`
	BodyPlain string       `json:"body_plain"`
	BodyHTML  string       `json:"body_html"`
	Parent    *EmailParent `json:"parent"`
}

type AddWorkerEmailGoogleData struct {
	LastHistoryID uint64        `json:"last_history_id"`
	Token         *oauth2.Token `json:"token"`
}

type AddWorkerEmailSmtpImapData struct {
	Mailboxes   []Mailbox     `json:"mailboxes"`
	Token       *oauth2.Token `json:"token"`
	Credentials *SmtpImap     `json:"credentials"`
}

type AddWorkerEmail struct {
	ID        uuid.UUID                   `json:"id"`
	ImapSync  bool                        `json:"imap_sync"`
	Email     string                      `json:"email"`
	FirstName string                      `json:"first_name"`
	LastName  string                      `json:"last_name"`
	Type      InboxProvider               `json:"type"`
	Google    *AddWorkerEmailGoogleData   `json:"google"`
	SmtpImap  *AddWorkerEmailSmtpImapData `json:"smtp_imap"`

	Cfg oauth2.Config `json:"-"`
}

type RemoveWorkerEmail struct {
	UserID  string `json:"user_id"`
	EmailID string `json:"email_id"`
}

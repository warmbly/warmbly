package email

import "time"

const (
	MAX_BODY_SIZE   = 300 * 1024 // 300 kb
	MAX_HEADER_SIZE = 100 * 1024 // 100 kb
)

type Message struct {
	ID        string `json:"id"`
	MessageID string `json:"message_id"`

	ThreadID string   `json:"thread_id"`
	LabelIDs []string `json:"label_ids"`
	CC       []string `json:"cc"`
	BCC      []string `json:"bcc"`
	From     []string `json:"from"`
	To       []string `json:"to"`
	ReplyTo  string   `json:"reply_to"`

	Subject string `json:"subject"`
	Snippet string `json:"snippet"` // Smaller sentence of the message

	BodyPlain string `json:"body_plain"`
	BodyHTML  string `json:"body_html"`

	Saw bool `json:"saw"`

	SentDate     time.Time `json:"sent_date"`
	ReceivedDate time.Time `json:"received_date"`
}

type Service struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

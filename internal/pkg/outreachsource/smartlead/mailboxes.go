package smartlead

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

type account struct {
	ID            num     `json:"id"`
	FromName      string  `json:"from_name"`
	FromEmail     string  `json:"from_email"`
	Username      string  `json:"username"`
	Password      string  `json:"password"`
	SMTPHost      string  `json:"smtp_host"`
	SMTPPort      num     `json:"smtp_port"`
	SMTPPortType  string  `json:"smtp_port_type"`
	IMAPHost      string  `json:"imap_host"`
	IMAPPort      num     `json:"imap_port"`
	IMAPPortType  string  `json:"imap_port_type"`
	IMAPUsername  string  `json:"imap_username"`
	IMAPPassword  string  `json:"imap_password"`
	DifferentIMAP bool    `json:"is_different_imap_account"`
	MessagePerDay num     `json:"message_per_day"`
	Signature     *string `json:"signature"`
	ReplyTo       *string `json:"different_reply_to_address"`
	Type          string  `json:"type"`
	ClientID      num     `json:"client_id"`
	Tags          []struct {
		Name string `json:"tag_name"`
	} `json:"tags"`
	Warmup *struct {
		Status string `json:"status"`
	} `json:"warmup_details"`
}

const accountPage = 100

func (s *session) allAccounts(ctx context.Context) ([]account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accountsOK {
		return s.accounts, nil
	}
	var all []account
	for off := 0; ; off += accountPage {
		var raw json.RawMessage
		q := url.Values{"offset": {strconv.Itoa(off)}, "limit": {strconv.Itoa(accountPage)}}
		if err := s.c.Get(ctx, "/email-accounts/", q, &raw); err != nil {
			return nil, err
		}
		var page []account
		if err := decodeList(raw, &page, "data"); err != nil {
			return nil, fmt.Errorf("%s: decode email accounts: %w", label, err)
		}
		for _, a := range page {
			// The list's passwords are encoded; only the single-account read is plain.
			a.Password, a.IMAPPassword = "", ""
			all = append(all, a)
		}
		if len(page) < accountPage {
			break
		}
	}
	s.accounts, s.accountsOK = all, true
	return all, nil
}

func (s *session) accountsFor(ctx context.Context, ws source.Workspace) ([]account, error) {
	all, err := s.allAccounts(ctx)
	if err != nil {
		return nil, err
	}
	var out []account
	for _, a := range all {
		ok, err := s.owns(ctx, ws, a.ClientID.String())
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, a)
		}
	}
	return out, nil
}

func (s *session) Mailboxes(ctx context.Context, ws source.Workspace) ([]source.Mailbox, error) {
	accs, err := s.accountsFor(ctx, ws)
	if err != nil {
		return nil, err
	}
	out := make([]source.Mailbox, 0, len(accs))
	for _, a := range accs {
		m := mapMailbox(a)
		if m.Kind == source.MailboxSMTP {
			full, err := s.accountSecrets(ctx, a.ID.String())
			var ae *source.APIError
			switch {
			case errors.As(err, &ae):
				// One unreadable account is asked for again, not a failed import.
			case err != nil:
				return nil, err
			default:
				addSecrets(&m, full)
			}
		}
		out = append(out, m)
	}
	return out, nil
}

// accountSecrets reads one account, whose passwords Smartlead returns decoded.
func (s *session) accountSecrets(ctx context.Context, id string) (account, error) {
	var raw json.RawMessage
	if err := s.c.Get(ctx, "/email-accounts/"+url.PathEscape(id)+"/", nil, &raw); err != nil {
		return account{}, err
	}
	var a account
	if err := decodeOne(raw, &a); err != nil {
		return account{}, fmt.Errorf("%s: decode email account: %w", label, err)
	}
	return a, nil
}

func mapMailbox(a account) source.Mailbox {
	m := source.Mailbox{
		ID:       a.ID.String(),
		Email:    strings.ToLower(strings.TrimSpace(a.FromEmail)),
		FromName: strings.TrimSpace(a.FromName),
		DailyCap: a.MessagePerDay.Int(),
	}
	if a.Signature != nil {
		m.Signature = *a.Signature
	}
	if a.ReplyTo != nil {
		m.ReplyTo = strings.TrimSpace(*a.ReplyTo)
	}
	if a.Warmup != nil {
		on := strings.EqualFold(a.Warmup.Status, "ACTIVE")
		m.Warmup = &on
	}
	for _, t := range a.Tags {
		if n := strings.TrimSpace(t.Name); n != "" {
			m.Tags = append(m.Tags, n)
		}
	}
	switch strings.ToUpper(a.Type) {
	case "GMAIL":
		m.Kind = source.MailboxGoogle
		return m
	case "OUTLOOK":
		m.Kind = source.MailboxMicrosoft
		return m
	}
	m.Kind = source.MailboxSMTP
	m.SMTP = &source.Server{
		Host:     strings.TrimSpace(a.SMTPHost),
		Port:     a.SMTPPort.Int(),
		Security: security(a.SMTPPortType, a.SMTPPort.Int()),
		Username: strings.TrimSpace(a.Username),
	}
	user := strings.TrimSpace(a.IMAPUsername)
	if user == "" {
		user = m.SMTP.Username
	}
	m.IMAP = &source.Server{
		Host:     strings.TrimSpace(a.IMAPHost),
		Port:     a.IMAPPort.Int(),
		Security: security(a.IMAPPortType, a.IMAPPort.Int()),
		Username: user,
	}
	return m
}

func addSecrets(m *source.Mailbox, full account) {
	if m.SMTP == nil || m.IMAP == nil {
		return
	}
	m.SMTP.Password = full.Password
	m.IMAP.Password = full.IMAPPassword
	// Without a separate IMAP login the SMTP one signs in to both.
	if m.IMAP.Password == "" && !full.DifferentIMAP {
		m.IMAP.Password = full.Password
	}
}

func security(kind string, port int) source.Security {
	switch strings.ToUpper(strings.TrimSpace(kind)) {
	case "SSL":
		return source.SecuritySSL
	case "STARTTLS":
		return source.SecurityStartTLS
	case "NONE", "PLAIN":
		return source.SecurityNone
	}
	// "TLS" and a blank type are read from the port.
	switch port {
	case 465, 993:
		return source.SecuritySSL
	case 587, 143, 25, 2525:
		return source.SecurityStartTLS
	}
	return ""
}

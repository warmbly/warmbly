package graphkit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"slices"
	"sort"
	"time"
)

// The simulator's mailboxes and the partner's own settings.

func (f *Fake) mailboxLocked(id string) (*fakeMailbox, error) {
	m := f.mailboxes[id]
	if m == nil {
		return nil, fakeErr(http.StatusNotFound, CodeNotFound, "no such mailbox")
	}
	return m, nil
}

func (f *Fake) ListMailboxes(_ context.Context, externalID, domainID, _ string, _ int) (*Page[Mailbox], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	f.advanceLocked()
	out := &Page[Mailbox]{Data: []Mailbox{}}
	for _, m := range f.mailboxes {
		if m.external == externalID && (domainID == "" || m.m.DomainID == domainID) {
			out.Data = append(out.Data, *cloneJSON(m.m))
		}
	}
	sort.Slice(out.Data, func(i, j int) bool { return out.Data[i].ID < out.Data[j].ID })
	return out, nil
}

func (f *Fake) GetMailbox(_ context.Context, mailboxID string) (*Mailbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	f.advanceLocked()
	m, err := f.mailboxLocked(mailboxID)
	if err != nil {
		return nil, err
	}
	return cloneJSON(m.m), nil
}

func (f *Fake) UpdateMailbox(_ context.Context, mailboxID string, patch MailboxPatch, key string) (*Mailbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "mailbox:"+mailboxID, patch, func() (*Mailbox, error) {
		m, err := f.mailboxLocked(mailboxID)
		if err != nil {
			return nil, err
		}
		if patch.FirstName != nil {
			m.m.FirstName = *patch.FirstName
		}
		if patch.LastName != nil {
			m.m.LastName = *patch.LastName
		}
		f.emitLocked(EventMailboxUpdated, m.external, m.m)
		return cloneJSON(m.m), nil
	})
}

func (f *Fake) SetMailboxPhoto(_ context.Context, mailboxID, filename string, data []byte, key string) (*Mailbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sum := sha256.Sum256(data)
	return replay(f, key, "photo:"+mailboxID, hex.EncodeToString(sum[:]), func() (*Mailbox, error) {
		m, err := f.mailboxLocked(mailboxID)
		if err != nil {
			return nil, err
		}
		if len(data) > 2<<20 {
			return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "photos are at most 2 MB")
		}
		u := "https://api.graphkit.com/v1/infrastructure/mailboxes/" + mailboxID + "/photo"
		m.m.PhotoURL = &u
		f.emitLocked(EventMailboxUpdated, m.external, m.m)
		return cloneJSON(m.m), nil
	})
}

// fakePhoto is a 1x1 transparent PNG.
var fakePhoto = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\r\n-\xb4\x00\x00\x00\x00IEND\xaeB`\x82")

func (f *Fake) MailboxPhoto(_ context.Context, mailboxID string) ([]byte, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, "", err
	}
	m, err := f.mailboxLocked(mailboxID)
	if err != nil {
		return nil, "", err
	}
	if m.m.PhotoURL == nil {
		return nil, "", fakeErr(http.StatusNotFound, CodeNotFound, "no photo")
	}
	return append([]byte{}, fakePhoto...), "image/png", nil
}

func (f *Fake) CancelMailbox(_ context.Context, mailboxID, key string) (*Mailbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "cancel:"+mailboxID, nil, func() (*Mailbox, error) {
		m, err := f.mailboxLocked(mailboxID)
		if err != nil {
			return nil, err
		}
		switch m.m.Status {
		case MailboxCancelled, MailboxFailed, MailboxCancelling:
			return cloneJSON(m.m), nil
		case MailboxProvisioning:
			now := f.now().UTC()
			m.m.Status, m.m.CancelsAt, m.m.NextPeriodMonthlyMicros = MailboxCancelled, &now, nil
			f.emitLocked(EventMailboxCancelled, m.external, m.m)
			return cloneJSON(m.m), nil
		}
		at := f.now().UTC()
		if m.m.CurrentPeriodEnd != nil {
			at = *m.m.CurrentPeriodEnd
		}
		m.m.Status, m.m.CancelsAt, m.m.NextPeriodMonthlyMicros = MailboxCancelling, &at, nil
		f.emitLocked(EventMailboxUpdated, m.external, m.m)
		return cloneJSON(m.m), nil
	})
}

func (f *Fake) ResumeMailbox(_ context.Context, mailboxID, key string) (*Mailbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "resume:"+mailboxID, nil, func() (*Mailbox, error) {
		m, err := f.mailboxLocked(mailboxID)
		if err != nil {
			return nil, err
		}
		if m.m.Status == MailboxActive && m.m.CancelsAt == nil {
			return cloneJSON(m.m), nil
		}
		if m.m.Status != MailboxCancelling {
			return nil, fakeErr(http.StatusConflict, CodeStateConflict, "no cancellation is pending")
		}
		monthly := m.m.MonthlyMicros
		m.m.Status, m.m.CancelsAt, m.m.NextPeriodMonthlyMicros = MailboxActive, nil, &monthly
		f.emitLocked(EventMailboxUpdated, m.external, m.m)
		return cloneJSON(m.m), nil
	})
}

func (f *Fake) MailboxCredentials(_ context.Context, mailboxID string) (*Credentials, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	m, err := f.mailboxLocked(mailboxID)
	if err != nil {
		return nil, err
	}
	if (m.m.Connection.Method != ConnectPassword && !m.m.IsAdmin) || (m.m.Status != MailboxActive && m.m.Status != MailboxCancelling) {
		return nil, fakeErr(http.StatusConflict, CodeCredentialsUnavailable, "no credentials for this mailbox")
	}
	c := &Credentials{Email: m.m.Email, Username: m.m.Email, Password: m.password, AppPassword: m.password,
		IMAP: Server{Host: "imap.gmail.com", Port: 993, Security: "ssl"}, SMTP: Server{Host: "smtp.gmail.com", Port: 465, Security: "ssl"},
		LoginURL: "https://mail.google.com"}
	if m.m.Provider == ProviderMicrosoft {
		c.IMAP = Server{Host: "outlook.office365.com", Port: 993, Security: "ssl"}
		c.SMTP = Server{Host: "smtp.office365.com", Port: 587, Security: "starttls"}
		c.LoginURL = "https://outlook.office.com"
	}
	if m.m.IsAdmin {
		c.AdminConsoleURL = "https://admin.google.com"
		if m.m.Provider == ProviderMicrosoft {
			c.AdminConsoleURL = "https://admin.microsoft.com"
		}
	}
	return c, nil
}

// brokerScopes are the scopes a token for the account's mailboxes may carry: its own Google identity's, else the partner's.
func (f *Fake) brokerScopesLocked(external string) []string {
	var registered []string
	switch {
	case f.accountDel[external] != nil && f.accountDel[external].Google != nil:
		registered = f.accountDel[external].Google.Scopes
	case f.delegation != nil && f.delegation.Google != nil:
		registered = f.delegation.Google.Scopes
	}
	var out []string
	for _, sc := range registered {
		if slices.Contains(fakeBrokerScopes, sc) {
			out = append(out, sc)
		}
	}
	return out
}

func (f *Fake) MailboxAccessToken(_ context.Context, mailboxID string, scopes []string, key string) (*AccessToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "token:"+mailboxID, scopes, func() (*AccessToken, error) {
		m, err := f.mailboxLocked(mailboxID)
		if err != nil {
			return nil, err
		}
		if m.m.Provider != ProviderGoogle || m.m.Connection.Method != ConnectToken || !m.m.Connection.Ready {
			return nil, fakeErr(http.StatusConflict, CodeCredentialsUnavailable, "no token for this mailbox")
		}
		allowed := f.brokerScopesLocked(m.external)
		if len(allowed) == 0 {
			// A tokenmode domain with no registered scopes gets every brokerable one.
			allowed = fakeBrokerScopes
		}
		if len(scopes) == 0 {
			scopes = allowed
		}
		for _, sc := range scopes {
			if !slices.Contains(allowed, sc) {
				return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "scope not registered", map[string]any{"allowed": allowed})
			}
		}
		sum := sha256.Sum256([]byte(key + mailboxID))
		return &AccessToken{AccessToken: "sandbox-token-" + hex.EncodeToString(sum[:12]), TokenType: "Bearer",
			ExpiresAt: f.now().Add(time.Hour).UTC(), Scopes: append([]string{}, scopes...)}, nil
	})
}

// ---- partner ------------------------------------------------------------

func validDelegation(d Delegation, account bool) error {
	if d.Google != nil {
		if !account && d.Google.ClientID == "" {
			return fakeErr(http.StatusBadRequest, CodeInvalidRequest, "google.client_id is required")
		}
		brokerable := false
		for _, sc := range d.Google.Scopes {
			brokerable = brokerable || slices.Contains(fakeBrokerScopes, sc)
		}
		if len(d.Google.Scopes) == 0 || (account && d.Google.ClientID == "" && !brokerable) {
			return fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "google.scopes must include a scope GraphKit can broker", map[string]any{"allowed": fakeBrokerScopes})
		}
	}
	if d.Microsoft != nil && d.Microsoft.AppID == "" {
		return fakeErr(http.StatusBadRequest, CodeInvalidRequest, "microsoft.app_id is required")
	}
	return nil
}

func (f *Fake) PutDelegation(_ context.Context, d Delegation, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	d.UpdatedAt = nil
	_, err := replay(f, key, "delegation", d, func() (*struct{}, error) {
		if err := validDelegation(d, false); err != nil {
			return nil, err
		}
		now := f.now().UTC()
		f.delegation = cloneJSON(d)
		f.delegation.UpdatedAt = &now
		f.reconnectLocked("")
		return &struct{}{}, nil
	})
	return err
}

func (f *Fake) GetDelegation(context.Context) (*Delegation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	if f.delegation == nil {
		return &Delegation{}, nil
	}
	return cloneJSON(*f.delegation), nil
}

func (f *Fake) PutAccountDelegation(_ context.Context, externalID string, d Delegation, key string) (*Delegation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d.UpdatedAt = nil
	return replay(f, key, "delegation:"+externalID, d, func() (*Delegation, error) {
		if f.accounts[externalID] == nil {
			return nil, fakeErr(http.StatusNotFound, CodeAccountNotFound, "no such account")
		}
		if d.Empty() {
			return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "set google or microsoft; DELETE clears")
		}
		if err := validDelegation(d, true); err != nil {
			return nil, err
		}
		now := f.now().UTC()
		stored := cloneJSON(d)
		stored.UpdatedAt = &now
		f.accountDel[externalID] = stored
		f.reconnectLocked(externalID)
		return cloneJSON(*stored), nil
	})
}

func (f *Fake) GetAccountDelegation(_ context.Context, externalID string) (*Delegation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	if f.accounts[externalID] == nil {
		return nil, fakeErr(http.StatusNotFound, CodeAccountNotFound, "no such account")
	}
	if d := f.accountDel[externalID]; d != nil {
		return cloneJSON(*d), nil
	}
	return &Delegation{}, nil
}

func (f *Fake) DeleteAccountDelegation(_ context.Context, externalID, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, err := replay(f, key, "undelegate:"+externalID, nil, func() (*struct{}, error) {
		if f.accounts[externalID] == nil {
			return nil, fakeErr(http.StatusNotFound, CodeAccountNotFound, "no such account")
		}
		delete(f.accountDel, externalID)
		f.reconnectLocked(externalID)
		return &struct{}{}, nil
	})
	return err
}

func (f *Fake) GetPartnerSettings(context.Context) (*PartnerSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	out := f.settings
	return &out, nil
}

func (f *Fake) UpdatePartnerSettings(_ context.Context, in PartnerSettings, key string) (*PartnerSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "settings", in, func() (*PartnerSettings, error) {
		if in.RegistrantNotices != "" {
			if in.RegistrantNotices != "registrar" && in.RegistrantNotices != "partner" {
				return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "registrant_notices is registrar or partner")
			}
			f.settings.RegistrantNotices = in.RegistrantNotices
		}
		if in.DMARCPolicyDefault != "" {
			if !slices.Contains([]string{"none", "quarantine", "reject"}, in.DMARCPolicyDefault) {
				return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "dmarc_policy_default is none, quarantine or reject")
			}
			f.settings.DMARCPolicyDefault = in.DMARCPolicyDefault
		}
		out := f.settings
		return &out, nil
	})
}

// PutWebhook creates the secret on the first call and returns the current one on every call.
func (f *Fake) PutWebhook(_ context.Context, endpoint string, events []string, key string) (*WebhookEndpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "webhook", map[string]any{"url": endpoint, "events": events}, func() (*WebhookEndpoint, error) {
		if len(events) == 0 {
			events = []string{"*"}
		}
		now := f.now().UTC()
		if f.webhook == nil {
			f.webhook = &WebhookEndpoint{}
		}
		if f.secret == "" {
			f.secret = "whsec_" + f.id("fake")
		}
		f.webhook.URL, f.webhook.Events, f.webhook.UpdatedAt = endpoint, events, &now
		out := *cloneJSON(*f.webhook)
		secret := f.secret
		out.Secret = &secret
		return &out, nil
	})
}

func (f *Fake) GetWebhook(context.Context) (*WebhookEndpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	if f.webhook == nil {
		return nil, fakeErr(http.StatusNotFound, CodeNotFound, "no webhook endpoint")
	}
	return cloneJSON(*f.webhook), nil
}

// DeleteWebhook removes the endpoint. The secret stays, so a later PUT returns it, and the filter resets.
func (f *Fake) DeleteWebhook(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, err := replay(f, key, "webhook.delete", nil, func() (*struct{}, error) {
		if f.webhook == nil {
			return nil, fakeErr(http.StatusNotFound, CodeNotFound, "no webhook endpoint")
		}
		f.webhook = nil
		return &struct{}{}, nil
	})
	return err
}

// RotateWebhook replaces the secret; the previous one keeps signing for a day.
func (f *Fake) RotateWebhook(_ context.Context, key string) (*WebhookEndpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "rotate", nil, func() (*WebhookEndpoint, error) {
		if f.webhook == nil {
			return nil, fakeErr(http.StatusNotFound, CodeNotFound, "no webhook endpoint")
		}
		now := f.now().UTC()
		expires := now.Add(24 * time.Hour)
		f.secret = "whsec_" + f.id("fake")
		f.webhook.PreviousSecretExpiresAt, f.webhook.UpdatedAt = &expires, &now
		out := *cloneJSON(*f.webhook)
		secret := f.secret
		out.Secret = &secret
		return &out, nil
	})
}

func (f *Fake) Events(_ context.Context, after string, limit int) (*Page[Event], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	f.advanceLocked()
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "limit is at most 100")
	}
	start := 0
	if after != "" {
		start = -1
		for i, ev := range f.events {
			if ev.ID == after {
				start = i + 1
				break
			}
		}
		if start < 0 {
			return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "unknown cursor")
		}
	}
	end := min(start+limit, len(f.events))
	out := &Page[Event]{Data: append([]Event{}, f.events[start:end]...)}
	out.Pagination.HasMore = end < len(f.events)
	if len(out.Data) > 0 {
		out.Pagination.NextCursor = out.Data[len(out.Data)-1].ID
	}
	return out, nil
}

func (f *Fake) Balance(context.Context) (*PartnerBalance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refusedLocked(); err != nil {
		return nil, err
	}
	return &PartnerBalance{BalanceMicros: f.balance, LowThresholdMicros: 50_000_000, Currency: "USD"}, nil
}

var (
	_ API = (*Fake)(nil)
	_ API = (*Client)(nil)
)

func (f *Fake) PutMailboxSignInLink(_ context.Context, mailboxID, url string, expiresAt time.Time, key string) (*Mailbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "signin-link:"+mailboxID, url, func() (*Mailbox, error) {
		m, err := f.mailboxLocked(mailboxID)
		if err != nil {
			return nil, err
		}
		if m.m.Connection.Method != ConnectOAuth || m.m.Connection.Ready {
			return nil, fakeErr(http.StatusConflict, CodeStateConflict, "this mailbox does not wait for a sign-in")
		}
		at := expiresAt.UTC()
		m.m.Connection.SignIn = &ConnectionSignIn{LinkExpiresAt: &at}
		return cloneJSON(m.m), nil
	})
}

func (f *Fake) MailboxSignedIn(_ context.Context, mailboxID, key string) (*Mailbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "signed-in:"+mailboxID, nil, func() (*Mailbox, error) {
		m, err := f.mailboxLocked(mailboxID)
		if err != nil {
			return nil, err
		}
		if m.m.Connection.Method != ConnectOAuth {
			return nil, fakeErr(http.StatusConflict, CodeStateConflict, "this mailbox does not connect by sign-in")
		}
		now := f.now().UTC()
		m.m.Connection.Ready = true
		m.m.Connection.SignIn = &ConnectionSignIn{SignedInAt: &now}
		return cloneJSON(m.m), nil
	})
}

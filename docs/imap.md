# IMAP Integration

Warmbly supports IMAP/SMTP for non-Gmail email providers, including Outlook (with OAuth2) and standard IMAP servers.

## Overview

IMAP (Internet Message Access Protocol) is used for reading emails from mailboxes. Combined with SMTP for sending, it provides full email functionality for providers that don't offer a REST API.

## CondStore Extension

CondStore (RFC 7162) enables efficient incremental synchronization using modification sequences (MODSEQ).

### How It Works

1. Server assigns a MODSEQ to each message modification
2. Client stores the highest MODSEQ seen
3. On next sync, client requests only changes since that MODSEQ
4. Reduces bandwidth and processing compared to full sync

### IMAP Commands

```
# Check for CondStore support
A1 SELECT INBOX
* OK [HIGHESTMODSEQ 123456]

# Fetch changes since last sync
A2 FETCH 1:* (FLAGS) (CHANGEDSINCE 123456)
```

## Authentication

### Plain Authentication

Standard username/password authentication for most IMAP servers.

```
Hostname: imap.example.com
Port: 993 (SSL) or 143 (STARTTLS)
Username: user@example.com
Password: app-specific-password
```

### OAuth2 (XOAUTH2)

Used for Microsoft Outlook and other OAuth2-supporting providers.

```
# XOAUTH2 SASL mechanism
AUTH XOAUTH2 <base64-encoded-token>
```

The token format:
```
user=email@example.com^Aauth=Bearer <access_token>^A^A
```

## SMTP Sending

Outgoing emails are sent via SMTP, typically on port 587 with STARTTLS.

```
Hostname: smtp.example.com
Port: 587 (STARTTLS) or 465 (SSL)
```

## Folder Management

### UIDValidity

IMAP uses UIDValidity to detect mailbox changes. If UIDValidity changes, all cached UIDs are invalid and a full resync is required.

### Common Folders

| Folder | IMAP Name | Description |
|--------|-----------|-------------|
| Inbox | INBOX | Primary inbox |
| Sent | Sent, Sent Items, Sent Mail | Sent messages |
| Drafts | Drafts | Draft messages |
| Trash | Trash, Deleted Items | Deleted messages |
| Spam | Spam, Junk | Spam folder |

## Supported Providers

| Provider | IMAP Server | SMTP Server | Auth |
|----------|-------------|-------------|------|
| Outlook | outlook.office365.com:993 | smtp.office365.com:587 | OAuth2 |
| Yahoo | imap.mail.yahoo.com:993 | smtp.mail.yahoo.com:587 | App Password |
| Zoho | imap.zoho.com:993 | smtp.zoho.com:587 | App Password |
| Custom | configurable | configurable | Plain |

## Error Handling

| Error | Cause | Resolution |
|-------|-------|------------|
| LOGIN failed | Invalid credentials | Refresh token or update password |
| UIDVALIDITY changed | Mailbox reset | Full resync required |
| Connection timeout | Network issue | Retry with backoff |
| Too many connections | Rate limit | Queue and retry |

## Code References

- IMAP client: `internal/email/imap.go`
- OAuth2 SASL: Uses `github.com/emersion/go-sasl`
- IMAP library: `github.com/emersion/go-imap/v2`

# Gmail API Integration

Warmbly uses the Gmail API for Google Workspace and Gmail accounts, providing efficient incremental sync via the History API.

## Overview

The Gmail API offers advantages over IMAP:
- Incremental sync via History API
- Native label support
- Better rate limits
- Push notifications via Pub/Sub

## Authentication

### OAuth2 Flow

1. User initiates connection from frontend
2. Backend redirects to Google OAuth consent screen
3. User grants access with required scopes
4. Google redirects back with authorization code
5. Backend exchanges code for access/refresh tokens
6. Tokens stored encrypted in database

### Required Scopes

```
https://www.googleapis.com/auth/gmail.readonly    # Read emails
https://www.googleapis.com/auth/gmail.send        # Send emails
https://www.googleapis.com/auth/gmail.modify      # Modify labels
https://mail.google.com/                          # Full access (warmup)
```

### Token Refresh

Access tokens expire after 1 hour. The backend automatically refreshes using the stored refresh token.

## History API

The History API enables incremental synchronization by tracking changes since the last sync.

### Initial Sync

1. List all message IDs with `messages.list`
2. Batch fetch message metadata
3. Store the current `historyId`

### Incremental Sync

1. Call `history.list` with stored `historyId`
2. Process changes:
   - `messagesAdded` - new messages
   - `messagesDeleted` - deleted messages
   - `labelsAdded` / `labelsRemoved` - label changes
3. Update stored `historyId`

### Example Response

```json
{
  "history": [
    {
      "id": "700123",
      "messagesAdded": [
        {
          "message": {
            "id": "18a2b3c4d5e6",
            "threadId": "18a2b3c4d5e6",
            "labelIds": ["INBOX", "UNREAD"]
          }
        }
      ],
      "labelsRemoved": [
        {
          "message": {"id": "18a2b3c4d5e6"},
          "labelIds": ["UNREAD"]
        }
      ]
    }
  ],
  "historyId": "700124",
  "nextPageToken": "..."
}
```

## Sending Emails

### MIME Structure

Emails are sent as base64url-encoded MIME messages.

```
From: sender@example.com
To: recipient@example.com
Subject: Hello
Content-Type: multipart/alternative; boundary="boundary"

--boundary
Content-Type: text/plain; charset="UTF-8"

Plain text body

--boundary
Content-Type: text/html; charset="UTF-8"

<html><body>HTML body</body></html>
--boundary--
```

### Threading

To reply in a thread:
- Set `threadId` in the request
- Include `In-Reply-To` and `References` headers

## Labels

Gmail uses labels instead of folders. System labels have special IDs.

| Label | ID | Description |
|-------|-----|-------------|
| Inbox | INBOX | Primary inbox |
| Sent | SENT | Sent messages |
| Drafts | DRAFT | Draft messages |
| Trash | TRASH | Deleted messages |
| Spam | SPAM | Spam folder |
| Starred | STARRED | Starred messages |
| Unread | UNREAD | Unread flag |

## Rate Limits

Gmail API has per-user and per-project quotas:

| Quota | Limit |
|-------|-------|
| Queries per day | 1,000,000,000 |
| Queries per user per second | 250 |
| Batch requests | 100 requests per batch |

## Error Handling

| HTTP Status | Meaning | Action |
|-------------|---------|--------|
| 401 | Token expired | Refresh token |
| 403 | Rate limited | Exponential backoff |
| 404 | Message not found | Skip, may be deleted |
| 410 | History expired | Full resync required |
| 429 | Too many requests | Backoff and retry |

## Code References

- Gmail client: `internal/email/gmail.go`
- OAuth2 handling: `github.com/meszmate/google-go`
- Google API library: `google.golang.org/api/gmail/v1`

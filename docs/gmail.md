# History Example:
```json
{
  "history": [
    {
      "id": "700123",
      "messages": [
        {
          "id": "18a2b3c4d5e6",
          "threadId": "18a2b3c4d5e6",
          "labelIds": ["INBOX", "UNREAD"],
          "historyId": "700123",
          "internalDate": 1712345678900,
          "snippet": "Hi! This is the message body...",
          "sizeEstimate": 12345,
          "payload": {
            "mimeType": "multipart/alternative",
            "filename": "",
            "headers": [
              { "name": "From",    "value": "Alice <alice@example.com>" },
              { "name": "To",      "value": "me@example.com" },
              { "name": "Subject", "value": "Hello from Alice" },
              { "name": "Date",    "value": "Sat, 06 Apr 2024 12:34:56 +0000" }
            ],
            "body": { "size": 0 },
            "parts": [
              {
                "partId": "0",
                "mimeType": "text/plain",
                "filename": "",
                "headers": [],
                "body": {
                  "size": 42,
                  "data": "SGksIHRoaXMgaXMgdGhlIHBsYWluIHRleHQ="
                }
              },
              {
                "partId": "1",
                "mimeType": "text/html",
                "filename": "",
                "headers": [],
                "body": {
                  "size": 88,
                  "data": "PGRpdj5IaSEgVGhpcyBpcyB0aGUgPGI+SFRNTDwvYj4gdmVyc2lvbi48L2Rpdj4="
                }
              },
              {
                "partId": "2",
                "mimeType": "image/png",
                "filename": "screenshot.png",
                "headers": [
                  { "name": "Content-Type",  "value": "image/png; name=\"screenshot.png\"" },
                  { "name": "Content-Disposition", "value": "attachment; filename=\"screenshot.png\"" }
                ],
                "body": {
                  "size": 102_400,
                  "attachmentId": "ANGjdJ_...123"
                }
              }
            ]
          }
        }
      ],
      "messagesAdded": [
        {
          "message": {
            "id": "18a2b3c4d5e6",
            "threadId": "18a2b3c4d5e6",
            "labelIds": ["INBOX", "UNREAD"],
            "snippet": "Hi! This is the message body...",
            "sizeEstimate": 12345
          }
        }
      ],
      "labelsRemoved": [
        {
          "message": {
            "id": "18a2b3c4d5e6",
            "labelIds": ["UNREAD"]
          },
          "labelIds": ["UNREAD"]
        }
      ]
    }
  ],
  "historyId": "700124",
  "nextPageToken": "Cg..."
}
```
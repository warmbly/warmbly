# EMSG Format

EMSG (Email Message Blob) is a compact binary format for storing email content in S3, designed for efficient storage and retrieval.

## Overview

EMSG provides a lightweight alternative to JSON for storing email bodies, with:
- Binary encoding for smaller size
- Forward-compatible versioning
- Efficient parsing without full deserialization

## Binary Layout

```
┌──────────────────────────────────────────┐
│ Magic Number (4 bytes): "EMSG"           │
├──────────────────────────────────────────┤
│ Version (1 byte): 0x01                   │
├──────────────────────────────────────────┤
│ Flags (4 bytes): bitmask of sections     │
├──────────────────────────────────────────┤
│ Section 1: [Length 4B][Data...]          │
├──────────────────────────────────────────┤
│ Section 2: [Length 4B][Data...]          │
├──────────────────────────────────────────┤
│ ...                                      │
└──────────────────────────────────────────┘
```

## Flags

| Bit | Flag | Description |
|-----|------|-------------|
| 0 | `FlagPlainText` | Plain-text email body present |
| 1 | `FlagHTMLBody` | HTML email body present |

## S3 Storage

### Key Pattern

```
emails/{YYYY}/{MM}/{DD}/{taskID}.emsg
```

Example: `emails/2026/01/29/550e8400-e29b-41d4-a716-446655440000.emsg`

### Lifecycle Policies

Configure S3 lifecycle rules to automatically expire old messages:

```json
{
  "Rules": [
    {
      "ID": "expire-old-emsg",
      "Prefix": "emails/",
      "Status": "Enabled",
      "Expiration": {
        "Days": 30
      }
    }
  ]
}
```

## Usage

### Encoding

```go
import "github.com/warmbly/warmbly/internal/pkg/emsg"

blob := emsg.New()
blob.SetPlainText("Hello, world!")
blob.SetHTMLBody("<html><body>Hello, world!</body></html>")

data, err := blob.Encode()
// Upload data to S3
```

### Decoding

```go
import "github.com/warmbly/warmbly/internal/pkg/emsg"

// Download data from S3
blob, err := emsg.Decode(data)
if err != nil {
    return err
}

if blob.HasPlainText() {
    text := blob.PlainText()
}

if blob.HasHTMLBody() {
    html := blob.HTMLBody()
}
```

## Code References

- Implementation: `internal/pkg/emsg/emsg.go`

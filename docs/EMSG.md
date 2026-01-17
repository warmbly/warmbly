# EMSG

We use our `EMSG` package to store data that requires more space or structure than a typical database row — such as **email message bodies**, **HTML + plain text content**, **headers**, and **attachment metadata**.<br/>
<br/>
This format provides a **compact**, **binary**, and **forward-compatible** representation that can be safely stored in systems like **Amazon S3**, efficiently retrieved, and parsed without relying on heavy encodings such as JSON.

---

## 🧩 Overview

`EMSG` (short for *Email Message Blob*) is a lightweight binary container format designed to represent structured message content using a simple header + data layout.<br/>
<br/>
Each blob contains:<br/>
[Magic 4B]["EMSG"]<br/>
[Version 1B]<br/>
[Flags 4B] → bitmask indicating which sections exist<br/>
[Sections...] → variable-length binary sections<br/>

Each section begins with a 4-byte length field followed by raw bytes.

---

## ⚙️ Sections

Bit | Flag | Meaning<br/>
<br/>
0 `FlagPlainText`: Plain-text email body<br/>
1 `FlagHTMLBody`: HTML-formatted email body

---

## Using S3 Lifecycle Policies
It is possible configure object expiration rules at the bucket or prefix level.<br/>
<br/>
Example (in JSON or via AWS console):
```json
{
  "Rules": [
    {
      "ID": "expire-old-emsg",
      "Prefix": "users/",
      "Status": "Enabled",
      "Expiration": {
        "Days": 30
      }
    }
  ]
}
```

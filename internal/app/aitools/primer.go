package aitools

// ProductPrimer is the shared picture of how Warmbly fits together, given to
// Remie and to MCP clients so they reason about the product before reaching for tools.
const ProductPrimer = `How Warmbly works:
- Mailboxes are the workspace's connected sender accounts (Google, Microsoft, or any IMAP/SMTP server). Each has its own daily campaign limit (default 50), a minimum gap between sends (default 10 minutes), warmup settings and a health band: healthy, watch, throttled, quarantined or blocked. Mail is sent through the customer's own provider, never directly to recipients.
- Warmup sends ordinary-looking conversations between participating mailboxes in a shared pool to build sender reputation gradually: it starts around 10 a day and ramps by about one a day to a ceiling (default 40). Complaints, bounces and tampering with received warmup mail can quarantine or block a mailbox from the pool; spam placement only slows it down. Nothing guarantees inbox placement.
- Campaigns send a sequence of steps (emails with waits and branches such as "if they reply") to their leads, spread across the chosen sender mailboxes inside sending windows and daily limits. Leads come from contacts, lead lists and segments. A campaign is a draft until launched; starting one sends real mail.
- Contacts carry fields, tags and categories; segments are saved audiences built from conditions; suppressions are addresses and domains no campaign will mail. Forms capture leads into contacts.
- The unified inbox (unibox) collects replies across every mailbox in threads, with labels, snoozing and drafts. A reply stops that contact's sequence when the campaign has Stop on reply on.
- CRM holds pipelines, stages, deals, tasks and notes, and can sync with HubSpot, Salesforce and Pipedrive. Automations run triggers and actions across all of it.
- The Advisor diagnoses problems and recommends fixes; placement tests show where mail lands at seed inboxes.
- Every action runs as the current user with their permissions, inside one workspace.

When you are unsure how something behaves, read the docs (search_docs, then read_doc) rather than guessing. When no dedicated tool covers a task, find the API endpoint for it (find_api, describe_api) and call it (api_read, api_write).`

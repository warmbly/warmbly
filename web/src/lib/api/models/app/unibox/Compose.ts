// Compose (fresh outbound) contracts. Mirrors the backend payloads for
// GET /unibox/compose/candidates and POST /unibox/compose.

import type Contact from "../contacts/Contact";

// One connected mailbox scored as a potential sender for the typed recipient.
export interface ComposeCandidate {
  id: string;
  email: string;
  name: string;
  provider: string;
  auth_state: "unknown" | "passing" | "failing";
  warmup_active: boolean;
  daily_limit: number;
  sent_today: number;
  remaining_today: number;
  /** Prior messages exchanged with the recipient from this mailbox. */
  history_messages: number;
  /** RFC3339; omitted when this mailbox never contacted the recipient. */
  last_contact_at?: string | null;
  score: number;
  /** Human-readable scoring signals ("has history", "low load today", ...). */
  reasons: string[];
  recommended: boolean;
}

export interface ComposeCandidatesResponse {
  accounts: ComposeCandidate[];
  recommended_account_id: string | null;
  recommended_reason: string;
  /** Known contact behind the typed address, when one exists. */
  contact: Contact | null;
  /** Set when the address is suppressed; the composer should warn hard. */
  suppression: { reason: string } | null;
}

export interface ComposeSendInput {
  /** Omit (or send "") to let the backend auto-pick the sender. */
  email_account_id?: string;
  to: string[];
  cc?: string[];
  bcc?: string[];
  subject: string;
  body_html?: string;
  body_plain?: string;
  // "instant" → send right away
  // "smart"   → next slot picked by the per-mailbox scheduler
  // "scheduled" → use scheduled_at verbatim (must be future)
  send_mode?: "instant" | "smart" | "scheduled";
  /** ISO timestamp; only honoured when send_mode = "scheduled". */
  scheduled_at?: string;
}

export interface ComposeSendResponse {
  task_id: string;
  scheduled_at: string;
  send_mode: string;
  account_id: string;
  account_email: string;
  /** True when the backend picked the sender automatically. */
  auto: boolean;
  /** Why the auto-picker chose this account (only set when auto). */
  picked_reason?: string;
}

// Parameters for the inbox search endpoint (GET /unibox).
// Mirrors backend models.MailSearchParams. Empty / undefined fields are
// stripped before serialization so the URL stays clean.

/** Canonical mail folders, mirroring the backend Folder* constants. */
export type UniboxFolder =
  | "inbox"
  | "sent"
  | "drafts"
  | "archive"
  | "spam"
  | "trash";

export const UNIBOX_FOLDERS: UniboxFolder[] = [
  "inbox",
  "sent",
  "drafts",
  "archive",
  "spam",
  "trash",
];

export interface UniboxSearchParams {
  /**
   * Free text. Matched against the subject, the preview, the message body,
   * and the sender and recipients. Quoted phrases, OR and -exclusion work;
   * a half-typed word matches as a prefix.
   */
  query?: string;
  from?: string; // Sender substring
  /** Exact address match against sender or recipients (compose history). */
  address?: string;
  /** Message direction relative to our mailboxes. */
  direction?: "sent" | "received";
  /**
   * Selected account IDs. The server filters with email_id IN (…).
   * Use this directly when picking specific mailboxes, or set
   * `tagId` instead and let the filter UI resolve it into the set
   * of accounts that carry the tag.
   */
  accountIds?: string[];
  /**
   * UI-only convenience: when set, the filter sheet resolves the tag
   * to the matching account IDs at apply time. The server never sees
   * this field; it sees the resolved `accountIds` instead.
   */
  tagId?: string;
  unseen?: boolean; // Only unread
  /**
   * Snoozed scope. true = only snoozed threads; undefined = exclude
   * snoozed (default inbox view); "any" = no snooze filter at all
   * (debug only).
   */
  snoozed?: true | "any";
  /**
   * Folder scope. Undefined = every working folder: spam, trash and archive
   * all stay out, so filing a conversation takes it out of every view the
   * workspace reads from and not only the Inbox folder.
   */
  folder?: UniboxFolder;
  /**
   * Put filed conversations back into an unscoped list. Only "All mail" and
   * reference reads (the composer's history panel) ask for it; a working view
   * that included archived mail would make Archive look like it did nothing.
   */
  includeArchived?: boolean;
  /** Awaiting reply: threads where the last message was from us. */
  awaitingReply?: boolean;
  /** Agent drafts: threads with a pending inbox-agent reply draft. */
  agentDrafts?: boolean;
  /**
   * Conversation-label filter. Threads carrying any of these category
   * ids match. Sent to the server as `category_ids`.
   */
  categoryIds?: string[];
  /**
   * Conversations no person wrote in (security alerts, bounces, autoresponders):
   * true lists only those, false leaves them out, undefined is both.
   */
  automated?: boolean;
  since?: Date; // From date
  until?: Date; // To date
  sortBy?: "newest" | "oldest";
  cursor?: string;
  limit?: number;
}

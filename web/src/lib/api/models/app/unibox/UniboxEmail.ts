export default interface UniboxEmail {
  id: string;
  from: string;
  to: string;
  subject: string;
  date: Date;
  is_seen: boolean;
  thread_id?: string;
  account_id: string;
  /** One-line preview from the message list; the full body is fetched per message. */
  snippet?: string;
  /** Number of messages in the conversation (for the stacked count badge). */
  message_count?: number;
  /** Conversation labels (categories) assigned to the thread. */
  labels?: { id: string; title: string; color: string }[];
}

/** GET /unibox/:id — the full message, body included. */
export interface UniboxEmailDetail extends UniboxEmail {
  /** Sanitized by the API before it is sent; safe to render. */
  body_html: string;
  body_plain: string;
  /** True when the stored body could not be read and body_plain is only the preview. */
  body_truncated?: boolean;
}

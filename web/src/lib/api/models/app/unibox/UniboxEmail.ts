export default interface UniboxEmail {
  id: string;
  from: string;
  to: string;
  subject: string;
  body: string;
  date: Date;
  is_seen: boolean;
  thread_id?: string;
  account_id: string;
  /** Number of messages in the conversation (for the stacked count badge). */
  message_count?: number;
  /** Conversation labels (categories) assigned to the thread. */
  labels?: { id: string; title: string; color: string }[];
}

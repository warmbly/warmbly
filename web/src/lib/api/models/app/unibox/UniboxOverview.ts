// Mirror of internal/models.UniboxOverview. One request fills both
// the scope rail and the top metric strip.

export interface UniboxMailboxOverview {
  id: string;
  email: string;
  name: string;
  unread: number;
  total: number;
}

export interface UniboxTagOverview {
  id: string;
  title: string;
  color: string;
  unread: number;
  total: number;
}

// Per-conversation-label counts (unread/total counted as threads).
export interface UniboxCategoryOverview {
  id: string;
  title: string;
  color: string;
  unread: number;
  total: number;
}

export default interface UniboxOverview {
  total: number;
  unread: number;
  today: number;
  week: number;
  snoozed: number;
  awaiting_reply: number;
  /** Pending outbound email tasks queued by the user. */
  scheduled_pending: number;
  /**
   * Hard cap on how many pending scheduled sends one user can queue
   * at once. Each pending send occupies a Cloud Tasks queue slot the
   * platform pays for, so the cap protects against runaway abuse.
   * Dashboard renders `scheduled_pending / scheduled_pending_max` so
   * the user sees their position before hitting the wall.
   */
  scheduled_pending_max: number;
  mailboxes: UniboxMailboxOverview[];
  tags: UniboxTagOverview[];
  categories: UniboxCategoryOverview[];
  generated_at: string;
  window_today_start: string;
  window_week_start: string;
}

import type { SendPause } from "@/lib/api/models/app/analytics/AccountStatus";

export interface SendPauseCopy {
    title: string;
    body: string;
    /** The one thing the owner can do about it. */
    action: "resume" | "fix_dns" | "reconnect" | "wait";
}

// Plain words for a provider pause, keyed on the reason the server recorded.
export function sendPauseCopy(pause: SendPause): SendPauseCopy {
    switch (pause.reason) {
        case "rate_limited":
            return {
                title: "Paused: the provider asked this mailbox to slow down",
                body: "The mail provider is limiting how fast this mailbox can send. Warmbly pauses it and resumes on its own; a limit that repeats pauses it for longer, up to 2 hours. Your other mailboxes keep sending, and no lead is charged an attempt.",
                action: "wait",
            };
        case "daily_limit":
            return {
                title: "Paused: daily sending limit reached",
                body: "The mail provider says this mailbox has used its sending quota. Warmbly waits for the provider's reset, or 24 hours when it gives none. Leads move to your other mailboxes or wait without losing an attempt. If this repeats, lower the mailbox's daily cap.",
                action: "resume",
            };
        case "sender_blocked":
            return {
                title: "Paused: the provider restricted this mailbox",
                body: "The mail provider is refusing mail from this account, often after unusual sending activity. Check the account with your provider. Warmbly tries again after the pause and waits longer each time it is refused, up to a day.",
                action: "resume",
            };
        case "domain_auth":
            return {
                title: "Paused: the sending domain failed authentication",
                body: "A receiving server refused this mailbox's mail because the domain's SPF, DKIM or DMARC records did not pass. Fix the domain's DNS records, then choose Resume now; a domain whose check was failing also resumes on its own once it passes. Reconnecting the mailbox does not fix this.",
                action: "fix_dns",
            };
        case "permanent":
            return {
                title: "Paused: the mail server refused this mailbox",
                body: "The mail server refused to send for this account. Fix the account with your mail provider, then resume sending. Warmbly also tries one send on its own after 3 hours, and waits longer after each refusal, up to a day.",
                action: "resume",
            };
        case "authentication":
            return {
                title: "Paused: the mail server refused the sign-in",
                body: "The server refused this mailbox's login when it last sent. Reconnect the mailbox with its current password or authorization to resume sending.",
                action: "reconnect",
            };
        case "retry":
            return {
                title: "Briefly paused: the mail server had a temporary problem",
                body: "The mail server could not take a send just now. Warmbly resumes in a few minutes on its own.",
                action: "wait",
            };
        default:
            return {
                title: "Briefly paused after a refused send",
                body: "The provider refused a send from this mailbox. Warmbly resumes on its own when the pause ends.",
                action: "wait",
            };
    }
}

/** When sending starts again: the end of a timed pause, or a held mailbox's next automatic try. */
export function sendPauseResumesAt(pause: SendPause): Date | null {
    const at = pause.held ? pause.retry_at : pause.until;
    return at ? new Date(at) : null;
}

// One line for a row tooltip.
export function sendPauseSummary(pause: SendPause): string {
    const { title } = sendPauseCopy(pause);
    const at = sendPauseResumesAt(pause);
    if (!at) return title;
    return `${title}. ${pause.held ? "Next automatic try" : "Resumes"} ${at.toLocaleString()}`;
}

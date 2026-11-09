import type { LeadSite, LeadSiteUnread } from "@/lib/api/models/app/leads/Lead";

// Sites read before the reason was recorded carry only the error text.
const BLOCKED_STATUS = /answered (401|403|406|429|503)\b/;

export function unreadReason(site: LeadSite): LeadSiteUnread {
    if (site.unread) return site.unread;
    if (site.error && BLOCKED_STATUS.test(site.error)) return "blocked";
    if (site.error?.includes("robots.txt")) return "robots";
    return "unreachable";
}

// Why a site has no details, said as a fact about the site rather than a failure.
export function unreadDetail(site: LeadSite): string {
    switch (unreadReason(site)) {
        case "blocked":
            return "This website doesn't let automated visitors read it.";
        case "robots":
            return "This website asks crawlers not to read it, so it was left alone.";
        default:
            return "The website didn't respond when it was read.";
    }
}

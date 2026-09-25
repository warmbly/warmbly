// The per-domain import choices as data: defaults, validation and the options they become.
import type { DomainRedirect, TrackingSuggestion, VendorDomainLink } from "@/lib/api/models/app/emails/SendingDomain";
import type { MailboxImportOptions } from "@/lib/api/models/app/emails/MailboxImport";
import { noDnsControl, redirectTargetProblem, trackingHostProblem } from "@/components/app/emails/domains/rules";

/** What the import knows about one domain. */
export interface DomainInfo {
    domain: string;
    /** Detected host; a shared consumer host offers no choices. */
    mail_host?: string;
    /** Absent when tracking is off on the instance. */
    tracking?: TrackingSuggestion | null;
    trackingLoading?: boolean;
    redirect?: DomainRedirect | null;
    /** The inbox vendor account holding the domain, when one does. */
    vendor_domain?: VendorDomainLink | null;
}

/** What the person changed for one domain; anything unset falls back to the all-domains choice. */
export interface DomainPick {
    track?: boolean;
    /** A host typed for this domain in place of the shared subdomain. */
    host?: string;
    redirect?: boolean;
    url?: string;
}

/** The all-domains choice plus each domain's own changes. */
export interface DomainPicks {
    all: DomainPick & { label?: string };
    each: Record<string, DomainPick>;
}

export const DEFAULT_TRACKING_LABEL = "link";

export const emptyPicks = (): DomainPicks => ({ all: {}, each: {} });

export function picksTouched(p: DomainPicks): boolean {
    return Object.keys(p.all).length > 0 || Object.keys(p.each).length > 0;
}

// Hosts whose domain belongs to the provider, so nobody here can add its DNS.
const SHARED_HOSTS = new Set(["gmail", "outlook", "yahoo", "aol", "icloud", "gmx", "yandex", "proton"]);

export function offersChoices(info: DomainInfo): boolean {
    return !noDnsControl(info.domain) && !SHARED_HOSTS.has(info.mail_host ?? "");
}

export function vendorWritesCname(v: VendorDomainLink | null | undefined): boolean {
    return !!v && v.can_dns && (v.dns_types ?? []).some((t) => t.toUpperCase() === "CNAME");
}

export function vendorForwards(v: VendorDomainLink | null | undefined): boolean {
    return !!v && v.can_forward;
}

/** The host a domain gets: one already working stays, a new one takes the chosen subdomain. */
export function trackingHost(info: DomainInfo, label: string | undefined): string {
    const t = info.tracking;
    if (!t) return "";
    if (t.status !== "suggested") return t.host;
    return `${(label || DEFAULT_TRACKING_LABEL).toLowerCase()}.${info.domain}`;
}

export function effectivePick(info: DomainInfo, picks: DomainPicks) {
    const own = picks.each[info.domain];
    const all = picks.all;
    return {
        // On unless turned off: a host is saved before its DNS exists and links keep the shared host until it resolves.
        track: !!info.tracking && (own?.track ?? all.track ?? true),
        host: (own?.host ?? trackingHost(info, all.label)).trim().toLowerCase(),
        redirect: own?.redirect ?? all.redirect ?? false,
        url: own?.url ?? (all.redirect ? all.url : undefined) ?? info.redirect?.target_url ?? "",
    };
}

/** The picks as import options; keys absent when nothing is picked. */
export function domainOptions(infos: DomainInfo[], picks: DomainPicks): Pick<MailboxImportOptions, "tracking_domains" | "redirects"> {
    const tracking: Record<string, string> = {};
    const redirects: Record<string, string> = {};
    for (const info of infos) {
        if (!offersChoices(info)) continue;
        const e = effectivePick(info, picks);
        if (e.track && e.host) tracking[info.domain] = e.host;
        if (e.redirect && e.url.trim()) redirects[info.domain] = e.url.trim();
    }
    return {
        ...(Object.keys(tracking).length > 0 ? { tracking_domains: tracking } : {}),
        ...(Object.keys(redirects).length > 0 ? { redirects } : {}),
    };
}

/** Why the chosen subdomain cannot be used, or null. */
export function labelProblem(label: string | undefined): string | null {
    const l = (label ?? DEFAULT_TRACKING_LABEL).trim().toLowerCase();
    if (!l) return "Enter the subdomain to use, like link.";
    if (!/^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(l)) return "Use one plain word of letters, digits or dashes, like link.";
    return null;
}

/** Why the picks cannot be sent, or null. */
export function domainChoiceIssue(infos: DomainInfo[], picks: DomainPicks): string | null {
    const usable = infos.filter(offersChoices);
    if (usable.length === 0) return null;
    const newHosts = usable.some((i) => i.tracking?.status === "suggested" && picks.each[i.domain]?.host === undefined && effectivePick(i, picks).track);
    if (newHosts) {
        const lp = labelProblem(picks.all.label);
        if (lp) return `Tracking subdomain: ${lp}`;
    }
    if (picks.all.redirect) {
        const p = redirectTargetProblem(picks.all.url ?? "", "");
        if (p) return `Redirect for every domain: ${p}`;
    }
    for (const info of usable) {
        const e = effectivePick(info, picks);
        if (e.track && picks.each[info.domain]?.host !== undefined) {
            const hp = trackingHostProblem(e.host, info.domain);
            if (hp) return `${info.domain}: ${hp}`;
        }
        if (!e.redirect) continue;
        const problem = redirectTargetProblem(e.url, info.domain);
        if (problem) return `${info.domain}: ${problem} Or untick its redirect.`;
    }
    return null;
}

/** Domains left with DNS to add after the import: a redirect not live yet, or a tracking host still to point. */
export function dnsFollowUps(infos: DomainInfo[], picks: DomainPicks): number {
    let n = 0;
    for (const info of infos) {
        if (!offersChoices(info)) continue;
        const e = effectivePick(info, picks);
        const redirectDns =
            e.redirect && !vendorForwards(info.vendor_domain) && !(info.redirect?.verified && info.redirect.target_url === e.url.trim());
        const trackDns = e.track && needsRecord(info, e.host) && !vendorWritesCname(info.vendor_domain);
        if (redirectDns || trackDns) n++;
    }
    return n;
}

/** Whether the host still needs its CNAME: anything but the host already found working. */
export function needsRecord(info: DomainInfo, host: string): boolean {
    return info.tracking?.status === "suggested" || host !== info.tracking?.host;
}

/** How many domains the vendor sets up with no DNS to add, per choice. */
export function vendorCoverage(infos: DomainInfo[]) {
    const usable = infos.filter(offersChoices);
    const cname = usable.filter((i) => vendorWritesCname(i.vendor_domain));
    const forward = usable.filter((i) => vendorForwards(i.vendor_domain));
    return { domains: usable.length, cname: cname.length, forward: forward.length, vendor: cname[0]?.vendor_domain?.vendor || forward[0]?.vendor_domain?.vendor || "" };
}

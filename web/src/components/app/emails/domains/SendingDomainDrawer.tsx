// Sending domain detail: a right slide-over with three tabs. Overview is the
// domain's authentication, hosts and the vendor holding it; Tracking is the one
// tracking host every mailbox uses; Redirect sends the root to the company
// website, through the vendor when it can. All read the list query, so a
// teammate's change lands live.
import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";
import {
    AlertTriangleIcon,
    ArrowRightIcon,
    CheckCircle2Icon,
    CheckIcon,
    CircleDashedIcon,
    ExternalLinkIcon,
    GlobeIcon,
    LayoutGridIcon,
    Loader2Icon,
    MinusIcon,
    MousePointerClickIcon,
    RefreshCwIcon,
    Trash2Icon,
    XIcon,
    ZapIcon,
} from "lucide-react";
import type { SendingDomain, DNSRecord, DomainRedirect, VendorDomainLink } from "@/lib/api/models/app/emails/SendingDomain";
import type TrackingDomain from "@/lib/api/models/app/emails/TrackingDomain";
import {
    useDeleteDomainRedirect,
    useSetDomainRedirect,
    useSetDomainTracking,
    useSetVendorForwarding,
    useSetVendorTracking,
    useTrackingSuggestion,
    useVerifyDomainRedirect,
} from "@/lib/api/hooks/app/emails/useSendingDomains";
import { vendorLabel } from "@/lib/api/models/app/emails/MailboxSources";
import { Label, TextInput } from "@/components/ui/field";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import ProviderLogo, { LogoStack } from "@/components/app/emails/ProviderLogo";
import { useConfirm } from "@/hooks/context/confirm";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import timeAgo from "@/lib/helper/timeAgo";
import { mailHostLabel } from "@/lib/mailHost";
import { cn } from "@/lib/utils";
import { DnsRecordsTable, StatusPill, VendorChip, type RecordRow } from "./parts";
import {
    authPill,
    domainVendor,
    redirectPill,
    redirectState,
    redirectTargetProblem,
    shortUrl,
    trackingPill,
    trackingRecord,
    trackingState,
    vendorCanCname,
} from "./rules";

export type DomainTab = "overview" | "tracking" | "redirect";

const TABS: { key: DomainTab; label: string; icon: typeof GlobeIcon }[] = [
    { key: "overview", label: "Overview", icon: LayoutGridIcon },
    { key: "tracking", label: "Tracking", icon: MousePointerClickIcon },
    { key: "redirect", label: "Redirect", icon: GlobeIcon },
];

const PURPOSE_LABEL: Record<string, string> = { ownership: "Ownership", root: "Root", www: "www" };

const paneVariants = {
    enter: (dir: 1 | -1) => ({ x: dir * 24, opacity: 0 }),
    center: { x: 0, opacity: 1 },
    exit: (dir: 1 | -1) => ({ x: dir * -24, opacity: 0 }),
};

/** A vendor refusal in words that name the vendor. */
function vendorError(e: AppError, vendor: string): string {
    if (e?.code === "mailbox_vendor_domain_unsupported") return `${vendor} does not offer this for this domain through its API.`;
    if (e?.code === "mailbox_vendor_domain_not_found") return `${vendor} no longer lists this domain on the connected account.`;
    return buildError(e);
}

export default function SendingDomainDrawer({
    domains,
    open,
    tab,
    setTab,
    onClose,
}: {
    domains: SendingDomain[] | undefined;
    /** The open domain's name, or "". */
    open: string;
    tab: DomainTab;
    setTab: (t: DomainTab) => void;
    onClose: () => void;
}) {
    const domain = domains?.find((d) => d.domain === open) ?? null;
    return (
        <AnimatePresence>
            {open && domain && <Detail key={domain.domain} domain={domain} tab={tab} setTab={setTab} onClose={onClose} />}
        </AnimatePresence>
    );
}

function Detail({
    domain,
    tab,
    setTab,
    onClose,
}: {
    domain: SendingDomain;
    tab: DomainTab;
    setTab: (t: DomainTab) => void;
    onClose: () => void;
}) {
    const confirm = useConfirm();

    // Drafts are null until edited, so a teammate's save shows through an untouched form.
    const [hostDraft, setHostDraft] = React.useState<string | null>(null);
    const [urlDraft, setUrlDraft] = React.useState<string | null>(null);
    const [wwwDraft, setWwwDraft] = React.useState<boolean | null>(null);
    const [fwdDraft, setFwdDraft] = React.useState<string | null>(null);
    const redirect = domain.redirect ?? null;
    const dirty =
        (hostDraft !== null && hostDraft.trim() !== "") ||
        (urlDraft !== null && urlDraft.trim() !== (redirect?.target_url ?? "")) ||
        (wwwDraft !== null && wwwDraft !== (redirect?.include_www ?? true)) ||
        (fwdDraft !== null && fwdDraft.trim() !== (domain.vendor_domain?.forwarding ?? ""));

    const requestClose = React.useCallback(() => {
        if (dirty) confirm.show("Discard what you typed here?", async () => onClose());
        else onClose();
    }, [dirty, confirm, onClose]);

    // Escape closes the innermost layer only: the confirm owns it while it is up.
    React.useEffect(() => {
        function onKey(e: KeyboardEvent) {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            requestClose();
        }
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [requestClose]);

    // Slide toward the tab picked: right of the current one comes in from the right.
    const [direction, setDirection] = React.useState<1 | -1>(1);
    const goTo = (t: DomainTab) => {
        if (t === tab) return;
        setDirection(TABS.findIndex((x) => x.key === t) > TABS.findIndex((x) => x.key === tab) ? 1 : -1);
        setTab(t);
    };

    const hosts = domain.mail_hosts ?? [];

    return (
        <motion.div
            key="overlay"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
            className="fixed inset-0 z-[110] flex justify-end bg-slate-900/30 backdrop-blur-[2px]"
            onMouseDown={requestClose}
        >
            <motion.aside
                initial={{ x: 32, opacity: 0 }}
                animate={{ x: 0, opacity: 1 }}
                exit={{ x: 32, opacity: 0 }}
                transition={{ duration: 0.2, ease: [0.32, 0.72, 0, 1] }}
                onMouseDown={(e) => e.stopPropagation()}
                className="flex flex-col w-full max-w-full md:w-[34rem] md:max-w-[95%] h-full bg-white border-l border-slate-200 shadow-[-12px_0_24px_-12px_rgba(15,23,42,0.08)]"
            >
                <div className="shrink-0 px-5 h-14 flex items-center gap-3 border-b border-slate-200">
                    {hosts.length > 0 ? (
                        <LogoStack ids={hosts} size="lg" max={2} className="shrink-0" />
                    ) : (
                        <div className="w-8 h-8 rounded-lg bg-sky-50 text-sky-700 flex items-center justify-center shrink-0">
                            <GlobeIcon className="w-4 h-4" />
                        </div>
                    )}
                    <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-1.5 min-w-0">
                            <span className="text-[13px] font-medium text-slate-900 truncate">{domain.domain}</span>
                            <VendorChip d={domain} compact />
                        </div>
                        <div className="text-[10.5px] text-slate-400 truncate">
                            {domain.mailboxes.toLocaleString()} {domain.mailboxes === 1 ? "mailbox" : "mailboxes"}
                            {hosts.length > 0 ? ` · ${hosts.map((h) => mailHostLabel(h)).join(", ")}` : ""}
                        </div>
                    </div>
                    <button
                        type="button"
                        onClick={requestClose}
                        aria-label="Close"
                        className="w-7 h-7 rounded-md flex items-center justify-center text-slate-400 hover:text-slate-900 hover:bg-slate-100 transition-colors shrink-0"
                    >
                        <XIcon className="w-4 h-4" />
                    </button>
                </div>

                <div className="shrink-0 px-3 flex items-center gap-1 border-b border-slate-200 overflow-x-auto overflow-y-hidden no-scrollbar">
                    {TABS.map((t) => {
                        const active = tab === t.key;
                        return (
                            <button
                                key={t.key}
                                type="button"
                                onClick={() => goTo(t.key)}
                                className={cn(
                                    "relative h-10 px-2.5 inline-flex shrink-0 items-center gap-1.5 text-[12.5px] transition-colors",
                                    active ? "text-slate-900 font-medium" : "text-slate-500 hover:text-slate-800",
                                )}
                            >
                                <t.icon className="w-3.5 h-3.5" />
                                {t.label}
                                {active && (
                                    <motion.span
                                        layoutId="domain-tab-underline"
                                        className="absolute left-1.5 right-1.5 bottom-0 h-0.5 rounded-full bg-sky-600"
                                        transition={{ type: "spring", duration: 0.3, bounce: 0.15 }}
                                    />
                                )}
                            </button>
                        );
                    })}
                </div>

                <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden">
                    <AnimatePresence mode="wait" initial={false} custom={direction}>
                        <motion.div
                            key={tab}
                            custom={direction}
                            variants={paneVariants}
                            initial="enter"
                            animate="center"
                            exit="exit"
                            transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                        >
                            {tab === "overview" && <OverviewTab domain={domain} goTo={goTo} />}
                            {tab === "tracking" && <TrackingTab domain={domain} draft={hostDraft} setDraft={setHostDraft} />}
                            {tab === "redirect" && (
                                <RedirectTab
                                    domain={domain}
                                    redirect={redirect}
                                    urlDraft={urlDraft}
                                    setUrlDraft={setUrlDraft}
                                    wwwDraft={wwwDraft}
                                    setWwwDraft={setWwwDraft}
                                    fwdDraft={fwdDraft}
                                    setFwdDraft={setFwdDraft}
                                />
                            )}
                        </motion.div>
                    </AnimatePresence>
                </div>
            </motion.aside>
        </motion.div>
    );
}

const Eyebrow = ({ children }: { children: React.ReactNode }) => (
    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{children}</div>
);

/* ── Overview ─────────────────────── */

const AUTH_ROWS: { key: "auth_spf" | "auth_dkim" | "auth_dmarc"; label: string; what: string }[] = [
    { key: "auth_spf", label: "SPF", what: "Lists the servers allowed to send for the domain." },
    { key: "auth_dkim", label: "DKIM", what: "Signs each message so it cannot be altered on the way." },
    { key: "auth_dmarc", label: "DMARC", what: "Tells receivers what to do with mail that fails the other two." },
];

function OverviewTab({ domain, goTo }: { domain: SendingDomain; goTo: (t: DomainTab) => void }) {
    const auth = authPill(domain);
    const tracking = trackingPill(domain);
    const redirect = redirectPill(domain);
    const checked = domain.auth_state !== "unknown";
    const hosts = domain.mail_hosts ?? [];
    return (
        <div className="px-5 py-4 space-y-5">
            <div className="space-y-2">
                <div className="flex items-center gap-2">
                    <Eyebrow>Authentication</Eyebrow>
                    <StatusPill tone={auth.tone} title={auth.title} className="ml-auto">
                        {auth.label}
                    </StatusPill>
                </div>
                <div className="rounded-md border border-slate-200 divide-y divide-slate-100">
                    {AUTH_ROWS.map((r) => {
                        const on = domain[r.key];
                        return (
                            <div key={r.key} className="px-3 py-2 flex items-start gap-2.5">
                                <span
                                    className={cn(
                                        "mt-0.5 size-4 rounded-full inline-flex items-center justify-center shrink-0",
                                        !checked ? "bg-slate-100 text-slate-400" : on ? "bg-emerald-50 text-emerald-600" : "bg-slate-100 text-slate-400",
                                    )}
                                >
                                    {!checked ? <CircleDashedIcon className="w-2.5 h-2.5" /> : on ? <CheckIcon className="w-2.5 h-2.5" strokeWidth={3} /> : <MinusIcon className="w-2.5 h-2.5" />}
                                </span>
                                <div className="min-w-0 flex-1">
                                    <div className="flex items-center gap-2">
                                        <span className="text-[12.5px] font-medium text-slate-900">{r.label}</span>
                                        <span className="text-[11px] text-slate-500">
                                            {!checked ? "Not checked yet" : on ? "Found" : r.key === "auth_dkim" ? "Not confirmed" : "Not found"}
                                        </span>
                                    </div>
                                    <p className="text-[11.5px] text-slate-500 leading-relaxed">
                                        {r.what}
                                        {checked && !on && r.key === "auth_dkim" && " DNS cannot list DKIM keys, so a key at an unusual selector may still be there."}
                                    </p>
                                </div>
                            </div>
                        );
                    })}
                </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                <GlanceCard
                    icon={MousePointerClickIcon}
                    title="Tracking domain"
                    tone={tracking.tone}
                    label={tracking.label}
                    hint={tracking.title}
                    action={trackingState(domain) === "live" ? "Manage" : "Set up"}
                    onClick={() => goTo("tracking")}
                />
                <GlanceCard
                    icon={GlobeIcon}
                    title="Website redirect"
                    tone={redirect.tone}
                    label={redirect.label}
                    hint={redirect.title}
                    action={redirectState(domain) === "none" ? "Set up" : "Manage"}
                    onClick={() => goTo("redirect")}
                />
            </div>

            <VendorCard domain={domain} goTo={goTo} />

            <div className="space-y-2">
                <Eyebrow>Mail {hosts.length === 1 ? "host" : "hosts"}</Eyebrow>
                {hosts.length === 0 ? (
                    <p className="text-[12px] text-slate-500">Not detected yet.</p>
                ) : (
                    <div className="flex flex-wrap gap-1.5">
                        {hosts.map((h) => (
                            <span
                                key={h}
                                className="inline-flex items-center gap-1.5 h-7 pl-1 pr-2.5 rounded-md border border-slate-200 bg-white text-[12px] text-slate-700"
                            >
                                <ProviderLogo id={h} size="sm" framed={false} />
                                {mailHostLabel(h)}
                            </span>
                        ))}
                    </div>
                )}
            </div>
        </div>
    );
}

function GlanceCard({
    icon: Icon,
    title,
    tone,
    label,
    hint,
    action,
    onClick,
}: {
    icon: typeof GlobeIcon;
    title: string;
    tone: React.ComponentProps<typeof StatusPill>["tone"];
    label: string;
    hint: string;
    action: string;
    onClick: () => void;
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            className="group rounded-md border border-slate-200 hover:border-slate-300 hover:bg-slate-50/60 px-3 py-2.5 text-left transition-colors min-w-0"
        >
            <div className="flex items-center gap-1.5 text-[11.5px] text-slate-500">
                <Icon className="w-3.5 h-3.5 text-slate-400" />
                {title}
                <span className="ml-auto inline-flex items-center gap-0.5 text-sky-700 opacity-80 group-hover:opacity-100">
                    {action}
                    <ArrowRightIcon className="w-3 h-3 group-hover:translate-x-0.5 transition-transform" />
                </span>
            </div>
            <StatusPill tone={tone} title={hint} className="mt-1.5">
                {label}
            </StatusPill>
        </button>
    );
}

/** What the vendor holding the domain can do for it, with a shortcut to each. */
function VendorCard({ domain, goTo }: { domain: SendingDomain; goTo: (t: DomainTab) => void }) {
    const vendorId = domainVendor(domain);
    if (!vendorId) return null;
    const name = vendorLabel(vendorId);
    const v = domain.vendor_domain;
    return (
        <div className="rounded-lg border border-slate-200 overflow-hidden">
            <div className="px-3 py-2.5 flex items-center gap-2.5 bg-slate-50/60 border-b border-slate-100">
                <ProviderLogo id={vendorId} size="lg" />
                <div className="min-w-0">
                    <p className="text-[12.5px] font-medium text-slate-900">{v ? `Managed by ${name}` : `Mailboxes from ${name}`}</p>
                    <p className="text-[11.5px] text-slate-500 truncate">
                        {v ? `Your connected ${name} account holds this domain.` : `No connected ${name} account lists this domain.`}
                    </p>
                </div>
            </div>
            {v ? (
                <ul className="px-3 py-2 space-y-1.5 text-[11.5px] text-slate-600">
                    <Capability
                        on={vendorCanCname(domain)}
                        yes="Adds the tracking record for you in one click"
                        no="Tracking records are added at your DNS provider"
                        onClick={() => goTo("tracking")}
                    />
                    <Capability
                        on={v.can_forward}
                        yes={v.forwarding ? `Forwards ${domain.domain} to ${shortUrl(v.forwarding)}` : `Can forward ${domain.domain} to your website`}
                        no="Forwarding goes through DNS records instead"
                        onClick={() => goTo("redirect")}
                    />
                    {v.can_forward && v.forwarding_reviewed && (
                        <li className="pl-6 text-[11px] text-slate-400">{name} applies forwarding changes by hand, usually within a day.</li>
                    )}
                </ul>
            ) : (
                <p className="px-3 py-2 text-[11.5px] text-slate-500 leading-relaxed">
                    Connect the {name} account that holds {domain.domain} to set its tracking record and forwarding from here. Until
                    then, both go through your DNS provider.
                </p>
            )}
        </div>
    );
}

function Capability({ on, yes, no, onClick }: { on: boolean; yes: string; no: string; onClick: () => void }) {
    return (
        <li>
            <button type="button" onClick={onClick} className="w-full flex items-start gap-2 text-left group">
                <span
                    className={cn(
                        "mt-px size-4 rounded-full inline-flex items-center justify-center shrink-0",
                        on ? "bg-sky-50 text-sky-600" : "bg-slate-100 text-slate-400",
                    )}
                >
                    {on ? <ZapIcon className="w-2.5 h-2.5" /> : <MinusIcon className="w-2.5 h-2.5" />}
                </span>
                <span className={cn("min-w-0 flex-1 group-hover:text-slate-900 transition-colors", on ? "text-slate-700" : "text-slate-500")}>
                    {on ? yes : no}
                </span>
                <ArrowRightIcon className="w-3 h-3 mt-0.5 text-slate-300 group-hover:text-slate-500 transition-colors shrink-0" />
            </button>
        </li>
    );
}

/* ── Tracking ─────────────────────── */

function TrackingTab({
    domain,
    draft,
    setDraft,
}: {
    domain: SendingDomain;
    draft: string | null;
    setDraft: (v: string | null) => void;
}) {
    const confirm = useConfirm();
    const suggestion = useTrackingSuggestion(domain.domain);
    const save = useSetDomainTracking();
    const viaVendor = useSetVendorTracking();
    const [result, setResult] = React.useState<TrackingDomain | null>(null);
    const vendorName = vendorLabel(domain.vendor_domain?.vendor);

    const sug = suggestion.data;
    const notConfigured = (suggestion.error as AppError | null)?.code === "tracking_host_not_configured";
    const inUse = domain.tracking_domains;
    const host = (draft ?? sug?.host ?? "").trim().toLowerCase();
    const edited = draft !== null && host !== (sug?.host ?? "");
    const target = sug?.cname_target ?? result?.cname_target ?? "";
    const fullyOn = inUse.length === 1 && inUse[0].host === host && inUse[0].mailboxes >= domain.mailboxes;
    const n = domain.mailboxes;

    async function apply() {
        if (!host || save.isPending) return;
        try {
            const res = await save.mutateAsync({ domain: domain.domain, host });
            setResult(res.tracking);
            setDraft(null);
            const on = `${res.mailboxes.toLocaleString()} ${res.mailboxes === 1 ? "mailbox" : "mailboxes"}`;
            if (res.tracking.tracking_domain_verified) toast.success(`${host} is tracking links for ${on}`);
            else toast.success(`Saved on ${on}. It switches on by itself once the CNAME resolves.`);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    const vendorOffer = vendorCanCname(domain) && trackingState(domain) !== "live";
    const hostProblem = host && host !== domain.domain && !host.endsWith(`.${domain.domain}`) ? `The host has to be on ${domain.domain}, like link.${domain.domain}.` : null;

    async function applyViaVendor() {
        if (!host || hostProblem || viaVendor.isPending) return;
        try {
            const res = await viaVendor.mutateAsync({ domain: domain.domain, host });
            setResult(res.tracking);
            setDraft(null);
            const on = `${res.mailboxes.toLocaleString()} ${res.mailboxes === 1 ? "mailbox" : "mailboxes"}`;
            if (res.tracking.tracking_domain_verified) toast.success(`${vendorName} added the record. ${host} is tracking links for ${on}.`);
            else toast.success(`${vendorName} added the record. ${host} switches on for ${on} once DNS picks it up.`);
        } catch (e) {
            toast.error(vendorError(e as AppError, vendorName));
        }
    }

    function clear() {
        confirm.show(
            `Stop using a tracking domain on the ${n.toLocaleString()} ${n === 1 ? "mailbox" : "mailboxes"} on ${domain.domain}? Their links go back to the shared tracking host.`,
            async () => {
                try {
                    await save.mutateAsync({ domain: domain.domain, host: "" });
                    setResult(null);
                    setDraft(null);
                    toast.success("Tracking domain cleared");
                } catch (e) {
                    toast.error(buildError(e as AppError));
                }
            },
        );
    }

    return (
        <div className="px-5 py-4 space-y-5">
            <p className="text-[12px] text-slate-600 leading-relaxed">
                Tracked links and the open pixel are served from this host. A host on your own domain keeps every link in a message
                pointing at the company that sent it.
            </p>

            {vendorOffer && !notConfigured && (
                <div className="rounded-lg border border-sky-200 bg-sky-50/50 p-3 space-y-2">
                    <div className="flex items-center gap-2.5">
                        <ProviderLogo id={domain.vendor_domain!.vendor} size="lg" />
                        <div className="min-w-0">
                            <p className="text-[12.5px] font-medium text-slate-900">Set up tracking with {vendorName}</p>
                            <p className="text-[11.5px] text-slate-600">
                                {vendorName} adds the CNAME for you, then every mailbox on {domain.domain} uses it.
                            </p>
                        </div>
                    </div>
                    {host && target && (
                        <div className="rounded-md bg-white border border-sky-100 px-2.5 py-1.5 text-[11.5px] font-mono text-slate-700 flex items-center gap-1.5 min-w-0">
                            <span className="truncate">{host}</span>
                            <ArrowRightIcon className="w-3 h-3 text-slate-400 shrink-0" />
                            <span className="truncate text-slate-500">{target}</span>
                        </div>
                    )}
                    {hostProblem && <p className="text-[11.5px] text-amber-700">{hostProblem}</p>}
                    <button
                        type="button"
                        onClick={() => void applyViaVendor()}
                        disabled={!host || !!hostProblem || viaVendor.isPending || suggestion.isLoading}
                        className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                    >
                        {viaVendor.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <ZapIcon className="w-3 h-3" />}
                        {viaVendor.isPending ? `Asking ${vendorName}…` : `Set up with ${vendorName}`}
                    </button>
                </div>
            )}

            <div className="space-y-1.5">
                <Eyebrow>In use</Eyebrow>
                {inUse.length === 0 ? (
                    <p className="text-[12px] text-slate-500">None yet. Links use the shared tracking host.</p>
                ) : (
                    <div className="rounded-md border border-slate-200 divide-y divide-slate-100">
                        {inUse.map((t) => (
                            <div key={t.host} className="px-3 py-2 flex items-center gap-2 min-w-0">
                                <span className="text-[12px] font-mono text-slate-800 truncate min-w-0">{t.host}</span>
                                {t.verified ? <StatusPill tone="emerald">Verified</StatusPill> : <StatusPill tone="amber">Waiting for DNS</StatusPill>}
                                <span className="ml-auto text-[11px] text-slate-500 tabular-nums shrink-0">
                                    {t.mailboxes.toLocaleString()} of {n.toLocaleString()}
                                </span>
                            </div>
                        ))}
                    </div>
                )}
            </div>

            <div className="space-y-2">
                <Eyebrow>
                    {vendorOffer ? "Or add the record yourself" : `Use one tracking domain for all ${n.toLocaleString()} ${n === 1 ? "mailbox" : "mailboxes"} on this domain`}
                </Eyebrow>
                {suggestion.isLoading ? (
                    <p className="text-[12px] text-slate-500 inline-flex items-center gap-1.5">
                        <Loader2Icon className="w-3 h-3 animate-spin" />
                        Checking DNS for a tracking host
                    </p>
                ) : notConfigured ? (
                    <p className="text-[12px] text-amber-800 leading-relaxed">
                        Open and click tracking is not set up on this instance, so there is no tracking host to point a domain at.
                    </p>
                ) : (
                    <>
                        <div className="flex items-center gap-1.5">
                            <TextInput
                                value={draft ?? sug?.host ?? ""}
                                onChange={setDraft}
                                placeholder={`link.${domain.domain}`}
                                className="flex-1 font-mono"
                                onKeyDown={(e) => {
                                    if (e.key === "Enter") void apply();
                                }}
                            />
                            <button
                                type="button"
                                onClick={() => void apply()}
                                disabled={!host || save.isPending}
                                className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50 shrink-0"
                            >
                                {save.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                {fullyOn && !edited ? "Check again" : "Save"}
                            </button>
                        </div>
                        {suggestion.error && !notConfigured && (
                            <p className="text-[11.5px] text-slate-500">Could not check DNS for a suggestion. Type the host to use.</p>
                        )}
                        {sug && !edited && sug.status === "active" && (
                            <p className="text-[11.5px] text-emerald-700 inline-flex items-center gap-1">
                                <CheckCircle2Icon className="w-3 h-3" />
                                Already in use here and verified.
                            </p>
                        )}
                        {sug && !edited && sug.status === "found" && (
                            <p className="text-[11.5px] text-emerald-700 inline-flex items-center gap-1">
                                <CheckCircle2Icon className="w-3 h-3" />
                                Already points at Warmbly, ready to use.
                            </p>
                        )}
                        {host && target && (edited || sug?.status === "suggested") && (
                            <div className="space-y-1.5">
                                <p className="text-[11.5px] text-slate-600 leading-relaxed">
                                    Add this record at your DNS provider. You can save first: links switch to the host by themselves
                                    once the record resolves.
                                </p>
                                <DnsRecordsTable records={[trackingRecord(host, target)]} />
                            </div>
                        )}
                        {result && !result.tracking_domain_verified && result.message && (
                            <p className="text-[11.5px] text-amber-800 leading-relaxed flex items-start gap-1.5">
                                <AlertTriangleIcon className="w-3 h-3 mt-0.5 shrink-0" />
                                <span>
                                    {result.message}
                                    {result.observed ? ` DNS answered ${result.observed}.` : ""}
                                </span>
                            </p>
                        )}
                    </>
                )}
            </div>

            {inUse.length > 0 && (
                <div className="pt-1">
                    <button
                        type="button"
                        onClick={clear}
                        disabled={save.isPending}
                        className="h-7 px-2.5 rounded-md border border-slate-200 text-[12px] text-rose-700 hover:bg-rose-50 hover:border-rose-200 inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                    >
                        <Trash2Icon className="w-3 h-3" />
                        Clear the tracking domain
                    </button>
                </div>
            )}
        </div>
    );
}

/* ── Redirect ─────────────────────── */

// The list's records carry no probe, so a record the last check saw in place stays ticked.
function mergeRecords(list: DNSRecord[], checked: DomainRedirect | null): RecordRow[] {
    const seen = new Set(
        (checked?.records ?? []).filter((r) => r.ok).map((r) => `${r.purpose}|${r.type}|${r.name}|${r.value}`),
    );
    return list.map((r, i) => ({
        key: `${r.purpose}-${r.type}-${r.value}-${i}`,
        purpose: PURPOSE_LABEL[r.purpose] ?? r.purpose,
        type: r.type,
        name: r.name,
        value: r.value,
        ok: r.ok || seen.has(`${r.purpose}|${r.type}|${r.name}|${r.value}`),
        optional: r.optional,
    }));
}

function RedirectTab({
    domain,
    redirect,
    urlDraft,
    setUrlDraft,
    wwwDraft,
    setWwwDraft,
    fwdDraft,
    setFwdDraft,
}: {
    domain: SendingDomain;
    redirect: DomainRedirect | null;
    urlDraft: string | null;
    setUrlDraft: (v: string | null) => void;
    wwwDraft: boolean | null;
    setWwwDraft: (v: boolean | null) => void;
    fwdDraft: string | null;
    setFwdDraft: (v: string | null) => void;
}) {
    const v = domain.vendor_domain ?? null;
    const canVendor = !!v?.can_forward;
    // The vendor's forwarding is the easy path, unless DNS records are already in use.
    const [mode, setMode] = React.useState<"vendor" | "dns">(canVendor && !redirect ? "vendor" : "dns");
    const shown = canVendor ? mode : "dns";
    const name = vendorLabel(v?.vendor);
    return (
        <div className="px-5 py-4 space-y-5">
            <p className="text-[12px] text-slate-600 leading-relaxed">
                A domain that answers visitors like a real company reads as more trustworthy, so send anyone who opens{" "}
                <span className="font-medium text-slate-900">{domain.domain}</span> in a browser to your main website.
            </p>

            {canVendor && (
                <div className="grid grid-cols-2 p-0.5 rounded-md bg-slate-100 text-[12px]" role="tablist" aria-label="How to redirect">
                    {(["vendor", "dns"] as const).map((m) => {
                        const active = shown === m;
                        return (
                            <button
                                key={m}
                                type="button"
                                role="tab"
                                aria-selected={active}
                                onClick={() => setMode(m)}
                                className={cn(
                                    "relative h-7 rounded inline-flex items-center justify-center gap-1.5 transition-colors",
                                    active ? "text-slate-900 font-medium" : "text-slate-500 hover:text-slate-800",
                                )}
                            >
                                {active && (
                                    <motion.span
                                        layoutId="redirect-mode"
                                        className="absolute inset-0 rounded bg-white shadow-sm ring-1 ring-slate-200"
                                        transition={{ type: "spring", duration: 0.3, bounce: 0.15 }}
                                    />
                                )}
                                <span className="relative inline-flex items-center gap-1.5">
                                    {m === "vendor" ? <ProviderLogo id={v!.vendor} size="xs" framed={false} /> : <GlobeIcon className="w-3 h-3" />}
                                    {m === "vendor" ? `With ${name}` : "With DNS records"}
                                </span>
                            </button>
                        );
                    })}
                </div>
            )}
            {v?.forwarding && redirect && (
                <p className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-[11.5px] text-amber-800 leading-relaxed">
                    Both {name} forwarding and a DNS redirect are set for {domain.domain}. Whichever your DNS points at wins, so keep
                    one and remove the other.
                </p>
            )}
            {v && !canVendor && (
                <p className="text-[11.5px] text-slate-500 flex items-start gap-1.5">
                    <ProviderLogo id={v.vendor} size="xs" framed={false} className="mt-px" />
                    <span>{name} cannot forward domains through its API, so the redirect uses DNS records.</span>
                </p>
            )}

            <AnimatePresence mode="wait" initial={false}>
                <motion.div
                    key={shown}
                    initial={{ opacity: 0, x: shown === "vendor" ? -16 : 16 }}
                    animate={{ opacity: 1, x: 0 }}
                    exit={{ opacity: 0, x: shown === "vendor" ? 16 : -16 }}
                    transition={{ duration: 0.16, ease: [0.22, 1, 0.36, 1] }}
                >
                    {shown === "vendor" && v ? (
                        <VendorForwarding domain={domain} link={v} draft={fwdDraft} setDraft={setFwdDraft} />
                    ) : (
                        <DnsRedirect
                            domain={domain}
                            redirect={redirect}
                            urlDraft={urlDraft}
                            setUrlDraft={setUrlDraft}
                            wwwDraft={wwwDraft}
                            setWwwDraft={setWwwDraft}
                        />
                    )}
                </motion.div>
            </AnimatePresence>
        </div>
    );
}

/** The vendor forwards the bare domain; some vendors apply it by hand later. */
function VendorForwarding({
    domain,
    link,
    draft,
    setDraft,
}: {
    domain: SendingDomain;
    link: VendorDomainLink;
    draft: string | null;
    setDraft: (v: string | null) => void;
}) {
    const confirm = useConfirm();
    const save = useSetVendorForwarding();
    const [tried, setTried] = React.useState(false);
    const name = vendorLabel(link.vendor);
    const current = link.forwarding ?? "";
    const url = draft ?? current;
    const problem = redirectTargetProblem(url, domain.domain);
    const changed = url.trim() !== current;

    function normalized(raw: string): string {
        const t = raw.trim();
        return t.includes("://") ? t : `https://${t}`;
    }

    async function submit() {
        setTried(true);
        if (problem || save.isPending || !changed) return;
        try {
            await save.mutateAsync({ domain: domain.domain, url: normalized(url) });
            setDraft(null);
            setTried(false);
            toast.success(
                link.forwarding_reviewed
                    ? `Sent to ${name}. They apply it by hand, usually within a day.`
                    : `${name} now forwards ${domain.domain}.`,
            );
        } catch (e) {
            toast.error(vendorError(e as AppError, name));
        }
    }

    function removeForwarding() {
        confirm.show(`Stop ${name} forwarding ${domain.domain}? Visitors stop being sent to ${shortUrl(current)}.`, async () => {
            try {
                await save.mutateAsync({ domain: domain.domain, url: "" });
                setDraft(null);
                toast.success("Forwarding removed");
            } catch (e) {
                toast.error(vendorError(e as AppError, name));
            }
        });
    }

    return (
        <div className="space-y-4">
            {current ? (
                <div className="rounded-md border border-emerald-200 bg-emerald-50/60 px-3 py-2.5 flex items-start gap-2">
                    <CheckCircle2Icon className="w-3.5 h-3.5 text-emerald-600 mt-0.5 shrink-0" />
                    <div className="min-w-0 text-[11.5px] leading-relaxed">
                        <p className="text-[12.5px] font-medium text-emerald-900">Forwarded by {name}</p>
                        <p className="text-emerald-800/90">
                            {domain.domain} goes to{" "}
                            <a
                                href={current}
                                target="_blank"
                                rel="noreferrer"
                                className="inline-flex items-center gap-0.5 underline decoration-emerald-300 hover:decoration-emerald-600 break-all"
                            >
                                {shortUrl(current)}
                                <ExternalLinkIcon className="w-2.5 h-2.5 shrink-0" />
                            </a>
                            .
                        </p>
                    </div>
                </div>
            ) : (
                <div className="rounded-md border border-slate-200 bg-slate-50/60 px-3 py-2.5 flex items-start gap-2.5">
                    <ProviderLogo id={link.vendor} size="md" />
                    <p className="min-w-0 text-[11.5px] text-slate-600 leading-relaxed">
                        {name} holds {domain.domain}, so it can forward the domain for you. No DNS records to add.
                    </p>
                </div>
            )}

            <div className="space-y-2.5">
                <div>
                    <Label>Website</Label>
                    <TextInput
                        value={url}
                        onChange={setDraft}
                        placeholder="https://yourcompany.com"
                        className="w-full"
                        invalid={tried && !!problem}
                        onKeyDown={(e) => {
                            if (e.key === "Enter") void submit();
                        }}
                    />
                    {tried && problem && <p className="mt-1 text-[11px] text-rose-700">{problem}</p>}
                </div>
                {link.forwarding_reviewed && (
                    <p className="text-[11.5px] text-amber-800 flex items-start gap-1.5 leading-relaxed">
                        <AlertTriangleIcon className="w-3 h-3 mt-0.5 shrink-0" />
                        <span>{name} applies this change by hand, usually within a day.</span>
                    </p>
                )}
                <div className="flex items-center gap-1.5 flex-wrap">
                    <button
                        type="button"
                        onClick={() => void submit()}
                        disabled={save.isPending || !changed}
                        className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                    >
                        {save.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <ZapIcon className="w-3 h-3" />}
                        {current ? "Update forwarding" : `Forward with ${name}`}
                    </button>
                    {draft !== null && changed && (
                        <button
                            type="button"
                            onClick={() => {
                                setDraft(null);
                                setTried(false);
                            }}
                            className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                        >
                            Discard
                        </button>
                    )}
                    {current && link.can_unforward && (
                        <button
                            type="button"
                            onClick={removeForwarding}
                            disabled={save.isPending}
                            className="ml-auto h-7 px-2.5 rounded-md border border-slate-200 text-[12px] text-rose-700 hover:bg-rose-50 hover:border-rose-200 inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                        >
                            <Trash2Icon className="w-3 h-3" />
                            Remove forwarding
                        </button>
                    )}
                </div>
                {current && !link.can_unforward && (
                    <p className="text-[11px] text-slate-400">{name} does not remove forwarding through its API. Change it in {name} directly.</p>
                )}
            </div>
        </div>
    );
}

function DnsRedirect({
    domain,
    redirect,
    urlDraft,
    setUrlDraft,
    wwwDraft,
    setWwwDraft,
}: {
    domain: SendingDomain;
    redirect: DomainRedirect | null;
    urlDraft: string | null;
    setUrlDraft: (v: string | null) => void;
    wwwDraft: boolean | null;
    setWwwDraft: (v: boolean | null) => void;
}) {
    const confirm = useConfirm();
    const save = useSetDomainRedirect();
    const verify = useVerifyDomainRedirect();
    const remove = useDeleteDomainRedirect();
    const [checked, setChecked] = React.useState<DomainRedirect | null>(null);
    const [tried, setTried] = React.useState(false);

    const url = urlDraft ?? redirect?.target_url ?? "";
    const www = wwwDraft ?? redirect?.include_www ?? true;
    const problem = redirectTargetProblem(url, domain.domain);
    const changed = !redirect || url.trim() !== redirect.target_url || www !== redirect.include_www;
    const lastCheck = checked && redirect && checked.id === redirect.id ? checked : null;
    const records = redirect ? mergeRecords(redirect.records, lastCheck) : [];
    const lastError = lastCheck?.last_error ?? redirect?.last_error;

    async function submit() {
        setTried(true);
        if (problem || save.isPending) return;
        try {
            const r = await save.mutateAsync({ domain: domain.domain, body: { target_url: url.trim(), include_www: www } });
            setChecked(r);
            setUrlDraft(null);
            setWwwDraft(null);
            setTried(false);
            toast.success(r.verified ? "Redirect saved and live" : "Redirect saved. Add the DNS records below to switch it on.");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    async function checkNow() {
        if (verify.isPending) return;
        try {
            const r = await verify.mutateAsync(domain.domain);
            setChecked(r);
            if (r.verified) toast.success("DNS is in place. The redirect is live.");
            else toast("Not every record is visible yet. It keeps checking by itself.");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    function removeRedirect() {
        confirm.show(`Stop redirecting ${domain.domain}? Visitors stop being sent to your website once DNS no longer points here.`, async () => {
            try {
                await remove.mutateAsync(domain.domain);
                setChecked(null);
                setUrlDraft(null);
                setWwwDraft(null);
                toast.success("Redirect removed");
            } catch (e) {
                toast.error(buildError(e as AppError));
            }
        });
    }

    return (
        <div className="space-y-5">

            {redirect && (
                <div
                    className={cn(
                        "rounded-md border px-3 py-2.5 flex items-start gap-2",
                        redirect.verified ? "border-emerald-200 bg-emerald-50/60" : "border-amber-200 bg-amber-50/60",
                    )}
                >
                    {redirect.verified ? (
                        <CheckCircle2Icon className="w-3.5 h-3.5 text-emerald-600 mt-0.5 shrink-0" />
                    ) : (
                        <Loader2Icon className="w-3.5 h-3.5 text-amber-600 mt-0.5 shrink-0" />
                    )}
                    <div className="min-w-0 flex-1 text-[11.5px] leading-relaxed">
                        <p className={cn("text-[12.5px] font-medium", redirect.verified ? "text-emerald-900" : "text-amber-900")}>
                            {redirect.verified ? "Live" : "Waiting for DNS"}
                        </p>
                        <p className={redirect.verified ? "text-emerald-800/90" : "text-amber-800/90"}>
                            {redirect.verified ? (
                                <>
                                    {domain.domain}
                                    {redirect.include_www ? ` and www.${domain.domain}` : ""} redirect to{" "}
                                    <a
                                        href={redirect.target_url}
                                        target="_blank"
                                        rel="noreferrer"
                                        className="inline-flex items-center gap-0.5 underline decoration-emerald-300 hover:decoration-emerald-600 break-all"
                                    >
                                        {redirect.target_url}
                                        <ExternalLinkIcon className="w-2.5 h-2.5 shrink-0" />
                                    </a>
                                    .
                                </>
                            ) : (
                                lastError || "Add the records below at your DNS provider."
                            )}
                        </p>
                        {redirect.last_checked_at && (
                            <p className="text-[11px] text-slate-500 mt-0.5">Last checked {timeAgo(lastCheck?.last_checked_at ?? redirect.last_checked_at)}</p>
                        )}
                    </div>
                </div>
            )}

            <div className="space-y-2.5">
                <div>
                    <Label>Website</Label>
                    <TextInput
                        value={url}
                        onChange={setUrlDraft}
                        placeholder="https://yourcompany.com"
                        className="w-full"
                        invalid={tried && !!problem}
                        onKeyDown={(e) => {
                            if (e.key === "Enter") void submit();
                        }}
                    />
                    {tried && problem && <p className="mt-1 text-[11px] text-rose-700">{problem}</p>}
                </div>
                <div className="flex items-center gap-2">
                    <div className="min-w-0 flex-1">
                        <div className="text-[12.5px] text-slate-900">Include www.{domain.domain}</div>
                        <div className="text-[11.5px] text-slate-500">Needs one more record, a CNAME for www.</div>
                    </div>
                    <Toggle value={www} onChange={setWwwDraft} ariaLabel={`Include www.${domain.domain}`} />
                </div>
                <div className="flex items-center gap-1.5">
                    <button
                        type="button"
                        onClick={() => void submit()}
                        disabled={save.isPending || (!!redirect && !changed)}
                        className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                    >
                        {save.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                        {redirect ? "Update redirect" : "Save redirect"}
                    </button>
                    {redirect && changed && (
                        <button
                            type="button"
                            onClick={() => {
                                setUrlDraft(null);
                                setWwwDraft(null);
                                setTried(false);
                            }}
                            className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                        >
                            Discard
                        </button>
                    )}
                </div>
            </div>

            {redirect && (
                <div className="space-y-2">
                    <div className="flex items-center gap-2">
                        <Eyebrow>DNS records for {domain.domain}</Eyebrow>
                        <button
                            type="button"
                            onClick={() => void checkNow()}
                            disabled={verify.isPending}
                            className="ml-auto h-6 px-2 rounded-md border border-slate-200 text-[11.5px] text-slate-700 hover:bg-slate-50 inline-flex items-center gap-1 transition-colors disabled:opacity-50"
                        >
                            <RefreshCwIcon className={cn("w-3 h-3", verify.isPending && "animate-spin")} />
                            Check now
                        </button>
                    </div>
                    <DnsRecordsTable records={records} />
                    <p className="text-[11.5px] text-slate-500 leading-relaxed">
                        The root records replace whatever {domain.domain} serves today. New records can take from a few minutes to a
                        few hours to appear. Warmbly checks by itself every 10 minutes and switches the redirect on once the ownership and
                        root records are in place.
                    </p>
                </div>
            )}

            {redirect && (
                <div className="pt-1">
                    <button
                        type="button"
                        onClick={removeRedirect}
                        disabled={remove.isPending}
                        className="h-7 px-2.5 rounded-md border border-slate-200 text-[12px] text-rose-700 hover:bg-rose-50 hover:border-rose-200 inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                    >
                        <Trash2Icon className="w-3 h-3" />
                        Remove the redirect
                    </button>
                </div>
            )}
        </div>
    );
}

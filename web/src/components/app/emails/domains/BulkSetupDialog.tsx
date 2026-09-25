// One tracking subdomain and one redirect website for many sending domains at
// once. Each domain takes the easiest path: its inbox vendor writes the record
// or forwards the root when it can, otherwise the DNS to add is listed after.
import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";
import { AlertTriangleIcon, CheckCircle2Icon, CircleDashedIcon, CopyIcon, Loader2Icon, SparklesIcon, XIcon } from "lucide-react";
import type { BulkDomainResult, SendingDomain } from "@/lib/api/models/app/emails/SendingDomain";
import { useBulkDomainSetup } from "@/lib/api/hooks/app/emails/useSendingDomains";
import { vendorLabel } from "@/lib/api/models/app/emails/MailboxSources";
import { TextInput } from "@/components/ui/field";
import { Checkbox } from "@/components/ui/checkbox";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { useConfirm } from "@/hooks/context/confirm";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import { DEFAULT_TRACKING_LABEL, labelProblem, vendorForwards } from "@/components/app/emails/import/domainChoiceRules";
import { commonWebsite, noDnsControl, redirectTargetProblem, trackingHostProblem, trackingState, vendorCanCname } from "./rules";
import { Chip } from "./parts";
import { PerDomainList, type DomainOverride, type PerDomainRow } from "./PerDomainList";

export interface BulkSetupIntent {
    domains: string[];
    tracking: boolean;
    redirect: boolean;
}

type Phase = "form" | "running" | "done";

export default function BulkSetupDialog({
    intent,
    all,
    onClose,
    onOpenDomain,
}: {
    /** null while closed. */
    intent: BulkSetupIntent | null;
    /** Every sending domain, for the targets and the website to offer. */
    all: SendingDomain[];
    onClose: () => void;
    onOpenDomain: (domain: string) => void;
}) {
    return <AnimatePresence>{intent && <Dialog key="bulk" intent={intent} all={all} onClose={onClose} onOpenDomain={onOpenDomain} />}</AnimatePresence>;
}

function Dialog({
    intent,
    all,
    onClose,
    onOpenDomain,
}: {
    intent: BulkSetupIntent;
    all: SendingDomain[];
    onClose: () => void;
    onOpenDomain: (domain: string) => void;
}) {
    const confirm = useConfirm();
    const bulk = useBulkDomainSetup();
    const website = React.useMemo(() => commonWebsite(all), [all]);
    const [trackOn, setTrackOn] = React.useState(intent.tracking);
    const [label, setLabel] = React.useState(DEFAULT_TRACKING_LABEL);
    const [replace, setReplace] = React.useState(false);
    const [redirectOn, setRedirectOn] = React.useState(intent.redirect);
    const [url, setUrl] = React.useState(website);
    const [phase, setPhase] = React.useState<Phase>("form");
    const [done, setDone] = React.useState(0);
    const [results, setResults] = React.useState<Map<string, BulkDomainResult>>(() => new Map());

    const picked = React.useMemo(() => {
        const set = new Set(intent.domains);
        return all.filter((d) => set.has(d.domain) && !noDnsControl(d.domain));
    }, [all, intent.domains]);
    const [each, setEach] = React.useState<Record<string, DomainOverride>>({});
    const live = picked.filter((d) => trackingState(d) === "live");
    const base = label.trim().toLowerCase() || DEFAULT_TRACKING_LABEL;
    // Each domain's values: its own where someone changed it, the shared ones otherwise.
    const eff = (d: SendingDomain) => {
        const own = each[d.domain];
        const keep = trackingState(d) === "live" && !replace;
        return {
            track: own?.track ?? (trackOn && !keep),
            host: (own?.host ?? (keep ? (d.tracking_domains[0]?.host ?? "") : `${base}.${d.domain}`)).trim().toLowerCase(),
            redirect: own?.redirect ?? redirectOn,
            url: own?.url ?? url,
        };
    };
    const trackTargets = picked.filter((d) => eff(d).track);
    const redirectTargets = picked.filter((d) => eff(d).redirect);
    const cnameByVendor = trackTargets.filter(vendorCanCname).length;
    const forwardByVendor = redirectTargets.filter((d) => vendorForwards(d.vendor_domain)).length;
    const cnameVendor = vendorLabel(trackTargets.find(vendorCanCname)?.vendor_domain?.vendor);
    const forwardVendor = vendorLabel(redirectTargets.find((d) => vendorForwards(d.vendor_domain))?.vendor_domain?.vendor);
    const n = picked.length;

    const lp = trackTargets.some((d) => each[d.domain]?.host === undefined) ? labelProblem(label) : null;
    const up = redirectTargets.some((d) => each[d.domain]?.url === undefined) ? redirectTargetProblem(url, "") : null;
    const hostProblems = new Map(
        trackTargets.filter((d) => each[d.domain]?.host !== undefined).map((d) => [d.domain, trackingHostProblem(eff(d).host, d.domain)] as const),
    );
    const urlProblems = new Map(redirectTargets.map((d) => [d.domain, redirectTargetProblem(eff(d).url, d.domain)] as const));
    const rowIssue = [...hostProblems, ...urlProblems].find(([, p]) => p);
    const issue =
        n === 0
            ? "None of these domains can take DNS changes."
            : trackTargets.length + redirectTargets.length === 0
              ? trackOn && live.length === n
                  ? "Every picked domain already has a verified tracking domain."
                  : "Turn on a tracking domain, a redirect, or both."
              : lp
                ? `Tracking subdomain: ${lp}`
                : up && url.trim() === ""
                  ? "Enter the website to redirect to."
                  : up
                    ? `Redirect: ${up}`
                    : rowIssue
                      ? `${rowIssue[0]}: ${rowIssue[1]}`
                      : null;
    const listRows: PerDomainRow[] = picked.map((d) => {
        const e = eff(d);
        const own = each[d.domain];
        const verifiedHost = trackingState(d) === "live" && e.host === d.tracking_domains[0]?.host;
        return {
            domain: d.domain,
            canTrack: true,
            track: e.track,
            host: e.host,
            redirect: e.redirect,
            url: e.url,
            urlPlaceholder: url,
            custom: !!own && Object.keys(own).length > 0,
            trackChip: verifiedHost ? (
                <Chip tone="emerald">Verified</Chip>
            ) : vendorCanCname(d) ? (
                <Chip tone="sky">{vendorLabel(d.vendor_domain?.vendor)} adds DNS</Chip>
            ) : (
                <Chip tone="slate">Needs DNS</Chip>
            ),
            redirectChip: vendorForwards(d.vendor_domain) ? <Chip tone="sky">Via {vendorLabel(d.vendor_domain?.vendor)}</Chip> : null,
            hostProblem: hostProblems.get(d.domain) ?? null,
            urlProblem: e.redirect && e.url.trim() ? (urlProblems.get(d.domain) ?? null) : null,
        };
    });
    const total = trackTargets.length + redirectTargets.length;
    const running = phase === "running";
    const dirty =
        phase === "form" &&
        (trackOn !== intent.tracking || redirectOn !== intent.redirect || label !== DEFAULT_TRACKING_LABEL || url !== website || Object.keys(each).length > 0);

    const requestClose = React.useCallback(() => {
        if (running) return;
        if (dirty) confirm.show("Discard these choices?", async () => onClose());
        else onClose();
    }, [running, dirty, confirm, onClose]);

    React.useEffect(() => {
        function onKey(e: KeyboardEvent) {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            requestClose();
        }
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [requestClose]);

    const merge = (rows: BulkDomainResult[]) => {
        setResults((prev) => {
            const next = new Map(prev);
            for (const r of rows) next.set(r.domain, { ...next.get(r.domain), ...r, tracking: r.tracking ?? next.get(r.domain)?.tracking, redirect: r.redirect ?? next.get(r.domain)?.redirect });
            return next;
        });
        setDone((v) => v + rows.length);
    };

    async function run() {
        if (issue || running) return;
        setPhase("running");
        setDone(0);
        setResults(new Map());
        try {
            if (trackTargets.length > 0) {
                const hosts = Object.fromEntries(trackTargets.filter((d) => each[d.domain]?.host !== undefined).map((d) => [d.domain, eff(d).host]));
                await bulk.mutateAsync({
                    body: { domains: trackTargets.map((d) => d.domain), tracking_label: lp === null ? base : undefined, tracking_hosts: hosts },
                    onChunk: merge,
                });
            }
            if (redirectTargets.length > 0) {
                const urls = Object.fromEntries(redirectTargets.filter((d) => each[d.domain]?.url !== undefined).map((d) => [d.domain, eff(d).url.trim()]));
                await bulk.mutateAsync({
                    body: { domains: redirectTargets.map((d) => d.domain), redirect_url: url.trim() || undefined, redirect_urls: urls },
                    onChunk: merge,
                });
            }
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
        setPhase("done");
    }

    const rows = [...results.values()];
    const cnameToAdd = rows.filter((r) => r.tracking && !r.tracking.error && r.tracking.via === "dns" && !r.tracking.verified);
    const redirectDns = rows.filter((r) => r.redirect && !r.redirect.error && r.redirect.via === "dns" && !r.redirect.verified);
    const failed = rows.filter((r) => r.tracking?.error || r.redirect?.error);
    const ready = rows.filter((r) => !r.tracking?.error && !r.redirect?.error).length;

    function copyRecords() {
        const text = cnameToAdd.map((r) => `${r.tracking!.host}\tCNAME\t${r.tracking!.cname_target ?? ""}`).join("\n");
        navigator.clipboard.writeText(text).then(
            () => toast.success(`Copied ${cnameToAdd.length} ${cnameToAdd.length === 1 ? "record" : "records"}`),
            () => toast.error("Could not copy. Select the records and copy them by hand."),
        );
    }

    return (
        <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
            onMouseDown={(e) => {
                if (e.target === e.currentTarget) requestClose();
            }}
            className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
        >
            <motion.div
                initial={{ y: 8, opacity: 0 }}
                animate={{ y: 0, opacity: 1 }}
                exit={{ y: 8, opacity: 0 }}
                transition={{ duration: 0.16 }}
                role="dialog"
                aria-modal="true"
                aria-label="Set up domains"
                className="w-full max-w-[520px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[calc(100dvh-2rem)]"
            >
                <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                    <div className="size-5 rounded bg-sky-50 text-sky-600 flex items-center justify-center">
                        <SparklesIcon className="w-3 h-3" />
                    </div>
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Set up</span>
                    <div className="h-4 w-px bg-slate-200" />
                    <span className="text-[12.5px] text-slate-900 font-medium truncate">
                        {n.toLocaleString()} {n === 1 ? "domain" : "domains"}
                    </span>
                    <button
                        type="button"
                        onClick={requestClose}
                        disabled={running}
                        aria-label="Close"
                        className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors disabled:opacity-40"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                </div>

                <div className="px-4 py-4 flex-1 min-h-0 overflow-y-auto">
                    {phase === "form" ? (
                        <div className="space-y-4">
                            <Section
                                on={trackOn}
                                onToggle={setTrackOn}
                                title="Own tracking domain"
                                recommended
                                body="Links and the open pixel use a host on each domain instead of one shared with other senders, so every link in a message points at the company that sent it."
                            >
                                <div className="flex items-center gap-2">
                                    <span className="inline-flex items-center min-w-0 flex-1 rounded-md border border-slate-200 bg-white focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100">
                                        <input
                                            value={label}
                                            onChange={(e) => setLabel(e.target.value.trim().toLowerCase())}
                                            aria-label="Tracking subdomain"
                                            aria-invalid={!!lp}
                                            spellCheck={false}
                                            className="h-7 w-20 pl-2 bg-transparent text-[12px] font-mono text-slate-900 outline-none"
                                        />
                                        <span className="pr-2 text-[12px] font-mono text-slate-400 truncate">.{picked.length === 1 ? picked[0].domain : "<domain>"}</span>
                                    </span>
                                </div>
                                {lp && <p className="text-[11px] text-rose-700">{lp}</p>}
                                {live.length > 0 && (
                                    <label className="flex items-center gap-2 text-[11.5px] text-slate-600 cursor-pointer">
                                        <Checkbox tone="slate" checked={replace} onChange={(e) => setReplace(e.target.checked)} />
                                        Also replace the verified tracking domain on {live.length.toLocaleString()} {live.length === 1 ? "domain" : "domains"}
                                    </label>
                                )}
                                {trackTargets.length > 0 && (
                                    <Coverage
                                        byVendor={cnameByVendor}
                                        total={trackTargets.length}
                                        vendorText={`${cnameVendor} adds the CNAME`}
                                        selfText="need a CNAME you add; links switch over by themselves once it resolves"
                                    />
                                )}
                            </Section>

                            <Section
                                on={redirectOn}
                                onToggle={setRedirectOn}
                                title="Redirect the root to your website"
                                body="Someone who types one of these domains into a browser lands on your website instead of an empty page, so the domain reads as part of a real business."
                            >
                                <TextInput value={url} onChange={setUrl} placeholder="https://yourcompany.com" invalid={!!up && url.trim() !== ""} className="w-full" />
                                {up && url.trim() !== "" && <p className="text-[11px] text-rose-700">{up}</p>}
                                {redirectTargets.length > 0 && (
                                    <Coverage
                                        byVendor={forwardByVendor}
                                        total={redirectTargets.length}
                                        vendorText={`${forwardVendor} forwards the domain`}
                                        selfText="need DNS records you add, listed on each domain afterwards"
                                    />
                                )}
                            </Section>

                            {n > 1 && (
                                <PerDomainList
                                    rows={listRows}
                                    onChange={(d, patch) => setEach((prev) => ({ ...prev, [d]: { ...prev[d], ...patch } }))}
                                    onReset={(d) =>
                                        setEach((prev) => {
                                            const next = { ...prev };
                                            delete next[d];
                                            return next;
                                        })
                                    }
                                />
                            )}
                        </div>
                    ) : (
                        <div className="space-y-4">
                            <div>
                                <div className="flex items-center justify-between text-[11.5px] text-slate-600 mb-1.5">
                                    <span className="inline-flex items-center gap-1.5">
                                        {running ? <Loader2Icon className="w-3 h-3 animate-spin text-sky-600" /> : <CheckCircle2Icon className="w-3 h-3 text-emerald-600" />}
                                        {running ? "Setting up…" : `${ready.toLocaleString()} of ${new Set([...trackTargets, ...redirectTargets].map((d) => d.domain)).size.toLocaleString()} domains set up`}
                                    </span>
                                    <span className="tabular-nums text-slate-400">
                                        {Math.min(done, total).toLocaleString()} / {total.toLocaleString()}
                                    </span>
                                </div>
                                <div className="h-1 rounded-full bg-slate-100 overflow-hidden">
                                    <motion.div
                                        className="h-full rounded-full bg-sky-500"
                                        initial={false}
                                        animate={{ width: `${total ? Math.round((Math.min(done, total) / total) * 100) : 0}%` }}
                                        transition={{ duration: 0.4, ease: [0.22, 1, 0.36, 1] }}
                                    />
                                </div>
                            </div>

                            {cnameToAdd.length > 0 && (
                                <div className="space-y-1.5">
                                    <div className="flex items-center gap-2">
                                        <Eyebrow>CNAME records to add</Eyebrow>
                                        <button
                                            type="button"
                                            onClick={copyRecords}
                                            className="ml-auto h-6 px-2 rounded-md text-[11px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors"
                                        >
                                            <CopyIcon className="w-3 h-3" />
                                            Copy all
                                        </button>
                                    </div>
                                    <div className="rounded-md border border-slate-200 divide-y divide-slate-100 max-h-48 overflow-y-auto">
                                        {cnameToAdd.map((r) => (
                                            <div key={r.domain} className="px-3 py-1.5 flex items-center gap-2 min-w-0 text-[11.5px] font-mono">
                                                <span className="text-slate-800 truncate">{r.tracking!.host}</span>
                                                <span className="text-slate-400 shrink-0">CNAME</span>
                                                <span className="text-slate-500 truncate ml-auto">{r.tracking!.cname_target}</span>
                                            </div>
                                        ))}
                                    </div>
                                    <p className="text-[11px] text-slate-500 leading-relaxed">
                                        The hosts are already saved. Links keep the shared host until each record resolves, then switch over by themselves.
                                    </p>
                                </div>
                            )}

                            {redirectDns.length > 0 && (
                                <div className="space-y-1.5">
                                    <Eyebrow>Redirects waiting for DNS</Eyebrow>
                                    <p className="text-[11px] text-slate-500 leading-relaxed">
                                        Each needs an ownership TXT record and its root pointed here. Open a domain for its records.
                                    </p>
                                    <div className="flex flex-wrap gap-1">
                                        {redirectDns.map((r) => (
                                            <button
                                                key={r.domain}
                                                type="button"
                                                onClick={() => onOpenDomain(r.domain)}
                                                className="h-6 px-2 rounded-md border border-slate-200 text-[11px] font-mono text-slate-700 hover:border-sky-300 hover:bg-sky-50 transition-colors"
                                            >
                                                {r.domain}
                                            </button>
                                        ))}
                                    </div>
                                </div>
                            )}

                            {failed.length > 0 && (
                                <div className="space-y-1.5">
                                    <Eyebrow>Not set up</Eyebrow>
                                    <div className="rounded-md border border-amber-200 bg-amber-50/40 divide-y divide-amber-100">
                                        {failed.map((r) => (
                                            <div key={r.domain} className="px-3 py-1.5 text-[11.5px] leading-relaxed">
                                                <span className="font-mono text-slate-800">{r.domain}</span>
                                                <span className="text-amber-900">: {r.tracking?.error || r.redirect?.error}</span>
                                            </div>
                                        ))}
                                    </div>
                                </div>
                            )}

                            {!running && failed.length === 0 && cnameToAdd.length === 0 && redirectDns.length === 0 && rows.length > 0 && (
                                <p className="text-[12px] text-emerald-700 inline-flex items-center gap-1.5">
                                    <CheckCircle2Icon className="w-3.5 h-3.5" />
                                    Done, with nothing left to add.
                                </p>
                            )}
                        </div>
                    )}
                </div>

                <div className="px-3 h-12 border-t border-slate-200 flex items-center gap-1.5 shrink-0">
                    {phase === "form" && issue && (
                        <span className="text-[11px] text-slate-500 truncate min-w-0 inline-flex items-center gap-1">
                            <AlertTriangleIcon className="w-3 h-3 shrink-0 text-amber-600" />
                            {issue}
                        </span>
                    )}
                    {phase === "form" ? (
                        <>
                            <button
                                type="button"
                                onClick={requestClose}
                                className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={() => void run()}
                                disabled={!!issue}
                                title={issue ?? undefined}
                                className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50 shrink-0"
                            >
                                Set up {n.toLocaleString()} {n === 1 ? "domain" : "domains"}
                            </button>
                        </>
                    ) : (
                        <button
                            type="button"
                            onClick={onClose}
                            disabled={running}
                            className="ml-auto h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                        >
                            {running && <Loader2Icon className="w-3 h-3 animate-spin" />}
                            {running ? "Working…" : "Done"}
                        </button>
                    )}
                </div>
            </motion.div>
        </motion.div>
    );
}

function Eyebrow({ children }: { children: React.ReactNode }) {
    return <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{children}</div>;
}

function Section({
    on,
    onToggle,
    title,
    body,
    recommended,
    children,
}: {
    on: boolean;
    onToggle: (v: boolean) => void;
    title: string;
    body: string;
    recommended?: boolean;
    children: React.ReactNode;
}) {
    return (
        <div className={cn("rounded-lg border p-3 transition-colors", on ? "border-sky-200 bg-sky-50/30" : "border-slate-200")}>
            <div className="flex items-start gap-3">
                <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5">
                        <span className="text-[12.5px] font-medium text-slate-900">{title}</span>
                        {recommended && (
                            <span className="inline-flex items-center h-[18px] px-1.5 rounded text-[10.5px] font-medium text-sky-700 bg-sky-50">Recommended</span>
                        )}
                    </div>
                    <p className="mt-0.5 text-[11.5px] text-slate-500 leading-relaxed">{body}</p>
                </div>
                <Toggle value={on} onChange={onToggle} ariaLabel={title} />
            </div>
            <AnimatePresence initial={false}>
                {on && (
                    <motion.div
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                        className="overflow-hidden"
                    >
                        <div className="pt-2.5 space-y-1.5">{children}</div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

function Coverage({ byVendor, total, vendorText, selfText }: { byVendor: number; total: number; vendorText: string; selfText: string }) {
    const self = total - byVendor;
    return (
        <div className="space-y-0.5 text-[11px] leading-relaxed">
            {byVendor > 0 && (
                <p className="text-emerald-700 inline-flex items-start gap-1">
                    <CheckCircle2Icon className="w-3 h-3 mt-0.5 shrink-0" />
                    <span>
                        {byVendor === total ? `On all ${total.toLocaleString()}` : `On ${byVendor.toLocaleString()}`}, {vendorText}. Nothing to add.
                    </span>
                </p>
            )}
            {self > 0 && (
                <p className="text-slate-500 flex items-start gap-1">
                    <CircleDashedIcon className="w-3 h-3 mt-0.5 shrink-0" />
                    <span>
                        {self.toLocaleString()} {self === 1 ? "domain" : "domains"} {selfText}.
                    </span>
                </p>
            )}
        </div>
    );
}

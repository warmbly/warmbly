// Run + result: one screen for an import job, live while it runs (realtime
// MAILBOX_IMPORT_PROGRESS, with a 5s poll only while running) and the place to
// finish it afterwards: failures grouped by cause with their fix, a retry per
// cause (optionally with a new password), a per-row fix, and Sign in for rows
// that connect with Google or Microsoft sign-in.
import React from "react";
import { Link } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import toast from "react-hot-toast";
import {
    AlertTriangleIcon,
    Building2Icon,
    CheckCircle2Icon,
    ChevronLeftIcon,
    ChevronRightIcon,
    DownloadIcon,
    GlobeIcon,
    KeyRoundIcon,
    Loader2Icon,
    LogInIcon,
    PlusIcon,
    RotateCcwIcon,
    WrenchIcon,
    XCircleIcon,
    XIcon,
} from "lucide-react";
import { DitherMeter } from "@/components/ui/dither";
import { TextInput } from "@/components/ui/field";
import useMailboxImport from "@/lib/api/hooks/app/emails/useMailboxImport";
import useMailboxImportRows from "@/lib/api/hooks/app/emails/useMailboxImportRows";
import { useCancelMailboxImport, useRetryMailboxImport } from "@/lib/api/hooks/app/emails/useMailboxImportActions";
import downloadMailboxImportFailed from "@/lib/api/client/app/emails/imports/downloadMailboxImportFailed";
import { downloadBlob } from "@/lib/api/client/app/contacts/exportContacts";
import {
    importDone,
    type ImportCause,
    type ImportRow,
    type ImportRowStatus,
    type MailboxImport,
} from "@/lib/api/models/app/emails/MailboxImport";
import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";
import useMailboxOAuth from "@/hooks/useMailboxOAuth";
import useMicrosoftAdminConsent from "@/hooks/useMicrosoftAdminConsent";
import { useGrantConfig } from "@/lib/api/hooks/app/emails/useMailboxGrants";
import { vendorLabel, type DomainGrant } from "@/lib/api/models/app/emails/MailboxSources";
import { useConfirm } from "@/hooks/context/confirm";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import timeAgo from "@/lib/helper/timeAgo";
import { mailHostLabel, mailHostOAuthProvider } from "@/lib/mailHost";
import { cn } from "@/lib/utils";
import {
    AUTHORIZING_STATUS,
    ROW_STATUS,
    VENDOR_AUTHORIZING,
    importSourceName,
    isPasswordCause,
    isSigninCause,
    plural,
} from "./importFields";
import { Banner, HostMark, Linkified, Pill, SectionLabel, StatCard } from "./parts";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import RowFixEditor from "./RowFixEditor";
import { SkeletonRows } from "./Discovering";

const PAGE_SIZE = 25;

interface Filter {
    status: ImportRowStatus | "";
    cause: string;
}

const FILTERS: { status: ImportRowStatus | ""; label: string }[] = [
    { status: "", label: "All" },
    { status: "failed", label: "Failed" },
    { status: "needs_signin", label: "Needs sign-in" },
    { status: "connected", label: "Connected" },
    { status: "updated", label: "Updated" },
    { status: "skipped", label: "Skipped" },
    { status: "queued", label: "Queued" },
    { status: "cancelled", label: "Cancelled" },
];

export default function RunStep({
    importId,
    onDone,
    onAllowance,
    onImportAnother,
    anotherLabel = "Import another list",
    dnsFollowUps = 0,
}: {
    importId: string;
    onDone: () => void;
    onAllowance?: () => void;
    onImportAnother: () => void;
    /** The footer's way back to the start, named for the source. */
    anotherLabel?: string;
    /** Domains whose picked redirect or tracking host still needs DNS records. */
    dnsFollowUps?: number;
}) {
    const qc = useQueryClient();
    const confirm = useConfirm();
    const job = useMailboxImport(importId);
    const retry = useRetryMailboxImport(importId);
    const cancel = useCancelMailboxImport(importId);
    const gmailOAuth = useAuthConfig().config.gmail_oauth_connect === true;
    const [signingLine, setSigningLine] = React.useState<number | null>(null);
    const [downloading, setDownloading] = React.useState(false);

    const oauth = useMailboxOAuth({
        onConnected: () => {
            qc.invalidateQueries({ queryKey: ["emails", "imports", importId] });
            setSigningLine(null);
        },
        onAllowance,
        messages: { loading: "Connecting…", success: "Mailbox connected. Its row updates in a moment." },
    });
    React.useEffect(() => {
        if (!oauth.busy) setSigningLine(null);
    }, [oauth.busy]);

    // Rows that need Microsoft sign-in can connect through one admin consent instead.
    const msGrants = useGrantConfig(true).data?.microsoft_enabled === true;
    const [granted, setGranted] = React.useState<DomainGrant | null>(null);
    const consent = useMicrosoftAdminConsent(setGranted);

    const [filter, setFilter] = React.useState<Filter>({ status: "", cause: "" });
    const [cursors, setCursors] = React.useState<string[]>([""]);
    const [fixing, setFixing] = React.useState<number | null>(null);
    const applyFilter = (f: Filter) => {
        setFilter(f);
        setCursors([""]);
        setFixing(null);
    };

    const data = job.data;
    const running = data?.status === "running";
    const rows = useMailboxImportRows(
        importId,
        { status: filter.status, cause: filter.cause, cursor: cursors[cursors.length - 1] || undefined, limit: PAGE_SIZE },
        running,
    );

    if (job.isLoading) {
        return (
            <div className="p-4 space-y-3" role="status" aria-label="Loading the import">
                <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
                    {Array.from({ length: 4 }, (_, i) => (
                        <span key={i} className="h-14 rounded-md skeleton-shimmer" />
                    ))}
                </div>
                <div className="rounded-md border border-slate-200 overflow-hidden">
                    <SkeletonRows rows={5} />
                </div>
            </div>
        );
    }
    if (!data) {
        return (
            <div className="p-4">
                <Banner tone="red" title="This import could not be loaded">
                    {job.error ? buildError(job.error as unknown as AppError) : "It may have been removed."}
                </Banner>
            </div>
        );
    }

    const c = data.counts;
    const done = importDone(c);
    const total = data.total || done + c.queued + c.running;
    const frac = total > 0 ? done / total : 0;
    const ok = c.connected + c.updated;

    // Which provider a row signs in with, when this deployment offers it one.
    const signInProvider = (row: ImportRow) => {
        const provider = mailHostOAuthProvider(row.mail_host);
        if (!provider) return null;
        if (provider === "gmail" && !gmailOAuth && !oauth.viaCloud) return null;
        return provider;
    };

    const signIn = (row: ImportRow) => {
        const provider = signInProvider(row);
        if (!provider || oauth.busy) return;
        setSigningLine(row.line);
        void oauth.start(provider, { loginHint: row.email || undefined });
    };

    async function runRetry(body: { cause?: string; password?: string }, label: string) {
        try {
            await retry.mutateAsync(body);
            toast.success(label);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    function stop() {
        confirm.show(
            "Stop this import? Mailboxes already connected stay connected. Rows not tried yet are marked cancelled.",
            async () => {
                try {
                    await cancel.mutateAsync();
                } catch (e) {
                    toast.error(buildError(e as AppError));
                }
            },
        );
    }

    async function downloadFailed() {
        if (downloading || !data) return;
        setDownloading(true);
        try {
            const blob = await downloadMailboxImportFailed(importId);
            const base = (data.filename || "mailboxes").replace(/\.[^.]+$/, "");
            downloadBlob(blob, `${base}-failed.csv`);
        } catch (e) {
            toast.error(buildError(e as AppError));
        } finally {
            setDownloading(false);
        }
    }

    const authorizing = data.causes.find((x) => x.cause === VENDOR_AUTHORIZING)?.count ?? 0;
    const signinWaiting = Math.max(0, c.needs_signin - authorizing);
    const failureCauses = data.causes.filter((x) => x.cause !== VENDOR_AUTHORIZING);
    const allowanceHit = data.causes.some((x) => x.cause === "allowance_reached");
    const msSignin = msGrants && c.needs_signin > 0 && data.causes.some((x) => x.cause === "microsoft_signin");
    const reimportHint =
        data.source === "vendor"
            ? `Import them from ${vendorLabel(data.vendor) || "the vendor"} again`
            : data.source === "paste"
              ? "Paste the list again"
              : "Import the file again";
    // Rows parked on the vendor keep the import running; only they left means it is waiting, not importing.
    const onlyAuthorizing = authorizing > 0 && c.queued + c.running === 0;
    const title = running && !onlyAuthorizing
        ? `Importing ${plural(total, "mailbox", "mailboxes")}`
        : data.status === "cancelled"
          ? "Import stopped"
          : c.failed > 0
            ? "Finished with some failures"
            : signinWaiting > 0
              ? `Almost done: ${plural(signinWaiting, "mailbox needs", "mailboxes need")} sign-in`
              : authorizing > 0
                ? `Almost done: ${plural(authorizing, "mailbox is", "mailboxes are")} being authorized`
                : "All mailboxes imported";

    const page = rows.data;
    const pageIndex = cursors.length - 1;

    return (
        <div className="flex flex-col">
            <div className="p-4 space-y-3">
                <div className="flex items-center gap-3">
                    {running && !onlyAuthorizing ? (
                        <Loader2Icon className="w-6 h-6 text-sky-600 animate-spin shrink-0" />
                    ) : data.status === "cancelled" ? (
                        <XCircleIcon className="w-6 h-6 text-slate-400 shrink-0" />
                    ) : authorizing > 0 && c.failed === 0 && signinWaiting === 0 ? (
                        <Loader2Icon className="w-6 h-6 text-sky-600 animate-spin shrink-0" />
                    ) : c.failed > 0 || c.needs_signin > 0 ? (
                        <AlertTriangleIcon className="w-6 h-6 text-amber-600 shrink-0" />
                    ) : (
                        <CheckCircle2Icon className="w-6 h-6 text-emerald-600 shrink-0" />
                    )}
                    <div className="min-w-0 flex-1">
                        <p className="text-[13.5px] text-slate-900 font-semibold truncate">{title}</p>
                        <p className="text-[11.5px] text-slate-500 leading-snug mt-0.5 truncate flex items-center gap-1.5">
                            {data.vendor && <ProviderLogo id={data.vendor} size="xs" framed={false} />}
                            {importSourceName(data)}
                            {(data.source === "file" || data.source === "paste") && data.vendor ? ` · ${vendorLabel(data.vendor)}` : ""} · started{" "}
                            {timeAgo(data.created_at)}
                        </p>
                    </div>
                </div>

                <div>
                    <div className="flex items-baseline justify-between gap-2 mb-1">
                        <span className="text-[12px] text-slate-700 font-medium">Progress</span>
                        <span className="text-[11.5px] font-mono tabular-nums text-slate-700">
                            {done.toLocaleString()}
                            <span className="text-slate-400"> / {total.toLocaleString()}</span>
                        </span>
                    </div>
                    <DitherMeter frac={frac} tone={running ? "sky" : c.failed > 0 ? "amber" : "emerald"} height={6} />
                    {running && (
                        <p className="text-[11.5px] text-slate-500 mt-1.5">
                            Each mailbox is verified against its server, a few seconds each. The import keeps running. You can close this window.
                        </p>
                    )}
                </div>

                <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
                    <StatCard
                        label={c.updated > 0 ? "Connected / updated" : "Connected"}
                        value={ok}
                        accent="emerald"
                        active={filter.status === "connected"}
                        onClick={() => applyFilter({ status: filter.status === "connected" ? "" : "connected", cause: "" })}
                    />
                    <StatCard
                        label="Needs sign-in"
                        value={c.needs_signin}
                        accent={c.needs_signin > 0 ? "sky" : "slate"}
                        active={filter.status === "needs_signin"}
                        onClick={() => applyFilter({ status: filter.status === "needs_signin" ? "" : "needs_signin", cause: "" })}
                    />
                    <StatCard
                        label="Skipped"
                        value={c.skipped + c.cancelled}
                        accent="slate"
                        active={filter.status === "skipped"}
                        onClick={() => applyFilter({ status: filter.status === "skipped" ? "" : "skipped", cause: "" })}
                    />
                    <StatCard
                        label="Failed"
                        value={c.failed}
                        accent={c.failed > 0 ? "red" : "slate"}
                        active={filter.status === "failed" && !filter.cause}
                        onClick={() => applyFilter({ status: filter.status === "failed" && !filter.cause ? "" : "failed", cause: "" })}
                    />
                </div>

                {dnsFollowUps > 0 && (
                    <div className="rounded-md border border-slate-200 px-3 py-2 flex items-center gap-2">
                        <GlobeIcon className="w-3.5 h-3.5 text-sky-600 shrink-0" />
                        <p className="min-w-0 flex-1 text-[11.5px] text-slate-600 leading-relaxed">
                            The tracking domains and redirects you picked switch on once their DNS records are in place.
                        </p>
                        <Link
                            to="/app/emails/domains"
                            onClick={onDone}
                            className="shrink-0 text-[12px] font-medium text-sky-700 hover:text-sky-900 underline decoration-sky-300"
                        >
                            Finish DNS for {plural(dnsFollowUps, "domain", "domains")}
                        </Link>
                    </div>
                )}

                {allowanceHit && (
                    <Banner tone="amber" title="Some rows were past your mailbox allowance">
                        They were not tried.{" "}
                        {onAllowance ? (
                            <button type="button" onClick={onAllowance} className="underline font-medium">
                                Request more
                            </button>
                        ) : (
                            "Request more"
                        )}
                        , then retry them here.
                    </Banner>
                )}

                {authorizing > 0 && (
                    <div className="rounded-md border border-sky-200 bg-sky-50/60 px-3 py-2.5 flex items-start gap-2.5">
                        <Loader2Icon className="w-3.5 h-3.5 text-sky-600 mt-0.5 shrink-0 animate-spin" />
                        <div className="min-w-0 flex-1">
                            <p className="text-[12.5px] font-medium text-sky-900">
                                {vendorLabel(data.vendor) || "Your inbox vendor"} is authorizing Warmbly for{" "}
                                {plural(authorizing, "mailbox", "mailboxes")}
                            </p>
                            <p className="text-[11.5px] text-sky-800/90 leading-relaxed mt-0.5">
                                It approves Warmbly through the admin mailbox it holds on each domain, so nobody has to sign in. This
                                usually takes a few minutes, and the rows connect on their own. You can close this window.
                            </p>
                        </div>
                    </div>
                )}

                {signinWaiting > 0 && (
                    <div className="rounded-md border border-sky-200 bg-sky-50/60 px-3 py-2.5 flex items-start gap-2.5">
                        <LogInIcon className="w-3.5 h-3.5 text-sky-600 mt-0.5 shrink-0" />
                        <div className="min-w-0 flex-1">
                            <p className="text-[12.5px] font-medium text-sky-900">
                                {plural(signinWaiting, "mailbox connects", "mailboxes connect")} with Microsoft or Google sign-in
                            </p>
                            <p className="text-[11.5px] text-sky-800/90 leading-relaxed mt-0.5">
                                Their host does not take a password over IMAP. Use Sign in on each row: the window opens for that
                                address, and the row turns connected once you approve.
                            </p>
                            {msSignin && !granted && (
                                <button
                                    type="button"
                                    onClick={() => void consent.start()}
                                    disabled={consent.busy}
                                    className="mt-2 h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                                >
                                    {consent.busy ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <Building2Icon className="w-3 h-3" />}
                                    {consent.busy ? "Waiting for the administrator…" : "Connect the whole Microsoft 365 organization instead"}
                                </button>
                            )}
                        </div>
                        <button
                            type="button"
                            onClick={() => applyFilter({ status: "needs_signin", cause: "" })}
                            className="shrink-0 h-7 px-2.5 rounded-md border border-sky-200 bg-white text-[12px] text-sky-800 hover:bg-sky-50 transition-colors"
                        >
                            Show them
                        </button>
                    </div>
                )}

                {granted && (
                    <div className="rounded-md border border-emerald-200 bg-emerald-50/60 px-3 py-2.5 flex items-start gap-2.5">
                        <CheckCircle2Icon className="w-3.5 h-3.5 text-emerald-600 mt-0.5 shrink-0" />
                        <div className="min-w-0">
                            <p className="text-[12.5px] font-medium text-emerald-900">
                                Microsoft 365 organization connected{granted.domains.length > 0 ? `: ${granted.domains.join(", ")}` : ""}
                            </p>
                            <p className="text-[11.5px] text-emerald-800/90 leading-relaxed mt-0.5">
                                The rows here stay waiting for sign-in and cannot be retried. {reimportHint}, and every row on a
                                domain the grant covers connects through it, with no sign-in.
                            </p>
                        </div>
                    </div>
                )}

                {failureCauses.length > 0 && (
                    <div className="space-y-1.5">
                        <SectionLabel>Why rows failed</SectionLabel>
                        {failureCauses.map((cause) => (
                            <CauseCard
                                key={cause.cause}
                                cause={cause}
                                busy={retry.isPending}
                                active={filter.cause === cause.cause}
                                onShow={() =>
                                    applyFilter(filter.cause === cause.cause ? { status: "", cause: "" } : { status: "failed", cause: cause.cause })
                                }
                                onRetry={() => void runRetry({ cause: cause.cause }, `${plural(cause.count, "row", "rows")} queued again`)}
                                onRetryWithPassword={(password) =>
                                    runRetry({ cause: cause.cause, password }, `${plural(cause.count, "row", "rows")} queued with the new password`)
                                }
                            />
                        ))}
                    </div>
                )}

                {data.credentials_expire_at && (c.failed > 0 || c.needs_signin > 0 || running) && (
                    <p className="text-[11px] text-slate-500">
                        The passwords from this import are kept for retries until{" "}
                        {new Date(data.credentials_expire_at).toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" })},
                        then deleted. After that a retry needs a new password.
                    </p>
                )}

                <div>
                    <div className="flex items-center gap-1 flex-wrap mb-1.5">
                        {FILTERS.filter((f) => f.status === "" || countFor(data, f.status) > 0 || filter.status === f.status).map((f) => {
                            const active = filter.status === f.status && !filter.cause;
                            return (
                                <button
                                    key={f.status || "all"}
                                    type="button"
                                    onClick={() => applyFilter({ status: f.status, cause: "" })}
                                    className={cn(
                                        "h-6 px-2 rounded-md text-[11.5px] font-medium transition-colors inline-flex items-center gap-1",
                                        active ? "bg-sky-50 text-sky-700" : "text-slate-500 hover:text-slate-900 hover:bg-slate-100",
                                    )}
                                >
                                    {f.label}
                                    {f.status !== "" && <span className="tabular-nums opacity-70">{countFor(data, f.status).toLocaleString()}</span>}
                                </button>
                            );
                        })}
                        {filter.cause && (
                            <span className="inline-flex items-center gap-1 h-6 pl-2 pr-1 rounded-md bg-sky-50 text-sky-700 text-[11.5px] font-medium max-w-[240px]">
                                <span className="truncate">{data.causes.find((x) => x.cause === filter.cause)?.title ?? filter.cause}</span>
                                <button
                                    type="button"
                                    aria-label="Clear the cause filter"
                                    onClick={() => applyFilter({ status: "", cause: "" })}
                                    className="size-4 rounded inline-flex items-center justify-center hover:bg-sky-100"
                                >
                                    <XIcon className="w-2.5 h-2.5" />
                                </button>
                            </span>
                        )}
                        {rows.isFetching && <Loader2Icon className="w-3 h-3 text-slate-400 animate-spin ml-1" />}
                    </div>

                    <div className="rounded-md border border-slate-200 overflow-hidden">
                        <div className="hidden sm:grid grid-cols-[48px_minmax(0,1fr)_120px_minmax(0,1.2fr)_92px] gap-2 px-3 py-1.5 bg-slate-50/60 border-b border-slate-200">
                            {["Line", "Email", "Status", "Detail", ""].map((h, i) => (
                                <span key={i} className="text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">
                                    {h}
                                </span>
                            ))}
                        </div>
                        {rows.isLoading ? (
                            <div className="px-3 py-6 text-center text-[11.5px] text-slate-400">Loading rows</div>
                        ) : !page || page.data.length === 0 ? (
                            <div className="px-3 py-6 text-center text-[11.5px] text-slate-400">No rows here.</div>
                        ) : (
                            page.data.map((row) => {
                                const st =
                                    row.status === "needs_signin" && row.cause === VENDOR_AUTHORIZING
                                        ? AUTHORIZING_STATUS
                                        : (ROW_STATUS[row.status] ?? ROW_STATUS.failed);
                                const provider = signInProvider(row);
                                const wantsSignIn = row.status === "needs_signin" || (row.status === "failed" && isSigninCause(row.cause));
                                const canSignIn = wantsSignIn && !!provider;
                                const canFix = row.status === "failed" || (row.status === "needs_signin" && !provider);
                                return (
                                    <div key={row.line} className="border-b border-slate-100 last:border-b-0">
                                        <div className="grid grid-cols-[40px_minmax(0,1fr)_auto] sm:grid-cols-[48px_minmax(0,1fr)_120px_minmax(0,1.2fr)_92px] gap-2 px-3 py-2 items-start">
                                            <span className="text-[11px] text-slate-500 font-mono pt-0.5">{row.line}</span>
                                            <div className="min-w-0">
                                                <div className="flex items-center gap-1.5 min-w-0">
                                                    <HostMark host={row.mail_host} className="size-5" />
                                                    <span className="text-[12px] text-slate-800 truncate">{row.email || <span className="text-slate-300">no address</span>}</span>
                                                </div>
                                                <div className="sm:hidden mt-1 flex items-center gap-1.5">
                                                    <Pill className={st.cls}>{st.label}</Pill>
                                                    {row.message && <span className="text-[11px] text-slate-500 truncate">{row.message}</span>}
                                                </div>
                                            </div>
                                            <div className="hidden sm:block pt-0.5">
                                                <Pill className={st.cls}>{st.label}</Pill>
                                            </div>
                                            <div className="hidden sm:block min-w-0 text-[11.5px] text-slate-600 leading-snug pt-0.5">
                                                {row.message || (row.mail_host ? mailHostLabel(row.mail_host) : "")}
                                            </div>
                                            <div className="flex justify-end">
                                                {canSignIn ? (
                                                    <button
                                                        type="button"
                                                        onClick={() => signIn(row)}
                                                        disabled={!!oauth.busy}
                                                        className="h-6 px-2 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[11.5px] font-medium inline-flex items-center gap-1 transition-colors disabled:opacity-60"
                                                    >
                                                        {signingLine === row.line ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <LogInIcon className="w-3 h-3" />}
                                                        Sign in
                                                    </button>
                                                ) : canFix ? (
                                                    <button
                                                        type="button"
                                                        onClick={() => setFixing(fixing === row.line ? null : row.line)}
                                                        aria-expanded={fixing === row.line}
                                                        className={cn(
                                                            "h-6 px-2 rounded-md border text-[11.5px] font-medium inline-flex items-center gap-1 transition-colors",
                                                            fixing === row.line
                                                                ? "border-sky-300 bg-sky-50 text-sky-700"
                                                                : "border-slate-200 text-slate-700 hover:bg-slate-50",
                                                        )}
                                                    >
                                                        <WrenchIcon className="w-3 h-3" />
                                                        Fix
                                                    </button>
                                                ) : null}
                                            </div>
                                        </div>
                                        {fixing === row.line && <RowFixEditor importId={importId} row={row} onClose={() => setFixing(null)} />}
                                    </div>
                                );
                            })
                        )}
                        {page && (pageIndex > 0 || page.pagination.has_more) && (
                            <div className="px-3 h-9 border-t border-slate-100 bg-slate-50/40 flex items-center gap-1">
                                <span className="text-[11px] text-slate-500 tabular-nums">Page {pageIndex + 1}</span>
                                <button
                                    type="button"
                                    aria-label="Previous page"
                                    disabled={pageIndex === 0}
                                    onClick={() => {
                                        setCursors((cs) => cs.slice(0, -1));
                                        setFixing(null);
                                    }}
                                    className="ml-auto size-6 rounded-md border border-slate-200 bg-white text-slate-500 hover:text-slate-900 inline-flex items-center justify-center disabled:opacity-40 transition-colors"
                                >
                                    <ChevronLeftIcon className="w-3 h-3" />
                                </button>
                                <button
                                    type="button"
                                    aria-label="Next page"
                                    disabled={!page.pagination.has_more || !page.pagination.next_cursor}
                                    onClick={() => {
                                        const nextCursor = page.pagination.next_cursor;
                                        if (!nextCursor) return;
                                        setCursors((cs) => [...cs, nextCursor]);
                                        setFixing(null);
                                    }}
                                    className="size-6 rounded-md border border-slate-200 bg-white text-slate-500 hover:text-slate-900 inline-flex items-center justify-center disabled:opacity-40 transition-colors"
                                >
                                    <ChevronRightIcon className="w-3 h-3" />
                                </button>
                            </div>
                        )}
                    </div>
                </div>
            </div>

            <div className="px-3 min-h-12 py-1.5 sm:py-0 sm:h-12 border-t border-slate-200 flex items-center gap-1.5 bg-slate-50/80 backdrop-blur-sm sticky bottom-0 z-[2]">
                {c.failed > 0 && (
                    <button
                        type="button"
                        onClick={() => void downloadFailed()}
                        disabled={downloading}
                        title="The failed rows as uploaded, without passwords, plus the error and its fix"
                        className="h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                    >
                        {downloading ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <DownloadIcon className="w-3 h-3" />}
                        <span className="hidden sm:inline">Download failed rows</span>
                        <span className="sm:hidden">Failed rows</span>
                    </button>
                )}
                {!running && (
                    <button
                        type="button"
                        onClick={onImportAnother}
                        className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5 transition-colors"
                    >
                        <PlusIcon className="w-3 h-3" />
                        <span className="hidden sm:inline">{anotherLabel}</span>
                        <span className="sm:hidden">Another</span>
                    </button>
                )}
                <div className="ml-auto flex items-center gap-1.5">
                    {running && (
                        <button
                            type="button"
                            onClick={stop}
                            disabled={cancel.isPending}
                            className="h-7 px-3 rounded-md border border-slate-200 bg-white text-[12px] text-slate-700 hover:bg-slate-50 inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                        >
                            <XIcon className="w-3 h-3" />
                            Stop
                        </button>
                    )}
                    <button
                        type="button"
                        onClick={onDone}
                        className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                    >
                        <CheckCircle2Icon className="w-3 h-3" />
                        Done
                    </button>
                </div>
            </div>
        </div>
    );
}

function countFor(job: MailboxImport, status: ImportRowStatus): number {
    return job.counts[status] ?? 0;
}

function CauseCard({
    cause,
    busy,
    active,
    onShow,
    onRetry,
    onRetryWithPassword,
}: {
    cause: ImportCause;
    busy: boolean;
    active: boolean;
    onShow: () => void;
    onRetry: () => void;
    onRetryWithPassword: (password: string) => Promise<void>;
}) {
    const [password, setPassword] = React.useState("");
    const signin = isSigninCause(cause.cause);
    // The server's flag decides; the sign-in pattern only covers a server that still marks those retryable.
    const canRetry = cause.retryable && !signin;
    const passwordCause = isPasswordCause(cause.cause);
    const appPassword = /app_password/.test(cause.cause);
    return (
        <div className={cn("rounded-md border px-3 py-2.5", active ? "border-sky-300 bg-sky-50/40" : "border-slate-200")}>
            <div className="flex items-baseline gap-2">
                <span className="text-[12.5px] font-medium text-slate-900 min-w-0 flex-1">{cause.title}</span>
                <span className="text-[11px] font-mono tabular-nums text-red-600 shrink-0">{plural(cause.count, "row", "rows")}</span>
            </div>
            <p className="text-[11.5px] text-slate-600 leading-relaxed mt-0.5">
                <Linkified text={cause.fix} />
            </p>
            <div className="mt-2 flex items-center gap-1.5 flex-wrap">
                <button
                    type="button"
                    onClick={onShow}
                    className="h-6 px-2 rounded-md text-[11.5px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                >
                    {active ? "Show all rows" : "Show these rows"}
                </button>
                {canRetry && (
                    <button
                        type="button"
                        onClick={onRetry}
                        disabled={busy}
                        className="h-6 px-2 rounded-md border border-slate-200 text-[11.5px] text-slate-700 hover:bg-slate-50 inline-flex items-center gap-1 transition-colors disabled:opacity-50"
                    >
                        <RotateCcwIcon className="w-3 h-3" />
                        Retry these
                    </button>
                )}
                {signin && (
                    <span className="text-[11px] text-slate-500">Use Sign in on each of these rows.</span>
                )}
            </div>
            {passwordCause && canRetry && (
                <form
                    className="mt-2 flex items-center gap-1.5"
                    onSubmit={(e) => {
                        e.preventDefault();
                        if (!password || busy) return;
                        void onRetryWithPassword(password).then(() => setPassword(""));
                    }}
                >
                    <KeyRoundIcon className="w-3 h-3 text-slate-400 shrink-0" />
                    <TextInput
                        value={password}
                        onChange={setPassword}
                        type="password"
                        autoComplete="new-password"
                        placeholder={`New ${appPassword ? "app password" : "password"} for these ${cause.count.toLocaleString()}`}
                        className="flex-1"
                    />
                    <button
                        type="submit"
                        disabled={!password || busy}
                        className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1 transition-colors disabled:opacity-50 shrink-0"
                    >
                        Retry with it
                    </button>
                </form>
            )}
        </div>
    );
}

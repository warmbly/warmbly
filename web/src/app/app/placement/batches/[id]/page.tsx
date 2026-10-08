// One placement batch: how far it has got, where the copies landed overall,
// and the same result by mailbox, sending domain, sending provider and
// recipient provider, so one bad mailbox reads apart from a domain or a
// provider losing reputation. Live through the child tests' events.

import React from "react";
import useBrowseState from "@/hooks/useBrowseState";
import { batchTab, batchSenderSort, batchSenderStatus, browseString, browseBoolean } from "@/lib/browse-accounts-analytics";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { motion } from "framer-motion";
import { ArrowLeftIcon, ArrowUpRightIcon, AtSignIcon, Grid3x3Icon, Loader2Icon, MailIcon, ServerIcon, SquareIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { EmptyBlock, SectionBar } from "@/components/layout/Page";
import ScrollStrip from "@/components/ui/scroll-strip";
import { SearchInput } from "@/components/ui/field";
import { SelectMenu } from "@/components/ui/select-menu";
import PermissionButton from "@/components/ui/PermissionButton";
import { IssueRow } from "@/components/app/campaigns/ContentScore";
import { useConfirm } from "@/hooks/context/confirm";
import useDebouncedValue from "@/hooks/useDebouncedValue";
import useCampaign from "@/lib/api/hooks/app/campaigns/useCampaign";
import { useCancelPlacementBatch, usePlacementBatch, usePlacementBatchSenders } from "@/lib/api/hooks/app/placement/usePlacement";
import {
    PANEL_LABEL,
    type PlacementBatchDetail,
    type PlacementBatchGroup,
    type PlacementBatchSender,
    type PlacementBatchSenderSort,
    type PlacementBatchSenderStatus,
    type PlacementCounts,
    type PlacementTracking,
} from "@/lib/api/models/app/placement/Placement";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { PlacementBar, PlacementCaveat, PlacementLegend } from "@/components/app/placement/tests/PlacementParts";
import { FOLDER, fmtDate, fmtRate, rateTone } from "@/components/app/placement/tests/placementTests";
import { BatchProgressBar, BatchStatusChip, SenderStatusChip } from "@/components/app/placement/batches/BatchParts";
import {
    SENDER_REASON,
    SENDER_STATUS,
    batchOpen,
    progressLine,
    scopeSummary,
} from "@/components/app/placement/batches/placementBatches";
import { cn } from "@/lib/utils";

type Tab = "mailboxes" | "domains" | "providers" | "recipients";

const TABS: { key: Tab; label: string; icon: typeof MailIcon }[] = [
    { key: "mailboxes", label: "Mailboxes", icon: MailIcon },
    { key: "domains", label: "Domains", icon: AtSignIcon },
    { key: "providers", label: "Providers", icon: ServerIcon },
    { key: "recipients", label: "Recipient providers", icon: Grid3x3Icon },
];

const TRACKING_LABEL: Record<PlacementTracking, string> = {
    campaign: "Tracking as the campaign",
    on: "Tracked",
    off: "Untracked",
    compare: "With and without tracking",
};

// Domains drawn in the matrix before "Show all".
const MATRIX_ROWS = 100;

export default function PlacementBatchPage() {
    const { id = "" } = useParams({ from: "/app/placement/batches/$id" });
    const q = usePlacementBatch(id);
    const err = q.error as unknown as AppError | null;

    return (
        <div className="flex flex-col min-h-full bg-white">
            <div className="px-3 sm:px-5 pt-3 sm:pt-4">
                <Link
                    to="/app/placement"
                    search={{ tab: "batches" }}
                    className="inline-flex items-center gap-1 h-6 -ml-1.5 px-1.5 mb-1 rounded-md text-[11.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                >
                    <ArrowLeftIcon className="w-3 h-3" />
                    Placement tests
                </Link>
            </div>
            {q.isLoading ? (
                <div className="px-3 sm:px-5 pb-6 space-y-3">
                    <div className="h-6 w-72 max-w-full rounded bg-slate-100 animate-pulse" />
                    <div className="h-3 w-96 max-w-full rounded bg-slate-50 animate-pulse" />
                    <div className="h-2 w-full rounded bg-slate-50 animate-pulse" />
                </div>
            ) : q.isError || !q.data ? (
                <EmptyBlock
                    title={err?.status === 404 ? "This batch does not exist" : "This batch could not be loaded"}
                    body={err?.status === 404 ? "It may belong to another workspace." : err ? buildError(err) : undefined}
                />
            ) : (
                <Detail key={q.data.id} batch={q.data} />
            )}
        </div>
    );
}

function Detail({ batch }: { batch: PlacementBatchDetail }) {
    const confirm = useConfirm();
    const cancel = useCancelPlacementBatch();
    const campaign = useCampaign(batch.campaign_id ?? "");
    const [tab, setTab] = useBrowseState<Tab>(`placement.batch.${batch.id}.tab`, "mailboxes", batchTab);
    const open = batchOpen(batch.status);
    const p = batch.progress;

    const onCancel = () =>
        confirm.show(
            "Stop this batch? Senders that have not started are cancelled. Copies already sent keep being classified.",
            async () => {
                try {
                    await cancel.mutateAsync(batch.id);
                    toast.success("Batch stopped.");
                } catch (e) {
                    const err = e as AppError;
                    toast.error(err?.code === "placement_batch_not_running" ? "This batch has already finished." : buildError(err));
                }
            },
        );

    const counts: Record<Tab, number | undefined> = {
        mailboxes: p.total,
        domains: batch.domains.length || undefined,
        providers: batch.providers.length || undefined,
        recipients: batch.recipients.length || undefined,
    };

    return (
        <>
            {/* Header */}
            <div className="px-3 sm:px-5 pb-4 flex flex-wrap items-start gap-3 border-b border-slate-200">
                <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                        <h1 className="min-w-0 max-w-full text-[18px] font-semibold text-slate-900 truncate">{batch.subject || "(no subject)"}</h1>
                        <BatchStatusChip status={batch.status} />
                        <span className="inline-flex items-center h-5 px-1.5 rounded-md bg-slate-100 text-slate-600 text-[10.5px] font-medium whitespace-nowrap">
                            {PANEL_LABEL[batch.panel] ?? batch.panel}
                        </span>
                        <span
                            className={cn(
                                "inline-flex items-center h-5 px-1.5 rounded-md text-[10.5px] font-medium whitespace-nowrap",
                                batch.tracking === "compare" ? "bg-sky-50 text-sky-700" : batch.tracking === "on" ? "bg-amber-50 text-amber-700" : "bg-slate-100 text-slate-600",
                            )}
                        >
                            {TRACKING_LABEL[batch.tracking] ?? batch.tracking}
                        </span>
                    </div>
                    <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11.5px] text-slate-500">
                        <span>{scopeSummary(batch)}</span>
                        <span>{batch.started_at ? `Started ${fmtDate(batch.started_at)}` : `Queued ${fmtDate(batch.created_at)}`}</span>
                        {batch.finished_at && <span>Finished {fmtDate(batch.finished_at)}</span>}
                        {batch.credits_spent > 0 && <span>Paid {batch.credits_spent.toLocaleString()} credits</span>}
                        {batch.campaign_id && (
                            <Link to="/app/campaigns/$id/steps" params={{ id: batch.campaign_id }} className="inline-flex items-center gap-0.5 text-sky-700 hover:text-sky-800">
                                {campaign.data?.name ?? "Campaign"}
                                <ArrowUpRightIcon className="w-3 h-3" />
                            </Link>
                        )}
                    </div>
                    {batch.error && <p className="mt-1.5 text-[11.5px] text-rose-600">{batch.error}</p>}
                </div>
                {open && (
                    <PermissionButton
                        permission="SEND_CAMPAIGNS"
                        type="button"
                        onClick={onCancel}
                        disabled={cancel.isPending}
                        className="shrink-0 h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-[12px] font-medium text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                    >
                        {cancel.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <SquareIcon className="w-3 h-3" />}
                        Stop batch
                    </PermissionButton>
                )}
            </div>

            {/* Progress */}
            <section className="px-5 py-4 border-b border-slate-200">
                <div className="flex items-baseline gap-2">
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Senders</span>
                    <span className="font-mono text-[10.5px] text-slate-400 tabular-nums">{p.total.toLocaleString()}</span>
                </div>
                <BatchProgressBar progress={p} height={8} className="mt-2" />
                <p className="mt-2 text-[11.5px] text-slate-500">
                    {progressLine(p) || "No senders yet."}
                    {p.deferred > 0 && open && ` Deferred senders are retried until ${fmtDate(batch.retry_until)}.`}
                </p>
            </section>

            {/* Overall placement */}
            <section className="border-b border-slate-200">
                <SectionBar label="Where it landed">
                    <PlacementLegend className="hidden md:flex" />
                </SectionBar>
                {batch.untracked ? (
                    <div className="grid sm:grid-cols-2">
                        <Overall title="With tracking" counts={batch.summary} className="sm:border-r border-slate-200 max-sm:border-b" />
                        <Overall title="Without tracking" counts={batch.untracked} />
                    </div>
                ) : (
                    <Overall counts={batch.summary} />
                )}
            </section>

            <ScrollStrip activeKey={tab} className="shrink-0 border-b border-slate-200" innerClassName="px-3 gap-1">
                {TABS.map((t) => {
                    const active = tab === t.key;
                    return (
                        <button
                            key={t.key}
                            type="button"
                            data-active={active}
                            onClick={() => setTab(t.key)}
                            className={cn(
                                "relative h-10 px-2.5 inline-flex shrink-0 items-center gap-1.5 text-[12.5px] transition-colors",
                                active ? "text-slate-900 font-medium" : "text-slate-500 hover:text-slate-800",
                            )}
                        >
                            <t.icon className="w-3.5 h-3.5" />
                            {t.label}
                            {counts[t.key] != null && (
                                <span className="font-mono text-[10.5px] text-slate-400 tabular-nums">{counts[t.key]?.toLocaleString()}</span>
                            )}
                            {active && (
                                <motion.span
                                    layoutId="placement-batch-tab-underline"
                                    className="absolute left-1.5 right-1.5 bottom-0 h-0.5 rounded-full bg-sky-600"
                                    transition={{ type: "spring", duration: 0.3, bounce: 0.15 }}
                                />
                            )}
                        </button>
                    );
                })}
            </ScrollStrip>

            {tab === "mailboxes" && <SendersTab batch={batch} />}
            {tab === "domains" && <GroupTable groups={batch.domains} label="Domain" empty="No domain has a verdict yet." />}
            {tab === "providers" && <GroupTable groups={batch.providers} label="Provider" empty="No provider has a verdict yet." />}
            {tab === "recipients" && <Matrix batch={batch} />}

            <ContentCheck batch={batch} />
            <PlacementCaveat className="mx-5 my-5" />
        </>
    );
}

// Overall rates for one copy (or one half of a comparison).
function Overall({ title, counts, className }: { title?: string; counts: PlacementCounts; className?: string }) {
    const tabs = counts.promotions + counts.other;
    const cells: { label: string; rate: number | null; n: number; tone: string }[] = [
        { label: "Inbox", rate: counts.inbox_rate, n: counts.inbox, tone: FOLDER.inbox.text },
        { label: "Gmail tabs", rate: counts.tabs_rate, n: tabs, tone: FOLDER.promotions.text },
        { label: "Spam", rate: counts.spam_rate, n: counts.spam, tone: FOLDER.spam.text },
        { label: "Never arrived", rate: counts.missing_rate, n: counts.missing, tone: FOLDER.missing.text },
    ];
    return (
        <div className={cn("px-5 py-4 min-w-0", className)}>
            {title && <div className="mb-2 text-[12.5px] font-medium text-slate-900">{title}</div>}
            <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
                {cells.map((c) => (
                    <div key={c.label}>
                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{c.label}</div>
                        <div className={cn("mt-1 text-[22px] font-light leading-none tabular-nums", c.rate == null ? "text-slate-300" : c.tone)}>
                            {fmtRate(c.rate)}
                        </div>
                        <div className="mt-1 text-[10.5px] text-slate-400 font-mono">
                            {c.n.toLocaleString()} cop{c.n === 1 ? "y" : "ies"}
                        </div>
                    </div>
                ))}
            </div>
            <PlacementBar counts={counts} height={8} className="mt-3" />
            <p className="mt-2 text-[11px] text-slate-500">
                {counts.total === 0
                    ? "No copy has been sent yet."
                    : `${counts.delivered.toLocaleString()} of ${counts.total.toLocaleString()} copies sent and classified${counts.pending > 0 ? `, ${counts.pending.toLocaleString()} waiting` : ""}. Rates are shares of the copies that were sent.`}
            </p>
        </div>
    );
}

const SORTS: { value: PlacementBatchSenderSort; label: string }[] = [
    { value: "worst", label: "Worst inbox rate first" },
    { value: "best", label: "Best inbox rate first" },
    { value: "email", label: "Address" },
    { value: "status", label: "Status" },
];

const STATUS_ORDER: PlacementBatchSenderStatus[] = ["running", "queued", "deferred", "completed", "skipped", "failed", "cancelled"];

function SendersTab({ batch }: { batch: PlacementBatchDetail }) {
    const navigate = useNavigate();
    const [sort, setSort] = useBrowseState<PlacementBatchSenderSort>(`placement.batch.${batch.id}.senders.sort`, "worst", batchSenderSort);
    const [status, setStatus] = useBrowseState<PlacementBatchSenderStatus | "">(`placement.batch.${batch.id}.senders.status`, "", batchSenderStatus);
    const [q, setQ] = useBrowseState(`placement.batch.${batch.id}.senders.search`, "", browseString);
    const debounced = useDebouncedValue(q.trim(), 300);
    const list = usePlacementBatchSenders(batch.id, { sort, status, q: debounced });
    const p = batch.progress;

    const statusOptions = [
        { value: "", label: `Every status (${p.total.toLocaleString()})` },
        ...STATUS_ORDER.filter((s) => p[s] > 0 || s === status).map((s) => ({
            value: s,
            label: `${SENDER_STATUS[s].label} (${p[s].toLocaleString()})`,
        })),
    ];
    const showResults = list.senders.some((s) => s.summary.total > 0);

    return (
        <section className="flex flex-col">
            <div className="px-5 py-2 border-b border-slate-200/60 flex flex-wrap items-center gap-2">
                <div className="flex-1 min-w-[180px] max-w-sm">
                    <SearchInput value={q} onChange={setQ} placeholder="Search mailboxes…" />
                </div>
                <div className="ml-auto flex flex-wrap items-center gap-2">
                    <SelectMenu
                        value={status}
                        onChange={(v) => setStatus(v as PlacementBatchSenderStatus | "")}
                        options={statusOptions}
                        aria-label="Status"
                        align="end"
                    />
                    <SelectMenu
                        value={sort}
                        onChange={(v) => setSort(v as PlacementBatchSenderSort)}
                        options={SORTS}
                        aria-label="Sort"
                        align="end"
                    />
                </div>
            </div>

            {list.isLoading ? (
                <div className="divide-y divide-slate-200/60">
                    {Array.from({ length: 5 }).map((_, i) => (
                        <div key={i} className="h-12 px-5 flex items-center gap-4">
                            <div className="h-3 w-48 rounded bg-slate-100 animate-pulse" />
                            <div className="h-3 flex-1 rounded bg-slate-50 animate-pulse" />
                        </div>
                    ))}
                </div>
            ) : list.isError ? (
                <EmptyBlock title="Mailboxes could not be loaded" body={buildError(list.error as unknown as AppError)} />
            ) : list.senders.length === 0 ? (
                <p className="px-5 py-6 text-[12px] text-slate-400">{debounced || status ? "No mailbox matches that." : "No senders in this batch."}</p>
            ) : (
                <div className="overflow-x-auto">
                    <table className="w-full text-left">
                        <thead>
                            <tr className="h-8 border-b border-slate-200/60 text-[10px] uppercase tracking-[0.14em] text-slate-400">
                                <th className="px-5 font-medium">Mailbox</th>
                                <th className="px-3 font-medium">Status</th>
                                {showResults && (
                                    <>
                                        <th className="px-3 font-medium hidden sm:table-cell w-[20%]">Where it landed</th>
                                        <th className="px-3 font-medium text-right">Inbox</th>
                                        <th className="px-5 font-medium text-right hidden md:table-cell">Spam</th>
                                    </>
                                )}
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-slate-200/60">
                            {list.senders.map((s) => (
                                <SenderRow
                                    key={s.id}
                                    sender={s}
                                    showResults={showResults}
                                    onOpen={s.test_ids.length > 0 ? () => navigate({ to: "/app/placement/$id", params: { id: s.test_ids[0] } }) : undefined}
                                />
                            ))}
                        </tbody>
                    </table>
                    {list.hasNextPage && (
                        <div className="px-5 py-3 flex justify-center">
                            <button
                                type="button"
                                onClick={() => list.fetchNextPage()}
                                disabled={list.isFetchingNextPage}
                                className="h-7 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] font-medium text-slate-700 inline-flex items-center gap-1.5 disabled:opacity-60"
                            >
                                {list.isFetchingNextPage && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                                Load more
                            </button>
                        </div>
                    )}
                </div>
            )}
        </section>
    );
}

function SenderRow({ sender: s, showResults, onOpen }: { sender: PlacementBatchSender; showResults: boolean; onOpen?: () => void }) {
    const explained = s.status === "skipped" || s.status === "deferred" || s.status === "failed";
    const reason = s.reason ? (SENDER_REASON[s.reason] ?? s.detail) : s.detail;
    const sub =
        s.status === "deferred"
            ? `${reason ? `${reason}. ` : ""}Retries ${fmtDate(s.next_attempt_at)}`
            : explained
              ? reason
              : undefined;
    return (
        <tr
            onClick={onOpen}
            onKeyDown={(e) => {
                if (e.key === "Enter" && onOpen) onOpen();
            }}
            tabIndex={onOpen ? 0 : undefined}
            className={cn("h-12", onOpen && "cursor-pointer hover:bg-slate-50/80 transition-colors outline-none focus-visible:bg-slate-50")}
        >
            <td className="px-5 py-2 max-w-0 w-[40%]">
                <div className="flex items-center gap-1.5 min-w-0">
                    <span className="text-[12.5px] font-medium text-slate-900 truncate">{s.sender_email || "Deleted mailbox"}</span>
                    {s.sender_family_label && (
                        <span className="shrink-0 h-4 px-1.5 rounded bg-slate-100 text-[10px] text-slate-500 inline-flex items-center">
                            {s.sender_family_label}
                        </span>
                    )}
                </div>
            </td>
            <td className="px-3 py-2 max-w-0">
                <SenderStatusChip status={s.status} />
                {sub && (
                    <div className="mt-0.5 text-[11px] text-slate-500 truncate" title={s.detail || sub}>
                        {sub}
                    </div>
                )}
            </td>
            {showResults && (
                <>
                    <td className="px-3 py-2 hidden sm:table-cell">{s.summary.total > 0 && <PlacementBar counts={s.summary} />}</td>
                    <td className={cn("px-3 py-2 text-right font-mono text-[12px] tabular-nums", rateTone(s.summary.inbox_rate))}>
                        {fmtRate(s.summary.inbox_rate)}
                    </td>
                    <td className="px-5 py-2 text-right hidden md:table-cell font-mono text-[12px] tabular-nums text-rose-600">
                        {s.summary.spam_rate == null ? <span className="text-slate-400">—</span> : fmtRate(s.summary.spam_rate)}
                    </td>
                </>
            )}
        </tr>
    );
}

function GroupTable({ groups, label, empty }: { groups: PlacementBatchGroup[]; label: string; empty: string }) {
    if (groups.length === 0) return <p className="px-5 py-6 text-[12px] text-slate-400">{empty}</p>;
    return (
        <div className="overflow-x-auto">
            <table className="w-full text-left">
                <thead>
                    <tr className="h-8 border-b border-slate-200/60 text-[10px] uppercase tracking-[0.14em] text-slate-400">
                        <th className="px-5 font-medium">{label}</th>
                        <th className="px-3 font-medium text-right hidden sm:table-cell">Tested</th>
                        <th className="px-3 font-medium w-[26%] hidden sm:table-cell" />
                        <th className="px-3 font-medium text-right">Inbox</th>
                        <th className="px-3 font-medium text-right">Spam</th>
                        <th className="px-5 font-medium text-right hidden md:table-cell">Missing</th>
                    </tr>
                </thead>
                <tbody className="divide-y divide-slate-200/60">
                    {groups.map((g) => (
                        <tr key={g.key} className="h-10">
                            <td className="px-5 text-[12.5px] font-medium text-slate-900 whitespace-nowrap">{g.label || g.key}</td>
                            <td className="px-3 text-right font-mono text-[11px] tabular-nums text-slate-500 hidden sm:table-cell">
                                {g.tested.toLocaleString()}/{g.senders.toLocaleString()}
                            </td>
                            <td className="px-3 hidden sm:table-cell">
                                <PlacementBar counts={g.counts} />
                            </td>
                            <td className={cn("px-3 text-right font-mono text-[11.5px] tabular-nums", rateTone(g.counts.inbox_rate))}>
                                {fmtRate(g.counts.inbox_rate)}
                            </td>
                            <td className="px-3 text-right font-mono text-[11.5px] tabular-nums text-rose-600">{fmtRate(g.counts.spam_rate)}</td>
                            <td className="px-5 text-right font-mono text-[11.5px] tabular-nums text-slate-500 hidden md:table-cell">
                                {fmtRate(g.counts.missing_rate)}
                            </td>
                        </tr>
                    ))}
                </tbody>
            </table>
        </div>
    );
}

// Sending domain by recipient provider: the inbox rate in each cell.
function Matrix({ batch }: { batch: PlacementBatchDetail }) {
    const [all, setAll] = useBrowseState(`placement.batch.${batch.id}.matrix.showAll`, false, browseBoolean);
    const cols = batch.recipients;
    if (cols.length === 0 || batch.matrix.length === 0) {
        return <p className="px-5 py-6 text-[12px] text-slate-400">No copy has a verdict yet.</p>;
    }
    const rows = all ? batch.matrix : batch.matrix.slice(0, MATRIX_ROWS);
    const cell = (c: PlacementCounts | undefined) =>
        !c || c.inbox_rate == null ? (
            <span className="text-slate-300">—</span>
        ) : (
            <span className={rateTone(c.inbox_rate)} title={`${c.inbox} of ${c.delivered} in the inbox`}>
                {fmtRate(c.inbox_rate)}
            </span>
        );
    return (
        <div>
            <p className="px-5 pt-3 text-[11px] text-slate-400">
                Inbox rate for each sending domain at each recipient provider, worst domains first.
            </p>
            <div className="overflow-x-auto">
                <table className="text-left min-w-full">
                    <thead>
                        <tr className="h-8 border-b border-slate-200/60 text-[10px] uppercase tracking-[0.14em] text-slate-400">
                            <th className="px-5 font-medium sticky left-0 bg-white">Sending domain</th>
                            {cols.map((c) => (
                                <th key={c.family} className="px-3 font-medium text-right whitespace-nowrap">
                                    {c.label || c.family}
                                </th>
                            ))}
                        </tr>
                    </thead>
                    <tbody className="divide-y divide-slate-200/60">
                        <tr className="h-9 bg-slate-50/60">
                            <td className="px-5 text-[12px] font-medium text-slate-700 whitespace-nowrap sticky left-0 bg-slate-50">Every domain</td>
                            {cols.map((c) => (
                                <td key={c.family} className="px-3 text-right font-mono text-[11.5px] tabular-nums">
                                    {cell(c.counts)}
                                </td>
                            ))}
                        </tr>
                        {rows.map((r) => (
                            <tr key={r.domain} className="h-9">
                                <td className="px-5 text-[12px] text-slate-900 whitespace-nowrap sticky left-0 bg-white">{r.domain}</td>
                                {cols.map((c) => (
                                    <td key={c.family} className="px-3 text-right font-mono text-[11.5px] tabular-nums">
                                        {cell(r.recipients.find((x) => x.family === c.family)?.counts)}
                                    </td>
                                ))}
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>
            {batch.matrix.length > MATRIX_ROWS && (
                <div className="px-5 py-3 flex justify-center">
                    <button
                        type="button"
                        onClick={() => setAll((v) => !v)}
                        className="h-7 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] font-medium text-slate-700"
                    >
                        {all ? `Show the first ${MATRIX_ROWS}` : `Show all ${batch.matrix.length.toLocaleString()} domains`}
                    </button>
                </div>
            )}
        </div>
    );
}

function ContentCheck({ batch }: { batch: PlacementBatchDetail }) {
    const { score } = batch.content;
    const issues = batch.content.issues ?? [];
    const tone = score >= 80 ? "text-emerald-600" : score >= 50 ? "text-amber-600" : "text-rose-600";
    const label = score >= 80 ? "Looks good" : score >= 50 ? "Could improve" : "Needs work";
    return (
        <section className="border-t border-slate-200 mt-4">
            <SectionBar label="Content check" />
            <div className="px-5 py-3">
                <div className="flex items-baseline gap-2">
                    <span className={cn("text-[22px] font-light tabular-nums", tone)}>{score}</span>
                    <span className="text-[11px] text-slate-400">out of 100</span>
                    <span className={cn("text-[11.5px] font-medium", tone)}>{label}</span>
                </div>
                {issues.length === 0 ? (
                    <p className="mt-1 text-[11.5px] text-slate-500">Nothing in the copy stands out to a spam filter.</p>
                ) : (
                    <ul className="mt-1 divide-y divide-slate-100 max-w-2xl">
                        {issues.map((issue, i) => (
                            <IssueRow key={`${issue.code}-${i}`} issue={issue} />
                        ))}
                    </ul>
                )}
                <p className="mt-2 text-[11px] text-slate-400 leading-relaxed">
                    The copy is the same for every sender, so a sender or domain that lands worse than the rest points at
                    reputation, not content.
                </p>
            </div>
        </section>
    );
}

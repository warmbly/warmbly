// Automatic inbox tagging: the review surface.
//
// The feature labels inbound mail and scores it for relevance, and, per the
// switches under Settings > Sending, may hold, stop, open a task for or
// suppress the sender. This page shows the confidence, where the verdict came
// from, and what it did, so a person can judge the reversible actions that
// run from day one and decide when to trust the suppression.

import useBrowseState from "@/hooks/useBrowseState";
import { browseFlagSchema } from "@/lib/browse-other-lists";
import { Link } from "@tanstack/react-router";
import { CheckIcon, FilterIcon, InfoIcon, SparklesIcon } from "lucide-react";

import {
    EmptyBlock,
    Page,
    PageBody,
    PageTopbar,
    SectionBar,
    Stat,
    StatStrip,
} from "@/components/layout/Page";
import { NoAccess } from "@/components/layout/NoAccess";
import { TagMeaningTooltip } from "@/components/ui/tag-meaning-tooltip";
import { usePermission } from "@/hooks/usePermission";
import useInboxTagReview from "@/lib/api/hooks/app/inboxtag/useInboxTagReview";
import type { InboxTagRow } from "@/lib/api/models/app/inboxtag/InboxTagReview";
import { cn } from "@/lib/utils";

const ACTION_LABEL: Record<string, string> = {
    hold: "held",
    stop: "stopped",
    task: "task opened",
    suppress: "suppressed",
};

const PRIORITY_TONE: Record<string, string> = {
    now: "bg-rose-50 text-rose-700",
    today: "bg-amber-50 text-amber-700",
    whenever: "bg-sky-50 text-sky-700",
    ignore: "bg-slate-100 text-slate-500",
};

function pct(v: number): string {
    return `${Math.round(v * 100)}%`;
}

// Confidence is the number this page exists to show, so it is coloured rather
// than merely printed: below the floor nothing was trusted, and that has to be
// visible at a glance across fifty rows.
function ConfidenceChip({ value, floorBreached }: { value: number; floorBreached: boolean }) {
    return (
        <span
            title={floorBreached ? "Below the confidence floor — nothing was acted on" : "Model confidence"}
            className={cn(
                "shrink-0 px-1.5 rounded font-mono text-[10.5px] tabular-nums",
                floorBreached ? "bg-rose-50 text-rose-700" : value >= 0.9 ? "bg-emerald-50 text-emerald-700" : "bg-slate-100 text-slate-600",
            )}
        >
            {pct(value)}
        </span>
    );
}

// Mirrors ReturnDateFloor in internal/app/inboxtag/policy.go.
const RETURN_DATE_FLOOR = 0.4;

function returnDateNoul(r: InboxTagRow): number | null {
    const a = r.answers?.return_date as { noul?: unknown } | undefined;
    return typeof a?.noul === "number" ? a.noul : null;
}

// The away message's return date and whether the model read it as the day the
// sender is back. Below the floor the hold used the workspace fallback instead.
function ReturnDateChip({ r }: { r: InboxTagRow }) {
    const noul = returnDateNoul(r);
    if (!r.return_date || noul === null) return null;
    const doubted = noul < RETURN_DATE_FLOOR;
    const day = new Date(`${r.return_date}T00:00:00Z`).toLocaleDateString(undefined, {
        day: "numeric",
        month: "short",
        timeZone: "UTC",
    });
    return (
        <span
            title={
                doubted
                    ? "The model read this date as not the return, so the hold used the fallback"
                    : "Return date read from the away message, confirmed by the model"
            }
            className={cn(
                "px-1.5 rounded text-[10.5px] tabular-nums",
                doubted ? "bg-rose-50 text-rose-700 line-through" : "bg-emerald-50 text-emerald-700",
            )}
        >
            back {day} · {pct(noul)}
        </span>
    );
}

function Row({ r }: { r: InboxTagRow }) {
    return (
        <div className="px-5 py-3 flex items-start gap-3 hover:bg-slate-50 transition-colors">
            <span
                title={`Relevance ${r.relevance} of 100`}
                className="shrink-0 w-9 text-right font-mono text-[12.5px] tabular-nums text-slate-900"
            >
                {r.relevance}
            </span>
            <span
                className={cn(
                    "shrink-0 px-1.5 rounded text-[10px] font-semibold uppercase tracking-wide",
                    PRIORITY_TONE[r.priority] ?? PRIORITY_TONE.ignore,
                )}
            >
                {r.priority || "—"}
            </span>

            <div className="min-w-0 flex-1">
                <div className="flex items-center gap-1.5 flex-wrap">
                    {r.labels.length === 0 ? (
                        <span className="text-[11.5px] text-slate-400">no labels</span>
                    ) : (
                        r.labels.map((l) => (
                            <TagMeaningTooltip key={l} title={l}>
                                <span className="px-1.5 rounded bg-slate-100 text-slate-700 text-[11px] cursor-help">
                                    {l}
                                </span>
                            </TagMeaningTooltip>
                        ))
                    )}
                </div>
                <div className="mt-1 flex items-center gap-2 text-[11px] text-slate-400 flex-wrap">
                    <ReturnDateChip r={r} />
                    {(r.actions ?? []).map((a) => (
                        <span
                            key={a}
                            title="What this verdict was allowed to do"
                            className={cn(
                                "px-1.5 rounded text-[10px] font-semibold uppercase tracking-wide",
                                a === "suppress" ? "bg-rose-50 text-rose-700" : "bg-sky-50 text-sky-700",
                            )}
                        >
                            {ACTION_LABEL[a] ?? a}
                        </span>
                    ))}
                    <span className="font-mono truncate max-w-[22ch]" title={r.thread_id}>
                        {r.thread_id || "—"}
                    </span>
                    <span>·</span>
                    <span title="Where the verdict came from">
                        {r.kind_source === "header" ? "decided offline" : `model ${r.model}`}
                    </span>
                    {r.input_tokens > 0 && (
                        <>
                            <span>·</span>
                            <span title="Input tokens; output is not billed">{r.input_tokens} tok</span>
                        </>
                    )}
                </div>
            </div>

            <div className="shrink-0 flex items-center gap-2">
                <span className="text-[11.5px] text-slate-600">{r.kind || "—"}</span>
                <ConfidenceChip value={r.kind_confidence} floorBreached={r.review_reason === "kind"} />
                {r.intent && (
                    <>
                        <span className="text-[11.5px] text-slate-600">{r.intent}</span>
                        <ConfidenceChip value={r.intent_confidence} floorBreached={r.review_reason === "intent"} />
                    </>
                )}
            </div>
        </div>
    );
}

export default function InboxTaggingPage() {
    const canView = usePermission("VIEW_ANALYTICS");
    const [needsReviewOnly, setNeedsReviewOnly] = useBrowseState("settings.inbox-tagging.needs-review", false, browseFlagSchema);
    const q = useInboxTagReview(needsReviewOnly);
    const d = q.data?.pages[0];

    if (!canView) {
        return <NoAccess feature="automatic inbox tagging" permissionLabel="View analytics" />;
    }

    const rows = q.data?.pages.flatMap((page) => page.data) ?? [];

    return (
        <Page>
            <PageTopbar
                eyebrow="Automatic inbox tagging"
                subtitle="What the classifier decided, how sure it was, and what it did. Which actions run is set under Sending."
            />

            {!d?.enabled && !q.isPending && !q.isError && (
                <div className="mx-5 mt-4 px-3 py-2.5 rounded-md border border-amber-200 bg-amber-50 flex items-start gap-2">
                    <InfoIcon className="w-3.5 h-3.5 text-amber-600 shrink-0 mt-0.5" />
                    <p className="text-[11.5px] text-amber-800 leading-relaxed">
                        Classification is off on this instance. It needs a <code className="font-mono">TYPESAFE_API_KEY</code> and{" "}
                        <code className="font-mono">INBOX_TAGGING_ENABLED=true</code>. It sends message content to the
                        configured classifier, so it stays off until an operator turns it on deliberately. Timestamp-only
                        follow-up labels continue to run locally.
                    </p>
                </div>
            )}

            <StatStrip cols={4}>
                <Stat label="Classified" value={q.isPending ? "—" : (d?.summary.total ?? 0).toLocaleString()} sub="messages" accent={(d?.summary.total ?? 0) > 0} />
                <Stat label="Needs review" value={q.isPending ? "—" : (d?.summary.needs_review ?? 0).toLocaleString()} sub="below the confidence floor" />
                <Stat label="Decided offline" value={q.isPending ? "—" : (d?.summary.from_offline ?? 0).toLocaleString()} sub="kind read without the model" />
                <Stat
                    label="Actions taken"
                    value={q.isPending ? "—" : (d?.summary.acted ?? 0).toLocaleString()}
                    sub="held, stopped, task opened or suppressed"
                    accent={(d?.summary.acted ?? 0) > 0}
                    last
                />
            </StatStrip>

            <SectionBar label="Recent decisions" count={rows.length}>
                <button
                    type="button"
                    onClick={() => setNeedsReviewOnly((v) => !v)}
                    className={cn(
                        "h-6 px-2 rounded-md inline-flex items-center gap-1.5 text-[11px] transition-colors",
                        needsReviewOnly
                            ? "bg-sky-50 text-sky-700"
                            : "text-slate-500 hover:text-slate-900 hover:bg-slate-100",
                    )}
                >
                    {needsReviewOnly ? <CheckIcon className="w-3 h-3" /> : <FilterIcon className="w-3 h-3" />}
                    Needs review only
                </button>
            </SectionBar>

            <PageBody>
                {q.isPending ? (
                    <div className="divide-y divide-slate-200/60">
                        {Array.from({ length: 5 }).map((_, i) => (
                            <div key={i} className="h-14 px-5 flex items-center gap-3">
                                <div className="h-3 w-8 bg-slate-100 rounded animate-pulse" />
                                <div className="h-3 w-64 bg-slate-100 rounded animate-pulse" />
                            </div>
                        ))}
                    </div>
                ) : q.isError ? (
                    <EmptyBlock title="Couldn't load inbox tagging" body="Try again in a moment." />
                ) : rows.length === 0 ? (
                    <EmptyBlock
                        title={needsReviewOnly ? "Nothing needs review" : "Nothing classified yet"}
                        body={
                            d?.enabled
                                ? "Inbound mail is labelled as it arrives. Our own sends are never classified, and common automated replies are decided offline without a model call."
                                : "Turn the feature on and inbound mail starts being labelled as it arrives."
                        }
                    />
                ) : (
                    <>
                        <div className="divide-y divide-slate-200/60">
                            {rows.map((r) => (
                                <Row key={r.id} r={r} />
                            ))}
                        </div>
                        {q.hasNextPage && (
                            <div className="px-5 py-3 border-t border-slate-200">
                                <button
                                    type="button"
                                    onClick={() => q.fetchNextPage()}
                                    disabled={q.isFetchingNextPage}
                                    className="h-7 px-3 rounded-md border border-slate-200 text-[11.5px] text-slate-700 hover:bg-slate-50 disabled:opacity-50"
                                >
                                    {q.isFetchingNextPage ? "Loading…" : "Load more"}
                                </button>
                            </div>
                        )}
                    </>
                )}
            </PageBody>

            <div className="px-5 py-3 flex items-center gap-1.5 text-[11px] text-slate-400 flex-wrap">
                <SparklesIcon className="w-3 h-3" />
                Every label is a workspace label, so the{" "}
                <Link to="/app/unibox/{-$scope}/{-$threadId}" params={{ scope: "all", threadId: undefined }} className="underline underline-offset-2 hover:text-slate-700">
                    inbox
                </Link>{" "}
                filters on them like any other. What a verdict may do is set under{" "}
                <Link to="/app/settings/sending" className="underline underline-offset-2 hover:text-slate-700">
                    Sending
                </Link>
                .
            </div>
        </Page>
    );
}

import React from "react";
import { Link } from "@tanstack/react-router";
import { AnimatePresence, motion } from "framer-motion";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowLeftIcon, CheckIcon, SparklesIcon, UndoIcon, UsersIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { useConfirm } from "@/hooks/context/confirm";
import { usePermission } from "@/hooks/usePermission";
import { MD_QUERY, useMediaQuery } from "@/hooks/useMediaQuery";
import {
    openingReviewStatus,
    writeLeadAIOnce,
    useLeadsBeingWritten,
    useApproveLeads,
    useCampaignReview,
    useReopenLeads,
    useReviewLeads,
    useSetReview,
} from "@/lib/api/hooks/app/campaigns/useCampaignReview";
import type { MissingField, ReviewStatus } from "@/lib/api/models/app/campaigns/Review";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { fmtUsd } from "@/lib/money";
import ReviewToolbar from "./ReviewToolbar";
import ReviewLeadList from "./ReviewLeadList";
import LeadEmailsPane from "./LeadEmailsPane";
import { chunked, useBulkAIWrite } from "./useBulkAIWrite";
import { ReviewSkeleton, RunningGrid } from "./reviewLook";
import { filterCount } from "./reviewStatus";
import { cn } from "@/lib/utils";

const DISCARD = "Discard your changes to this lead's email?";

function typing(target: EventTarget | null): boolean {
    const el = target as HTMLElement | null;
    if (!el) return false;
    return el.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(el.tagName) || !!el.closest("[role='dialog'],[role='alertdialog'],[data-floating]");
}

// The leads of a campaign and each one's emails, inside an email step's panel:
// stepId shows that step only and initialLead opens one lead first.
export default function CampaignReviewView({ campaignId, stepId, initialLead }: { campaignId: string; stepId?: string; initialLead?: string }) {
    const confirm = useConfirm();
    const canSend = usePermission("SEND_CAMPAIGNS");
    const canEdit = usePermission("MANAGE_CAMPAIGNS");
    const canAI = usePermission("USE_AI");
    const summary = useCampaignReview(campaignId);
    const reviewMode = summary.data?.review_leads ?? false;

    // A link can open one lead, shown whatever the list's filter.
    const linked = initialLead ?? null;
    const [status, setStatus] = React.useState<"all" | ReviewStatus | null>(linked ? "all" : null);
    const [missing, setMissing] = React.useState<MissingField | undefined>();
    const [search, setSearch] = React.useState("");
    const [q, setQ] = React.useState("");
    const [activeId, setActiveId] = React.useState<string | null>(linked);
    const [selected, setSelected] = React.useState<Set<string>>(new Set());
    const [mobilePane, setMobilePane] = React.useState(!!linked);
    // Bumped to drop the open lead's draft after a confirmed discard.
    const [paneEpoch, setPaneEpoch] = React.useState(0);
    const wide = useMediaQuery(MD_QUERY);
    const dirtyRef = React.useRef(false);
    const rootRef = React.useRef<HTMLDivElement | null>(null);

    // The list opens where the work is: waiting leads in review mode, otherwise leads not yet emailed.
    const effectiveStatus: "all" | ReviewStatus = status ?? (summary.data ? openingReviewStatus(summary.data) : "waiting");

    React.useEffect(() => {
        // Rows picked while the old results showed may be hidden by the new ones.
        const t = setTimeout(() => {
            setQ(search);
            setSelected(new Set());
        }, 250);
        return () => clearTimeout(t);
    }, [search]);

    const list = useReviewLeads(campaignId, { status: effectiveStatus, missing, q });
    const leads = list.leads;
    const approve = useApproveLeads(campaignId);
    const reopen = useReopenLeads(campaignId);
    const setReview = useSetReview(campaignId);
    const bulk = useBulkAIWrite(campaignId);
    const writingLeads = useLeadsBeingWritten(campaignId);

    // Keep a lead open beside the list once it loads; on a phone nothing opens until tapped.
    const index = leads.findIndex((l) => l.contact_id === activeId);
    const activeRef = React.useRef(activeId);
    activeRef.current = activeId;
    React.useEffect(() => {
        if (list.isLoading || !wide) return;
        if (activeId === null && leads.length > 0) setActiveId(leads[0].contact_id);
    }, [list.isLoading, leads, activeId, wide]);

    // The direction the pane slides when another lead opens.
    const [direction, setDirection] = React.useState(1);
    const guarded = (fn: (discarded: boolean) => void) => {
        if (dirtyRef.current) confirm.show(DISCARD, async () => {
            dirtyRef.current = false;
            fn(true);
        });
        else fn(false);
    };
    const open = (id: string) =>
        guarded(() => {
            const to = leads.findIndex((l) => l.contact_id === id);
            if (to >= 0 && index >= 0) setDirection(to >= index ? 1 : -1);
            setActiveId(id);
            setMobilePane(true);
        });
    const move = (delta: number) => {
        const next = leads[index + delta];
        if (!next) return;
        setDirection(delta > 0 ? 1 : -1);
        open(next.contact_id);
    };

    // The next leads' AI copy is written in the background while one is read,
    // so moving on shows it written. One at a time, each lead once.
    const qc = useQueryClient();
    const aiStep = summary.data?.ai.blocks ? summary.data.ai.step_id : undefined;
    const prewritten = React.useRef(new Set<string>());
    const canPrewrite = canAI && canEdit && !!aiStep && (summary.data?.ai.pending ?? 0) > 0 && !bulk.running;
    const upcoming = index >= 0 ? leads.slice(index + 1, index + 3).filter((l) => l.status !== "started").map((l) => l.contact_id) : [];
    const upcomingKey = upcoming.join(",");
    React.useEffect(() => {
        if (!canPrewrite || !aiStep || !upcomingKey) return;
        let stopped = false;
        void (async () => {
            for (const id of upcomingKey.split(",")) {
                const key = `${aiStep}:${id}`;
                if (stopped || prewritten.current.has(key)) continue;
                prewritten.current.add(key);
                try {
                    await writeLeadAIOnce(qc, campaignId, id, aiStep);
                } catch (err) {
                    // Out of balance or over the limit: stop reaching ahead; the open lead says why.
                    const code = (err as AppError).code;
                    if (code === "insufficient_credits" || code === "usage_cap_exceeded" || code === "ai_unavailable") return;
                    prewritten.current.delete(key);
                }
            }
        })();
        return () => {
            stopped = true;
        };
    }, [canPrewrite, aiStep, upcomingKey, qc, campaignId]);

    const approveOne = async () => {
        if (!activeId) return;
        const approved = activeId;
        const following = leads[index + 1]?.contact_id ?? leads[index - 1]?.contact_id ?? null;
        try {
            const res = await approve.mutateAsync({ contact_ids: [approved] });
            if (res.released > 0) toast.success("Approved. Their first email goes out on schedule.");
            else toast("This lead is no longer waiting for review.");
            // Move on only if the reviewer is still on the lead they approved, with no edit started meanwhile.
            if (effectiveStatus === "waiting" && following && activeRef.current === approved && !dirtyRef.current) {
                setDirection(1);
                setActiveId(following);
            }
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    };
    const reopenOne = async () => {
        if (!activeId) return;
        try {
            await reopen.mutateAsync([activeId]);
            toast.success("Back to review");
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    };

    // Keyboard: J/K move through leads, A approves the open lead; only with focus in Review or nowhere.
    React.useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.metaKey || e.ctrlKey || e.altKey || typing(e.target)) return;
            const target = e.target as Node | null;
            if (target !== document.body && !rootRef.current?.contains(target)) return;
            if (e.key === "j") move(1);
            else if (e.key === "k") move(-1);
            else if (e.key === "a" && canSend && !dirtyRef.current && leads[index]?.status === "waiting") void approveOne();
            else return;
            e.preventDefault();
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    });

    const pickStatus = (s: "all" | ReviewStatus) =>
        guarded(() => {
            setStatus(s);
            setSelected(new Set());
            setActiveId(null);
        });
    const pickMissing = (f: MissingField) =>
        guarded(() => {
            setMissing(f);
            setStatus("all");
            setSelected(new Set());
            setActiveId(null);
        });

    const toggleReview = (enabled: boolean) => {
        const waiting = summary.data?.counts.waiting ?? 0;
        const run = async () => {
            try {
                const res = await setReview.mutateAsync(enabled);
                toast.success(
                    enabled
                        ? `Review is on. ${res.held.toLocaleString()} lead${res.held === 1 ? " waits" : "s wait"} for approval.`
                        : `Review is off. ${res.released.toLocaleString()} lead${res.released === 1 ? "" : "s"} approved.`,
                );
                setStatus(null);
                // An open edit stays open; the list still moves to the new opening filter.
                if (!dirtyRef.current) setActiveId(null);
            } catch (err) {
                toast.error(buildError(err as AppError));
            }
        };
        if (!enabled) {
            if (!canSend) {
                toast.error("Turning review off approves every waiting lead, which needs the Send campaigns permission.");
                return;
            }
            // Always asked: a lead can start waiting after the count above was read.
            confirm.show(
                waiting > 0
                    ? `Turn review off? The ${waiting.toLocaleString()} waiting lead${waiting === 1 ? " is" : "s are"} approved and start on schedule.`
                    : "Turn review off? Any lead waiting for approval is approved and starts on schedule.",
                run,
            );
        } else {
            void run();
        }
    };

    // onlySelected writes the selection; otherwise every lead still missing copy.
    const writeAll = (onlySelected: boolean) => {
        const s = summary.data;
        if (!s) return;
        const ids = onlySelected ? [...selected] : [];
        const scope = onlySelected ? `${ids.length.toLocaleString()} selected lead${ids.length === 1 ? "" : "s"}` : `${s.ai.pending.toLocaleString()} leads`;
        const cost = onlySelected && s.ai.pending > 0 ? (s.ai.estimated_cost_usd / s.ai.pending) * ids.length : s.ai.estimated_cost_usd;
        confirm.show(
            `Write the first email's AI copy now for ${scope}? It costs about ${fmtUsd(cost)} from the balance, the same as writing it at send time, and is not charged again when it sends.`,
            async () => {
                void bulk.run(onlySelected ? { contact_ids: ids } : {});
            },
        );
    };

    const bulkApprove = () => {
        const ids = [...selected];
        confirm.show(`Approve ${ids.length.toLocaleString()} lead${ids.length === 1 ? "" : "s"}? Their first emails go out on schedule.`, async () => {
            try {
                let released = 0;
                for (const part of chunked(ids)) released += (await approve.mutateAsync({ contact_ids: part })).released;
                toast.success(`${released.toLocaleString()} approved`);
                setSelected(new Set());
            } catch (err) {
                toast.error(buildError(err as AppError));
            }
        });
    };
    const approveEverything = () => {
        const n = summary.data?.counts.waiting ?? 0;
        confirm.show(`Approve all ${n.toLocaleString()} waiting leads? Their first emails go out on schedule.`, async () => {
            try {
                const res = await approve.mutateAsync({ all: true });
                toast.success(`${res.released.toLocaleString()} approved`);
            } catch (err) {
                toast.error(buildError(err as AppError));
            }
        });
    };
    const bulkReopen = async () => {
        try {
            let held = 0;
            for (const part of chunked([...selected])) held += (await reopen.mutateAsync(part)).held;
            toast.success(`${held.toLocaleString()} sent back to review`);
            setSelected(new Set());
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    };

    if (summary.isLoading) return <ReviewSkeleton />;
    if (summary.isError || !summary.data) {
        return (
            <div className="flex-1 flex flex-col items-center justify-center gap-2 p-10 text-[12.5px] text-slate-500">
                Could not load the review.
                <button type="button" onClick={() => void summary.refetch()} className="text-sky-700 font-medium">
                    Try again
                </button>
            </div>
        );
    }

    if (summary.data.counts.total === 0) {
        return (
            <div className="flex-1 flex flex-col items-center justify-center gap-2 p-10 text-center">
                <UsersIcon className="w-5 h-5 text-slate-400" />
                <p className="text-[12.5px] text-slate-600">No leads yet. Add leads, then read this email as each one gets it here before it goes out.</p>
                <Link to="/app/campaigns/$id/leads" params={{ id: campaignId }} className="text-[12.5px] text-sky-700 hover:text-sky-800 font-medium">
                    Go to Leads
                </Link>
            </div>
        );
    }

    const waiting = summary.data.counts.waiting;
    // Campaign counts do not cover a search or a missing-field filter.
    const filtered = !!q || !!missing;
    const shownTotal = filtered ? (list.hasNextPage ? undefined : leads.length) : filterCount(summary.data.counts, effectiveStatus);
    const backToList = (
        <button
            type="button"
            onClick={() => setMobilePane(false)}
            className="md:hidden h-7 px-1.5 rounded text-[12px] text-slate-600 hover:bg-slate-100 inline-flex items-center gap-1"
        >
            <ArrowLeftIcon className="w-3.5 h-3.5" />
            Leads
        </button>
    );
    const emptyText =
        effectiveStatus === "waiting"
            ? "Nobody is waiting for review."
            : missing
              ? "No leads match."
              : q
                ? "No leads match your search."
                : "No leads here.";

    return (
        <div ref={rootRef} className="h-full min-h-0 bg-white overflow-hidden flex flex-col">
            <ReviewToolbar
                campaignId={campaignId}
                summary={summary.data}
                canToggle={canEdit}
                toggling={setReview.isPending}
                onToggleReview={toggleReview}
                onMissing={pickMissing}
                canApproveAll={reviewMode && waiting > 0 && canSend}
                onApproveAll={approveEverything}
                canWriteAI={canAI && canEdit}
                aiProgress={bulk.progress}
                onWriteAI={() => writeAll(false)}
                onStopAI={bulk.stop}
            />
            <div className="flex-1 min-h-0 flex">
                <div className={cn("w-full md:w-[270px] lg:w-[290px] shrink-0 md:border-r border-slate-200 min-h-0", mobilePane ? "hidden md:block" : "block")}>
                    <ReviewLeadList
                        writing={writingLeads}
                        leads={leads}
                        loading={list.isLoading}
                        failed={list.isError}
                        onRetry={() => void list.refetch()}
                        filtered={filtered}
                        hasMore={!!list.hasNextPage}
                        loadingMore={list.isFetchingNextPage}
                        onLoadMore={() => void list.fetchNextPage()}
                        activeId={activeId}
                        onOpen={open}
                        selected={selected}
                        onToggle={(id) =>
                            setSelected((cur) => {
                                const next = new Set(cur);
                                if (next.has(id)) next.delete(id);
                                else next.add(id);
                                return next;
                            })
                        }
                        onToggleAll={(all) => setSelected(all ? new Set(leads.map((l) => l.contact_id)) : new Set())}
                        search={search}
                        onSearch={(v) => {
                            setSearch(v);
                            // Bulk actions act only on leads the reviewer can see.
                            setSelected(new Set());
                        }}
                        status={effectiveStatus}
                        onStatus={pickStatus}
                        counts={summary.data.counts}
                        reviewMode={reviewMode}
                        missing={missing}
                        onClearMissing={() =>
                            guarded(() => {
                                setMissing(undefined);
                                setActiveId(null);
                            })
                        }
                        emptyText={emptyText}
                    />
                </div>
                <div className={cn("relative flex-1 min-w-0 min-h-0 overflow-hidden", mobilePane ? "block" : "hidden md:block")}>
                    <AnimatePresence initial={false} custom={direction} mode="popLayout">
                    {activeId ? (
                        <motion.div
                            key={activeId}
                            custom={direction}
                            variants={{
                                enter: (d: number) => ({ opacity: 0, y: d * 14 }),
                                center: { opacity: 1, y: 0 },
                                exit: (d: number) => ({ opacity: 0, y: d * -10 }),
                            }}
                            initial="enter"
                            animate="center"
                            exit="exit"
                            transition={{ duration: 0.2, ease: "easeOut" }}
                            className="absolute inset-0"
                        >
                        <LeadEmailsPane
                            key={`${activeId}:${paneEpoch}`}
                            campaignId={campaignId}
                            contactId={activeId}
                            lockedStep={stepId}
                            reviewMode={reviewMode}
                            canSend={canSend}
                            canEdit={canEdit}
                            canAI={canAI}
                            onApprove={approveOne}
                            onReopen={reopenOne}
                            position={index >= 0 ? index + 1 : undefined}
                            total={shownTotal}
                            onPrev={index > 0 ? () => move(-1) : undefined}
                            onNext={index >= 0 && index < leads.length - 1 ? () => move(1) : undefined}
                            onBack={() =>
                                guarded((discarded) => {
                                    if (discarded) setPaneEpoch((e) => e + 1);
                                    setMobilePane(false);
                                })
                            }
                            onDirtyChange={(d) => {
                                dirtyRef.current = d;
                            }}
                        />
                        </motion.div>
                    ) : (
                        <motion.div key="none" initial={{ opacity: 0 }} animate={{ opacity: 1 }} className="h-full flex flex-col items-center justify-center gap-2 text-[12.5px] text-slate-500 p-6 text-center">
                            {list.isLoading ? "" : "Pick a lead to read their emails as they will be sent."}
                            {backToList}
                        </motion.div>
                    )}
                    </AnimatePresence>
                </div>
            </div>

            {selected.size > 0 && (
                <div className="fixed bottom-4 left-1/2 -translate-x-1/2 z-30 flex items-center max-w-[calc(100vw-16px)] flex-wrap justify-center md:max-w-none md:flex-nowrap gap-1.5 rounded-md border border-slate-200 bg-white shadow-[0_6px_20px_-4px_rgba(15,23,42,0.12),0_2px_4px_rgba(15,23,42,0.04)] px-2 py-1.5">
                    <div className="inline-flex items-center gap-1.5 px-2 h-7 rounded bg-sky-50 text-sky-700 text-[12px] font-medium">
                        <CheckIcon className="w-3 h-3" />
                        <span>{selected.size.toLocaleString()} selected</span>
                    </div>
                    {canSend && (
                        <button
                            type="button"
                            onClick={bulkApprove}
                            disabled={approve.isPending}
                            className="h-7 px-2.5 rounded text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 font-medium inline-flex items-center gap-1.5 disabled:opacity-60"
                        >
                            {approve.isPending ? <RunningGrid className="text-sky-600" /> : <CheckIcon className="w-3 h-3" />}
                            Approve
                        </button>
                    )}
                    {canAI && canEdit && summary.data.ai.blocks > 0 && (
                        <button
                            type="button"
                            onClick={() => writeAll(true)}
                            disabled={bulk.running}
                            aria-label={bulk.running ? "Writing AI copy" : "Write AI copy"}
                            className="h-7 px-2.5 rounded text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 font-medium inline-flex items-center gap-1.5 disabled:opacity-60"
                        >
                            {bulk.running ? <RunningGrid className="text-sky-600" /> : <SparklesIcon className="w-3 h-3" />}
                            <span className="hidden sm:inline">{bulk.running ? "Writing AI copy" : "Write AI copy"}</span>
                        </button>
                    )}
                    {reviewMode && canEdit && (
                        <button
                            type="button"
                            onClick={() => void bulkReopen()}
                            disabled={reopen.isPending}
                            aria-label="Back to review"
                            className="h-7 px-2.5 rounded text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 font-medium inline-flex items-center gap-1.5 disabled:opacity-60"
                        >
                            {reopen.isPending ? <RunningGrid className="text-sky-600" /> : <UndoIcon className="w-3 h-3" />}
                            <span className="hidden sm:inline">Back to review</span>
                        </button>
                    )}
                    <button
                        type="button"
                        onClick={() => setSelected(new Set())}
                        aria-label="Clear selection"
                        className="size-7 rounded text-slate-500 hover:text-slate-800 hover:bg-slate-100 inline-flex items-center justify-center"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                </div>
            )}
        </div>
    );
}

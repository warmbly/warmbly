// Running campaign copy: the opening steps and active A/B arms that warmup
// carries while a campaign runs, and where each landed.
import React from "react";
import { Link } from "@tanstack/react-router";
import { AnimatePresence, motion } from "framer-motion";
import { ChevronRightIcon, TriangleAlertIcon } from "lucide-react";
import { EmptyBlock, SectionBar } from "@/components/layout/Page";
import { useWarmupCampaignCopy } from "@/lib/api/hooks/app/warmup/useWarmupCopy";
import type { WarmupCampaignCopyStep } from "@/lib/api/models/app/warmup/WarmupCopy";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import { campaignStatusBucket, campaignStatusLabel } from "@/components/app/campaigns/status";
import { CopyPlacementSummary, GroupBreakdown } from "./copyParts";

// Pill colours by status bucket; the words come from the shared campaign status labels.
const STATUS_PILL_CLASS: Record<ReturnType<typeof campaignStatusBucket>, string> = {
    active: "bg-emerald-50 text-emerald-700 border-emerald-200",
    paused: "bg-amber-50 text-amber-700 border-amber-200",
    completed: "bg-slate-100 text-slate-600 border-slate-200",
    draft: "bg-slate-100 text-slate-600 border-slate-200",
};

interface CampaignGroup {
    id: string;
    name: string;
    status: string;
    steps: WarmupCampaignCopyStep[];
}

// Keeps the server's order: active campaigns first.
function groupByCampaign(steps: WarmupCampaignCopyStep[]): CampaignGroup[] {
    const groups = new Map<string, CampaignGroup>();
    for (const s of steps) {
        let g = groups.get(s.campaign_id);
        if (!g) {
            g = { id: s.campaign_id, name: s.campaign_name, status: s.campaign_status, steps: [] };
            groups.set(s.campaign_id, g);
        }
        g.steps.push(s);
    }
    return [...groups.values()];
}

function armKey(s: WarmupCampaignCopyStep): string {
    return `${s.sequence_id}:${s.variant_id ?? "base"}`;
}

export default function CampaignCopySection({ days, enabled }: { days: number; enabled: boolean }) {
    const list = useWarmupCampaignCopy(days);
    const groups = React.useMemo(() => groupByCampaign(list.steps), [list.steps]);
    const [open, setOpen] = React.useState<string | null>(null);
    const eligible = list.steps.filter((s) => s.eligible).length;

    return (
        <section>
            <SectionBar label="Running campaigns" count={list.data ? list.steps.length : undefined}>
                {list.data && list.steps.length > 0 && (
                    <span className="text-[11px] text-slate-400">
                        {eligible.toLocaleString()} of {list.steps.length.toLocaleString()} can be warmed
                    </span>
                )}
            </SectionBar>

            {!enabled && list.steps.length > 0 && (
                <div className="px-5 py-2 border-b border-slate-200/60 text-[11.5px] text-amber-700 bg-amber-50/50">
                    Warming up running campaigns is off, so warmup does not send these steps right now.
                </div>
            )}

            {list.isLoading ? (
                <div className="divide-y divide-slate-200/60">
                    {[0, 1, 2].map((i) => (
                        <div key={i} className="h-12 px-5 flex items-center gap-4">
                            <div className="h-3 w-40 rounded bg-slate-100 animate-pulse" />
                            <div className="h-3 flex-1 rounded bg-slate-50 animate-pulse" />
                        </div>
                    ))}
                </div>
            ) : list.isError ? (
                <EmptyBlock title="Campaign copy could not be loaded" body={buildError(list.error as unknown as AppError)} />
            ) : groups.length === 0 ? (
                <EmptyBlock
                    title="No running campaigns"
                    body="While a campaign runs, its opening steps appear here with where their warmup sends landed."
                />
            ) : (
                <div>
                    {groups.map((g) => (
                        <div key={g.id} className="border-b border-slate-200/60">
                            <div className="h-9 px-5 flex items-center gap-2 bg-slate-50/60">
                                <Link
                                    to="/app/campaigns/$id"
                                    params={{ id: g.id }}
                                    className="text-[12.5px] font-medium text-slate-900 hover:text-sky-700 truncate"
                                >
                                    {g.name || "Untitled campaign"}
                                </Link>
                                <StatusPill status={g.status} />
                            </div>
                            <div className="divide-y divide-slate-200/60">
                                {g.steps.map((s) => {
                                    const key = armKey(s);
                                    return (
                                        <StepRow
                                            key={key}
                                            step={s}
                                            open={open === key}
                                            onToggle={() => setOpen((cur) => (cur === key ? null : key))}
                                        />
                                    );
                                })}
                            </div>
                        </div>
                    ))}
                </div>
            )}
        </section>
    );
}

function StatusPill({ status }: { status: string }) {
    return (
        <span className={cn("shrink-0 inline-flex items-center h-5 px-1.5 rounded-md border text-[10.5px] font-medium first-letter:uppercase", STATUS_PILL_CLASS[campaignStatusBucket(status)])}>
            {campaignStatusLabel(status)}
        </span>
    );
}

function StepRow({ step, open, onToggle }: { step: WarmupCampaignCopyStep; open: boolean; onToggle: () => void }) {
    const panelId = `copy-step-${step.sequence_id}-${step.variant_id ?? "base"}`;
    return (
        <div>
            <button
                type="button"
                onClick={onToggle}
                aria-expanded={open}
                aria-controls={panelId}
                className="w-full min-h-12 px-5 py-2 flex flex-col md:flex-row md:items-center gap-1.5 md:gap-4 text-left hover:bg-slate-50/80 transition-colors outline-none focus-visible:bg-slate-50"
            >
                <div className="flex items-start gap-2 min-w-0 md:flex-1">
                    <ChevronRightIcon
                        className={cn("w-3.5 h-3.5 mt-0.5 text-slate-400 shrink-0 transition-transform", open && "rotate-90")}
                    />
                    <div className="min-w-0">
                        <div className="flex items-center gap-1.5 min-w-0">
                            <span className="text-[12.5px] text-slate-900 truncate">{step.step_name || "Opening step"}</span>
                            {step.variant_id && (
                                <span className="shrink-0 h-4 px-1.5 rounded bg-sky-50 text-sky-700 text-[10px] inline-flex items-center">
                                    {step.variant_name || "Variant"}
                                </span>
                            )}
                        </div>
                        <div className="text-[11.5px] text-slate-500 truncate">{step.subject || "(no subject)"}</div>
                    </div>
                </div>
                <div className="pl-5.5 md:pl-0 md:w-[46%] min-w-0">
                    {step.eligible ? (
                        <CopyPlacementSummary placement={step.placement} />
                    ) : (
                        <span className="flex items-start gap-1.5 text-[11.5px] text-amber-700 leading-snug">
                            <TriangleAlertIcon className="w-3.5 h-3.5 shrink-0 mt-px" />
                            {step.ineligible_reason || "Warmup cannot send this step as the campaign sends it."}
                        </span>
                    )}
                </div>
            </button>
            <AnimatePresence initial={false}>
                {open && (
                    <motion.div
                        id={panelId}
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.18 }}
                        className="overflow-hidden"
                    >
                        <div className="pr-5 pl-10.5 pb-3">
                            <GroupBreakdown groups={step.by_group} />
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

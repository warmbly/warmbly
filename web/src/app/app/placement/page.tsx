// Inbox placement tests: the workspace's tests, the seed panels it can test
// against, and its own seed inboxes. Live through PLACEMENT_TEST_UPDATED and
// the audit spine; nothing here polls.

import React from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { motion } from "framer-motion";
import { InboxIcon, Loader2Icon, ListIcon, PlusIcon, XIcon } from "lucide-react";
import { EmptyBlock, Page, PageTopbar, SectionBar, TopbarAction } from "@/components/layout/Page";
import ScrollStrip from "@/components/ui/scroll-strip";
import { usePermission, showPermissionDenied } from "@/hooks/usePermission";
import useCampaign from "@/lib/api/hooks/app/campaigns/useCampaign";
import { usePlacementOverview, usePlacementTests } from "@/lib/api/hooks/app/placement/usePlacement";
import { PANEL_LABEL, type PlacementTest } from "@/lib/api/models/app/placement/Placement";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import NewPlacementTestDialog from "@/components/app/placement/tests/NewPlacementTestDialog";
import SeedInboxes from "@/components/app/placement/tests/SeedInboxes";
import {
    PanelStrip,
    PlacementBar,
    PlacementCaveat,
    PlacementLegend,
    StatusChip,
    TrackingBadge,
} from "@/components/app/placement/tests/PlacementParts";
import { ORIGIN_LABEL, fmtDate, fmtRate, rateTone, usageLabel } from "@/components/app/placement/tests/placementTests";
import { cn } from "@/lib/utils";

type Tab = "tests" | "seeds";

const TABS: { key: Tab; label: string; icon: typeof ListIcon }[] = [
    { key: "tests", label: "Tests", icon: ListIcon },
    { key: "seeds", label: "Seed inboxes", icon: InboxIcon },
];

export default function PlacementPage() {
    const [params, setParams] = useSearchParams();
    const tab: Tab = params.get("tab") === "seeds" ? "seeds" : "tests";
    const campaignId = params.get("campaign_id");
    const canStart = usePermission("SEND_CAMPAIGNS");
    const overview = usePlacementOverview();
    const [dialogOpen, setDialogOpen] = React.useState(false);

    const setTab = (t: Tab) => {
        const next = new URLSearchParams(params);
        if (t === "tests") next.delete("tab");
        else next.set("tab", t);
        setParams(next, { replace: true });
    };

    const openNew = () => {
        if (!canStart) {
            showPermissionDenied("SEND_CAMPAIGNS");
            return;
        }
        setDialogOpen(true);
    };

    const usage = overview.data?.usage;
    const exhausted = usage?.limit != null && usage.used >= usage.limit;

    return (
        <Page>
            <PageTopbar
                eyebrow="Placement tests"
                subtitle={overview.data ? usageLabel(overview.data) : undefined}
            >
                {exhausted && (
                    <span className="hidden md:inline text-[11px] text-slate-400">Your own seed inboxes are never counted.</span>
                )}
                <TopbarAction icon={<PlusIcon className="w-3.5 h-3.5" />} onClick={openNew}>
                    New test
                </TopbarAction>
            </PageTopbar>

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
                            {t.key === "seeds" && overview.data && (
                                <span className="font-mono text-[10.5px] text-slate-400 tabular-nums">{overview.data.workspace_seeds}</span>
                            )}
                            {active && (
                                <motion.span
                                    layoutId="placement-tab-underline"
                                    className="absolute left-1.5 right-1.5 bottom-0 h-0.5 rounded-full bg-sky-600"
                                    transition={{ type: "spring", duration: 0.3, bounce: 0.15 }}
                                />
                            )}
                        </button>
                    );
                })}
            </ScrollStrip>

            {tab === "seeds" ? (
                <SeedInboxes />
            ) : (
                <>
                    {overview.data && <PanelStrip overview={overview.data} />}
                    <TestsTable
                        campaignId={campaignId}
                        onClearCampaign={() => {
                            const next = new URLSearchParams(params);
                            next.delete("campaign_id");
                            setParams(next, { replace: true });
                        }}
                        onNew={openNew}
                    />
                </>
            )}

            <NewPlacementTestDialog
                open={dialogOpen}
                onClose={() => setDialogOpen(false)}
                prefill={campaignId ? { campaignId } : undefined}
            />
        </Page>
    );
}

function TestsTable({
    campaignId,
    onClearCampaign,
    onNew,
}: {
    campaignId: string | null;
    onClearCampaign: () => void;
    onNew: () => void;
}) {
    const navigate = useNavigate();
    const list = usePlacementTests(campaignId);
    const campaign = useCampaign(campaignId ?? "");

    return (
        <section className="flex-1 min-h-0 flex flex-col">
            <SectionBar label="Tests" count={list.total ?? undefined}>
                {campaignId && (
                    <span className="inline-flex items-center gap-1 h-6 pl-2 pr-1 rounded-md bg-sky-50 text-sky-700 text-[11.5px] max-w-[260px]">
                        <span className="truncate">Campaign: {campaign.data?.name ?? "…"}</span>
                        <button
                            type="button"
                            onClick={onClearCampaign}
                            aria-label="Show every test"
                            className="size-4 rounded inline-flex items-center justify-center hover:bg-sky-100"
                        >
                            <XIcon className="w-3 h-3" />
                        </button>
                    </span>
                )}
                <PlacementLegend className="hidden lg:flex" />
            </SectionBar>

            {list.isLoading ? (
                <div className="divide-y divide-slate-200/60">
                    {Array.from({ length: 5 }).map((_, i) => (
                        <div key={i} className="h-12 px-5 flex items-center gap-4">
                            <div className="h-3 w-40 rounded bg-slate-100 animate-pulse" />
                            <div className="h-3 flex-1 rounded bg-slate-50 animate-pulse" />
                        </div>
                    ))}
                </div>
            ) : list.isError ? (
                <EmptyBlock title="Tests could not be loaded" body={buildError(list.error as unknown as AppError)} />
            ) : list.tests.length === 0 ? (
                <EmptyBlock
                    title={campaignId ? "No tests for this campaign yet" : "No placement tests yet"}
                    body="Send a campaign step or your own copy to a panel of seed inboxes and see whether it reaches the inbox, a Gmail tab or spam."
                    cta={
                        <button
                            type="button"
                            onClick={onNew}
                            className="h-7 px-2.5 rounded-md inline-flex items-center gap-1.5 text-[12px] font-medium bg-sky-600 hover:bg-sky-700 text-white transition-colors"
                        >
                            <PlusIcon className="w-3.5 h-3.5" />
                            New test
                        </button>
                    }
                />
            ) : (
                <div className="overflow-x-auto">
                    <table className="w-full text-left">
                        <thead>
                            <tr className="h-8 border-b border-slate-200/60 text-[10px] uppercase tracking-[0.14em] text-slate-400">
                                <th className="px-5 font-medium">Sender and subject</th>
                                <th className="px-3 font-medium hidden lg:table-cell">Panel</th>
                                <th className="px-3 font-medium hidden md:table-cell">Tracking</th>
                                <th className="px-3 font-medium">Status</th>
                                <th className="px-3 font-medium hidden sm:table-cell w-[18%]">Where it landed</th>
                                <th className="px-3 font-medium text-right">Inbox</th>
                                <th className="px-5 font-medium text-right hidden md:table-cell">Started</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-slate-200/60">
                            {list.tests.map((t) => (
                                <TestRow key={t.id} test={t} onOpen={() => navigate(`/app/placement/${t.id}`)} />
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

            {list.tests.length > 0 && <PlacementCaveat className="mx-5 my-4" />}
        </section>
    );
}

function TestRow({ test, onOpen }: { test: PlacementTest; onOpen: () => void }) {
    const s = test.summary;
    return (
        <tr
            onClick={onOpen}
            onKeyDown={(e) => {
                if (e.key === "Enter") onOpen();
            }}
            tabIndex={0}
            className="h-12 cursor-pointer hover:bg-slate-50/80 transition-colors outline-none focus-visible:bg-slate-50"
        >
            <td className="px-5 py-2 max-w-0 w-[34%]">
                <div className="flex items-center gap-1.5 min-w-0">
                    <span className="text-[12.5px] font-medium text-slate-900 truncate">{test.sender_email || "Deleted mailbox"}</span>
                    {test.origin !== "manual" && (
                        <span className="shrink-0 h-4 px-1.5 rounded bg-slate-100 text-[10px] text-slate-500 inline-flex items-center">
                            {ORIGIN_LABEL[test.origin] ?? test.origin}
                        </span>
                    )}
                </div>
                <div className="text-[11.5px] text-slate-500 truncate">{test.subject || "(no subject)"}</div>
            </td>
            <td className="px-3 py-2 hidden lg:table-cell text-[11.5px] text-slate-600 whitespace-nowrap">{PANEL_LABEL[test.panel] ?? test.panel}</td>
            <td className="px-3 py-2 hidden md:table-cell">
                <TrackingBadge test={test} />
            </td>
            <td className="px-3 py-2">
                <StatusChip status={test.status} counts={s} />
            </td>
            <td className="px-3 py-2 hidden sm:table-cell">
                <PlacementBar counts={s} />
            </td>
            <td className={cn("px-3 py-2 text-right font-mono text-[12px] tabular-nums", rateTone(s.inbox_rate))}>
                {fmtRate(s.inbox_rate)}
            </td>
            <td className="px-5 py-2 text-right hidden md:table-cell text-[11px] text-slate-400 whitespace-nowrap">
                <Link to={`/app/placement/${test.id}`} onClick={(e) => e.stopPropagation()} className="hover:text-slate-700">
                    {fmtDate(test.created_at)}
                </Link>
            </td>
        </tr>
    );
}

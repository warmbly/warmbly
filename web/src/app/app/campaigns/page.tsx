import { useUserProfile } from "@/hooks/context/user";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import useStartCampaign from "@/lib/api/hooks/app/campaigns/useStartCampaign";
import useStopCampaign from "@/lib/api/hooks/app/campaigns/useStopCampaign";
import { useConfirm } from "@/hooks/context/confirm";
import { NewCampaignDialog } from "@/components/app/campaigns/NewCampaignDialog";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import {
    AlertTriangleIcon,
    CalendarIcon,
    FilterIcon,
    FolderIcon,
    Loader2Icon,
    PauseIcon,
    PlayIcon,
    PlusIcon,
    RefreshCcwIcon,
    Settings2Icon,
} from "lucide-react";
import {
    EmptyBlock,
    Page,
    PageBody,
    PageTopbar,
    SectionBar,
    Stat,
    StatStrip,
    TopbarAction,
} from "@/components/layout/Page";
import { SearchInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";

type StatusFilter = "all" | "active" | "paused" | "draft";
type SortMode = "newest" | "oldest" | "name";

export default function CampaignsPage() {
    const p = useUserProfile();
    const confirm = useConfirm();
    const startCampaign = useStartCampaign();
    const stopCampaign = useStopCampaign();
    const [folder, setFolder] = useState<string>("");
    const [query, setQuery] = useState<string>("");
    const [status, setStatus] = useState<StatusFilter>("all");
    const [sort, setSort] = useState<SortMode>("newest");
    const [newOpen, setNewOpen] = useState<boolean>(false);

    async function toggleCampaign(id: string, currentStatus: string) {
        try {
            if (currentStatus === "active") {
                await toast.promise(stopCampaign.mutateAsync(id), {
                    loading: "Pausing campaign…",
                    success: "Campaign paused",
                    error: (e: AppError) => buildError(e),
                });
            } else {
                await toast.promise(startCampaign.mutateAsync(id), {
                    loading: "Starting campaign…",
                    success: "Campaign started",
                    error: (e: AppError) => buildError(e),
                });
            }
        } catch {
            /* toast.promise already surfaced */
        }
    }

    const campaignsData = useCampaigns({ query, folder });
    const campaigns = campaignsData.campaigns ?? [];

    const folders = p.user.folders ?? [];
    const activeFolder = folders.find((f) => f.id === folder);

    const filtered = useMemo(() => {
        const base = status === "all"
            ? campaigns
            : campaigns.filter((c) => (c.status ?? "draft") === status);
        const sorted = [...base];
        if (sort === "newest") {
            sorted.sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime());
        } else if (sort === "oldest") {
            sorted.sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime());
        } else {
            sorted.sort((a, b) => (a.name ?? "").localeCompare(b.name ?? ""));
        }
        return sorted;
    }, [campaigns, status, sort]);

    const counts = useMemo(() => {
        const stats = { total: campaigns.length, active: 0, paused: 0, draft: 0 };
        for (const c of campaigns) {
            const s = c.status ?? "draft";
            if (s === "active") stats.active++;
            else if (s === "paused") stats.paused++;
            else stats.draft++;
        }
        return stats;
    }, [campaigns]);

    return (
        <Page>
            <PageTopbar
                eyebrow="Campaigns"
                subtitle={
                    campaignsData.isPending
                        ? "Loading…"
                        : campaignsData.isError
                            ? "Failed to load"
                            : `${campaigns.length} ${campaigns.length === 1 ? "sequence" : "sequences"}`
                }
            >
                <TopbarAction
                    variant="ghost"
                    icon={<Settings2Icon className="w-3 h-3" />}
                    onClick={() => p.setFoldersEdit(true)}
                >
                    Folders
                </TopbarAction>
                <TopbarAction
                    icon={<PlusIcon className="w-3 h-3" />}
                    onClick={() => setNewOpen(true)}
                >
                    New campaign
                </TopbarAction>
            </PageTopbar>

            <StatStrip cols={4}>
                <Stat
                    label="All"
                    value={counts.total}
                    sub="campaigns"
                    onClick={() => setStatus("all")}
                />
                <Stat
                    label="Active"
                    value={counts.active}
                    sub="sending now"
                    accent={counts.active > 0}
                    onClick={() => setStatus("active")}
                />
                <Stat
                    label="Paused"
                    value={counts.paused}
                    sub="resumable"
                    onClick={() => setStatus("paused")}
                />
                <Stat
                    label="Draft"
                    value={counts.draft}
                    sub="not started"
                    last
                    onClick={() => setStatus("draft")}
                />
            </StatStrip>

            <SectionBar
                label={status === "all" ? "All campaigns" : `${status[0].toUpperCase()}${status.slice(1)}`}
                count={filtered.length}
            >
                <SearchInput
                    value={query}
                    onChange={setQuery}
                    placeholder="Search campaigns…"
                    className="w-56"
                />

                <PopoverMenu align="end">
                    <PopoverMenuTrigger asChild>
                        <SelectButton
                            icon={<FolderIcon className="w-3.5 h-3.5" />}
                            label={activeFolder?.title ?? "All folders"}
                        />
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={200}>
                        <PopoverMenuLabel>Folders</PopoverMenuLabel>
                        <PopoverMenuItem
                            onSelect={() => setFolder("")}
                            selected={!folder}
                        >
                            All folders
                        </PopoverMenuItem>
                        {folders.map((f) => (
                            <PopoverMenuItem
                                key={f.id}
                                onSelect={() => setFolder(folder === f.id ? "" : f.id)}
                                icon={<span className="size-2 rounded-full" style={{ backgroundColor: f.color }} />}
                                selected={folder === f.id}
                            >
                                {f.title}
                            </PopoverMenuItem>
                        ))}
                        <PopoverMenuSeparator />
                        <PopoverMenuItem
                            onSelect={() => p.setFoldersEdit(true)}
                            icon={<Settings2Icon className="w-3 h-3" />}
                        >
                            Manage folders
                        </PopoverMenuItem>
                    </PopoverMenuContent>
                </PopoverMenu>

                <PopoverMenu align="end">
                    <PopoverMenuTrigger asChild>
                        <SelectButton
                            icon={<FilterIcon className="w-3.5 h-3.5" />}
                            label={
                                sort === "newest"
                                    ? "Newest"
                                    : sort === "oldest"
                                        ? "Oldest"
                                        : "Name"
                            }
                        />
                    </PopoverMenuTrigger>
                    <PopoverMenuContent>
                        <PopoverMenuLabel>Sort</PopoverMenuLabel>
                        <PopoverMenuItem
                            selected={sort === "newest"}
                            onSelect={() => setSort("newest")}
                        >
                            Newest first
                        </PopoverMenuItem>
                        <PopoverMenuItem
                            selected={sort === "oldest"}
                            onSelect={() => setSort("oldest")}
                        >
                            Oldest first
                        </PopoverMenuItem>
                        <PopoverMenuItem
                            selected={sort === "name"}
                            onSelect={() => setSort("name")}
                        >
                            Name (A–Z)
                        </PopoverMenuItem>
                    </PopoverMenuContent>
                </PopoverMenu>
            </SectionBar>

            <PageBody>
                {campaignsData.isPending ? (
                    <SkeletonRows />
                ) : campaignsData.isError ? (
                    <ErrorState
                        message={
                            campaignsData.error?.message ||
                            "The request failed. The backend may be down or returning an error."
                        }
                        onRetry={() => campaignsData.refetch()}
                        isRefetching={campaignsData.isFetching}
                    />
                ) : filtered.length === 0 ? (
                    campaigns.length === 0 ? (
                        <EmptyBlock
                            title="No campaigns yet"
                            body="Create your first sequence to start reaching prospects."
                            cta={
                                <TopbarAction
                                    icon={<PlusIcon className="w-3 h-3" />}
                                    onClick={() => setNewOpen(true)}
                                >
                                    New campaign
                                </TopbarAction>
                            }
                        />
                    ) : (
                        <EmptyBlock
                            title={`No ${status} campaigns`}
                            body={`Switch to “All” to see every sequence.`}
                            cta={
                                <TopbarAction onClick={() => setStatus("all")} variant="ghost">
                                    Show all
                                </TopbarAction>
                            }
                        />
                    )
                ) : (
                    <div className="divide-y divide-slate-200/60">
                        {filtered.map((c) => {
                            const cstatus = c.status ?? "draft";
                            const dot =
                                cstatus === "active"
                                    ? "bg-emerald-500"
                                    : cstatus === "paused"
                                        ? "bg-amber-500"
                                        : "bg-slate-300";
                            const stateLabel =
                                cstatus === "active" ? "running" : cstatus;
                            const StateIcon =
                                cstatus === "active" ? PauseIcon : PlayIcon;
                            return (
                                <Link
                                    key={c.id}
                                    to={`/app/campaigns/${c.id}`}
                                    className="group h-11 px-5 flex items-center gap-3 hover:bg-slate-50 transition-colors"
                                >
                                    <span className={`size-1.5 rounded-full shrink-0 ${dot}`} />
                                    <span className="text-[12.5px] text-slate-900 font-medium truncate max-w-[40%]">
                                        {c.name}
                                    </span>
                                    <span className="font-mono text-[10.5px] text-slate-400 tabular-nums shrink-0">
                                        {c.id.slice(0, 8)}
                                    </span>
                                    {c.description && (
                                        <span className="text-[11.5px] text-slate-400 truncate hidden md:inline">
                                            {c.description}
                                        </span>
                                    )}
                                    <span className="ml-auto text-[10px] uppercase tracking-[0.1em] text-slate-500 font-medium shrink-0">
                                        {stateLabel}
                                    </span>
                                    <span className="font-mono text-[10.5px] text-slate-400 tabular-nums flex items-center gap-1 shrink-0">
                                        <CalendarIcon className="w-3 h-3" />
                                        {c.created_at
                                            ? new Date(c.created_at).toLocaleDateString("en-US", {
                                                month: "short",
                                                day: "numeric",
                                            })
                                            : "—"}
                                    </span>
                                    <button
                                        type="button"
                                        onClick={(e) => {
                                            e.preventDefault();
                                            e.stopPropagation();
                                            const action =
                                                cstatus === "active" ? "Pause" : "Start";
                                            confirm?.show(
                                                `${action} ${c.name}?`,
                                                () => toggleCampaign(c.id, cstatus),
                                            );
                                        }}
                                        disabled={
                                            (cstatus === "active" && stopCampaign.isPending) ||
                                            (cstatus !== "active" && startCampaign.isPending)
                                        }
                                        className="size-6 rounded text-slate-400 hover:text-slate-900 hover:bg-slate-100 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity shrink-0 disabled:opacity-30"
                                        aria-label={
                                            cstatus === "active" ? "Pause campaign" : "Start campaign"
                                        }
                                    >
                                        <StateIcon className="w-3.5 h-3.5" />
                                    </button>
                                </Link>
                            );
                        })}
                    </div>
                )}
            </PageBody>

            <NewCampaignDialog open={newOpen} onClose={() => setNewOpen(false)} />
        </Page>
    );
}

function ErrorState({
    message,
    onRetry,
    isRefetching,
}: {
    message: string;
    onRetry: () => void;
    isRefetching: boolean;
}) {
    return (
        <div className="px-5 py-12 text-center">
            <div className="mx-auto mb-3 size-8 rounded-md bg-red-50 text-red-600 flex items-center justify-center">
                <AlertTriangleIcon className="w-4 h-4" />
            </div>
            <p className="text-[12.5px] text-slate-900 font-medium">Couldn't load campaigns</p>
            <p className="text-[11.5px] text-slate-500 mt-1 max-w-[44ch] mx-auto leading-relaxed">
                {message}
            </p>
            <div className="mt-4 flex items-center justify-center gap-1.5">
                <button
                    type="button"
                    onClick={onRetry}
                    disabled={isRefetching}
                    className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                >
                    {isRefetching ? (
                        <Loader2Icon className="w-3 h-3 animate-spin" />
                    ) : (
                        <RefreshCcwIcon className="w-3 h-3" />
                    )}
                    Try again
                </button>
                <button
                    type="button"
                    onClick={() => window.location.reload()}
                    className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12px] font-medium transition-colors"
                >
                    Reload page
                </button>
            </div>
        </div>
    );
}

function SkeletonRows() {
    return (
        <div className="divide-y divide-slate-200/60">
            {Array.from({ length: 8 }).map((_, i) => (
                <div key={i} className="h-11 px-5 flex items-center gap-3">
                    <div className="size-1.5 rounded-full bg-slate-200" />
                    <div className="h-3 w-44 bg-slate-100 rounded animate-pulse" />
                    <div className="font-mono h-3 w-12 bg-slate-100 rounded animate-pulse" />
                    <div className="ml-auto h-3 w-16 bg-slate-100 rounded animate-pulse" />
                </div>
            ))}
        </div>
    );
}

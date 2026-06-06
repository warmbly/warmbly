import { useUserProfile } from "@/hooks/context/user";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import useStartCampaign from "@/lib/api/hooks/app/campaigns/useStartCampaign";
import useStopCampaign from "@/lib/api/hooks/app/campaigns/useStopCampaign";
import useUpdateCampaign from "@/lib/api/hooks/app/campaigns/useUpdateCampaign";
import { useConfirm } from "@/hooks/context/confirm";
import { NewCampaignDialog } from "@/components/app/campaigns/NewCampaignDialog";
import LaunchCampaignDialog from "@/components/app/campaigns/LaunchCampaignDialog";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import type Folder from "@/lib/api/models/app/Folder";
import { cn, hexToRgba } from "@/lib/utils";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import {
    AlertTriangleIcon,
    CalendarIcon,
    CheckIcon,
    CheckCircle2Icon,
    FileTextIcon,
    FilterIcon,
    FolderIcon,
    Loader2Icon,
    type LucideIcon,
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

type StatusFilter = "all" | "active" | "paused" | "draft" | "completed";
type SortMode = "newest" | "oldest" | "name";

// Collapse the backend's raw campaign statuses into the buckets the list
// filters/counts by. paused_no_accounts / paused_trial_expired are auto-pause
// variants, so they belong with "paused"; anything unknown reads as draft.
function statusBucket(s?: string): "active" | "paused" | "completed" | "draft" {
    if (s === "active") return "active";
    if (s === "completed") return "completed";
    if (s && s.startsWith("paused")) return "paused";
    return "draft";
}

// Per-state label + leading mark for a campaign row. "active" renders the
// animated dot-grid loader; every other state is a 14px lucide icon so the
// fixed-width leading slot keeps each row's name aligned.
const STATUS_LABEL: Record<string, string> = {
    active: "running",
    paused: "paused",
    paused_no_accounts: "no accounts",
    paused_trial_expired: "trial expired",
    completed: "finished",
    draft: "draft",
};

// Single source of truth for a status's color — drives BOTH the leading mark
// and the right-side text label so they always agree. emerald = live/done,
// amber = paused/needs-attention, slate = not started.
const STATUS_TONE: Record<string, string> = {
    active: "text-emerald-600",
    completed: "text-emerald-600",
    paused: "text-amber-600",
    paused_no_accounts: "text-amber-600",
    paused_trial_expired: "text-amber-600",
    draft: "text-slate-500",
};

function statusTone(status: string): string {
    return STATUS_TONE[status] ?? STATUS_TONE.draft;
}

function CampaignStatusMark({ status }: { status: string }) {
    const tone = statusTone(status);
    if (status === "active") {
        return <span className={cn("campaign-grid", tone)} aria-hidden title="Sending now" />;
    }
    let Icon: LucideIcon = FileTextIcon;
    let title = "Draft — not started";
    if (status === "completed") {
        Icon = CheckCircle2Icon;
        title = "Finished";
    } else if (status === "paused") {
        Icon = PauseIcon;
        title = "Paused";
    } else if (status === "paused_no_accounts" || status === "paused_trial_expired") {
        Icon = AlertTriangleIcon;
        title = status === "paused_no_accounts" ? "Paused — no sending accounts" : "Paused — trial expired";
    }
    return <Icon className={cn("w-3.5 h-3.5", tone)} aria-label={title} />;
}

// Read-only chips showing which folders a campaign belongs to — resolves the
// campaign's folder ids against the user's folders, shows up to 2 then "+N".
// Each chip is tinted with the folder's own color for a quick visual read.
function CampaignFolderChips({ campaign, folders }: { campaign: Campaign; folders: Folder[] }) {
    const mine = (campaign.folders ?? [])
        .map((id) => folders.find((f) => f.id === id))
        .filter((f): f is Folder => !!f);
    if (mine.length === 0) return null;
    const shown = mine.slice(0, 2);
    const extra = mine.length - shown.length;
    return (
        <span className="hidden sm:flex items-center gap-1.5 shrink-0">
            {shown.map((f) => (
                <span
                    key={f.id}
                    title={f.title}
                    className="inline-flex items-center gap-1.5 h-[18px] pl-1.5 pr-2 rounded-full text-[10.5px] font-medium text-slate-700 max-w-[130px]"
                    style={{ backgroundColor: hexToRgba(f.color, 0.16) }}
                >
                    <span
                        className="inline-block size-2 rounded-full ring-1 ring-black/5 shrink-0"
                        style={{ backgroundColor: f.color }}
                    />
                    <span className="truncate">{f.title}</span>
                </span>
            ))}
            {extra > 0 && (
                <span
                    className="inline-flex items-center h-[18px] px-1.5 rounded-full text-[10.5px] font-medium text-slate-500 bg-slate-100"
                    title={mine.slice(2).map((f) => f.title).join(", ")}
                >
                    +{extra}
                </span>
            )}
        </span>
    );
}

// Per-row "move to folder" control: a folder button (revealed on hover, or
// kept visible + sky when the campaign is already filed) that opens a popover
// of the user's folders. Toggling an item PATCHes the campaign's `folders`
// array; the menu stays open so several folders can be toggled at once.
function CampaignFolderMenu({ campaign, folders }: { campaign: Campaign; folders: Folder[] }) {
    const p = useUserProfile();
    const update = useUpdateCampaign(campaign.id);
    const current = campaign.folders ?? [];
    const inCount = current.length;

    function setFolders(next: string[]) {
        update.mutate(
            { folders: next },
            { onError: (e) => toast.error(buildError(e as unknown as AppError)) },
        );
    }

    return (
        <PopoverMenu align="end">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    aria-label="Move to folder"
                    title={
                        inCount > 0
                            ? `In ${inCount} folder${inCount === 1 ? "" : "s"}`
                            : "Move to folder"
                    }
                    className={cn(
                        "size-6 rounded flex items-center justify-center transition-opacity shrink-0",
                        inCount > 0
                            ? "text-sky-600 hover:bg-sky-50 opacity-100"
                            : "text-slate-400 hover:text-slate-900 hover:bg-slate-100 opacity-100 md:opacity-0 md:group-hover:opacity-100",
                    )}
                >
                    <FolderIcon className="w-3.5 h-3.5" />
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={200}>
                <PopoverMenuLabel>Folders</PopoverMenuLabel>
                {folders.length === 0 ? (
                    <PopoverMenuItem
                        onSelect={() => p.setFoldersEdit(true)}
                        icon={<PlusIcon className="w-3 h-3" />}
                    >
                        Create a folder
                    </PopoverMenuItem>
                ) : (
                    folders.map((f) => {
                        const isIn = current.includes(f.id);
                        return (
                            <PopoverMenuItem
                                key={f.id}
                                closeOnSelect={false}
                                selected={isIn}
                                onSelect={() =>
                                    setFolders(
                                        isIn
                                            ? current.filter((x) => x !== f.id)
                                            : [...current, f.id],
                                    )
                                }
                                icon={
                                    <span
                                        className="inline-block size-2.5 rounded-full ring-1 ring-black/5"
                                        style={{ backgroundColor: f.color }}
                                    />
                                }
                                trailing={
                                    isIn ? (
                                        <CheckIcon className="w-3.5 h-3.5 text-sky-600" strokeWidth={2.5} />
                                    ) : null
                                }
                            >
                                {f.title}
                            </PopoverMenuItem>
                        );
                    })
                )}
                {inCount > 0 && (
                    <>
                        <PopoverMenuSeparator />
                        <PopoverMenuItem danger onSelect={() => setFolders([])}>
                            Remove from all
                        </PopoverMenuItem>
                    </>
                )}
                <PopoverMenuSeparator />
                <PopoverMenuItem
                    onSelect={() => p.setFoldersEdit(true)}
                    icon={<Settings2Icon className="w-3 h-3" />}
                >
                    Manage folders
                </PopoverMenuItem>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

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
    const [launchTarget, setLaunchTarget] = useState<Campaign | null>(null);

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
            : campaigns.filter((c) => statusBucket(c.status) === status);
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
        const stats = { total: campaigns.length, active: 0, paused: 0, draft: 0, completed: 0 };
        for (const c of campaigns) {
            stats[statusBucket(c.status)]++;
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

            <StatStrip cols={5}>
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
                    onClick={() => setStatus("draft")}
                />
                <Stat
                    label="Done"
                    value={counts.completed}
                    sub="finished"
                    last
                    onClick={() => setStatus("completed")}
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
                    className="w-full sm:w-56"
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
                                icon={<span className="inline-block size-2 rounded-full" style={{ backgroundColor: f.color }} />}
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
                            const stateLabel = STATUS_LABEL[cstatus] ?? cstatus;
                            const StateIcon =
                                cstatus === "active" ? PauseIcon : PlayIcon;
                            return (
                                <Link
                                    key={c.id}
                                    to={`/app/campaigns/${c.id}`}
                                    className="group h-11 px-5 flex items-center gap-3 hover:bg-slate-50 transition-colors"
                                >
                                    {/* Fixed-width leading slot so every row's name aligns,
                                        whatever state mark (loader or icon) sits in it. */}
                                    <span className="shrink-0 w-3.5 flex items-center justify-center">
                                        <CampaignStatusMark status={cstatus} />
                                    </span>
                                    <span className="text-[12.5px] text-slate-900 font-medium truncate max-w-[40%]">
                                        {c.name}
                                    </span>
                                    <span className="font-mono text-[10.5px] text-slate-400 tabular-nums shrink-0">
                                        {c.id.slice(0, 8)}
                                    </span>
                                    <CampaignFolderChips campaign={c} folders={folders} />
                                    {c.description && (
                                        <span className="text-[11.5px] text-slate-400 truncate hidden md:inline">
                                            {c.description}
                                        </span>
                                    )}
                                    <span className={cn("ml-auto text-[10px] uppercase tracking-[0.1em] font-medium shrink-0", statusTone(cstatus))}>
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
                                    <CampaignFolderMenu campaign={c} folders={folders} />
                                    <button
                                        type="button"
                                        onClick={(e) => {
                                            e.preventDefault();
                                            e.stopPropagation();
                                            if (cstatus === "active") {
                                                confirm?.show(
                                                    `Pause ${c.name}?`,
                                                    () => toggleCampaign(c.id, cstatus),
                                                );
                                            } else {
                                                setLaunchTarget(c);
                                            }
                                        }}
                                        disabled={
                                            (cstatus === "active" && stopCampaign.isPending) ||
                                            (cstatus !== "active" && startCampaign.isPending)
                                        }
                                        className="size-6 rounded text-slate-400 hover:text-slate-900 hover:bg-slate-100 flex items-center justify-center opacity-100 md:opacity-0 md:group-hover:opacity-100 transition-opacity shrink-0 disabled:opacity-30"
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
            <LaunchCampaignDialog
                campaign={launchTarget}
                onClose={() => setLaunchTarget(null)}
                onConfirm={(id) => startCampaign.mutateAsync(id)}
            />
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

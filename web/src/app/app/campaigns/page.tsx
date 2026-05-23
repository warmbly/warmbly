import { useUserProfile } from "@/hooks/context/user";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import {
    CalendarIcon,
    FilterIcon,
    FolderIcon,
    PauseIcon,
    PlayIcon,
    PlusIcon,
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

export default function CampaignsPage() {
    const p = useUserProfile();
    const [folder, setFolder] = useState<string>("");
    const [query, setQuery] = useState<string>("");
    const [status, setStatus] = useState<StatusFilter>("all");

    const campaignsData = useCampaigns({ query, folder });
    const campaigns = campaignsData.campaigns ?? [];

    const folders = p.user.folders ?? [];
    const activeFolder = folders.find((f) => f.id === folder);

    const filtered = useMemo(
        () => (status === "all" ? campaigns : campaigns.filter((c) => (c.status ?? "draft") === status)),
        [campaigns, status],
    );

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
                >
                    Folders
                </TopbarAction>
                <TopbarAction icon={<PlusIcon className="w-3 h-3" />}>
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
                            label="More"
                        />
                    </PopoverMenuTrigger>
                    <PopoverMenuContent>
                        <PopoverMenuLabel>Sort</PopoverMenuLabel>
                        <PopoverMenuItem selected>Newest first</PopoverMenuItem>
                        <PopoverMenuItem>Oldest first</PopoverMenuItem>
                        <PopoverMenuItem>Name (A–Z)</PopoverMenuItem>
                    </PopoverMenuContent>
                </PopoverMenu>
            </SectionBar>

            <PageBody>
                {campaignsData.isPending ? (
                    <SkeletonRows />
                ) : campaignsData.isError ? (
                    <EmptyBlock
                        title="Couldn't load campaigns"
                        body={
                            campaignsData.error?.message ||
                            "The request failed. Check the backend is up."
                        }
                        cta={
                            <TopbarAction onClick={() => campaignsData.refetch()} variant="ghost">
                                Retry
                            </TopbarAction>
                        }
                    />
                ) : filtered.length === 0 ? (
                    campaigns.length === 0 ? (
                        <EmptyBlock
                            title="No campaigns yet"
                            body="Create your first sequence to start reaching prospects."
                            cta={
                                <TopbarAction icon={<PlusIcon className="w-3 h-3" />}>
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
                                            /* TODO toggle */
                                        }}
                                        className="size-6 rounded text-slate-400 hover:text-slate-900 hover:bg-slate-100 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity shrink-0"
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
        </Page>
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
